package library

import (
	"fmt"
	"github.com/wobsoriano/glint/internal/ui/prefs/accel"

	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	sidebarW   = 196
	inspectorW = 268
	tileW      = 184
	tileH      = 156
)

var filters = []struct {
	filter store.Filter
	icon   theme.IconName
	label  string
}{
	{store.FilterAll, theme.IconLibrary, "All Captures"},
	{store.FilterScreenshots, theme.IconImage, "Screenshots"},
	{store.FilterRecordings, theme.IconFilm, "Recordings"},
}

var captures = []struct {
	action settings.HotkeyAction
	icon   theme.IconName
	label  string
}{
	{settings.HotkeyArea, theme.IconArea, "Area"},
	{settings.HotkeyWindow, theme.IconWindow, "Window"},
	{settings.HotkeyFullscreen, theme.IconMonitor, "Full Screen"},
	{settings.HotkeyText, theme.IconScan, "Text"},
}

var sorts = []struct {
	key   settings.SortKey
	label string
}{
	{settings.SortCaptured, "Capture Date"},
	{settings.SortName, "Name"},
	{settings.SortModified, "Modified"},
}

// View builds the window.
func (l *Library) View(c *ui.Context) {
	p := theme.Apply(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	if l.notice != "" {
		c.Toast(l.notice)
		l.notice = ""
	}
	prefs := l.prefs()
	visible := l.visible()
	l.shown = visible
	l.shortcuts(c)
	root := ui.Column(c).Fill()
	if files := root.DroppedFiles(); files != nil {
		l.host.OpenFiles(files)
	}
	root.Children(func() {
		l.toolbar(c, p, prefs)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			l.sidebar(c, p)
			ui.Column(c).Grow(1).MinWidth(0).Background(p.Base).Radius(theme.RadiusWindow, theme.RadiusWindow, 0, 0).
				Border(1, p.Border).Children(func() {
				l.content(c, p, prefs, visible)
				l.status(c, p, len(visible))
			})
			if prefs.Inspector {
				l.inspector(c, p)
			}
		})
	})
	names := l.trashing
	title := fmt.Sprintf("Move %d captures to the %s?", len(names), trashName)
	if len(names) == 1 {
		title = fmt.Sprintf("Move “%s” to the %s?", names[0].Meta.Title, trashName)
	}
	detail := fmt.Sprintf("Edits and recording tracks go with them. You can restore them from the %s.", trashName)
	switch ui.AlertDialog(c, &l.asking, title, detail, "Cancel", "Move to "+trashName) {
	case 1:
		l.trash()
	case 0, ui.AlertDismissed:
		l.trashing = nil
	}
}

// focusItems gives the keyboard to the grid or the list, whichever shows.
func (l *Library) focusItems() {
	if l.prefs().View == settings.ViewList {
		l.list.Handle.Focus()
	} else {
		l.grid.Handle.Focus()
	}
}

// shortcuts are the window's keys outside the grid and the list, which take the arrows,
// Ctrl+A and Enter themselves while they have the focus.
func (l *Library) shortcuts(c *ui.Context) {
	if c.Shortcut(ui.Cmd, ui.KeyF) {
		l.searchH.Focus()
	}
	if c.Shortcut(ui.Cmd|ui.Alt, ui.KeyI) {
		l.updatePrefs(func(p *settings.LibraryPrefs) { p.Inspector = !p.Inspector })
	}
	if l.renaming != nil {
		if c.Shortcut(0, ui.KeyEscape) {
			l.renaming = nil
		}
		return
	}
	if l.searchH.Focused(c) {
		if c.Shortcut(0, ui.KeyEscape) {
			l.search = ""
			l.focusItems()
		}
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyA) {
		l.selectAll()
	}
	if c.Shortcut(ui.Cmd, ui.KeyC) {
		if sel := l.selected(); len(sel) > 0 {
			l.host.Copy(sel)
		}
	}
	if c.Shortcut(0, ui.KeyDelete) || c.Shortcut(0, ui.KeyBackspace) {
		l.askTrash()
	}
	if c.Shortcut(0, ui.KeyEnter) || c.Shortcut(0, ui.KeyF2) {
		l.startRename()
	}
	if c.Shortcut(0, ui.KeySpace) && !l.host.CloseQuickLook() {
		if sel := l.selected(); len(sel) > 0 {
			l.host.QuickLook(sel[0])
		}
	}
	if c.Shortcut(0, ui.KeyEscape) {
		l.clearSelection()
	}
}

func (l *Library) toolbar(c *ui.Context, p theme.Palette, prefs *settings.LibraryPrefs) {
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, 48)).Padding(0, bar.Right+12, 0, bar.Left+14).Gap(8).DragWindow().Children(func() {
		ui.Row(c).Width(sidebarW - 14).Gap(10).Children(func() {
			ui.Image(c, theme.Mark(false)).Size(18, 18)
			ui.Text(c, "Glint Library").FontSize(theme.Caption)
		})
		search := ui.TextInput(c.Key("search"), &l.search).Bind(&l.searchH).Placeholder("Search by name").Label("Search").Width(240)
		if search.Changed() {
			l.chosen.Clear()
			l.cursor, l.renaming = -1, nil
		}
		if search.Submitted() {
			l.focusItems()
		}
		ui.Spacer(c)
		ui.Text(c, "Sort").FontSize(theme.Caption).TextColor(p.Muted)
		at := 0
		labels := make([]string, len(sorts))
		for i, s := range sorts {
			labels[i] = s.label
			if s.key == prefs.Sort {
				at = i
			}
		}
		if ui.Segmented(c.Key("sort"), &at, labels...).Label("Sort").Changed() {
			l.updatePrefs(func(p *settings.LibraryPrefs) { p.Sort = sorts[at].key })
		}
		view := map[settings.LibraryView]int{settings.ViewGrid: 0, settings.ViewList: 1}[prefs.View]
		seg := ui.SegmentedBase(c.Key("view"), &view, 2)
		seg.Track.Padding(2).Radius(theme.RadiusControl).Background(p.Control).Label("Layout").Children(func() {
			for i, it := range []struct {
				icon  theme.IconName
				label string
			}{{theme.IconGrid, "Grid"}, {theme.IconList, "List"}} {
				s := seg.Segment(i).Padding(4, 8).Radius(theme.RadiusControl).Label(it.label).Tooltip(it.label)
				if i == view {
					s.Background(p.ControlHover)
				}
				s.Children(func() { ui.Icon(c, theme.Icon(it.icon)).Size(16, 16) })
			}
		})
		if seg.Track.Changed() {
			l.updatePrefs(func(p *settings.LibraryPrefs) {
				p.View = []settings.LibraryView{settings.ViewGrid, settings.ViewList}[view]
			})
		}
		on := prefs.Inspector
		ins := ui.ToggleBase(c.Key("inspector"), &on).Padding(6).Radius(theme.RadiusControl).Label("Inspector").Tooltip(accel.Display("Inspector (Ctrl+Alt+I)"))
		if on {
			ins.Background(p.ControlHover)
		} else if ins.Hovered() {
			ins.Background(p.Control)
		}
		ins.Children(func() { ui.Icon(c, theme.Icon(theme.IconSidebar)).Size(16, 16) })
		if ins.Changed() {
			l.updatePrefs(func(p *settings.LibraryPrefs) { p.Inspector = on })
		}
	})
}

func navRow(c *ui.Context, p theme.Palette, key string, icon theme.IconName, label, count string, active bool) ui.Element {
	row := ui.Row(c.Key(key)).Height(30).Padding(0, 8).Gap(8).Radius(theme.RadiusControl).Cursor(ui.CursorPointer)
	switch {
	case active:
		row.Background(p.Accent.Alpha(0.2))
	case row.Hovered():
		row.Background(p.ControlHover)
	}
	row.Children(func() {
		ic := ui.Icon(c, theme.Icon(icon)).Size(15, 15).TextColor(p.Muted)
		if active {
			ic.TextColor(p.Accent)
		}
		ui.Text(c, label).Grow(1)
		if count != "" {
			ui.Text(c, count).FontSize(theme.Caption).TextColor(p.Muted)
		}
	})
	return row
}

func (l *Library) sidebar(c *ui.Context, p theme.Palette) {
	side := ui.Column(c).Width(sidebarW).Padding(4, 12, 12, 12).Gap(4)
	if !c.Vibrancy() {
		side.Background(p.Layer)
	}
	side.Children(func() {
		heading(c, p, "LIBRARY")
		for _, f := range filters {
			n := 0
			for _, it := range l.items {
				if f.filter.Admits(it.Kind()) {
					n++
				}
			}
			if navRow(c, p, f.label, f.icon, f.label, fmt.Sprint(n), l.filter == f.filter).Clicked() {
				l.setFilter(f.filter)
			}
		}
		ui.Spacer(c)
		heading(c, p, "NEW CAPTURE")
		for _, a := range captures {
			if navRow(c, p, a.label, a.icon, a.label, "", false).Clicked() {
				l.host.Capture(a.action)
			}
		}
		ui.Box(c).Height(8)
		if navRow(c, p, "settings", theme.IconSettings, "Settings", "", false).Clicked() {
			l.host.OpenSettings()
		}
	})
}

func heading(c *ui.Context, p theme.Palette, s string) {
	ui.Text(c, s).FontSize(theme.Caption).FontWeight(theme.Semibold).TextColor(p.Muted).Padding(4, 8)
}

func (l *Library) content(c *ui.Context, p theme.Palette, prefs *settings.LibraryPrefs, visible []store.Item) {
	if len(visible) == 0 {
		msg := "Loading…"
		switch {
		case !l.loaded:
		case l.search != "":
			msg = "No captures match your search."
		default:
			msg = fmt.Sprintf("No captures yet. Press %s to capture an area.", accel.Display(l.host.Settings().Hotkeys.Area))
		}
		ui.Column(c).Grow(1).Center().Children(func() { ui.Text(c, msg).TextColor(p.Muted) })
		return
	}
	var e ui.Element
	if prefs.View == settings.ViewList {
		e = l.listView(c, p, visible)
	} else {
		e = ui.GridView(c.Key("grid"), &l.grid, len(visible), tileW, tileH, func(i int) { l.tile(c, p, visible[i]) }).
			Grow(1).Padding(8, 16).AutoFocus()
	}
	e.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind == ui.InputPointerDown {
			l.clicks = ev.Clicks
		}
		return false
	})
	if e.Changed() && l.renaming != nil && !l.chosen.Has(l.renaming.dir) {
		l.renaming = nil
	}
	if e.Submitted() && l.renaming == nil {
		if l.clicks >= 2 {
			if l.cursor >= 0 && l.cursor < len(visible) {
				l.host.QuickLook(visible[l.cursor])
			}
		} else {
			l.startRename()
		}
		l.clicks = 0
	}
}

func (l *Library) renameInput(c *ui.Context, it store.Item) bool {
	if l.renaming == nil || l.renaming.dir != it.Dir {
		return false
	}
	if ui.TextInput(c.Key("rename"), &l.renaming.draft).AutoFocus().Label("Name").Submitted() {
		l.commitRename()
	}
	return true
}

func (l *Library) tile(c *ui.Context, p theme.Palette, it store.Item) {
	recording := it.Kind() == store.Recording
	col := ui.Column(c).Grow(1).Padding(6).Gap(6)
	hovered := col.Hovered()
	col.Children(func() {
		box := ui.Box(c).Height(110).Radius(theme.RadiusControl).Background(p.Canvas).Border(1, p.Border).Clip().Center()
		box.Children(func() {
			if bmp := l.thumb(it); bmp != nil {
				ui.Image(c, bmp).Fit(ui.Contain).Fill()
			} else {
				icon := theme.IconImage
				if recording {
					icon = theme.IconFilm
				}
				ui.Icon(c, theme.Icon(icon)).Size(26, 26).TextColor(p.Muted)
			}
			if hovered {
				ui.Row(c).Gap(4).Attach(ui.AnchorTopRight, ui.AnchorTopRight).Top(4).Right(4).Children(func() {
					if tileAction(c, p, theme.IconCopy, "Copy") {
						l.host.Copy([]store.Item{it})
					}
					if tileAction(c, p, theme.IconEye, "Quick Look") {
						l.host.QuickLook(it)
					}
					if tileAction(c, p, theme.IconEdit, "Edit") {
						l.host.Edit(it)
					}
				})
			}
			if recording && it.Meta.DurationMs != nil {
				ui.Text(c, duration(*it.Meta.DurationMs)).FontSize(theme.Caption).TextColor(ui.RGB(255, 255, 255)).
					Background(ui.RGBA(0, 0, 0, 0.7)).Padding(0, 4).Radius(theme.RadiusControl).
					Attach(ui.AnchorBottomRight, ui.AnchorBottomRight).Bottom(4).Right(4)
			}
		})
		if !l.renameInput(c, it) {
			ui.Text(c, it.Meta.Title).FontSize(theme.Caption).SingleLine().Padding(0, 4)
		}
	})
}

func tileAction(c *ui.Context, p theme.Palette, icon theme.IconName, tip string) bool {
	b := ui.Box(c.Key(tip)).Size(24, 24).Radius(12).Center().Background(ui.RGBA(28, 28, 31, 0.9)).
		Border(1, ui.RGBA(255, 255, 255, 0.18)).Cursor(ui.CursorPointer).Tooltip(tip).Label(tip)
	if b.Hovered() {
		b.Background(p.Accent)
	}
	b.Children(func() { ui.Icon(c, theme.Icon(icon)).Size(12, 12).TextColor(ui.RGB(255, 255, 255)) })
	return b.Clicked()
}

var listColumns = []ui.TableColumn{
	{Title: "", ID: "thumb", Width: 68, Fixed: true},
	{Title: "Name", MinWidth: 120},
	{Title: "Kind", Width: 90},
	{Title: "Dimensions", Width: 100},
	{Title: "Captured", Width: 180},
	{Title: "Size", Width: 80, Align: ui.End},
}

func (l *Library) listView(c *ui.Context, p theme.Palette, visible []store.Item) ui.Element {
	return ui.Table(c.Key("list"), &l.list, listColumns, len(visible), func(row, col int) {
		it := visible[row]
		muted := func(s string) { ui.Text(c, s).FontSize(theme.Caption).TextColor(p.Muted).SingleLine() }
		switch col {
		case 0:
			ui.Box(c).Size(52, 32).Radius(theme.RadiusControl).Background(p.Canvas).Clip().Center().Children(func() {
				if bmp := l.thumb(it); bmp != nil {
					ui.Image(c, bmp).Fit(ui.Cover).Fill()
				} else {
					ui.Icon(c, theme.Icon(kindIcon(it))).Size(14, 14).TextColor(p.Muted)
				}
			})
		case 1:
			if !l.renameInput(c, it) {
				ui.Text(c, it.Meta.Title).SingleLine()
			}
		case 2:
			muted(kindLabel(it))
		case 3:
			muted(fmt.Sprintf("%d × %d", it.Meta.Width, it.Meta.Height))
		case 4:
			muted(date(it.Meta.Created))
		case 5:
			muted(humanBytes(it.Bytes))
		}
	}).Grow(1).AutoFocus()
}

func kindIcon(it store.Item) theme.IconName {
	if it.Kind() == store.Recording {
		return theme.IconFilm
	}
	return theme.IconImage
}

func kindLabel(it store.Item) string {
	if it.Kind() == store.Recording {
		return "Recording"
	}
	return "Screenshot"
}

func (l *Library) status(c *ui.Context, p theme.Palette, n int) {
	text := fmt.Sprintf("%d capture(s)", n)
	if s := len(l.selected()); s > 0 {
		text = fmt.Sprintf("%d of %d selected", s, n)
	}
	ui.Row(c).Height(26).Padding(0, 16).Children(func() {
		ui.Text(c, text).FontSize(theme.Caption).TextColor(p.Muted)
	})
}

func (l *Library) inspector(c *ui.Context, p theme.Palette) {
	items := l.selected()
	ui.Column(c).Width(inspectorW).Padding(16).Gap(16).Background(p.Layer).Children(func() {
		ui.Scroll(c).Grow(1).Gap(8).Children(func() {
			switch len(items) {
			case 0:
				ui.Text(c, "Select a capture to see its details.").TextColor(p.Muted).Wrap()
			case 1:
				l.details(c, p, items[0])
			default:
				var total uint64
				for _, it := range items {
					total += it.Bytes
				}
				ui.Text(c, fmt.Sprintf("%d captures", len(items))).FontWeight(theme.Semibold)
				infoRow(c, p, "Disk usage", humanBytes(total))
			}
		})
		if len(items) == 0 {
			return
		}
		ui.Row(c).Wrap().Gap(8).Children(func() {
			if len(items) == 1 {
				if iconButton(c, true, theme.IconEdit, "Edit") {
					l.host.Edit(items[0])
				}
				if iconButton(c, false, theme.IconEye, "Quick Look") {
					l.host.QuickLook(items[0])
				}
			}
			if iconButton(c, false, theme.IconCopy, "Copy") {
				l.host.Copy(items)
			}
			if l.pickFolder != nil && iconButton(c, false, theme.IconSave, "Export…") {
				l.export(items)
			}
			if iconButton(c, false, theme.IconFolder, "Show in "+fileManagerName) {
				l.host.Reveal(items)
			}
			if iconButton(c, false, theme.IconTrash, "Move to "+trashName) {
				l.askTrash()
			}
		})
	})
}

func iconButton(c *ui.Context, primary bool, icon theme.IconName, label string) bool {
	build := ui.Button
	if primary {
		build = ui.PrimaryButton
	}
	return build(c.Key(label), "").Label(label).Children(func() {
		ui.Icon(c, theme.Icon(icon)).Size(14, 14)
		ui.Text(c, label).SingleLine()
	}).Clicked()
}

func (l *Library) details(c *ui.Context, p theme.Palette, it store.Item) {
	recording := it.Kind() == store.Recording
	editing := map[[2]bool]string{
		{true, false}:  "Annotated, edits saved",
		{false, false}: "Original, no edits",
		{true, true}:   "Studio project saved",
		{false, true}:  "No Studio edits",
	}[[2]bool{it.IsEdited(), recording}]
	ui.Text(c, it.Meta.Title).FontWeight(theme.Semibold).Wrap()
	ui.Text(c, kindLabel(it)).FontSize(theme.Caption).TextColor(p.Muted)
	ui.Divider(c)
	infoRow(c, p, "Dimensions", fmt.Sprintf("%d × %d", it.Meta.Width, it.Meta.Height))
	infoRow(c, p, "Captured", date(it.Meta.Created))
	infoRow(c, p, "Modified", date(it.Meta.Modified))
	if it.Meta.DurationMs != nil {
		infoRow(c, p, "Duration", duration(*it.Meta.DurationMs))
	}
	infoRow(c, p, "Disk usage", humanBytes(it.Bytes))
	infoRow(c, p, "Editing", editing)
	if it.Meta.Source != nil {
		infoRow(c, p, "Source", *it.Meta.Source)
	}
	if it.Meta.ImportedFrom != nil {
		infoBlock(c, p, "Imported from", *it.Meta.ImportedFrom)
	}
	infoBlock(c, p, "Location", it.Dir)
}

func infoRow(c *ui.Context, p theme.Palette, label, value string) {
	ui.Row(c.Key(label)).Gap(12).AlignItems(ui.Start).Children(func() {
		ui.Text(c, label).FontSize(theme.Caption).TextColor(p.Muted)
		ui.Spacer(c)
		ui.Text(c, value).FontSize(theme.Caption).Wrap().Shrink(1)
	})
}

func infoBlock(c *ui.Context, p theme.Palette, label, value string) {
	ui.Column(c.Key(label)).Gap(4).Children(func() {
		ui.Text(c, label).FontSize(theme.Caption).TextColor(p.Muted)
		ui.Text(c, value).FontSize(theme.Caption).Wrap().Selectable()
	})
}
