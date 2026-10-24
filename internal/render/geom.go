// Package render is Glint's CPU compositor: the frame around a screenshot (background, padding,
// shadow, window chrome, border), effects (watermark, perspective mockup, progressive blur),
// annotations and the EditDoc that ties them together. It is a port of the Rust crate
// glint-render and reads and writes the same JSON.
//
// Every *image.RGBA in this package holds premultiplied alpha, as Go defines it, and has its
// bounds at the origin. Use [Premultiply] and [Unpremultiply] to cross to straight alpha.
package render

import "math"

// Pt is a point in pixels.
type Pt struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
}

// P is shorthand for Pt{x, y}.
func P(x, y float32) Pt { return Pt{x, y} }

func (p Pt) Dist(o Pt) float32 {
	dx, dy := p.X-o.X, p.Y-o.Y
	return float32(math.Sqrt(float64(dx*dx + dy*dy)))
}

// R is an axis-aligned rectangle in pixels.
type R struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

func Rect(x, y, w, h float32) R { return R{x, y, w, h} }

// RectFromPts is the rectangle spanned by two corners.
func RectFromPts(a, b Pt) R {
	return R{min(a.X, b.X), min(a.Y, b.Y), abs32(a.X - b.X), abs32(a.Y - b.Y)}
}

func (r R) Right() float32  { return r.X + r.W }
func (r R) Bottom() float32 { return r.Y + r.H }
func (r R) Center() Pt      { return Pt{r.X + r.W/2, r.Y + r.H/2} }
func (r R) Area() float32   { return r.W * r.H }

func (r R) Contains(p Pt) bool {
	return p.X >= r.X && p.X <= r.Right() && p.Y >= r.Y && p.Y <= r.Bottom()
}

func (r R) ContainsRect(o R) bool {
	return o.X >= r.X && o.Y >= r.Y && o.Right() <= r.Right() && o.Bottom() <= r.Bottom()
}

func (r R) Inflate(d float32) R { return R{r.X - d, r.Y - d, r.W + 2*d, r.H + 2*d} }

func (r R) Union(o R) R {
	x, y := min(r.X, o.X), min(r.Y, o.Y)
	return R{x, y, max(r.Right(), o.Right()) - x, max(r.Bottom(), o.Bottom()) - y}
}

// Intersect returns the overlap and whether it is non-empty.
func (r R) Intersect(o R) (R, bool) {
	x, y := max(r.X, o.X), max(r.Y, o.Y)
	rr, b := min(r.Right(), o.Right()), min(r.Bottom(), o.Bottom())
	if rr > x && b > y {
		return R{x, y, rr - x, b - y}, true
	}
	return R{}, false
}

func (r R) Translate(dx, dy float32) R { return R{r.X + dx, r.Y + dy, r.W, r.H} }

// RoundOut is the smallest integer-aligned rect containing r.
func (r R) RoundOut() R {
	x, y := floor32(r.X), floor32(r.Y)
	return R{x, y, ceil32(r.Right()) - x, ceil32(r.Bottom()) - y}
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }
func floor32(v float32) float32 {
	return float32(math.Floor(float64(v)))
}
func ceil32(v float32) float32 { return float32(math.Ceil(float64(v))) }

// round32 rounds half away from zero, like Rust's f32::round.
func round32(v float32) float32 { return float32(math.Round(float64(v))) }
func sqrt32(v float32) float32  { return float32(math.Sqrt(float64(v))) }
func clamp32(v, lo, hi float32) float32 {
	return max(lo, min(hi, v))
}

// sinCos32 is Rust's deg.to_radians().sin_cos() in f32.
func sinCos32(deg float32) (float32, float32) {
	r := float64(toRadians32(deg))
	return float32(math.Sin(r)), float32(math.Cos(r))
}

// radsPerDeg32 rounds π to f32 before dividing, as Rust's f32::to_radians does.
const radsPerDeg32 = float32(math.Pi) / 180

func toRadians32(deg float32) float32 { return deg * radsPerDeg32 }
