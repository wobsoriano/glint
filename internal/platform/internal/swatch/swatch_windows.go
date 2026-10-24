// Package swatch puts a solid-colour window on the desktop with plain Win32, so tests and the
// probe can capture pixels whose colour they know.
package swatch

import (
	"errors"
	"image/color"
	"runtime"
	"sync"
	"unsafe"

	"github.com/wobsoriano/glint/internal/platform"
	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")

	procRegisterClassExW = user32.NewProc("RegisterClassExW")
	procCreateWindowExW  = user32.NewProc("CreateWindowExW")
	procDefWindowProcW   = user32.NewProc("DefWindowProcW")
	procDestroyWindow    = user32.NewProc("DestroyWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procUpdateWindow     = user32.NewProc("UpdateWindow")
	procGetMessageW      = user32.NewProc("GetMessageW")
	procTranslateMessage = user32.NewProc("TranslateMessage")
	procDispatchMessageW = user32.NewProc("DispatchMessageW")
	procPostMessageW     = user32.NewProc("PostMessageW")
	procPostQuitMessage  = user32.NewProc("PostQuitMessage")
	procGetClientRect    = user32.NewProc("GetClientRect")
	procFillRect         = user32.NewProc("FillRect")
	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
)

const (
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmEraseBkgnd     = 0x0014
	wsPopup          = 0x80000000
	wsExTopmost      = 0x00000008
	wsExToolWindow   = 0x00000080
	wsExNoActivate   = 0x08000000
	swShowNoActivate = 4
)

const className = "GlintPlatformSwatch"

var (
	registerOnce sync.Once
	registerErr  error
	brushesMu    sync.Mutex
	brushes      = map[uintptr]uintptr{}
)

func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmEraseBkgnd:
		brushesMu.Lock()
		b := brushes[hwnd]
		brushesMu.Unlock()
		var r struct{ L, T, R, B int32 }
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		procFillRect.Call(wp, uintptr(unsafe.Pointer(&r)), b)
		return 1
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wp, lp)
	return r
}

func register() error {
	registerOnce.Do(func() {
		name, _ := windows.UTF16PtrFromString(className)
		wc := struct {
			Size       uint32
			Style      uint32
			WndProc    uintptr
			ClsExtra   int32
			WndExtra   int32
			Instance   windows.Handle
			Icon       windows.Handle
			Cursor     windows.Handle
			Background windows.Handle
			MenuName   *uint16
			ClassName  *uint16
			IconSm     windows.Handle
		}{WndProc: windows.NewCallback(wndProc), ClassName: name}
		wc.Size = uint32(unsafe.Sizeof(wc))
		if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
			registerErr = err
		}
	})
	return registerErr
}

// Swatch is a borderless, topmost window filled with one colour that never takes focus.
type Swatch struct {
	HWND uintptr
	done chan struct{}
}

// Show opens a swatch over r. A tool swatch stays off the taskbar and out of Windows().
func Show(r platform.Rect, c color.RGBA, title string, tool bool) (*Swatch, error) {
	if err := register(); err != nil {
		return nil, err
	}
	s := &Swatch{done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(s.done)
		ex := uintptr(wsExTopmost | wsExNoActivate)
		if tool {
			ex |= wsExToolWindow
		}
		cls, _ := windows.UTF16PtrFromString(className)
		ttl, _ := windows.UTF16PtrFromString(title)
		brush, _, _ := procCreateSolidBrush.Call(uintptr(c.R) | uintptr(c.G)<<8 | uintptr(c.B)<<16)
		defer procDeleteObject.Call(brush)
		hwnd, _, err := procCreateWindowExW.Call(ex, uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(ttl)), wsPopup,
			uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), 0, 0, 0, 0)
		if hwnd == 0 {
			ready <- err
			return
		}
		brushesMu.Lock()
		brushes[hwnd] = brush
		brushesMu.Unlock()
		defer func() {
			brushesMu.Lock()
			delete(brushes, hwnd)
			brushesMu.Unlock()
		}()
		procShowWindow.Call(hwnd, swShowNoActivate)
		procUpdateWindow.Call(hwnd)
		s.HWND = hwnd
		ready <- nil
		var msg [48]byte
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(r) <= 0 {
				return
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg[0])))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg[0])))
		}
	}()
	if err := <-ready; err != nil {
		return nil, errors.Join(errors.New("swatch: cannot create window"), err)
	}
	return s, nil
}

// Close destroys the window and waits for its thread to finish.
func (s *Swatch) Close() {
	procPostMessageW.Call(s.HWND, wmClose, 0, 0)
	<-s.done
}
