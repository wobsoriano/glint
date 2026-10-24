package prefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
)

type fakeHost struct {
	s         settings.Settings
	saves     int
	login     []bool
	loginErr  error
	rebinds   []string
	refuse    string
	suspended int
	resumed   int
}

func (h *fakeHost) Settings() *settings.Settings { return &h.s }
func (h *fakeHost) SettingsChanged()             { h.saves++ }
func (h *fakeHost) SetLaunchAtLogin(on bool) error {
	h.login = append(h.login, on)
	return h.loginErr
}
func (h *fakeHost) RebindHotkey(a settings.HotkeyAction, accel string) error {
	h.rebinds = append(h.rebinds, fmt.Sprintf("%s=%s", a.Label(), accel))
	if accel == h.refuse {
		return fmt.Errorf("%s could not be registered, another app probably uses it (Hot key is already registered.)", accel)
	}
	return nil
}
func (h *fakeHost) HotkeyError(settings.HotkeyAction) error { return nil }
func (h *fakeHost) SuspendHotkeys()                         { h.suspended++ }
func (h *fakeHost) ResumeHotkeys()                          { h.resumed++ }

func open(t *testing.T, pane Pane) (*Prefs, *fakeHost, *ui.Tester) {
	t.Helper()
	h := &fakeHost{s: settings.Default()}
	p := New(h, pane, "0.2.0", nil, nil)
	return p, h, ui.NewTester(p.View, 860, 900)
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestEveryPaneRenders(t *testing.T) {
	_, _, tt := open(t, General)
	for _, c := range []struct{ pane, text string }{
		{"Screenshots", "Save without asking where"},
		{"Recordings", "Open in Studio"},
		{"Overlay", "Dismiss a card after dragging it out"},
		{"Shortcuts", "Capture previous area"},
		{"Privacy", "URLs with tokens"},
		{"About", "Version 0.2.0"},
		{"General", "Include Glint windows in screenshots"},
	} {
		must(t, tt.Click(c.pane))
		if !tt.HasText(c.text) {
			t.Errorf("%s pane shows %q: %q", c.pane, c.text, tt.Texts())
		}
	}
}

func TestTogglesSaveTheSettings(t *testing.T) {
	_, h, tt := open(t, Screenshots)
	must(t, tt.Click("Open the editor"))
	if !h.s.AfterShot.Edit || h.saves != 1 {
		t.Fatalf("after_shot.edit on and saved: %+v, %d saves", h.s.AfterShot, h.saves)
	}
	must(t, tt.Click("JPEG"))
	if h.s.Format != settings.FormatJPEG {
		t.Fatalf("format: %v", h.s.Format)
	}
	must(t, tt.Click("10 s"))
	if h.s.TimerSeconds != 10 {
		t.Fatalf("timer: %v", h.s.TimerSeconds)
	}
	must(t, tt.Click("Privacy"))
	must(t, tt.Click("Phone numbers"))
	must(t, tt.Click("Pixelate"))
	if h.s.RedactRules.Phones || h.s.RedactStyle != render.RedactPixelate {
		t.Fatalf("privacy: %+v %v", h.s.RedactRules, h.s.RedactStyle)
	}
}

func TestLaunchAtLoginGoesThroughTheHost(t *testing.T) {
	_, h, tt := open(t, General)
	must(t, tt.Click("Launch at login"))
	if !slices.Equal(h.login, []bool{true}) || !h.s.LaunchAtLogin {
		t.Fatalf("registered and saved: %v %v", h.login, h.s.LaunchAtLogin)
	}
	h.loginErr = errors.New("access denied")
	must(t, tt.Click("Launch at login"))
	if !h.s.LaunchAtLogin || !tt.HasText("access denied") {
		t.Fatalf("a refusal keeps the setting and says why: %v %q", h.s.LaunchAtLogin, tt.Texts())
	}
}

func TestRecordingAShortcut(t *testing.T) {
	_, h, tt := open(t, Shortcuts)
	must(t, tt.Click("Change Capture area"))
	if h.suspended != 1 || !tt.HasText("Press a shortcut… (Esc to cancel)") {
		t.Fatalf("recording suspends the shortcuts: %d %q", h.suspended, tt.Texts())
	}
	tt.Key(ui.Shift, ui.KeyK)
	if !tt.HasText("Include Ctrl, Alt or Win in the shortcut") || len(h.rebinds) != 0 {
		t.Fatalf("a plain key needs a modifier: %q %q", tt.Texts(), h.rebinds)
	}
	tt.Key(ui.Ctrl|ui.Alt, ui.KeyK)
	if !slices.Equal(h.rebinds, []string{"Capture area=Ctrl+Alt+K"}) {
		t.Fatalf("rebinds through the host: %q", h.rebinds)
	}
	if h.s.Hotkeys.Area != "Ctrl+Alt+K" || h.saves != 1 || h.resumed != 1 {
		t.Fatalf("binding saved, shortcuts resumed: %q %d %d", h.s.Hotkeys.Area, h.saves, h.resumed)
	}
	if !tt.HasText("Now Ctrl+Alt+K") {
		t.Errorf("says what it is now: %q", tt.Texts())
	}

	h.refuse = "Ctrl+Alt+J"
	must(t, tt.Click("Change Open Library"))
	tt.Key(ui.Ctrl|ui.Alt, ui.KeyJ)
	if h.s.Hotkeys.Library != "Ctrl+Shift+L" {
		t.Fatalf("a refused shortcut keeps the old one: %q", h.s.Hotkeys.Library)
	}
	if !tt.HasText("Ctrl+Alt+J could not be registered, another app probably uses it (Hot key is already registered.)") {
		t.Fatalf("shows the host's error: %q", tt.Texts())
	}

	must(t, tt.Click("Change Record screen"))
	tt.Key(0, ui.KeyEscape)
	if tt.HasText("Press a shortcut… (Esc to cancel)") || h.resumed != 3 || h.s.Hotkeys.Recording != "Ctrl+Shift+7" {
		t.Fatalf("Escape cancels: %q resumed %d", tt.Texts(), h.resumed)
	}

	must(t, tt.Click("Restore Defaults"))
	if h.s.Hotkeys.Area != "Ctrl+Shift+4" {
		t.Fatalf("defaults restored: %+v", h.s.Hotkeys)
	}
}

func TestDraggingCardActions(t *testing.T) {
	_, h, tt := open(t, Overlay)
	drag := func(from, to string) {
		t.Helper()
		a, ok := tt.Find(from)
		b, ok2 := tt.Find(to)
		if !ok || !ok2 {
			t.Fatalf("find %q %v, %q %v", from, ok, to, ok2)
		}
		tt.Press(a.X+a.W/2, a.Y+a.H/2)
		tt.Move(a.X+a.W/2+10, a.Y+a.H/2+10)
		tt.Move(b.X+b.W/2, b.Y+b.H/2)
		tt.Release(b.X+b.W/2, b.Y+b.H/2)
	}
	drag("Copy", "Edit")
	l := h.s.Overlay.Layout
	if a, _ := l.CornerAction(settings.TopRight); a != settings.CardCopy {
		t.Fatalf("Copy dropped on the top right corner: %+v", l)
	}
	if !slices.Contains(l.Center, settings.CardEdit) || h.saves == 0 {
		t.Fatalf("Edit went where Copy came from, and saved: %+v %d", l, h.saves)
	}
	drag("Quick Look", "Save")
	if c := h.s.Overlay.Layout.Center; !slices.Equal(c, []settings.CardAction{settings.CardEdit, settings.CardQuickLook, settings.CardSave}) {
		t.Fatalf("Quick Look dropped before Save in the center: %+v", h.s.Overlay.Layout)
	}
	must(t, tt.Click("Reset Layout"))
	got, _ := json.Marshal(h.s.Overlay.Layout)
	want, _ := json.Marshal(settings.DefaultCardLayout())
	if string(got) != string(want) {
		t.Fatalf("reset: %+v", h.s.Overlay.Layout)
	}
}
