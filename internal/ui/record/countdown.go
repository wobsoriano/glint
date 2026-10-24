package record

import (
	"strconv"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const countdownSide = 220

var (
	white     = ui.RGB(0xFF, 0xFF, 0xFF)
	ink       = ui.RGBA(0x0B, 0x0B, 0x0D, 0.85)
	inkBorder = ui.RGBA(255, 255, 255, 0.15)
	danger    = ui.RGB(0xC4, 0x2B, 0x1C)
	dangerHot = ui.RGB(0xD8, 0x3B, 0x2C)
)

// showCountdown shows the seconds left in the middle of what is about to be recorded.
func (c *Controller) showCountdown() {
	c.closeWindow(&c.countdown)
	t := *c.target
	f := t.frame()
	side := scaled(countdownSide, t.Display.Scale)
	r := platform.Rect{X: f.X + (f.W-side)/2, Y: f.Y + (f.H-side)/2, W: side, H: side}
	c.countdown = c.openChrome(passive, r, t.Display.Scale, c.countdownView)
}

func (c *Controller) countdownView(ctx *ui.Context) {
	theme.Apply(ctx)
	ctx.Root().Background(ui.Transparent)
	ui.Box(ctx).Fill().Center().Children(func() {
		ui.Column(ctx).Size(countdownSide-20, countdownSide-20).Center().Radius(theme.RadiusPill).
			Background(ink).Border(1, inkBorder).Children(func() {
			ui.Text(ctx, strconv.Itoa(c.left)).FontSize(104).FontWeight(theme.Semibold).TextColor(white).SingleLine()
			ui.Text(ctx, "Esc to cancel").FontSize(theme.Caption).TextColor(ui.RGBA(255, 255, 255, 0.7))
		})
	})
}
