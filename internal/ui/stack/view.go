package stack

import (
	"fmt"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

var (
	white     = ui.RGB(0xFF, 0xFF, 0xFF)
	chip      = ui.RGBA(28, 28, 31, 0.9)
	chipEdge  = ui.RGBA(255, 255, 255, 0.18)
	dim       = ui.RGBA(0, 0, 0, 0.55)
	danger    = ui.RGB(0xC4, 0x2B, 0x1C)
	doneGreen = ui.RGB(0x34, 0xC7, 0x59)
)

func (s *Stack) view(c *ui.Context) {
	p := theme.Apply(c)
	c.Root().Background(ui.Transparent)
	if s.collapsed {
		s.peek(c, p)
		return
	}
	root := ui.Column(c).Fill().Padding(pad).Gap(gap)
	s.setHovered(root.Hovered())
	root.Children(func() {
		s.header(c, p)
		layout := s.prefs.Layout.Normalized()
		for i := len(s.cards) - 1; i >= 0; i-- {
			s.cardView(c.Key(s.cards[i].id), s.cards[i], layout, p)
		}
	})
}

func (s *Stack) header(c *ui.Context, p theme.Palette) {
	left := s.prefs.Side != settings.SideRight
	row := ui.Row(c).Height(headerH).Gap(6).AlignItems(ui.Center)
	if !left {
		row.Justify(ui.End)
	}
	recordings := 0
	for _, cd := range s.cards {
		if cd.recording() {
			recordings++
		}
	}
	row.Children(func() {
		ui.Row(c).Height(26).PaddingX(12).AlignItems(ui.Center).Radius(13).Background(p.Flyout).Border(1, p.Border).Children(func() {
			ui.Text(c, countLabel(recordings, len(s.cards))).FontSize(theme.Caption).TextColor(p.Text)
		})
		clear := ui.ButtonBase(c.Key("clear")).Height(26).PaddingX(12).Center().Radius(13).Background(p.Flyout).Border(1, p.Border)
		if clear.Hovered() {
			clear.Background(p.Accent.Alpha(0.94))
		}
		clear.Children(func() { ui.Text(c, "Clear All").FontSize(theme.Caption).TextColor(p.Text) })
		if clear.Clicked() {
			s.clear()
		}
		chevron := theme.IconChevronLeft
		if !left {
			chevron = theme.IconChevronRight
		}
		tuck := ui.ButtonBase(c.Key("collapse")).Size(26, 26).Center().Radius(13).Background(p.Flyout).Border(1, p.Border).Tooltip("Tuck into the edge")
		if tuck.Hovered() {
			tuck.Background(p.Accent.Alpha(0.94))
		}
		tuck.Children(func() { ui.Icon(c, theme.Icon(chevron)).Size(13, 13).TextColor(p.Text) })
		if tuck.Clicked() {
			s.collapsed, s.menu = true, 0
			s.relayout()
		}
	})
}

// peek is the collapsed stack: a tab against the screen edge that brings the cards back.
func (s *Stack) peek(c *ui.Context, p theme.Palette) {
	left := s.prefs.Side != settings.SideRight
	tab := ui.ButtonBase(c.Key("peek")).Fill().Center().Background(p.Flyout).Border(1, p.Border).
		Tooltip(fmt.Sprintf("Show %d captures", len(s.cards)))
	chevron := theme.IconChevronRight
	if left {
		tab.Radius(0, theme.RadiusWindow, theme.RadiusWindow, 0)
	} else {
		tab.Radius(theme.RadiusWindow, 0, 0, theme.RadiusWindow)
		chevron = theme.IconChevronLeft
	}
	if tab.Hovered() {
		tab.Background(p.ControlHover.Over(p.Flyout))
	}
	tab.Children(func() {
		ui.Column(c).Gap(4).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, theme.Icon(chevron)).Size(14, 14).TextColor(p.Text)
			ui.Text(c, fmt.Sprint(len(s.cards))).FontSize(theme.Caption).TextColor(p.Text)
		})
	})
	if tab.Clicked() {
		s.collapsed = false
		s.relayout()
	}
}

func (s *Stack) cardView(c *ui.Context, cd *card, layout settings.CardLayout, p theme.Palette) {
	id := cd.id
	box := ui.Box(c).Size(cardW, cardH).Radius(theme.RadiusWindow).Clip().Border(1, p.Border).
		Background(p.Canvas).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.35)).Cursor(ui.CursorPointer)
	box.DragDataFrom(func() transfer.Data { return s.dragData(id) }, transfer.DragOptions{Done: func(r transfer.Result) { s.dragged(id, r) }})
	if box.DoubleClicked() {
		s.run(id, settings.CardEdit)
	}
	menuOpen := s.menu == id
	if !box.Hovered() {
		cd.hushed = false
	}
	reveal := (box.Hovered() || menuOpen) && cd.status == ""
	slots := SlotsFor(layout, cd.recording())
	box.Children(func() {
		ui.Image(c, cd.thumb).Fill().Fit(ui.Cover)
		if d := cd.item.Meta.DurationMs; cd.recording() {
			ms := uint64(0)
			if d != nil {
				ms = *d
			}
			ui.Row(c).Absolute().Bottom(6).Left(6).PaddingX(6).PaddingY(1).Gap(4).AlignItems(ui.Center).
				Radius(theme.RadiusControl).Background(ui.RGBA(0, 0, 0, 0.7)).Children(func() {
				ui.Icon(c, theme.Icon(theme.IconFilm)).Size(11, 11).TextColor(white)
				ui.Text(c, clock(ms)).FontSize(theme.Caption).TextColor(white)
			})
		}
		if reveal {
			s.actions(c, id, slots, cd.hushed)
		}
		if menuOpen {
			s.moreMenu(c, id, slots.Menu, p)
		}
		if cd.status != "" {
			ui.Row(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(ui.RGBA(0, 0, 0, 0.7)).
				Justify(ui.Center).AlignItems(ui.Center).Gap(8).Padding(8).Children(func() {
				ui.Icon(c, theme.Icon(theme.IconCheck)).Size(16, 16).TextColor(doneGreen)
				ui.Text(c, cd.status).FontSize(13).TextColor(white).MaxLines(2)
			})
		}
	})
}

// tip gives b the action's tooltip, or only its description while the card is hushed.
func tip(b ui.Element, a settings.CardAction, hushed bool) ui.Element {
	if hushed {
		return b.Description(help[a])
	}
	return b.Tooltip(help[a])
}

func (s *Stack) actions(c *ui.Context, id int, slots Slots, hushed bool) {
	ui.Box(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Background(dim).PassThrough()
	ui.Column(c).Absolute().Top(0).Left(0).Right(0).Bottom(0).Center().Gap(6).PassThrough().Children(func() {
		for _, a := range slots.Center {
			b := ui.ButtonBase(c.Key("c-"+string(a))).Width(112).Height(32).Radius(theme.RadiusPill).Center().
				Background(chip).Border(1, chipEdge)
			tip(b, a, hushed)
			if b.Hovered() {
				b.Background(theme.For(c).Accent)
			}
			b.Children(func() {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, theme.Icon(icons[a])).Size(14, 14).TextColor(white)
					ui.Text(c, a.Label()).FontSize(13).FontWeight(theme.Semibold).TextColor(white)
				})
			})
			if b.Clicked() {
				s.run(id, a)
			}
		}
	})
	for _, corner := range settings.Corners {
		a := slots.Corners[corner]
		if a == "" {
			continue
		}
		b := ui.ButtonBase(c.Key("k-"+string(a))).Absolute().Size(28, 28).Radius(14).Center().
			Background(chip).Border(1, chipEdge)
		tip(b, a, hushed)
		switch corner {
		case settings.TopLeft:
			b.Top(6).Left(6)
		case settings.TopRight:
			b.Top(6).Right(6)
		case settings.BottomLeft:
			b.Bottom(6).Left(6)
		case settings.BottomRight:
			b.Bottom(6).Right(6)
		}
		if b.Hovered() {
			if a == settings.CardDelete {
				b.Background(danger)
			} else {
				b.Background(theme.For(c).Accent)
			}
		}
		b.Children(func() { ui.Icon(c, theme.Icon(icons[a])).Size(14, 14).TextColor(white) })
		if b.Clicked() {
			s.run(id, a)
		}
	}
}

func (s *Stack) moreMenu(c *ui.Context, id int, menu []settings.CardAction, p theme.Palette) {
	ui.Column(c).Absolute().Top(6).Bottom(6).Left(40).Right(40).Padding(4).Gap(2).Radius(theme.RadiusWindow).
		Background(p.Flyout).Border(1, p.Border).Shadow(0, 4, 12, 0, ui.RGBA(0, 0, 0, 0.35)).Children(func() {
		if len(menu) == 0 {
			ui.Text(c, "No hidden actions").FontSize(theme.Caption).TextColor(p.Muted).Padding(8)
		}
		for _, a := range menu {
			row := ui.ButtonBase(c.Key("m-" + string(a))).Height(22).PaddingX(8).Radius(theme.RadiusControl).Tooltip(help[a])
			if row.Hovered() {
				row.Background(p.Accent)
			}
			row.Children(func() {
				ui.Row(c).Fill().Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, theme.Icon(icons[a])).Size(12, 12).TextColor(p.Text)
					ui.Text(c, a.Label()).FontSize(theme.Caption).TextColor(p.Text)
				})
			})
			if row.Clicked() {
				s.run(id, a)
			}
		}
	})
}
