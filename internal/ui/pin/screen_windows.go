package pin

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
	pRect   = user32.NewProc("GetWindowRect")
	pDpi    = shcore.NewProc("GetDpiForMonitor")
)

type rect32 struct{ Left, Top, Right, Bottom int32 }

func (r rect32) rect() platform.Rect {
	return platform.Rect{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
}

// workAreaAtCursor is the work area of the display under the pointer in physical pixels, and
// its scale.
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
	pDpi.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy)))
	return info.Work.rect(), float64(dx) / 96
}

// bounds is the window's rectangle in physical pixels.
func bounds(win *mygo.Window) (r platform.Rect) {
	hwnd := win.NativeHandle()
	mygo.RunOnMain(func() {
		var w rect32
		pRect.Call(hwnd, uintptr(unsafe.Pointer(&w)))
		r = w.rect()
	})
	return r
}

// setBounds moves and sizes the window in physical pixels; MyGo's geometry is whole DIPs.
func setBounds(win *mygo.Window, r platform.Rect) {
	const noZOrder, noActivate = 0x0004, 0x0010
	hwnd := win.NativeHandle()
	mygo.RunOnMain(func() {
		pPos.Call(hwnd, 0, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), noZOrder|noActivate)
	})
}

func cursor() (int, int) { return platform.CursorPos() }
