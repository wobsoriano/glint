package main

import (
	"os"
	"os/exec"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// toast is Glint's transient message: a pill near the bottom centre of the work area that shows
// without taking the keyboard and fades out, here by animating the window's opacity.
type toast struct {
	win  *mygo.Window
	hwnd uintptr
	msg  string
}

func showToast(msg string) *toast {
	t := &toast{msg: msg}
	const w, h = 320, 50
	work := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	t.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Glint toast", Width: w, Height: h, X: work.X + (work.Width-w)/2, Y: work.Y + work.Height - h - 24,
		Frameless: true, Transparent: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true, DisableResize: true, DisableShadow: true,
		Content: ui.View(func(c *ui.Context) {
			p := theme.Apply(c)
			c.Root().Background(ui.Transparent)
			ui.Row(c).Fill().Margin(6).Padding(0, 16).Gap(8).Radius(theme.RadiusPill).Background(p.Flyout).Border(1, p.Border).
				Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.25)).Children(func() {
				ui.Icon(c, theme.Icon(theme.IconSparkles)).Size(15, 15).TextColor(p.Accent)
				ui.Text(c, t.msg).SingleLine()
			})
		}),
	})
	t.hwnd = t.win.NativeHandle()
	t.win.ShowInactive()
	return t
}

// fade lowers the window's opacity to 0 over d and destroys it, returning the opacity steps set.
func (t *toast) fade(d time.Duration) int {
	t0, steps := time.Now(), 0
	for time.Since(t0) < d {
		t.win.SetOpacity(1 - float64(time.Since(t0))/float64(d))
		steps++
		time.Sleep(8 * time.Millisecond)
	}
	t.win.Destroy()
	return steps
}

func runToast() {
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		if !*auto {
			t := showToast("Copied to clipboard")
			go func() { time.Sleep(2 * time.Second); t.fade(300 * time.Millisecond); mygo.App.Quit() }()
			return
		}
		go func() {
			failures := 0
			defer finish(&failures)
			fg := exec.Command(os.Args[0], "-mode", "foreground")
			fg.Start()
			defer fg.Process.Kill()
			other := waitForeground("spike foreground")
			var t *toast
			mygo.RunOnMain(func() { t = showToast("Copied to clipboard") })
			time.Sleep(400 * time.Millisecond)
			check(&failures, foreground() == other && other != 0, "the toast leaves the other app foreground")
			r := frameRect(t.hwnd)
			full := captureScreen(r)
			savePNG(out("toast.png"), full)
			done := make(chan int)
			go func() { done <- t.fade(300 * time.Millisecond) }()
			time.Sleep(150 * time.Millisecond)
			mid := captureScreen(r)
			steps := <-done
			time.Sleep(100 * time.Millisecond)
			after := captureScreen(r)
			c := full.RGBAAt(int(r.w())/2, int(r.h())/2)
			m := mid.RGBAAt(int(r.w())/2, int(r.h())/2)
			a := after.RGBAAt(int(r.w())/2, int(r.h())/2)
			logf("toast centre pixel: shown %v, mid-fade %v, gone %v; %d opacity steps in 300 ms", c, m, a, steps)
			check(&failures, m != c && m != a, "the fade passes through a blend of the toast and what is behind it")
			alive := 0
			mygo.RunOnMain(func() { alive = len(mygo.Windows()) })
			check(&failures, alive == 0, "the toast window is gone after the fade (%d windows)", alive)
		}()
	})
}
