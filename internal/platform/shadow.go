package platform

import (
	"image"
	"image/color"
	"math"
)

// withShadow pads img with a transparent margin holding a soft drop shadow and rounds its
// corners the way Windows 11 draws a window's. dpi is the window's scale. It ports
// with_shadow in src/capture.rs, including the image crate's blur and Triangle resize, so a
// capture looks the same as one from the Rust build.
func withShadow(img *image.RGBA, dpi float64) *image.RGBA {
	s := float32(dpi)
	w, h := img.Rect.Dx(), img.Rect.Dy()
	radius := 8 * s
	pad := int(math.Round(float64(28 * s)))
	drop := int(math.Round(float64(6 * s)))
	ow, oh := w+pad*2, h+pad*2+drop

	const k = 4
	sw, sh := (ow+k-1)/k, (oh+k-1)/k
	mask := make([]float32, sw*sh)
	x0, y0 := pad/k, (pad+drop)/k
	for y := y0; y < y0+h/k; y++ {
		for x := x0; x < x0+w/k; x++ {
			mask[y*sw+x] = 255
		}
	}
	mask = gaussianBlur(mask, sw, sh, 14*s/k)
	big := resizeTriangle(mask, sw, sh, ow, oh)

	out := image.NewRGBA(image.Rect(0, 0, ow, oh))
	for i, v := range big {
		out.Pix[i*4+3] = uint8(float32(roundU8(v)) * 0.42)
	}
	for y := range h {
		for x := range w {
			cover := cornerCoverage(float32(x)+0.5, float32(y)+0.5, float32(w), float32(h), radius)
			if cover <= 0 {
				continue
			}
			p := img.Pix[img.PixOffset(img.Rect.Min.X+x, img.Rect.Min.Y+y):]
			d := out.Pix[out.PixOffset(x+pad, y+pad):]
			if cover == 1 && p[3] == 255 {
				copy(d[:4], p[:4])
				continue
			}
			a := cover * float32(p[3]) / 255
			below := float32(d[3]) / 255
			outA := a + below*(1-a)
			for c := range 3 {
				d[c] = uint8(math.Round(float64((float32(p[c])*a + float32(d[c])*below*(1-a)) / max(outA, 1e-6))))
			}
			d[3] = uint8(math.Round(float64(outA * 255)))
			// Rust keeps straight alpha; image.RGBA is premultiplied.
			r, g, b, _ := color.NRGBA{d[0], d[1], d[2], d[3]}.RGBA()
			d[0], d[1], d[2] = uint8(r>>8), uint8(g>>8), uint8(b>>8)
		}
	}
	return out
}

func cornerCoverage(x, y, w, h, r float32) float32 {
	var cx, cy float32
	switch {
	case x < r:
		cx = r
	case x > w-r:
		cx = w - r
	default:
		return 1
	}
	switch {
	case y < r:
		cy = r
	case y > h-r:
		cy = h - r
	default:
		return 1
	}
	d := float32(math.Hypot(float64(x-cx), float64(y-cy)))
	return min(max(r-d+0.5, 0), 1)
}

func roundU8(v float32) uint8 {
	return uint8(min(max(math.Round(float64(v)), 0), 255))
}

// gaussianBlur matches image::imageops::blur on a grey image: an odd kernel sized from
// sigma, edges clamped, both passes in float, rounded back to 8 bits.
func gaussianBlur(src []float32, w, h int, sigma float32) []float32 {
	size := int((((sigma-0.8)/0.3)+1)*2 + 1)
	size = max(size, 3)
	if size%2 == 0 {
		size++
	}
	kernel := make([]float32, size)
	mean := float32(size / 2)
	var sum float32
	scale := 1 / (float32(math.Sqrt(2*math.Pi)) * sigma)
	for i := range kernel {
		d := (float32(i) - mean) / sigma
		kernel[i] = float32(math.Exp(float64(-0.5*d*d))) * scale
		sum += kernel[i]
	}
	for i := range kernel {
		kernel[i] /= sum
	}
	half := size / 2
	tmp := make([]float32, len(src))
	for y := range h {
		row := src[y*w : (y+1)*w]
		for x := range w {
			var acc float32
			for i, kv := range kernel {
				acc += kv * row[min(max(x+i-half, 0), w-1)]
			}
			tmp[y*w+x] = acc
		}
	}
	out := make([]float32, len(src))
	for y := range h {
		for x := range w {
			var acc float32
			for i, kv := range kernel {
				acc += kv * tmp[min(max(y+i-half, 0), h-1)*w+x]
			}
			out[y*w+x] = float32(roundU8(acc))
		}
	}
	return out
}

// resizeTriangle matches image::imageops::resize with FilterType::Triangle for an upscale.
func resizeTriangle(src []float32, w, h, ow, oh int) []float32 {
	tmp := make([]float32, ow*h)
	for x := range ow {
		idx, wt := triangleTaps(x, w, ow)
		for y := range h {
			var acc float32
			for j, i := range idx {
				acc += wt[j] * src[y*w+i]
			}
			tmp[y*ow+x] = acc
		}
	}
	out := make([]float32, ow*oh)
	for y := range oh {
		idx, wt := triangleTaps(y, h, oh)
		for x := range ow {
			var acc float32
			for j, i := range idx {
				acc += wt[j] * tmp[i*ow+x]
			}
			out[y*ow+x] = acc
		}
	}
	return out
}

func triangleTaps(o, in, out int) ([]int, []float32) {
	ratio := float32(in) / float32(out)
	sratio := max(ratio, 1)
	support := sratio
	center := (float32(o) + 0.5) * ratio
	left := max(int(math.Floor(float64(center-support))), 0)
	right := min(int(math.Ceil(float64(center+support))), in)
	var idx []int
	var wt []float32
	var sum float32
	for i := left; i < right; i++ {
		v := 1 - float32(math.Abs(float64((float32(i)-center+0.5)/sratio)))
		if v <= 0 {
			continue
		}
		idx = append(idx, i)
		wt = append(wt, v)
		sum += v
	}
	for j := range wt {
		wt[j] /= sum
	}
	return idx, wt
}
