package studio

import (
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// The controls below keep the keyboard focus where it is when clicked, as the Rust Studio's
// do: Space, the arrows and Delete act on the timeline whatever was clicked last.

func uiColor(c render.Color) ui.Color { return ui.Color{R: c.R(), G: c.G(), B: c.B(), A: c.A()} }

func iconButton(c *ui.Context, key any, icon theme.IconName, active, enabled bool, tip string, pal theme.Palette) bool {
	return iconButtonSized(c, key, icon, active, enabled, tip, 32, pal)
}

func iconButtonSized(c *ui.Context, key any, icon theme.IconName, active, enabled bool, tip string, size float32, pal theme.Palette) bool {
	b := ui.ButtonBase(c.Key(key)).Size(size, size).Radius(theme.RadiusControl).Center().Label(tip).Tooltip(tip).KeepFocus()
	col := pal.Text
	switch {
	case !enabled:
		b.Opacity(0.4)
	case active:
		b.Background(pal.Accent)
		col = pal.OnAccent
	case b.Hovered():
		b.Background(pal.ControlHover)
	}
	clicked := b.Clicked() && enabled
	b.Children(func() { ui.Icon(c, theme.Icon(icon)).Size(16, 16).TextColor(col) })
	return clicked
}

// button is a labeled push button; icon is -1 for none.
func button(c *ui.Context, key any, label string, icon theme.IconName, primary bool, pal theme.Palette) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Height(30).PaddingX(12).Gap(6).Radius(theme.RadiusControl).Center().Border(1, pal.Border).Label(label).KeepFocus()
	col := pal.Text
	switch {
	case primary:
		b.Background(pal.Accent).BorderColor(pal.Accent)
		col = pal.OnAccent
		if b.Hovered() {
			b.Opacity(0.9)
		}
	case b.Hovered():
		b.Background(pal.ControlHover)
	default:
		b.Background(pal.Control)
	}
	b.Children(func() {
		if icon >= 0 {
			ui.Icon(c, theme.Icon(icon)).Size(14, 14).TextColor(col)
		}
		ui.Text(c, label).FontSize(13).TextColor(col).SingleLine()
	})
	return b
}

const noIcon theme.IconName = -1

func chip(c *ui.Context, key any, label string, selected bool, pal theme.Palette) bool {
	b := ui.ButtonBase(c.Key(key)).Height(26).PaddingX(8).Radius(theme.RadiusControl).Center().Border(1, pal.Border).Label(label).KeepFocus()
	col := pal.Text
	switch {
	case selected:
		b.Background(pal.Accent).BorderColor(pal.Accent)
		col = pal.OnAccent
	case b.Hovered():
		b.Background(pal.ControlHover)
	default:
		b.Background(pal.Control)
	}
	clicked := b.Clicked()
	b.Children(func() { ui.Text(c, label).FontSize(12).TextColor(col).SingleLine() })
	return clicked
}

func swatch(c *ui.Context, key any, colors []render.Color, selected bool, sz float32, label string, pal theme.Palette) bool {
	b := ui.ButtonBase(c.Key(key)).Size(sz, sz).Padding(2).Radius(sz*0.3).Border(2, ui.Transparent).Label(label).KeepFocus()
	switch {
	case selected:
		b.BorderColor(pal.Accent)
	case b.Hovered():
		b.BorderColor(pal.Muted)
	}
	clicked := b.Clicked()
	b.Children(func() {
		inner := ui.Box(c).Fill().Radius(sz*0.22).Border(1, pal.Border)
		if len(colors) >= 2 {
			stops := make([]ui.GradientStop, len(colors))
			for i, col := range colors {
				stops[i] = ui.GradientStop{Color: uiColor(col), At: float32(i) / float32(len(colors)-1)}
			}
			inner.LinearGradient(ui.LinearGradient{Angle: 135, Stops: stops})
		} else if len(colors) == 1 {
			inner.Background(uiColor(colors[0]))
		}
	})
	return clicked
}

func section(c *ui.Context, title string, pal theme.Palette, body func()) {
	ui.Column(c.Key(title)).Gap(8).Padding(12).Margin(0, 0, 8, 0).Radius(theme.RadiusWindow).Background(pal.Card).Border(1, pal.Border).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(theme.Semibold).TextColor(pal.Text)
		body()
	})
}

func hint(c *ui.Context, text string, pal theme.Palette) {
	ui.Text(c, text).FontSize(12).TextColor(pal.Muted)
}

func separator(c *ui.Context, pal theme.Palette) {
	ui.Box(c).Size(1, 20).Margin(0, 4).Shrink(0).Background(pal.Border)
}

// slider is a labeled slider over lo..hi; it returns the value and whether the user moved it.
func slider(c *ui.Context, key, title string, value, lo, hi float32, display string, pal theme.Palette) (float32, bool) {
	v := float64(value)
	changed := false
	ui.Column(c.Key(key)).Gap(2).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(pal.Text).Grow(1)
			ui.Text(c, display).FontSize(12).TextColor(pal.Muted)
		})
		changed = ui.Slider(c, &v, float64(lo), float64(hi)).Label(title).KeepFocus().Changed()
	})
	return float32(v), changed
}

// toggle is a switch whose whole row turns it over, as the Rust Studio's toggles are.
func toggle(c *ui.Context, key, title string, on bool, pal theme.Palette) bool {
	v := on
	changed := false
	row := ui.ButtonBase(c.Key(key)).Gap(12).Label(title).KeepFocus()
	clicked := row.Clicked()
	row.Children(func() {
		ui.Text(c, title).FontSize(13).TextColor(pal.Text).Grow(1)
		changed = ui.Switch(c, &v).Label(title).KeepFocus().Changed()
	})
	return changed || clicked
}

// progressBar is a thin bar filled to p of 0..1.
func progressBar(c *ui.Context, p float64, label string, pal theme.Palette) {
	ui.Box(c).Height(6).Radius(3).Background(pal.ControlHover).Label(label).Draw(func(pt *ui.Painter, r ui.Rect) {
		pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W * float32(max(0, min(p, 1))), H: r.H}, pal.Accent, 3)
	})
}
