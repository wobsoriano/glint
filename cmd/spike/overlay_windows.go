package main

import (
	"cmp"
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// overlay is the capture overlay of one display: the frozen screen drawn 1:1, a crosshair, a
// selection with the rest dimmed, a live size label and a magnifier.
type overlay struct {
	mon     monitor
	win     *mygo.Window
	hwnd    uintptr
	shot    *image.RGBA
	bmp     *ui.Bitmap
	dim     bool
	scale   float32
	pointer ui.Point // DIPs in the window
	inside  bool
	press   ui.Point
	sel     ui.Rect // DIPs; W 0 for none
	drag    bool
	window  bool // Space toggles window mode

	shownAt   time.Time
	firstDraw time.Duration
	drawn     bool
	events    []string
	onKey     func(string)
}

func (o *overlay) toPhysical(p ui.Point) image.Point {
	return image.Pt(int(math.Round(float64(p.X*o.scale))), int(math.Round(float64(p.Y*o.scale))))
}

func (o *overlay) selPhysical() image.Rectangle {
	a := o.toPhysical(ui.Point{X: o.sel.X, Y: o.sel.Y})
	b := o.toPhysical(ui.Point{X: o.sel.X + o.sel.W, Y: o.sel.Y + o.sel.H})
	return image.Rectangle{a, b}
}

func (o *overlay) note(format string, args ...any) {
	s := fmt.Sprintf(format, args...)
	o.events = append(o.events, s)
	logf("overlay: %s", s)
}

func (o *overlay) view(c *ui.Context) {
	p := theme.Apply(c)
	c.Root().Background(ui.RGB(0, 0, 0))
	if c.Shortcut(0, ui.KeyEscape) {
		o.note("shortcut Escape")
		if o.onKey != nil {
			o.onKey("escape")
		}
		o.hide()
	}
	box := ui.Box(c).Fill().Cursor(ui.CursorCrosshair).Focusable().AutoFocus()
	box.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			o.press, o.drag = ui.Point{X: ev.X, Y: ev.Y}, true
			o.sel = ui.Rect{X: ev.X, Y: ev.Y}
			o.note("down at %.2f,%.2f DIP = %v px", ev.X, ev.Y, o.toPhysical(o.press))
		case ui.InputPointerMove:
			o.pointer, o.inside = ui.Point{X: ev.X, Y: ev.Y}, true
			if o.drag {
				o.sel = ui.Rect{X: min(o.press.X, ev.X), Y: min(o.press.Y, ev.Y), W: abs32(ev.X - o.press.X), H: abs32(ev.Y - o.press.Y)}
			}
		case ui.InputPointerUp:
			o.drag = false
			o.note("up at %.2f,%.2f DIP = %v px; selection %v px", ev.X, ev.Y, o.toPhysical(ui.Point{X: ev.X, Y: ev.Y}), o.selPhysical())
		case ui.InputKeyDown:
			switch ev.Key {
			case ui.KeySpace:
				o.window = !o.window
				o.note("key Space: window mode %v", o.window)
				if o.onKey != nil {
					o.onKey("space")
				}
			case ui.KeyEscape:
				o.note("key Escape")
				if o.onKey != nil {
					o.onKey("escape")
				}
				o.hide()
			default:
				return false
			}
		default:
			return false
		}
		return true
	})
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		if !o.drawn && !o.shownAt.IsZero() {
			o.drawn, o.firstDraw = true, time.Since(o.shownAt)
			o.note("first Draw %.1f ms after Show, painter scale %.3f, box %.2fx%.2f DIP", ms(o.firstDraw), pt.Scale(), r.W, r.H)
		}
		o.scale = pt.Scale()
		pt.Image(o.bmp, r, ui.FillBox)
		o.paint(pt, r, p)
	})
}

func (o *overlay) paint(pt *ui.Painter, r ui.Rect, p theme.Palette) {
	s := o.sel
	if o.dim {
		if s.W > 0 && s.H > 0 {
			pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: s.Y - r.Y}, p.Scrim, 0)
			pt.Fill(ui.Rect{X: r.X, Y: s.Y + s.H, W: r.W, H: r.Y + r.H - s.Y - s.H}, p.Scrim, 0)
			pt.Fill(ui.Rect{X: r.X, Y: s.Y, W: s.X - r.X, H: s.H}, p.Scrim, 0)
			pt.Fill(ui.Rect{X: s.X + s.W, Y: s.Y, W: r.X + r.W - s.X - s.W, H: s.H}, p.Scrim, 0)
			pt.Stroke(s, ui.RGB(255, 255, 255), 0, 1/o.scale)
			sz := o.selPhysical().Size()
			label := ui.Span{Text: fmt.Sprintf("%d × %d", sz.X, sz.Y), Size: 12, Weight: 600, Color: ui.RGB(255, 255, 255)}
			w, h := pt.MeasureText(0, label)
			lx, ly := s.X, s.Y+s.H+6
			if ly+h+8 > r.H {
				ly = s.Y - h - 14
			}
			pt.Fill(ui.Rect{X: lx, Y: ly, W: w + 12, H: h + 6}, ui.RGBA(0, 0, 0, 0.75), theme.RadiusControl)
			pt.RichText(lx+6, ly+3, 0, label)
		} else {
			pt.Fill(r, p.Scrim.Alpha(0.5), 0)
		}
	}
	if o.inside && o.dim {
		o.magnifier(pt, r)
	}
}

// magnifier shows 15×15 physical pixels around the pointer, 8 times larger, pixelated.
func (o *overlay) magnifier(pt *ui.Painter, r ui.Rect) {
	const n, zoom = 15, 8
	at := o.toPhysical(o.pointer)
	src := ui.Rect{X: float32(at.X - n/2), Y: float32(at.Y - n/2), W: n, H: n}
	side := float32(n*zoom) / o.scale
	dst := ui.Rect{X: o.pointer.X + 20, Y: o.pointer.Y + 20, W: side, H: side}
	if dst.X+dst.W > r.W {
		dst.X = o.pointer.X - 20 - side
	}
	if dst.Y+dst.H > r.H {
		dst.Y = o.pointer.Y - 20 - side
	}
	pt.Shadow(dst, side/2, 0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.4))
	pt.Clip(dst, side/2, func() {
		pt.ImageRegion(o.bmp, src, dst, ui.ImageOptions{})
		cell := float32(zoom) / o.scale
		mid := ui.Rect{X: dst.X + cell*(n/2), Y: dst.Y + cell*(n/2), W: cell, H: cell}
		pt.Stroke(mid, ui.RGB(255, 255, 255), 0, 1/o.scale)
	})
	pt.Stroke(dst, ui.RGBA(255, 255, 255, 0.8), side/2, 2/o.scale)
	c := o.shot.RGBAAt(at.X, at.Y)
	label := ui.Span{Text: fmt.Sprintf("%d, %d  #%02X%02X%02X", at.X, at.Y, c.R, c.G, c.B), Size: 11, Color: ui.RGB(255, 255, 255)}
	w, h := pt.MeasureText(0, label)
	pt.Fill(ui.Rect{X: dst.X + (dst.W-w)/2 - 6, Y: dst.Y + dst.H + 6, W: w + 12, H: h + 4}, ui.RGBA(0, 0, 0, 0.75), theme.RadiusControl)
	pt.RichText(dst.X+(dst.W-w)/2, dst.Y+dst.H+8, 0, label)
}

var hideGap = flag.Duration("gap", 200*time.Millisecond, "how long the overlay stays hidden between the timed shows")

var variant = flag.String("variant", "", "overlay: panel-square (default), plain, after, nocaption, fullscreen, nocap-mark, notop, notop-nocap, reassert, panel; editor: vector (default), cpu, canvas")

func (o *overlay) show() {
	o.drawn, o.shownAt = false, time.Now()
	switch cmp.Or(*variant, "panel-square") {
	case "plain", "panel":
		placePhysical(o.hwnd, o.mon.bounds)
		o.win.Show()
	case "panel-square":
		setCornerPreference(o.hwnd, 1)     // DWMWCP_DONOTROUND
		setBorderColor(o.hwnd, 0xFFFFFFFE) // DWMWA_COLOR_NONE
		placePhysical(o.hwnd, o.mon.bounds)
		o.win.Show()
	case "after":
		o.win.Show()
		placePhysical(o.hwnd, o.mon.bounds)
	case "nocaption":
		stripCaption(o.hwnd)
		placePhysical(o.hwnd, o.mon.bounds)
		o.win.Show()
	case "fullscreen":
		placePhysical(o.hwnd, o.mon.bounds)
		logf("MarkFullscreenWindow: %v", markFullscreen(o.hwnd))
		o.win.Show()
	case "nocap-mark":
		stripCaption(o.hwnd)
		placePhysical(o.hwnd, o.mon.bounds)
		markFullscreen(o.hwnd)
		o.win.Show()
	case "notop", "notop-nocap":
		if *variant == "notop-nocap" {
			stripCaption(o.hwnd)
		}
		pSetWindowPos.Call(o.hwnd, 0, uintptr(o.mon.bounds.Left), uintptr(o.mon.bounds.Top), uintptr(o.mon.bounds.w()), uintptr(o.mon.bounds.h()), swpNoActivate)
		o.win.Show()
	case "reassert":
		placePhysical(o.hwnd, o.mon.bounds)
		o.win.Show()
		go func() {
			for i := 0; i < 30; i++ {
				time.Sleep(16 * time.Millisecond)
				pSetWindowPos.Call(o.hwnd, hwndTopmost, 0, 0, 0, 0, 0x0001|0x0002|swpNoActivate)
			}
		}()
	}
	o.win.Focus()
	o.win.Invalidate()
}

func (o *overlay) hide() {
	o.win.Hide()
	o.sel, o.inside = ui.Rect{}, false
}

func newOverlay(m monitor, shot *image.RGBA) *overlay {
	o := &overlay{mon: m, shot: shot, bmp: ui.NewBitmap(shot).Pixelated(), scale: 1}
	d := findDisplay(m)
	o.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "Glint capture",
		X:             d.Bounds.X,
		Y:             d.Bounds.Y,
		Width:         d.Bounds.Width,
		Height:        d.Bounds.Height,
		Frameless:     true,
		AlwaysOnTop:   !strings.HasPrefix(*variant, "notop"),
		SkipTaskbar:   true,
		Hidden:        true,
		DisableResize: true,
		DisableShadow: true,
		Panel:         strings.HasPrefix(cmp.Or(*variant, "panel-square"), "panel"),
		Content:       ui.View(o.view),
	})
	o.hwnd = o.win.NativeHandle()
	return o
}

func findDisplay(m monitor) mygo.Display {
	for _, d := range mygo.Screen.Displays() {
		if uintptr(d.ID) == m.handle {
			return d
		}
	}
	return mygo.Screen.PrimaryDisplay()
}

func noise(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	r := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(img.Pix); i += 4 {
		v := r.Uint32()
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = uint8(v), uint8(v>>8), uint8(v>>16), 255
	}
	return img
}

func runOverlay() {
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		var overlays []*overlay
		for _, m := range monitors() {
			d := findDisplay(m)
			logf("display %s physical %v work %v primary %v; MyGo DIP bounds %+v work %+v scale %.2f",
				m.deviceName, m.bounds, m.work, m.primary, d.Bounds, d.WorkArea, d.ScaleFactor)
			shot := captureScreen(m.bounds)
			if *auto {
				shot = noise(int(m.bounds.w()), int(m.bounds.h()))
			}
			overlays = append(overlays, newOverlay(m, shot))
		}
		show := func() {
			for _, o := range overlays {
				o.show()
			}
		}
		err := mygo.GlobalShortcut.Register("Ctrl+Shift+4", show)
		logf("register Ctrl+Shift+4: %v", err)
		if *auto {
			go driveOverlay(overlays)
		} else {
			for _, o := range overlays {
				o.dim = true
			}
		}
	})
}

func driveOverlay(overlays []*overlay) {
	failures := 0
	defer finish(&failures)
	o := overlays[0]
	saved := cursorPos()
	defer moveTo(saved.X, saved.Y)

	fg := exec.Command(os.Args[0], "-mode", "foreground")
	fg.Stdout = os.Stdout
	if err := fg.Start(); err != nil {
		check(&failures, false, "start the foreground app: %v", err)
		return
	}
	defer fg.Process.Kill()
	other := uintptr(0)
	for i := 0; i < 100 && other == 0; i++ {
		time.Sleep(50 * time.Millisecond)
		if h := foreground(); windowText(h) == "spike foreground" {
			other = h
		}
	}
	check(&failures, other != 0, "another app's window is foreground before the hotkey (%q)", windowText(foreground()))

	moveTo(o.mon.bounds.Left+o.mon.bounds.w()/2, o.mon.bounds.Top+o.mon.bounds.h()/2)
	keys(vkControl, vkShift, '4')
	taskbarPt := point{o.mon.work.Left + 8, o.mon.work.Bottom + 4}
	var timeline []string
	t0 := time.Now()
	for time.Since(t0) < 1200*time.Millisecond {
		owner := rootClass(taskbarPt)
		if len(timeline) == 0 || !strings.HasSuffix(timeline[len(timeline)-1], owner) {
			timeline = append(timeline, fmt.Sprintf("%dms %s", time.Since(t0).Milliseconds(), owner))
		}
		time.Sleep(10 * time.Millisecond)
	}
	logf("window over the taskbar at %v after the hotkey: %v", taskbarPt, timeline)
	fgNow := foreground()
	check(&failures, fgNow == o.hwnd, "the overlay is foreground after Ctrl+Shift+4 from another app (foreground %q)", windowText(fgNow))
	check(&failures, o.drawn, "first frame drawn %.1f ms after Show", ms(o.firstDraw))

	wr, cr := windowRect(o.hwnd), clientRect(o.hwnd)
	check(&failures, wr == o.mon.bounds && cr == o.mon.bounds, "window %v and client %v cover the display %v exactly, taskbar included", wr, cr, o.mon.bounds)
	logf("overlay hwnd style %#x exstyle %#x (topmost %v, toolwindow %v, layered %v, noredirection %v) dpi %d corner pref %d",
		style(o.hwnd), exStyle(o.hwnd), exStyle(o.hwnd)&0x8 != 0, exStyle(o.hwnd)&0x80 != 0, exStyle(o.hwnd)&0x80000 != 0, exStyle(o.hwnd)&0x200000 != 0, dpiForWindow(o.hwnd), cornerPreference(o.hwnd))

	moveTo(o.mon.bounds.Left+o.mon.bounds.w()/3, o.mon.bounds.Top+o.mon.bounds.h()/3)
	time.Sleep(200 * time.Millisecond)
	screen := captureScreen(o.mon.bounds)
	n, first := diff(screen, o.shot, 0)
	check(&failures, n == 0, "the frozen bitmap shows 1:1 on screen: %d of %d pixels differ (first at %v)", n, len(o.shot.Pix)/4, first)
	if n > 0 {
		savePNG(out("overlay-1to1-diff.png"), crop(screen, image.Rect(first.X-20, first.Y-20, first.X+40, first.Y+40)))
	}
	corner, _ := diff(crop(screen, image.Rect(0, 0, 12, 12)), crop(o.shot, image.Rect(0, 0, 12, 12)), 0)
	check(&failures, corner == 0, "no rounded corner: %d of 144 corner pixels differ", corner)

	now, cross := cursorShape()
	moveTo(o.mon.bounds.Left+400, o.mon.bounds.Top+300)
	time.Sleep(100 * time.Millisecond)
	now, cross = cursorShape()
	check(&failures, now == cross, "the cursor over the overlay is the system crosshair (%#x, crosshair %#x)", now, cross)

	mygo.RunOnMain(func() {
		real := fakeDesktop(int(o.mon.bounds.w()), int(o.mon.bounds.h()))
		o.shot = real
		o.bmp.Update(real)
		o.dim = true
		o.win.Invalidate()
	})
	from, to := point{o.mon.bounds.Left + 301, o.mon.bounds.Top + 203}, point{o.mon.bounds.Left + 941, o.mon.bounds.Top + 683}
	var frames []time.Duration
	last := time.Now()
	drag(from, to, 40, 8*time.Millisecond)
	_ = frames
	_ = last
	time.Sleep(150 * time.Millisecond)
	moveTo(to.X+3, to.Y-171)
	time.Sleep(200 * time.Millisecond)
	want := image.Rect(int(from.X-o.mon.bounds.Left), int(from.Y-o.mon.bounds.Top), int(to.X-o.mon.bounds.Left), int(to.Y-o.mon.bounds.Top))
	got := image.Rectangle{}
	mygo.RunOnMain(func() { got = o.selPhysical() })
	check(&failures, got == want, "dragging %v to %v selects %v px (want %v)", from, to, got, want)
	shot := captureScreen(o.mon.bounds)
	savePNG(out("overlay-selection.png"), crop(shot, image.Rect(want.Min.X-60, want.Min.Y-60, want.Max.X+200, want.Max.Y+60)))
	in := shot.RGBAAt(want.Min.X+50, want.Min.Y+50)
	src := o.shot.RGBAAt(want.Min.X+50, want.Min.Y+50)
	outside := shot.RGBAAt(want.Min.X-50, want.Min.Y-50)
	srcOut := o.shot.RGBAAt(want.Min.X-50, want.Min.Y-50)
	check(&failures, in == src && dimmer(outside, srcOut), "inside the selection is undimmed (%v = %v), outside is dimmed (%v from %v)", in, src, outside, srcOut)

	gotKey := make(chan string, 4)
	mygo.RunOnMain(func() { o.onKey = func(k string) { gotKey <- k } })
	keys(vkSpace)
	check(&failures, waitKey(gotKey, "space"), "Space reaches the overlay")
	keys(vkEscape)
	check(&failures, waitKey(gotKey, "escape"), "Escape reaches the overlay")
	time.Sleep(200 * time.Millisecond)
	visible := false
	mygo.RunOnMain(func() { visible = o.win.IsVisible() })
	check(&failures, !visible, "Escape hid the overlay")

	var fresh, again []float64
	for i := 0; i < 10; i++ {
		newShot := i%2 == 0
		if newShot {
			shot := noise(int(o.mon.bounds.w()), int(o.mon.bounds.h()))
			mygo.RunOnMain(func() {
				o.shot = shot
				o.bmp.Update(o.shot)
				o.dim = false
			})
		}
		probe := image.Rect(500, 500, 532, 532)
		wantProbe := crop(o.shot, probe)
		screenProbe := rect{o.mon.bounds.Left + 500, o.mon.bounds.Top + 500, o.mon.bounds.Left + 532, o.mon.bounds.Top + 532}
		t0 := time.Now()
		mygo.RunOnMain(o.show)
		for time.Since(t0) < 2*time.Second {
			if d, _ := diff(captureScreen(screenProbe), wantProbe, 0); d == 0 {
				break
			}
			time.Sleep(500 * time.Microsecond)
		}
		took := ms(time.Since(t0))
		if newShot {
			fresh = append(fresh, took)
		} else {
			again = append(again, took)
		}
		logf("show %d (new bitmap %v): first Draw %.1f ms, on screen %.1f ms", i, newShot, ms(o.firstDraw), took)
		mygo.RunOnMain(o.hide)
		time.Sleep(*hideGap)
	}
	logf("Show to pixels on screen: with a new 1920x1080 bitmap %v ms; same bitmap again %v ms", fresh, again)
}

// fakeDesktop stands in for a screenshot in the evidence: a wallpaper gradient and two windows.
func fakeDesktop(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(40 + 60*x/w), uint8(70 + 80*y/h), uint8(140 + 60*x/w), 255})
		}
	}
	box := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				img.SetRGBA(x, y, c)
			}
		}
	}
	box(200, 150, 900, 650, color.RGBA{243, 243, 243, 255})
	box(200, 150, 900, 182, color.RGBA{225, 225, 225, 255})
	for i := 0; i < 12; i++ {
		box(230, 210+i*34, 230+120+(i*97)%420, 222+i*34, color.RGBA{60, 60, 60, 255})
	}
	box(700, 420, 1300, 900, color.RGBA{32, 32, 32, 255})
	for i := 0; i < 10; i++ {
		box(730, 470+i*36, 730+90+(i*131)%400, 482+i*36, color.RGBA{0, 120, 212, 255})
	}
	return img
}

func dimmer(a, b color.RGBA) bool {
	return int(a.R)+int(a.G)+int(a.B) < int(b.R)+int(b.G)+int(b.B) || (b.R < 8 && b.G < 8 && b.B < 8)
}

func waitKey(ch chan string, want string) bool {
	select {
	case k := <-ch:
		return k == want
	case <-time.After(time.Second):
		return false
	}
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func abs32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// runForeground is another app's window that has the keyboard, for the overlay to take it from.
func runForeground() {
	mygo.App.WhenReady(func() {
		var w *mygo.Window
		w = mygo.NewWindow(mygo.WindowOptions{Title: "spike foreground", Width: 360, Height: 200, X: 40, Y: 40, Content: ui.View(func(c *ui.Context) {
			ui.Box(c).Fill().Focusable().AutoFocus().HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind == ui.InputKeyDown {
					logf("foreground app got key %v", ev.Key)
				}
				return false
			}).Children(func() { ui.Text(c, "Another app") })
		})})
		w.Focus()
		if *grab != "" {
			logf("foreground app holds %s: %v", *grab, mygo.GlobalShortcut.Register(*grab, func() {}))
		}
	})
}
