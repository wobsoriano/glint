package main

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	cardW, cardH = 224, 148
	cardGap      = 12
	cardEdge     = 12
)

type card struct {
	id      int
	path    string
	thumb   *ui.Bitmap
	clicked string
	result  *transfer.Result
}

// stack is the window of preview cards in the bottom-left corner of the work area.
type stack struct {
	win      *mygo.Window
	hwnd     uintptr
	cards    []*card
	onResult func(*card, transfer.Result)
	onClick  func(*card, string)
}

func (s *stack) view(c *ui.Context) {
	p := theme.Apply(c)
	c.Root().Background(ui.Transparent)
	ui.Column(c).Fill().Padding(10).Gap(cardGap).Justify(ui.End).Children(func() {
		for _, cd := range s.cards {
			s.card(c.Key(cd.id), cd, p)
		}
	})
}

func (s *stack) card(c *ui.Context, cd *card, p theme.Palette) {
	files, err := transfer.FileData(cd.path)
	if err != nil {
		logf("FileData: %v", err)
	}
	box := ui.Box(c).Size(cardW, cardH).Radius(theme.RadiusWindow).Clip().Border(1, p.Border).
		Background(p.Canvas).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.35))
	box.DragData(files, transfer.DragOptions{Done: func(r transfer.Result) {
		cd.result = &r
		if s.onResult != nil {
			s.onResult(cd, r)
		}
	}})
	hovered := box.Hovered()
	box.Children(func() {
		ui.Image(c, cd.thumb).Fill().Fit(ui.Cover)
		if !hovered {
			return
		}
		ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(ui.RGBA(0, 0, 0, 0.35)).PassThrough()
		ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(8).Children(func() {
			for _, label := range []string{"Copy", "Save"} {
				b := ui.ButtonBase(c.Key(label)).Width(96).Height(30).Radius(theme.RadiusPill).Center().
					Background(ui.RGBA(255, 255, 255, 0.92)).TextColor(ui.RGB(0x1B, 0x1B, 0x1B))
				if b.Hovered() {
					b.Background(ui.RGB(255, 255, 255))
				}
				b.Children(func() { ui.Text(c, label).FontSize(13).Bold() })
				if b.Clicked() {
					s.click(cd, label)
				}
			}
		})
		corners := []struct {
			icon      theme.IconName
			name      string
			top, left bool
		}{{theme.IconClose, "close", true, true}, {theme.IconEdit, "edit", true, false}, {theme.IconPin, "pin", false, true}, {theme.IconMore, "more", false, false}}
		for _, k := range corners {
			b := ui.ButtonBase(c.Key(k.name)).Absolute().Size(26, 26).Radius(13).Center().
				Background(ui.RGBA(0, 0, 0, 0.55)).TextColor(ui.RGB(255, 255, 255))
			if k.top {
				b.Top(6)
			} else {
				b.Bottom(6)
			}
			if k.left {
				b.Left(6)
			} else {
				b.Right(6)
			}
			b.Children(func() { ui.Icon(c, theme.Icon(k.icon)).Size(14, 14) })
			if b.Clicked() {
				s.click(cd, k.name)
			}
		}
	})
}

func (s *stack) click(cd *card, what string) {
	cd.clicked = what
	logf("card %d: clicked %s", cd.id, what)
	if s.onClick != nil {
		s.onClick(cd, what)
	}
}

func (s *stack) size() (int, int) {
	return cardW + 20, len(s.cards)*(cardH+cardGap) - cardGap + 20
}

// slideOut moves the window off the left edge of the screen over d, from a goroutine.
func (s *stack) slideOut(d time.Duration) (steps int, last int) {
	x0, y := s.win.Position()
	w, _ := s.win.Size()
	t0 := time.Now()
	for {
		t := float64(time.Since(t0)) / float64(d)
		if t >= 1 {
			break
		}
		ease := 1 - (1-t)*(1-t)*(1-t)
		s.win.SetPosition(x0+int(float64(-w-20-x0)*ease), y)
		steps++
		time.Sleep(7 * time.Millisecond)
	}
	s.win.SetPosition(-w-20, y)
	last, _ = s.win.Position()
	return steps, last
}

func newStack(cards []*card, noActivate bool) *stack {
	s := &stack{cards: cards}
	w, h := s.size()
	work := mygo.Screen.PrimaryDisplay().WorkArea
	s.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "Glint cards",
		X:             work.X + cardEdge - 10,
		Y:             work.Y + work.Height - h - cardEdge + 10,
		Width:         w,
		Height:        h,
		Frameless:     true,
		Transparent:   true,
		AlwaysOnTop:   true,
		SkipTaskbar:   true,
		Hidden:        true,
		DisableResize: true,
		DisableShadow: true,
		Content:       ui.View(s.view),
	})
	s.hwnd = s.win.NativeHandle()
	if noActivate {
		const gwlExStyle, wsExNoActivate = ^uintptr(19), 0x08000000
		pSetWindowLongPtrW.Call(s.hwnd, gwlExStyle, exStyle(s.hwnd)|wsExNoActivate)
	}
	return s
}

func runCards() {
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		dir, err := os.MkdirTemp("", "glint-spike-cards")
		if err != nil {
			logf("temp dir: %v", err)
			return
		}
		var cards []*card
		for i := 1; i <= 2; i++ {
			img := fakeDesktop(1280, 800)
			path := filepath.Join(dir, fmt.Sprintf("Glint spike %d.png", i))
			savePNG(path, img)
			cards = append(cards, &card{id: i, path: path, thumb: ui.NewBitmap(img)})
		}
		if !*auto {
			s := newStack(cards, false)
			s.win.ShowInactive()
			return
		}
		go driveCards(cards, dir)
	})
}

func driveCards(cards []*card, dir string) {
	failures := 0
	defer finish(&failures)
	saved := cursorPos()
	defer moveTo(saved.X, saved.Y)

	fg := exec.Command(os.Args[0], "-mode", "foreground")
	fg.Stdout = os.Stdout
	if err := fg.Start(); err != nil {
		check(&failures, false, "start the foreground app: %v", err)
		return
	}
	defer fg.Process.Kill()
	other := waitForeground("spike foreground")
	check(&failures, other != 0, "another app has the keyboard")

	var s *stack
	mygo.RunOnMain(func() { s = newStack(cards, false); s.win.ShowInactive() })
	time.Sleep(400 * time.Millisecond)
	check(&failures, foreground() == other, "ShowInactive leaves the other app foreground (%q)", windowText(foreground()))
	wr := windowRect(s.hwnd)
	logf("stack window %v exstyle %#x corner pref %d", wr, exStyle(s.hwnd), cornerPreference(s.hwnd))
	first := image.Rect(int(wr.Left)+10, int(wr.Top)+10, int(wr.Left)+10+cardW, int(wr.Top)+10+cardH)
	moveTo(int32(first.Max.X+150), int32(first.Min.Y-100))
	time.Sleep(200 * time.Millisecond)
	before := captureScreen(rectOf(first))
	moveTo(int32(first.Min.X+cardW/2), int32(first.Min.Y+20))
	time.Sleep(300 * time.Millisecond)
	after := captureScreen(rectOf(first))
	n, _ := diff(before, after, 8)
	check(&failures, n > 2000, "hovering an inactive card reveals its buttons (%d pixels changed)", n)
	savePNG(out("card-hover.png"), after)
	check(&failures, foreground() == other, "hovering leaves the other app foreground")

	clicks := make(chan string, 8)
	mygo.RunOnMain(func() { s.onClick = func(_ *card, what string) { clicks <- what } })
	click(int32(first.Min.X+cardW/2), int32(first.Min.Y+cardH/2-19))
	got := waitString(clicks, time.Second)
	check(&failures, got == "Copy", "the Copy pill takes a click (%q)", got)
	logf("after clicking the card, foreground is %q (stack %v)", windowText(foreground()), foreground() == s.hwnd)

	mygo.RunOnMain(func() { s.win.Destroy() })
	pSetForegroundWindow.Call(other)
	time.Sleep(200 * time.Millisecond)
	mygo.RunOnMain(func() { s = newStack(cards, true); s.win.ShowInactive() })
	mygo.RunOnMain(func() { s.onClick = func(_ *card, what string) { clicks <- what } })
	time.Sleep(400 * time.Millisecond)
	wr = windowRect(s.hwnd)
	first = image.Rect(int(wr.Left)+10, int(wr.Top)+10, int(wr.Left)+10+cardW, int(wr.Top)+10+cardH)
	moveTo(int32(first.Min.X+cardW/2), int32(first.Min.Y+20))
	time.Sleep(300 * time.Millisecond)
	click(int32(first.Min.X+cardW/2), int32(first.Min.Y+cardH/2-19))
	got = waitString(clicks, time.Second)
	check(&failures, got == "Copy" && foreground() == other, "with WS_EX_NOACTIVATE the click lands (%q) and the other app stays foreground (%q)", got, windowText(foreground()))

	dest := filepath.Join(dir, fmt.Sprintf("glint-drop-%d", os.Getpid()))
	os.MkdirAll(dest, 0o755)
	explorer := openExplorer(dest)
	check(&failures, explorer != 0, "an Explorer window shows %s", dest)
	if explorer != 0 {
		target := rect{760, 160, 1660, 760}
		pSetWindowPos.Call(explorer, 0, uintptr(target.Left), uintptr(target.Top), uintptr(target.w()), uintptr(target.h()), 0)
		pSetForegroundWindow.Call(explorer)
		time.Sleep(700 * time.Millisecond)
		results := make(chan transfer.Result, 2)
		mygo.RunOnMain(func() { s.onResult = func(_ *card, r transfer.Result) { results <- r } })
		drag(point{int32(first.Min.X + 30), int32(first.Max.Y - 20)}, point{1210, 520}, 30, 15*time.Millisecond)
		var r transfer.Result
		select {
		case r = <-results:
		case <-time.After(3 * time.Second):
		}
		copied := waitFile(filepath.Join(dest, "Glint spike 1.png"), 3*time.Second)
		check(&failures, copied && r.Operation == transfer.Copy, "dragging the card into Explorer copies the PNG (file %v, result %+v)", copied, r)
		check(&failures, closeWindow(explorer), "the Explorer window closed")
	}

	results := make(chan transfer.Result, 2)
	mygo.RunOnMain(func() { s.onResult = func(_ *card, r transfer.Result) { results <- r } })
	drag(point{int32(first.Min.X + 60), int32(first.Max.Y - 20)}, point{0, int32(first.Max.Y - 20)}, 20, 15*time.Millisecond)
	var r transfer.Result
	select {
	case r = <-results:
	case <-time.After(3 * time.Second):
	}
	check(&failures, r.Canceled && !r.Escaped && r.Point.X <= 2, "a drag let go past the left edge over nothing reports Canceled, not Escaped, at %v", r.Point)

	steps, last := s.slideOut(220 * time.Millisecond)
	w, _ := s.win.Size()
	check(&failures, last+w <= 0, "the stack slides off-screen with SetPosition: %d steps in 220 ms, ends at x=%d (asked for %d)", steps, last, -w-20)
	x, _ := s.win.Position()
	wr = windowRect(s.hwnd)
	logf("after the slide: Position x=%d, window rect %v", x, wr)
}

func rectOf(r image.Rectangle) rect {
	return rect{int32(r.Min.X), int32(r.Min.Y), int32(r.Max.X), int32(r.Max.Y)}
}

func waitForeground(title string) uintptr {
	for i := 0; i < 100; i++ {
		if h := foreground(); windowText(h) == title {
			return h
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0
}

func waitString(ch chan string, d time.Duration) string {
	select {
	case s := <-ch:
		return s
	case <-time.After(d):
		return ""
	}
}

func waitFile(path string, d time.Duration) bool {
	for t0 := time.Now(); time.Since(t0) < d; time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// openExplorer opens a folder window and returns it once it shows.
func openExplorer(dir string) uintptr {
	exec.Command("explorer.exe", dir).Start()
	name := filepath.Base(dir)
	for i := 0; i < 60; i++ {
		time.Sleep(100 * time.Millisecond)
		var found uintptr
		cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
			buf := make([]uint16, 64)
			pGetClassNameW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 64)
			if syscall.UTF16ToString(buf) == "CabinetWClass" && strings.HasPrefix(windowText(h), name) {
				found = h
				return 0
			}
			return 1
		})
		pEnumWindows.Call(cb, 0)
		if found != 0 {
			return found
		}
	}
	return 0
}

// closeWindow asks another app's window to close and reports whether it went.
func closeWindow(h uintptr) bool {
	const wmClose = 0x0010
	pPostMessageW.Call(h, wmClose, 0, 0)
	for i := 0; i < 40; i++ {
		time.Sleep(50 * time.Millisecond)
		if ok, _, _ := pIsWindow.Call(h); ok == 0 {
			return true
		}
	}
	return false
}
