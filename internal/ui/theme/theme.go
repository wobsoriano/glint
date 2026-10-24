// Package theme is Glint's shared look: the palette of docs/design.md in light and dark, the
// accent the desktop chose, corner radii, spacing, type, the icon set and the Glint mark.
package theme

import (
	"runtime"

	"github.com/egoist/mygo/ui"
)

const (
	RadiusWindow  = 8
	RadiusControl = 4
	// RadiusPill rounds the large Copy and Save buttons of a preview card.
	RadiusPill = 999
)

// Spacing is the theme's unit: paddings and gaps are multiples of it (ui.Theme.Space).
const Spacing = 4

// Type sizes in DIPs. Body is the 14 px of Windows 11's body text; macOS reads 13.
const (
	Caption  = 12
	Body     = 14
	BodyMac  = 13
	Subtitle = 20
	Title    = 28
	Semibold = 600
)

// Font is the family Glint's text uses: Segoe UI Variable on Windows, the system's elsewhere.
func Font() string {
	if runtime.GOOS == "windows" {
		return "Segoe UI Variable Text"
	}
	return ""
}

// FallbackAccent is Windows' default blue, used when the desktop reports no accent.
var FallbackAccent = ui.RGB(0x00, 0x67, 0xC0)

// Palette is every color Glint's own surfaces draw with. Base, Layer and Card are translucent
// over a system backdrop (Mica) and opaque without one.
type Palette struct {
	Dark         bool
	Accent       ui.Color
	OnAccent     ui.Color
	Base         ui.Color
	Layer        ui.Color
	Card         ui.Color
	Control      ui.Color
	ControlHover ui.Color
	Border       ui.Color
	Text         ui.Color
	Muted        ui.Color
	Canvas       ui.Color
	Field        ui.Color
	Flyout       ui.Color
	Danger       ui.Color
	// Scrim dims the frozen screen outside a capture selection.
	Scrim ui.Color
}

// New returns the palette for an appearance, the desktop's accent (A 0 when it has none) and
// whether a system backdrop shows behind the window. It is the Rust build's read_theme.
func New(dark bool, accent ui.Color, backdrop bool) Palette {
	if accent.A == 0 {
		accent = FallbackAccent
	}
	r, g, b := uint32(accent.R), uint32(accent.G), uint32(accent.B)
	if dark && max(r, g, b) < 0x90 {
		lift := func(c uint32) uint32 { return c + (0xFF-c)/3 }
		r, g, b = lift(r), lift(g), lift(b)
	}
	acc := ui.RGB(uint8(r), uint8(g), uint8(b))
	onAccent := ui.RGB(0xFF, 0xFF, 0xFF)
	if (0.299*float32(r)+0.587*float32(g)+0.114*float32(b))/255 > 0.6 {
		onAccent = ui.RGB(0, 0, 0)
	}
	var p Palette
	if dark {
		p = Palette{
			Dark:         true,
			Base:         hex(0x202020D0),
			Layer:        hex(0x2C2C2CA0),
			Card:         hex(0xFFFFFF0D),
			Control:      hex(0xFFFFFF0F),
			ControlHover: hex(0xFFFFFF1C),
			Border:       hex(0xFFFFFF17),
			Text:         hex(0xFFFFFFFF),
			Muted:        hex(0xFFFFFFA3),
			Canvas:       hex(0x141414FF),
			Field:        hex(0x1C1C1CE6),
			Flyout:       hex(0x2C2C2CF8),
			Danger:       hex(0xFF99A4FF),
		}
	} else {
		p = Palette{
			Base:         hex(0xF3F3F3D8),
			Layer:        hex(0xFBFBFBA8),
			Card:         hex(0xFFFFFFB8),
			Control:      hex(0xFFFFFFB3),
			ControlHover: hex(0xF0F0F0F0),
			Border:       hex(0x0000001A),
			Text:         hex(0x1B1B1BFF),
			Muted:        hex(0x5F5F5FFF),
			Canvas:       hex(0xE4E4E4FF),
			Field:        hex(0xFFFFFFE6),
			Flyout:       hex(0xF9F9F9F8),
			Danger:       hex(0xC42B1CFF),
		}
	}
	p.Accent, p.OnAccent = acc, onAccent
	p.Scrim = ui.RGBA(0, 0, 0, 0.45)
	if !backdrop {
		p.Base.A = 0xFF
		p.Layer = p.Layer.Over(p.Base)
	}
	return p
}

// For returns the palette of the window c builds: its appearance, the desktop's accent, and
// whether the window shows a material behind the view.
func For(c *ui.Context) Palette {
	return New(c.Theme().Dark, c.Preferences().Accent, c.Vibrancy())
}

// UI returns base with Glint's palette, type and radii, for MyGo's widgets to draw with.
func (p Palette) UI(base *ui.Theme) *ui.Theme {
	t := *base
	t.Dark = p.Dark
	t.Background = p.Base
	t.Surface = p.Control
	t.SurfaceHover = p.ControlHover
	t.SurfacePressed = p.Control.Mix(p.ControlHover, 0.5).Alpha(0.8)
	t.Border = p.Border
	t.Text = p.Text
	t.TextMuted = p.Muted
	t.Accent = p.Accent
	t.AccentHover = p.Accent.Alpha(0.9)
	t.AccentPressed = p.Accent.Alpha(0.8)
	t.AccentText = p.OnAccent
	t.Danger = p.Danger
	t.Selection = p.Accent.Alpha(0.35)
	t.Focus = p.Text
	t.Inverse = p.Flyout.Over(ui.RGB(0, 0, 0))
	if !p.Dark {
		t.Inverse = p.Flyout.Over(ui.RGB(0xFF, 0xFF, 0xFF))
	}
	t.InverseText = p.Text
	t.Radius = RadiusControl
	t.Spacing = Spacing
	t.Font = Font()
	if runtime.GOOS == "darwin" {
		t.FontSize = BodyMac * max(base.FontSize/13, 1)
	} else {
		t.FontSize = Body * max(base.FontSize/14, 1)
	}
	return &t
}

// Apply sets Glint's theme on the window c builds, and returns its palette.
func Apply(c *ui.Context) Palette {
	p := For(c)
	c.SetTheme(p.UI(c.Theme()))
	return p
}

func hex(rgba uint32) ui.Color {
	return ui.Color{R: uint8(rgba >> 24), G: uint8(rgba >> 16), B: uint8(rgba >> 8), A: uint8(rgba)}
}
