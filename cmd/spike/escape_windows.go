package main

import (
	"syscall"
	"unsafe"
)

// Raw Win32 escape hatches through Window.NativeHandle, for what MyGo does not expose.

var (
	ole32              = syscall.NewLazyDLL("ole32.dll")
	pCoCreateInstance  = ole32.NewProc("CoCreateInstance")
	pSetWindowLongPtrW = user32.NewProc("SetWindowLongPtrW")
)

type guid struct {
	a    uint32
	b, c uint16
	d    [8]byte
}

var (
	clsidTaskbarList = guid{0x56FDF344, 0xFD6D, 0x11d0, [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidTaskbarList2  = guid{0x602D4995, 0xB13A, 0x429b, [8]byte{0xA6, 0x6E, 0x19, 0x35, 0xE4, 0x4F, 0x43, 0x17}}
)

// markFullscreen tells the taskbar that hwnd is a full-screen window, so that the taskbar goes
// below it while it is active (ITaskbarList2::MarkFullscreenWindow). Main thread, where MyGo has
// initialized COM.
func markFullscreen(hwnd uintptr) bool {
	var tb *struct{ vt *[9]uintptr }
	const clsctxInprocServer = 1
	if hr, _, _ := pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, clsctxInprocServer, uintptr(unsafe.Pointer(&iidTaskbarList2)), uintptr(unsafe.Pointer(&tb))); hr != 0 {
		logf("CoCreateInstance(TaskbarList): %#x", hr)
		return false
	}
	this := uintptr(unsafe.Pointer(tb))
	syscall.SyscallN(tb.vt[3], this)                      // HrInit
	hr, _, _ := syscall.SyscallN(tb.vt[8], this, hwnd, 1) // MarkFullscreenWindow
	syscall.SyscallN(tb.vt[2], this)                      // Release
	return hr == 0
}

// stripCaption removes the caption, system menu and minimize box MyGo gives a frameless window,
// which keep the shell from seeing it as full screen.
func stripCaption(hwnd uintptr) {
	const gwlStyle = ^uintptr(15)
	const wsCaption, wsSysMenu, wsMinimizeBox, wsPopup = 0x00C00000, 0x00080000, 0x00020000, 0x80000000
	s := style(hwnd)
	pSetWindowLongPtrW.Call(hwnd, gwlStyle, (s&^(wsCaption|wsSysMenu|wsMinimizeBox))|wsPopup)
	pSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, 0x0001|0x0002|swpNoZOrder|swpNoActivate|swpFrameChange)
}
