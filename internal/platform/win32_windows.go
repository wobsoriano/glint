package platform

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	shcore = windows.NewLazySystemDLL("shcore.dll")

	procSetProcessDpiAwarenessContext = user32.NewProc("SetProcessDpiAwarenessContext")
	procGetThreadDpiAwarenessContext  = user32.NewProc("GetThreadDpiAwarenessContext")
	procAreDpiAwarenessContextsEqual  = user32.NewProc("AreDpiAwarenessContextsEqual")
	procEnumDisplayMonitors           = user32.NewProc("EnumDisplayMonitors")
	procGetMonitorInfoW               = user32.NewProc("GetMonitorInfoW")
	procGetCursorPos                  = user32.NewProc("GetCursorPos")
	procGetDC                         = user32.NewProc("GetDC")
	procReleaseDC                     = user32.NewProc("ReleaseDC")
	procPrintWindow                   = user32.NewProc("PrintWindow")
	procGetWindowRect                 = user32.NewProc("GetWindowRect")
	procGetDpiForWindow               = user32.NewProc("GetDpiForWindow")
	procIsIconic                      = user32.NewProc("IsIconic")
	procGetWindowLongW                = user32.NewProc("GetWindowLongW")
	procGetWindowTextW                = user32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW          = user32.NewProc("GetWindowTextLengthW")
	procSetWindowDisplayAffinity      = user32.NewProc("SetWindowDisplayAffinity")
	procSystemParametersInfoW         = user32.NewProc("SystemParametersInfoW")

	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
	procBitBlt             = gdi32.NewProc("BitBlt")
	procGdiFlush           = gdi32.NewProc("GdiFlush")

	procGetDpiForMonitor = shcore.NewProc("GetDpiForMonitor")
)

const (
	dpiAwarenessContextPerMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (HANDLE)-4

	srcCopy    = 0x00CC0020
	captureBlt = 0x40000000

	pwRenderFullContent = 2
	gwlExStyle          = -20
	wsExToolWindow      = 0x00000080
	monitorInfoPrimary  = 1
	mdtEffectiveDPI     = 0

	wdaNone               = 0
	wdaExcludeFromCapture = 0x11

	spiGetDeskWallpaper = 0x0073
)

// Coordinates are physical pixels only when the process is per-monitor-v2 aware. MyGo's
// Backend.Init sets that too, and the manifest `mygo build` writes declares it; whichever
// comes second fails harmlessly with access denied. Doing it here at init means this package
// is right on its own, in tests and tools, and before any window exists, which the API needs.
func init() {
	procSetProcessDpiAwarenessContext.Call(dpiAwarenessContextPerMonitorV2)
}

// perMonitorV2 reports whether the calling thread sees physical pixels.
func perMonitorV2() bool {
	ctx, _, _ := procGetThreadDpiAwarenessContext.Call()
	eq, _, _ := procAreDpiAwarenessContextsEqual.Call(ctx, dpiAwarenessContextPerMonitorV2)
	return eq != 0
}

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) toRect() Rect {
	return Rect{int(r.Left), int(r.Top), int(r.Right - r.Left), int(r.Bottom - r.Top)}
}

type monitorInfoEx struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
	Device  [32]uint16
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func call(p *windows.LazyProc, args ...uintptr) (uintptr, error) {
	r, _, err := p.Call(args...)
	if r == 0 {
		if err == nil || err == windows.ERROR_SUCCESS {
			err = syscall.EINVAL
		}
		return 0, &callError{p.Name, err}
	}
	return r, nil
}

type callError struct {
	fn  string
	err error
}

func (e *callError) Error() string { return "platform: " + e.fn + ": " + e.err.Error() }
func (e *callError) Unwrap() error { return e.err }

func windowRect(hwnd uintptr) (rect, error) {
	var r rect
	_, err := call(procGetWindowRect, hwnd, uintptr(unsafe.Pointer(&r)))
	return r, err
}

// frameBounds is the window's visible frame, without the invisible resize borders that
// GetWindowRect includes.
func frameBounds(hwnd uintptr) (rect, error) {
	var r rect
	err := windows.DwmGetWindowAttribute(windows.HWND(hwnd), windows.DWMWA_EXTENDED_FRAME_BOUNDS, unsafe.Pointer(&r), uint32(unsafe.Sizeof(r)))
	return r, err
}

func windowText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	got, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf[:got])
}

func windowScale(hwnd uintptr) float64 {
	dpi, _, _ := procGetDpiForWindow.Call(hwnd)
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / 96
}
