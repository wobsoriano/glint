package record

import (
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	barW, barH, barMargin = 300, 48, 28
	blinkEvery            = 600 * time.Millisecond
)

var (
	recRed    = ui.RGB(0xFF, 0x45, 0x3A)
	pauseGold = ui.RGB(0xF7, 0xB5, 0x00)
)

type bar struct {
	born          time.Time
	confirmDelete bool
}

// showBar shows the controls of a running recording near the bottom of its display.
func (c *Controller) showBar(t Target) {
	c.barView = &bar{born: time.Now()}
	c.bar = c.openChrome(duringRecording, bottomCentre(t.Display, barW, barH, barMargin), t.Display.Scale, c.barUI)
}

func (c *Controller) barUI(ctx *ui.Context) {
	p := theme.Apply(ctx)
	ctx.Root().Background(ui.Transparent)
	b := c.barView
	if b == nil {
		return
	}
	shell := ui.Row(ctx).Fill().AlignItems(ui.Center).Gap(4).PaddingX(8).Radius(theme.RadiusWindow).
		Background(p.Flyout).Border(1, p.Border)
	if b.confirmDelete {
		shell.Justify(ui.SpaceBetween).Children(func() {
			ui.Text(ctx, "Delete this recording?").TextColor(p.Text).PaddingStart(8).SingleLine()
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
