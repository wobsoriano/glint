package prefs

import (
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

var cardIcons = map[settings.CardAction]theme.IconName{
	settings.CardCopy: theme.IconCopy, settings.CardSave: theme.IconSave, settings.CardSaveAs: theme.IconSaveAs,
	settings.CardCompress: theme.IconCompress, settings.CardPin: theme.IconPin, settings.CardQuickLook: theme.IconEye,
	settings.CardEdit: theme.IconEdit, settings.CardCopyText: theme.IconScan, settings.CardDelete: theme.IconTrash,
	settings.CardDismiss: theme.IconClose, settings.CardMore: theme.IconMore,
}

var cardHelp = map[settings.CardAction]string{
	settings.CardCopy:      "Copy the image to the clipboard",
	settings.CardSave:      "Save to the export folder",
	settings.CardSaveAs:    "Save to a location you choose",
	settings.CardCompress:  "Save a smaller JPEG and copy it as a file",
	settings.CardPin:       "Pin above other windows",
	settings.CardQuickLook: "Open a large preview",
	settings.CardEdit:      "Open in the editor",
	settings.CardCopyText:  "Copy recognized text (on-device OCR)",
	settings.CardDelete:    "Move the capture to the " + map[bool]string{true: "Trash", false: "Recycle Bin"}[isMac],
	settings.CardDismiss:   "Close the card and keep the capture",
	settings.CardMore:      "Show the hidden actions",
}

var corners = []struct {
	corner settings.Corner
	at     ui.Anchor
	label  string
	dx, dy float32
}{
	{settings.TopLeft, ui.AnchorTopLeft, "Top left", 10, 10},
	{settings.TopRight, ui.AnchorTopRight, "Top right", -10, 10},
	{settings.BottomLeft, ui.AnchorBottomLeft, "Bottom left", 10, -10},
	{settings.BottomRight, ui.AnchorBottomRight, "Bottom right", -10, -10},
}

func (p *Prefs) overlay(c *ui.Context, pal theme.Palette) {
	o := &p.s().Overlay
	group(c, pal, "PREVIEW STACK", func() {
		choose(p, c, "Position", &o.Side, []settings.Side{settings.SideLeft, settings.SideRight}, []string{"Left", "Right"})
		choose(p, c, "Close automatically", &o.AutoCloseSeconds, []uint32{0, 5, 10, 30, 60}, []string{"Never", "5 s", "10 s", "30 s", "1 min"})
		p.toggle(c, "Dismiss a card after dragging it out", &o.DismissAfterDrag)
	})
	group(c, pal, "CARD ACTIONS", func() {
		noteText(c, pal, "Drag actions between the four corners, the center buttons and the hidden tray.")
		ui.Row(c).Justify(ui.Center).Padding(8, 0).Children(func() { p.card(c, pal, &o.Layout) })
		ui.Text(c, "Hidden").FontSize(theme.Caption).TextColor(pal.Muted)
		tray := ui.Row(c.Key("tray")).Wrap().Gap(8).Padding(8).MinHeight(48).Radius(theme.RadiusWindow).
			Background(pal.Layer).Border(1, pal.Border).Label("Hidden actions")
		p.dropZone(tray, pal, &o.Layout, settings.TrayZone, -1)
		tray.Children(func() {
			for _, a := range o.Layout.Hidden {
				p.chip(c, pal, a)
			}
			if len(o.Layout.Hidden) == 0 {
				noteText(c, pal, "Drag actions here to hide them")
			}
		})
		ui.Row(c).Children(func() {
			if ui.Button(c, "Reset Layout").Clicked() {
				o.Layout = settings.DefaultCardLayout()
				p.changed()
			}
		})
	})
}

// card is the capture card drawn large, its corners and center row taking dropped actions.
func (p *Prefs) card(c *ui.Context, pal theme.Palette, l *settings.CardLayout) {
	dashed := ui.RGBA(255, 255, 255, 0.33)
	ui.Box(c.Key("card")).Size(380, 250).Radius(theme.RadiusWindow).Border(1, pal.Border).
		Gradient(ui.RGB(0x3B, 0x3F, 0x7A), ui.RGB(0x7A, 0x3B, 0x66), 135).Children(func() {
		center := ui.Column(c.Key("center")).Absolute().Top(62).Bottom(62).Left(96).Right(96).Center().Gap(6).
			Radius(theme.RadiusWindow).Border(1, dashed).Background(ui.RGBA(0, 0, 0, 0.25)).Label("Center buttons")
		p.dropZone(center, pal, l, settings.CenterZone, -1)
		center.Children(func() {
			for i, a := range l.Center {
				chip := p.chip(c, pal, a)
				p.dropZone(chip, pal, l, settings.CenterZone, i)
			}
			if len(l.Center) == 0 {
				ui.Text(c, "Drop up to three buttons here").FontSize(theme.Caption).TextColor(ui.RGBA(255, 255, 255, 0.7))
			}
		})
		for _, k := range corners {
			slot := ui.Box(c.Key(k.label)).Attach(k.at, k.at).Left(k.dx).Top(k.dy).MinWidth(44).Height(34).Padding(4).Center().
				Radius(17).Border(1, dashed).Background(ui.RGBA(0, 0, 0, 0.4)).Label(k.label)
			p.dropZone(slot, pal, l, settings.CornerZone(k.corner), -1)
			slot.Children(func() {
				if a, ok := l.CornerAction(k.corner); ok {
					p.chip(c, pal, a)
				} else {
					ui.Text(c, "Empty").FontSize(theme.Caption).TextColor(pal.Muted)
				}
			})
		}
	})
}

// dropZone makes e take an action dragged onto it into zone, at index of the center row.
func (p *Prefs) dropZone(e ui.Element, pal theme.Palette, l *settings.CardLayout, zone settings.Zone, index int) {
	if _, over := ui.DragOver[settings.CardAction](e); over {
		e.Border(1, pal.Accent).Background(pal.Accent.Alpha(0.35))
	}
	if a, ok := ui.Drop[settings.CardAction](e); ok {
		l.Place(a, zone, index)
		p.changed()
	}
}

func (p *Prefs) chip(c *ui.Context, pal theme.Palette, a settings.CardAction) ui.Element {
	chip := ui.Row(c.Key(a)).Height(26).Padding(0, 8).Gap(6).Radius(13).Background(pal.Control).Border(1, pal.Border).
		Cursor(ui.CursorGrab).Drag(a).Tooltip(cardHelp[a]).Label(a.Label())
	if chip.Hovered() {
		chip.Border(1, pal.Accent)
	}
	chip.Children(func() {
		ui.Icon(c, theme.Icon(cardIcons[a])).Size(13, 13)
		ui.Text(c, a.Label()).FontSize(theme.Caption)
	})
	return chip
}
