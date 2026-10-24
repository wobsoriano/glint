package editor

import (
	"encoding/json"
	"fmt"
	"image"
	"math"
	"slices"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
)

// geom maps between the canvas box (DIPs, relative to the box), the canvas at the preview scale
// and the image, for the frame being drawn.
type geom struct {
	area  ui.Rect
	sf    float32
	frame render.Frame // the view document at scale
	scale float32      // render scale of the preview: min(zoom, 1)
	view  view         // device pixels per canvas pixel (at scale 1)
	full  size         // the canvas at scale 1
}

func (g geom) vsize() size { return size{g.area.W * g.sf, g.area.H * g.sf} }

func (g geom) toImage(x, y float32) (render.Pt, bool) {
	c := g.view.toCanvas(render.Pt{X: x * g.sf, Y: y * g.sf})
	return g.frame.ToImage(render.Pt{X: c.X * g.scale, Y: c.Y * g.scale})
}

// fromCanvas maps a canvas point at the preview scale to the box.
func (g geom) fromCanvas(c render.Pt) render.Pt {
	s := g.view.toScreen(render.Pt{X: c.X / g.scale, Y: c.Y / g.scale})
	return render.Pt{X: s.X / g.sf, Y: s.Y / g.sf}
}

func (g geom) toLocal(p render.Pt) render.Pt { return g.fromCanvas(g.frame.ToCanvas(p)) }

// imagePerDIP is how many image pixels one DIP of the box covers.
func (g geom) imagePerDIP() float32 {
	return g.sf * g.scale / (g.frame.Layout.Scale * g.view.zoom)
}

// localBox is the box-space bounds of an image-space rect, through the projection.
func (g geom) localBox(r render.R) ui.Rect {
	pts := [4]render.Pt{{X: r.X, Y: r.Y}, {X: r.Right(), Y: r.Y}, {X: r.Right(), Y: r.Bottom()}, {X: r.X, Y: r.Bottom()}}
	x0, y0 := float32(math.MaxFloat32), float32(math.MaxFloat32)
	x1, y1 := -x0, -y0
	for _, p := range pts {
		q := g.toLocal(p)
		x0, y0, x1, y1 = min(x0, q.X), min(y0, q.Y), max(x1, q.X), max(y1, q.Y)
	}
	return ui.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}

// previewJob is one render of the view document: region in canvas pixels at scale, without
// the annotations in excluded, which sprites draw while the pointer moves them.
type previewJob struct {
	key      string
	doc      render.EditDoc
	scale    float32
	region   render.R
	excluded []uint64
}

type previewImage struct {
	job previewJob
	img *image.RGBA
}

// renderer renders preview jobs on a goroutine of its own; a newer job replaces one waiting.
// Without post it renders on the caller's goroutine, as a test does.
type renderer struct {
	mu      sync.Mutex
	pending *previewJob
	wake    chan struct{}
	post    func(func())
	deliver func(previewImage)

	base   *image.RGBA
	comp   render.Compositor
	blurBg *image.RGBA
	bgPath string
	bgImg  *image.RGBA
}

func newRenderer(base *image.RGBA, post func(func()), deliver func(previewImage)) *renderer {
	r := &renderer{base: base, post: post, deliver: deliver, wake: make(chan struct{}, 1)}
	if post != nil {
		go r.loop()
	}
	return r
}

func (r *renderer) request(j previewJob) {
	if r.post == nil {
		r.deliver(previewImage{j, r.render(j)})
		return
	}
	r.mu.Lock()
	r.pending = &j
	r.mu.Unlock()
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

func (r *renderer) stop() {
	if r.post != nil {
		close(r.wake)
	}
}

func (r *renderer) loop() {
	for range r.wake {
		r.mu.Lock()
		j := r.pending
		r.pending = nil
		r.mu.Unlock()
		if j == nil {
			continue
		}
		out := previewImage{*j, r.render(*j)}
		r.post(func() { r.deliver(out) })
	}
}

func (r *renderer) render(j previewJob) *image.RGBA {
	src := render.Sources{Base: r.base}
	switch bg := j.doc.Style.Background.(type) {
	case render.BlurredBackground:
		if r.blurBg == nil {
			r.blurBg = render.MakeBlurBg(r.base)
		}
		src.BlurBg = r.blurBg
	case render.ImageBackground:
		if bg.Path != r.bgPath {
			r.bgPath = bg.Path
			r.bgImg, _ = render.LoadBackground(bg.Path)
		}
		src.BgImage = r.bgImg
	}
	region := j.region
	return r.comp.Render(j.doc, src, j.scale, &region)
}

// sprite is one annotation rendered alone by internal/render, at the preview scale, for
// drawing while the pointer moves it; a move only places it elsewhere.
type sprite struct {
	key    string
	bmp    *ui.Bitmap
	origin render.Pt // the image point at the sprite's top-left pixel
	w, h   int
}

const spriteMargin = 2

func spriteKey(a render.Annotation, scale float32) (string, render.R) {
	pad := max(a.Width, 4)*1.5 + 4
	if c, ok := a.Shape.(render.Counter); ok {
		pad += c.Size * 0.2
	}
	b := a.Bounds().Inflate(pad)
	norm := a.Clone()
	norm.Translate(-b.X, -b.Y)
	j, _ := json.Marshal(norm)
	return fmt.Sprintf("%s|%v|%v|%v", j, b.W, b.H, scale), b
}

func newSprite(a render.Annotation, key string, b render.R, scale float32) *sprite {
	w := int(math.Ceil(float64(b.W*scale))) + 2*spriteMargin
	h := int(math.Ceil(float64(b.H*scale))) + 2*spriteMargin
	img := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	norm := a.Clone()
	norm.Translate(-b.X, -b.Y)
	l := render.Layout{Width: w, Height: h, Scale: scale, Image: render.R{X: spriteMargin, Y: spriteMargin, W: b.W * scale, H: b.H * scale}}
	render.DrawAnnotation(img, l, norm)
	return &sprite{key: key, bmp: ui.NewBitmap(img), origin: render.Pt{X: b.X - spriteMargin/scale, Y: b.Y - spriteMargin/scale}, w: w, h: h}
}

// canvas is what the canvas box keeps between frames.
type canvas struct {
	geom     geom
	ready    bool
	renderer *renderer
	shown    *previewImage
	bmp      *ui.Bitmap
	pixel    bool
	asked    string
	sprites  map[uint64]*sprite
	// onDraw, when set, is told each time the canvas paints, for frame timing.
	onDraw func()
}

func (cv *canvas) deliver(p previewImage) {
	if cv.bmp != nil && cv.shown != nil && cv.shown.img.Rect.Size() == p.img.Rect.Size() && cv.pixel == (p.job.scale >= 1) {
		cv.bmp.Update(p.img)
	} else {
		cv.bmp = ui.NewBitmap(p.img)
		cv.pixel = p.job.scale >= 1
		if cv.pixel {
			cv.bmp.Pixelated()
		}
	}
	cv.shown = &p
}

// liveIDs are the annotations the pointer is moving that sprites can draw: not redactions or
// highlights, whose look depends on the pixels around them, and not under a projection or a
// progressive blur, which the compositor applies to everything on the card.
func (e *Editor) liveIDs(fr render.Frame) []uint64 {
	m := e.m
	if m.crop != nil || fr.HasProjection || m.doc.Effects.Blur.IsActive() {
		return nil
	}
	var ids []uint64
	switch d := m.drag.(type) {
	case dragCreate:
		ids = []uint64{d.id}
	case dragMove:
		ids = m.sel
	case dragHandle:
		ids = []uint64{d.id}
	}
	return slices.DeleteFunc(slices.Clone(ids), func(id uint64) bool {
		a := m.ann(id)
		if a == nil || a.IsRedaction() {
			return true
		}
		_, hl := a.Shape.(render.Highlight)
		return hl
	})
}

// layout resolves this frame's geometry and asks for a preview render when the picture changed.
func (e *Editor) layoutCanvas(area ui.Rect, sf float32) {
	cv := &e.cv
	m := e.m
	doc := m.viewDoc()
	bw, bh := m.baseSize()
	l1 := render.NewFrame(doc, bw, bh, 1).Layout
	full := size{float32(l1.Width), float32(l1.Height)}
	g := geom{area: area, sf: sf, full: full}
	g.view = e.vp.resolve(full, g.vsize(), sf)
	g.scale = min(g.view.zoom, 1)
	g.frame = render.NewFrame(doc, bw, bh, g.scale)
	cv.geom, cv.ready = g, true

	vs := g.vsize()
	visible, ok := render.R{X: g.view.origin.X, Y: g.view.origin.Y, W: vs.w / g.view.zoom, H: vs.h / g.view.zoom}.Intersect(render.R{W: full.w, H: full.h})
	if !ok {
		visible = render.R{W: 1, H: 1}
	}
	lw, lh := float32(g.frame.Layout.Width), float32(g.frame.Layout.Height)
	region, ok := render.R{X: visible.X * g.scale, Y: visible.Y * g.scale, W: visible.W * g.scale, H: visible.H * g.scale}.RoundOut().Intersect(render.R{W: lw, H: lh})
	if !ok {
		region = render.R{W: 1, H: 1}
	}
	live := e.liveIDs(g.frame)
	doc.Annotations = slices.DeleteFunc(doc.Annotations, func(a render.Annotation) bool { return slices.Contains(live, a.ID) })
	key := fmt.Sprintf("%s|%v|%v|%v", doc.JSON(), region, g.scale, live)
	if key != cv.asked {
		cv.asked = key
		cv.renderer.request(previewJob{key: key, doc: doc, scale: g.scale, region: region, excluded: live})
	}
}

// spriteIDs are the annotations drawn as sprites: those moving, and those the preview on
// screen left out and still exist.
func (e *Editor) spriteIDs() []uint64 {
	ids := e.liveIDs(e.cv.geom.frame)
	if s := e.cv.shown; s != nil {
		for _, id := range s.job.excluded {
			if !slices.Contains(ids, id) && e.m.ann(id) != nil {
				ids = append(ids, id)
			}
		}
	}
	return ids
}

func (e *Editor) paintCanvas(p *ui.Painter, r ui.Rect, pal palette) {
	cv := &e.cv
	e.layoutCanvas(r, p.Scale())
	if cv.onDraw != nil {
		cv.onDraw()
	}
	g := cv.geom
	if s := cv.shown; s != nil && cv.bmp != nil {
		at := g.fromCanvas(render.Pt{X: s.job.region.X, Y: s.job.region.Y})
		end := g.fromCanvas(render.Pt{X: s.job.region.Right(), Y: s.job.region.Bottom()})
		box := ui.Rect{X: r.X + at.X, Y: r.Y + at.Y, W: end.X - at.X, H: end.Y - at.Y}
		if _, ok := s.job.doc.Style.Background.(render.NoBackground); ok {
			p.Fill(box, ui.RGB(0x1A, 0x1A, 0x1D), 0)
		}
		p.Image(cv.bmp, box, ui.FillBox)
	}
	e.paintSprites(p, r)
	e.paintOverlays(p, r, pal)
}

func (e *Editor) paintSprites(p *ui.Painter, r ui.Rect) {
	cv := &e.cv
	ids := e.spriteIDs()
	if len(ids) == 0 {
		cv.sprites = nil
		return
	}
	if cv.sprites == nil {
		cv.sprites = map[uint64]*sprite{}
	}
	g := cv.geom
	scale := g.frame.Layout.Scale
	img := g.frame.Layout.Image
	tl := g.fromCanvas(render.Pt{X: img.X, Y: img.Y})
	br := g.fromCanvas(render.Pt{X: img.Right(), Y: img.Bottom()})
	clip := ui.Rect{X: r.X + tl.X, Y: r.Y + tl.Y, W: br.X - tl.X, H: br.Y - tl.Y}
	radius := float32(0)
	if st := e.m.doc.Style; st.Inset == 0 && g.frame.Layout.ChromeH == 0 {
		radius = st.Radius * g.frame.Layout.Unit * g.view.zoom / g.scale / g.sf
	}
	k := g.view.zoom / g.scale / g.sf // DIPs per sprite pixel
	p.Clip(clip, radius, func() {
		for _, id := range ids {
			a := e.m.ann(id)
			key, b := spriteKey(*a, scale)
			s := cv.sprites[id]
			if s == nil || s.key != key {
				s = newSprite(*a, key, b, scale)
				cv.sprites[id] = s
			}
			at := g.toLocal(render.Pt{X: b.X - spriteMargin/scale, Y: b.Y - spriteMargin/scale})
			p.Image(s.bmp, ui.Rect{X: r.X + at.X, Y: r.Y + at.Y, W: float32(s.w) * k, H: float32(s.h) * k}, ui.FillBox)
		}
	})
	for id := range cv.sprites {
		if !slices.Contains(ids, id) {
			delete(cv.sprites, id)
		}
	}
}

func handleDot(p *ui.Painter, r ui.Rect, at render.Pt, accent ui.Color) {
	box := ui.Rect{X: r.X + at.X - 5, Y: r.Y + at.Y - 5, W: 10, H: 10}
	p.Fill(box, ui.RGB(255, 255, 255), 5)
	p.Stroke(box, accent, 5, 2)
}

func (e *Editor) paintOverlays(p *ui.Painter, r ui.Rect, pal palette) {
	g := e.cv.geom
	m := e.m
	off := func(b ui.Rect) ui.Rect { return ui.Rect{X: r.X + b.X, Y: r.Y + b.Y, W: b.W, H: b.H} }
	if c := m.crop; c != nil {
		img := off(g.localBox(m.fullRect()))
		cr := off(g.localBox(c.rect))
		shade := ui.RGBA(0, 0, 0, float32(0xA6)/255)
		for _, s := range []ui.Rect{
			{X: img.X, Y: img.Y, W: img.W, H: cr.Y - img.Y},
			{X: img.X, Y: cr.Y + cr.H, W: img.W, H: img.Y + img.H - cr.Y - cr.H},
			{X: img.X, Y: cr.Y, W: cr.X - img.X, H: cr.H},
			{X: cr.X + cr.W, Y: cr.Y, W: img.X + img.W - cr.X - cr.W, H: cr.H},
		} {
			if s.W > 0 && s.H > 0 {
				p.Fill(s, shade, 0)
			}
		}
		p.Stroke(cr, ui.RGB(255, 255, 255), 0, 2)
		for _, h := range cropHandles(c.rect, m.cropRatio() == 0) {
			handleDot(p, r, g.toLocal(h), pal.Accent)
		}
		label := ui.Span{Text: fmt.Sprintf("%d × %d", int(c.rect.W), int(c.rect.H)), Size: 12, Color: ui.RGB(255, 255, 255)}
		w, h := p.MeasureText(0, label)
		y := max(cr.Y-26, r.Y+4)
		p.Fill(ui.Rect{X: cr.X, Y: y, W: w + 16, H: h + 4}, ui.RGBA(0, 0, 0, 0.8), 6)
		p.RichText(cr.X+8, y+2, 0, label)
		return
	}
	for _, a := range m.selectedAnns() {
		b := off(g.localBox(a.Bounds()))
		p.Stroke(ui.Rect{X: b.X - 3, Y: b.Y - 3, W: b.W + 6, H: b.H + 6}, pal.Accent, 2, 1)
	}
	if a, ok := m.singleSelected(); ok && m.typing == 0 {
		for _, h := range a.Handles() {
			handleDot(p, r, g.toLocal(h), pal.Accent)
		}
	}
	if d, ok := m.drag.(dragMarquee); ok {
		b := off(g.localBox(render.RectFromPts(d.start, d.now)))
		p.Fill(b, pal.Accent.Alpha(float32(0x26)/255), 0)
		p.Stroke(b, pal.Accent, 0, 1)
	}
}
