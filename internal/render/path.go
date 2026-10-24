package render

import "math"

type pathVerb uint8

const (
	verbMove pathVerb = iota
	verbLine
	verbQuad
	verbCubic
	verbClose
)

// Path is a sequence of contours made of lines and Bézier curves, in pixels.
type Path struct {
	verbs []pathVerb
	pts   []Pt
}

func (p *Path) MoveTo(x, y float32) {
	p.verbs = append(p.verbs, verbMove)
	p.pts = append(p.pts, Pt{x, y})
}

func (p *Path) LineTo(x, y float32) {
	p.ensureStart()
	p.verbs = append(p.verbs, verbLine)
	p.pts = append(p.pts, Pt{x, y})
}

func (p *Path) QuadTo(x1, y1, x, y float32) {
	p.ensureStart()
	p.verbs = append(p.verbs, verbQuad)
	p.pts = append(p.pts, Pt{x1, y1}, Pt{x, y})
}

func (p *Path) CubicTo(x1, y1, x2, y2, x, y float32) {
	p.ensureStart()
	p.verbs = append(p.verbs, verbCubic)
	p.pts = append(p.pts, Pt{x1, y1}, Pt{x2, y2}, Pt{x, y})
}

func (p *Path) Close() {
	if n := len(p.verbs); n > 0 && p.verbs[n-1] != verbClose {
		p.verbs = append(p.verbs, verbClose)
	}
}

func (p *Path) ensureStart() {
	if len(p.verbs) == 0 {
		p.MoveTo(0, 0)
	} else if p.verbs[len(p.verbs)-1] == verbClose {
		last := p.lastMove()
		p.MoveTo(last.X, last.Y)
	}
}

func (p *Path) lastMove() Pt {
	i := len(p.pts)
	for v := len(p.verbs) - 1; v >= 0; v-- {
		i -= pointsOf(p.verbs[v])
		if p.verbs[v] == verbMove {
			return p.pts[i]
		}
	}
	return Pt{}
}

func pointsOf(v pathVerb) int {
	return [...]int{1, 1, 2, 3, 0}[v]
}

// Empty reports whether the path has no drawable segment.
func (p *Path) Empty() bool { return p == nil || len(p.pts) < 2 }

// Bounds is the bounding box of the path's points, control points included.
func (p *Path) Bounds() R {
	if p == nil || len(p.pts) == 0 {
		return R{}
	}
	x0, y0, x1, y1 := p.pts[0].X, p.pts[0].Y, p.pts[0].X, p.pts[0].Y
	for _, q := range p.pts[1:] {
		x0, y0, x1, y1 = min(x0, q.X), min(y0, q.Y), max(x1, q.X), max(y1, q.Y)
	}
	return R{x0, y0, x1 - x0, y1 - y0}
}

// Transformed maps every point through t.
func (p *Path) Transformed(t Transform) *Path {
	q := &Path{verbs: append([]pathVerb(nil), p.verbs...), pts: make([]Pt, len(p.pts))}
	for i, v := range p.pts {
		q.pts[i] = t.Apply(v)
	}
	return q
}

// segments calls fn for every segment, with contours closed back to their start.
func (p *Path) segments(fn func(verb pathVerb, pts []Pt)) {
	i := 0
	var start, cur Pt
	open := false
	closeContour := func() {
		if open && cur != start {
			fn(verbLine, []Pt{cur, start})
		}
		open = false
	}
	for _, v := range p.verbs {
		switch v {
		case verbMove:
			closeContour()
			start, cur = p.pts[i], p.pts[i]
			open = true
		case verbLine:
			fn(verbLine, []Pt{cur, p.pts[i]})
			cur = p.pts[i]
		case verbQuad:
			fn(verbQuad, []Pt{cur, p.pts[i], p.pts[i+1]})
			cur = p.pts[i+1]
		case verbCubic:
			fn(verbCubic, []Pt{cur, p.pts[i], p.pts[i+1], p.pts[i+2]})
			cur = p.pts[i+2]
		case verbClose:
			closeContour()
			cur = start
		}
		i += pointsOf(v)
	}
	closeContour()
}

// RectPath is a closed rectangle.
func RectPath(r R) *Path {
	p := &Path{}
	p.MoveTo(r.X, r.Y)
	p.LineTo(r.Right(), r.Y)
	p.LineTo(r.Right(), r.Bottom())
	p.LineTo(r.X, r.Bottom())
	p.Close()
	return p
}

// validRect mirrors tiny-skia's Rect::from_xywh with the minimum size the Rust code passes.
func validRect(r R) (R, bool) {
	r.W, r.H = max(r.W, 0.5), max(r.H, 0.5)
	ok := !math.IsNaN(float64(r.X+r.Y+r.W+r.H)) && !math.IsInf(float64(r.Right()+r.Bottom()), 0)
	return r, ok
}

// RoundedRect is r with corners of the given radius, nil if r is not finite.
func RoundedRect(r R, radius float32) *Path {
	rad := max(min(radius, r.W/2, r.H/2), 0)
	if rad < 0.5 {
		rr, ok := validRect(r)
		if !ok {
			return nil
		}
		return RectPath(rr)
	}
	k := rad * 0.4477
	x, y, rr, b := r.X, r.Y, r.Right(), r.Bottom()
	p := &Path{}
	p.MoveTo(x+rad, y)
	p.LineTo(rr-rad, y)
	p.CubicTo(rr-k, y, rr, y+k, rr, y+rad)
	p.LineTo(rr, b-rad)
	p.CubicTo(rr, b-k, rr-k, b, rr-rad, b)
	p.LineTo(x+rad, b)
	p.CubicTo(x+k, b, x, b-k, x, b-rad)
	p.LineTo(x, y+rad)
	p.CubicTo(x, y+k, x+k, y, x+rad, y)
	p.Close()
	return p
}

// OvalPath is the ellipse inscribed in r, starting at its right-middle point and running
// clockwise on screen, as tiny-skia's push_oval does.
func OvalPath(r R) *Path {
	const k = 0.5522847498
	cx, cy := r.X+r.W/2, r.Y+r.H/2
	rx, ry := r.W/2, r.H/2
	ox, oy := rx*k, ry*k
	p := &Path{}
	p.MoveTo(cx+rx, cy)
	p.CubicTo(cx+rx, cy+oy, cx+ox, cy+ry, cx, cy+ry)
	p.CubicTo(cx-ox, cy+ry, cx-rx, cy+oy, cx-rx, cy)
	p.CubicTo(cx-rx, cy-oy, cx-ox, cy-ry, cx, cy-ry)
	p.CubicTo(cx+ox, cy-ry, cx+rx, cy-oy, cx+rx, cy)
	p.Close()
	return p
}

func CirclePath(cx, cy, r float32) *Path {
	if r <= 0 {
		return nil
	}
	return OvalPath(R{cx - r, cy - r, 2 * r, 2 * r})
}

// PolygonPath is the closed polygon through points.
func PolygonPath(points []Pt) *Path {
	if len(points) == 0 {
		return nil
	}
	p := &Path{}
	p.MoveTo(points[0].X, points[0].Y)
	for _, q := range points[1:] {
		p.LineTo(q.X, q.Y)
	}
	p.Close()
	return p
}

// contour is a flattened subpath.
type contour struct {
	pts    []Pt
	closed bool
}

// flatten turns the path into polylines whose chords stay within tol of the curves.
func (p *Path) flatten(tol float32) []contour {
	var out []contour
	var cur *contour
	i := 0
	var last Pt
	push := func(q Pt) {
		if n := len(cur.pts); n == 0 || cur.pts[n-1] != q {
			cur.pts = append(cur.pts, q)
		}
		last = q
	}
	for _, v := range p.verbs {
		switch v {
		case verbMove:
			out = append(out, contour{})
			cur = &out[len(out)-1]
			push(p.pts[i])
		case verbLine:
			push(p.pts[i])
		case verbQuad:
			p0, p1, p2 := last, p.pts[i], p.pts[i+1]
			dd := Pt{p0.X - 2*p1.X + p2.X, p0.Y - 2*p1.Y + p2.Y}
			n := segmentsFor(sqrt32(dd.X*dd.X+dd.Y*dd.Y)/4, tol)
			for s := 1; s <= n; s++ {
				t := float32(s) / float32(n)
				u := 1 - t
				push(Pt{u*u*p0.X + 2*u*t*p1.X + t*t*p2.X, u*u*p0.Y + 2*u*t*p1.Y + t*t*p2.Y})
			}
		case verbCubic:
			p0, p1, p2, p3 := last, p.pts[i], p.pts[i+1], p.pts[i+2]
			d1 := Pt{p0.X - 2*p1.X + p2.X, p0.Y - 2*p1.Y + p2.Y}
			d2 := Pt{p1.X - 2*p2.X + p3.X, p1.Y - 2*p2.Y + p3.Y}
			dd := max(sqrt32(d1.X*d1.X+d1.Y*d1.Y), sqrt32(d2.X*d2.X+d2.Y*d2.Y))
			n := segmentsFor(dd*0.75, tol)
			for s := 1; s <= n; s++ {
				t := float32(s) / float32(n)
				u := 1 - t
				a, b, c, d := u*u*u, 3*u*u*t, 3*u*t*t, t*t*t
				push(Pt{a*p0.X + b*p1.X + c*p2.X + d*p3.X, a*p0.Y + b*p1.Y + c*p2.Y + d*p3.Y})
			}
		case verbClose:
			cur.closed = true
			last = cur.pts[0]
		}
		i += pointsOf(v)
	}
	return out
}

// segmentsFor is how many chords keep a curve whose chord error is err/n² within tol.
func segmentsFor(err, tol float32) int {
	n := int(math.Ceil(math.Sqrt(float64(err / tol))))
	return max(1, min(n, 500))
}
