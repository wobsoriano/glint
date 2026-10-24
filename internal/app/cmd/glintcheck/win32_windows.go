package main

import (
	"bytes"
	"flag"
	"fmt"
	"image/png"
	"os"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32                       = windows.NewLazySystemDLL("user32.dll")
	kernel32                     = windows.NewLazySystemDLL("kernel32.dll")
	shell32                      = windows.NewLazySystemDLL("shell32.dll")
	procOpenClipboard            = user32.NewProc("OpenClipboard")
	procCloseClipboard           = user32.NewProc("CloseClipboard")
	procEnumClipboardFormats     = user32.NewProc("EnumClipboardFormats")
	procGetClipboardFormatNameW  = user32.NewProc("GetClipboardFormatNameW")
	procGetClipboardData         = user32.NewProc("GetClipboardData")
	procGetWindowTextW           = user32.NewProc("GetWindowTextW")
	procGetWindowRect            = user32.NewProc("GetWindowRect")
	procGetWindowDisplayAffinity = user32.NewProc("GetWindowDisplayAffinity")
	procGetWindowLongPtrW        = user32.NewProc("GetWindowLongPtrW")
	procGlobalLock               = kernel32.NewProc("GlobalLock")
	procGlobalUnlock             = kernel32.NewProc("GlobalUnlock")
	procGlobalSize               = kernel32.NewProc("GlobalSize")
	procRtlMoveMemory            = kernel32.NewProc("RtlMoveMemory")
	procDragQueryFileW           = shell32.NewProc("DragQueryFileW")
)

const (
	cfUnicodeText = 13
	cfDIB         = 8
	cfHDROP       = 15
)

func formatName(f uintptr) string {
	switch f {
	case cfUnicodeText:
		return "CF_UNICODETEXT"
	case cfDIB:
		return "CF_DIB"
	case 17:
		return "CF_DIBV5"
	case cfHDROP:
		return "CF_HDROP"
	case 1:
		return "CF_TEXT"
	case 7:
		return "CF_OEMTEXT"
	case 16:
		return "CF_LOCALE"
	case 2:
		return "CF_BITMAP"
	}
	buf := make([]uint16, 256)
	n, _, _ := procGetClipboardFormatNameW.Call(f, uintptr(unsafe.Pointer(&buf[0])), 256)
	if n == 0 {
		return fmt.Sprintf("#%d", f)
	}
	return windows.UTF16ToString(buf[:n])
}

func globalBytes(h uintptr) []byte {
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return nil
	}
	defer procGlobalUnlock.Call(h)
	n, _, _ := procGlobalSize.Call(h)
	out := make([]byte, n)
	if n > 0 {
		procRtlMoveMemory.Call(uintptr(unsafe.Pointer(&out[0])), p, n)
	}
	return out
}

// clip prints the clipboard's formats, its text and files, and writes its PNG.
func clip(args []string) error {
	fs := flag.NewFlagSet("clip", flag.ExitOnError)
	out := fs.String("png", "", "write the clipboard's PNG here")
	fs.Parse(args)
	var opened uintptr
	for range 20 {
		if opened, _, _ = procOpenClipboard.Call(0); opened != 0 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if opened == 0 {
		return fmt.Errorf("the clipboard is busy")
	}
	defer procCloseClipboard.Call()
	var names []string
	var pngData []byte
	for f := uintptr(0); ; {
		f, _, _ = procEnumClipboardFormats.Call(f)
		if f == 0 {
			break
		}
		name := formatName(f)
		names = append(names, name)
		h, _, _ := procGetClipboardData.Call(f)
		if h == 0 {
			continue
		}
		switch {
		case name == "PNG":
			pngData = globalBytes(h)
		case f == cfUnicodeText:
			b := globalBytes(h)
			u := unsafe.Slice((*uint16)(unsafe.Pointer(&b[0])), len(b)/2)
			fmt.Printf("text: %q\n", windows.UTF16ToString(u))
		case f == cfHDROP:
			n, _, _ := procDragQueryFileW.Call(h, 0xFFFFFFFF, 0, 0)
			for i := uintptr(0); i < n; i++ {
				buf := make([]uint16, 1024)
				procDragQueryFileW.Call(h, i, uintptr(unsafe.Pointer(&buf[0])), 1024)
				fmt.Printf("file: %s\n", windows.UTF16ToString(buf))
			}
		case f == cfDIB:
			b := globalBytes(h)
			if len(b) >= 16 {
				w := int32(uint32(b[4]) | uint32(b[5])<<8 | uint32(b[6])<<16 | uint32(b[7])<<24)
				hh := int32(uint32(b[8]) | uint32(b[9])<<8 | uint32(b[10])<<16 | uint32(b[11])<<24)
				fmt.Printf("dib: %dx%d\n", w, hh)
			}
		}
	}
	fmt.Printf("formats: %s\n", strings.Join(names, ", "))
	if *out != "" {
		if pngData == nil {
			return fmt.Errorf("the clipboard holds no PNG")
		}
		img, err := png.Decode(bytes.NewReader(pngData))
		if err != nil {
			return err
		}
		fmt.Printf("png: %v -> %s\n", img.Bounds().Size(), *out)
		return os.WriteFile(*out, pngData, 0o644)
	}
	return nil
}

// wins lists top-level windows of a process or with a title, visible or not, with their
// rectangles and whether they are kept out of captures.
func wins(args []string) error {
	fs := flag.NewFlagSet("wins", flag.ExitOnError)
	pid := fs.Int("pid", 0, "process id")
	title := fs.String("title", "", "title substring")
	fs.Parse(args)
	fg := windows.GetForegroundWindow()
	fmt.Printf("foreground: %#x %q\n", fg, text(uintptr(fg)))
	cb := windows.NewCallback(func(h, _ uintptr) uintptr {
		var p uint32
		windows.GetWindowThreadProcessId(windows.HWND(h), &p)
		t := text(h)
		if (*pid != 0 && int(p) != *pid) || (*title != "" && !strings.Contains(t, *title)) {
			return 1
		}
		var r struct{ L, T, R, B int32 }
		procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
		var aff uint32
		procGetWindowDisplayAffinity.Call(h, uintptr(unsafe.Pointer(&aff)))
		ex, _, _ := procGetWindowLongPtrW.Call(h, uintptr(^uintptr(19)))
		fmt.Printf("  %#x pid=%d visible=%v affinity=%#x exstyle=%#x rect=(%d,%d %dx%d) %q\n", h, p,
			windows.IsWindowVisible(windows.HWND(h)), aff, ex, r.L, r.T, r.R-r.L, r.B-r.T, t)
		return 1
	})
	return windows.EnumWindows(cb, nil)
}

func text(h uintptr) string {
	buf := make([]uint16, 256)
	procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return windows.UTF16ToString(buf)
}
