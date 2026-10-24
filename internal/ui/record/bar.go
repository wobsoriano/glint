package record

import (
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	barW, barH, barMargin = 300, 48, 28
	blinkEvery            = 600 * time.Millisecond
	deleteQuestion        = "Delete this recording?"
	// confirmChrome is the delete confirmation's width without its three texts: paddings,
	// gaps, the border, the Delete icon, and 8 DIPs between the question and Keep.
	// bar_test.go holds it to the layout.
	confirmChrome = 112
)

var (
	recRed    = ui.RGB(0xFF, 0x45, 0x3A)
	pauseGold = ui.RGB(0xF7, 0xB5, 0x00)
)

type bar struct {
	born          time.Time
	display       platform.Display
	confirmDelete bool
	// width is the window's width in DIPs, wider while the bar asks before deleting so the
	// question shows whole.
	width float64
}

// showBar shows the controls of a running recording near the bottom of its display.
func (c *Controller) showBar(t Target) {
	c.forget(SurfaceBar)
	c.barView = &bar{born: time.Now(), display: t.Display, width: barW}
	c.bar = c.openChrome(duringRecording, bottomCentre(t.Display, barW, barH, barMargin), t.Display.Scale, c.barUI)
}

func (c *Controller) barUI(ctx *ui.Context) {
	p := theme.Apply(ctx)
	ctx.Root().Background(ui.Transparent)
	b := c.barView
	if b == nil {
		return
	}
	width := float64(barW)
	if b.confirmDelete {
		width = confirmWidth(ctx)
	}
	if width != b.width {
		b.width = width
		later(func() { c.resizeBar(b, width) })
	}
	shell := ui.Row(ctx).Fill().AlignItems(ui.Center).Gap(4).PaddingX(8).Radius(theme.RadiusWindow).
		Background(p.Flyout).Border(1, p.Border)
	if b.confirmDelete {
		shell.PaddingStart(16).Justify(ui.SpaceBetween).Children(func() {
			ui.Text(ctx, deleteQuestion).TextColor(p.Text).SingleLine()
			ui.Row(ctx).Gap(6).Children(func() {
				keep := ui.Button(ctx.Key("keep"), "Keep")
				c.probe(SurfaceBar, "keep", keep)
				if keep.Clicked() {
					b.confirmDelete = false
				}
				del := ui.ButtonBase(ctx.Key("confirm-delete")).Height(30).PaddingX(12).Gap(6).AlignItems(ui.Center).
					Radius(theme.RadiusControl).Background(danger).Cursor(ui.CursorPointer)
				if del.Hovered() {
					del.Background(dangerHot)
				}
				del.Children(func() {
					ui.Icon(ctx, theme.Icon(theme.IconTrash)).Size(14, 14).TextColor(white)
					ui.Text(ctx, "Delete").TextColor(white).SingleLine()
				})
				c.probe(SurfaceBar, "confirm-delete", del)
				if del.Clicked() {
					later(c.delete)
				}
			})
		})
		return
	}

	label, paused := "Starting", false
	if c.stage == live {
		label, paused = clock(uint64(c.session.rec.Elapsed().Milliseconds())), c.session.rec.IsPaused()
	}
	ctx.After(blinkEvery / 3)
	dot := recRed
	switch {
	case paused:
		dot = pauseGold
	case time.Since(b.born)/blinkEvery%2 == 1:
		dot = recRed.Alpha(0.25)
	}
	shell.Children(func() {
		ui.Row(ctx).Width(118).Gap(8).PaddingX(8).AlignItems(ui.Center).Children(func() {
			ui.Box(ctx).Size(10, 10).Radius(5).Background(dot)
			ui.Text(ctx, label).FontWeight(theme.Semibold).TextColor(p.Text).SingleLine()
			if paused {
				ui.Text(ctx, "Paused").FontSize(theme.Caption).TextColor(p.Muted).SingleLine()
			}
		})
		ui.Box(ctx).Size(1, 22).Background(p.Border)
		pauseIcon, pauseTip := theme.IconPause, "Pause"
		if paused {
			pauseIcon, pauseTip = theme.IconPlay, "Resume"
		}
		if c.barButton(ctx, p, "pause", pauseIcon, pauseTip, false) {
			c.pauseToggle()
		}
		if c.barButton(ctx, p, "restart", theme.IconRestart, "Restart", false) {
			later(c.restart)
		}
		if c.barButton(ctx, p, "stop", theme.IconStop, "Stop", false) {
			later(c.stop)
		}
		if c.barButton(ctx, p, "delete", theme.IconTrash, "Delete", true) {
			b.confirmDelete = true
		}
	})
}

// confirmWidth fits the delete confirmation to its texts in the window's font, and is never
// narrower than the bar.
func confirmWidth(ctx *ui.Context) float64 {
	w := float32(confirmChrome)
	for _, s := range []string{deleteQuestion, "Keep", "Delete"} {
		tw, _ := ctx.MeasureText(0, ui.Span{Text: s})
		w += tw
	}
	return max(math.Ceil(float64(w)), barW)
}

// resizeBar keeps the bar centred above the bottom of its display at the new width.
func (c *Controller) resizeBar(b *bar, width float64) {
	if c.bar != nil && c.barView == b {
		place(c.bar, bottomCentre(b.display, width, barH, barMargin))
	}
}

func (c *Controller) barButton(ctx *ui.Context, p theme.Palette, name string, icon theme.IconName, tip string, risky bool) bool {
	b := ui.ButtonBase(ctx.Key(name)).Size(32, 32).Center().Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Label(tip).Tooltip(tip)
	if b.Hovered() {
		if risky {
			b.Background(danger.Alpha(0.9))
		} else {
			b.Background(p.ControlHover)
		}
	}
	b.Children(func() { ui.Icon(ctx, theme.Icon(icon)).Size(16, 16).TextColor(p.Text) })
	c.probe(SurfaceBar, name, b)
	return b.Clicked()
}
