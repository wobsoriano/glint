package input

import (
	"unsafe"
)

var (
	procGetIconInfo        = user32.NewProc("GetIconInfo")
	procDrawIconEx         = user32.NewProc("DrawIconEx")
	procGetObjectW         = gdi32.NewProc("GetObjectW")
	procCreateCompatibleDC = gdi32.NewProc("CreateCompatibleDC")
	procCreateDIBSection   = gdi32.NewProc("CreateDIBSection")
	procSelectObject       = gdi32.NewProc("SelectObject")
	procDeleteObject       = gdi32.NewProc("DeleteObject")
	procDeleteDC           = gdi32.NewProc("DeleteDC")
)

type iconInfo struct {
	fIcon              int32
	xHotspot, yHotspot uint32
	hbmMask, hbmColor  uintptr
}

type bitmap struct {
	bmType, bmWidth, bmHeight, bmWidthBytes int32
	bmPlanes, bmBitsPixel                   uint16
	bmBits                                  uintptr
}

type bitmapInfoHeader struct {
	biSize                           uint32
	biWidth, biHeight                int32
	biPlanes, biBitCount             uint16
	biCompression, biSizeImage       uint32
	biXPelsPerMeter, biYPelsPerMeter int32
	biClrUsed, biClrImportant        uint32
}

type bitmapInfo struct {
	header bitmapInfoHeader
	colors [1]uint32
}

// renderCursor draws a cursor on black and on white and recovers straight RGBA from the
// pair, which works for colour, alpha and monochrome (mask-only) cursors alike.
func renderCursor(cursor uintptr) (cursorImage, bool) {
	var info iconInfo
	if r, _, _ := procGetIconInfo.Call(cursor, uintptr(unsafe.Pointer(&info))); r == 0 {
		return cursorImage{}, false
	}
	defer func() {
		for _, h := range []uintptr{info.hbmColor, info.hbmMask} {
			if h != 0 {
				procDeleteObject.Call(h)
			}
		}
	}()
	w, h := bitmapSize(info)
	if w <= 0 || h <= 0 {
		return cursorImage{}, false
	}
	onBlack, ok1 := drawCursor(cursor, w, h, 0x00)
	onWhite, ok2 := drawCursor(cursor, w, h, 0xFF)
	if !ok1 || !ok2 {
		return cursorImage{}, false
	}
	return cursorImage{width: w, height: h, hotspot: [2]uint32{info.xHotspot, info.yHotspot}, rgba: mergeOnBlackWhite(onBlack, onWhite)}, true
}

// bitmapSize is the colour bitmap's size, or half the mask's height for a monochrome cursor,
// whose mask stacks the AND and XOR halves.
func bitmapSize(info iconInfo) (int, int) {
	handle, halve := info.hbmColor, false
	if handle == 0 {
		handle, halve = info.hbmMask, true
	}
	var bm bitmap
	if r, _, _ := procGetObjectW.Call(handle, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm))); r == 0 {
		return 0, 0
	}
	h := bm.bmHeight
	if halve {
		h /= 2
	}
	return int(max(bm.bmWidth, 0)), int(max(h, 0))
}

func drawCursor(cursor uintptr, w, h int, fill byte) ([]byte, bool) {
	dc, _, _ := procCreateCompatibleDC.Call(0)
	if dc == 0 {
		return nil, false
	}
	defer procDeleteDC.Call(dc)
	bi := bitmapInfo{header: bitmapInfoHeader{
		biSize: uint32(unsafe.Sizeof(bitmapInfoHeader{})), biWidth: int32(w), biHeight: -int32(h),
		biPlanes: 1, biBitCount: 32,
	}}
	var bits *byte
	const dibRGBColors = 0
	dib, _, _ := procCreateDIBSection.Call(dc, uintptr(unsafe.Pointer(&bi)), dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return nil, false
	}
	defer procDeleteObject.Call(dib)
	pix := unsafe.Slice(bits, w*h*4)
	for i := range pix {
		pix[i] = fill
	}
	old, _, _ := procSelectObject.Call(dc, dib)
	const diNormal = 3
	r, _, _ := procDrawIconEx.Call(dc, 0, 0, cursor, uintptr(w), uintptr(h), 0, 0, diNormal)
	procSelectObject.Call(dc, old)
	if r == 0 {
		return nil, false
	}
	return append([]byte(nil), pix...), true
}

// mergeOnBlackWhite turns BGRA drawn over black and over white into straight RGBA: the
// difference between the two backgrounds is 255 minus alpha, and the black one holds the
// premultiplied colour.
func mergeOnBlackWhite(onBlack, onWhite []byte) []byte {
	out := make([]byte, 0, len(onBlack))
	for i := 0; i+4 <= len(onBlack); i += 4 {
		b, w := onBlack[i:i+4], onWhite[i:i+4]
		alpha := 255 - min(max(int(w[0])-int(b[0]), 0), 255)
		unpremul := func(c byte) byte {
			if alpha == 0 {
				return 0
			}
			return byte(min(int(c)*255/alpha, 255))
		}
		out = append(out, unpremul(b[2]), unpremul(b[1]), unpremul(b[0]), byte(alpha))
	}
	return out
}
