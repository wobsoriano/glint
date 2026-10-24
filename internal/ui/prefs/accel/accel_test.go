package accel

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestFromKeyBuildsAcceleratorsAsRustDoes(t *testing.T) {
	for _, c := range []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
	}{
		{ui.Ctrl | ui.Shift, ui.Key4, "Ctrl+Shift+4"},
		{ui.Ctrl | ui.Alt, ui.KeyL, "Ctrl+Alt+L"},
		{0, ui.KeyF9, "F9"},
		{ui.Ctrl, ui.KeyA, "Ctrl+A"},
		{ui.Ctrl, ui.KeyUp, "Ctrl+Up"},
		{ui.Super | ui.Shift | ui.Alt | ui.Ctrl, ui.KeyK, "Ctrl+Alt+Shift+Super+K"},
	} {
		if got, err := FromKey(c.mods, c.key); err != nil || got != c.want {
			t.Errorf("FromKey(%v, %v) = %q, %v; want %q", c.mods, c.key, got, err, c.want)
		}
	}
	if _, err := FromKey(ui.Shift, ui.KeyK); err != ErrNeedsModifier {
		t.Errorf("Shift+K needs a modifier: %v", err)
	}
	if _, err := FromKey(ui.Ctrl, ui.KeyUnknown); err == nil {
		t.Error("an unknown key is refused")
	}
}

func TestMacChords(t *testing.T) {
	for in, want := range map[string]string{
		"Ctrl+Shift+4":           "⌃⇧4",
		"Inspector (Ctrl+Alt+I)": "Inspector (⌃⌥I)",
		"Super+Shift+Space":      "⇧⌘Space",
		"Ctrl+Alt+Shift+Super+K": "⌃⌥⇧⌘K",
	} {
		if got := MacChords(in); got != want {
			t.Errorf("MacChords(%q) = %q, want %q", in, got, want)
		}
	}
}
