package record

import (
	"fmt"
	"math"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/settings"
)

// clock is the bar's elapsed time: 01:05, then 1:02:05 past an hour.
func clock(ms uint64) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}

// meterFraction maps a microphone RMS onto the meter: -60 dB is empty, 0 dB full.
func meterFraction(rms float32) float32 {
	db := 20 * math.Log10(math.Max(float64(rms), 1e-6))
	return float32(min(max((db+60)/60, 0), 1))
}

// device is how the picker shows a chosen camera or microphone.
type device int

const (
	deviceOff device = iota
	deviceLoading
	devicePresent
	deviceMissing
)

type namedDevice struct{ id, name string }

// deviceChoice says whether the chosen device is connected, and its name when it is.
func deviceChoice(selected *string, loaded bool, devices []namedDevice) (device, string) {
	switch {
	case selected == nil:
		return deviceOff, ""
	case !loaded:
		return deviceLoading, ""
	}
	i := slices.IndexFunc(devices, func(d namedDevice) bool { return d.id == *selected })
	if i < 0 {
		return deviceMissing, ""
	}
	return devicePresent, devices[i].name
}

// recordedLabel is a global shortcut as a recording's input track labels the key press, so
// recordings leave Glint's own shortcuts out of their keystroke captions.
func recordedLabel(accel string) string {
	if runtime.GOOS == "darwin" {
		return macChord(accel)
	}
	parts := strings.Split(accel, "+")
	key, mods := parts[len(parts)-1], parts[:len(parts)-1]
	if strings.EqualFold(key, "Printscreen") {
		key = "PrintScreen"
	}
	var out []string
	for _, m := range []struct {
		label string
		names []string
	}{{"Win", []string{"Super", "Win"}}, {"Ctrl", []string{"Ctrl"}}, {"Alt", []string{"Alt"}}, {"Shift", []string{"Shift"}}} {
		if slices.ContainsFunc(mods, func(s string) bool { return slices.Contains(m.names, s) }) {
			out = append(out, m.label)
		}
	}
	return strings.Join(append(out, key), "+")
}

// macChord writes an accelerator as macOS menus do, ⌃⌥⇧⌘ then the key, as the Rust
// build's mac_chords with Ctrl as ⌃.
func macChord(accel string) string {
	parts := strings.Split(accel, "+")
	key, mods := parts[len(parts)-1], parts[:len(parts)-1]
	if len(mods) == 0 {
		return accel
	}
	var b strings.Builder
	for _, m := range []struct {
		sym   string
		names []string
	}{{"⌃", []string{"Ctrl"}}, {"⌥", []string{"Alt"}}, {"⇧", []string{"Shift"}}, {"⌘", []string{"Super", "Win", "Cmd"}}} {
		if slices.ContainsFunc(mods, func(s string) bool { return slices.Contains(m.names, s) }) {
			b.WriteString(m.sym)
		}
	}
	if len(key) > 1 && strings.ToLower(key) == key {
		b.WriteByte(' ')
	}
	b.WriteString(key)
	return b.String()
}

// ignoredKeys are Glint's own global shortcuts, which a recording leaves out of its captions.
func ignoredKeys(h settings.Hotkeys) []string {
	var out []string
	for _, a := range settings.HotkeyActions {
		if accel := *h.Binding(a); accel != "" {
			out = append(out, recordedLabel(accel))
		}
	}
	return out
}

type track struct{ name, file, noun string }

// tracks are a recording's optional tracks in the order a notice names them: the name
// media.Recorder's warnings give each, the file it writes, and how a notice says it. The
// recording draws the pointer from the input track, so without it there is no cursor.
var tracks = []track{
	{"microphone", "mic.wav", "the microphone"},
	{"system audio", "system.wav", "system audio"},
	{"camera", "camera.mp4", "the camera"},
	{"input", input.File, "the cursor"},
}

// toastChars is about the most a toast shows before it cuts the text.
const toastChars = 76

// recordingNotice turns a recorder's warnings into one line for a toast, naming each track
// once however many warnings name it: the tracks left out, then those that stopped early,
// then anything else, without its detail. It is empty when nothing went wrong.
func recordingNotice(running bool, warnings []string) string {
	without, early := make([]bool, len(tracks)), make([]bool, len(tracks))
	var rest []string
	for _, w := range warnings {
		head, _, _ := strings.Cut(w, ": ")
		i := slices.IndexFunc(tracks, func(t track) bool {
			return head == t.name+" not recorded" || filepath.Base(head) == t.file
		})
		switch {
		case i < 0:
			if line := capitalize(head); !slices.Contains(rest, line) {
				rest = append(rest, line)
			}
		case strings.HasSuffix(head, " not recorded"):
			without[i] = true
		default:
			early[i] = true
		}
	}
	var lines []string
	if nouns := named(without); nouns != "" {
		verb := "Recorded"
		if running {
			verb = "Recording"
		}
		lines = append(lines, verb+" without "+nouns)
	}
	if nouns := named(early); nouns != "" {
		lines = append(lines, capitalize(nouns)+" stopped early")
	}
	return strings.Join(append(lines, rest...), ". ")
}

// named lists the tracks marked in which, as "a, b and c".
func named(which []bool) string {
	var s []string
	for i, on := range which {
		if on {
			s = append(s, tracks[i].noun)
		}
	}
	return list(s)
}

// list joins a, b and c as "a, b and c".
func list(s []string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// span is the words [start, end) of one line.
type span struct{ start, end int }

// wrap breaks words of the given widths greedily into lines no wider than limit; a word wider
// than a line gets a line of its own.
func wrap(widths []float32, space, limit float32) []span {
	var lines []span
	start, used := 0, float32(0)
	for i, w := range widths {
		switch {
		case i > start && used+space+w > limit:
			lines = append(lines, span{start, i})
			start, used = i, w
		case i > start:
			used += space + w
		default:
			used = w
		}
	}
	if start < len(widths) {
		lines = append(lines, span{start, len(widths)})
	}
	return lines
}
