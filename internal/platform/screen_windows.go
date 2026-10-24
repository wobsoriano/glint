package platform

import (
	"errors"
	"fmt"
	"image"
	"slices"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The OS allows a limited number of callbacks per process, so each enumeration has one,
// fed through a sink guarded by a lock.
var (
	enumMu       sync.Mutex
	enumSink     func(uintptr) bool
	enumCallback = windows.NewCallback(func(h, _, _, _ uintptr) uintptr {
		if enumSink(h) {
			return 1
		}
		return 0
	})
	enumWindowsCallback = windows.NewCallback(func(h, _ uintptr) uintptr {
		if enumSink(h) {
			return 1
		}
		return 0
	})
)

func enumMonitors() []uintptr {
	enumMu.Lock()
	defer enumMu.Unlock()
	var out []uintptr
	enumSink = func(h uintptr) bool { out = append(out, h); return true }
	procEnumDisplayMonitors.Call(0, 0, enumCallback, 0)
	enumSink = nil
	return out
}

func Displays() ([]Display, error) {
	var out []Display
	for _, m := range enumMonitors() {
		info := monitorInfoEx{Size: uint32(unsafe.Sizeof(monitorInfoEx{}))}
		if _, err := call(procGetMonitorInfoW, m, uintptr(unsafe.Pointer(&info))); err != nil {
			return nil, err
		}
		var dx, dy uint32
		scale := 1.0
		if r, _, _ := procGetDpiForMonitor.Call(m, mdtEffectiveDPI, uintptr(unsafe.Pointer(&dx)), uintptr(unsafe.Pointer(&dy))); r == 0 && dx > 0 {
			scale = float64(dx) / 96
		}
		out = append(out, Display{
			ID:      int64(m),
			Bounds:  info.Monitor.toRect(),
			Scale:   scale,
			Primary: info.Flags&monitorInfoPrimary != 0,
		})
	}
	if len(out) == 0 {
		return nil, errors.New("platform: no displays")
	}
	slices.SortFunc(out, func(a, b Display) int {
		if a.Bounds.X != b.Bounds.X {
			return a.Bounds.X - b.Bounds.X
		}
		return a.Bounds.Y - b.Bounds.Y
	})
	for i := range out {
		out[i].Name = fmt.Sprintf("Display %d", i+1)
	}
	slices.SortStableFunc(out, func(a, b Display) int {
		switch {
		case a.Primary == b.Primary:
			return 0
		case a.Primary:
			return -1
		}
		return 1
	})
	return out, nil
}

func CursorPos() (int, int) {
	var p struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return int(p.X), int(p.Y)
}

// CaptureRect reads the screen DC with GDI, as the Rust build did through xcap. On an HDR
// desktop GDI gave the right SDR colours where Desktop Duplication (ffmpeg's ddagrab) did
// not. The screen DC spans the virtual desktop, so one blit covers every monitor.
func CaptureRect(r Rect) (*image.RGBA, error) {
	if r.Empty() {
		return nil, errors.New("platform: empty capture rect")
	}
	screen, err := call(procGetDC, 0)
	if err != nil {
		return nil, err
	}
	defer procReleaseDC.Call(0, screen)
	return drawDIB(screen, r.W, r.H, func(dc uintptr) error {
		_, err := call(procBitBlt, dc, 0, 0, uintptr(r.W), uintptr(r.H), screen, uintptr(r.X), uintptr(r.Y), srcCopy|captureBlt)
		return err
	})
}

// CaptureWindow renders the window with PrintWindow, so windows covering it do not show,
// then crops the invisible resize borders off.
func CaptureWindow(id int64, shadow bool) (*image.RGBA, error) {
	hwnd := uintptr(id)
	if !windows.IsWindow(windows.HWND(hwnd)) {
		return nil, fmt.Errorf("platform: no window %#x", id)
	}
	if iconic, _, _ := procIsIconic.Call(hwnd); iconic != 0 {
		return nil, fmt.Errorf("platform: window %#x is minimized", id)
	}
	outer, err := windowRect(hwnd)
	if err != nil {
		return nil, err
	}
	frame, err := frameBounds(hwnd)
	if err != nil {
		frame = outer
	}
	full := outer.toRect()
	if full.Empty() {
		return nil, fmt.Errorf("platform: window %#x has no size", id)
	}
	screen, err := call(procGetDC, 0)
	if err != nil {
		return nil, err
	}
	defer procReleaseDC.Call(0, screen)
	img, err := drawDIB(screen, full.W, full.H, func(dc uintptr) error {
		_, err := call(procPrintWindow, hwnd, dc, pwRenderFullContent)
		return err
	})
	if err != nil {
		return nil, err
	}
	crop := frame.toRect().Intersect(full)
	if crop.Empty() {
		return nil, fmt.Errorf("platform: window %#x has no visible frame", id)
	}
	sub := img.SubImage(image.Rect(crop.X-full.X, crop.Y-full.Y, crop.X-full.X+crop.W, crop.Y-full.Y+crop.H)).(*image.RGBA)
	if shadow {
		return withShadow(sub, windowScale(hwnd)), nil
	}
	out := image.NewRGBA(image.Rect(0, 0, crop.W, crop.H))
	for y := range crop.H {
		copy(out.Pix[y*out.Stride:(y+1)*out.Stride], sub.Pix[y*sub.Stride:])
	}
	return out, nil
}

// drawDIB lets draw paint into a w×h top-down 32-bit DIB and returns it as opaque RGBA.
func drawDIB(compat uintptr, w, h int, draw func(dc uintptr) error) (*image.RGBA, error) {
	dc, err := call(procCreateCompatibleDC, compat)
	if err != nil {
		return nil, err
	}
	defer procDeleteDC.Call(dc)
	bi := bitmapInfoHeader{
		Width:    int32(w),
		Height:   -int32(h),
		Planes:   1,
		BitCount: 32,
	}
	bi.Size = uint32(unsafe.Sizeof(bi))
	var bits unsafe.Pointer
	bmp, err := call(procCreateDIBSection, dc, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if err != nil {
		return nil, err
	}
	defer procDeleteObject.Call(bmp)
	old, _, _ := procSelectObject.Call(dc, bmp)
	defer procSelectObject.Call(dc, old)
	if err := draw(dc); err != nil {
		return nil, err
	}
	procGdiFlush.Call()

	img := image.NewRGBA(image.Rect(0, 0, w, h))
	src := unsafe.Slice((*uint32)(bits), w*h)
	dst := unsafe.Slice((*uint32)(unsafe.Pointer(&img.Pix[0])), w*h)
	for i, v := range src {
		// BGRA in memory reads as 0xAARRGGBB; swap red and blue and force opaque.
		dst[i] = 0xFF000000 | v&0x0000FF00 | (v>>16)&0xFF | (v&0xFF)<<16
	}
	return img, nil
}
