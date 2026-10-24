package record

import (
	"fmt"
	"strconv"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	pickerW, pickerH, pickerMargin = 820, 56, 28
	panelGap, rowH, scriptH        = 8, 30, 262
	meterW                         = 48
)

var countdowns = []uint32{0, 1, 3, 5}

type panel int

const (
	noPanel panel = iota
	cameraPanel
	micPanel
	timerPanel
	scriptPanel
)

// picker is the bar that chooses what to record and with which devices. It is a Panel
// window, which stays above the taskbar and takes the keyboard (Esc, Enter) from the app in
// front; its lists open above it as flyouts that close as menus do.
type picker struct {
	win      *mygo.Window
	panelWin *mygo.Window
	panel    panel
	display  platform.Display

	loaded  bool
	mics    []capture.AudioDevice
	cameras []media.CameraDevice
	script  string
}

func (c *Controller) showPicker() {
	if c.picker != nil {
		c.picker.close()
	}
	d, ok := c.displayAtCursor()
	if c.target != nil {
		d, ok = c.target.Display, true
	}
	if !ok {
		return
	}
	c.forget(SurfacePicker)
	pk := &picker{display: d, script: c.prefs().Script}
	r := bottomCentre(d, pickerW, pickerH, pickerMargin)
	pk.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Glint recording", Width: dips(r.W, d.Scale), Height: dips(r.H, d.Scale),
		Frameless: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true, DisableResize: true, Panel: true,
		Content: ui.View(func(ctx *ui.Context) { c.pickerView(ctx, pk) }),
	})
	pk.win.OnClose(func(e *mygo.CloseEvent) {
		e.PreventDefault()
		later(func() {
			if c.picker == pk {
				c.closePicker()
			}
		})
	})
	place(pk.win, r)
	pk.win.Show()
	pk.win.Focus()
	c.picker = pk
	go func() {
		mics, _ := capture.Microphones()
		cams, _ := media.Cameras()
		mygo.RunOnMain(func() {
			pk.mics, pk.cameras, pk.loaded = mics, cams, true
			if pk.win != nil {
				pk.win.Invalidate()
			}
		})
	}()
}

func (pk *picker) close() {
	pk.closePanel()
	if pk.win != nil {
		pk.win.Destroy()
		pk.win = nil
	}
}

func (pk *picker) closePanel() {
	if pk.panelWin != nil {
		pk.panelWin.Destroy()
		pk.panelWin = nil
	}
	pk.panel = noPanel
}

func panelHeight(p panel, mics, cameras int) int {
	switch p {
	case cameraPanel:
		return 16 + rowH*(cameras+1)
	case micPanel:
		return 16 + rowH*(mics+1)
	case timerPanel:
		return 16 + rowH
	}
	return scriptH
}

// togglePanel opens one list above the bar, or closes it when it shows. It is focusable, so
// it takes the keyboard and closes as the user presses outside it.
func (c *Controller) togglePanel(pk *picker, p panel) {
	open := pk.panel
	pk.closePanel()
	c.forget(SurfacePanel)
	if open == p {
		return
	}
	pk.panel = p
	pk.script = c.prefs().Script
	var win *mygo.Window
	win = mygo.NewFlyout(mygo.FlyoutOptions{
		Parent: pk.win, Anchor: mygo.Rectangle{Width: pickerW, Height: pickerH}, Placement: mygo.PlacementTopStart, Gap: panelGap,
		Width: pickerW, Height: panelHeight(p, len(pk.mics), len(pk.cameras)), Focusable: true,
		Content: ui.View(func(ctx *ui.Context) { c.panelView(ctx, pk, p) }),
	})
	win.OnClosed(func() {
		if pk.panelWin == win {
			pk.panelWin, pk.panel = nil, noPanel
			if pk.win != nil {
				pk.win.Invalidate()
			}
		}
	})
	pk.panelWin = win
}

func caution(p theme.Palette) ui.Color {
	if p.Dark {
		return ui.RGB(0xFC, 0xE1, 0x00)
	}
	return ui.RGB(0x9D, 0x5D, 0x00)
}

func (c *Controller) pickerView(ctx *ui.Context, pk *picker) {
	p := theme.Apply(ctx)
	ctx.Root().Background(p.Flyout.Over(ui.RGB(0, 0, 0)))
	counting := c.stage == counting
	if ctx.Shortcut(0, ui.KeyEscape) {
		switch {
		case counting:
			c.cancelCountdown()
		case pk.panel != noPanel:
			pk.closePanel()
		default:
			later(c.closePicker)
		}
	}
	if ctx.Shortcut(0, ui.KeyEnter) && !counting {
		later(c.record)
	}
	ctx.After(50 * time.Millisecond)
	prefs := c.prefs()
	kind := prefs.Target
	if c.target != nil {
		kind = c.target.Kind
	}

	ui.Row(ctx).Fill().AlignItems(ui.Center).Gap(4).PaddingX(8).Children(func() {
		for _, s := range []struct {
			kind  settings.RecordTarget
			icon  theme.IconName
			label string
		}{{settings.RecordDisplay, theme.IconMonitor, "Display"}, {settings.RecordWindow, theme.IconWindow, "Window"}, {settings.RecordArea, theme.IconArea, "Area"}} {
			chip := c.chip(ctx, p, "src-"+s.label, kind == s.kind, !counting, "")
			chip.Children(func() {
				ui.Icon(ctx, theme.Icon(s.icon)).Size(16, 16).TextColor(p.Text)
				ui.Text(ctx, s.label).TextColor(p.Text).SingleLine()
			})
			if chip.Clicked() && !counting {
				k := s.kind
				later(func() { c.choose(k) })
			}
		}
		ui.Box(ctx).Size(1, 26).MarginX(4).Background(p.Border)

		cams := make([]namedDevice, len(pk.cameras))
		for i, d := range pk.cameras {
			cams[i] = namedDevice{string(d.ID), d.Name}
		}
		camIcon, camColor, camLabel, camTip := theme.IconCameraOff, p.Text, "No camera", "Camera"
		switch state, name := deviceChoice(prefs.Camera, pk.loaded, cams); state {
		case deviceLoading:
			camIcon, camLabel = theme.IconCamera, "Camera"
		case devicePresent:
			camIcon, camLabel = theme.IconCamera, name
		case deviceMissing:
			camColor, camLabel, camTip = caution(p), "Camera unavailable", "Camera unavailable: recording continues without it"
		}
		cam := c.chip(ctx, p, "camera", pk.panel == cameraPanel, true, camTip).MaxWidth(176)
		cam.Children(func() {
			ui.Icon(ctx, theme.Icon(camIcon)).Size(16, 16).TextColor(camColor)
			ui.Text(ctx, camLabel).TextColor(camColor).SingleLine().Ellipsis("…")
		})
		if cam.Clicked() {
			c.togglePanel(pk, cameraPanel)
		}

		mics := make([]namedDevice, len(pk.mics))
		for i, d := range pk.mics {
			mics[i] = namedDevice{string(d.ID), d.Name}
		}
		micIcon, micColor, micTip := theme.IconMic, p.Text, "Microphone"
		switch state, _ := deviceChoice(prefs.Mic, pk.loaded, mics); state {
		case deviceOff:
			micIcon, micTip = theme.IconMicOff, "No microphone"
		case deviceMissing:
			micIcon, micColor, micTip = theme.IconMicOff, caution(p), "Microphone unavailable: recording continues without it"
		}
		level, _ := c.micLevel()
		mic := c.chip(ctx, p, "mic", pk.panel == micPanel, true, micTip)
		mic.Children(func() {
			ui.Icon(ctx, theme.Icon(micIcon)).Size(16, 16).TextColor(micColor)
			ui.Box(ctx).Size(meterW, 6).Radius(3).Background(p.ControlHover).Clip().Children(func() {
				ui.Box(ctx).Size(meterW*meterFraction(level), 6).Radius(3).Background(p.Accent)
			})
		})
		if mic.Clicked() {
			c.togglePanel(pk, micPanel)
		}

		sysIcon, sysTip := theme.IconSpeakerOff, "System audio off"
		if prefs.SystemAudio {
			sysIcon, sysTip = theme.IconSpeaker, "Recording system audio"
		}
		sys := c.chip(ctx, p, "system-audio", prefs.SystemAudio, true, sysTip)
		sys.Children(func() { ui.Icon(ctx, theme.Icon(sysIcon)).Size(16, 16).TextColor(p.Text) })
		if sys.Clicked() {
			c.updatePrefs(func(r *settings.RecordPrefs) { r.SystemAudio = !r.SystemAudio })
		}

		timer := c.chip(ctx, p, "timer", pk.panel == timerPanel, true, "Start timer")
		timer.Children(func() {
			ui.Icon(ctx, theme.Icon(theme.IconTimer)).Size(16, 16).TextColor(p.Text)
			ui.Text(ctx, countdownLabel(prefs.Countdown)).TextColor(p.Text).SingleLine()
		})
		if timer.Clicked() {
			c.togglePanel(pk, timerPanel)
		}

		prompt := c.chip(ctx, p, "prompter", prefs.Teleprompter || pk.panel == scriptPanel, true, "Teleprompter")
		prompt.Children(func() { ui.Icon(ctx, theme.Icon(theme.IconPrompter)).Size(16, 16).TextColor(p.Text) })
		if prompt.Clicked() {
			c.togglePanel(pk, scriptPanel)
		}

		ui.Spacer(ctx)
		if counting {
			cancel := ui.Button(ctx.Key("cancel"), "Cancel").Height(34)
			c.probe(SurfacePicker, "cancel", cancel)
			if cancel.Clicked() {
				c.cancelCountdown()
			}
		} else {
			label := "Choose…"
			if c.target != nil {
				label = "Record"
			}
			rec := ui.ButtonBase(ctx.Key("record")).Height(34).PaddingX(12).Gap(6).AlignItems(ui.Center).
				Radius(theme.RadiusControl).Background(danger).Cursor(ui.CursorPointer).Label(label)
			if rec.Hovered() {
				rec.Background(dangerHot)
			}
			rec.Children(func() {
				ui.Icon(ctx, theme.Icon(theme.IconRecord)).Size(15, 15).TextColor(white)
				ui.Text(ctx, label).FontWeight(theme.Semibold).TextColor(white).SingleLine()
			})
			c.probe(SurfacePicker, "record", rec)
			if rec.Clicked() {
				later(c.record)
			}
		}
		closeBtn := ui.ButtonBase(ctx.Key("close")).Size(32, 32).Center().Radius(theme.RadiusControl).Cursor(ui.CursorPointer).
			Label("Close").Tooltip("Close (Esc)")
		if closeBtn.Hovered() {
			closeBtn.Background(p.ControlHover)
		}
		closeBtn.Children(func() { ui.Icon(ctx, theme.Icon(theme.IconClose)).Size(15, 15).TextColor(p.Text) })
		c.probe(SurfacePicker, "close", closeBtn)
		if closeBtn.Clicked() {
			later(c.closePicker)
		}
	})
}

func countdownLabel(n uint32) string {
	if n == 0 {
		return "Off"
	}
	return fmt.Sprintf("%d s", n)
}

// chip is a button of the picker's bar: lit while active, dimmed while it takes no clicks.
func (c *Controller) chip(ctx *ui.Context, p theme.Palette, name string, active, enabled bool, tip string) ui.Element {
	b := ui.ButtonBase(ctx.Key(name)).Height(36).PaddingX(10).Gap(6).AlignItems(ui.Center).Shrink(0).
		Radius(theme.RadiusControl).Disabled(!enabled)
	if tip != "" {
		b.Tooltip(tip).Label(tip)
	}
	switch {
	case !enabled:
		b.Opacity(0.4)
	case active:
		b.Background(p.Accent.Alpha(0.25)).Cursor(ui.CursorPointer)
	case b.Hovered():
		b.Background(p.ControlHover).Cursor(ui.CursorPointer)
	default:
		b.Cursor(ui.CursorPointer)
	}
	c.probe(SurfacePicker, name, b)
	return b
}

func (c *Controller) panelView(ctx *ui.Context, pk *picker, which panel) {
	p := theme.Apply(ctx)
	ctx.Root().Background(ui.Transparent)
	prefs := c.prefs()
	box := ui.Column(ctx).Fill().Radius(theme.RadiusWindow).Background(p.Flyout.Over(ui.RGB(0, 0, 0))).Border(1, p.Border)
	switch which {
	case cameraPanel, micPanel:
		box.Padding(8).Children(func() {
			if which == cameraPanel {
				if c.panelRow(ctx, p, "cam-none", "No camera", prefs.Camera == nil) {
					c.setCamera(pk, nil)
				}
				for _, d := range pk.cameras {
					id := string(d.ID)
					if c.panelRow(ctx, p, "cam-"+id, d.Name, prefs.Camera != nil && *prefs.Camera == id) {
						c.setCamera(pk, &id)
					}
				}
				return
			}
			if c.panelRow(ctx, p, "mic-none", "No microphone", prefs.Mic == nil) {
				c.setMic(pk, nil)
			}
			for _, d := range pk.mics {
				id := string(d.ID)
				if c.panelRow(ctx, p, "mic-"+id, d.Name, prefs.Mic != nil && *prefs.Mic == id) {
					c.setMic(pk, &id)
				}
			}
		})
	case timerPanel:
		box.Padding(8).Children(func() {
			ui.Row(ctx).Height(rowH).Gap(8).AlignItems(ui.Center).Children(func() {
				for _, n := range countdowns {
					if c.choiceChip(ctx, p, "timer-"+strconv.Itoa(int(n)), countdownLabel(n), prefs.Countdown == n) {
						c.updatePrefs(func(r *settings.RecordPrefs) { r.Countdown = n })
						later(func() { pk.closePanel(); pk.win.Invalidate() })
					}
				}
			})
		})
	case scriptPanel:
		box.Padding(12).Gap(8).Children(func() { c.scriptView(ctx, p, pk) })
	}
}

func (c *Controller) panelRow(ctx *ui.Context, p theme.Palette, key, label string, selected bool) bool {
	row := ui.ButtonBase(ctx.Key(key)).Height(rowH).PaddingX(8).Gap(8).AlignItems(ui.Center).Justify(ui.Start).Radius(theme.RadiusControl).Cursor(ui.CursorPointer)
	if row.Hovered() {
		row.Background(p.ControlHover)
	}
	row.Children(func() {
		ui.Box(ctx).Size(16, 16).Children(func() {
			if selected {
				ui.Icon(ctx, theme.Icon(theme.IconCheck)).Size(14, 14).TextColor(p.Accent)
			}
		})
		ui.Text(ctx, label).TextColor(p.Text).SingleLine().Ellipsis("…")
	})
	c.probe(SurfacePanel, key, row)
	return row.Clicked()
}

func (c *Controller) choiceChip(ctx *ui.Context, p theme.Palette, key, label string, on bool) bool {
	b := ui.ButtonBase(ctx.Key(key)).Height(28).PaddingX(12).Center().Radius(theme.RadiusControl).Cursor(ui.CursorPointer).Border(1, p.Border)
	switch {
	case on:
		b.Background(p.Accent).Border(1, p.Accent)
	case b.Hovered():
		b.Background(p.ControlHover)
	}
	text := p.Text
	if on {
		text = p.OnAccent
	}
	b.Children(func() { ui.Text(ctx, label).FontSize(theme.Caption).TextColor(text).SingleLine() })
	c.probe(SurfacePanel, key, b)
	return b.Clicked()
}

func (c *Controller) setMic(pk *picker, id *string) {
	c.updatePrefs(func(r *settings.RecordPrefs) { r.Mic = id })
	c.restartMonitor()
	later(func() { pk.closePanel(); pk.win.Invalidate() })
}

func (c *Controller) setCamera(pk *picker, id *string) {
	c.updatePrefs(func(r *settings.RecordPrefs) { r.Camera = id })
	later(func() {
		c.showCamera()
		pk.closePanel()
		pk.win.Invalidate()
	})
}

func (c *Controller) scriptView(ctx *ui.Context, p theme.Palette, pk *picker) {
	prefs := c.prefs()
	ui.Row(ctx).AlignItems(ui.Center).Justify(ui.SpaceBetween).Children(func() {
		ui.Text(ctx, "Teleprompter").FontWeight(theme.Semibold).TextColor(p.Text)
		ui.Row(ctx).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(ctx, "Show while recording").FontSize(theme.Caption).TextColor(p.Muted)
			on := prefs.Teleprompter
			sw := ui.Switch(ctx.Key("prompter-on"), &on).Label("Show while recording")
			c.probe(SurfacePanel, "prompter-on", sw)
			if on != prefs.Teleprompter {
				c.updatePrefs(func(r *settings.RecordPrefs) { r.Teleprompter = on })
				pk.win.Invalidate()
			}
		})
	})
	area := ui.TextArea(ctx.Key("script"), &pk.script).Height(150).Placeholder("Type or paste your script").AutoFocus()
	c.probe(SurfacePanel, "script", area)
	if pk.script != prefs.Script {
		script := pk.script
		c.updatePrefs(func(r *settings.RecordPrefs) { r.Script = script })
	}
	ui.Row(ctx).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.Text(ctx, "Lines").FontSize(theme.Caption).TextColor(p.Muted)
		for n := uint32(1); n <= 5; n++ {
			if c.choiceChip(ctx, p, "lines-"+strconv.Itoa(int(n)), strconv.Itoa(int(n)), prefs.PrompterLines == n) {
				c.updatePrefs(func(r *settings.RecordPrefs) { r.PrompterLines = n })
			}
		}
		ui.Text(ctx, "Scroll the teleprompter to move through the script").FontSize(theme.Caption).TextColor(p.Muted).Grow(1).SingleLine()
		clear := ui.Button(ctx.Key("script-clear"), "Clear")
		c.probe(SurfacePanel, "script-clear", clear)
		if clear.Clicked() {
			pk.script = ""
			c.updatePrefs(func(r *settings.RecordPrefs) { r.Script = "" })
		}
	})
}
