package app

import (
	"os"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"

	"github.com/wobsoriano/glint/internal/platform"
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSetWinEventHook    = user32.NewProc("SetWinEventHook")
	procGetAncestor        = user32.NewProc("GetAncestor")
	procGetDoubleClickTime = user32.NewProc("GetDoubleClickTime")
)

func foregroundWindow() int64 { return int64(windows.GetForegroundWindow()) }

func doubleClickTime() time.Duration {
	ms, _, _ := procGetDoubleClickTime.Call()
	return time.Duration(ms) * time.Millisecond
}

var (
	includeOwn atomic.Bool
	hookOnce   sync.Once
	// One callback each for the process's life: Windows allows a process only so many.
	onShow = windows.NewCallback(func(hook, event, hwnd, idObject, idChild, thread, at uintptr) uintptr {
		const eventObjectShow, objidWindow, gaRoot = 0x8002, 0, 2
		if event == eventObjectShow && int32(idObject) == objidWindow {
			if root, _, _ := procGetAncestor.Call(hwnd, gaRoot); root == hwnd {
				applyAffinity(hwnd)
			}
		}
		return 0
	})
	eachOwn = windows.NewCallback(func(hwnd, _ uintptr) uintptr {
		var pid uint32
		windows.GetWindowThreadProcessId(windows.HWND(hwnd), &pid)
		if int(pid) == os.Getpid() {
			applyAffinity(hwnd)
		}
		return 1
	})
)

func applyAffinity(hwnd uintptr) { platform.ExcludeFromCapture(hwnd, !includeOwn.Load()) }

// excludeOwnWindows keeps every window of this process out of screenshots and recordings,
// the ones open now and every one shown later, unless include is set. It hooks window shows
// as the Rust build did, so surfaces built by other packages need no call of their own. Call
// it on the main thread, whose message loop delivers the hook's events.
func excludeOwnWindows(include bool) {
	includeOwn.Store(include)
	hookOnce.Do(func() {
		const eventObjectShow, outOfContext = 0x8002, 0
		procSetWinEventHook.Call(eventObjectShow, eventObjectShow, 0, onShow, uintptr(os.Getpid()), 0, outOfContext)
	})
	windows.EnumWindows(eachOwn, nil)
}
