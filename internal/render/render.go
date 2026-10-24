package render

import (
	"image"
	"strconv"
)

// Layout is the geometry of a styled screenshot on its canvas.
type Layout struct {
	Width, Height int
	// Canvas pixels per source pixel.
	Scale float32
	// Points to canvas pixels (DPI scale × render scale).
	Unit    float32
	Card    R
	Image   R
	ChromeH float32
	// Region of the source screenshot shown, in source pixels.
	Crop R
}

func (l Layout) ToCanvas(p Pt) Pt {
	return Pt{l.Image.X + (p.X-l.Crop.X)*l.Scale, l.Image.Y + (p.Y-l.Crop.Y)*l.Scale}
}

func (l Layout) ToSource(p Pt) Pt {
	return Pt{(p.X-l.Image.X)/l.Scale + l.Crop.X, (p.Y-l.Image.Y)/l.Scale + l.Crop.Y}
}

func (l Layout) RectToCanvas(r R) R {
	a := l.ToCanvas(Pt{r.X, r.Y})
	return R{a.X, a.Y, r.W * l.Scale, r.H * l.Scale}
}

// Translated moves every canvas position by (dx, dy); used to render a region.
func (l Layout) Translated(dx, dy float32) Layout {
	l.Card = l.Card.Translate(dx, dy)
	l.Image = l.Image.Translate(dx, dy)
	return l
}

// LayoutFor lays out style around crop. dpi is the scale factor of the capture's monitor,
// scale the render scale.
func LayoutFor(style Style, crop R, dpi, scale float32) Layout {
	return LayoutWithBorder(style, crop, dpi, scale, 0)
}

// LayoutWithBorder leaves room for a border of border points around the card. Card stays the
// inner card; the border ring is Card.Inflate(BorderPx(border, unit)).
func LayoutWithBorder(style Style, crop R, dpi, scale, border float32) Layout {
	unit := dpi * scale
	iw := max(round32(crop.W*scale), 1)
	ih := max(round32(crop.H*scale), 1)
	pad := round32(style.Padding * unit)
	inset := round32(style.Inset * unit)
	b := BorderPx(border, unit)
	var chromeH float32
	switch style.Chrome {
	case ChromeMacLight, ChromeMacDark:
		chromeH = round32(30 * unit)
	case ChromeWinLight, ChromeWinDark:
		chromeH = round32(32 * unit)
	}
	cardW := iw + 2*inset + 2*b
	cardH := ih + 2*inset + chromeH + 2*b
	cw := cardW + 2*pad
	ch := cardH + 2*pad
	if a := style.Aspect; a != nil {
		target := a.W / a.H
		if cw/ch < target {
			cw = round32(ch * target)
		} else {
			ch = round32(cw / target)
		}
	}
	col := float32(style.Align % 3)
	row := float32(style.Align / 3)
	cardX := round32(pad+(cw-cardW-2*pad)*col/2) + b
	cardY := round32(pad+(ch-cardH-2*pad)*row/2) + b
	return Layout{
		Width:   int(max(cw, 1)),
		Height:  int(max(ch, 1)),
		Scale:   iw / max(crop.W, 1),
		Unit:    unit,
		Card:    R{cardX, cardY, cardW - 2*b, cardH - 2*b},
		Image:   R{cardX + inset, cardY + inset + chromeH, iw, ih},
		ChromeH: chromeH,
		Crop:    crop,
	}
}

// BorderPx is a border thickness in canvas pixels.
func BorderPx(border, unit float32) float32 {
	if border <= 0 {
		return 0
	}
	return max(round32(border*unit), 1)
}

func gradientShader(stops []Color, angle, w, h float32, ts Transform) Paint {
	sin, cos := sinCos32(angle)
	dx, dy := cos, sin
	half := abs32(w*dx)/2 + abs32(h*dy)/2
	cx, cy := w/2, h/2
	return Paint{Shader: &LinearGradient{
		Start:     Pt{cx - dx*half, cy - dy*half},
		End:       Pt{cx + dx*half, cy + dy*half},
		Stops:     stops,
		Transform: ts,
	}}
}

// drawCover draws img scaled to cover a w×h canvas placed in pm by ts.
func drawCover(pm *image.RGBA, img *image.RGBA, w, h float32, ts Transform, q FilterQuality) {
	iw, ih := float32(img.Rect.Dx()), float32(img.Rect.Dy())
	s := max(w/iw, h/ih)
	tx := (w - iw*s) / 2
	ty := (h - ih*s) / 2
	t := Scale(s, s).PostTranslate(tx, ty).PostConcat(ts)
	fillAll(pm, Paint{Shader: &Pattern{Image: img, Spread: SpreadPad, Quality: q, Transform: t}}, nil)
}

// DrawBackground fills pm with the background of a w×h canvas whose origin sits at origin in
// pm. blurBg is MakeBlurBg of the screenshot, bgImage the decoded ImageBackground; either may
// be nil when unused.
func DrawBackground(pm *image.RGBA, bg Background, w, h float32, origin Pt, blurBg, bgImage *image.RGBA) {
	ts := Translate(origin.X, origin.Y)
	switch bg := bg.(type) {
	case SolidBackground:
		Fill(pm, bg.Color)
	case GradientBackground:
		fillAll(pm, gradientShader(bg.Gradient.Stops, bg.Gradient.Angle, w, h, ts), nil)
	case BlurredBackground:
		if blurBg != nil {
			drawCover(pm, blurBg, w, h, ts, FilterBilinear)
			fillAll(pm, Solid(Color(0x0000001F)), nil)
		}
	case ImageBackground:
		if bgImage != nil {
			drawCover(pm, bgImage, w, h, ts, FilterBicubic)
		} else {
			Fill(pm, RGB(0x27272A))
		}
	}
}

// DrawShadow draws a blurred drop shadow of shape, offset slightly downwards.
func DrawShadow(pm *image.RGBA, shape *Path, blur, opacity float32) {
	if shape.Empty() {
		return
	}
	blurR := max(blur, 1)
	offY := blur * 0.45
	margin := ceil32(blurR * 3)
	b := shape.Bounds()
	bounds := R{b.X, b.Y + offY, b.W, b.H}.Inflate(margin)
	screen := R{0, 0, float32(pm.Rect.Dx()), float32(pm.Rect.Dy())}.Inflate(margin)
	region, ok := bounds.Intersect(screen)
	if !ok {
		return
	}
	region = region.RoundOut()
	rw, rh := int(region.W), int(region.H)
	if rw <= 0 || rh <= 0 {
		return
	}
	mask := NewMask(rw, rh)
	FillMask(mask, shape.Transformed(Translate(-region.X, offY-region.Y)))
	BlurAlpha(mask.Pix, rw, rh, int(round32(blurR/1.7)))
	// blendPx of black depends only on the mask value and the destination byte, so its float
	// math is tabulated once per mask value.
	var rgb, alpha [256][256]uint8
	for m := 1; m < 256; m++ {
		a := clamp32(float32(m)/255*opacity, 0, 1)
		if a <= 0 {
			continue
		}
		inv := 1 - a
		for d := range 256 {
			rgb[m][d] = uint8(min(round32(float32(float32(0)*a)+float32(float32(d)*inv)), 255))
			alpha[m][d] = uint8(min(round32(float32(255*a)+float32(float32(d)*inv)), 255))
		}
	}
	pw, ph := pm.Rect.Dx(), pm.Rect.Dy()
	rx, ry := int(region.X), int(region.Y)
	for y := 0; y < rh; y++ {
		py := ry + y
		if py < 0 || py >= ph {
			continue
		}
		for x := 0; x < rw; x++ {
			px := rx + x
			if px < 0 || px >= pw {
				continue
			}
			m := mask.Pix[y*rw+x]
			if m == 0 || float32(m)/255*opacity <= 0 {
				continue
			}
			d := pm.Pix[py*pm.Stride+px*4 : py*pm.Stride+px*4+4 : py*pm.Stride+px*4+4]
			na := alpha[m][d[3]]
			d[0], d[1], d[2], d[3] = min(rgb[m][d[0]], na), min(rgb[m][d[1]], na), min(rgb[m][d[2]], na), na
		}
	}
}

// Inputs are what RenderBase composes.
type Inputs struct {
	Style  Style
	Layout Layout
	// The cropped screenshot, already resized to Layout.Image's size.
	Image *image.RGBA
	// MakeBlurBg of the screenshot, for BlurredBackground.
	BlurBg *image.RGBA
	// The decoded ImageBackground.
	BgImage *image.RGBA
	Title   *string
}

// RenderBase renders background, shadow, window chrome, screenshot and redactions.
func RenderBase(in Inputs, annotations []Annotation) *image.RGBA {
	l := in.Layout
	pm := image.NewRGBA(image.Rect(0, 0, l.Width, l.Height))
	DrawBackground(pm, in.Style.Background, float32(l.Width), float32(l.Height), Pt{}, in.BlurBg, in.BgImage)
	if in.Style.Shadow > 0 && in.Style.ShadowOpacity > 0 {
		if p := RoundedRect(l.Card, in.Style.Radius*l.Unit); p != nil {
			DrawShadow(pm, p, in.Style.Shadow*l.Unit*0.5, in.Style.ShadowOpacity)
		}
	}
	mask := drawCard(pm, in, 0, Transparent)
	applyRedactions(pm, l, in.Image, redactions(annotations), mask)
	return pm
}

func redactions(anns []Annotation) []Redact {
	var out []Redact
	for _, a := range anns {
		if r, ok := a.Shape.(Redact); ok {
			out = append(out, r)
		}
	}
	return out
}

// drawCard draws the border ring, card fill, chrome and screenshot. border is in canvas
// pixels. It returns the mask of the visible screenshot (the image clipped to the card).
func drawCard(pm *image.RGBA, in Inputs, border float32, borderColor Color) *image.Alpha {
	l := in.Layout
	radius := in.Style.Radius * l.Unit
	if border > 0 {
		if p := RoundedRect(l.Card.Inflate(border), radius); p != nil {
			FillPath(pm, p, Solid(borderColor), nil)
		}
	}
	cardPath := RoundedRect(l.Card, max(radius-border, 0))
	if cardPath == nil {
		return nil
	}
	cardMask := NewMask(pm.Rect.Dx(), pm.Rect.Dy())
	FillMask(cardMask, cardPath)
	if in.Style.Inset > 0 || l.ChromeH > 0 {
		var fill Color
		switch {
		case in.Style.AutoBalance:
			fill = EdgeColor(in.Image)
		case in.Style.Chrome.IsDark():
			fill = RGB(0x1E1E1E)
		default:
			fill = White
		}
		FillPath(pm, cardPath, Solid(fill), nil)
	}
	if l.ChromeH > 0 {
		var title *string
		if in.Style.ChromeTitle {
			title = in.Title
		}
		drawChrome(pm, l, in.Style.Chrome, title, cardMask)
	}
	imageMask := cardMask
	if r, ok := validRect(l.Image); ok {
		IntersectMask(imageMask, RectPath(r))
	}
	DrawPixmap(pm, int(l.Image.X), int(l.Image.Y), in.Image, imageMask)
	return imageMask
}

func drawChrome(pm *image.RGBA, l Layout, chrome Chrome, title *string, mask *image.Alpha) {
	u := l.Unit
	bar := R{l.Card.X, l.Card.Y, l.Card.W, l.ChromeH}
	bg, fg, line := RGB(0xF2F2F4), RGB(0x5F5F66), RGB(0xDADADF)
	if chrome.IsDark() {
		bg, fg, line = RGB(0x2B2B2E), RGB(0xA1A1A6), RGB(0x161618)
	}
	if r, ok := validRect(bar); ok {
		FillRect(pm, r, Solid(bg), mask)
	}
	FillRect(pm, R{bar.X, bar.Bottom() - max(u, 1), bar.W, max(u, 1)}, Solid(line), mask)
	cy := bar.Y + bar.H/2
	switch chrome {
	case ChromeMacLight, ChromeMacDark:
		for i, c := range []uint32{0xFF5F57, 0xFEBC2E, 0x28C840} {
			if p := CirclePath(bar.X+18*u+float32(i)*20*u, cy, 6*u); p != nil {
				FillPath(pm, p, Solid(RGB(c)), nil)
			}
		}
	default:
		stroke := Stroke{Width: max(u, 1)}
		bw := 46 * u
		s := 5 * u
		x0 := bar.Right() - bw/2
		p := &Path{}
		p.MoveTo(x0-s, cy-s)
		p.LineTo(x0+s, cy+s)
		p.MoveTo(x0+s, cy-s)
		p.LineTo(x0-s, cy+s)
		x1 := x0 - bw
		appendPath(p, RectPath(R{x1 - s, cy - s, s * 2, s * 2}))
		x2 := x1 - bw
		p.MoveTo(x2-s, cy)
		p.LineTo(x2+s, cy)
		StrokePath(pm, p, Solid(fg), stroke, nil)
	}
	if title != nil {
		size := 12.5 * u
		font := FontFor("Segoe UI", WeightRegular)
		maxW := bar.W - 200*u
		t := []rune(*title)
		for len(t) > 0 {
			if w, _ := font.Measure(string(t), size); w <= maxW {
				break
			}
			t = t[:len(t)-1]
		}
		tw, th := font.Measure(string(t), size)
		font.Draw(pm, string(t), bar.X+(bar.W-tw)/2, cy-th/2, size, AlignLeft, fg)
	}
}

// applyRedactions redacts pm. Pixels come from image (the screenshot drawn at l.Image), so the
// result does not depend on which part of the canvas pm covers.
func applyRedactions(pm *image.RGBA, l Layout, img *image.RGBA, reds []Redact, mask *image.Alpha) {
	screen := R{0, 0, float32(pm.Rect.Dx()), float32(pm.Rect.Dy())}
	for _, rd := range reds {
		full := l.RectToCanvas(rd.Rect)
		area, ok := full.Intersect(l.Image)
		if !ok {
			continue
		}
		area = area.RoundOut()
		if _, ok := area.Intersect(screen); !ok {
			continue
		}
		block := max(rd.Strength*l.Scale, 2)
		local := area.Translate(-l.Image.X, -l.Image.Y)
		anchor := full.Translate(-l.Image.X, -l.Image.Y)
		patch := redactPatch(img, local, anchor, rd.Kind, block)
		if patch == nil {
			continue
		}
		DrawPixmap(pm, int(area.X), int(area.Y), patch, mask)
	}
}

// redactPatch is the redacted pixels of area (image coordinates); cells align to anchor.
func redactPatch(img *image.RGBA, area, anchor R, kind RedactStyle, strength float32) *image.RGBA {
	iw, ih := img.Rect.Dx(), img.Rect.Dy()
	x0 := min(max(int(area.X), 0), iw)
	y0 := min(max(int(area.Y), 0), ih)
	x1 := min(max(int(area.Right()), 0), iw)
	y1 := min(max(int(area.Bottom()), 0), ih)
	if x1 <= x0+1 || y1 <= y0+1 {
		return nil
	}
	rw, rh := x1-x0, y1-y0
	out := subImage(img, image.Rect(x0, y0, x1, y1))
	offset := func(start int, anchor float32, block int) int {
		return int(max(float32(start)-round32(anchor), 0)) % block
	}
	switch kind {
	case RedactSolid:
		for i := 0; i < len(out.Pix); i += 4 {
			out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = 24, 24, 27, 255
		}
	case RedactPixelate:
		block := int(max(round32(strength), 2))
		pixelate(out.Pix, rw, rh, block, offset(x0, anchor.X, block), offset(y0, anchor.Y, block))
	case RedactBlur:
		// Pixelate first so that the blur cannot be reversed, then smooth it out.
		block := int(max(round32(strength*0.5), 2))
		pixelate(out.Pix, rw, rh, block, offset(x0, anchor.X, block), offset(y0, anchor.Y, block))
		BlurRGBA(out.Pix, rw, rh, int(max(round32(strength*0.7), 2)))
	}
	return out
}

// cellStarts are the starts of block-sized cells along n pixels when the region begins off
// pixels into a cell.
func cellStarts(n, block, off int) []int {
	v := []int{0}
	s := (block - off%block) % block
	if s == 0 {
		s = block
	}
	for ; s < n; s += block {
		v = append(v, s)
	}
	return v
}

func pixelate(data []uint8, w, h, block, ox, oy int) {
	ys, xs := cellStarts(h, block, oy), cellStarts(w, block, ox)
	for yi, by := range ys {
		ey := h
		if yi+1 < len(ys) {
			ey = ys[yi+1]
		}
		for xi, bx := range xs {
			ex := w
			if xi+1 < len(xs) {
				ex = xs[xi+1]
			}
			var sum [4]uint32
			for y := by; y < ey; y++ {
				for x := bx; x < ex; x++ {
					i := (y*w + x) * 4
					for c := range 4 {
						sum[c] += uint32(data[i+c])
					}
				}
			}
			n := uint32((ex - bx) * (ey - by))
			var avg [4]uint8
			for c := range 4 {
				avg[c] = uint8(sum[c] / n)
			}
			for y := by; y < ey; y++ {
				for x := bx; x < ex; x++ {
					copy(data[(y*w+x)*4:], avg[:])
				}
			}
		}
	}
}

// RenderAnnotations draws every non-redaction annotation over a base render. Highlights dim
// the screenshot outside the union of their rectangles.
func RenderAnnotations(pm *image.RGBA, l Layout, annotations []Annotation) {
	drawHighlights(pm, l, annotations, nil)
	for _, a := range annotations {
		if !a.IsRedaction() {
			DrawAnnotation(pm, l, a)
		}
	}
}

func drawHighlights(pm *image.RGBA, l Layout, annotations []Annotation, imageMask *image.Alpha) {
	var holes []R
	for _, a := range annotations {
		if h, ok := a.Shape.(Highlight); ok {
			holes = append(holes, l.RectToCanvas(h.Rect))
		}
	}
	if len(holes) == 0 {
		return
	}
	w, h := pm.Rect.Dx(), pm.Rect.Dy()
	var dim *image.Alpha
	if imageMask != nil {
		dim = NewMask(w, h)
		copy(dim.Pix, imageMask.Pix)
	} else {
		dim = NewMask(w, h)
		if r, ok := validRect(l.Image); ok {
			FillMask(dim, RectPath(r))
		}
	}
	p := &Path{}
	for _, hole := range holes {
		if r, ok := validRect(hole); ok {
			appendPath(p, RectPath(r))
		}
	}
	if p.Empty() {
		return
	}
	cut := NewMask(w, h)
	FillMask(cut, p)
	for i, c := range cut.Pix {
		dim.Pix[i] = uint8(uint16(dim.Pix[i]) * (255 - uint16(c)) / 255)
	}
	fillAll(pm, Solid(Color(0x00000099)), dim)
}

func contrast(c Color) Color {
	if c.Luma() > 0.65 {
		return RGB(0x111111)
	}
	return White
}

// DrawAnnotation draws one annotation; redactions and highlights are drawn elsewhere.
func DrawAnnotation(pm *image.RGBA, l Layout, a Annotation) {
	s := l.Scale
	w := max(a.Width*s, 1)
	paint := Solid(a.Color)
	stroke := Stroke{Width: w, Cap: CapRound, Join: JoinRound}
	switch sh := a.Shape.(type) {
	case Arrow:
		p0, p1 := l.ToCanvas(sh.From), l.ToCanvas(sh.To)
		if path := arrowPath(p0, p1, w); path != nil {
			// A soft contrast outline keeps arrows readable on any background.
			outline := Stroke{Width: max(w*0.35, 1), Join: JoinRound}
			StrokePath(pm, path, Solid(contrast(a.Color).WithAlpha(110)), outline, nil)
			FillPath(pm, path, paint, nil)
		}
	case Line:
		p0, p1 := l.ToCanvas(sh.From), l.ToCanvas(sh.To)
		p := &Path{}
		p.MoveTo(p0.X, p0.Y)
		p.LineTo(p1.X, p1.Y)
		StrokePath(pm, p, paint, stroke, nil)
	case RectShape:
		if p := RoundedRect(l.RectToCanvas(sh.Rect), w*0.9); p != nil {
			if sh.Filled {
				FillPath(pm, p, paint, nil)
			} else {
				StrokePath(pm, p, paint, stroke, nil)
			}
		}
	case Ellipse:
		if r, ok := validRect(l.RectToCanvas(sh.Rect)); ok {
			p := OvalPath(r)
			if sh.Filled {
				FillPath(pm, p, paint, nil)
			} else {
				StrokePath(pm, p, paint, stroke, nil)
			}
		}
	case Pen:
		if len(sh.Points) < 2 {
			return
		}
		pts := make([]Pt, len(sh.Points))
		for i, q := range sh.Points {
			pts[i] = l.ToCanvas(q)
		}
		p := &Path{}
		p.MoveTo(pts[0].X, pts[0].Y)
		for i := 1; i+1 < len(pts); i++ {
			mid := Pt{(pts[i].X + pts[i+1].X) / 2, (pts[i].Y + pts[i+1].Y) / 2}
			p.QuadTo(pts[i].X, pts[i].Y, mid.X, mid.Y)
		}
		last := pts[len(pts)-1]
		p.LineTo(last.X, last.Y)
		StrokePath(pm, p, paint, stroke, nil)
	case Text:
		if sh.Text == "" {
			return
		}
		p := l.ToCanvas(sh.Pos)
		size := sh.Size * s
		face := FontFor(sh.Font.Family, sh.Font.Weight)
		align := sh.Font.Align
		switch sh.Style {
		case TextPlain:
			face.Draw(pm, sh.Text, p.X, p.Y, size, align, a.Color)
		case TextOutline:
			o := max(size*0.07, 1)
			oc := contrast(a.Color)
			for _, d := range [8][2]float32{{-o, 0}, {o, 0}, {0, -o}, {0, o}, {-o, -o}, {o, o}, {-o, o}, {o, -o}} {
				face.Draw(pm, sh.Text, p.X+d[0], p.Y+d[1], size, align, oc)
			}
			face.Draw(pm, sh.Text, p.X, p.Y, size, align, a.Color)
		case TextPill:
			tw, th := face.Measure(sh.Text, size)
			pad := size * 0.35
			bg := R{p.X - pad, p.Y - pad*0.6, tw + pad*2, th + pad*1.2}
			if path := RoundedRect(bg, size*0.4); path != nil {
				FillPath(pm, path, paint, nil)
			}
			face.Draw(pm, sh.Text, p.X, p.Y, size, align, contrast(a.Color))
		}
	case Counter:
		c := l.ToCanvas(sh.Center)
		r := sh.Size * s
		if p := CirclePath(c.X, c.Y, r); p != nil {
			FillPath(pm, p, paint, nil)
			StrokePath(pm, p, Solid(White.WithAlpha(230)), Stroke{Width: max(r*0.12, 1)}, nil)
		}
		label := strconv.FormatUint(uint64(sh.N), 10)
		fs := r * 1.05
		face := FontFor("Segoe UI", WeightSemibold)
		tw, th := face.Measure(label, fs)
		face.Draw(pm, label, c.X-tw/2, c.Y-th/2, fs, AlignLeft, contrast(a.Color))
	}
}

// arrowPath is a tapered arrow with a sharp head.
func arrowPath(a, b Pt, w float32) *Path {
	length := a.Dist(b)
	if length < 1 {
		return nil
	}
	dx, dy := (b.X-a.X)/length, (b.Y-a.Y)/length
	nx, ny := -dy, dx
	headLen := min(w*4.2, length*0.6)
	headW := w * 3.4
	hb := Pt{b.X - dx*headLen, b.Y - dy*headLen}
	tail := w * 0.3
	neck := w * 1.0
	p := &Path{}
	p.MoveTo(a.X+nx*tail/2, a.Y+ny*tail/2)
	p.LineTo(hb.X+nx*neck/2, hb.Y+ny*neck/2)
	p.LineTo(hb.X+nx*headW/2-dx*w*0.6, hb.Y+ny*headW/2-dy*w*0.6)
	p.LineTo(b.X, b.Y)
	p.LineTo(hb.X-nx*headW/2-dx*w*0.6, hb.Y-ny*headW/2-dy*w*0.6)
	p.LineTo(hb.X-nx*neck/2, hb.Y-ny*neck/2)
	p.LineTo(a.X-nx*tail/2, a.Y-ny*tail/2)
	p.Close()
	return p
}
