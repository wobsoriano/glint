package toast

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32               = windows.NewLazySystemDLL("user32.dll")
	shcore               = windows.NewLazySystemDLL("shcore.dll")
	procGetCursorPos     = user32.NewProc("GetCursorPos")
	procMonitorFromPoint = user32.NewProc("MonitorFromPoint")
	procGetMonitorInfoW  = user32.NewProc("GetMonitorInfoW")
	procSetWindowPos     = user32.NewProc("SetWindowPos")
	procGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")
)

type rect struct{ Left, Top, Right, Bottom int32 }

type monitorInfo struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
}

// workAreaAtCursor is the work area of the display with the pointer, in physical pixels,
// and that display's scale.
func workAreaAtCursor() (rect, float64) {
	var pt struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	const defaultToNearest = 2
	mon, _, _ := procMonitorFromPoint.Call(uintptr(uint32(pt.X))|uintptr(uint32(pt.Y))<<32, defaultToNearest)
	info := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	procGetMonitorInfoW.Call(mon, uintptr(unsafe.Pointer(&info)))
	dx, dy := uint32(96), uint32(96)
	procGetDpiForMonitor.Call(mon, 0, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy)))
	return info.Work, float64(dx) / 96
}

// place puts the toast t.bottom DIPs above the bottom centre of the work area, in physical
// pixels: MyGo's own geometry is whole DIPs and rounds negative coordinates toward zero. The
// second call corrects the size after WM_DPICHANGED on a display of another scale.
func (t *Toast) place(w int) {
	work, scale := workAreaAtCursor()
	pw, ph := int32(float64(w)*scale), int32(float64(height)*scale)
	x := work.Left + (work.Right-work.Left-pw)/2
	y := work.Bottom - ph - int32(t.bottom*scale)
	const topmost = ^uintptr(0)
	const noActivate = 0x0010
	h := t.win.NativeHandle()
	for range 2 {
		procSetWindowPos.Call(h, topmost, uintptr(x), uintptr(y), uintptr(pw), uintptr(ph), noActivate)
	}
}
