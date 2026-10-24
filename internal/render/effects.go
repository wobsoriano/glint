package render

import (
	"image"
	"math"
)

// Homography is a 3x3 projective transform, row-major:
// (x, y) -> ((a x + b y + c) / w, (d x + e y + f) / w) with w = g x + h y + i.
type Homography [9]float64

var IdentityHomography = Homography{1, 0, 0, 0, 1, 0, 0, 0, 1}

// RectToQuad maps src's corners (top-left, top-right, bottom-right, bottom-left) onto quad.
func RectToQuad(src R, quad [4]Pt) (Homography, bool) {
	if src.W <= 0 || src.H <= 0 {
		return Homography{}, false
	}
	var p [4][2]float64
	for i, q := range quad {
		p[i] = [2]float64{float64(q.X), float64(q.Y)}
	}
	dx1, dx2, dx3 := p[1][0]-p[2][0], p[3][0]-p[2][0], p[0][0]-p[1][0]+p[2][0]-p[3][0]
	dy1, dy2, dy3 := p[1][1]-p[2][1], p[3][1]-p[2][1], p[0][1]-p[1][1]+p[2][1]-p[3][1]
	var unit Homography
	if math.Abs(dx3) < 1e-9 && math.Abs(dy3) < 1e-9 {
		unit = Homography{p[1][0] - p[0][0], p[3][0] - p[0][0], p[0][0], p[1][1] - p[0][1], p[3][1] - p[0][1], p[0][1], 0, 0, 1}
	} else {
		den := dx1*dy2 - dx2*dy1
		if math.Abs(den) < 1e-12 {
			return Homography{}, false
		}
		g := (dx3*dy2 - dx2*dy3) / den
		h := (dx1*dy3 - dx3*dy1) / den
		unit = Homography{
			p[1][0] - p[0][0] + g*p[1][0], p[3][0] - p[0][0] + h*p[3][0], p[0][0],
			p[1][1] - p[0][1] + g*p[1][1], p[3][1] - p[0][1] + h*p[3][1], p[0][1],
			g, h, 1,
		}
	}
	sx, sy := 1/float64(src.W), 1/float64(src.H)
	ox, oy := float64(src.X), float64(src.Y)
	n := Homography{sx, 0, -ox * sx, 0, sy, -oy * sy, 0, 0, 1}
	return unit.thenAfter(n), true
}

func (m Homography) thenAfter(first Homography) Homography {
	var out Homography
	for r := range 3 {
		for c := range 3 {
			s := 0.0
			for k := range 3 {
				s += m[r*3+k] * first[k*3+c]
			}
			out[r*3+c] = s
		}
	}
	return out
}

func (m Homography) Apply(p Pt) Pt {
	x, y := float64(p.X), float64(p.Y)
	w := m[6]*x + m[7]*y + m[8]
	if math.Abs(w) < 1e-12 {
		return p
	}
	return Pt{float32((m[0]*x + m[1]*y + m[2]) / w), float32((m[3]*x + m[4]*y + m[5]) / w)}
}

func (m Homography) Inverse() (Homography, bool) {
	a, b, c, d, e, f, g, h, i := m[0], m[1], m[2], m[3], m[4], m[5], m[6], m[7], m[8]
	det := a*(e*i-f*h) - b*(d*i-f*g) + c*(d*h-e*g)
	if math.Abs(det) < 1e-12 {
		return Homography{}, false
	}
	r := 1 / det
	return Homography{
		(e*i - f*h) * r, (c*h - b*i) * r, (b*f - c*e) * r,
		(f*g - d*i) * r, (a*i - c*g) * r, (c*d - a*f) * r,
		(d*h - e*g) * r, (b*g - a*h) * r, (a*e - b*d) * r,
	}, true
}

// Translated moves both the input and output planes by (dx, dy).
func (m Homography) Translated(dx, dy float32) Homography {
	to := Homography{1, 0, float64(dx), 0, 1, float64(dy), 0, 0, 1}
	from := Homography{1, 0, -float64(dx), 0, 1, -float64(dy), 0, 0, 1}
	return to.thenAfter(m.thenAfter(from))
}

// WAt is the denominator at p; projected points are only meaningful where it is positive.
func (m Homography) WAt(p Pt) float64 {
	return m[6]*float64(p.X) + m[7]*float64(p.Y) + m[8]
}

func projectPoint(p Pt, card R, canvasW, canvasH float32, s Perspective) Pt {
	pivot := card.Center()
	x, y, z := float64(p.X-pivot.X), float64(p.Y-pivot.Y), 0.0
	rad := func(d float32) float64 { return float64(d) * radsPerDeg64 }
	sa, ca := math.Sincos(rad(s.RotX))
	y, z = y*ca-z*sa, y*sa+z*ca
	sb, cb := math.Sincos(rad(s.RotY))
	x, z = x*cb+z*sb, -x*sb+z*cb
	fov := min(max(float64(s.FOV), 18), 80)
	longest := float64(max(card.W, card.H))
	focal := longest * (0.35 + 0.5/max(math.Tan(fov*radsPerDeg64/2), 0.01))
	k := focal / max(focal-z, focal*0.12)
	px, py := x*k, y*k
	sr, cr := math.Sincos(rad(s.RotZ))
	rx, ry := px*cr-py*sr, px*sr+py*cr
	zoom := min(max(float64(s.Zoom), 0.2), 4)
	return Pt{pivot.X + float32(rx*zoom) + s.PanX*canvasW, pivot.Y + float32(ry*zoom) + s.PanY*canvasH}
}

// Projection maps the card plane to the mockup; card is the bordered card in canvas pixels.
// ok is false when the perspective is inactive.
func Projection(card R, canvasW, canvasH float32, s Perspective) (Homography, bool) {
	if !s.IsActive() {
		return Homography{}, false
	}
	var quad [4]Pt
	for i, c := range [4]Pt{{card.X, card.Y}, {card.Right(), card.Y}, {card.Right(), card.Bottom()}, {card.X, card.Bottom()}} {
		quad[i] = projectPoint(c, card, canvasW, canvasH, s)
	}
	return RectToQuad(card, quad)
}

// WarpOnto draws src (whose pixel (0,0) sits at srcOrigin in plane coordinates) through h onto
// dst (whose pixel (0,0) sits at dstOrigin), with bilinear sampling, source-over.
func WarpOnto(dst *image.RGBA, dstOrigin Pt, src *image.RGBA, srcOrigin Pt, h Homography) {
	inv, ok := h.Inverse()
	if !ok {
		return
	}
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	dw, dh := dst.Rect.Dx(), dst.Rect.Dy()
	corners := [4]Pt{{0, 0}, {float32(sw), 0}, {float32(sw), float32(sh)}, {0, float32(sh)}}
	allFront := true
	for i := range corners {
		corners[i] = Pt{corners[i].X + srcOrigin.X, corners[i].Y + srcOrigin.Y}
		if h.WAt(corners[i]) <= 0 {
			allFront = false
		}
	}
	x0, y0, x1, y1 := 0, 0, dw, dh
	if allFront {
		minx, miny := float32(math.MaxFloat32), float32(math.MaxFloat32)
		maxx, maxy := -float32(math.MaxFloat32), -float32(math.MaxFloat32)
		for _, c := range corners {
			q := h.Apply(c)
			minx, miny, maxx, maxy = min(minx, q.X), min(miny, q.Y), max(maxx, q.X), max(maxy, q.Y)
		}
		clampI := func(v, hi int) int { return min(max(v, 0), hi) }
		x0 = clampI(int(floor32(minx-dstOrigin.X))-1, dw)
		y0 = clampI(int(floor32(miny-dstOrigin.Y))-1, dh)
		x1 = clampI(int(ceil32(maxx-dstOrigin.X))+1, dw)
		y1 = clampI(int(ceil32(maxy-dstOrigin.Y))+1, dh)
	}
	m := inv
	for y := y0; y < y1; y++ {
		fy := float64(y) + 0.5 + float64(dstOrigin.Y)
		for x := x0; x < x1; x++ {
			fx := float64(x) + 0.5 + float64(dstOrigin.X)
			w := m[6]*fx + m[7]*fy + m[8]
			if w <= 1e-12 {
				continue
			}
			u := (m[0]*fx+m[1]*fy+m[2])/w - float64(srcOrigin.X) - 0.5
			v := (m[3]*fx+m[4]*fy+m[5])/w - float64(srcOrigin.Y) - 0.5
			if u < -1 || v < -1 || u >= float64(sw) || v >= float64(sh) {
				continue
			}
			ui, vi := int(math.Floor(u)), int(math.Floor(v))
			fu, fv := float32(u-float64(ui)), float32(v-float64(vi))
			var acc [4]float32
			for dy, wy := range [2]float32{1 - fv, fv} {
				sy := vi + dy
				if sy < 0 || sy >= sh || wy == 0 {
					continue
				}
				for dx, wx := range [2]float32{1 - fu, fu} {
					sx := ui + dx
					if sx < 0 || sx >= sw || wx == 0 {
						continue
					}
					p := src.Pix[sy*src.Stride+sx*4:]
					k := wx * wy
					for c := range 4 {
						acc[c] += float32(float32(p[c]) * k)
					}
				}
			}
			if acc[3] < 0.5 {
				continue
			}
			d := dst.Pix[y*dst.Stride+x*4:]
			ia := 1 - acc[3]/255
			for c := range 4 {
				d[c] = uint8(min(round32(acc[c]+float32(float32(d[c])*ia)), 255))
			}
		}
	}
}

// radsPerDeg64 rounds π to f64 before dividing, as Rust's f64::to_radians does.
const radsPerDeg64 = float64(math.Pi) / 180

// transitionShare evaluates 0.10 + 0.55 * 0.48 in f32 steps, as Rust folds it.
func transitionShare() float32 {
	a, b, c := float32(0.10), float32(0.55), float32(0.48)
	return a + float32(b*c)
}

// BlurRadius is the blur strength in pixels for an area whose shortest edge is shortest.
func BlurRadius(s ProgressiveBlur, shortest float32) float32 {
	return max(s.Strength*shortest/1000, 0.5)
}

// BlurField says how blurred (0..1) each point of an extent is.
type BlurField struct {
	focus      Pt
	normal     Pt
	mode       BlurMode
	focusSize  float32
	transition float32
}

func NewBlurField(extent R, s ProgressiveBlur) BlurField {
	focus := extent.Center()
	corners := [4]Pt{{extent.X, extent.Y}, {extent.Right(), extent.Y}, {extent.Right(), extent.Bottom()}, {extent.X, extent.Bottom()}}
	sin, cos := sinCos32(s.Angle)
	normal := Pt{-sin, cos}
	shortest := min(extent.W, extent.H)
	start := clamp32(s.Start, 0, 1)
	transition := shortest * transitionShare()
	var focusSize float32
	switch s.Mode {
	case BlurRadial:
		mn := shortest * 0.04
		mx := mn
		for _, c := range corners {
			mx = max(mx, c.Dist(focus))
		}
		focusSize = mn + start*(mx-mn)
	default:
		mn := shortest * 0.025
		mx := mn
		for _, c := range corners {
			mx = max(mx, abs32((c.X-focus.X)*normal.X+(c.Y-focus.Y)*normal.Y))
		}
		focusSize = mn + start*(mx-mn)
	}
	return BlurField{focus, normal, s.Mode, focusSize, max(transition, 1)}
}

func (f BlurField) At(p Pt) float32 {
	var d float32
	if f.mode == BlurRadial {
		d = p.Dist(f.focus)
	} else {
		d = abs32((p.X-f.focus.X)*f.normal.X + (p.Y-f.focus.Y)*f.normal.Y)
	}
	t := clamp32((d-f.focusSize)/f.transition, 0, 1)
	return t * t * (3 - 2*t)
}

// ProgressiveBlurArea blurs area of pm progressively. extent (in pm's coordinates) defines
// the focus geometry; mask, when non-nil, limits which pixels change.
func ProgressiveBlurArea(pm *image.RGBA, area, extent R, s ProgressiveBlur, mask *image.Alpha) {
	screen := R{0, 0, float32(pm.Rect.Dx()), float32(pm.Rect.Dy())}
	a, ok := area.RoundOut().Intersect(screen)
	if !ok {
		return
	}
	ax, ay, aw, ah := int(a.X), int(a.Y), int(a.W), int(a.H)
	if aw < 2 || ah < 2 {
		return
	}
	radius := BlurRadius(s, min(extent.W, extent.H))
	base := make([]uint8, aw*ah*4)
	for y := 0; y < ah; y++ {
		copy(base[y*aw*4:(y+1)*aw*4], pm.Pix[(ay+y)*pm.Stride+ax*4:])
	}
	levels := [][]uint8{base}
	for _, f := range []float32{0.25, 0.5, 1} {
		l := append([]uint8(nil), base...)
		BlurRGBA(l, aw, ah, int(max(round32(radius*f/1.7), 1)))
		levels = append(levels, l)
	}
	field := NewBlurField(extent, s)
	for y := 0; y < ah; y++ {
		for x := 0; x < aw; x++ {
			gx, gy := ax+x, ay+y
			t := field.At(Pt{float32(gx) + 0.5, float32(gy) + 0.5}) * 3
			if mask != nil {
				t *= float32(mask.Pix[gy*mask.Stride+gx]) / 255
			}
			if t <= 0 {
				continue
			}
			lo := min(int(floor32(t)), 2)
			fr := t - float32(lo)
			i := (y*aw + x) * 4
			d := pm.Pix[gy*pm.Stride+gx*4:]
			for c := range 4 {
				v := float32(float32(levels[lo][i+c])*(1-fr)) + float32(float32(levels[lo+1][i+c])*fr)
				d[c] = uint8(round32(v))
			}
		}
	}
}

// DrawWatermark draws the watermark over image (the screenshot rect), clipped by mask. unit
// converts points to canvas pixels.
func DrawWatermark(pm *image.RGBA, img R, unit float32, wm Watermark, mask *image.Alpha) {
	if !wm.IsVisible() {
		return
	}
	face := FontFor(wm.Font.Family, wm.Font.Weight)
	size := max(wm.Size*unit, 1)
	tw, th := face.Measure(wm.Text, size)
	color := wm.Color.WithAlpha(uint8(round32(float32(wm.Color.A()) * clamp32(wm.Opacity, 0, 1))))
	if !wm.Placement.IsTiled() {
		i := wm.Placement.Cell()
		margin := round32(min(img.W, img.H) * 0.04)
		col := float32(i % 3)
		row := float32(min(i/3, 2))
		x := img.X + margin + (img.W-2*margin-tw)*col/2
		y := img.Y + margin + (img.H-2*margin-th)*row/2
		layer := image.NewRGBA(pm.Rect)
		face.Draw(layer, wm.Text, x, y, size, wm.Font.Align, color)
		DrawPixmap(pm, 0, 0, layer, mask)
		return
	}
	gap := size * 2.2
	cw, ch := max(ceil32(tw+gap), 1), max(ceil32(th+gap*0.8), 1)
	tile := image.NewRGBA(image.Rect(0, 0, int(cw), int(ch)))
	face.Draw(tile, wm.Text, gap/2, gap*0.4, size, wm.Font.Align, color)
	c := img.Center()
	ts := Translate(c.X-cw/2, c.Y-ch/2).PostConcat(RotateAt(wm.Rotation, c.X, c.Y))
	paint := Paint{Shader: &Pattern{Image: tile, Spread: SpreadRepeat, Quality: FilterBilinear, Transform: ts}}
	if r, ok := validRect(img); ok {
		FillRect(pm, r, paint, mask)
	}
}

// PolygonMask is the anti-aliased coverage of the closed polygon through points.
func PolygonMask(w, h int, points []Pt) *image.Alpha {
	p := PolygonPath(points)
	if p == nil {
		return nil
	}
	m := NewMask(w, h)
	FillMask(m, p)
	return m
}
