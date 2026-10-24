package render

import (
	"image"
	"math"
)

// Paint is what a fill deposits: Color when Shader is nil, otherwise the shader's colors.
// Blending is always source-over.
type Paint struct {
	Color  Color
	Shader Shader
}

// Shader is a *LinearGradient or a *Pattern.
type Shader interface{ isShader() }

func Solid(c Color) Paint { return Paint{Color: c} }

func (p Paint) withOpacity(o float32) Paint {
	if p.Shader == nil {
		a := float32(p.Color.A()) / 255 * o
		return Paint{Color: p.Color.WithAlpha(uint8(a*255 + 0.5))}
	}
	if pt, ok := p.Shader.(*Pattern); ok {
		q := *pt
		q.Opacity = pt.opacity() * o
		return Paint{Shader: &q}
	}
	return p
}

// LinearGradient pads its evenly spaced stops from Start to End, in the space Transform maps
// to canvas pixels.
type LinearGradient struct {
	Start, End Pt
	Stops      []Color
	Transform  Transform
}

type FilterQuality uint8

const (
	FilterNearest FilterQuality = iota
	FilterBilinear
	FilterBicubic
)

type SpreadMode uint8

const (
	SpreadPad SpreadMode = iota
	SpreadRepeat
)

// Pattern paints an image placed on the canvas by Transform.
type Pattern struct {
	Image     *image.RGBA
	Spread    SpreadMode
	Quality   FilterQuality
	Opacity   float32 // 0 means 1
	Transform Transform
}

func (*LinearGradient) isShader() {}
func (*Pattern) isShader()        {}

func (p *Pattern) opacity() float32 {
	if p.Opacity == 0 {
		return 1
	}
	return p.Opacity
}

// gradientCtx is tiny-skia's evaluated gradient: t -> bias + factor*t per stop interval.
type gradientCtx struct {
	ts       Transform
	tValues  []float32
	factors  [][4]float32
	biases   [][4]float32
	twoStops bool
	opaque   bool
}

func (g *LinearGradient) ctx() (*gradientCtx, Color, bool) {
	if len(g.Stops) == 0 {
		return nil, 0, false
	}
	if len(g.Stops) == 1 {
		return nil, g.Stops[0], false
	}
	vx, vy := g.End.X-g.Start.X, g.End.Y-g.Start.Y
	length := sqrt32(vx*vx + vy*vy)
	if length <= 1.0/(1<<15) {
		return nil, g.Stops[len(g.Stops)-1], false
	}
	inv := float32(0)
	if length != 0 {
		inv = 1 / length
	}
	vx, vy = vx*inv, vy*inv
	sin, cos := -vy, vx
	cosInv := 1 - cos
	unit := TransformFromRow(cos, sin, -sin, cos, float32(sin*g.Start.Y)+float32(cosInv*g.Start.X), float32(-sin*g.Start.X)+float32(cosInv*g.Start.Y))
	unit = unit.PostTranslate(-g.Start.X, -g.Start.Y).PostScale(inv, inv)
	shaderInv, ok := g.Transform.Invert()
	if !ok {
		return nil, 0, false
	}
	c := &gradientCtx{ts: shaderInv.PostConcat(unit), opaque: true}
	col := func(cc Color) [4]float32 {
		return [4]float32{float32(cc.R()) / 255, float32(cc.G()) / 255, float32(cc.B()) / 255, float32(cc.A()) / 255}
	}
	n := len(g.Stops) - 1
	pos := make([]float32, len(g.Stops))
	for i := range pos {
		pos[i] = float32(i) / float32(n)
		if g.Stops[i].A() != 255 {
			c.opaque = false
		}
	}
	pos[n] = 1
	if len(g.Stops) == 2 {
		c0, c1 := col(g.Stops[0]), col(g.Stops[1])
		c.twoStops = true
		c.factors = [][4]float32{{c1[0] - c0[0], c1[1] - c0[1], c1[2] - c0[2], c1[3] - c0[3]}}
		c.biases = [][4]float32{c0}
		return c, 0, true
	}
	first, last := 0, len(g.Stops)-1
	if g.Stops[0] == g.Stops[1] {
		first = 1
	}
	if g.Stops[last-1] == g.Stops[last] {
		last--
	}
	tl, cl := pos[first], col(g.Stops[first])
	c.factors = append(c.factors, [4]float32{})
	c.biases = append(c.biases, cl)
	c.tValues = append(c.tValues, 0)
	for i := first; i < last; i++ {
		tr, cr := pos[i+1], col(g.Stops[i+1])
		if tl < tr {
			var f, b [4]float32
			for k := range 4 {
				f[k] = (cr[k] - cl[k]) / (tr - tl)
				b[k] = cl[k] - float32(f[k]*tl)
			}
			c.factors = append(c.factors, f)
			c.biases = append(c.biases, b)
			c.tValues = append(c.tValues, tl)
		}
		tl, cl = tr, cr
	}
	c.factors = append(c.factors, [4]float32{})
	c.biases = append(c.biases, cl)
	c.tValues = append(c.tValues, tl)
	return c, 0, true
}

// rowTerm is the part of the gradient's t that is constant along row y (pixel center).
func (c *gradientCtx) rowTerm(y float32) float32 { return float32(y*c.ts.KX) + c.ts.TX }

// color evaluates the lowp gradient stages at a pixel center x on a row with rowTerm rt.
func (c *gradientCtx) color(x, rt float32) [4]uint16 {
	t := float32(x*c.ts.SX) + rt
	t = clamp32(t, 0, 1)
	var f, b [4]float32
	if c.twoStops {
		f, b = c.factors[0], c.biases[0]
	} else {
		idx := 0
		for i := 1; i < len(c.tValues); i++ {
			if t >= c.tValues[i] {
				idx++
			}
		}
		f, b = c.factors[idx], c.biases[idx]
	}
	var out [4]uint16
	for k := range 3 {
		out[k] = uint16(clamp32(float32(t*f[k])+b[k], 0, 1)*255 + 0.5)
	}
	a := float32(t*f[3]) + b[3]
	out[3] = uint16(max(a*255+0.5, 0))
	if !c.opaque {
		for k := range 3 {
			out[k] = div255(out[k] * out[3])
		}
	}
	return out
}

// blit composites paint through cov (nil means full coverage) and mask onto dst. Solid colors
// and gradients follow tiny-skia's 8-bit lowp pipeline, patterns its float highp pipeline.
func blit(dst *image.RGBA, cov coverage, paint Paint, mask *image.Alpha) {
	rect := cov.rect.Intersect(dst.Rect)
	if rect.Empty() {
		return
	}
	switch sh := paint.Shader.(type) {
	case *Pattern:
		blitPattern(dst, cov, rect, sh, mask)
		return
	case *LinearGradient:
		ctx, solid, ok := sh.ctx()
		if !ok {
			if ctx == nil && solid != 0 {
				blitLowp(dst, cov, rect, nil, solid.premul(), solid.A() == 255, mask)
			}
			return
		}
		blitLowp(dst, cov, rect, ctx, [4]uint16{}, ctx.opaque, mask)
		return
	}
	blitLowp(dst, cov, rect, nil, paint.Color.premul(), paint.Color.A() == 255, mask)
}

func blitLowp(dst *image.RGBA, cov coverage, rect image.Rectangle, g *gradientCtx, solid [4]uint16, opaque bool, mask *image.Alpha) {
	// tiny-skia strength-reduces source-over to source for opaque paint without a clip mask.
	source := opaque && mask == nil
	cw := cov.rect.Dx()
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		px := dst.Pix[dst.PixOffset(rect.Min.X, y):]
		var mrow []uint8
		if mask != nil {
			mrow = mask.Pix[mask.PixOffset(rect.Min.X, y):]
		}
		var crow []uint8
		if cov.a != nil {
			crow = cov.a[(y-cov.rect.Min.Y)*cw+(rect.Min.X-cov.rect.Min.X):]
		}
		var rt float32
		if g != nil {
			rt = g.rowTerm(float32(y) + 0.5)
		}
		for i := 0; i < rect.Dx(); i++ {
			c := uint16(255)
			if crow != nil {
				c = uint16(crow[i])
				if c == 0 {
					continue
				}
			}
			s := solid
			if g != nil {
				s = g.color(float32(rect.Min.X+i)+0.5, rt)
			}
			d := px[i*4 : i*4+4 : i*4+4]
			if source {
				if c == 255 {
					d[0], d[1], d[2], d[3] = uint8(s[0]), uint8(s[1]), uint8(s[2]), uint8(s[3])
					continue
				}
				for k := range 4 {
					d[k] = uint8(div255(uint16(d[k])*(255-c) + s[k]*c))
				}
				continue
			}
			if mrow != nil {
				m := uint16(mrow[i])
				if m == 0 {
					continue
				}
				for k := range 4 {
					s[k] = div255(s[k] * m)
				}
			}
			if c != 255 {
				for k := range 4 {
					s[k] = div255(s[k] * c)
				}
			}
			ia := 255 - s[3]
			for k := range 4 {
				d[k] = uint8(s[k] + div255(uint16(d[k])*ia))
			}
		}
	}
}

func blitPattern(dst *image.RGBA, cov coverage, rect image.Rectangle, p *Pattern, mask *image.Alpha) {
	inv, ok := p.Transform.Invert()
	if !ok || p.Image == nil || p.Image.Rect.Empty() {
		return
	}
	quality := p.Quality
	if inv.IsIdentity() || inv.isTranslate() {
		quality = FilterNearest
	}
	sm := newSampler(p.Image, p.Spread)
	opacity := p.opacity()
	cw := cov.rect.Dx()
	const factor = float32(1.0 / 255.0)
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		px := dst.Pix[dst.PixOffset(rect.Min.X, y):]
		var mrow []uint8
		if mask != nil {
			mrow = mask.Pix[mask.PixOffset(rect.Min.X, y):]
		}
		var crow []uint8
		if cov.a != nil {
			crow = cov.a[(y-cov.rect.Min.Y)*cw+(rect.Min.X-cov.rect.Min.X):]
		}
		fy := float32(y) + 0.5
		for i := 0; i < rect.Dx(); i++ {
			c := uint8(255)
			if crow != nil {
				c = crow[i]
				if c == 0 {
					continue
				}
			}
			var m uint8 = 255
			if mrow != nil {
				m = mrow[i]
				if m == 0 {
					continue
				}
			}
			q := inv.Apply(Pt{float32(rect.Min.X+i) + 0.5, fy})
			var s [4]float32
			switch quality {
			case FilterNearest:
				s = sm.gather(sm.tile(q.X, true), sm.tile(q.Y, false))
			case FilterBilinear:
				s = sm.bilinear(q.X, q.Y)
			default:
				s = sm.bicubic(q.X, q.Y)
			}
			if opacity != 1 {
				for k := range 4 {
					s[k] *= opacity
				}
			}
			d := px[i*4 : i*4+4 : i*4+4]
			if m == 255 && c == 255 && s[3] >= 1 {
				for k := range 4 {
					d[k] = unnorm(s[k])
				}
				continue
			}
			if m != 255 {
				mf := float32(m) / 255
				for k := range 4 {
					s[k] *= mf
				}
			}
			if c != 255 {
				cf := float32(c) * factor
				for k := range 4 {
					s[k] *= cf
				}
			}
			ia := 1 - s[3]
			for k := range 4 {
				d[k] = unnorm(float32(float32(d[k])*factor*ia) + s[k])
			}
		}
	}
}

// unnorm is tiny-skia's float-to-byte store: clamp, scale, round half to even.
func unnorm(v float32) uint8 {
	if v <= 0 {
		return 0
	}
	if v >= 1 {
		return 255
	}
	x := v * 255
	i := int32(x)
	if f := x - float32(i); f > 0.5 || f == 0.5 && i&1 == 1 {
		i++
	}
	return uint8(i)
}

type sampler struct {
	img    *image.RGBA
	w, h   int
	spread SpreadMode
}

func newSampler(img *image.RGBA, spread SpreadMode) sampler {
	return sampler{img, img.Rect.Dx(), img.Rect.Dy(), spread}
}

func (s sampler) tile(v float32, isX bool) float32 {
	if s.spread != SpreadRepeat {
		return v
	}
	limit := float32(s.h)
	if isX {
		limit = float32(s.w)
	}
	return v - floor32(v*(1/limit))*limit
}

func ulpSub(v float32) float32 { return math.Float32frombits(math.Float32bits(v) - 1) }

func (s sampler) gather(x, y float32) [4]float32 {
	x = min(max(x, 0), ulpSub(float32(s.w)))
	y = min(max(y, 0), ulpSub(float32(s.h)))
	i := int(y)*s.img.Stride + int(x)*4
	p := s.img.Pix[i : i+4 : i+4]
	const f = float32(1.0 / 255.0)
	return [4]float32{float32(p[0]) * f, float32(p[1]) * f, float32(p[2]) * f, float32(p[3]) * f}
}

func fract(v float32) float32 { return v - floor32(v) }

func (s sampler) bilinear(cx, cy float32) [4]float32 {
	fx, fy := fract(cx+0.5), fract(cy+0.5)
	wx := [2]float32{1 - fx, fx}
	wy := [2]float32{1 - fy, fy}
	var acc [4]float32
	y := cy - 0.5
	for j := range 2 {
		x := cx - 0.5
		for i := range 2 {
			p := s.gather(s.tile(x, true), s.tile(y, false))
			w := wx[i] * wy[j]
			for k := range 4 {
				acc[k] = float32(w*p[k]) + acc[k]
			}
			x++
		}
		y++
	}
	return acc
}

func bicubicNear(t float32) float32 {
	return float32(t*(float32(t*(float32(-21.0/18.0*t)+27.0/18.0))+9.0/18.0)) + 1.0/18.0
}

func bicubicFar(t float32) float32 {
	return (t * t) * (float32(7.0/18.0*t) - 6.0/18.0)
}

func (s sampler) bicubic(cx, cy float32) [4]float32 {
	fx, fy := fract(cx+0.5), fract(cy+0.5)
	wx := [4]float32{bicubicFar(1 - fx), bicubicNear(1 - fx), bicubicNear(fx), bicubicFar(fx)}
	wy := [4]float32{bicubicFar(1 - fy), bicubicNear(1 - fy), bicubicNear(fy), bicubicFar(fy)}
	var acc [4]float32
	y := cy - 1.5
	for j := range 4 {
		x := cx - 1.5
		for i := range 4 {
			p := s.gather(s.tile(x, true), s.tile(y, false))
			w := wx[i] * wy[j]
			for k := range 4 {
				acc[k] = float32(w*p[k]) + acc[k]
			}
			x++
		}
		y++
	}
	for k := range 4 {
		acc[k] = min(max(acc[k], 0), 1)
	}
	return acc
}

// DrawPixmap draws src with its top-left at (x, y), source-over, through mask when non-nil.
// It computes what tiny-skia's float pipeline computes for an integer-offset pixmap; opaque
// and empty source pixels, which that pipeline passes through exactly, skip the float math.
func DrawPixmap(dst *image.RGBA, x, y int, src *image.RGBA, mask *image.Alpha) {
	r := image.Rect(x, y, x+src.Rect.Dx(), y+src.Rect.Dy()).Intersect(dst.Rect)
	if r.Empty() {
		return
	}
	const factor = float32(1.0 / 255.0)
	for py := r.Min.Y; py < r.Max.Y; py++ {
		d := dst.Pix[dst.PixOffset(r.Min.X, py):]
		s := src.Pix[src.PixOffset(src.Rect.Min.X+r.Min.X-x, src.Rect.Min.Y+py-y):]
		var m []uint8
		if mask != nil {
			m = mask.Pix[mask.PixOffset(r.Min.X, py):]
		}
		for i := 0; i < r.Dx(); i++ {
			sp := s[i*4 : i*4+4 : i*4+4]
			mv := uint8(255)
			if m != nil {
				mv = m[i]
			}
			if mv == 0 || sp[3] == 0 && sp[0]|sp[1]|sp[2] == 0 {
				continue
			}
			dp := d[i*4 : i*4+4 : i*4+4]
			if mv == 255 && sp[3] == 255 {
				copy(dp, sp)
				continue
			}
			var c [4]float32
			mf := float32(mv) / 255
			for k := range 4 {
				c[k] = float32(sp[k]) * factor
				if mv != 255 {
					c[k] *= mf
				}
			}
			ia := 1 - c[3]
			for k := range 4 {
				dp[k] = unnorm(float32(float32(dp[k])*factor*ia) + c[k])
			}
		}
	}
}

// DrawPixmapTransformed draws src mapped by t onto dst with the given filter.
func DrawPixmapTransformed(dst *image.RGBA, src *image.RGBA, t Transform, q FilterQuality, mask *image.Alpha) {
	p := RectPath(R{0, 0, float32(src.Rect.Dx()), float32(src.Rect.Dy())}).Transformed(t)
	FillPath(dst, p, Paint{Shader: &Pattern{Image: src, Quality: q, Transform: t}}, mask)
}
