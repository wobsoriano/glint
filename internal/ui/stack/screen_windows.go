package stack

import (
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"

	"github.com/wobsoriano/glint/internal/platform"
)

var (
	user32  = windows.NewLazySystemDLL("user32.dll")
	shcore  = windows.NewLazySystemDLL("shcore.dll")
	pCursor = user32.NewProc("GetCursorPos")
	pFromPt = user32.NewProc("MonitorFromPoint")
	pInfo   = user32.NewProc("GetMonitorInfoW")
	pPos    = user32.NewProc("SetWindowPos")
	pDpi    = shcore.NewProc("GetDpiForMonitor")
)

type rect32 struct{ Left, Top, Right, Bottom int32 }

// workAreaAtCursor is the work area of the display under the pointer in physical pixels, and
// its scale. MyGo's Display gives whole DIPs, which cannot name every pixel at 125% or 175%.
func workAreaAtCursor() (platform.Rect, float64) {
	var pt struct{ X, Y int32 }
	pCursor.Call(uintptr(unsafe.Pointer(&pt)))
	const defaultToNearest = 2
	mon, _, _ := pFromPt.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, defaultToNearest)
	info := struct {
		Size          uint32
		Monitor, Work rect32
		Flags         uint32
	}{Size: 40}
	pInfo.Call(mon, uintptr(unsafe.Pointer(&info)))
	dx, dy := uint32(96), uint32(96)
	const effectiveDPI = 0
	pDpi.Call(mon, effectiveDPI, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy)))
	w := info.Work
	return platform.Rect{X: int(w.Left), Y: int(w.Top), W: int(w.Right - w.Left), H: int(w.Bottom - w.Top)}, float64(dx) / 96
}

// place puts the window on r in physical pixels, topmost, without activating it. A flyout's
// own placement would keep it inside the work area and round through DIPs.
func place(win *mygo.Window, r platform.Rect) {
	const topmost, noActivate = ^uintptr(0), 0x0010
	hwnd := win.NativeHandle()
	mygo.RunOnMain(func() {
		pPos.Call(hwnd, topmost, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), noActivate)
	})
}
