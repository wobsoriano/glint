package main

import (
	"image"
	"image/color"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// runProtect shows a magenta window with SetContentProtection(true) and captures the screen with
// GDI: magenta means it is captured, black that it is masked (WDA_MONITOR), and what is behind
// it, a green window of the spike, that it is left out (WDA_EXCLUDEFROMCAPTURE).
func runProtect() {
	mygo.App.WhenReady(func() {
		magenta := ui.RGB(255, 0, 255)
		mk := func(title string, x int, panel bool) *mygo.Window {
			return mygo.NewWindow(mygo.WindowOptions{
				Title: title, X: x, Y: 200, Width: 300, Height: 200, Frameless: true, AlwaysOnTop: true,
				SkipTaskbar: true, Panel: panel, Hidden: true,
				Content: ui.View(func(c *ui.Context) { c.Root().Background(magenta) }),
			})
		}
		plain, panel := mk("protect plain", 300, false), mk("protect panel", 700, true)
		backdrop := mygo.NewWindow(mygo.WindowOptions{Title: "protect backdrop", X: 200, Y: 120, Width: 900, Height: 360, Frameless: true, SkipTaskbar: true, Hidden: true,
			Content: ui.View(func(c *ui.Context) { c.Root().Background(ui.RGB(0, 160, 0)) })})
		if !*auto {
			plain.SetContentProtection(true)
			plain.Show()
			return
		}
		go func() {
			failures := 0
			defer finish(&failures)
			mygo.RunOnMain(func() { backdrop.ShowInactive() })
			for _, w := range []*mygo.Window{plain, panel} {
				hwnd := w.NativeHandle()
				mygo.RunOnMain(func() { w.ShowInactive() })
				time.Sleep(400 * time.Millisecond)
				r := clientRect(hwnd)
				inner := rect{r.Left + 20, r.Top + 20, r.Right - 20, r.Bottom - 20}
				before := classify(captureScreen(inner))
				check(&failures, before == "captured", "%s unprotected: GDI capture shows it (%s)", windowText(hwnd), before)
				mygo.RunOnMain(func() { w.SetContentProtection(true) })
				time.Sleep(300 * time.Millisecond)
				aff := displayAffinity(hwnd)
				shot := captureScreen(rect{r.Left - 40, r.Top - 40, r.Right + 40, r.Bottom + 40})
				after := classify(crop(shot, image.Rect(60, 60, int(r.w())+20, int(r.h())+20)))
				check(&failures, aff == 0x11 && after == "backdrop", "%s protected: affinity %#x (0x11 WDA_EXCLUDEFROMCAPTURE, 0x1 WDA_MONITOR); GDI capture shows %s", windowText(hwnd), aff, after)
				savePNG(out("protect-"+map[bool]string{false: "plain", true: "panel"}[w.IsPanel()]+".png"), shot)
				mygo.RunOnMain(func() { w.SetContentProtection(false); w.Hide() })
			}
		}()
	})
}

// classify says what a capture of the magenta window's inside shows.
func classify(img *image.RGBA) string {
	b := img.Bounds()
	magenta, black, green := 0, 0, 0
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			switch c := img.RGBAAt(x, y); c {
			case color.RGBA{255, 0, 255, 255}:
				magenta++
			case color.RGBA{0, 160, 0, 255}:
				green++
			case color.RGBA{0, 0, 0, 255}:
				black++
			}
		}
	}
	n := b.Dx() * b.Dy()
	switch {
	case magenta == n:
		return "captured"
	case black == n:
		return "black"
	case green == n:
		return "backdrop"
	case magenta == 0:
		return "left out"
	}
	return "mixed"
}
