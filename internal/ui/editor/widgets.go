package editor

import (
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

func uiColor(c render.Color) ui.Color {
	return ui.Color{R: c.R(), G: c.G(), B: c.B(), A: c.A()}
}

func iconButton(c *ui.Context, key any, icon theme.IconName, active bool, tip string, pal palette) bool {
	b := ui.ButtonBase(c.Key(key)).Size(32, 32).Radius(theme.RadiusControl).Center().Label(tip).Tooltip(tip)
	col := pal.Text
	switch {
	case active:
		b.Background(pal.Accent)
		col = pal.OnAccent
	case b.Hovered():
		b.Background(pal.ControlHover)
	}
	clicked := b.Clicked()
	b.Children(func() { ui.Icon(c, theme.Icon(icon)).Size(16, 16).TextColor(col) })
	return clicked
}

func button(c *ui.Context, key any, label string, icon *theme.IconName, primary bool, pal palette) bool {
	b := ui.ButtonBase(c.Key(key)).Height(30).PaddingX(12).Gap(6).Radius(theme.RadiusControl).Center().Border(1, pal.Border)
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
	clicked := b.Clicked()
	b.Children(func() {
		if icon != nil {
			ui.Icon(c, theme.Icon(*icon)).Size(14, 14).TextColor(col)
		}
		ui.Text(c, label).FontSize(13).TextColor(col).SingleLine()
	})
	return clicked
}

func iconRef(n theme.IconName) *theme.IconName { return &n }

func chip(c *ui.Context, key any, label string, selected bool, pal palette) bool {
	b := ui.ButtonBase(c.Key(key)).Height(26).PaddingX(8).Radius(theme.RadiusControl).Center().Border(1, pal.Border)
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

func swatch(c *ui.Context, key any, colors []render.Color, selected bool, sz float32, label string, pal palette) bool {
	b := ui.ButtonBase(c.Key(key)).Size(sz, sz).Padding(2).Radius(sz*0.3).Border(2, ui.Transparent).Label(label)
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

func section(c *ui.Context, title string, pal palette, body func()) {
	ui.Column(c.Key(title)).Gap(8).Padding(12).Margin(0, 0, 8, 0).Radius(theme.RadiusWindow).Background(pal.Card).Border(1, pal.Border).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(theme.Semibold).TextColor(pal.Text)
		body()
	})
}

func note(c *ui.Context, text string, pal palette) {
	ui.Text(c, text).FontSize(12).TextColor(pal.Muted)
}

func label(c *ui.Context, text string, pal palette) {
	ui.Text(c, text).FontSize(12).TextColor(pal.Muted).SingleLine().Shrink(0)
}

func divider(c *ui.Context, pal palette) {
	ui.Box(c).Size(1, 20).Margin(0, 4).Shrink(0).Background(pal.Border)
}

// slider shows a labeled slider; it returns the value and whether the user moved it.
func slider(c *ui.Context, key, title string, value, lo, hi float32, display string, pal palette) (float32, bool) {
	v := float64(value)
	changed := false
	ui.Column(c.Key(key)).Gap(2).Children(func() {
		ui.Row(c).Children(func() {
			ui.Text(c, title).FontSize(12).TextColor(pal.Text).Grow(1)
			ui.Text(c, display).FontSize(12).TextColor(pal.Muted)
		})
		changed = ui.Slider(c, &v, float64(lo), float64(hi)).Label(title).Changed()
	})
	return float32(v), changed
}

// track is a bare slider of a set width, for the style bar.
func track(c *ui.Context, key, title string, value, lo, hi, width float32) (float32, bool) {
	v := float64(value)
	changed := ui.Slider(c.Key(key), &v, float64(lo), float64(hi)).Label(title).Width(width)
	return float32(v), changed.Changed()
}

func toggle(c *ui.Context, key, title string, on bool, pal palette) bool {
	v := on
	changed := false
	ui.Row(c.Key(key)).Gap(12).Children(func() {
		ui.Text(c, title).FontSize(13).TextColor(pal.Text).Grow(1)
		changed = ui.Switch(c, &v).Label(title).Changed()
	})
	return changed
}

// grid9 is a 3×3 picker of a cell; it returns the cell clicked, or -1.
func grid9(c *ui.Context, key string, selected int, pal palette) int {
	clicked := -1
	ui.Column(c.Key(key)).Gap(3).Children(func() {
		for row := range 3 {
			ui.Row(c).Gap(3).Children(func() {
				for col := range 3 {
					i := row*3 + col
					b := ui.ButtonBase(c.Key(i)).Size(16, 16).Radius(3).Border(1, pal.Border).Label(key + " " + string(rune('1'+i)))
					switch {
					case i == selected:
						b.Background(pal.Accent).BorderColor(pal.Accent)
					case b.Hovered():
						b.Background(pal.ControlHover)
					default:
						b.Background(pal.Control)
					}
					if b.Clicked() {
						clicked = i
					}
				}
			})
		}
	})
	return clicked
}

// colorRow is the ink palette; it returns the color clicked.
func colorRow(c *ui.Context, key string, current render.Color, sz float32, pal palette) (render.Color, bool) {
	var picked render.Color
	ok := false
	ui.Row(c.Key(key)).Gap(2).Wrap().Children(func() {
		for i, hex := range render.Ink {
			col := render.RGB(hex)
			if swatch(c, i, []render.Color{col}, current.WithAlpha(255) == col, sz, col.Hex(), pal) {
				picked, ok = col, true
			}
		}
	})
	return picked, ok
}
