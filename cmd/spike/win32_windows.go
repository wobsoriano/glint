package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32 = syscall.NewLazyDLL("user32.dll")
	gdi32  = syscall.NewLazyDLL("gdi32.dll")
	dwmapi = syscall.NewLazyDLL("dwmapi.dll")

	pGetDC                    = user32.NewProc("GetDC")
	pReleaseDC                = user32.NewProc("ReleaseDC")
	pSetWindowPos             = user32.NewProc("SetWindowPos")
	pGetWindowRect            = user32.NewProc("GetWindowRect")
	pGetClientRect            = user32.NewProc("GetClientRect")
	pClientToScreen           = user32.NewProc("ClientToScreen")
	pGetForegroundWindow      = user32.NewProc("GetForegroundWindow")
	pSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	pSendInput                = user32.NewProc("SendInput")
	pGetCursorPos             = user32.NewProc("GetCursorPos")
	pSetCursorPos             = user32.NewProc("SetCursorPos")
	pEnumDisplayMonitors      = user32.NewProc("EnumDisplayMonitors")
	pGetMonitorInfoW          = user32.NewProc("GetMonitorInfoW")
	pGetWindowDisplayAffinity = user32.NewProc("GetWindowDisplayAffinity")
	pGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	pGetWindowTextW           = user32.NewProc("GetWindowTextW")
	pFindWindowW              = user32.NewProc("FindWindowW")
	pRegisterHotKey           = user32.NewProc("RegisterHotKey")
	pGetCursorInfo            = user32.NewProc("GetCursorInfo")
	pLoadCursorW              = user32.NewProc("LoadCursorW")
	pGetDpiForWindow          = user32.NewProc("GetDpiForWindow")
	pShowWindow               = user32.NewProc("ShowWindow")
	pPostMessageW             = user32.NewProc("PostMessageW")
	pWindowFromPoint          = user32.NewProc("WindowFromPoint")
	pGetAncestor              = user32.NewProc("GetAncestor")
	pGetClassNameW            = user32.NewProc("GetClassNameW")

	pCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	pCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	pSelectObject       = gdi32.NewProc("SelectObject")
	pBitBlt             = gdi32.NewProc("BitBlt")
	pDeleteDC           = gdi32.NewProc("DeleteDC")
	pDeleteObject       = gdi32.NewProc("DeleteObject")

	pDwmGetWindowAttribute = dwmapi.NewProc("DwmGetWindowAttribute")
	pDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

type rect struct{ Left, Top, Right, Bottom int32 }

func (r rect) w() int32 { return r.Right - r.Left }
func (r rect) h() int32 { return r.Bottom - r.Top }

func (r rect) String() string { return fmt.Sprintf("(%d,%d %dx%d)", r.Left, r.Top, r.w(), r.h()) }

type point struct{ X, Y int32 }

type monitor struct {
	handle      uintptr
	bounds      rect
	work        rect
	primary     bool
	deviceName  string
	scalePct    int
	displayDIPs string
}

type monitorInfoEx struct {
	Size    uint32
	Monitor rect
	Work    rect
	Flags   uint32
	Device  [32]uint16
}

// monitors lists the displays in physical pixels; the process must be DPI aware (MyGo makes it
// per-monitor aware when the app starts).
func monitors() []monitor {
	var out []monitor
	cb := syscall.NewCallback(func(h, _, _, _ uintptr) uintptr {
		mi := monitorInfoEx{Size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		pGetMonitorInfoW.Call(h, uintptr(unsafe.Pointer(&mi)))
		out = append(out, monitor{handle: h, bounds: mi.Monitor, work: mi.Work, primary: mi.Flags&1 != 0, deviceName: syscall.UTF16ToString(mi.Device[:])})
		return 1
	})
	pEnumDisplayMonitors.Call(0, 0, cb, 0)
	return out
}

// captureScreen copies a rectangle of the desktop in physical pixels with GDI, as Glint's
// fallback capture does: what DWM composes, with CAPTUREBLT for layered windows.
func captureScreen(r rect) *image.RGBA {
	w, h := int(r.w()), int(r.h())
	screen, _, _ := pGetDC.Call(0)
	defer pReleaseDC.Call(0, screen)
	mem, _, _ := pCreateCompatibleDC.Call(screen)
	defer pDeleteDC.Call(mem)
	type bitmapInfoHeader struct {
		Size                         uint32
		Width, Height                int32
		Planes, BitCount             uint16
		Compression, SizeImage       uint32
		XPelsPerMeter, YPelsPerMeter int32
		ClrUsed, ClrImportant        uint32
		_                            [4]byte
	}
	bi := bitmapInfoHeader{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	bi.Size = 40
	var bits unsafe.Pointer
	bmp, _, _ := pCreateDIBSection.Call(mem, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	defer pDeleteObject.Call(bmp)
	old, _, _ := pSelectObject.Call(mem, bmp)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	pBitBlt.Call(mem, 0, 0, uintptr(w), uintptr(h), screen, uintptr(r.Left), uintptr(r.Top), srcCopy|captureBlt)
	pSelectObject.Call(mem, old)
	src := unsafe.Slice((*byte)(bits), w*h*4)
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(src); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = src[i+2], src[i+1], src[i], 255
	}
	return img
}

func windowRect(hwnd uintptr) rect {
	var r rect
	pGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	return r
}

// frameRect is what DWM draws of the window, without the invisible resize borders.
func frameRect(hwnd uintptr) rect {
	var r rect
	const dwmwaExtendedFrameBounds = 9
	pDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
	return r
}

func clientRect(hwnd uintptr) rect {
	var r rect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	var p point
	pClientToScreen.Call(hwnd, uintptr(unsafe.Pointer(&p)))
	return rect{p.X, p.Y, p.X + r.Right, p.Y + r.Bottom}
}

const (
	hwndTopmost    = ^uintptr(0)
	swpNoActivate  = 0x0010
	swpNoZOrder    = 0x0004
	swpShowWindow  = 0x0040
	swpNoOwnerZ    = 0x0200
	swpFrameChange = 0x0020
)

// placePhysical puts the window on exactly r, in physical pixels: MyGo's SetBounds takes whole
// DIPs, which cannot name every pixel at 125%, 150% or 175%.
func placePhysical(hwnd uintptr, r rect) {
	pSetWindowPos.Call(hwnd, hwndTopmost, uintptr(r.Left), uintptr(r.Top), uintptr(r.w()), uintptr(r.h()), swpNoActivate)
}

// rootClass names the class of the top-level window under a screen point.
func rootClass(p point) string {
	h, _, _ := pWindowFromPoint.Call(uintptr(*(*uint64)(unsafe.Pointer(&p))))
	const gaRoot = 2
	root, _, _ := pGetAncestor.Call(h, gaRoot)
	buf := make([]uint16, 128)
	pGetClassNameW.Call(root, uintptr(unsafe.Pointer(&buf[0])), 128)
	return syscall.UTF16ToString(buf)
}

func foreground() uintptr {
	h, _, _ := pGetForegroundWindow.Call()
	return h
}

func windowText(hwnd uintptr) string {
	buf := make([]uint16, 256)
	pGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf)
}

func displayAffinity(hwnd uintptr) uint32 {
	var a uint32
	pGetWindowDisplayAffinity.Call(hwnd, uintptr(unsafe.Pointer(&a)))
	return a
}

func exStyle(hwnd uintptr) uintptr {
	const gwlExStyle = ^uintptr(19) // -20
	v, _, _ := pGetWindowLongPtrW.Call(hwnd, gwlExStyle)
	return v
}

func style(hwnd uintptr) uintptr {
	const gwlStyle = ^uintptr(15) // -16
	v, _, _ := pGetWindowLongPtrW.Call(hwnd, gwlStyle)
	return v
}

func dpiForWindow(hwnd uintptr) int {
	d, _, _ := pGetDpiForWindow.Call(hwnd)
	return int(d)
}

func cornerPreference(hwnd uintptr) int32 {
	var v int32
	const dwmwaWindowCornerPreference = 33
	pDwmGetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&v)), 4)
	return v
}

func setCornerPreference(hwnd uintptr, v int32) {
	const dwmwaWindowCornerPreference = 33
	pDwmSetWindowAttribute.Call(hwnd, dwmwaWindowCornerPreference, uintptr(unsafe.Pointer(&v)), 4)
}

func setBorderColor(hwnd uintptr, v uint32) {
	const dwmwaBorderColor = 34
	pDwmSetWindowAttribute.Call(hwnd, dwmwaBorderColor, uintptr(unsafe.Pointer(&v)), 4)
}

// cursorShape reports the handle of the cursor showing now and of the system's crosshair.
func cursorShape() (now, cross uintptr) {
	type cursorInfo struct {
		Size, Flags uint32
		Cursor      uintptr
		Pos         point
	}
	ci := cursorInfo{Size: uint32(unsafe.Sizeof(cursorInfo{}))}
	pGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci)))
	const idcCross = 32515
	cross, _, _ = pLoadCursorW.Call(0, idcCross)
	return ci.Cursor, cross
}

func cursorPos() point {
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return p
}

// Synthetic input, in short bursts.

type mouseInput struct {
	Type      uint32
	_         uint32
	DX, DY    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type keyInput struct {
	Type     uint32
	_        uint32
	VK, Scan uint16
	Flags    uint32
	Time     uint32
	Extra    uintptr
	_        [8]byte
}

const (
	mouseMove     = 0x0001
	mouseLeftDown = 0x0002
	mouseLeftUp   = 0x0004
	mouseRightDn  = 0x0008
	mouseRightUp  = 0x0010
	keyUp         = 0x0002
)

func sendMouse(flags uint32) {
	in := mouseInput{Type: 0, Flags: flags}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

func moveTo(x, y int32) {
	pSetCursorPos.Call(uintptr(x), uintptr(y))
	sendMouse(mouseMove) // a relative move of 0 makes the window under the cursor hear it
}

// drag presses at from, moves to to in steps, and releases there.
func drag(from, to point, steps int, pause time.Duration) {
	moveTo(from.X, from.Y)
	time.Sleep(pause)
	sendMouse(mouseLeftDown)
	time.Sleep(pause)
	for i := 1; i <= steps; i++ {
		x := from.X + (to.X-from.X)*int32(i)/int32(steps)
		y := from.Y + (to.Y-from.Y)*int32(i)/int32(steps)
		moveTo(x, y)
		time.Sleep(pause)
	}
	sendMouse(mouseLeftUp)
}

func click(x, y int32) {
	moveTo(x, y)
	time.Sleep(30 * time.Millisecond)
	sendMouse(mouseLeftDown)
	sendMouse(mouseLeftUp)
}

func rightClick(x, y int32) {
	moveTo(x, y)
	time.Sleep(30 * time.Millisecond)
	sendMouse(mouseRightDn)
	sendMouse(mouseRightUp)
}

// keys presses the virtual keys down in order and releases them in reverse.
func keys(vks ...uint16) {
	for _, vk := range vks {
		in := keyInput{Type: 1, VK: vk}
		pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
	for i := len(vks) - 1; i >= 0; i-- {
		in := keyInput{Type: 1, VK: vks[i], Flags: keyUp}
		pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
}

const (
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkEscape  = 0x1B
	vkSpace   = 0x20
	vkReturn  = 0x0D
	vkDown    = 0x28
	vkRight   = 0x27
	vkF9      = 0x78
)

func savePNG(path string, img image.Image) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		logf("save %s: %v", path, err)
		return
	}
	f, err := os.Create(path)
	if err != nil {
		logf("save %s: %v", path, err)
		return
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		logf("save %s: %v", path, err)
		return
	}
	logf("saved %s", path)
}
