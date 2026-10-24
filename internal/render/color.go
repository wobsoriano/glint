package render

import (
	"fmt"
	"math"
)

// Color is a straight-alpha RGBA color packed as 0xRRGGBBAA. It serializes as that number.
type Color uint32

const (
	White       Color = 0xFFFFFFFF
	Black       Color = 0x000000FF
	Transparent Color = 0
)

// RGB is an opaque color from 0xRRGGBB.
func RGB(hex uint32) Color { return Color(hex<<8 | 0xFF) }

func RGBA8(r, g, b, a uint8) Color {
	return Color(uint32(r)<<24 | uint32(g)<<16 | uint32(b)<<8 | uint32(a))
}

func (c Color) R() uint8 { return uint8(c >> 24) }
func (c Color) G() uint8 { return uint8(c >> 16) }
func (c Color) B() uint8 { return uint8(c >> 8) }
func (c Color) A() uint8 { return uint8(c) }

func (c Color) WithAlpha(a uint8) Color { return c&0xFFFFFF00 | Color(a) }

// Luma is perceived luminance in 0..1.
func (c Color) Luma() float32 {
	return (0.299*float32(c.R()) + 0.587*float32(c.G()) + 0.114*float32(c.B())) / 255
}

// Hex is "#RRGGBB".
func (c Color) Hex() string { return fmt.Sprintf("#%02X%02X%02X", c.R(), c.G(), c.B()) }

// FromHSL builds an opaque color; h in 0..360, s and l in 0..1.
func FromHSL(h, s, l float32) Color {
	c := (1 - abs32(2*l-1)) * s
	hp := remEuclid(h, 360) / 60
	x := c * (1 - abs32(float32(math.Mod(float64(hp), 2))-1))
	var r, g, b float32
	switch uint32(hp) {
	case 0:
		r, g, b = c, x, 0
	case 1:
		r, g, b = x, c, 0
	case 2:
		r, g, b = 0, c, x
	case 3:
		r, g, b = 0, x, c
	case 4:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	m := l - c/2
	f := func(v float32) uint8 { return uint8(round32(clamp32(v+m, 0, 1) * 255)) }
	return RGBA8(f(r), f(g), f(b), 255)
}

// HSL returns h in 0..360 and s, l in 0..1.
func (c Color) HSL() (h, s, l float32) {
	r, g, b := float32(c.R())/255, float32(c.G())/255, float32(c.B())/255
	mx, mn := max(r, g, b), min(r, g, b)
	l = (mx + mn) / 2
	if abs32(mx-mn) < 1.1920929e-7 {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch {
	case mx == r:
		h = (g - b) / d
		if g < b {
			h += 6
		}
	case mx == g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	return h * 60, s, l
}

func remEuclid(v, m float32) float32 {
	r := float32(math.Mod(float64(v), float64(m)))
	if r < 0 {
		r += m
	}
	return r
}

// premul is c premultiplied the way tiny-skia uploads a uniform color (float math, +0.5 truncation).
func (c Color) premul() [4]uint16 {
	a := float32(c.A()) / 255
	conv := func(v uint8) uint16 {
		f := float32(v) / 255
		if c.A() != 255 {
			f = clamp32(f*a, 0, 1)
		}
		return uint16(f*255 + 0.5)
	}
	return [4]uint16{conv(c.R()), conv(c.G()), conv(c.B()), uint16(a*255 + 0.5)}
}

// premulU8 is tiny-skia's ColorU8::premultiply, used when a color is written without blending.
func (c Color) premulU8() [4]uint8 {
	a := c.A()
	if a == 255 {
		return [4]uint8{c.R(), c.G(), c.B(), a}
	}
	return [4]uint8{mulDiv255Round(c.R(), a), mulDiv255Round(c.G(), a), mulDiv255Round(c.B(), a), a}
}

func mulDiv255Round(c, a uint8) uint8 {
	prod := uint32(c)*uint32(a) + 128
	return uint8((prod + prod>>8) >> 8)
}

// Solids is the solid background palette.
var Solids = [12]uint32{
	0xFFFFFF, 0xF4F4F5, 0xD4D4D8, 0x71717A, 0x27272A, 0x000000, 0xFDE68A, 0xFCA5A5, 0x93C5FD,
	0x86EFAC, 0xC4B5FD, 0xF9A8D4,
}

// Ink is the annotation palette.
var Ink = [10]uint32{
	0xFF3B30, 0xFF9500, 0xFFCC00, 0x34C759, 0x00C7BE, 0x007AFF, 0x5856D6, 0xAF52DE, 0xFFFFFF,
	0x1C1C1E,
}
