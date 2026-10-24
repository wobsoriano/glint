package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

var grab = flag.String("grab", "", "foreground mode: hold this global shortcut, as another app would")

// markPNG renders the Glint mark at size pixels, for the tray.
func markPNG(size int) []byte {
	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Image(c, theme.Mark(false)).Size(float32(size), float32(size))
	}, size, size, 1)
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

type trayProbe struct {
	clicks   chan time.Time
	menu     chan string
	second   chan string
	hotkey   chan string
	tray     *mygo.Tray
	menuRoot *mygo.Menu
}

func runTray() {
	if !mygo.App.RequestSingleInstanceLock() {
		fmt.Println("second instance: forwarded", os.Args[1:], "and quit")
		os.Exit(0)
	}
	p := &trayProbe{clicks: make(chan time.Time, 16), menu: make(chan string, 16), second: make(chan string, 4), hotkey: make(chan string, 4)}
	mygo.App.OnSecondInstance(func(args []string, wd string) {
		logf("second instance: args %q in %s", args, wd)
		p.second <- strings.Join(args, " ")
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		item := func(label string) *mygo.MenuItem {
			return &mygo.MenuItem{Label: label, Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				logf("menu: %s (checked %v)", it.Label, it.Checked)
				p.menu <- fmt.Sprintf("%s=%v", it.Label, it.Checked)
			}}
		}
		check := func(label string, on bool) *mygo.MenuItem {
			it := item(label)
			it.Type, it.Checked = mygo.MenuItemCheckbox, on
			return it
		}
		radio := func(label string, on bool) *mygo.MenuItem {
			it := item(label)
			it.Type, it.Checked = mygo.MenuItemRadio, on
			return it
		}
		p.menuRoot = mygo.NewMenu([]*mygo.MenuItem{
			{Label: "Capture Area", Accelerator: "Ctrl+Shift+4", Click: item("Capture Area").Click},
			{Label: "After capture", Submenu: []*mygo.MenuItem{
				check("Show preview", true),
				check("Copy to clipboard", false),
				mygo.Separator(),
				radio("PNG", true),
				radio("JPEG", false),
			}},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		})
		var err error
		p.tray, err = mygo.NewTray(mygo.TrayOptions{ID: "glint-spike", Icon: markPNG(32), ToolTip: "Glint spike", Menu: p.menuRoot})
		logf("NewTray: %v", err)
		if err != nil {
			return
		}
		p.tray.OnClick(func() { logf("tray: click"); p.clicks <- time.Now() })
		p.tray.OnRightClick(func() { logf("tray: right click") })
		logf("windows open: %d", len(mygo.Windows()))
		if *auto {
			go driveTray(p)
		}
	})
}

func driveTray(p *trayProbe) {
	failures := 0
	defer finish(&failures)
	saved := cursorPos()
	defer moveTo(saved.X, saved.Y)
	defer func() {
		time.Sleep(300 * time.Millisecond)
		if windowText(foreground()) == "System tray overflow window." {
			keys(vkEscape)
		}
	}()

	err := mygo.GlobalShortcut.Register("Ctrl+Shift+4", func() { p.hotkey <- "Ctrl+Shift+4" })
	check(&failures, err == nil, "Register Ctrl+Shift+4: %v", err)
	err = mygo.GlobalShortcut.Register("Ctrl+Shift+4", func() {})
	check(&failures, err != nil, "registering it twice fails: %v", err)
	keys(vkControl, vkShift, '4')
	check(&failures, waitString(p.hotkey, time.Second) == "Ctrl+Shift+4", "pressing Ctrl+Shift+4 runs the shortcut")
	mygo.GlobalShortcut.Unregister("Ctrl+Shift+4")
	check(&failures, !mygo.GlobalShortcut.IsRegistered("Ctrl+Shift+4"), "Unregister: IsRegistered is false")
	keys(vkControl, vkShift, '4')
	check(&failures, waitString(p.hotkey, 400*time.Millisecond) == "", "after Unregister the keys do nothing")
	err = mygo.GlobalShortcut.Register("Ctrl+Shift+Bogus", func() {})
	check(&failures, err != nil, "a malformed accelerator fails: %v", err)

	other := exec.Command(os.Args[0], "-mode", "foreground", "-grab", "Ctrl+Shift+F9")
	other.Stdout = os.Stdout
	other.Start()
	time.Sleep(1500 * time.Millisecond)
	err = mygo.GlobalShortcut.Register("Ctrl+Shift+F9", func() {})
	check(&failures, err != nil, "a shortcut another app holds fails: %v", err)
	other.Process.Kill()

	second := exec.Command(os.Args[0], "-mode", "tray", "--", "--capture", "area")
	outp, _ := second.CombinedOutput()
	got := waitString(p.second, 2*time.Second)
	check(&failures, strings.Contains(got, "--capture area"), "a second instance forwards its arguments (%q) and exits (%q)", got, strings.TrimSpace(string(outp)))

	n := 0
	mygo.RunOnMain(func() { n = len(mygo.Windows()) })
	check(&failures, n == 0, "the app runs with %d windows", n)

	b := p.tray.Bounds()
	logf("tray bounds %+v", b)
	if b.Width == 0 {
		check(&failures, false, "the tray icon has bounds (it may be in the overflow)")
		return
	}
	chevX, chevY := int32(b.X+b.Width/2), int32(b.Y+b.Height/2)
	cx, cy := chevX, chevY
	logf("under the icon's bounds: %s", rootClass(point{cx, cy}))
	// Windows 11 puts a new icon in the overflow, and Bounds is then the chevron's: open it and
	// find the mark's orange streaks in the flyout.
	overflow := func() (int32, int32, bool) {
		click(chevX, chevY)
		time.Sleep(700 * time.Millisecond)
		area := rect{chevX - 300, chevY - 400, chevX + 300, int32(b.Y)}
		shot := captureScreen(area)
		var sx, sy, n int
		for y := 0; y < shot.Bounds().Dy(); y++ {
			for x := 0; x < shot.Bounds().Dx(); x++ {
				c := shot.RGBAAt(x, y)
				if c.R > 230 && c.G > 110 && c.G < 215 && c.B < 140 {
					sx, sy, n = sx+x, sy+y, n+1
				}
			}
		}
		if n < 4 {
			return 0, 0, false
		}
		return area.Left + int32(sx/n), area.Top + int32(sy/n), true
	}
	// The flyout may still be open from the last click, and a click on the chevron then closes it.
	reopen := func() (int32, int32, bool) {
		if x, y, ok := overflow(); ok {
			return x, y, ok
		}
		return overflow()
	}
	ix, iy, found := overflow()
	check(&failures, found, "the icon shows in the overflow flyout at %d,%d", ix, iy)
	if !found {
		keys(vkEscape)
		return
	}
	cx, cy = ix, iy
	click(cx, cy)
	check(&failures, !waitTime(p.clicks, time.Second).IsZero(), "a left click reaches OnClick")
	time.Sleep(600 * time.Millisecond)
	drainTimes(p.clicks)
	if _, _, ok := reopen(); ok {
		click(cx, cy)
		time.Sleep(40 * time.Millisecond)
		click(cx, cy)
		time.Sleep(700 * time.Millisecond)
		ts := drainTimes(p.clicks)
		dbl, _, _ := pGetDoubleClickTime.Call()
		logf("a double click in the overflow: %d OnClick %v (double-click time %d ms)", len(ts), ts, dbl)
		if len(ts) == 0 {
			keys(vkEscape)
		}
	}

	if _, _, ok := reopen(); !ok {
		check(&failures, false, "the overflow opens again for the right click")
		keys(vkEscape)
		return
	}
	rightClick(cx, cy)
	menu := waitMenu(true)
	check(&failures, menu != 0, "a right click opens the menu")
	if menu != 0 {
		r := windowRect(menu)
		shot := captureScreen(rect{r.Left - 10, r.Top - 10, r.Right + 220, r.Bottom + 10})
		keys(vkDown)
		keys(vkDown)
		keys(vkRight)
		time.Sleep(700 * time.Millisecond)
		shot = captureScreen(r)
		savePNG(out("tray-menu.png"), shot)
		keys(vkDown)
		keys(vkReturn)
		got := waitString(p.menu, time.Second)
		check(&failures, got == "Copy to clipboard=true" || got == "Show preview=false", "a checkbox of the submenu flips Checked before Click runs (%q)", got)
		check(&failures, waitMenu(false) == 0, "the menu closed")
	}
}

func waitTime(ch chan time.Time, d time.Duration) time.Time {
	select {
	case t := <-ch:
		return t
	case <-time.After(d):
		return time.Time{}
	}
}

func drainTimes(ch chan time.Time) []time.Time {
	var out []time.Time
	for {
		select {
		case t := <-ch:
			out = append(out, t)
		default:
			return out
		}
	}
}

// waitMenu waits for a popup menu (#32768) to show, or to go.
func waitMenu(show bool) uintptr {
	cls, _ := syscall.UTF16PtrFromString("#32768")
	for i := 0; i < 40; i++ {
		h, _, _ := pFindWindowW.Call(uintptr(unsafe.Pointer(cls)), 0)
		visible := false
		if h != 0 {
			v, _, _ := pIsWindowVisible.Call(h)
			visible = v != 0
		}
		if visible == show {
			if visible {
				return h
			}
			return 0
		}
		time.Sleep(50 * time.Millisecond)
	}
	if show {
		return 0
	}
	return 1
}
