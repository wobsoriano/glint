package record

import (
	"math"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// dragSlop is how far the pointer moves, in DIPs, before a press becomes a drag.
const dragSlop = 4

type selector struct {
	display platform.Display
	shot    *ui.Bitmap
	windows []platform.Window // front to back, on this display
	done    func(Selection, bool)
	win     *mygo.Window

	pressed, dragging bool
	from, to          ui.Point
	hover             int // index into windows, -1 for none
	finished          bool
}

// Select lets the user drag an area of the display or click a window on it, over a frozen
// picture of the screen, and calls done on the main thread; Esc calls it with ok false. It is
// a Host.SelectRegion for hosts without a capture overlay of their own. Main thread only.
func Select(kind settings.RecordTarget, display platform.Display, done func(sel Selection, ok bool)) {
	shot, err := platform.CaptureRect(display.Bounds)
	if err != nil {
		done(Selection{}, false)
		return
	}
	s := &selector{display: display, shot: ui.NewBitmap(shot).Pixelated(), done: done, hover: -1}
	if list, err := platform.Windows(); err == nil {
		for _, w := range list {
			if w.PID != os.Getpid() && !w.Bounds.Intersect(display.Bounds).Empty() {
				s.windows = append(s.windows, w)
			}
		}
	}
	d := display.Bounds
	s.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Glint select", Width: dips(d.W, display.Scale), Height: dips(d.H, display.Scale),
		Frameless: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true, DisableResize: true, DisableShadow: true, Panel: true,
		Content: ui.View(s.view),
	})
	s.win.OnClose(func(e *mygo.CloseEvent) {
		if !s.finished {
			e.PreventDefault()
			later(func() { s.finish(Selection{}, false) })
		}
	})
	squareCorners(s.win)
	place(s.win, d)
	s.win.Show()
	s.win.Focus()
}

func (s *selector) finish(sel Selection, ok bool) {
	if s.finished {
		return
	}
	s.finished = true
	s.win.Destroy()
	s.done(sel, ok)
}

// px is a point of the view in physical desktop pixels.
func (s *selector) px(p ui.Point) (int, int) {
	k := s.display.Scale
	return s.display.Bounds.X + int(math.Round(float64(p.X)*k)), s.display.Bounds.Y + int(math.Round(float64(p.Y)*k))
}

func (s *selector) windowAt(p ui.Point) int {
	x, y := s.px(p)
	for i, w := range s.windows {
		if w.Bounds.Contains(x, y) {
			return i
		}
	}
	return -1
}

func (s *selector) dragRect() platform.Rect {
	x0, y0 := s.px(s.from)
	x1, y1 := s.px(s.to)
	return platform.Rect{X: min(x0, x1), Y: min(y0, y1), W: abs(x1 - x0), H: abs(y1 - y0)}
}

func abs(v int) int { return max(v, -v) }

func (s *selector) view(ctx *ui.Context) {
	p := theme.Apply(ctx)
	ctx.Root().Background(ui.RGB(0, 0, 0))
	if ctx.Shortcut(0, ui.KeyEscape) {
		later(func() { s.finish(Selection{}, false) })
	}
	box := ui.Box(ctx).Fill().Cursor(ui.CursorCrosshair).Focusable().AutoFocus()
	box.HandleInput(func(ev ui.InputEvent) bool {
		pt := ui.Point{X: ev.X, Y: ev.Y}
		switch ev.Kind {
		case ui.InputPointerDown:
			s.pressed, s.dragging, s.from, s.to = true, false, pt, pt
		case ui.InputPointerMove:
			s.to = pt
			if s.pressed && (abs32(pt.X-s.from.X) > dragSlop || abs32(pt.Y-s.from.Y) > dragSlop) {
				s.dragging = true
			}
			if !s.pressed {
				s.hover = s.windowAt(pt)
			}
		case ui.InputPointerUp:
			s.to = pt
			switch {
			case s.dragging:
				r := s.dragRect()
				later(func() { s.finish(Selection{Rect: r}, true) })
			case s.hover >= 0:
				w := s.windows[s.hover]
				later(func() { s.finish(Selection{Rect: w.Bounds, Window: &w}, true) })
			}
			s.pressed, s.dragging = false, false
		default:
			return false
		}
		return true
	})
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		pt.Image(s.shot, r, ui.FillBox)
		var hole ui.Rect
		k := float32(s.display.Scale)
		toView := func(q platform.Rect) ui.Rect {
			return ui.Rect{X: r.X + float32(q.X-s.display.Bounds.X)/k, Y: r.Y + float32(q.Y-s.display.Bounds.Y)/k, W: float32(q.W) / k, H: float32(q.H) / k}
		}
		switch {
		case s.dragging:
			hole = toView(s.dragRect())
		case s.hover >= 0:
			hole = toView(s.windows[s.hover].Bounds)
		}
		dimAround(pt, r, hole, p.Scrim)
		if hole.W > 0 {
			pt.StrokeDashed(ui.Rect{X: hole.X - 2, Y: hole.Y - 2, W: hole.W + 4, H: hole.H + 4}, p.Accent, 0, 2)
		}
	})
	box.Children(func() {
		ui.Row(ctx).Absolute().Top(24).FillWidth().Justify(ui.Center).PassThrough().Children(func() {
			ui.Row(ctx).Height(32).PaddingX(14).AlignItems(ui.Center).Radius(16).Background(p.Flyout).Border(1, p.Border).Children(func() {
				ui.Text(ctx, "Drag to record an area, or click a window · Esc to go back").FontSize(theme.Caption).TextColor(p.Text).SingleLine()
			})
		})
	})
}

func abs32(v float32) float32 { return max(v, -v) }

// dimAround shades r except the hole.
func dimAround(pt *ui.Painter, r, hole ui.Rect, c ui.Color) {
	if hole.W <= 0 || hole.H <= 0 {
		pt.Fill(r, c, 0)
		return
	}
	pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: hole.Y - r.Y}, c, 0)
	pt.Fill(ui.Rect{X: r.X, Y: hole.Y + hole.H, W: r.W, H: r.Y + r.H - hole.Y - hole.H}, c, 0)
	pt.Fill(ui.Rect{X: r.X, Y: hole.Y, W: hole.X - r.X, H: hole.H}, c, 0)
	pt.Fill(ui.Rect{X: hole.X + hole.W, Y: hole.Y, W: r.X + r.W - hole.X - hole.W, H: hole.H}, c, 0)
}
