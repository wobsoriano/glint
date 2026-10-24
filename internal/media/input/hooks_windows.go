package input

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32 = windows.NewLazySystemDLL("user32.dll")
	gdi32  = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi = windows.NewLazySystemDLL("dwmapi.dll")

	procSetWindowsHookExW                    = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx                  = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx                       = user32.NewProc("CallNextHookEx")
	procGetMessageW                          = user32.NewProc("GetMessageW")
	procDispatchMessageW                     = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW                   = user32.NewProc("PostThreadMessageW")
	procSetTimer                             = user32.NewProc("SetTimer")
	procKillTimer                            = user32.NewProc("KillTimer")
	procGetCursorInfo                        = user32.NewProc("GetCursorInfo")
	procGetWindowRect                        = user32.NewProc("GetWindowRect")
	procMapVirtualKeyW                       = user32.NewProc("MapVirtualKeyW")
	procSetThreadDpiAwarenessContext         = user32.NewProc("SetThreadDpiAwarenessContext")
	procDwmGetWindowAttribute                = dwmapi.NewProc("DwmGetWindowAttribute")
	procGetModuleHandleW                     = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW")
	dpiAwarenessPerMonitorV2         uintptr = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (HANDLE)-4
)

const (
	whKeyboardLL = 13
	whMouseLL    = 14
	hcAction     = 0

	wmQuit        = 0x0012
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	wmTimer       = 0x0113
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMButtonDown = 0x0207
	wmMButtonUp   = 0x0208

	cursorShowing = 1
	cursorPollMS  = 32

	dwmwaExtendedFrameBounds = 9
)

type point struct{ x, y int32 }

type rect struct{ left, top, right, bottom int32 }

type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
	private uint32
}

type msllHookStruct struct {
	pt        point
	mouseData uint32
	flags     uint32
	time      uint32
	extraInfo uintptr
}

type kbdllHookStruct struct {
	vkCode, scanCode, flags, time uint32
	extraInfo                     uintptr
}

type cursorInfo struct {
	cbSize, flags uint32
	hCursor       uintptr
	pt            point
}

// windowOrigin is the window's top-left in physical desktop pixels. The calling thread must
// be per-monitor DPI aware, as the hook thread is.
func windowOrigin(hwnd int64, bounds WindowBounds) (image.Point, bool) {
	var r rect
	if bounds == BoundsDWM {
		hr, _, _ := procDwmGetWindowAttribute.Call(uintptr(hwnd), dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
		return image.Pt(int(r.left), int(r.top)), hr == 0
	}
	ok, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&r)))
	return image.Pt(int(r.left), int(r.top)), ok != 0
}

// hookState turns hook messages into recorder calls: it maps desktop positions to the
// capture, tracks modifiers for shortcut labels, and renders new cursor shapes. Only the
// hook thread touches it.
type hookState struct {
	rec    *recorder
	pmap   PointerMap
	cursor uintptr
	mods   mods
	down   map[uint32]bool
}

func newHookState(o Options, cursorsDir string) *hookState {
	return &hookState{
		rec:  newRecorder(o.Size, o.Scale, cursorsDir, o.Clock, o.IgnoredKeys),
		pmap: o.Map,
		down: map[uint32]bool{},
	}
}

func (s *hookState) onMouse(message uint32, x, y int32) {
	if s.rec.clock.IsPaused() {
		return
	}
	p := s.pmap.toCapture(int(x), int(y))
	switch message {
	case wmMouseMove:
		s.rec.moved(p)
	case wmLButtonDown:
		s.rec.pressed(ButtonLeft, p)
	case wmRButtonDown:
		s.rec.pressed(ButtonRight, p)
	case wmMButtonDown:
		s.rec.pressed(ButtonMiddle, p)
	case wmLButtonUp:
		s.rec.released(ButtonLeft, p)
	case wmRButtonUp:
		s.rec.released(ButtonRight, p)
	case wmMButtonUp:
		s.rec.released(ButtonMiddle, p)
	}
}

func (s *hookState) onKey(message, vk, scan uint32) {
	down := message == wmKeyDown || message == wmSysKeyDown
	if s.mods.apply(vk, scan, down) {
		return
	}
	if !down {
		delete(s.down, vk)
		return
	}
	if s.down[vk] {
		return
	}
	s.down[vk] = true
	if label := shortcutLabel(vk, s.mods); label != "" {
		s.rec.key(label)
	}
}

// pollCursor notices cursor shape changes, and moves the hooks did not see (SetCursorPos
// moves the cursor without an input event).
func (s *hookState) pollCursor() {
	ci := cursorInfo{cbSize: uint32(unsafe.Sizeof(cursorInfo{}))}
	if r, _, _ := procGetCursorInfo.Call(uintptr(unsafe.Pointer(&ci))); r == 0 || ci.flags&cursorShowing == 0 {
		return
	}
	p := s.pmap.toCapture(int(ci.pt.x), int(ci.pt.y))
	s.rec.polled(p)
	if ci.hCursor == s.cursor {
		return
	}
	img, ok := renderCursor(ci.hCursor)
	if !ok {
		return
	}
	s.cursor = ci.hCursor
	s.rec.cursorChanged(img, p)
}

// active is the running track's state. Hook procedures carry no context, and only one input
// track runs at a time.
var (
	active  *hookState
	running atomic.Bool

	mouseProc = syscall.NewCallback(func(code, wparam, lparam uintptr) uintptr {
		if int32(code) == hcAction && active != nil {
			data := *(**msllHookStruct)(unsafe.Pointer(&lparam))
			active.onMouse(uint32(wparam), data.pt.x, data.pt.y)
		}
		r, _, _ := procCallNextHookEx.Call(0, code, wparam, lparam)
		return r
	})
	keyboardProc = syscall.NewCallback(func(code, wparam, lparam uintptr) uintptr {
		if int32(code) == hcAction && active != nil {
			data := *(**kbdllHookStruct)(unsafe.Pointer(&lparam))
			active.onKey(uint32(wparam), data.vkCode, data.scanCode)
		}
		r, _, _ := procCallNextHookEx.Call(0, code, wparam, lparam)
		return r
	})
)

// Track runs low-level mouse and keyboard hooks on their own OS thread.
type Track struct {
	threadID uint32
	done     chan struct{}
	tl       Timeline
}

// Start installs the hooks and returns once they are in place.
func Start(o Options) (*Track, error) {
	cursorsDir := filepath.Join(o.SessionDir, "cursors")
	if err := os.MkdirAll(cursorsDir, 0o755); err != nil {
		return nil, err
	}
	if !running.CompareAndSwap(false, true) {
		return nil, errors.New("an input track is already running")
	}
	t := &Track{done: make(chan struct{})}
	started := make(chan error, 1)
	go func() {
		defer close(t.done)
		defer running.Store(false)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		prev, _, _ := procSetThreadDpiAwarenessContext.Call(dpiAwarenessPerMonitorV2)
		if prev != 0 {
			defer procSetThreadDpiAwarenessContext.Call(prev)
		}

		module, _, _ := procGetModuleHandleW.Call(0)
		mouse, _, merr := procSetWindowsHookExW.Call(whMouseLL, mouseProc, module, 0)
		if mouse == 0 {
			started <- fmt.Errorf("WH_MOUSE_LL: %w", merr)
			return
		}
		defer procUnhookWindowsHookEx.Call(mouse)
		keyboard, _, kerr := procSetWindowsHookExW.Call(whKeyboardLL, keyboardProc, module, 0)
		if keyboard == 0 {
			started <- fmt.Errorf("WH_KEYBOARD_LL: %w", kerr)
			return
		}
		defer procUnhookWindowsHookEx.Call(keyboard)

		active = newHookState(o, cursorsDir)
		defer func() { active = nil }()
		active.pollCursor()
		timer, _, _ := procSetTimer.Call(0, 0, cursorPollMS, 0)
		defer procKillTimer.Call(0, timer)
		t.threadID = windows.GetCurrentThreadId()
		started <- nil

		var m msg
		for {
			r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			if m.message == wmTimer {
				active.pollCursor()
			}
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
		t.tl = active.rec.finish()
	}()
	if err := <-started; err != nil {
		<-t.done
		return nil, err
	}
	return t, nil
}

// Stop removes the hooks and returns the timeline, with clicks still held closed at the
// clock's media time. Calling it again returns the same timeline.
func (t *Track) Stop() (Timeline, error) {
	select {
	case <-t.done:
		return t.tl, nil
	default:
	}
	if r, _, err := procPostThreadMessageW.Call(uintptr(t.threadID), wmQuit, 0, 0); r == 0 {
		return Timeline{}, fmt.Errorf("PostThreadMessageW: %w", err)
	}
	<-t.done
	return t.tl, nil
}
