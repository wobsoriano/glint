package main

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log"
	"math"
	"math/rand/v2"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/vector"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const docW, docH = 2560, 1440

type annKind int

const (
	annRect annKind = iota
	annArrow
	annEllipse
	annText
	annHighlight
)

type annotation struct {
	kind annKind
	box  ui.Rect // document pixels
	col  ui.Color
}

// editor draws a 2560×1440 capture with 50 annotations under a pan and zoom, one selected with
// handles, and moves the selected one as the pointer drags. -variant picks how: vector (the
// painter draws everything each frame), cpu (an *image.RGBA composited on the CPU and pushed with
// Bitmap.Update), canvas (a ui.Canvas redrawn when the document changes).
type editor struct {
	variant string
	base    *image.RGBA
	baseBmp *ui.Bitmap
	anns    []annotation
	sel     int
	zoom    float32
	pan     ui.Point
	drag    bool
	grab    ui.Point
	dirty   bool

	cpu      [2]*image.RGBA
	cpuIdx   int
	cpuBmp   *ui.Bitmap
	canvas   *ui.Canvas
	composed []time.Duration // cpu: compositing per frame
	copied   []time.Duration // cpu: of which copying the capture
	draws    []time.Time
	drawCost []time.Duration
}

func newEditor(variant string) *editor {
	e := &editor{variant: variant, base: fakeDesktop(docW, docH), zoom: 0.5, pan: ui.Point{X: 20, Y: 20}, dirty: true}
	e.baseBmp = ui.NewBitmap(e.base)
	r := rand.New(rand.NewPCG(5, 6))
	cols := []ui.Color{ui.Hex("#E81123"), ui.Hex("#0078D4"), ui.Hex("#FFB900"), ui.Hex("#107C10")}
	for i := 0; i < 50; i++ {
		x, y := float32(r.IntN(docW-400)), float32(r.IntN(docH-300))
		e.anns = append(e.anns, annotation{kind: annKind(i % 5), box: ui.Rect{X: x, Y: y, W: float32(80 + r.IntN(300)), H: float32(40 + r.IntN(200))}, col: cols[i%4]})
	}
	e.sel = 7
	e.anns[e.sel].box = ui.Rect{X: 300, Y: 300, W: 300, H: 180}
	if variant == "cpu" {
		e.cpu = [2]*image.RGBA{image.NewRGBA(image.Rect(0, 0, docW, docH)), image.NewRGBA(image.Rect(0, 0, docW, docH))}
		e.compose()
		e.cpuBmp = ui.NewBitmap(e.cpu[e.cpuIdx])
	}
	if variant == "canvas" {
		e.canvas = ui.NewCanvas(docW, docH)
	}
	return e
}

func (e *editor) view(c *ui.Context) {
	p := theme.Apply(c)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(44).Padding(0, 12).Gap(4).Background(p.Layer).BorderWidth(0, 0, 1, 0).BorderColor(p.Border).Children(func() {
			for _, ic := range []theme.IconName{theme.IconSelect, theme.IconArrow, theme.IconRect, theme.IconEllipse, theme.IconText, theme.IconHighlight, theme.IconCounter, theme.IconRedact, theme.IconCrop} {
				ui.ButtonBase(c.Key(ic)).Size(32, 32).Radius(theme.RadiusControl).Center().Children(func() { ui.Icon(c, theme.Icon(ic)).Size(18, 18) })
			}
			ui.Spacer(c)
			ui.Text(c, "variant "+e.variant).TextColor(p.Muted)
		})
		box := ui.Box(c).Grow(1).Background(p.Canvas).Clip()
		box.HandleInput(e.input)
		box.Draw(e.paint)
	})
}

func (e *editor) toDoc(x, y float32) ui.Point {
	return ui.Point{X: (x - e.pan.X) / e.zoom, Y: (y - e.pan.Y) / e.zoom}
}

func (e *editor) input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		d := e.toDoc(ev.X, ev.Y)
		b := e.anns[e.sel].box
		e.drag, e.grab = true, ui.Point{X: d.X - b.X, Y: d.Y - b.Y}
	case ui.InputPointerMove:
		if e.drag {
			d := e.toDoc(ev.X, ev.Y)
			e.anns[e.sel].box.X, e.anns[e.sel].box.Y = d.X-e.grab.X, d.Y-e.grab.Y
			e.dirty = true
		}
	case ui.InputPointerUp:
		e.drag = false
	case ui.InputScroll:
		e.zoom = max(0.1, min(4, e.zoom*float32(math.Pow(1.1, float64(-ev.DY/40)))))
		e.dirty = true
	default:
		return false
	}
	return true
}

func (e *editor) paint(p *ui.Painter, r ui.Rect) {
	t0 := time.Now()
	view := ui.Scaling(e.zoom, e.zoom).Then(ui.Translation(e.pan.X, e.pan.Y))
	docAt := ui.Rect{X: r.X, Y: r.Y, W: docW, H: docH}
	doc := ui.Rect{W: docW, H: docH}
	switch e.variant {
	case "vector":
		p.Transform(view, r.X, r.Y, func() {
			p.Image(e.baseBmp, docAt, ui.FillBox)
			for _, a := range e.anns {
				a.box.X, a.box.Y = a.box.X+r.X, a.box.Y+r.Y
				drawAnn(p, a)
			}
		})
	case "cpu":
		if e.dirty {
			t := time.Now()
			e.cpuIdx ^= 1
			e.compose()
			e.composed = append(e.composed, time.Since(t))
			e.cpuBmp.Update(e.cpu[e.cpuIdx])
		}
		p.Transform(view, r.X, r.Y, func() { p.Image(e.cpuBmp, docAt, ui.FillBox) })
	case "canvas":
		if e.dirty {
			e.canvas.Draw(p, func(cp *ui.Painter) {
				cp.Image(e.baseBmp, doc, ui.FillBox)
				for _, a := range e.anns {
					drawAnn(cp, a)
				}
			})
		}
		p.Transform(view, r.X, r.Y, func() { p.Image(e.canvas.Bitmap(), docAt, ui.FillBox) })
	}
	e.dirty = false
	b := e.anns[e.sel].box
	sel := ui.Rect{X: r.X + e.pan.X + b.X*e.zoom, Y: r.Y + e.pan.Y + b.Y*e.zoom, W: b.W * e.zoom, H: b.H * e.zoom}
	p.Stroke(sel, ui.Hex("#0078D4"), 0, 1)
	for _, h := range [][2]float32{{0, 0}, {0.5, 0}, {1, 0}, {0, 0.5}, {1, 0.5}, {0, 1}, {0.5, 1}, {1, 1}} {
		hr := ui.Rect{X: sel.X + sel.W*h[0] - 4, Y: sel.Y + sel.H*h[1] - 4, W: 8, H: 8}
		p.Fill(hr, ui.RGB(255, 255, 255), 2)
		p.Stroke(hr, ui.Hex("#0078D4"), 2, 1)
	}
	e.draws = append(e.draws, t0)
	e.drawCost = append(e.drawCost, time.Since(t0))
}

func drawAnn(p *ui.Painter, a annotation) {
	b := a.box
	switch a.kind {
	case annRect:
		p.Stroke(b, a.col, 6, 6)
	case annHighlight:
		p.Fill(b, a.col.Alpha(0.35), 2)
	case annEllipse:
		var path ui.Path
		const k = 0.5523
		cx, cy, rx, ry := b.X+b.W/2, b.Y+b.H/2, b.W/2, b.H/2
		path.MoveTo(cx+rx, cy).CubeTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry).CubeTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy).
			CubeTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry).CubeTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy).Close()
		p.StrokePath(&path, 6, a.col)
	case annArrow:
		var path ui.Path
		path.MoveTo(b.X, b.Y+b.H).LineTo(b.X+b.W, b.Y)
		p.StrokePath(&path, 8, a.col)
		var head ui.Path
		head.MoveTo(b.X+b.W, b.Y).LineTo(b.X+b.W-34, b.Y+6).LineTo(b.X+b.W-14, b.Y+26).Close()
		p.FillPath(&head, a.col)
	case annText:
		p.RichText(b.X, b.Y, b.W, ui.Span{Text: "Glint annotation text", Size: 32, Weight: 600, Color: a.col})
	}
}

// compose is the CPU path: the capture and the annotations into an *image.RGBA, as
// internal/render would composite them.
func (e *editor) compose() {
	dst := e.cpu[e.cpuIdx]
	t := time.Now()
	copy(dst.Pix, e.base.Pix)
	e.copied = append(e.copied, time.Since(t))
	var z vector.Rasterizer
	for _, a := range e.anns {
		b := a.box
		c := color.RGBA{a.col.R, a.col.G, a.col.B, 255}
		x0, y0 := int(b.X), int(b.Y)
		z.Reset(int(b.W)+16, int(b.H)+16)
		ox, oy := float32(8), float32(8)
		switch a.kind {
		case annRect, annText:
			rectPath(&z, ox, oy, b.W, b.H)
			rectPath(&z, ox+6, oy+6, b.W-12, b.H-12)
		case annHighlight:
			rectPath(&z, ox, oy, b.W, b.H)
			c = color.RGBA{uint8(int(c.R) * 35 / 100), uint8(int(c.G) * 35 / 100), uint8(int(c.B) * 35 / 100), 89}
		case annEllipse:
			ellipsePath(&z, ox+b.W/2, oy+b.H/2, b.W/2, b.H/2)
			ellipsePath(&z, ox+b.W/2, oy+b.H/2, b.W/2-6, b.H/2-6)
		case annArrow:
			z.MoveTo(ox, oy+b.H-4)
			z.LineTo(ox+b.W-4, oy)
			z.LineTo(ox+b.W, oy+4)
			z.LineTo(ox+4, oy+b.H)
			z.ClosePath()
		}
		r := image.Rect(x0-8, y0-8, x0-8+int(b.W)+16, y0-8+int(b.H)+16)
		if !r.In(dst.Bounds()) {
			continue
		}
		z.DrawOp = draw.Over
		z.Draw(dst, r, image.NewUniform(c), image.Point{})
	}
}

func rectPath(z *vector.Rasterizer, x, y, w, h float32) {
	z.MoveTo(x, y)
	z.LineTo(x+w, y)
	z.LineTo(x+w, y+h)
	z.LineTo(x, y+h)
	z.ClosePath()
}

func ellipsePath(z *vector.Rasterizer, cx, cy, rx, ry float32) {
	for i := 0; i <= 48; i++ {
		a := float64(i) / 48 * 2 * math.Pi
		x, y := cx+rx*float32(math.Cos(a)), cy+ry*float32(math.Sin(a))
		if i == 0 {
			z.MoveTo(x, y)
		} else {
			z.LineTo(x, y)
		}
	}
	z.ClosePath()
}

// frameLog collects MyGo's MYGO_FRAME_STATS lines.
type frameLog struct {
	mu    sync.Mutex
	lines []string
}

func (f *frameLog) Write(b []byte) (int, error) {
	f.mu.Lock()
	f.lines = append(f.lines, string(b))
	f.mu.Unlock()
	return len(b), nil
}

var statLine = regexp.MustCompile(`took ([\d.]+) ms: build ([\d.]+).*layout ([\d.]+), paint ([\d.]+), present ([\d.]+) \(([^)]*)\)`)

func (f *frameLog) summary(from int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var total, paint, present []float64
	paths := map[string]int{}
	for _, l := range f.lines[from:] {
		m := statLine.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		t, _ := strconv.ParseFloat(m[1], 64)
		pa, _ := strconv.ParseFloat(m[4], 64)
		pr, _ := strconv.ParseFloat(m[5], 64)
		total, paint, present = append(total, t), append(paint, pa), append(present, pr)
		kind, _, _ := strings.Cut(m[6], ":")
		if strings.HasPrefix(m[6], "drawn on the GPU: redrew") {
			kind = "drawn on the GPU, damaged part only"
		}
		paths[kind]++
	}
	return fmt.Sprintf("%d frames; frame ms %s; paint %s; present %s; paths %v", len(total), stats(total), stats(paint), stats(present), paths)
}

func (f *frameLog) mark() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.lines)
}

func stats(v []float64) string {
	if len(v) == 0 {
		return "-"
	}
	s := slices.Clone(v)
	slices.Sort(s)
	q := func(p float64) float64 { return s[min(len(s)-1, int(p*float64(len(s))))] }
	return fmt.Sprintf("p50 %.2f p90 %.2f max %.2f", q(0.5), q(0.9), s[len(s)-1])
}

func durs(d []time.Duration) []float64 {
	out := make([]float64, len(d))
	for i, v := range d {
		out[i] = ms(v)
	}
	return out
}

func runEditor() {
	frames := &frameLog{}
	log.SetOutput(frames)
	v := *variant
	if v == "fullscreen" {
		v = "vector"
	}
	mygo.App.WhenReady(func() {
		e := newEditor(v)
		win := mygo.NewWindow(mygo.WindowOptions{Title: "Glint editor spike", Width: 1400, Height: 900, X: 100, Y: 60, Content: ui.View(e.view)})
		if *auto {
			go driveEditor(e, win, frames)
		}
	})
}

func driveEditor(e *editor, win *mygo.Window, frames *frameLog) {
	failures := 0
	defer finish(&failures)
	saved := cursorPos()
	defer moveTo(saved.X, saved.Y)
	time.Sleep(800 * time.Millisecond)
	hwnd := win.NativeHandle()
	pSetForegroundWindow.Call(hwnd)
	cr := clientRect(hwnd)
	time.Sleep(300 * time.Millisecond)
	var sel ui.Rect
	mygo.RunOnMain(func() {
		b := e.anns[e.sel].box
		sel = ui.Rect{X: e.pan.X + b.X*e.zoom, Y: e.pan.Y + b.Y*e.zoom, W: b.W * e.zoom, H: b.H * e.zoom}
	})
	from := point{cr.Left + int32(sel.X+sel.W/2), cr.Top + 44 + int32(sel.Y+sel.H/2)}
	mark := frames.mark()
	mygo.RunOnMain(func() { e.draws, e.drawCost, e.composed, e.copied = nil, nil, nil, nil })
	t0 := time.Now()
	steps := 0
	moveTo(from.X, from.Y)
	sendMouse(mouseLeftDown)
	for time.Since(t0) < 2500*time.Millisecond {
		f := float64(time.Since(t0)) / float64(2500*time.Millisecond)
		x := from.X + int32(500*math.Sin(f*2*math.Pi))
		y := from.Y + int32(200*math.Sin(f*4*math.Pi))
		moveTo(x, y)
		steps++
		time.Sleep(4 * time.Millisecond)
	}
	sendMouse(mouseLeftUp)
	time.Sleep(200 * time.Millisecond)
	var intervals []float64
	var cost, composed, copied []float64
	mygo.RunOnMain(func() {
		for i := 1; i < len(e.draws); i++ {
			intervals = append(intervals, ms(e.draws[i].Sub(e.draws[i-1])))
		}
		cost, composed, copied = durs(e.drawCost), durs(e.composed), durs(e.copied)
	})
	drawsPerSec := float64(len(intervals)+1) / 2.5
	logf("editor %s: %d pointer moves, %d Draw calls (%.0f/s); Draw interval %s; Draw callback cost %s; CPU compose %s, of which copying the 2560x1440 capture %s",
		e.variant, steps, len(intervals)+1, drawsPerSec, stats(intervals), stats(cost), stats(composed), stats(copied))
	logf("editor %s MYGO_FRAME_STATS: %s", e.variant, frames.summary(mark))
	check(&failures, drawsPerSec > 40, "the %s editor redraws at %.0f frames a second while dragging", e.variant, drawsPerSec)
	shot := captureScreen(cr)
	savePNG(out("editor-"+e.variant+".png"), downscale(shot, 2))
}

// downscale halves img n-1 times... by averaging n×n blocks.
func downscale(img *image.RGBA, n int) *image.RGBA {
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx()/n, b.Dy()/n))
	for y := 0; y < out.Bounds().Dy(); y++ {
		for x := 0; x < out.Bounds().Dx(); x++ {
			var s [4]int
			for dy := 0; dy < n; dy++ {
				for dx := 0; dx < n; dx++ {
					c := img.RGBAAt(b.Min.X+x*n+dx, b.Min.Y+y*n+dy)
					s[0], s[1], s[2], s[3] = s[0]+int(c.R), s[1]+int(c.G), s[2]+int(c.B), s[3]+int(c.A)
				}
			}
			out.SetRGBA(x, y, color.RGBA{uint8(s[0] / (n * n)), uint8(s[1] / (n * n)), uint8(s[2] / (n * n)), uint8(s[3] / (n * n))})
		}
	}
	return out
}
