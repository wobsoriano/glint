package record

import (
	"unsafe"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"

	"github.com/wobsoriano/glint/internal/platform"
)

var (
	user32       = windows.NewLazySystemDLL("user32.dll")
	dwmapi       = windows.NewLazySystemDLL("dwmapi.dll")
	pSetPos      = user32.NewProc("SetWindowPos")
	pMonitorInfo = user32.NewProc("GetMonitorInfoW")
	pIsWindow    = user32.NewProc("IsWindow")
	pIsIconic    = user32.NewProc("IsIconic")
	pDwmGet      = dwmapi.NewProc("DwmGetWindowAttribute")
	pDwmSet      = dwmapi.NewProc("DwmSetWindowAttribute")
)

type rect32 struct{ Left, Top, Right, Bottom int32 }

func (r rect32) rect() platform.Rect {
	return platform.Rect{X: int(r.Left), Y: int(r.Top), W: int(r.Right - r.Left), H: int(r.Bottom - r.Top)}
}

// place puts the window on r in physical pixels, topmost, without activating it. MyGo's
// geometry is whole DIPs converted with the window's current DPI, which cannot name every
// pixel of a scaled display, and a flyout's own placement keeps it inside the work area.
func place(win *mygo.Window, r platform.Rect) {
	const topmost, noActivate = ^uintptr(0), 0x0010
	hwnd := win.NativeHandle()
	mygo.RunOnMain(func() {
		pSetPos.Call(hwnd, topmost, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), noActivate)
	})
}

// workArea is the display without the taskbar.
func workArea(d platform.Display) platform.Rect {
	info := struct {
		Size          uint32
		Monitor, Work rect32
		Flags         uint32
	}{Size: 40}
	if ok, _, _ := pMonitorInfo.Call(uintptr(d.ID), uintptr(unsafe.Pointer(&info))); ok == 0 {
		return d.Bounds
	}
	return info.Work.rect()
}

// windowRect is the window's visible frame now; ok is false once it has closed or while it is
// minimized.
func windowRect(id int64) (platform.Rect, bool) {
	h := uintptr(id)
	if ok, _, _ := pIsWindow.Call(h); ok == 0 {
		return platform.Rect{}, false
	}
	if min, _, _ := pIsIconic.Call(h); min != 0 {
		return platform.Rect{}, false
	}
	const extendedFrameBounds = 9
	var r rect32
	if hr, _, _ := pDwmGet.Call(h, extendedFrameBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r)); hr != 0 {
		return platform.Rect{}, false
	}
	return r.rect(), true
}

// squareCorners turns off the round corners and the 1 px border Windows 11 gives a Panel
// window, for one that covers a display.
func squareCorners(win *mygo.Window) {
	hwnd := win.NativeHandle()
	mygo.RunOnMain(func() {
		const cornerPreference, borderColor = 33, 34
		doNotRound, none := int32(1), uint32(0xFFFFFFFE)
		pDwmSet.Call(hwnd, cornerPreference, uintptr(unsafe.Pointer(&doNotRound)), 4)
		pDwmSet.Call(hwnd, borderColor, uintptr(unsafe.Pointer(&none)), 4)
	})
}
