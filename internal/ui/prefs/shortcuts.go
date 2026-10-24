package prefs

import (
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/prefs/accel"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

var success = ui.RGB(0x34, 0xC7, 0x59)

func (p *Prefs) shortcuts(c *ui.Context, pal theme.Palette) {
	s := p.s()
	group(c, pal, "GLOBAL SHORTCUTS", func() {
		refuses := "Windows"
		if isMac {
			refuses = "macOS"
		}
		noteText(c, pal, "Click Change, then press the new combination. If "+refuses+" refuses it, the previous shortcut keeps working.")
		for _, a := range settings.HotkeyActions {
			p.shortcutRow(c, pal, a, *s.Hotkeys.Binding(a))
		}
		ui.Row(c).Children(func() {
			if ui.Button(c, "Restore Defaults").Clicked() {
				p.restoreDefaults()
			}
		})
	})
}

func (p *Prefs) shortcutRow(c *ui.Context, pal theme.Palette, a settings.HotkeyAction, binding string) {
	recording := p.recording != nil && *p.recording == a
	ui.Column(c.Key(a)).Gap(4).Children(func() {
		ui.Row(c).Gap(12).Children(func() {
			ui.Text(c, a.Label()).Grow(1)
			if recording {
				box := ui.Box(c.Key("recorder")).Height(26).Padding(0, 8).Center().Radius(theme.RadiusControl).
					Border(1, pal.Accent).Focusable().AutoFocus().Label("New shortcut")
				box.HandleInput(func(ev ui.InputEvent) bool {
					if ev.Kind != ui.InputKeyDown {
						return ev.Kind == ui.InputText
					}
					p.recordKey(ev.Mods, ev.Key)
					return true
				})
				box.Children(func() {
					ui.Text(c, "Press a shortcut… (Esc to cancel)").FontSize(theme.Caption).TextColor(pal.Accent)
				})
			} else {
				shown := accel.Display(binding)
				if binding == "" {
					shown = "None"
				}
				ui.Text(c, shown).FontSize(theme.Caption).Padding(3, 8).Radius(theme.RadiusControl).
					Background(pal.Control).Border(1, pal.Border)
			}
			label := "Change"
			if recording {
				label = "Cancel"
			}
			if ui.Button(c.Key("change"), label).Label(label + " " + a.Label()).Clicked() {
				if recording {
					p.stopRecording()
				} else {
					p.startRecording(a)
				}
			}
		})
		switch n := p.note; {
		case n != nil && n.action == a && n.failed:
			ui.Text(c, n.text).FontSize(theme.Caption).TextColor(pal.Danger).Wrap()
		case n != nil && n.action == a:
			ui.Text(c, n.text).FontSize(theme.Caption).TextColor(success)
		default:
			if err := p.host.HotkeyError(a); err != nil {
				ui.Text(c, err.Error()).FontSize(theme.Caption).TextColor(pal.Danger).Wrap()
			}
		}
	})
}

func (p *Prefs) startRecording(a settings.HotkeyAction) {
	if p.recording == nil {
		p.host.SuspendHotkeys()
	}
	p.recording, p.note = &a, nil
}

func (p *Prefs) stopRecording() {
	if p.recording != nil {
		p.recording = nil
		p.host.ResumeHotkeys()
	}
}

// recordKey takes a key pressed while recording: Escape cancels, a modifier alone waits
// for the key, a combination that cannot be global says why and keeps recording, and
// any other is offered to the host.
func (p *Prefs) recordKey(mods ui.Modifiers, key ui.Key) {
	a := *p.recording
	switch {
	case key == ui.KeyEscape && mods == 0:
		p.stopRecording()
		return
	case key == ui.KeyUnknown:
		return
	}
	shortcut, err := accel.FromKey(mods, key)
	if err != nil {
		p.note = &note{action: a, text: err.Error(), failed: true}
		return
	}
	p.stopRecording()
	p.rebind(a, shortcut)
}

func (p *Prefs) rebind(a settings.HotkeyAction, shortcut string) bool {
	if err := p.host.RebindHotkey(a, shortcut); err != nil {
		p.note = &note{action: a, text: err.Error(), failed: true}
		return false
	}
	*p.s().Hotkeys.Binding(a) = shortcut
	p.changed()
	p.note = &note{action: a, text: "Now " + accel.Display(shortcut)}
	return true
}

func (p *Prefs) restoreDefaults() {
	p.stopRecording()
	defaults := settings.Default().Hotkeys
	var first *note
	for _, a := range settings.HotkeyActions {
		if p.rebind(a, *defaults.Binding(a)) || first != nil {
			continue
		}
		first = p.note
	}
	p.note = first
}

// Close ends a recording the window was making, giving the app its shortcuts back.
func (p *Prefs) Close() { p.stopRecording() }
