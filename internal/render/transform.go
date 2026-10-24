package render

import "math"

// Transform is a 2D affine transform with tiny-skia's field layout and composition rules:
// (x, y) -> (SX x + KX y + TX, KY x + SY y + TY).
type Transform struct{ SX, KY, KX, SY, TX, TY float32 }

var Identity = Transform{SX: 1, SY: 1}

func TransformFromRow(sx, ky, kx, sy, tx, ty float32) Transform {
	return Transform{sx, ky, kx, sy, tx, ty}
}

func Translate(tx, ty float32) Transform { return Transform{1, 0, 0, 1, tx, ty} }
func Scale(sx, sy float32) Transform     { return Transform{sx, 0, 0, sy, 0, 0} }

// Rotate rotates by angle degrees.
func Rotate(angle float32) Transform {
	v := float64(toRadians32(angle))
	a, b := float32(math.Cos(v)), float32(math.Sin(v))
	return Transform{a, b, -b, a, 0, 0}
}

// RotateAt rotates by angle degrees around (tx, ty).
func RotateAt(angle, tx, ty float32) Transform {
	return Identity.PreConcat(Translate(tx, ty)).PreConcat(Rotate(angle)).PreConcat(Translate(-tx, -ty))
}

func (t Transform) IsIdentity() bool   { return t == Identity }
func (t Transform) hasScale() bool     { return t.SX != 1 || t.SY != 1 }
func (t Transform) hasSkew() bool      { return t.KX != 0 || t.KY != 0 }
func (t Transform) hasTranslate() bool { return t.TX != 0 || t.TY != 0 }
func (t Transform) isTranslate() bool {
	return !t.hasScale() && !t.hasSkew() && t.hasTranslate()
}

// PreConcat applies other before t.
func (t Transform) PreConcat(other Transform) Transform { return concat(t, other) }

// PostConcat applies other after t.
func (t Transform) PostConcat(other Transform) Transform { return concat(other, t) }
func (t Transform) PostTranslate(tx, ty float32) Transform {
	return t.PostConcat(Translate(tx, ty))
}
func (t Transform) PostScale(sx, sy float32) Transform { return t.PostConcat(Scale(sx, sy)) }

func concat(a, b Transform) Transform {
	switch {
	case a.IsIdentity():
		return b
	case b.IsIdentity():
		return a
	case !a.hasSkew() && !b.hasSkew():
		return Transform{a.SX * b.SX, 0, 0, a.SY * b.SY, float32(a.SX*b.TX) + a.TX, float32(a.SY*b.TY) + a.TY}
	}
	mam := func(a, b, c, d float32) float32 {
		return float32(float64(a)*float64(b) + float64(c)*float64(d))
	}
	return Transform{
		mam(a.SX, b.SX, a.KX, b.KY),
		mam(a.KY, b.SX, a.SY, b.KY),
		mam(a.SX, b.KX, a.KX, b.SY),
		mam(a.KY, b.KX, a.SY, b.SY),
		mam(a.SX, b.TX, a.KX, b.TY) + a.TX,
		mam(a.KY, b.TX, a.SY, b.TY) + a.TY,
	}
}

// Invert returns the inverse and whether it exists.
func (t Transform) Invert() (Transform, bool) {
	if t.IsIdentity() {
		return t, true
	}
	if (t.hasScale() || t.hasTranslate()) && !t.hasSkew() {
		if t.hasScale() {
			ix, iy := 1/t.SX, 1/t.SY
			return Transform{ix, 0, 0, iy, -t.TX * ix, -t.TY * iy}, true
		}
		return Translate(-t.TX, -t.TY), true
	}
	det := float64(t.SX)*float64(t.SY) - float64(t.KX)*float64(t.KY)
	const nearlyZero = 1.0 / (1 << 12)
	if math.Abs(float64(float32(det))) <= nearlyZero*nearlyZero*nearlyZero {
		return Transform{}, false
	}
	inv := 1 / det
	dcs := func(a, b, c, d float32) float32 {
		return float32((float64(a)*float64(b) - float64(c)*float64(d)) * inv)
	}
	r := Transform{
		float32(float64(t.SY) * inv),
		float32(float64(-t.KY) * inv),
		float32(float64(-t.KX) * inv),
		float32(float64(t.SX) * inv),
		dcs(t.KX, t.TY, t.SY, t.TX),
		dcs(t.KY, t.TX, t.SX, t.TY),
	}
	for _, v := range []float32{r.SX, r.KY, r.KX, r.SY, r.TX, r.TY} {
		if math.IsInf(float64(v), 0) || math.IsNaN(float64(v)) {
			return Transform{}, false
		}
	}
	return r, true
}

// Apply maps p, evaluated the way tiny-skia's pipeline evaluates it.
func (t Transform) Apply(p Pt) Pt {
	return Pt{float32(p.X*t.SX) + (float32(p.Y*t.KX) + t.TX), float32(p.X*t.KY) + (float32(p.Y*t.SY) + t.TY)}
}
