package render

import (
	"image"
	"math"
	"sync"
)

// rasterizer accumulates signed area coverage, the algorithm of ab_glyph_rasterizer (font-rs),
// with x clamped into the grid so paths may extend past it.
type rasterizer struct {
	w, h int
	a    []float32
}

var rasterPool = sync.Pool{New: func() any { return &rasterizer{} }}

func (r *rasterizer) reset(w, h int) {
	n := w*h + 4
	if cap(r.a) < n {
		r.a = make([]float32, n)
	} else {
		r.a = r.a[:n]
		clear(r.a)
	}
	r.w, r.h = w, h
}

func (r *rasterizer) add(row, x int, v float32) {
	r.a[row+min(max(x, 0), r.w)] += v
}

func (r *rasterizer) line(p0, p1 Pt) {
	if abs32(p0.Y-p1.Y) <= 1.1920929e-7 {
		return
	}
	dir := float32(1)
	if p0.Y >= p1.Y {
		dir, p0, p1 = -1, p1, p0
	}
	dxdy := (p1.X - p0.X) / (p1.Y - p0.Y)
	x := p0.X
	y0 := 0
	if p0.Y > 0 {
		y0 = int(p0.Y)
	}
	if p0.Y < 0 {
		x -= p0.Y * dxdy
	}
	yEnd := min(r.h, max(0, int(ceil32(p1.Y))))
	for y := y0; y < yEnd; y++ {
		row := y * r.w
		dy := min(float32(y+1), p1.Y) - max(float32(y), p0.Y)
		xnext := x + dxdy*dy
		d := dy * dir
		x0, x1 := x, xnext
		if !(x < xnext) {
			x0, x1 = xnext, x
		}
		x0floor := floor32(x0)
		x0i := int(x0floor)
		x1ceil := ceil32(x1)
		x1i := int(x1ceil)
		if x1i <= x0i+1 {
			xmf := 0.5*(x+xnext) - x0floor
			r.add(row, x0i, d-d*xmf)
			r.add(row, x0i+1, d*xmf)
		} else {
			s := 1 / (x1 - x0)
			x0f := x0 - x0floor
			a0 := 0.5 * s * (1 - x0f) * (1 - x0f)
			x1f := x1 - x1ceil + 1
			am := 0.5 * s * x1f * x1f
			r.add(row, x0i, d*a0)
			if x1i == x0i+2 {
				r.add(row, x0i+1, d*(1-a0-am))
			} else {
				a1 := s * (1.5 - x0f)
				r.add(row, x0i+1, d*(a1-a0))
				r.addRun(row, x0i+2, x1i-1, d*s)
				a2 := a1 + float32(x1i-x0i-3)*s
				r.add(row, x1i-1, d*(1-a2-am))
			}
			r.add(row, x1i, d*am)
		}
		x = xnext
	}
}

// addRun adds v to every cell in [from, to), folding cells outside the grid onto its edges.
func (r *rasterizer) addRun(row, from, to int, v float32) {
	if from >= to {
		return
	}
	if from < 0 {
		n := min(to, 0) - from
		for range n {
			r.a[row] += v
		}
		from = 0
	}
	hi := min(to, r.w)
	for x := from; x < hi; x++ {
		r.a[row+x] += v
	}
	for x := max(from, r.w); x < to; x++ {
		r.a[row+r.w] += v
	}
}

func lerpPt(t float32, p0, p1 Pt) Pt {
	return Pt{p0.X + t*(p1.X-p0.X), p0.Y + t*(p1.Y-p0.Y)}
}

func (r *rasterizer) quad(p0, p1, p2 Pt) {
	devx := p0.X - 2*p1.X + p2.X
	devy := p0.Y - 2*p1.Y + p2.Y
	devsq := devx*devx + devy*devy
	if devsq < 0.333 {
		r.line(p0, p2)
		return
	}
	n := 1 + int(floor32(sqrt32(sqrt32(3*devsq))))
	p := p0
	nrecip := 1 / float32(n)
	t := float32(0)
	for i := 0; i < n-1; i++ {
		t += nrecip
		pn := lerpPt(t, lerpPt(t, p0, p1), lerpPt(t, p1, p2))
		r.line(p, pn)
		p = pn
	}
	r.line(p, p2)
}

func (r *rasterizer) cubic(p0, p1, p2, p3 Pt, depth int) {
	const flatnessSquared = 0.35 * 0.35
	longlen := p0.Dist(p1) + p1.Dist(p2) + p2.Dist(p3)
	shortlen := p0.Dist(p3)
	if depth < 16 && longlen*longlen-shortlen*shortlen > flatnessSquared {
		p01, p12, p23 := lerpPt(0.5, p0, p1), lerpPt(0.5, p1, p2), lerpPt(0.5, p2, p3)
		pa, pb := lerpPt(0.5, p01, p12), lerpPt(0.5, p12, p23)
		mp := lerpPt(0.5, pa, pb)
		r.cubic(p0, p01, pa, mp, depth+1)
		r.cubic(mp, pb, p23, p3, depth+1)
		return
	}
	r.line(p0, p3)
}

func (r *rasterizer) path(p *Path, dx, dy float32) {
	off := func(q Pt) Pt { return Pt{q.X + dx, q.Y + dy} }
	p.segments(func(v pathVerb, s []Pt) {
		switch v {
		case verbLine:
			r.line(off(s[0]), off(s[1]))
		case verbQuad:
			r.quad(off(s[0]), off(s[1]), off(s[2]))
		case verbCubic:
			r.cubic(off(s[0]), off(s[1]), off(s[2]), off(s[3]), 0)
		}
	})
}

// coverage is an anti-aliased coverage mask over Rect.
type coverage struct {
	rect image.Rectangle
	a    []uint8
}

// rasterize computes p's nonzero coverage within clip, or ok=false when nothing is covered.
func rasterize(p *Path, clip image.Rectangle) (coverage, bool) {
	if p.Empty() {
		return coverage{}, false
	}
	b := p.Bounds()
	if b.W <= 1.0/(1<<12) || b.H <= 1.0/(1<<12) {
		return coverage{}, false
	}
	rb := b.RoundOut()
	rect := image.Rect(int(rb.X), int(rb.Y), int(rb.Right()), int(rb.Bottom())).Intersect(clip)
	if rect.Empty() {
		return coverage{}, false
	}
	w, h := rect.Dx(), rect.Dy()
	r := rasterPool.Get().(*rasterizer)
	defer rasterPool.Put(r)
	r.reset(w, h)
	r.path(p, -float32(rect.Min.X), -float32(rect.Min.Y))
	out := make([]uint8, w*h)
	acc := float32(0)
	for i, v := range r.a[:w*h] {
		acc += v
		c := acc
		if c < 0 {
			c = -c
		}
		if c >= 1 {
			out[i] = 255
		} else {
			out[i] = uint8(c*255 + 0.5)
		}
	}
	return coverage{rect, out}, true
}

// NewMask is an empty clip mask: zero everywhere.
func NewMask(w, h int) *image.Alpha { return image.NewAlpha(image.Rect(0, 0, w, h)) }

// FillMask sets m to p's anti-aliased coverage wherever p covers it more.
func FillMask(m *image.Alpha, p *Path) {
	cov, ok := rasterize(p, m.Rect)
	if !ok {
		return
	}
	w := cov.rect.Dx()
	for y := cov.rect.Min.Y; y < cov.rect.Max.Y; y++ {
		row := m.Pix[m.PixOffset(cov.rect.Min.X, y):]
		crow := cov.a[(y-cov.rect.Min.Y)*w:]
		for x := 0; x < w; x++ {
			c := uint16(crow[x])
			d := uint16(row[x])
			switch {
			case c == 0:
			case c == 255 || d == 0:
				// div255(d*(255-c) + 255*c) is exactly c here.
				row[x] = uint8(c)
			default:
				row[x] = uint8(div255(d*(255-c) + 255*c))
			}
		}
	}
}

// IntersectMask multiplies m by p's coverage.
func IntersectMask(m *image.Alpha, p *Path) {
	sub := NewMask(m.Rect.Dx(), m.Rect.Dy())
	FillMask(sub, p)
	for i, a := range m.Pix {
		m.Pix[i] = mulDiv255Round(a, sub.Pix[i])
	}
}

func div255(v uint16) uint16 { return (v + 255) >> 8 }

// Fill sets every pixel of dst to c, premultiplied.
func Fill(dst *image.RGBA, c Color) {
	p := c.premulU8()
	for i := 0; i < len(dst.Pix); i += 4 {
		dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = p[0], p[1], p[2], p[3]
	}
}

// FillPath fills p (nonzero winding, anti-aliased) with paint, clipped by mask when non-nil.
func FillPath(dst *image.RGBA, p *Path, paint Paint, mask *image.Alpha) {
	cov, ok := rasterize(p, dst.Rect)
	if !ok {
		return
	}
	blit(dst, cov, paint, mask)
}

// FillRect fills r; integer-aligned rects are exact.
func FillRect(dst *image.RGBA, r R, paint Paint, mask *image.Alpha) {
	FillPath(dst, RectPath(r), paint, mask)
}

// fillAll paints every pixel of dst at full coverage.
func fillAll(dst *image.RGBA, paint Paint, mask *image.Alpha) {
	blit(dst, coverage{rect: dst.Rect}, paint, mask)
}

// LineCap and LineJoin follow tiny-skia's stroke options.
type LineCap uint8

const (
	CapButt LineCap = iota
	CapRound
	CapSquare
)

type LineJoin uint8

const (
	JoinMiter LineJoin = iota
	JoinRound
	JoinBevel
)

// Stroke describes a stroke. The zero values are butt caps and miter joins with limit 4.
type Stroke struct {
	Width      float32
	Cap        LineCap
	Join       LineJoin
	MiterLimit float32
}

// StrokePath strokes p. Strokes up to 1px wide are drawn 1px wide with their alpha scaled by
// the width, as tiny-skia's hairlines are.
func StrokePath(dst *image.RGBA, p *Path, paint Paint, s Stroke, mask *image.Alpha) {
	if s.Width < 0 || p.Empty() {
		return
	}
	if s.Width <= 1 {
		if s.Width > 0 && s.Width < 1 {
			scale := int32(s.Width * 256)
			paint = paint.withOpacity(float32((255*scale)>>8) / 255)
		}
		s.Width = 1
	}
	FillPath(dst, strokeOutline(p, s), paint, mask)
}

const strokeTolerance = 0.05

// strokeOutline is the outline of p stroked with s, to be filled with nonzero winding.
func strokeOutline(p *Path, s Stroke) *Path {
	hw := s.Width / 2
	out := &Path{}
	limit := s.MiterLimit
	if limit == 0 {
		limit = 4
	}
	for _, c := range p.flatten(strokeTolerance) {
		pts := c.pts
		if c.closed && len(pts) > 1 && pts[len(pts)-1] == pts[0] {
			pts = pts[:len(pts)-1]
		}
		if len(pts) == 1 {
			if c.closed {
				continue
			}
			switch s.Cap {
			case CapRound:
				appendPath(out, CirclePath(pts[0].X, pts[0].Y, hw))
			case CapSquare:
				appendPath(out, RectPath(R{pts[0].X - hw, pts[0].Y - hw, 2 * hw, 2 * hw}))
			}
			continue
		}
		rev := make([]Pt, len(pts))
		for i, q := range pts {
			rev[len(pts)-1-i] = q
		}
		if c.closed && len(pts) > 2 {
			side(out, pts, true, hw, s.Join, limit, true)
			out.Close()
			side(out, rev, true, hw, s.Join, limit, true)
			out.Close()
			continue
		}
		side(out, pts, false, hw, s.Join, limit, true)
		capEnd(out, pts[len(pts)-1], unitDir(pts[len(pts)-2], pts[len(pts)-1]), hw, s.Cap)
		side(out, rev, false, hw, s.Join, limit, false)
		capEnd(out, pts[0], unitDir(pts[1], pts[0]), hw, s.Cap)
		out.Close()
	}
	return out
}

func appendPath(dst, src *Path) {
	if src == nil {
		return
	}
	dst.verbs = append(dst.verbs, src.verbs...)
	dst.pts = append(dst.pts, src.pts...)
}

func unitDir(a, b Pt) Pt {
	dx, dy := b.X-a.X, b.Y-a.Y
	l := sqrt32(dx*dx + dy*dy)
	if l == 0 {
		return Pt{1, 0}
	}
	return Pt{dx / l, dy / l}
}

func normal(d Pt) Pt { return Pt{-d.Y, d.X} }

// side walks pts on the +normal side, offset by hw, with joins between segments. When start
// is true the walk begins with a MoveTo, otherwise it continues the current contour.
func side(out *Path, pts []Pt, closed bool, hw float32, join LineJoin, limit float32, start bool) {
	n := len(pts)
	segs := n - 1
	if closed {
		segs = n
	}
	dir := func(i int) Pt { return unitDir(pts[i%n], pts[(i+1)%n]) }
	off := func(p, d Pt) Pt {
		nn := normal(d)
		return Pt{p.X + nn.X*hw, p.Y + nn.Y*hw}
	}
	first := off(pts[0], dir(0))
	if start {
		out.MoveTo(first.X, first.Y)
	} else {
		out.LineTo(first.X, first.Y)
	}
	seglen := func(i int) float32 { return pts[i%n].Dist(pts[(i+1)%n]) }
	for i := 0; i < segs; i++ {
		d0 := dir(i)
		end := pts[(i+1)%n]
		a0 := off(end, d0)
		out.LineTo(a0.X, a0.Y)
		if !closed && i == segs-1 {
			break
		}
		joinAt(out, end, d0, dir(i+1), min(seglen(i), seglen(i+1)), hw, join, limit)
	}
}

// joinAt joins two segments meeting at v; shortest is the shorter segment's length.
func joinAt(out *Path, v, d0, d1 Pt, shortest, hw float32, join LineJoin, limit float32) {
	n0, n1 := normal(d0), normal(d1)
	a1 := Pt{v.X + n1.X*hw, v.Y + n1.Y*hw}
	cross := d0.X*d1.Y - d0.Y*d1.X
	dot := d0.X*d1.X + d0.Y*d1.Y
	if abs32(cross) < 1e-6 && dot > 0 {
		out.LineTo(a1.X, a1.Y)
		return
	}
	if cross > 0 {
		// Inside the turn. Meeting where the offset lines cross keeps the outline free of
		// overlapping loops, which signed-area coverage would count twice at the edges. When
		// that point falls past a segment, pivot through the vertex as Skia does.
		if 1+dot > 1e-6 && hw*cross/(1+dot) <= shortest {
			k := hw / (1 + dot)
			out.LineTo(v.X+(n0.X+n1.X)*k, v.Y+(n0.Y+n1.Y)*k)
		} else {
			out.LineTo(v.X, v.Y)
		}
		out.LineTo(a1.X, a1.Y)
		return
	}
	switch join {
	case JoinRound:
		sweep := float32(math.Atan2(float64(cross), float64(dot)))
		arc(out, v, n0, sweep, hw)
	case JoinMiter:
		if (1+dot)/2 >= 1/(limit*limit) && 1+dot > 1e-6 {
			k := hw / (1 + dot)
			out.LineTo(v.X+(n0.X+n1.X)*k, v.Y+(n0.Y+n1.Y)*k)
		}
	}
	out.LineTo(a1.X, a1.Y)
}

// arc continues the contour along a circle of radius r around c, from direction from
// (a unit vector) through sweep radians.
func arc(out *Path, c, from Pt, sweep, r float32) {
	step := float32(math.Pi / 4)
	if r > strokeTolerance {
		step = 2 * float32(math.Acos(float64(1-strokeTolerance/r)))
	}
	n := int(math.Ceil(float64(abs32(sweep) / step)))
	n = max(1, min(n, 256))
	base := math.Atan2(float64(from.Y), float64(from.X))
	for i := 1; i <= n; i++ {
		a := base + float64(sweep)*float64(i)/float64(n)
		out.LineTo(c.X+r*float32(math.Cos(a)), c.Y+r*float32(math.Sin(a)))
	}
}

func capEnd(out *Path, p, d Pt, hw float32, c LineCap) {
	nn := normal(d)
	switch c {
	case CapRound:
		arc(out, p, nn, -math.Pi, hw)
	case CapSquare:
		out.LineTo(p.X+nn.X*hw+d.X*hw, p.Y+nn.Y*hw+d.Y*hw)
		out.LineTo(p.X-nn.X*hw+d.X*hw, p.Y-nn.Y*hw+d.Y*hw)
	}
	out.LineTo(p.X-nn.X*hw, p.Y-nn.Y*hw)
}
