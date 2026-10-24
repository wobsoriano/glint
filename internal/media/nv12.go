package media

import (
	"image"
	"runtime"
	"sync"
)

// Nv12Frame is one decoded frame in NV12: Height rows of luma, then (Height+1)/2 rows of
// interleaved Cb Cr, every row Stride bytes.
type Nv12Frame struct {
	// TMS is the presentation time in media milliseconds.
	TMS           int64
	Width, Height int
	Stride        int
	Data          []byte
	matrix        yuvMatrix
}

// yuvMatrix turns Y'CbCr into R'G'B' in 16.16 fixed point: rgb = yScale*(y-yOff) + the
// chroma terms, with chroma centred on 128.
type yuvMatrix struct {
	yScale, yOff   int32
	rv, gu, gv, bu int32
}

var (
	bt709Limited = yuvMatrix{yScale: 76_309, yOff: 16, rv: 117_489, gu: -13_975, gv: -34_925, bu: 138_438}
	bt601Limited = yuvMatrix{yScale: 76_309, yOff: 16, rv: 104_597, gu: -25_675, gv: -53_279, bu: 132_201}
	bt709Full    = yuvMatrix{yScale: 65_536, yOff: 0, rv: 103_206, gu: -12_276, gv: -30_679, bu: 121_609}
	bt601Full    = yuvMatrix{yScale: 65_536, yOff: 0, rv: 91_881, gu: -22_553, gv: -46_802, bu: 116_130}
)

// pickMatrix follows the stream's tags. Untagged streams get what Media Foundation assumes:
// BT.601 below 720 rows, BT.709 from there, limited range.
func pickMatrix(bt709, bt601, full bool, height int) yuvMatrix {
	use709 := bt709 || (!bt601 && height >= 720)
	switch {
	case use709 && full:
		return bt709Full
	case use709:
		return bt709Limited
	case full:
		return bt601Full
	}
	return bt601Limited
}

var nv12Pool sync.Pool

func takeBuffer(n int) []byte {
	if b, ok := nv12Pool.Get().(*[]byte); ok && cap(*b) >= n {
		return (*b)[:n]
	}
	return make([]byte, n)
}

// Release hands the frame's memory back for the next decoded frame. The frame is unusable
// afterwards. Frames that are never released are collected as usual.
func (f *Nv12Frame) Release() {
	if f.Data != nil {
		b := f.Data
		nv12Pool.Put(&b)
		f.Data = nil
	}
}

// ToRGBA converts to opaque RGBA, splitting rows across all cores. dst is reused when it has
// the frame's size.
func (f *Nv12Frame) ToRGBA(dst *image.RGBA) *image.RGBA {
	w, h := f.Width, f.Height
	if dst == nil || dst.Rect.Dx() != w || dst.Rect.Dy() != h {
		dst = image.NewRGBA(image.Rect(0, 0, w, h))
	}
	luma, chroma := f.Data[:f.Stride*h], f.Data[f.Stride*h:]
	m := f.matrix
	parallelRows(h, 2, func(y0, y1 int) {
		for y := y0; y < y1; y++ {
			yrow := luma[y*f.Stride : y*f.Stride+w]
			uvrow := chroma[(y/2)*f.Stride : (y/2)*f.Stride+(w+1)&^1]
			out := dst.Pix[y*dst.Stride : y*dst.Stride+w*4]
			for x := 0; x < w; x += 2 {
				u, v := int32(uvrow[x])-128, int32(uvrow[x+1])-128
				dr, dg, db := m.rv*v+32_768, m.gu*u+m.gv*v+32_768, m.bu*u+32_768
				for k := x; k < min(x+2, w); k++ {
					c := m.yScale * (int32(yrow[k]) - m.yOff)
					p := out[k*4 : k*4+4 : k*4+4]
					p[0] = clamp8((c + dr) >> 16)
					p[1] = clamp8((c + dg) >> 16)
					p[2] = clamp8((c + db) >> 16)
					p[3] = 255
				}
			}
		}
	})
	return dst
}

func clamp8(v int32) uint8 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return uint8(v)
}

// BT.709 limited-range forward coefficients in 16.16 fixed point.
const (
	yR, yG, yB    = 11_966, 40_254, 4_064
	cbR, cbG, cbB = -6_596, -22_189, 28_784
	crR, crG, crB = 28_784, -26_145, -2_639
)

// pixelOrder says where red sits in each 4-byte pixel.
type pixelOrder int

const (
	orderRGBA pixelOrder = iota
	orderBGRA
)

// toNV12 converts even-sized 8-bit RGBA or BGRA rows, stride bytes apart, into NV12 with
// BT.709 limited range: w*h luma bytes in dst, then interleaved chroma averaged over each
// 2x2 block. Alpha is ignored.
func toNV12(px []byte, stride, w, h int, order pixelOrder, dst []byte) {
	// Swapping the red and blue coefficients handles BGRA with the same loop.
	c0, c2 := [3]int32{yR, cbR, crR}, [3]int32{yB, cbB, crB}
	if order == orderBGRA {
		c0, c2 = c2, c0
	}
	luma, chroma := dst[:w*h], dst[w*h:]
	parallelRows(h, 2, func(y0, y1 int) {
		for y := y0; y < y1; y += 2 {
			top := px[y*stride : y*stride+w*4]
			bot := px[(y+1)*stride : (y+1)*stride+w*4]
			ly0 := luma[y*w : y*w+w]
			ly1 := luma[(y+1)*w : (y+1)*w+w]
			uv := chroma[(y/2)*w : (y/2)*w+w]
			for x := 0; x+1 < w; x += 2 {
				a := top[x*4 : x*4+8 : x*4+8]
				b := bot[x*4 : x*4+8 : x*4+8]
				ar0, ag0, ab0 := int32(a[0]), int32(a[1]), int32(a[2])
				ar1, ag1, ab1 := int32(a[4]), int32(a[5]), int32(a[6])
				br0, bg0, bb0 := int32(b[0]), int32(b[1]), int32(b[2])
				br1, bg1, bb1 := int32(b[4]), int32(b[5]), int32(b[6])
				ly0[x] = uint8(16 + (c0[0]*ar0+yG*ag0+c2[0]*ab0+32_768)>>16)
				ly0[x+1] = uint8(16 + (c0[0]*ar1+yG*ag1+c2[0]*ab1+32_768)>>16)
				ly1[x] = uint8(16 + (c0[0]*br0+yG*bg0+c2[0]*bb0+32_768)>>16)
				ly1[x+1] = uint8(16 + (c0[0]*br1+yG*bg1+c2[0]*bb1+32_768)>>16)
				s0, s1, s2 := ar0+ar1+br0+br1, ag0+ag1+bg0+bg1, ab0+ab1+bb0+bb1
				uv[x] = uint8(128 + (c0[1]*s0+cbG*s1+c2[1]*s2+1<<17)>>18)
				uv[x+1] = uint8(128 + (c0[2]*s0+crG*s1+c2[2]*s2+1<<17)>>18)
			}
		}
	})
}

// parallelRows calls fn on disjoint bands of [0, rows) on every core, each band starting on
// a multiple of align.
func parallelRows(rows, align int, fn func(y0, y1 int)) {
	workers := runtime.GOMAXPROCS(0)
	per := (rows + workers - 1) / workers
	per = (per + align - 1) / align * align
	if per <= 0 || per >= rows {
		fn(0, rows)
		return
	}
	var wg sync.WaitGroup
	for y := 0; y < rows; y += per {
		wg.Add(1)
		go func(y0, y1 int) {
			defer wg.Done()
			fn(y0, y1)
		}(y, min(y+per, rows))
	}
	wg.Wait()
}
