package editor

import (
	"fmt"
	"image"
	"math"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/wobsoriano/glint/internal/render"
)

// ink is the style the next annotation is drawn with. Sizes are in points.
type ink struct {
	color          render.Color
	stroke         float32
	textSize       float32
	textStyle      render.TextStyle
	font           render.TextFont
	redactStrength float32
}

func defaultInk() ink {
	return ink{
		color: render.RGB(render.Ink[0]), stroke: 5, textSize: 28, textStyle: render.TextOutline,
		font: render.DefaultTextFont(), redactStrength: render.RedactStrength,
	}
}

// drag is the pointer gesture in progress, in image coordinates.
type drag interface{ isDrag() }

type (
	dragCreate struct {
		id    uint64
		start render.Pt
	}
	dragMove   struct{ last render.Pt }
	dragHandle struct {
		id  uint64
		idx int
	}
	dragMarquee struct {
		start, now render.Pt
		base       []uint64
	}
	dragCropNew    struct{ start render.Pt }
	dragCropMove   struct{ last render.Pt }
	dragCropHandle struct{ idx int }
)

func (dragCreate) isDrag()     {}
func (dragMove) isDrag()       {}
func (dragHandle) isDrag()     {}
func (dragMarquee) isDrag()    {}
func (dragCropNew) isDrag()    {}
func (dragCropMove) isDrag()   {}
func (dragCropHandle) isDrag() {}

// cropPreset is freeform (ratio 0), the original ratio (ratio -1), or a fixed ratio.
type cropPreset struct {
	name  string
	ratio float32
}

var cropPresets = [7]cropPreset{
	{"Freeform", 0}, {"Original", -1}, {"1:1", 1}, {"16:9", 16.0 / 9}, {"9:16", 9.0 / 16}, {"4:3", 4.0 / 3}, {"3:2", 3.0 / 2},
}

// cropEdit is the crop being edited, in base pixels; the document keeps its crop until applied.
type cropEdit struct {
	rect   render.R
	preset int
}

// model is the editor's document and everything the user is doing to it. Its methods are the
// editor's behavior, in image coordinates, with no window.
type model struct {
	base    *image.RGBA
	doc     render.EditDoc
	saved   render.EditDoc
	tool    render.Tool
	ink     ink
	sel     []uint64
	typing  uint64 // the text annotation being typed into, 0 for none
	hist    history
	drag    drag
	crop    *cropEdit
	nextID  uint64
	presets []render.Preset
	now     func() time.Time
}

func newModel(base *image.RGBA, doc render.EditDoc) *model {
	m := &model{base: base, doc: doc.Clone(), tool: render.ToolArrow, ink: defaultInk(), now: time.Now}
	seen := map[uint64]bool{}
	for _, a := range m.doc.Annotations {
		m.nextID = max(m.nextID, a.ID)
	}
	for i := range m.doc.Annotations {
		a := &m.doc.Annotations[i]
		if a.ID == 0 || seen[a.ID] {
			m.nextID++
			a.ID = m.nextID
		}
		seen[a.ID] = true
	}
	m.saved = m.doc.Clone()
	return m
}

func (m *model) baseSize() (uint32, uint32) {
	return uint32(m.base.Rect.Dx()), uint32(m.base.Rect.Dy())
}

func (m *model) fullRect() render.R {
	w, h := m.baseSize()
	return render.R{W: float32(w), H: float32(h)}
}

func (m *model) dirty() bool { return !sameDoc(m.doc, m.saved) }

func (m *model) checkpoint() { m.hist.checkpoint(m.doc) }

func (m *model) dropNoopCheckpoint() { m.hist.dropNoop(m.doc) }

// edit changes the document as one undo step per control, coalescing quick repeats.
func (m *model) edit(control string, f func(d *render.EditDoc)) {
	m.hist.checkpointFor(control, m.doc, m.now())
	f(&m.doc)
}

func (m *model) undo() {
	m.commitText()
	if d, ok := m.hist.popUndo(m.doc); ok {
		m.doc = d
		m.pruneSelection()
	}
}

func (m *model) redo() {
	if d, ok := m.hist.popRedo(m.doc); ok {
		m.doc = d
		m.pruneSelection()
	}
}

func (m *model) pruneSelection() {
	m.sel = slices.DeleteFunc(m.sel, func(id uint64) bool { return m.find(id) < 0 })
}

func (m *model) find(id uint64) int {
	return slices.IndexFunc(m.doc.Annotations, func(a render.Annotation) bool { return a.ID == id })
}

func (m *model) ann(id uint64) *render.Annotation {
	if i := m.find(id); i >= 0 {
		return &m.doc.Annotations[i]
	}
	return nil
}

func (m *model) selected(id uint64) bool { return slices.Contains(m.sel, id) }

func (m *model) selectedAnns() []render.Annotation {
	var out []render.Annotation
	for _, a := range m.doc.Annotations {
		if m.selected(a.ID) {
			out = append(out, a)
		}
	}
	return out
}

func (m *model) singleSelected() (render.Annotation, bool) {
	if len(m.sel) != 1 {
		return render.Annotation{}, false
	}
	if a := m.ann(m.sel[0]); a != nil {
		return *a, true
	}
	return render.Annotation{}, false
}

// commitText ends typing; a text annotation left empty goes away without an undo step.
func (m *model) commitText() {
	id := m.typing
	if id == 0 {
		return
	}
	m.typing = 0
	m.doc.Annotations = slices.DeleteFunc(m.doc.Annotations, func(a render.Annotation) bool {
		t, ok := a.Shape.(render.Text)
		return ok && a.ID == id && strings.TrimSpace(t.Text) == ""
	})
	m.pruneSelection()
	m.dropNoopCheckpoint()
}

func (m *model) modifySelected(control string, f func(a *render.Annotation)) {
	if len(m.sel) == 0 {
		return
	}
	m.hist.checkpointFor(control, m.doc, m.now())
	for i := range m.doc.Annotations {
		if m.selected(m.doc.Annotations[i].ID) {
			f(&m.doc.Annotations[i])
		}
	}
	m.dropNoopCheckpoint()
}

func (m *model) setTool(t render.Tool) {
	m.commitText()
	if t == render.ToolCrop {
		w, h := m.baseSize()
		m.crop = &cropEdit{rect: m.doc.CropRect(w, h)}
		m.sel = nil
	} else {
		m.crop = nil
	}
	m.tool = t
}

// activeTool is the tool the toolbar shows pressed.
func (m *model) activeTool() render.Tool {
	if m.crop != nil {
		return render.ToolCrop
	}
	return m.tool
}

func (m *model) applyCrop() {
	if c := m.crop; c != nil {
		m.crop = nil
		w, h := m.baseSize()
		m.checkpoint()
		m.doc.SetCrop(c.rect, w, h)
		m.dropNoopCheckpoint()
	}
	m.tool = render.ToolSelect
}

func (m *model) setCropPreset(i int) {
	if m.crop == nil {
		return
	}
	full := m.fullRect()
	m.crop.preset = i
	switch r := cropPresets[i].ratio; {
	case r == 0:
	case r < 0:
		m.crop.rect = full
	default:
		m.crop.rect = render.RatioCrop(full, r)
	}
}

// cropRatio is the width / height the crop keeps, or 0 for freeform.
func (m *model) cropRatio() float32 {
	if m.crop == nil {
		return 0
	}
	switch r := cropPresets[m.crop.preset].ratio; {
	case r < 0:
		w, h := m.baseSize()
		return float32(w) / float32(h)
	default:
		return r
	}
}

func cropHandles(r render.R, edges bool) []render.Pt {
	v := []render.Pt{{X: r.X, Y: r.Y}, {X: r.Right(), Y: r.Y}, {X: r.Right(), Y: r.Bottom()}, {X: r.X, Y: r.Bottom()}}
	if edges {
		c := r.Center()
		v = append(v, render.Pt{X: c.X, Y: r.Y}, render.Pt{X: r.Right(), Y: c.Y}, render.Pt{X: c.X, Y: r.Bottom()}, render.Pt{X: r.X, Y: c.Y})
	}
	return v
}

func signum(v float32) float32 {
	switch {
	case v > 0:
		return 1
	case v < 0:
		return -1
	}
	return 0
}

func ratioRect(anchor, p render.Pt, ratio float32, bounds render.R) render.R {
	sx, sy := signum(p.X-anchor.X), signum(p.Y-anchor.Y)
	maxW := anchor.X - bounds.X
	if sx >= 0 {
		maxW = bounds.Right() - anchor.X
	}
	maxH := anchor.Y - bounds.Y
	if sy >= 0 {
		maxH = bounds.Bottom() - anchor.Y
	}
	w := max(abs(p.X-anchor.X), abs(p.Y-anchor.Y)*ratio)
	w = min(w, maxW, maxH*ratio)
	h := w / ratio
	return render.RectFromPts(anchor, render.Pt{X: anchor.X + sx*w, Y: anchor.Y + sy*h})
}

func abs(v float32) float32 { return float32(math.Abs(float64(v))) }

func snapAngle(a, b render.Pt) render.Pt {
	dx, dy := b.X-a.X, b.Y-a.Y
	length := math.Hypot(float64(dx), float64(dy))
	step := math.Pi / 4
	snapped := math.Round(math.Atan2(float64(dy), float64(dx))/step) * step
	return render.Pt{X: a.X + float32(length*math.Cos(snapped)), Y: a.Y + float32(length*math.Sin(snapped))}
}

func square(a, b render.Pt) render.Pt {
	s := max(abs(b.X-a.X), abs(b.Y-a.Y))
	return render.Pt{X: a.X + s*signum(b.X-a.X), Y: a.Y + s*signum(b.Y-a.Y)}
}

func clampTo(p render.Pt, r render.R) render.Pt {
	return render.Pt{X: min(max(p.X, r.X), r.Right()), Y: min(max(p.Y, r.Y), r.Bottom())}
}

func nearest(pts []render.Pt, p render.Pt, tol float32) int {
	for i, h := range pts {
		if h.Dist(p) <= tol {
			return i
		}
	}
	return -1
}

func (m *model) newID() uint64 {
	m.nextID++
	return m.nextID
}

func (m *model) nextCounter() uint32 {
	var n uint32
	for _, a := range m.doc.Annotations {
		if c, ok := a.Shape.(render.Counter); ok {
			n = max(n, c.N)
		}
	}
	return n + 1
}

// pointerDown starts a gesture at p (image coordinates); tol is the hit slop in image pixels.
func (m *model) pointerDown(p render.Pt, tol float32, shift bool, clicks int) {
	if c := m.crop; c != nil {
		full := m.fullRect()
		switch idx := nearest(cropHandles(c.rect, m.cropRatio() == 0), p, tol*1.8); {
		case idx >= 0:
			m.drag = dragCropHandle{idx}
		case c.rect.Contains(p):
			m.drag = dragCropMove{p}
		default:
			m.drag = dragCropNew{clampTo(p, full)}
		}
		return
	}
	if a, ok := m.singleSelected(); ok {
		if idx := nearest(a.Handles(), p, tol*1.6); idx >= 0 {
			m.commitText()
			m.checkpoint()
			m.drag = dragHandle{a.ID, idx}
			return
		}
	}
	var hitID uint64
	hitText := false
	for i := len(m.doc.Annotations) - 1; i >= 0; i-- {
		if a := m.doc.Annotations[i]; a.Hit(p, tol) {
			_, hitText = a.Shape.(render.Text)
			hitID = a.ID
			break
		}
	}
	if hitID != 0 && hitID == m.typing {
		return
	}
	m.commitText()
	if hitID != 0 && m.find(hitID) < 0 {
		hitID = 0
	}

	if m.tool == render.ToolSelect || (hitID != 0 && m.selected(hitID)) {
		switch {
		case hitID != 0 && shift:
			if i := slices.Index(m.sel, hitID); i >= 0 {
				m.sel = slices.Delete(m.sel, i, i+1)
			} else {
				m.sel = append(m.sel, hitID)
			}
		case hitID != 0:
			if !m.selected(hitID) {
				m.sel = []uint64{hitID}
			}
			m.checkpoint()
			if hitText && clicks >= 2 {
				m.sel = []uint64{hitID}
				m.typing = hitID
			} else {
				m.drag = dragMove{p}
			}
		default:
			var base []uint64
			if shift {
				base = slices.Clone(m.sel)
			}
			m.sel = slices.Clone(base)
			m.drag = dragMarquee{p, p, base}
		}
		return
	}

	if m.tool == render.ToolText && hitID != 0 && hitText {
		m.sel = []uint64{hitID}
		m.checkpoint()
		m.typing = hitID
		return
	}

	dpi := m.doc.DPI
	box := render.RectFromPts(p, p)
	var shape render.Shape
	switch m.tool {
	case render.ToolArrow:
		shape = render.Arrow{From: p, To: p}
	case render.ToolLine:
		shape = render.Line{From: p, To: p}
	case render.ToolRect:
		shape = render.RectShape{Rect: box}
	case render.ToolFilledRect:
		shape = render.RectShape{Rect: box, Filled: true}
	case render.ToolEllipse:
		shape = render.Ellipse{Rect: box}
	case render.ToolHighlight:
		shape = render.Highlight{Rect: box}
	case render.ToolPixelate:
		shape = render.Redact{Rect: box, Kind: render.RedactPixelate, Strength: m.ink.redactStrength * dpi}
	case render.ToolBlur:
		shape = render.Redact{Rect: box, Kind: render.RedactBlur, Strength: m.ink.redactStrength * dpi}
	case render.ToolPen:
		shape = render.Pen{Points: []render.Pt{p}}
	case render.ToolText:
		size := m.ink.textSize * dpi
		shape = render.Text{Pos: render.Pt{X: p.X, Y: p.Y - size*0.62}, Size: size, Style: m.ink.textStyle, Font: m.ink.font}
	case render.ToolCounter:
		shape = render.Counter{Center: p, N: m.nextCounter(), Size: m.ink.textSize * 0.5 * dpi}
	default:
		return
	}
	m.checkpoint()
	id := m.newID()
	m.doc.Annotations = append(m.doc.Annotations, render.Annotation{ID: id, Shape: shape, Color: m.ink.color, Width: m.ink.stroke * dpi})
	m.sel = []uint64{id}
	switch m.tool {
	case render.ToolText:
		m.typing = id
	case render.ToolCounter:
	default:
		m.drag = dragCreate{id, p}
	}
}

// dragTo moves the gesture in progress to p; shift snaps lines to 45° and boxes to squares.
func (m *model) dragTo(p render.Pt, shift bool) {
	full := m.fullRect()
	switch d := m.drag.(type) {
	case dragCreate:
		a := m.ann(d.id)
		if a == nil {
			return
		}
		switch s := a.Shape.(type) {
		case render.Arrow:
			s.To = p
			if shift {
				s.To = snapAngle(d.start, p)
			}
			a.Shape = s
		case render.Line:
			s.To = p
			if shift {
				s.To = snapAngle(d.start, p)
			}
			a.Shape = s
		case render.RectShape, render.Ellipse, render.Highlight, render.Redact:
			end := p
			if shift {
				end = square(d.start, p)
			}
			a.Shape = withBox(s, render.RectFromPts(d.start, end))
		case render.Pen:
			if last := s.Points[len(s.Points)-1]; last.Dist(p) > 1.5 {
				s.Points = append(s.Points, p)
				a.Shape = s
			}
		}
	case dragMove:
		dx, dy := p.X-d.last.X, p.Y-d.last.Y
		for i := range m.doc.Annotations {
			if m.selected(m.doc.Annotations[i].ID) {
				m.doc.Annotations[i].Translate(dx, dy)
			}
		}
		m.drag = dragMove{p}
	case dragHandle:
		if a := m.ann(d.id); a != nil {
			a.SetHandle(d.idx, p)
		}
	case dragMarquee:
		box := render.RectFromPts(d.start, p)
		sel := slices.Clone(d.base)
		for _, a := range m.doc.Annotations {
			if _, ok := a.Bounds().Intersect(box); ok && !slices.Contains(sel, a.ID) {
				sel = append(sel, a.ID)
			}
		}
		m.sel = sel
		m.drag = dragMarquee{d.start, p, d.base}
	case dragCropNew:
		end := clampTo(p, full)
		r := render.RectFromPts(d.start, end)
		if k := m.cropRatio(); k != 0 {
			r = ratioRect(d.start, end, k, full)
		}
		if c, ok := r.RoundOut().Intersect(full); ok {
			m.crop.rect = c
		}
	case dragCropMove:
		r := m.crop.rect
		x := min(max(r.X+p.X-d.last.X, 0), full.W-r.W)
		y := min(max(r.Y+p.Y-d.last.Y, 0), full.H-r.H)
		m.crop.rect = render.R{X: x, Y: y, W: r.W, H: r.H}
		m.drag = dragCropMove{render.Pt{X: d.last.X + (x - r.X), Y: d.last.Y + (y - r.Y)}}
	case dragCropHandle:
		r := m.crop.rect
		q := clampTo(p, full)
		opposite := [4]render.Pt{{X: r.Right(), Y: r.Bottom()}, {X: r.X, Y: r.Bottom()}, {X: r.X, Y: r.Y}, {X: r.Right(), Y: r.Y}}
		var next render.R
		switch k := m.cropRatio(); {
		case d.idx <= 3 && k != 0:
			next = ratioRect(opposite[d.idx], q, k, full)
		case d.idx <= 3:
			next = render.RectFromPts(opposite[d.idx], q)
		case d.idx == 4:
			next = render.RectFromPts(render.Pt{X: r.X, Y: q.Y}, render.Pt{X: r.Right(), Y: r.Bottom()})
		case d.idx == 5:
			next = render.RectFromPts(render.Pt{X: r.X, Y: r.Y}, render.Pt{X: q.X, Y: r.Bottom()})
		case d.idx == 6:
			next = render.RectFromPts(render.Pt{X: r.X, Y: r.Y}, render.Pt{X: r.Right(), Y: q.Y})
		default:
			next = render.RectFromPts(render.Pt{X: q.X, Y: r.Y}, render.Pt{X: r.Right(), Y: r.Bottom()})
		}
		if next.W >= 8 && next.H >= 8 {
			if c, ok := next.RoundOut().Intersect(full); ok {
				m.crop.rect = c
			}
		}
	}
}

// dragEnd finishes the gesture: an annotation too small to see is dropped with its undo step.
func (m *model) dragEnd() {
	switch d := m.drag.(type) {
	case dragCreate:
		if a := m.ann(d.id); a != nil && tiny(*a) {
			m.doc.Annotations = slices.DeleteFunc(m.doc.Annotations, func(x render.Annotation) bool { return x.ID == d.id })
			m.sel = slices.DeleteFunc(m.sel, func(id uint64) bool { return id == d.id })
			if n := len(m.hist.undo); n > 0 {
				m.hist.undo = m.hist.undo[:n-1]
			}
		}
	case dragCropNew:
		if r := m.crop.rect; r.W < 8 || r.H < 8 {
			m.crop.rect = m.fullRect()
		}
	case dragMove, dragHandle:
		m.dropNoopCheckpoint()
	}
	m.drag = nil
}

func withBox(s render.Shape, r render.R) render.Shape {
	switch s := s.(type) {
	case render.RectShape:
		s.Rect = r
		return s
	case render.Ellipse:
		s.Rect = r
		return s
	case render.Highlight:
		return render.Highlight{Rect: r}
	case render.Redact:
		s.Rect = r
		return s
	}
	return s
}

func tiny(a render.Annotation) bool {
	b := a.Bounds()
	switch s := a.Shape.(type) {
	case render.Arrow, render.Line:
		return max(b.W, b.H) < a.Width*4+4
	case render.Pen:
		return len(s.Points) < 2
	}
	return b.W < 4 || b.H < 4
}

// typeText adds typed text to the annotation being typed into.
func (m *model) typeText(s string) {
	if a := m.ann(m.typing); a != nil {
		if t, ok := a.Shape.(render.Text); ok {
			t.Text += s
			a.Shape = t
		}
	}
}

// typeKey handles Enter (commit), Shift+Enter (a new line) and Backspace while typing.
func (m *model) typeKey(key string, shift bool) {
	a := m.ann(m.typing)
	if a == nil {
		m.commitText()
		return
	}
	t, ok := a.Shape.(render.Text)
	if !ok {
		m.commitText()
		return
	}
	switch key {
	case "enter":
		if !shift {
			m.commitText()
			return
		}
		t.Text += "\n"
	case "backspace":
		if _, n := utf8.DecodeLastRuneInString(t.Text); n > 0 {
			t.Text = t.Text[:len(t.Text)-n]
		}
	}
	a.Shape = t
}

func (m *model) deleteSelected() {
	if len(m.sel) == 0 {
		return
	}
	m.checkpoint()
	sel := m.sel
	m.sel = nil
	m.doc.Annotations = slices.DeleteFunc(m.doc.Annotations, func(a render.Annotation) bool { return slices.Contains(sel, a.ID) })
	m.typing = 0
}

func (m *model) selectAll() {
	m.setTool(render.ToolSelect)
	m.sel = nil
	for _, a := range m.doc.Annotations {
		m.sel = append(m.sel, a.ID)
	}
}

func (m *model) nudge(dx, dy float32) {
	m.modifySelected("nudge", func(a *render.Annotation) { a.Translate(dx, dy) })
}

// escape commits typing, cancels a crop, or clears the selection.
func (m *model) escape() {
	switch {
	case m.typing != 0:
		m.commitText()
	case m.crop != nil:
		m.setTool(render.ToolSelect)
	default:
		m.sel = nil
	}
}

// viewDoc is the document the canvas shows: the uncropped image while a crop is edited, and a
// caret after the text being typed.
func (m *model) viewDoc() render.EditDoc {
	d := m.doc.Clone()
	if m.crop != nil {
		w, h := m.baseSize()
		c := m.doc.CropRect(w, h)
		d.Crop = nil
		for i := range d.Annotations {
			d.Annotations[i].Translate(c.X, c.Y)
		}
		d.Effects.Perspective = render.DefaultPerspective()
		d.Effects.Blur.Enabled = false
	}
	if m.typing != 0 {
		for i, a := range d.Annotations {
			if t, ok := a.Shape.(render.Text); ok && a.ID == m.typing {
				t.Text += "|"
				d.Annotations[i].Shape = t
			}
		}
	}
	return d
}

// styleContext is which controls the style bar shows: those of the selection, or of the tool.
type styleContext struct {
	color, stroke, text, size, redact bool
	fill                              *bool
}

func (m *model) styleContext() styleContext {
	var cx styleContext
	add := func(s render.Shape) {
		switch s := s.(type) {
		case render.Arrow, render.Line, render.Pen:
			cx.color, cx.stroke = true, true
		case render.RectShape:
			cx.color = true
			cx.stroke = cx.stroke || !s.Filled
			f := s.Filled
			cx.fill = &f
		case render.Ellipse:
			cx.color = true
			cx.stroke = cx.stroke || !s.Filled
			f := s.Filled
			cx.fill = &f
		case render.Text:
			cx.color, cx.text, cx.size = true, true, true
		case render.Counter:
			cx.color, cx.size = true, true
		case render.Redact:
			cx.redact = true
		}
	}
	if len(m.sel) > 0 {
		for _, a := range m.selectedAnns() {
			add(a.Shape)
		}
		return cx
	}
	switch m.tool {
	case render.ToolArrow, render.ToolLine, render.ToolPen:
		add(render.Pen{})
	case render.ToolRect, render.ToolEllipse:
		add(render.RectShape{})
	case render.ToolFilledRect:
		add(render.RectShape{Filled: true})
	case render.ToolText:
		add(render.Text{})
	case render.ToolCounter:
		add(render.Counter{})
	case render.ToolPixelate, render.ToolBlur:
		add(render.Redact{})
	}
	return cx
}

func (m *model) setColor(c render.Color) {
	m.ink.color = c
	m.modifySelected("ink", func(a *render.Annotation) {
		if !a.IsRedaction() {
			a.Color = c
		}
	})
}

func (m *model) setStroke(v float32) {
	m.ink.stroke = float32(math.Round(float64(v)))
	w := m.ink.stroke * m.doc.DPI
	m.modifySelected("stroke", func(a *render.Annotation) { a.Width = w })
}

func (m *model) setFill(filled bool) {
	if len(m.sel) == 0 {
		m.tool = render.ToolRect
		if filled {
			m.tool = render.ToolFilledRect
		}
	}
	m.modifySelected("fill", func(a *render.Annotation) {
		switch s := a.Shape.(type) {
		case render.RectShape:
			s.Filled = filled
			a.Shape = s
		case render.Ellipse:
			s.Filled = filled
			a.Shape = s
		}
	})
}

// textFont is the font the style bar shows: the selected text's, or the ink's.
func (m *model) textFont() (render.TextFont, render.TextStyle) {
	for _, a := range m.selectedAnns() {
		if t, ok := a.Shape.(render.Text); ok {
			return t.Font, t.Style
		}
	}
	return m.ink.font, m.ink.textStyle
}

func (m *model) setFont(f render.TextFont) {
	m.ink.font = f
	m.modifySelected("tx-font", func(a *render.Annotation) {
		if t, ok := a.Shape.(render.Text); ok {
			t.Font = f
			a.Shape = t
		}
	})
}

func (m *model) setTextStyle(ts render.TextStyle) {
	m.ink.textStyle = ts
	m.modifySelected("ts", func(a *render.Annotation) {
		if t, ok := a.Shape.(render.Text); ok {
			t.Style = ts
			a.Shape = t
		}
	})
}

func (m *model) setTextSize(v float32) {
	m.ink.textSize = float32(math.Round(float64(v)))
	s := m.ink.textSize * m.doc.DPI
	m.modifySelected("tsize", func(a *render.Annotation) {
		switch sh := a.Shape.(type) {
		case render.Text:
			sh.Size = s
			a.Shape = sh
		case render.Counter:
			sh.Size = s * 0.5
			a.Shape = sh
		}
	})
}

// selectedRedaction is the first selected redaction's strength in points and kind.
func (m *model) selectedRedaction() (float32, render.RedactStyle, bool) {
	for _, a := range m.selectedAnns() {
		if r, ok := a.Shape.(render.Redact); ok {
			return r.Strength / m.doc.DPI, r.Kind, true
		}
	}
	return 0, 0, false
}

func (m *model) setRedactKind(k render.RedactStyle) {
	m.modifySelected("rk", func(a *render.Annotation) {
		if r, ok := a.Shape.(render.Redact); ok {
			r.Kind = k
			a.Shape = r
		}
	})
}

func (m *model) setRedactStrength(v float32) {
	m.ink.redactStrength = float32(math.Round(float64(v)))
	s := m.ink.redactStrength * m.doc.DPI
	m.modifySelected("strength", func(a *render.Annotation) {
		if r, ok := a.Shape.(render.Redact); ok {
			r.Strength = s
			a.Shape = r
		}
	})
}

func (m *model) addPreset() string {
	n := 1
	for slices.ContainsFunc(m.presets, func(p render.Preset) bool { return p.Name == fmt.Sprintf("Preset %d", n) }) {
		n++
	}
	name := fmt.Sprintf("Preset %d", n)
	m.presets = append(m.presets, render.PresetFromDoc(name, m.doc))
	return name
}

func (m *model) applyPreset(i int) {
	if i < 0 || i >= len(m.presets) {
		return
	}
	m.checkpoint()
	m.presets[i].Apply(&m.doc)
	m.dropNoopCheckpoint()
}

func (m *model) deletePreset(i int) (string, bool) {
	if i < 0 || i >= len(m.presets) {
		return "", false
	}
	name := m.presets[i].Name
	m.presets = slices.Delete(m.presets, i, i+1)
	return name, true
}

// importPresets adds presets, renaming those whose name is taken.
func (m *model) importPresets(list []render.Preset) {
	for _, p := range list {
		base := p.Name
		for k := 2; slices.ContainsFunc(m.presets, func(q render.Preset) bool { return q.Name == p.Name }); k++ {
			p.Name = fmt.Sprintf("%s %d", base, k)
		}
		m.presets = append(m.presets, p)
	}
}

func (m *model) resetLook() {
	m.edit("reset", func(d *render.EditDoc) {
		d.Style = render.DefaultStyle()
		d.Effects = render.DefaultEffects()
	})
}
