package main

import (
	"cmp"
	"image"
	"os"
	"os/exec"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// runOutline is the dashed outline around an area being recorded: a transparent window that
// clicks pass through and that screen capture leaves out.
func runOutline() {
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		area := image.Rect(500, 300, 1100, 700)
		const pad = 4
		view := ui.View(func(c *ui.Context) {
			c.Root().Background(ui.Transparent)
			ui.Box(c).Fill().Border(2, ui.RGB(0xE8, 0x11, 0x23)).BorderStyle(ui.BorderDashed).PassThrough()
		})
		bounds := rect{int32(area.Min.X - pad), int32(area.Min.Y - pad), int32(area.Max.X + pad), int32(area.Max.Y + pad)}
		var win *mygo.Window
		if cmp.Or(*variant, "flyout") == "flyout" {
			// A flyout has no redirection bitmap, so what the view leaves transparent shows what
			// is behind; a plain Transparent window keeps its bitmap and shows black there.
			owner := mygo.NewWindow(mygo.WindowOptions{Title: "Glint owner", Width: 1, Height: 1, Frameless: true, SkipTaskbar: true, Hidden: true})
			win = mygo.NewFlyout(mygo.FlyoutOptions{Parent: owner, Width: int(bounds.w()), Height: int(bounds.h()), Hidden: true, Content: view})
		} else {
			win = mygo.NewWindow(mygo.WindowOptions{
				Title: "Glint outline", X: int(bounds.Left), Y: int(bounds.Top), Width: int(bounds.w()), Height: int(bounds.h()),
				Frameless: true, Transparent: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true, DisableResize: true, DisableShadow: true,
				Content: view,
			})
		}
		placePhysical(win.NativeHandle(), bounds)
		win.SetIgnoreMouseEvents(true)
		logf("outline %s: exstyle %#x", cmp.Or(*variant, "flyout"), exStyle(win.NativeHandle()))
		if !*auto {
			win.ShowInactive()
			return
		}
		go func() {
			failures := 0
			defer finish(&failures)
			fg := exec.Command(os.Args[0], "-mode", "foreground")
			fg.Start()
			defer fg.Process.Kill()
			other := waitForeground("spike foreground")
			pSetWindowPos.Call(other, 0, uintptr(area.Min.X-60), uintptr(area.Min.Y-60), uintptr(area.Dx()+120), uintptr(area.Dy()+120), swpNoZOrder)
			hwnd := win.NativeHandle()
			mygo.RunOnMain(func() { win.ShowInactive() })
			time.Sleep(500 * time.Millisecond)
			r := windowRect(hwnd)
			shot := captureScreen(r)
			savePNG(out("outline.png"), shot)
			red := 0
			for x := 0; x < int(r.w()); x++ {
				if c := shot.RGBAAt(x, 1); c.R > 180 && c.G < 80 {
					red++
				}
			}
			check(&failures, red > int(r.w())/4 && red < int(r.w())*3/4, "the dashed outline draws: %d of %d pixels of the top edge are red", red, r.w())
			mid := shot.RGBAAt(int(r.w())/2, int(r.h())/2)
			check(&failures, mid.G > 120 && mid.R < 40, "the transparent inside shows the app behind it in a GDI capture (%v, the other app is green)", mid)
			under, _, _ := pWindowFromPoint.Call(uintptr(int64(area.Min.Y+150)<<32 | int64(area.Min.X+200)))
			root, _, _ := pGetAncestor.Call(under, 2)
			check(&failures, root == other && windowText(foreground()) == "spike foreground", "clicks inside the outline reach the app behind it (%q)", windowText(root))
			onEdge, _, _ := pWindowFromPoint.Call(uintptr(int64(r.Top+1)<<32 | int64(r.Left+int32(r.w())/2)))
			onEdge, _, _ = pGetAncestor.Call(onEdge, 2)
			logf("window under the outline's own border: %q (WS_EX_TRANSPARENT %v)", windowText(onEdge), exStyle(hwnd)&0x20 != 0)
			mygo.RunOnMain(func() { win.SetContentProtection(true) })
			time.Sleep(200 * time.Millisecond)
			hidden := captureScreen(r)
			red = 0
			for x := 0; x < int(r.w()); x++ {
				if c := hidden.RGBAAt(x, 1); c.R > 180 && c.G < 80 {
					red++
				}
			}
			check(&failures, red == 0, "with content protection a recording leaves the outline out (%d red pixels)", red)
		}()
	})
}
