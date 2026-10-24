package prefs

import (
	"fmt"
	"runtime"

	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// Pane is one page of the Settings window.
type Pane int

const (
	General Pane = iota
	Screenshots
	Recordings
	Overlay
	Shortcuts
	Privacy
	About
)

var panes = []struct {
	pane  Pane
	label string
	icon  theme.IconName
}{
	{General, "General", theme.IconSettings},
	{Screenshots, "Screenshots", theme.IconCamera},
	{Recordings, "Recordings", theme.IconFilm},
	{Overlay, "Overlay", theme.IconStack},
	{Shortcuts, "Shortcuts", theme.IconKeyboard},
	{Privacy, "Privacy", theme.IconShield},
	{About, "About", theme.IconSparkles},
}

var isMac = runtime.GOOS == "darwin"

// Version is the app's version, which the About pane shows.
var Version = "dev"

// Prefs is the Settings window's state. Build it with New and show View in a window; Open
// does both. All methods run on the main thread.
type Prefs struct {
	host Host
	Pane Pane
	// pickFolder asks for a folder and openPath opens one in Explorer or Finder; nil leaves
	// those buttons out.
	pickFolder func() (string, bool)
	openPath   func(path string)

	// recording is the shortcut being recorded, nil when none is.
	recording *settings.HotkeyAction
	note      *note
	loginErr  string
}

// note is the outcome of the last shortcut change, shown under its row.
type note struct {
	action settings.HotkeyAction
	text   string
	failed bool
}

// New makes the Settings window's state, showing pane first.
func New(host Host, pane Pane, pickFolder func() (string, bool), openPath func(string)) *Prefs {
	return &Prefs{host: host, Pane: pane, pickFolder: pickFolder, openPath: openPath}
}

func (p *Prefs) s() *settings.Settings { return p.host.Settings() }

func (p *Prefs) changed() { p.host.SettingsChanged() }

// View builds the window.
func (p *Prefs) View(c *ui.Context) {
	pal := theme.Apply(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	bar := c.TitleBar()
	top := max(bar.Height, 40)
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		nav := ui.Column(c).Width(190).Padding(0, 12, 12, 12).Gap(4)
		if !c.Vibrancy() {
			nav.Background(pal.Layer)
		}
		nav.Children(func() {
			ui.Row(c).Height(top).Gap(10).Padding(0, 0, 0, bar.Left+2).DragWindow().Children(func() {
				ui.Image(c, theme.Mark(false)).Size(18, 18)
				ui.Text(c, "Glint Settings").FontSize(theme.Caption)
			})
			for _, it := range panes {
				if navRow(c, pal, it.label, it.icon, p.Pane == it.pane).Clicked() {
					p.show(it.pane)
				}
			}
		})
		ui.Column(c).Grow(1).MinWidth(0).Background(pal.Base).Border(1, pal.Border).
			Radius(theme.RadiusWindow, 0, 0, 0).Children(func() {
			ui.Row(c).Height(top).Padding(0, bar.Right+12, 0, 24).DragWindow().Children(func() {
				ui.Text(c, panes[p.Pane].label).FontSize(theme.Subtitle).FontWeight(theme.Semibold)
			})
			ui.Scroll(c.Key(p.Pane)).Grow(1).Padding(8, 24, 24, 24).Gap(16).Children(func() {
				switch p.Pane {
				case General:
					p.general(c, pal)
				case Screenshots:
					p.screenshots(c, pal)
				case Recordings:
					p.recordings(c, pal)
				case Overlay:
					p.overlay(c, pal)
				case Shortcuts:
					p.shortcuts(c, pal)
				case Privacy:
					p.privacy(c, pal)
				case About:
					p.about(c, pal)
				}
			})
		})
	})
}

func (p *Prefs) show(pane Pane) {
	p.stopRecording()
	p.Pane = pane
}

func navRow(c *ui.Context, pal theme.Palette, label string, icon theme.IconName, active bool) ui.Element {
	row := ui.Row(c.Key(label)).Height(32).Padding(0, 8).Gap(8).Radius(theme.RadiusControl).Cursor(ui.CursorPointer)
	switch {
	case active:
		row.Background(pal.Accent.Alpha(0.2))
	case row.Hovered():
		row.Background(pal.ControlHover)
	}
	row.Children(func() {
		ic := ui.Icon(c, theme.Icon(icon)).Size(15, 15).TextColor(pal.Muted)
		if active {
			ic.TextColor(pal.Accent)
		}
		ui.Text(c, label)
	})
	return row
}

func group(c *ui.Context, pal theme.Palette, title string, fn func()) {
	ui.Column(c.Key(title)).Gap(12).Padding(16).Radius(theme.RadiusWindow).Background(pal.Layer).Border(1, pal.Border).Children(func() {
		ui.Text(c, title).FontSize(theme.Caption).FontWeight(theme.Semibold).TextColor(pal.Muted)
		fn()
	})
}

func noteText(c *ui.Context, pal theme.Palette, s string) {
	ui.Text(c, s).FontSize(theme.Caption).TextColor(pal.Muted).Wrap()
}

// toggle is a setting turned on and off by a switch, or a click anywhere on its row; it
// saves the settings as it changes.
func (p *Prefs) toggle(c *ui.Context, label string, on *bool) bool {
	changed := false
	row := ui.Row(c.Key(label)).Gap(12)
	row.Children(func() {
		ui.Text(c, label).Grow(1)
		changed = ui.Switch(c, on).Label(label).Changed()
	})
	if row.Clicked() {
		*on, changed = !*on, true
	}
	if changed {
		p.changed()
	}
	return changed
}

// labeled puts a control at the end of a row named by label.
func labeled(c *ui.Context, label string, control func()) {
	ui.Row(c.Key(label)).Gap(12).Children(func() {
		ui.Text(c, label).Grow(1)
		control()
	})
}

// choose is a segmented control over options, saving the settings when one is chosen.
func choose[T comparable](p *Prefs, c *ui.Context, label string, value *T, options []T, labels []string) {
	labeled(c, label, func() {
		at := 0
		for i, o := range options {
			if o == *value {
				at = i
			}
		}
		if ui.Segmented(c.Key(label), &at, labels...).Label(label).Changed() {
			*value = options[at]
			p.changed()
		}
	})
}

func (p *Prefs) folderRow(c *ui.Context, pal theme.Palette, label string, path string, change func(string)) {
	ui.Row(c.Key(label)).Gap(12).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
			ui.Text(c, label)
			ui.Text(c, path).FontSize(theme.Caption).TextColor(pal.Muted).SingleLine()
		})
		if p.openPath != nil && ui.Button(c.Key("open"), "Open").Clicked() {
			p.openPath(path)
		}
		if change != nil && p.pickFolder != nil && ui.Button(c.Key("change"), "Change…").Clicked() {
			if dir, ok := p.pickFolder(); ok {
				change(dir)
				p.changed()
			}
		}
	})
}

func (p *Prefs) general(c *ui.Context, pal theme.Palette) {
	s := p.s()
	group(c, pal, "STARTUP", func() {
		on := s.LaunchAtLogin
		row := ui.Row(c.Key("login")).Gap(12)
		changed := false
		row.Children(func() {
			ui.Text(c, "Launch at login").Grow(1)
			changed = ui.Switch(c, &on).Label("Launch at login").Changed()
		})
		if row.Clicked() {
			on, changed = !on, true
		}
		if changed {
			if err := p.host.SetLaunchAtLogin(on); err != nil {
				p.loginErr = err.Error()
			} else {
				p.loginErr = ""
				s.LaunchAtLogin = on
				p.changed()
			}
		}
		if p.loginErr != "" {
			ui.Text(c, p.loginErr).FontSize(theme.Caption).TextColor(pal.Danger).Wrap()
		}
		where := "tray"
		if isMac {
			where = "menu bar"
		}
		noteText(c, pal, "At login Glint starts quietly in the "+where+". Opening Glint yourself shows the Library.")
	})
	group(c, pal, "CAPTURES", func() {
		p.toggle(c, "Include Glint windows in screenshots", &s.IncludeOwnWindows)
		noteText(c, pal, "Off by default: previews, pins and editors stay out of your captures.")
	})
	group(c, pal, "LIBRARY", func() {
		p.folderRow(c, pal, "Library location", store.DefaultRoot(), nil)
	})
}

func (p *Prefs) screenshots(c *ui.Context, pal theme.Palette) {
	s := p.s()
	group(c, pal, "AFTER CAPTURE", func() {
		noteText(c, pal, "Each screenshot is added to the Library first, then any of these run.")
		p.toggle(c, "Show preview", &s.AfterShot.Preview)
		p.toggle(c, "Copy to clipboard", &s.AfterShot.Copy)
		p.toggle(c, "Save to the export folder", &s.AfterShot.Save)
		p.toggle(c, "Open the editor", &s.AfterShot.Edit)
		p.toggle(c, "Pin above other windows", &s.AfterShot.Pin)
	})
	group(c, pal, "SAVING", func() {
		choose(p, c, "Format", &s.Format, []settings.ExportFormat{settings.FormatPNG, settings.FormatJPEG}, []string{"PNG", "JPEG"})
		labeled(c, "JPEG quality (also used by Compress)", func() {
			q := float64(s.JPEGQuality)
			ui.StepSlider(c.Key("quality"), &q, 30, 100, 1).Label("JPEG quality").Width(200)
			if uint8(q) != s.JPEGQuality {
				s.JPEGQuality = uint8(q)
				p.changed()
			}
			ui.Text(c, fmt.Sprintf("%d%%", s.JPEGQuality)).FontSize(theme.Caption).TextColor(pal.Muted).Width(36)
		})
		p.folderRow(c, pal, "Export folder", s.SaveDir, func(dir string) { s.SaveDir = dir })
		p.toggle(c, "Save without asking where", &s.SaveWithoutDialog)
	})
	group(c, pal, "CAPTURE", func() {
		choose(p, c, "Self-timer", &s.TimerSeconds, []uint32{3, 5, 10}, []string{"3 s", "5 s", "10 s"})
		p.toggle(c, "Add a shadow to window captures", &s.WindowShadow)
		p.toggle(c, "Show magnifier while selecting", &s.ShowLoupe)
	})
}

func (p *Prefs) recordings(c *ui.Context, pal theme.Palette) {
	s := p.s()
	group(c, pal, "AFTER RECORDING", func() {
		noteText(c, pal, "Each recording is added to the Library first, then any of these run.")
		p.toggle(c, "Show preview", &s.AfterRecording.Preview)
		p.toggle(c, "Copy the video file", &s.AfterRecording.Copy)
		p.toggle(c, "Save to the export folder", &s.AfterRecording.Save)
		p.toggle(c, "Open in Studio", &s.AfterRecording.Studio)
	})
	group(c, pal, "RECORDING DEFAULTS", func() {
		choose(p, c, "Countdown", &s.Record.Countdown, []uint32{0, 3, 5, 10}, []string{"Off", "3 s", "5 s", "10 s"})
		p.toggle(c, "Record system audio", &s.Record.SystemAudio)
		noteText(c, pal, "The recording bar changes these too, along with the camera and the microphone.")
	})
}

func (p *Prefs) privacy(c *ui.Context, pal theme.Palette) {
	s := p.s()
	group(c, pal, "HIDE SENSITIVE INFO", func() {
		p.toggle(c, "Automatically hide sensitive info on capture", &s.AutoRedact)
		engine := "Windows OCR, so nothing leaves your PC"
		if isMac {
			engine = "macOS text recognition, so nothing leaves your Mac"
		}
		noteText(c, pal, "Uses on-device "+engine+". The original stays in the Library; you can undo a redaction in the editor.")
		choose(p, c, "Style", &s.RedactStyle, render.AllRedactStyles[:], []string{"Blur", "Pixelate", "Solid"})
		r := &s.RedactRules
		rules := []struct {
			label string
			on    *bool
		}{
			{"Email addresses", &r.Emails},
			{"Phone numbers", &r.Phones},
			{"Credit cards", &r.CreditCards},
			{"API keys, tokens, passwords", &r.Secrets},
			{"IP & MAC addresses", &r.IPAddresses},
			{"SSN & IBAN numbers", &r.IDs},
			{"URLs with tokens", &r.URLsWithTokens},
		}
		ui.Grid(c).Columns(2).GapX(24).GapY(8).Children(func() {
			for _, rule := range rules {
				p.toggle(c, rule.label, rule.on)
			}
		})
	})
}

func (p *Prefs) about(c *ui.Context, pal theme.Palette) {
	group(c, pal, "GLINT", func() {
		ui.Row(c).Gap(16).Children(func() {
			ui.Image(c, theme.Mark(false)).Size(56, 56)
			ui.Column(c).Gap(4).Children(func() {
				ui.Text(c, "Glint").FontSize(theme.Subtitle).FontWeight(theme.Semibold)
				ui.Text(c, "Version "+Version).TextColor(pal.Muted)
			})
		})
		noteText(c, pal, "Screenshots, recordings and their edits stay on this computer.")
		p.folderRow(c, pal, "Settings and Library", settings.DataDir(), nil)
	})
}
