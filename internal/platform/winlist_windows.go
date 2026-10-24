package platform

import (
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows follows list_windows in src/sys/win/capture.rs, except that Glint's own windows
// stay in: callers drop them by PID when they must.
func Windows() ([]Window, error) {
	var hwnds []uintptr
	enumMu.Lock()
	enumSink = func(h uintptr) bool { hwnds = append(hwnds, h); return true }
	err := windows.EnumWindows(enumWindowsCallback, nil)
	enumSink = nil
	enumMu.Unlock()
	if err != nil {
		return nil, err
	}

	apps := map[uint32]string{}
	var out []Window
	for _, h := range hwnds {
		w, ok := listed(h)
		if !ok {
			continue
		}
		pid := uint32(w.PID)
		app, seen := apps[pid]
		if !seen {
			app = exeName(pid)
			apps[pid] = app
		}
		w.App = app
		out = append(out, w)
	}
	return out, nil
}

func listed(h uintptr) (Window, bool) {
	hwnd := windows.HWND(h)
	if !windows.IsWindowVisible(hwnd) {
		return Window{}, false
	}
	if iconic, _, _ := procIsIconic.Call(h); iconic != 0 {
		return Window{}, false
	}
	gwl := int32(gwlExStyle)
	if ex, _, _ := procGetWindowLongW.Call(h, uintptr(gwl)); ex&wsExToolWindow != 0 {
		return Window{}, false
	}
	var cloaked uint32
	if windows.DwmGetWindowAttribute(hwnd, windows.DWMWA_CLOAKED, unsafe.Pointer(&cloaked), 4) == nil && cloaked != 0 {
		return Window{}, false
	}
	title := windowText(h)
	if title == "" || title == "Program Manager" {
		return Window{}, false
	}
	frame, err := frameBounds(h)
	if err != nil {
		return Window{}, false
	}
	b := frame.toRect()
	if b.W <= 20 || b.H <= 20 {
		return Window{}, false
	}
	var pid uint32
	windows.GetWindowThreadProcessId(hwnd, &pid)
	return Window{ID: int64(h), Title: title, PID: int(pid), Bounds: b}, true
}

func exeName(pid uint32) string {
	p, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(p)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(p, 0, &buf[0], &n) != nil {
		return ""
	}
	base := filepath.Base(windows.UTF16ToString(buf[:n]))
	return strings.TrimSuffix(base, filepath.Ext(base))
}
