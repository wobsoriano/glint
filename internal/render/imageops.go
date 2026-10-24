package render

import (
	"image"
	"image/draw"
	"math"
)

// Premultiply converts a straight-alpha image (any image.Image; *image.NRGBA is read raw) to
// a premultiplied *image.RGBA, rounding as the Rust compositor does.
func Premultiply(src image.Image) *image.RGBA {
	b := src.Bounds()
	n, ok := src.(*image.NRGBA)
	if !ok {
		if rgba, isRGBA := src.(*image.RGBA); isRGBA && rgba.Rect.Min == (image.Point{}) {
			return rgba
		}
		n = image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
		draw.Draw(n, n.Rect, src, b.Min, draw.Src)
		b = n.Rect
	}
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		s := n.Pix[n.PixOffset(b.Min.X, b.Min.Y+y):]
		d := out.Pix[y*out.Stride:]
		for x := 0; x < b.Dx()*4; x += 4 {
			a := uint16(s[x+3])
			if a == 255 {
				copy(d[x:x+4], s[x:x+4])
				continue
			}
			d[x] = uint8((uint16(s[x])*a + 127) / 255)
			d[x+1] = uint8((uint16(s[x+1])*a + 127) / 255)
			d[x+2] = uint8((uint16(s[x+2])*a + 127) / 255)
			d[x+3] = uint8(a)
		}
	}
	return out
}

// Unpremultiply converts to straight alpha the way tiny-skia's demultiply does, for saving.
func Unpremultiply(src *image.RGBA) *image.NRGBA {
	out := image.NewNRGBA(image.Rect(0, 0, src.Rect.Dx(), src.Rect.Dy()))
	for y := 0; y < src.Rect.Dy(); y++ {
		s := src.Pix[src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+y):]
		d := out.Pix[y*out.Stride:]
		for x := 0; x < src.Rect.Dx()*4; x += 4 {
			a := s[x+3]
			switch a {
			case 255:
				copy(d[x:x+4], s[x:x+4])
			case 0:
				d[x], d[x+1], d[x+2], d[x+3] = 0, 0, 0, 0
			default:
				af := float64(a) / 255
				for k := range 3 {
					d[x+k] = uint8(float64(s[x+k])/af + 0.5)
				}
				d[x+3] = a
			}
		}
	}
	return out
}

// PrepareImage crops src (straight or premultiplied, opaque screenshots do not care) to crop
// and resizes the region to w×h with a triangle filter.
func PrepareImage(src *image.RGBA, crop R, w, h int) *image.RGBA {
	x := int(max(crop.X, 0))
	y := int(max(crop.Y, 0))
	cw := max(min(int(crop.W), max(src.Rect.Dx()-x, 0)), 1)
	ch := max(min(int(crop.H), max(src.Rect.Dy()-y, 0)), 1)
	view := subImage(src, image.Rect(x, y, x+cw, y+ch))
	if cw == w && ch == h {
		return view
	}
	return Resize(view, max(w, 1), max(h, 1))
}

func subImage(src *image.RGBA, r image.Rectangle) *image.RGBA {
	r = r.Intersect(src.Rect)
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		copy(out.Pix[y*out.Stride:y*out.Stride+r.Dx()*4], src.Pix[src.PixOffset(r.Min.X, r.Min.Y+y):])
	}
	return out
}

// Resize is the image crate's resize with FilterType::Triangle: a vertical pass into floats,
// then a horizontal pass with rounding. It works on the bytes as stored.
func Resize(src *image.RGBA, nw, nh int) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	if w == 0 || h == 0 {
		return image.NewRGBA(image.Rect(0, 0, nw, nh))
	}
	if w == nw && h == nh {
		return subImage(src, src.Rect)
	}
	tmp := make([]float32, w*nh*4)
	ratio := float32(h) / float32(nh)
	sratio := max(ratio, 1)
	support := sratio
	var ws []float32
	for oy := 0; oy < nh; oy++ {
		left, n := filterWindow(&ws, oy, ratio, sratio, support, h)
		for x := 0; x < w; x++ {
			var t [4]float32
			for i, wt := range ws[:n] {
				p := src.Pix[src.PixOffset(src.Rect.Min.X+x, src.Rect.Min.Y+left+i):]
				for k := range 4 {
					t[k] += float32(float32(p[k]) * wt)
				}
			}
			copy(tmp[(oy*w+x)*4:], t[:])
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	ratio = float32(w) / float32(nw)
	sratio = max(ratio, 1)
	support = sratio
	for ox := 0; ox < nw; ox++ {
		left, n := filterWindow(&ws, ox, ratio, sratio, support, w)
		for y := 0; y < nh; y++ {
			var t [4]float32
			for i, wt := range ws[:n] {
				p := tmp[(y*w+left+i)*4:]
				for k := range 4 {
					t[k] += float32(p[k] * wt)
				}
			}
			d := out.Pix[y*out.Stride+ox*4:]
			for k := range 4 {
				d[k] = uint8(math.Round(float64(clamp32(t[k], 0, 255))))
			}
		}
	}
	return out
}

func filterWindow(ws *[]float32, out int, ratio, sratio, support float32, size int) (int, int) {
	input := (float32(out) + 0.5) * ratio
	left := int(floor32(input - support))
	left = min(max(left, 0), size-1)
	right := int(ceil32(input + support))
	right = min(max(right, left+1), size)
	input -= 0.5
	*ws = (*ws)[:0]
	sum := float32(0)
	for i := left; i < right; i++ {
		x := abs32((float32(i) - input) / sratio)
		wt := float32(0)
		if x < 1 {
			wt = 1 - x
		}
		*ws = append(*ws, wt)
		sum += wt
	}
	for i := range *ws {
		(*ws)[i] /= sum
	}
	return left, right - left
}

// Thumbnail is the image crate's thumbnail: a fast area average.
func Thumbnail(src *image.RGBA, nw, nh int) *image.RGBA {
	w, h := src.Rect.Dx(), src.Rect.Dy()
	out := image.NewRGBA(image.Rect(0, 0, nw, nh))
	if w == 0 || h == 0 {
		return out
	}
	px := func(x, y int) []uint8 { return src.Pix[src.PixOffset(src.Rect.Min.X+x, src.Rect.Min.Y+y):] }
	xr, yr := float32(w)/float32(nw), float32(h)/float32(nh)
	for oy := 0; oy < nh; oy++ {
		bottomf := float32(oy) * yr
		topf := bottomf + yr
		bottom := min(max(int(ceil32(bottomf)), 0), h-1)
		top := min(max(int(ceil32(topf)), bottom), h)
		for ox := 0; ox < nw; ox++ {
			leftf := float32(ox) * xr
			rightf := leftf + xr
			left := min(max(int(ceil32(leftf)), 0), w-1)
			right := min(max(int(ceil32(rightf)), left), w)
			var avg [4]uint8
			switch {
			case bottom != top && left != right:
				var sum [4]uint32
				for y := bottom; y < top; y++ {
					for x := left; x < right; x++ {
						p := px(x, y)
						for k := range 4 {
							sum[k] += uint32(p[k])
						}
					}
				}
				n := uint32((right - left) * (top - bottom))
				for k := range 4 {
					avg[k] = uint8(min((sum[k]+n/2)/n, 255))
				}
			case bottom != top:
				frac := (fract(leftf) + fract(rightf)) / 2
				var sl, sr [4]uint32
				for y := bottom; y < top; y++ {
					a, b := px(right-1, y), px(right, y)
					for k := range 4 {
						sl[k] += uint32(a[k])
						sr[k] += uint32(b[k])
					}
				}
				fr := frac / float32(top-bottom)
				fl := (1 - frac) / float32(top-bottom)
				for k := range 4 {
					avg[k] = uint8(float32(fl*float32(sl[k])) + float32(fr*float32(sr[k])))
				}
			case left != right:
				frac := (fract(topf) + fract(bottomf)) / 2
				var sb, st [4]uint32
				for x := left; x < right; x++ {
					a, b := px(x, top-1), px(x, top)
					for k := range 4 {
						sb[k] += uint32(a[k])
						st[k] += uint32(b[k])
					}
				}
				ft := frac / float32(right-left)
				fb := (1 - frac) / float32(right-left)
				for k := range 4 {
					avg[k] = uint8(float32(fb*float32(sb[k])) + float32(ft*float32(st[k])))
				}
			default:
				fracV := (fract(topf) + fract(bottomf)) / 2
				fracH := (fract(leftf) + fract(rightf)) / 2
				bl, tl, br, tr := px(right-1, top-1), px(right-1, top), px(right, top-1), px(right, top)
				ftr, ftl, fbr, fbl := fracV*fracH, fracV*(1-fracH), (1-fracV)*fracH, (1-fracV)*(1-fracH)
				for k := range 4 {
					avg[k] = uint8(float32(fbr*float32(br[k])) + float32(ftr*float32(tr[k])) + float32(fbl*float32(bl[k])) + float32(ftl*float32(tl[k])))
				}
			}
			copy(out.Pix[oy*out.Stride+ox*4:], avg[:])
		}
	}
	return out
}

// BlurRGBA is a fast approximate gaussian blur: three box-blur passes on interleaved RGBA.
func BlurRGBA(data []uint8, w, h, radius int) { blurChannels(data, w, h, radius, 4) }

// BlurAlpha is BlurRGBA for a single channel.
func BlurAlpha(data []uint8, w, h, radius int) { blurChannels(data, w, h, radius, 1) }

func blurChannels(data []uint8, w, h, radius, ch int) {
	if radius == 0 || w == 0 || h == 0 {
		return
	}
	tmp := make([]uint8, len(data))
	for range 3 {
		boxPass(data, tmp, w, h, radius, ch, true)
		boxPass(tmp, data, w, h, radius, ch, false)
	}
}

func boxPass(src, dst []uint8, w, h, r, ch int, horizontal bool) {
	if !horizontal {
		boxPassVertical(src, dst, w, h, r, ch)
		return
	}
	div := uint32(2*r + 1)
	var sums [4]uint32
	for o := 0; o < h; o++ {
		base := o * w * ch
		for c := 0; c < ch; c++ {
			s := uint32(src[base+c]) * uint32(r+1)
			for i := 1; i <= r; i++ {
				s += uint32(src[base+min(i, w-1)*ch+c])
			}
			sums[c] = s
		}
		if ch == 1 {
			boxRow1(src[base:base+w], dst[base:base+w], r, sums[0], div)
			continue
		}
		for i := 0; i < w; i++ {
			d := base + i*ch
			add := base + min(i+r+1, w-1)*ch
			sub := base + max(i-r, 0)*ch
			for c := 0; c < ch; c++ {
				dst[d+c] = uint8(sums[c] / div)
				sums[c] = sums[c] + uint32(src[add+c]) - uint32(src[sub+c])
			}
		}
	}
}

// boxRow1 is one single-channel row of the horizontal box pass.
func boxRow1(src, dst []uint8, r int, sum, div uint32) {
	w := len(src)
	for i := 0; i < w; i++ {
		dst[i] = uint8(sum / div)
		sum = sum + uint32(src[min(i+r+1, w-1)]) - uint32(src[max(i-r, 0)])
	}
}

// boxPassVertical is the vertical box pass with the same edge clamping, walking rows so that
// memory is read in order: one running sum per column.
func boxPassVertical(src, dst []uint8, w, h, r, ch int) {
	div := uint32(2*r + 1)
	row := w * ch
	sums := make([]uint32, row)
	for j := 0; j < row; j++ {
		sums[j] = uint32(src[j]) * uint32(r+1)
	}
	for i := 1; i <= r; i++ {
		s := src[min(i, h-1)*row:]
		for j := 0; j < row; j++ {
			sums[j] += uint32(s[j])
		}
	}
	for i := 0; i < h; i++ {
		d := dst[i*row : (i+1)*row]
		add := src[min(i+r+1, h-1)*row:]
		sub := src[max(i-r, 0)*row:]
		for j := 0; j < row; j++ {
			d[j] = uint8(sums[j] / div)
			sums[j] = sums[j] + uint32(add[j]) - uint32(sub[j])
		}
	}
}

// MakeBlurBg is the small blurred copy of a screenshot used by BlurredBackground.
func MakeBlurBg(img *image.RGBA) *image.RGBA {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	small := Thumbnail(img, max(w/12, 8), max(h/12, 8))
	pm := Premultiply(toNRGBAView(small))
	sw, sh := pm.Rect.Dx(), pm.Rect.Dy()
	BlurRGBA(pm.Pix, sw, sh, max(max(sw, sh)/20, 2))
	return pm
}

// toNRGBAView reinterprets an image's bytes as straight alpha, without conversion.
func toNRGBAView(m *image.RGBA) *image.NRGBA {
	return &image.NRGBA{Pix: m.Pix, Stride: m.Stride, Rect: m.Rect}
}

// EdgeColor is the average color around the image border, used for auto-balanced insets.
func EdgeColor(img *image.RGBA) Color {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	var r, g, b, n uint64
	add := func(x, y int) {
		p := img.Pix[img.PixOffset(img.Rect.Min.X+x, img.Rect.Min.Y+y):]
		c := demultiply(p[0], p[1], p[2], p[3])
		r += uint64(c[0])
		g += uint64(c[1])
		b += uint64(c[2])
		n++
	}
	step := max((w+h)/400, 1)
	for x := 0; x < w; x += step {
		add(x, 0)
		add(x, h-1)
	}
	for y := 0; y < h; y += step {
		add(0, y)
		add(w-1, y)
	}
	n = max(n, 1)
	return RGBA8(uint8(r/n), uint8(g/n), uint8(b/n), 255)
}

func demultiply(r, g, b, a uint8) [4]uint8 {
	switch a {
	case 255:
		return [4]uint8{r, g, b, a}
	case 0:
		return [4]uint8{}
	}
	af := float64(a) / 255
	f := func(v uint8) uint8 { return uint8(float64(v)/af + 0.5) }
	return [4]uint8{f(r), f(g), f(b), a}
}

// AutoGradient is two vivid colors derived from the screenshot, for the "Auto" gradient.
func AutoGradient(img *image.RGBA) []Color {
	small := Thumbnail(img, 24, 24)
	pick := func(x0, x1 int) Color {
		best, score := RGB(0x6366F1), float32(-1)
		for y := 0; y < small.Rect.Dy(); y++ {
			for x := x0; x < x1; x++ {
				p := small.Pix[small.PixOffset(x, y):]
				c := RGBA8(p[0], p[1], p[2], 255)
				_, s, l := c.HSL()
				if sc := s * (1 - abs32(l-0.5)*1.6); sc > score {
					best, score = c, sc
				}
			}
		}
		h, s, _ := best.HSL()
		return FromHSL(h, max(s, 0.55), 0.6)
	}
	mid := small.Rect.Dx() / 2
	a := pick(0, mid)
	b := pick(mid, small.Rect.Dx())
	if a == b {
		h, s, l := a.HSL()
		b = FromHSL(h+40, s, l)
	}
	return []Color{a, b}
}
