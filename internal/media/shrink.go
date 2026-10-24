package media

import (
	"image"

	"golang.org/x/image/draw"
)

// Shrink is an area-average downscale to w x h, at most the source size, split across all
// cores. A 4K frame takes a few milliseconds.
func Shrink(src *image.RGBA, w, h int) *image.RGBA {
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	base := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y)
	span := func(i, n, of int) (int, int) {
		a := i * of / n
		return a, min(max((i+1)*of/n, a+1), of)
	}
	cols := make([][2]int, w)
	for x := range cols {
		cols[x][0], cols[x][1] = span(x, w, sw)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	parallelRows(h, 1, func(r0, r1 int) {
		acc := make([]uint32, sw*4)
		for r := r0; r < r1; r++ {
			y0, y1 := span(r, h, sh)
			clear(acc)
			for y := y0; y < y1; y++ {
				row := src.Pix[base+y*src.Stride : base+y*src.Stride+sw*4]
				for i, v := range row {
					acc[i] += uint32(v)
				}
			}
			out := dst.Pix[r*dst.Stride : r*dst.Stride+w*4]
			for x, c := range cols {
				n := uint32((c[1] - c[0]) * (y1 - y0))
				for k := 0; k < 4; k++ {
					var sum uint32
					for sx := c[0]; sx < c[1]; sx++ {
						sum += acc[sx*4+k]
					}
					out[x*4+k] = uint8((sum + n/2) / n)
				}
			}
		}
	})
	return dst
}

// resize scales src to w x h: an area average when both sides shrink, bilinear otherwise.
func resize(src *image.RGBA, w, h int) *image.RGBA {
	if w <= src.Rect.Dx() && h <= src.Rect.Dy() {
		return Shrink(src, w, h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(dst, dst.Rect, src, src.Rect, draw.Src, nil)
	return dst
}
