// Command stackdemo shows the Quick Access stack with two screenshots and a recording in a
// Library under GLINT_DATA_DIR. With -auto it drives itself on the real desktop, checks what
// the stack does and writes screenshots to -out, then quits with 1 if a check failed. Run it
// inside the desktop lock:
//
//	go build -o %TEMP%\stackdemo.exe ./internal/ui/stack/cmd/stackdemo
//	set GLINT_DATA_DIR=%TEMP%\glint-stackdemo
//	go run ./tools/desklock -- %TEMP%\stackdemo.exe -auto
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/pin"
	"github.com/wobsoriano/glint/internal/ui/quicklook"
	"github.com/wobsoriano/glint/internal/ui/stack"
	"github.com/wobsoriano/glint/internal/ui/stack/drive"
)

var (
	auto       = flag.Bool("auto", false, "drive the stack, check it and quit")
	foreground = flag.Bool("foreground", false, "run as the other app that keeps the keyboard")
	outDir     = flag.String("out", filepath.Join(os.TempDir(), "glint-stackdemo-evidence"), "where screenshots go")
)

// host logs each call; by hand, Pin and Quick Look open the real surfaces.
type host struct {
	calls  chan string
	viewer *quicklook.Viewer
}

func (h *host) log(what string, item store.Item) error {
	fmt.Printf("host: %s %s\n", what, item.Meta.Title)
	select {
	case h.calls <- what + " " + item.Meta.Title:
	default:
	}
	return nil
}

func (h *host) Copy(i store.Item) error     { return h.log("copy", i) }
func (h *host) Compress(i store.Item) error { return h.log("compress", i) }
func (h *host) CopyText(i store.Item) error { return h.log("copy_text", i) }
func (h *host) Edit(i store.Item) error     { return h.log("edit", i) }
func (h *host) Delete(i store.Item) error   { return h.log("delete", i) }
func (h *host) Reveal(i store.Item) error   { return h.log("reveal", i) }

func (h *host) Save(i store.Item, choose bool) error {
	if choose {
		return h.log("save_as", i)
	}
	return h.log("save", i)
}

func (h *host) Pin(i store.Item) error {
	h.log("pin", i)
	if *auto {
		return nil
	}
	f, err := os.Open(i.Deliverable())
	if err != nil {
		return err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return err
	}
	pin.Open(img, 1)
	return nil
}

func (h *host) QuickLook(i store.Item) error {
	h.log("quick_look", i)
	if !*auto {
		h.viewer.Open(i)
	}
	return nil
}

// DragFile hard-links the deliverable under its title, as the Rust build does, so the file an
// app receives is named after the capture.
func (h *host) DragFile(i store.Item) (string, error) {
	dir := filepath.Join(settings.DataDir(), "Drag", i.ID())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, i.Meta.Title+filepath.Ext(i.Deliverable()))
	os.Remove(path)
	if err := os.Link(i.Deliverable(), path); err != nil {
		return "", err
	}
	return path, nil
}

func main() {
	flag.Parse()
	if *foreground {
		drive.RunForeground()
		if err := mygo.App.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if os.Getenv("GLINT_DATA_DIR") == "" {
		dir, err := os.MkdirTemp("", "glint-stackdemo")
		if err != nil {
			log.Fatal(err)
		}
		os.Setenv("GLINT_DATA_DIR", dir)
	}
	lib := store.New(store.DefaultRoot())
	now := time.Now()
	rec, err := drive.Recording(lib, 640, 360, 3, now)
	if err != nil {
		log.Fatal("recording: ", err)
	}
	shot1, err := drive.Screenshot(lib, "Demo shot one", 1280, 800, color.RGBA{0xE8, 0x6A, 0x2C, 255}, color.RGBA{0xF6, 0xC1, 0x45, 255}, now.Add(time.Second))
	if err != nil {
		log.Fatal("screenshot: ", err)
	}
	shot2, err := drive.Screenshot(lib, "Demo shot two", 1600, 900, color.RGBA{0x2C, 0x8A, 0x6A, 255}, color.RGBA{0x9A, 0xE6, 0xC8, 255}, now.Add(2*time.Second))
	if err != nil {
		log.Fatal("screenshot: ", err)
	}
	h := &host{calls: make(chan string, 16)}
	prefs := settings.Default().Overlay
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		h.viewer = quicklook.New(h)
		s := stack.New(h, prefs)
		if !*auto {
			for _, it := range []store.Item{rec, shot1, shot2} {
				s.Push(it)
			}
			return
		}
		go run(s, h, prefs, rec, shot1, shot2)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// geometry of the stack window in physical pixels, from its rectangle.
type geometry struct {
	r     platform.Rect
	scale float64
}

func (g geometry) px(dip float64) int { return int(dip*g.scale + 0.5) }

// card i counts from the top, the newest.
func (g geometry) card(i int) platform.Rect {
	y := 10 + 28 + 12 + float64(i)*(148+12)
	return platform.Rect{X: g.r.X + g.px(10), Y: g.r.Y + g.px(y), W: g.px(224), H: g.px(148)}
}

func geometryOf(s *stack.Stack) geometry {
	r := drive.WindowRect(s.Window().NativeHandle())
	return geometry{r: r, scale: float64(r.W) / 244}
}

func center(r platform.Rect) (int, int) { return r.X + r.W/2, r.Y + r.H/2 }

func waitCall(h *host, want string, d time.Duration) string {
	deadline := time.After(d)
	for {
		select {
		case got := <-h.calls:
			if strings.HasPrefix(got, want) {
				return got
			}
		case <-deadline:
			return ""
		}
	}
}

func drain(h *host) {
	for {
		select {
		case <-h.calls:
		default:
			return
		}
	}
}

func run(s *stack.Stack, h *host, prefs settings.OverlayPrefs, rec, shot1, shot2 store.Item) {
	l := drive.NewLog(*outDir)
	defer l.Finish()
	sx, sy := platform.CursorPos()
	defer drive.MoveTo(sx, sy)
	// Other workers' windows have covered the stack mid-run on the shared desktop; say so
	// when a click lands on one.
	clickStack := func(x, y int) {
		if w := drive.RootAt(x, y); w != s.Window().NativeHandle() {
			l.Printf("note: %q covers the stack at (%d,%d)", drive.WindowText(w), x, y)
		}
		drive.Click(x, y)
	}
	drive.MoveTo(960, 40)

	area, _ := workArea()
	corner := platform.Rect{X: area.X, Y: area.Y + area.H - 700, W: 300, H: 700}
	behind := drive.Capture(corner)
	fg, other := drive.StartForeground(l)
	if fg != nil {
		defer fg.Process.Kill()
	}
	l.Check(other != 0, "another app has the keyboard")

	for _, it := range []store.Item{rec, shot1, shot2} {
		s.Push(it)
	}
	time.Sleep(600 * time.Millisecond)
	l.Check(drive.Foreground() == other, "the stack shows without taking the keyboard (foreground %q)", drive.WindowText(drive.Foreground()))
	l.Check(s.Len() == 3, "three cards (%d)", s.Len())

	g := geometryOf(s)
	l.Printf("work area %+v, stack %+v, scale %.2f", area, g.r, g.scale)
	l.Check(g.r.X == area.X+g.px(12) && g.r.Y+g.r.H == area.Y+area.H-g.px(12), "the stack sits 12 DIPs in from the bottom-left corner of the work area (%+v)", g.r)
	gx, gy := g.r.X+g.r.W-g.px(5), g.r.Y+g.px(4)
	was, now := behind.RGBAAt(gx-corner.X, gy-corner.Y), drive.Capture(platform.Rect{X: gx, Y: gy, W: 1, H: 1}).RGBAAt(0, 0)
	l.Check(was == now, "the stack's padding shows what is behind it (%v, before %v)", now, was)
	l.Printf("the window under the stack's padding at (%d,%d) is %q (class of the stack's own: %v)", gx, gy, drive.WindowText(drive.RootAt(gx, gy)), drive.RootAt(gx, gy) == s.Window().NativeHandle())
	whole := g.r
	l.Shot("stack-left.png", platform.Rect{X: area.X, Y: whole.Y - 20, W: whole.W + 40, H: whole.H + 40})

	recCard := g.card(2)
	recShot := drive.Capture(recCard)
	cx, cy := recShot.Rect.Dx()/2, recShot.Rect.Dy()/2
	px := recShot.RGBAAt(cx/4, cy)
	l.Check(px.B > px.R+40, "the bottom card shows the recording's first frame (%v)", px)

	top := g.card(0)
	before := drive.Capture(top)
	drive.MoveTo(top.X+top.W/2, top.Y+12)
	time.Sleep(400 * time.Millisecond)
	after := l.Shot("card-hover.png", top)
	n := drive.Diff(before, after, 8)
	l.Check(n > 2000, "hovering the top card reveals its actions (%d pixels changed)", n)
	l.Check(drive.Foreground() == other, "hovering leaves the other app in front")

	tx, ty := center(top)
	drain(h)
	clickStack(tx, ty-g.px(19))
	l.Check(waitCall(h, "copy Demo shot two", 2*time.Second) != "", "the Copy pill in the center calls the host's Copy")
	time.Sleep(200 * time.Millisecond)
	l.Shot("card-copied.png", top)
	l.Check(drive.Foreground() == other, "clicking a card leaves the other app in front")
	time.Sleep(1500 * time.Millisecond)
	clickStack(tx, ty+g.px(19))
	l.Check(waitCall(h, "save Demo shot two", 2*time.Second) != "", "the Save pill calls the host's Save")
	time.Sleep(1500 * time.Millisecond)

	cornerAt := func(left, upper bool) (int, int) {
		x, y := top.X+g.px(6+14), top.Y+g.px(6+14)
		if !left {
			x = top.X + top.W - g.px(6+14)
		}
		if !upper {
			y = top.Y + top.H - g.px(6+14)
		}
		return x, y
	}
	clickStack(cornerAt(false, true))
	l.Check(waitCall(h, "edit Demo shot two", 2*time.Second) != "", "the top-right corner (Edit) calls the host's Edit")
	clickStack(cornerAt(false, false))
	time.Sleep(300 * time.Millisecond)
	l.Shot("card-more.png", top)
	clickStack(top.X+top.W/2, top.Y+g.px(6+4+11))
	l.Check(waitCall(h, "save_as Demo shot two", 2*time.Second) != "", "More opens the overflow menu, whose first row (Save As) calls the host")
	time.Sleep(1500 * time.Millisecond)

	layout := settings.DefaultCardLayout()
	layout.Place(settings.CardQuickLook, settings.CenterZone, 0)
	layout.Place(settings.CardCopyText, settings.CornerZone(settings.TopLeft), -1)
	prefs.Layout = layout
	s.SetPrefs(prefs)
	drive.MoveTo(top.X+top.W/2, top.Y+8)
	time.Sleep(400 * time.Millisecond)
	l.Shot("card-relayout.png", top)
	clickStack(tx, ty-g.px(38))
	l.Check(waitCall(h, "quick_look Demo shot two", 2*time.Second) != "", "after Settings > Overlay moves Quick Look to the center, its first pill calls Quick Look")
	clickStack(cornerAt(true, true))
	l.Check(waitCall(h, "copy_text Demo shot two", 2*time.Second) != "", "and the top-left corner, now Copy Text, calls Copy Text")
	prefs.Layout = settings.DefaultCardLayout()
	s.SetPrefs(prefs)
	time.Sleep(1500 * time.Millisecond)

	dest := filepath.Join(os.TempDir(), fmt.Sprintf("glint-drop-%d", os.Getpid()))
	os.MkdirAll(dest, 0o755)
	defer os.RemoveAll(dest)
	explorer := drive.OpenExplorer(dest)
	if l.Check(explorer != 0, "an Explorer window shows %s", dest) {
		drive.SetWindowRect(explorer, platform.Rect{X: 760, Y: 160, W: 900, H: 600})
		drive.SetForeground(explorer)
		time.Sleep(800 * time.Millisecond)
		l.Printf("before the drag: %q is under the card, %q under the drop point", drive.WindowText(drive.RootAt(top.X+g.px(30), top.Y+top.H-g.px(30))), drive.WindowText(drive.RootAt(1210, 520)))
		l.Shot("before-drag.png", platform.Rect{X: area.X, Y: area.Y + area.H/2, W: area.W, H: area.H / 2})
		drive.Drag(top.X+g.px(30), top.Y+top.H-g.px(30), 1210, 520, 30, 15*time.Millisecond)
		file := filepath.Join(dest, shot2.Meta.Title+".png")
		copied := drive.WaitFile(file, 4*time.Second)
		l.Check(copied, "dragging the top card into Explorer copies %s", file)
		if copied {
			l.Check(drive.Until(3*time.Second, func() bool { return sameSize(file, shot2.Deliverable()) }), "the dropped file is the capture's PNG")
		}
		l.Check(drive.Until(2*time.Second, func() bool { return s.Len() == 2 }), "the dropped card leaves the stack, as Dismiss after drag says (%d cards)", s.Len())
		time.Sleep(800 * time.Millisecond)
		l.Shot("explorer-drop.png", platform.Rect{X: 760, Y: 160, W: 900, H: 600})
		l.Check(drive.CloseWindow(explorer), "the Explorer window closed")
	}
	if r := drive.WindowRect(other); other != 0 {
		drive.Click(r.X+r.W/2, r.Y+r.H/2)
	}
	time.Sleep(300 * time.Millisecond)
	l.Check(drive.Foreground() == other, "a click gives the other app the keyboard back")

	// The drag ends over the demo's own window, which takes no files, not over the user's apps.
	drive.SetWindowRect(other, platform.Rect{X: area.X, Y: area.Y + area.H/3, W: 700, H: area.H * 2 / 3})
	time.Sleep(500 * time.Millisecond)
	g = geometryOf(s)
	top = g.card(0)
	drive.Drag(top.X+g.px(60), top.Y+top.H/2, area.X, top.Y+top.H/2, 20, 15*time.Millisecond)
	l.Check(drive.Until(2*time.Second, func() bool { return s.Len() == 1 }), "a card dragged past the left edge and let go there is dismissed (%d cards)", s.Len())
	l.Printf("foreground after the dismiss drag: %q", drive.WindowText(drive.Foreground()))

	prefs.AutoCloseSeconds = 2
	s.SetPrefs(prefs)
	drive.MoveTo(960, 40)
	s.Push(shot1)
	time.Sleep(300 * time.Millisecond)
	l.Check(s.Len() == 2, "a new card joins the recording (%d cards)", s.Len())
	t0 := time.Now()
	gone := drive.Until(4*time.Second, func() bool { return s.Len() == 1 })
	l.Check(gone && time.Since(t0) > 1500*time.Millisecond, "with auto close at 2 s the new card closes by itself after %.1fs, and the older card without a deadline stays", time.Since(t0).Seconds())

	s.Push(shot1)
	time.Sleep(300 * time.Millisecond)
	g = geometryOf(s)
	drive.MoveTo(center(g.card(0)))
	time.Sleep(3 * time.Second)
	l.Check(s.Len() == 2, "a card whose time is up stays while the pointer is over the stack (%d cards)", s.Len())
	drive.MoveTo(960, 40)
	t0 = time.Now()
	gone = drive.Until(4*time.Second, func() bool { return s.Len() == 1 })
	l.Check(gone && time.Since(t0) > 1500*time.Millisecond, "and closes %.1fs after the pointer leaves, the grace the Rust build gives", time.Since(t0).Seconds())

	prefs.Side = settings.SideRight
	s.SetPrefs(prefs)
	time.Sleep(400 * time.Millisecond)
	g = geometryOf(s)
	l.Check(g.r.X+g.r.W == area.X+area.W-g.px(12) && g.r.Y+g.r.H == area.Y+area.H-g.px(12), "switching the side to Right moves the stack to the bottom-right corner (%+v)", g.r)
	l.Shot("stack-right.png", platform.Rect{X: area.X + area.W - g.r.W - 40, Y: g.r.Y - 20, W: g.r.W + 40, H: g.r.H + 40})

	collapse := platform.Rect{X: g.r.X + g.r.W - g.px(10+26), Y: g.r.Y + g.px(10+1), W: g.px(26), H: g.px(26)}
	clickStack(center(collapse))
	time.Sleep(400 * time.Millisecond)
	tab := drive.WindowRect(s.Window().NativeHandle())
	l.Check(tab.W == g.px(30) && tab.H == g.px(84) && tab.X+tab.W == area.X+area.W, "the collapse button tucks the stack into a %dx%d peek tab flush with the right edge (%+v)", g.px(30), g.px(84), tab)
	l.Shot("peek-tab.png", platform.Rect{X: area.X + area.W - 200, Y: tab.Y - 40, W: 200, H: tab.H + 80})
	clickStack(center(tab))
	time.Sleep(400 * time.Millisecond)
	back := drive.WindowRect(s.Window().NativeHandle())
	l.Check(back == g.r, "clicking the tab brings the stack back (%+v)", back)
	l.Check(drive.Foreground() == other, "the other app kept the keyboard throughout (foreground %q)", drive.WindowText(drive.Foreground()))

	s.Clear()
	time.Sleep(300 * time.Millisecond)
	l.Check(!drive.Visible(s.Window().NativeHandle()), "Clear hides the stack")
}

func workArea() (platform.Rect, float64) {
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	wa := d.WorkArea
	k := d.ScaleFactor
	return platform.Rect{X: int(float64(wa.X) * k), Y: int(float64(wa.Y) * k), W: int(float64(wa.Width) * k), H: int(float64(wa.Height) * k)}, k
}

func sameSize(a, b string) bool {
	x, err1 := decodeSize(a)
	y, err2 := decodeSize(b)
	return err1 == nil && err2 == nil && x == y
}

func decodeSize(path string) (image.Point, error) {
	f, err := os.Open(path)
	if err != nil {
		return image.Point{}, err
	}
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	return image.Point{X: cfg.Width, Y: cfg.Height}, err
}
