package render

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Gradient is a linear gradient with evenly spaced stops.
type Gradient struct {
	Stops []Color `json:"stops"`
	// Degrees, 0 = left to right, 90 = top to bottom.
	Angle float32 `json:"angle"`
}

// Background is one of NoBackground, SolidBackground, GradientBackground, BlurredBackground
// and ImageBackground. It serializes as serde's externally tagged enum.
type Background interface{ isBackground() }

type NoBackground struct{}
type SolidBackground struct{ Color Color }
type GradientBackground struct{ Gradient Gradient }

// BlurredBackground is a heavily blurred copy of the screenshot itself.
type BlurredBackground struct{}

// ImageBackground is an image file (a wallpaper or a picture), scaled to cover.
type ImageBackground struct{ Path string }

func (NoBackground) isBackground()       {}
func (SolidBackground) isBackground()    {}
func (GradientBackground) isBackground() {}
func (BlurredBackground) isBackground()  {}
func (ImageBackground) isBackground()    {}

func marshalBackground(b Background) ([]byte, error) {
	switch b := b.(type) {
	case nil, NoBackground:
		return []byte(`"None"`), nil
	case BlurredBackground:
		return []byte(`"Blurred"`), nil
	case SolidBackground:
		return json.Marshal(map[string]Color{"Solid": b.Color})
	case GradientBackground:
		return json.Marshal(map[string]Gradient{"Gradient": b.Gradient})
	case ImageBackground:
		return json.Marshal(map[string]string{"Image": b.Path})
	}
	return nil, fmt.Errorf("unknown background %T", b)
}

func unmarshalBackground(data []byte) (Background, error) {
	var unit string
	if json.Unmarshal(data, &unit) == nil {
		switch unit {
		case "None":
			return NoBackground{}, nil
		case "Blurred":
			return BlurredBackground{}, nil
		}
		return nil, fmt.Errorf("unknown background %q", unit)
	}
	var tagged map[string]json.RawMessage
	if err := json.Unmarshal(data, &tagged); err != nil {
		return nil, err
	}
	if len(tagged) != 1 {
		return nil, fmt.Errorf("background needs exactly one variant, got %d", len(tagged))
	}
	for tag, v := range tagged {
		switch tag {
		case "Solid":
			var c Color
			if err := strictUnmarshal(v, &c); err != nil {
				return nil, err
			}
			return SolidBackground{c}, nil
		case "Gradient":
			var g Gradient
			if err := strictUnmarshal(v, &g); err != nil {
				return nil, err
			}
			return GradientBackground{g}, nil
		case "Image":
			var p string
			if err := strictUnmarshal(v, &p); err != nil {
				return nil, err
			}
			return ImageBackground{p}, nil
		}
		return nil, fmt.Errorf("unknown background %q", tag)
	}
	panic("unreachable")
}

func strictUnmarshal(data []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	return d.Decode(v)
}

// Chrome is a window title bar drawn above the screenshot.
type Chrome uint8

const (
	ChromeNone Chrome = iota
	ChromeMacLight
	ChromeMacDark
	ChromeWinLight
	ChromeWinDark
)

var chromeNames = []string{"None", "MacLight", "MacDark", "WinLight", "WinDark"}

var AllChromes = [5]Chrome{ChromeNone, ChromeMacLight, ChromeMacDark, ChromeWinLight, ChromeWinDark}

func (c Chrome) Label() string {
	return [...]string{"None", "macOS", "macOS Dark", "Windows", "Win Dark"}[c]
}

func (c Chrome) IsDark() bool { return c == ChromeMacDark || c == ChromeWinDark }

func (c Chrome) MarshalText() ([]byte, error) { return enumMarshal(c, chromeNames) }
func (c *Chrome) UnmarshalText(b []byte) error {
	return enumUnmarshal(c, b, chromeNames, "chrome")
}

type Aspect struct {
	W float32 `json:"w"`
	H float32 `json:"h"`
}

// NamedAspect is an entry of the aspect ratio menu; a nil Aspect is "Auto".
type NamedAspect struct {
	Name   string
	Aspect *Aspect
}

var Aspects = [8]NamedAspect{
	{"Auto", nil},
	{"1:1", &Aspect{1, 1}},
	{"4:3", &Aspect{4, 3}},
	{"3:2", &Aspect{3, 2}},
	{"16:9", &Aspect{16, 9}},
	{"16:10", &Aspect{16, 10}},
	{"4:5", &Aspect{4, 5}},
	{"9:16", &Aspect{9, 16}},
}

// Style is the frame around a screenshot. Sizes are in points and get multiplied by the
// capture's DPI scale. Missing JSON fields take DefaultStyle's values.
type Style struct {
	Background Background
	Padding    float32
	Inset      float32
	// Fill the inset with a color sampled from the screenshot edges.
	AutoBalance   bool
	Radius        float32
	Shadow        float32
	ShadowOpacity float32
	Aspect        *Aspect
	// 0..=8, row-major 3x3 grid (4 = centered).
	Align       uint8
	Chrome      Chrome
	ChromeTitle bool
}

func DefaultStyle() Style {
	return Style{
		Background:    GradientBackground{Gradients[0].Gradient.clone()},
		Padding:       64,
		Inset:         0,
		AutoBalance:   true,
		Radius:        12,
		Shadow:        40,
		ShadowOpacity: 0.45,
		Aspect:        nil,
		Align:         4,
		Chrome:        ChromeNone,
		ChromeTitle:   true,
	}
}

type styleJSON struct {
	Background    json.RawMessage `json:"background"`
	Padding       float32         `json:"padding"`
	Inset         float32         `json:"inset"`
	AutoBalance   bool            `json:"auto_balance"`
	Radius        float32         `json:"radius"`
	Shadow        float32         `json:"shadow"`
	ShadowOpacity float32         `json:"shadow_opacity"`
	Aspect        *Aspect         `json:"aspect"`
	Align         uint8           `json:"align"`
	Chrome        Chrome          `json:"chrome"`
	ChromeTitle   bool            `json:"chrome_title"`
}

func (s Style) MarshalJSON() ([]byte, error) {
	bg, err := marshalBackground(s.Background)
	if err != nil {
		return nil, err
	}
	return json.Marshal(styleJSON{bg, s.Padding, s.Inset, s.AutoBalance, s.Radius, s.Shadow, s.ShadowOpacity, s.Aspect, s.Align, s.Chrome, s.ChromeTitle})
}

func (s *Style) UnmarshalJSON(data []byte) error {
	d := DefaultStyle()
	j := styleJSON{nil, d.Padding, d.Inset, d.AutoBalance, d.Radius, d.Shadow, d.ShadowOpacity, d.Aspect, d.Align, d.Chrome, d.ChromeTitle}
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	bg := d.Background
	if j.Background != nil && string(j.Background) != "null" {
		var err error
		if bg, err = unmarshalBackground(j.Background); err != nil {
			return err
		}
	}
	*s = Style{bg, j.Padding, j.Inset, j.AutoBalance, j.Radius, j.Shadow, j.ShadowOpacity, j.Aspect, j.Align, j.Chrome, j.ChromeTitle}
	return nil
}

// Clone copies s so that the copy shares no slices or pointers with it.
func (s Style) Clone() Style {
	if g, ok := s.Background.(GradientBackground); ok {
		s.Background = GradientBackground{g.Gradient.clone()}
	}
	if s.Aspect != nil {
		a := *s.Aspect
		s.Aspect = &a
	}
	return s
}

func (g Gradient) clone() Gradient {
	return Gradient{append([]Color(nil), g.Stops...), g.Angle}
}

// NamedGradient is a gradient preset.
type NamedGradient struct {
	Name     string
	Gradient Gradient
}

func grad(angle float32, stops ...uint32) Gradient {
	g := Gradient{Angle: angle}
	for _, s := range stops {
		g.Stops = append(g.Stops, RGB(s))
	}
	return g
}

// Gradients are the gradient presets, in menu order. Treat them as read-only.
var Gradients = []NamedGradient{
	{"Aurora", grad(135, 0x6366F1, 0xA855F7, 0xEC4899)},
	{"Sunset", grad(135, 0xFF7E5F, 0xFEB47B)},
	{"Ocean", grad(135, 0x2193B0, 0x6DD5ED)},
	{"Mint", grad(135, 0x11998E, 0x38EF7D)},
	{"Peach", grad(135, 0xFFD1B3, 0xFF9A8B, 0xFF6A88)},
	{"Lavender", grad(135, 0xC3B1E1, 0x9F86C0, 0x5E548E)},
	{"Midnight", grad(135, 0x0F2027, 0x203A43, 0x2C5364)},
	{"Candy", grad(135, 0xF9A8D4, 0xC4B5FD, 0x93C5FD)},
	{"Citrus", grad(135, 0xF7971E, 0xFFD200)},
	{"Forest", grad(135, 0x134E5E, 0x71B280)},
	{"Rose", grad(135, 0xF43F5E, 0xFB7185, 0xFDA4AF)},
	{"Sky", grad(180, 0x89F7FE, 0x66A6FF)},
	{"Ember", grad(135, 0x200122, 0x6F0000)},
	{"Cosmic", grad(135, 0x1A2A6C, 0xB21F1F, 0xFDBB2D)},
	{"Slate", grad(135, 0x334155, 0x0F172A)},
	{"Paper", grad(135, 0xF5F7FA, 0xC3CFE2)},
	{"Lime", grad(135, 0xD4FC79, 0x96E6A1)},
	{"Grape", grad(135, 0x8E2DE2, 0x4A00E0)},
}

func enumMarshal[T ~uint8](v T, names []string) ([]byte, error) {
	if int(v) >= len(names) {
		return nil, fmt.Errorf("enum value %d out of range", v)
	}
	return []byte(names[v]), nil
}

func enumUnmarshal[T ~uint8](v *T, b []byte, names []string, what string) error {
	for i, n := range names {
		if n == string(b) {
			*v = T(i)
			return nil
		}
	}
	return fmt.Errorf("unknown %s %q", what, b)
}
