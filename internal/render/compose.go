package render

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg" // background images
	_ "image/png"
	"math"
	"os"

	_ "golang.org/x/image/webp"
)

// Frame is the geometry of a document rendered at some scale, in canvas pixels.
type Frame struct {
	Layout Layout
	// Border thickness in canvas pixels.
	Border float32
	// Card plane to canvas, when HasProjection.
	Projection    Homography
	HasProjection bool
}

func NewFrame(doc EditDoc, baseW, baseH uint32, scale float32) Frame {
	crop := doc.CropRect(baseW, baseH)
	local := R{0, 0, crop.W, crop.H}
	border := doc.Effects.Border.Points()
	l := LayoutWithBorder(doc.Style, local, doc.DPI, scale, border)
	b := BorderPx(border, l.Unit)
	h, ok := Projection(l.Card.Inflate(b), float32(l.Width), float32(l.Height), doc.Effects.Perspective)
	return Frame{l, b, h, ok}
}

func (f Frame) OuterCard() R { return f.Layout.Card.Inflate(f.Border) }

// ToCanvas maps image (annotation) coordinates to canvas pixels, through the projection.
func (f Frame) ToCanvas(p Pt) Pt {
	c := f.Layout.ToCanvas(p)
	if f.HasProjection {
		return f.Projection.Apply(c)
	}
	return c
}

// ToImage maps canvas pixels to image coordinates; ok is false behind the camera.
func (f Frame) ToImage(p Pt) (Pt, bool) {
	flat := p
	if f.HasProjection {
		inv, ok := f.Projection.Inverse()
		if !ok || inv.WAt(p) <= 0 {
			return Pt{}, false
		}
		flat = inv.Apply(p)
	}
	return f.Layout.ToSource(flat), true
}

// Sources are the images a document renders from.
type Sources struct {
	Base *image.RGBA
	// The decoded ImageBackground, if the document uses one.
	BgImage *image.RGBA
	// MakeBlurBg(Base), if the document uses BlurredBackground.
	BlurBg *image.RGBA
}

type preparedKey struct{ x, y, w, h, iw, ih uint32 }

// Compositor renders documents, reusing the screenshot, scene and card between renders that
// share them. It is not safe for concurrent use.
type Compositor struct {
	preparedKey preparedKey
	prepared    *image.RGBA
	sceneKey    string
	scene       *image.RGBA
	cardKey     string
	card        *image.RGBA
	cardMask    *image.Alpha
}

func clonePixmap(m *image.RGBA) *image.RGBA {
	if m == nil {
		return nil
	}
	c := image.NewRGBA(m.Rect)
	copy(c.Pix, m.Pix)
	return c
}

func cloneMask(m *image.Alpha) *image.Alpha {
	if m == nil {
		return nil
	}
	c := image.NewAlpha(m.Rect)
	copy(c.Pix, m.Pix)
	return c
}

// Render renders region (canvas pixels at scale; the whole canvas when nil) of doc.
func (c *Compositor) Render(doc EditDoc, src Sources, scale float32, region *R) *image.RGBA {
	bw, bh := uint32(src.Base.Rect.Dx()), uint32(src.Base.Rect.Dy())
	f := NewFrame(doc, bw, bh, scale)
	l := f.Layout
	canvas := R{0, 0, float32(l.Width), float32(l.Height)}
	reg := canvas
	if region != nil {
		if r, ok := region.RoundOut().Intersect(canvas); ok {
			reg = r
		}
	}
	blur := doc.Effects.Blur
	extent := canvas
	if blur.Edge == BlurClipped {
		extent = l.Image
	}
	var pad float32
	if blur.IsActive() {
		pad = ceil32(BlurRadius(blur, min(extent.W, extent.H)) * 3)
	}
	rp, ok := reg.Inflate(pad).RoundOut().Intersect(canvas)
	if !ok {
		rp = reg
	}

	scene := c.renderScene(doc, src, f, rp)

	cardRegion := rp
	if f.HasProjection {
		cardRegion = canvas
		if pre, ok := preimage(f.Projection, rp); ok {
			if r, ok := pre.Inflate(2 + pad).RoundOut().Intersect(canvas); ok {
				cardRegion = r
			}
		}
	}
	lc := l.Translated(-cardRegion.X, -cardRegion.Y)
	card, mask := c.renderCard(doc, src, f, cardRegion)
	drawHighlights(card, lc, doc.Annotations, mask)
	marks := image.NewRGBA(card.Rect)
	for _, a := range doc.Annotations {
		if !a.IsRedaction() {
			DrawAnnotation(marks, lc, a)
		}
	}
	DrawPixmap(card, 0, 0, marks, mask)
	DrawWatermark(card, lc.Image, l.Unit, doc.Effects.Watermark, mask)
	if blur.IsActive() && blur.Edge == BlurClipped {
		ProgressiveBlurArea(card, lc.Image, lc.Image, blur, mask)
	}
	if f.HasProjection {
		WarpOnto(scene, Pt{rp.X, rp.Y}, card, Pt{cardRegion.X, cardRegion.Y}, f.Projection)
	} else {
		DrawPixmap(scene, int(cardRegion.X-rp.X), int(cardRegion.Y-rp.Y), card, nil)
	}
	if blur.IsActive() && blur.Edge == BlurBleed {
		full := R{0, 0, float32(scene.Rect.Dx()), float32(scene.Rect.Dy())}
		ProgressiveBlurArea(scene, full, canvas.Translate(-rp.X, -rp.Y), blur, nil)
	}
	if rp == reg {
		return scene
	}
	r := reg.Translate(-rp.X, -rp.Y)
	return subImage(scene, image.Rect(int(r.X), int(r.Y), int(r.X)+int(r.W), int(r.Y)+int(r.H)))
}

func styleKey(s Style) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func (c *Compositor) renderScene(doc EditDoc, src Sources, f Frame, rp R) *image.RGBA {
	st := doc.Style
	key := fmt.Sprintf("%s|%v|%v|%v|%p|%p", styleKey(st), f, rp, doc.Effects.Perspective, src.BlurBg, src.BgImage)
	if c.scene != nil && c.sceneKey == key {
		return clonePixmap(c.scene)
	}
	l := f.Layout
	pm := image.NewRGBA(image.Rect(0, 0, int(rp.W), int(rp.H)))
	DrawBackground(pm, st.Background, float32(l.Width), float32(l.Height), Pt{-rp.X, -rp.Y}, src.BlurBg, src.BgImage)
	if st.Shadow > 0 && st.ShadowOpacity > 0 {
		outline := roundedOutline(f.OuterCard(), st.Radius*l.Unit)
		for i, p := range outline {
			if f.HasProjection {
				p = f.Projection.Apply(p)
			}
			outline[i] = Pt{p.X - rp.X, p.Y - rp.Y}
		}
		if path := PolygonPath(outline); path != nil {
			DrawShadow(pm, path, st.Shadow*l.Unit*0.5, st.ShadowOpacity)
		}
	}
	c.sceneKey, c.scene = key, clonePixmap(pm)
	return pm
}

func (c *Compositor) renderCard(doc EditDoc, src Sources, f Frame, cr R) (*image.RGBA, *image.Alpha) {
	bw, bh := uint32(src.Base.Rect.Dx()), uint32(src.Base.Rect.Dy())
	crop := doc.CropRect(bw, bh)
	l := f.Layout
	pk := preparedKey{uint32(crop.X), uint32(crop.Y), uint32(crop.W), uint32(crop.H), uint32(l.Image.W), uint32(l.Image.H)}
	if c.prepared == nil || c.preparedKey != pk {
		c.preparedKey, c.prepared = pk, PrepareImage(src.Base, crop, int(pk.iw), int(pk.ih))
	}
	lc := l.Translated(-cr.X, -cr.Y)
	reds := redactions(doc.Annotations)
	redJSON, _ := json.Marshal(reds)
	title := ""
	if doc.WindowTitle != nil {
		title = "t:" + *doc.WindowTitle
	}
	key := fmt.Sprintf("%s|%v|%v|%v|%s|%q|%p|%p", styleKey(doc.Style), f, cr, doc.Effects.Border, redJSON, title, src.BlurBg, src.BgImage)
	if c.card != nil && c.cardKey == key {
		return clonePixmap(c.card), cloneMask(c.cardMask)
	}
	in := Inputs{Style: doc.Style, Layout: lc, Image: c.prepared, BlurBg: src.BlurBg, BgImage: src.BgImage, Title: doc.WindowTitle}
	pm := image.NewRGBA(image.Rect(0, 0, int(cr.W), int(cr.H)))
	mask := drawCard(pm, in, f.Border, doc.Effects.Border.Color)
	applyRedactions(pm, lc, c.prepared, reds, mask)
	c.cardKey, c.card, c.cardMask = key, clonePixmap(pm), cloneMask(mask)
	return pm, mask
}

func preimage(h Homography, r R) (R, bool) {
	inv, ok := h.Inverse()
	if !ok {
		return R{}, false
	}
	corners := [4]Pt{{r.X, r.Y}, {r.Right(), r.Y}, {r.Right(), r.Bottom()}, {r.X, r.Bottom()}}
	x0, y0 := float32(math.MaxFloat32), float32(math.MaxFloat32)
	x1, y1 := -float32(math.MaxFloat32), -float32(math.MaxFloat32)
	for _, c := range corners {
		if inv.WAt(c) <= 0 {
			return R{}, false
		}
		p := inv.Apply(c)
		x0, y0, x1, y1 = min(x0, p.X), min(y0, p.Y), max(x1, p.X), max(y1, p.Y)
	}
	return R{x0, y0, x1 - x0, y1 - y0}, true
}

func roundedOutline(r R, radius float32) []Pt {
	rad := max(min(radius, r.W/2, r.H/2), 0)
	centers := [4]struct {
		c     Pt
		start float32
	}{
		{Pt{r.Right() - rad, r.Y + rad}, -90},
		{Pt{r.Right() - rad, r.Bottom() - rad}, 0},
		{Pt{r.X + rad, r.Bottom() - rad}, 90},
		{Pt{r.X + rad, r.Y + rad}, 180},
	}
	out := make([]Pt, 0, 36)
	for _, cs := range centers {
		for i := 0; i <= 8; i++ {
			sin, cos := sinCos32(cs.start + float32(i)*90/8)
			out = append(out, Pt{cs.c.X + rad*cos, cs.c.Y + rad*sin})
		}
	}
	return out
}

// LoadBackground decodes a background image (PNG, JPEG or WebP), capped at 3200 px on the
// long side. Straight-alpha images are resized before premultiplying, as the Rust code does.
func LoadBackground(path string) (*image.RGBA, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	img, _, err := image.Decode(file)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	var raw *image.RGBA
	straight := false
	if n, ok := img.(*image.NRGBA); ok {
		raw, straight = &image.RGBA{Pix: n.Pix, Stride: n.Stride, Rect: n.Rect}, true
		if n.Rect.Min != (image.Point{}) {
			raw = subImage(raw, n.Rect)
		}
	} else {
		raw = image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(raw, raw.Rect, img, b.Min, draw.Src)
	}
	w, h := raw.Rect.Dx(), raw.Rect.Dy()
	if long := max(w, h); long > 3200 {
		k := float32(3200) / float32(long)
		raw = Resize(raw, int(round32(float32(w)*k)), int(round32(float32(h)*k)))
	}
	if straight {
		return Premultiply(toNRGBAView(raw)), nil
	}
	return raw, nil
}

// RenderDoc renders a whole document at 1x, loading its background image if it has one.
func RenderDoc(base *image.RGBA, doc EditDoc) *image.RGBA {
	var src Sources
	src.Base = base
	switch bg := doc.Style.Background.(type) {
	case ImageBackground:
		if img, err := LoadBackground(bg.Path); err == nil {
			src.BgImage = img
		}
	case BlurredBackground:
		src.BlurBg = MakeBlurBg(base)
	}
	var c Compositor
	return c.Render(doc, src, 1, nil)
}
