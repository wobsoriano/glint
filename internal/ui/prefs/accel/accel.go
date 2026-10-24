// Package accel turns key presses into global-shortcut accelerators such as "Ctrl+Shift+4",
// the strings settings.json keeps, and writes them as the platform shows shortcuts.
package accel

import (
	"errors"
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"github.com/egoist/mygo/ui"
)

var isMac = runtime.GOOS == "darwin"

var names = func() map[ui.Key]string {
	m := map[ui.Key]string{
		ui.KeySpace: "Space", ui.KeyEnter: "Enter", ui.KeyTab: "Tab", ui.KeyBackspace: "Backspace",
		ui.KeyDelete: "Delete", ui.KeyInsert: "Insert", ui.KeyHome: "Home", ui.KeyEnd: "End",
		ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
		ui.KeyUp: "Up", ui.KeyDown: "Down", ui.KeyLeft: "Left", ui.KeyRight: "Right",
		ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/",
		ui.KeySemicolon: ";", ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]",
		ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
	}
	for i := range 26 {
		m[ui.KeyA+ui.Key(i)] = string(rune('A' + i))
	}
	for i := range 10 {
		m[ui.Key0+ui.Key(i)] = string(rune('0' + i))
	}
	for i := range 12 {
		m[ui.KeyF1+ui.Key(i)] = fmt.Sprintf("F%d", i+1)
	}
	return m
}()

// ErrNeedsModifier is the press of a key that needs a modifier to be global.
var ErrNeedsModifier = func() error {
	if isMac {
		return errors.New("Include ⌃, ⌥ or ⌘ in the shortcut")
	}
	return errors.New("Include Ctrl, Alt or Win in the shortcut")
}()

// FromKey is the accelerator a key press makes, in the order Ctrl, Alt, Shift, Super, as the
// Rust build writes them. A key that is not global on its own needs Ctrl, Alt or Super; the
// function keys do not. The error is the message the Settings window shows.
func FromKey(mods ui.Modifiers, key ui.Key) (string, error) {
	name, ok := names[key]
	if !ok {
		return "", errors.New("This key cannot be used in a global shortcut")
	}
	function := key >= ui.KeyF1 && key <= ui.KeyF12
	if mods&(ui.Ctrl|ui.Alt|ui.Super) == 0 && !function {
		return "", ErrNeedsModifier
	}
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}, {ui.Super, "Super"}} {
		if mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	return strings.Join(append(parts, name), "+"), nil
}

// Display writes a shortcut as the platform does: "Ctrl+Shift+4" with Win for Super on
// Windows, "⌃⇧4" on macOS. It rewrites every chord in a longer text, as "Inspector
// (Ctrl+Alt+I)".
func Display(text string) string {
	if !isMac {
		return strings.ReplaceAll(text, "Super", "Win")
	}
	return MacChords(text)
}

var chord = regexp.MustCompile(`\b((?:(?:Ctrl|Alt|Shift|Super|Win|Cmd)\+)+)([^\s)]+)`)

// MacChords writes each chord in text with the Mac's modifier symbols, Ctrl as ⌃, as the
// Rust build's mac_chords does for global shortcuts.
func MacChords(text string) string {
	return chord.ReplaceAllStringFunc(text, func(s string) string {
		m := chord.FindStringSubmatch(s)
		mods := strings.Split(strings.TrimSuffix(m[1], "+"), "+")
		has := func(names ...string) bool {
			for _, x := range mods {
				for _, n := range names {
					if x == n {
						return true
					}
				}
			}
			return false
		}
		var out strings.Builder
		for _, sym := range []struct {
			on bool
			s  string
		}{{has("Ctrl"), "⌃"}, {has("Alt"), "⌥"}, {has("Shift"), "⇧"}, {has("Super", "Win", "Cmd"), "⌘"}} {
			if sym.on {
				out.WriteString(sym.s)
			}
		}
		key := m[2]
		if len(key) > 1 && strings.ToLower(key) == key {
			out.WriteByte(' ')
		}
		out.WriteString(key)
		return out.String()
	})
}
