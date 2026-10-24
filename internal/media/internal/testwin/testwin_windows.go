package testwin

import (
	"errors"
	"image"
	"image/color"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi = windows.NewLazySystemDLL("dwmapi.dll")

	procRegisterClassExW             = user32.NewProc("RegisterClassExW")
	procCreateWindowExW              = user32.NewProc("CreateWindowExW")
	procDefWindowProcW               = user32.NewProc("DefWindowProcW")
	procDestroyWindow                = user32.NewProc("DestroyWindow")
	procShowWindow                   = user32.NewProc("ShowWindow")
	procSetWindowPos                 = user32.NewProc("SetWindowPos")
	procGetWindowRect                = user32.NewProc("GetWindowRect")
	procGetClientRect                = user32.NewProc("GetClientRect")
	procClientToScreen               = user32.NewProc("ClientToScreen")
	procGetMessageW                  = user32.NewProc("GetMessageW")
	procPeekMessageW                 = user32.NewProc("PeekMessageW")
	procTranslateMessage             = user32.NewProc("TranslateMessage")
	procDispatchMessageW             = user32.NewProc("DispatchMessageW")
	procPostMessageW                 = user32.NewProc("PostMessageW")
	procPostQuitMessage              = user32.NewProc("PostQuitMessage")
	procGetDC                        = user32.NewProc("GetDC")
	procReleaseDC                    = user32.NewProc("ReleaseDC")
	procFillRect                     = user32.NewProc("FillRect")
	procSetWindowDisplayAffinity     = user32.NewProc("SetWindowDisplayAffinity")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procCreateCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap       = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject                 = gdi32.NewProc("SelectObject")
	procDeleteObject                 = gdi32.NewProc("DeleteObject")
	procDeleteDC                     = gdi32.NewProc("DeleteDC")
	procBitBlt                       = gdi32.NewProc("BitBlt")
	procGdiFlush                     = gdi32.NewProc("GdiFlush")
	procGetStockObject               = gdi32.NewProc("GetStockObject")
	procSetDCBrushColor              = gdi32.NewProc("SetDCBrushColor")
	procDwmFlush                     = dwmapi.NewProc("DwmFlush")
)

const (
	wmDestroy    = 0x0002
	wmClose      = 0x0010
	wmQuit       = 0x0012
	wmEraseBkgnd = 0x0014

	wsPopup            = 0x80000000
	wsOverlappedWindow = 0x00CF0000
	wsExTopmost        = 0x00000008
	wsExToolWindow     = 0x00000080
	wsExNoActivate     = 0x08000000

	swShowNoActivate = 4
	swpNoActivate    = 0x0010
	swpNoZOrder      = 0x0004
	pmRemove         = 0x0001
	srcCopy          = 0x00CC0020
	dcBrush          = 18

	wdaExcludeFromCapture = 0x11
)

var perMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (HANDLE)-4

const className = "GlintMediaTestWindow"

type Options struct {
	Title string
	// Bordered gives the window a caption and resize borders instead of a bare popup.
	Bordered           bool
	ExcludeFromCapture bool
	// Fill colours the window when Paint is nil.
	Fill color.RGBA
	// Paint redraws the whole client area once per desktop composition, frame counting up
	// from 0.
	Paint func(c *Canvas, frame int64)
}

// Window is a topmost window that never takes focus.
type Window struct {
	HWND uintptr
	fill color.RGBA
	done chan struct{}
}

var (
	registerOnce sync.Once
	registerErr  error
	windowsMu    sync.Mutex
	open         = map[uintptr]*Window{}
)

func wndProc(hwnd, msg, wp, lp uintptr) uintptr {
	switch msg {
	case wmEraseBkgnd:
		windowsMu.Lock()
		w := open[hwnd]
		windowsMu.Unlock()
		if w != nil {
			var r [4]int32
			procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
			(&Canvas{dc: wp}).Fill(image.Rect(int(r[0]), int(r[1]), int(r[2]), int(r[3])), w.fill)
		}
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

// Open shows a window whose outer frame covers r in physical desktop pixels.
func Open(r image.Rectangle, o Options) (*Window, error) {
	if err := register(); err != nil {
		return nil, err
	}
	w := &Window{fill: o.Fill, done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(w.done)
		procSetThreadDpiAwarenessContext.Call(perMonitorV2)
		style := uintptr(wsPopup)
		if o.Bordered {
			style = wsOverlappedWindow
		}
		cls, _ := windows.UTF16PtrFromString(className)
		title, _ := windows.UTF16PtrFromString(o.Title)
		hwnd, _, err := procCreateWindowExW.Call(wsExTopmost|wsExNoActivate|wsExToolWindow, uintptr(unsafe.Pointer(cls)),
			uintptr(unsafe.Pointer(title)), style, uintptr(r.Min.X), uintptr(r.Min.Y), uintptr(r.Dx()), uintptr(r.Dy()), 0, 0, 0, 0)
		if hwnd == 0 {
			ready <- err
			return
		}
		windowsMu.Lock()
		open[hwnd] = w
		windowsMu.Unlock()
		defer func() {
			windowsMu.Lock()
			delete(open, hwnd)
			windowsMu.Unlock()
		}()
		if o.ExcludeFromCapture {
			if ok, _, err := procSetWindowDisplayAffinity.Call(hwnd, wdaExcludeFromCapture); ok == 0 {
				procDestroyWindow.Call(hwnd)
				ready <- err
				return
			}
		}
		procShowWindow.Call(hwnd, swShowNoActivate)
		w.HWND = hwnd
		ready <- nil
		if o.Paint == nil {
			pump(true)
			return
		}
		animate(hwnd, o.Paint)
	}()
	if err := <-ready; err != nil {
		return nil, errors.Join(errors.New("testwin: cannot create window"), err)
	}
	return w, nil
}

// pump dispatches messages until WM_QUIT; with wait false it returns once the queue is empty.
func pump(wait bool) (quit bool) {
	var m [48]byte
	for {
		if wait {
			if r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0); int32(r) <= 0 {
				return true
			}
		} else {
			if r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m[0])), 0, 0, 0, pmRemove); r == 0 {
				return false
			}
			if *(*uint32)(unsafe.Pointer(&m[8])) == wmQuit {
				return true
			}
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m[0])))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m[0])))
	}
}

// animate paints into an off-screen bitmap and copies it to the window in one blit per
// composition, so a capture never sees a half-drawn frame.
func animate(hwnd uintptr, paint func(*Canvas, int64)) {
	var mem, bitmap, old uintptr
	var bw, bh int
	defer func() {
		if mem != 0 {
			procSelectObject.Call(mem, old)
			procDeleteObject.Call(bitmap)
			procDeleteDC.Call(mem)
		}
	}()
	for frame := int64(0); ; frame++ {
		if pump(false) {
			return
		}
		var r [4]int32
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		w, h := int(r[2]), int(r[3])
		dc, _, _ := procGetDC.Call(hwnd)
		if dc == 0 {
			return
		}
		if mem == 0 || w != bw || h != bh {
			if mem != 0 {
				procSelectObject.Call(mem, old)
				procDeleteObject.Call(bitmap)
				procDeleteDC.Call(mem)
			}
			mem, _, _ = procCreateCompatibleDC.Call(dc)
			bitmap, _, _ = procCreateCompatibleBitmap.Call(dc, uintptr(max(w, 1)), uintptr(max(h, 1)))
			old, _, _ = procSelectObject.Call(mem, bitmap)
			bw, bh = w, h
		}
		paint(&Canvas{dc: mem, W: w, H: h}, frame)
		procBitBlt.Call(dc, 0, 0, uintptr(w), uintptr(h), mem, 0, 0, srcCopy)
		procGdiFlush.Call()
		procReleaseDC.Call(hwnd, dc)
		procDwmFlush.Call()
	}
}

// Canvas draws on a window's client area, origin at its top-left.
type Canvas struct {
	dc   uintptr
	W, H int
}

func (c *Canvas) Fill(r image.Rectangle, col color.RGBA) {
	rc := [4]int32{int32(r.Min.X), int32(r.Min.Y), int32(r.Max.X), int32(r.Max.Y)}
	procSetDCBrushColor.Call(c.dc, uintptr(col.R)|uintptr(col.G)<<8|uintptr(col.B)<<16)
	brush, _, _ := procGetStockObject.Call(dcBrush)
	procFillRect.Call(c.dc, uintptr(unsafe.Pointer(&rc)), brush)
}

func physical() func() {
	runtime.LockOSThread()
	prev, _, _ := procSetThreadDpiAwarenessContext.Call(perMonitorV2)
	return func() {
		if prev != 0 {
			procSetThreadDpiAwarenessContext.Call(prev)
		}
		runtime.UnlockOSThread()
	}
}

// Rect is the outer window rectangle in physical desktop pixels.
func (w *Window) Rect() image.Rectangle {
	defer physical()()
	var r [4]int32
	procGetWindowRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	return image.Rect(int(r[0]), int(r[1]), int(r[2]), int(r[3]))
}

// Client is the client area in physical desktop pixels.
func (w *Window) Client() image.Rectangle {
	defer physical()()
	var r [4]int32
	procGetClientRect.Call(w.HWND, uintptr(unsafe.Pointer(&r)))
	var p [2]int32
	procClientToScreen.Call(w.HWND, uintptr(unsafe.Pointer(&p)))
	return image.Rect(int(p[0]), int(p[1]), int(p[0]+r[2]), int(p[1]+r[3]))
}

// SetRect moves and sizes the outer frame to r, in physical desktop pixels.
func (w *Window) SetRect(r image.Rectangle) error {
	defer physical()()
	ok, _, err := procSetWindowPos.Call(w.HWND, 0, uintptr(r.Min.X), uintptr(r.Min.Y), uintptr(r.Dx()), uintptr(r.Dy()), swpNoActivate|swpNoZOrder)
	if ok == 0 {
		return err
	}
	return nil
}

// Close destroys the window and waits for its thread to finish.
func (w *Window) Close() {
	procPostMessageW.Call(w.HWND, wmClose, 0, 0)
	<-w.done
}
