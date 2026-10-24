package overlay

import (
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/wobsoriano/glint/internal/platform"
)

var (
	user32                    = windows.NewLazySystemDLL("user32.dll")
	dwmapi                    = windows.NewLazySystemDLL("dwmapi.dll")
	procSetWindowPos          = user32.NewProc("SetWindowPos")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")
)

// prepare readies a Panel window to cover b exactly: Windows 11 gives panels round corners and
// a 1 px border, which would hide pixels of the frozen screen, and MyGo's geometry is whole
// DIPs, which cannot name every pixel at 125% or 175%.
func prepare(hwnd uintptr, b platform.Rect) {
	const cornerPreference, borderColor = 33, 34
	doNotRound, colorNone := int32(1), uint32(0xFFFFFFFE)
	procDwmSetWindowAttribute.Call(hwnd, cornerPreference, uintptr(unsafe.Pointer(&doNotRound)), 4)
	procDwmSetWindowAttribute.Call(hwnd, borderColor, uintptr(unsafe.Pointer(&colorNone)), 4)
	const topmost = ^uintptr(0)
	const noActivate = 0x0010
	procSetWindowPos.Call(hwnd, topmost, uintptr(b.X), uintptr(b.Y), uintptr(b.W), uintptr(b.H), noActivate)
}
