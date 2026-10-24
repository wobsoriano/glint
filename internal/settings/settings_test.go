package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings/serde"
)

// testdata/rust holds files written by the Rust build's ignored go_fixtures tests
// (src/settings.rs, crates/glint-render/src/preset.rs), with the user's Pictures path in
// save_dir replaced by C:\Users\me. testdata/real is the Rust build's settings.json from a
// real install, sanitized the same way.

func read(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func encode(t *testing.T, v any) []byte {
	t.Helper()
	b, err := serde.MarshalPretty(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func firstDifference(got, want []byte) string {
	g, w := strings.Split(string(got), "\n"), strings.Split(string(want), "\n")
	for i := range max(len(g), len(w)) {
		var gl, wl string
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl != wl {
			return fmt.Sprintf("line %d: got %q, want %q", i+1, gl, wl)
		}
	}
	return "no line differs"
}

// withThisMachinesSaveDir puts this machine's default export folder into a Rust fixture
// written with the default, so it can be compared with Go's defaults byte for byte.
func withThisMachinesSaveDir(t *testing.T, rust []byte) []byte {
	t.Helper()
	var s struct {
		SaveDir string `json:"save_dir"`
	}
	if err := json.Unmarshal(rust, &s); err != nil {
		t.Fatal(err)
	}
	return bytes.Replace(rust, encode(t, s.SaveDir), encode(t, DefaultSaveDir()), 1)
}

func TestRustFilesRoundTripByteForByte(t *testing.T) {
	for _, name := range []string{
		"rust/settings-default.json", "rust/settings-custom.json", "rust/settings-partial-decoded.json", "real/settings.json",
	} {
		t.Run(name, func(t *testing.T) {
			want := read(t, filepath.Join("testdata", name))
			s, err := Parse(want)
			if err != nil {
				t.Fatal(err)
			}
			if got := encode(t, s); !bytes.Equal(got, want) {
				t.Errorf("re-encoded differs from the Rust file: %s", firstDifference(got, want))
			}
		})
	}
}

func TestEmptyFileGetsRustDefaults(t *testing.T) {
	s, err := Parse([]byte("{}"))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(s, Default()) {
		t.Errorf("{} decoded to %+v, not the defaults", s)
	}
	want := withThisMachinesSaveDir(t, read(t, "testdata/rust/settings-default.json"))
	if got := encode(t, s); !bytes.Equal(got, want) {
		t.Errorf("defaults differ from Rust's Settings::default(): %s", firstDifference(got, want))
	}
}

func TestMissingFieldsTakeDefaultsAndUnknownFieldsAreIgnored(t *testing.T) {
	s, err := Parse(read(t, "testdata/rust/settings-partial.json"))
	if err != nil {
		t.Fatal(err)
	}
	if s.TimerSeconds != 9 || s.Record.Countdown != 1 || s.Record.PrompterLines != 3 || s.Hotkeys.Text != "F1" || s.Hotkeys.Area != "Ctrl+Shift+4" {
		t.Errorf("partial file decoded to %+v", s)
	}
	want := withThisMachinesSaveDir(t, read(t, "testdata/rust/settings-partial-decoded.json"))
	if got := encode(t, s); !bytes.Equal(got, want) {
		t.Errorf("differs from what Rust decoded: %s", firstDifference(got, want))
	}
}

func TestFilesRustRejectsAreRejected(t *testing.T) {
	paths, _ := filepath.Glob("testdata/rust/settings-reject-*.json")
	if len(paths) == 0 {
		t.Fatal("no reject fixtures")
	}
	for _, p := range paths {
		if _, err := Parse(read(t, p)); err == nil {
			t.Errorf("%s parsed, but Rust rejects it", p)
		}
	}
	for _, bad := range []string{`{"style": null}`, `{"style": 3}`, `[]`, `{"overlay": {"side": "Up"}}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s parsed", bad)
		}
	}
}

func TestByteOrderMarkIsAccepted(t *testing.T) {
	s, err := Parse([]byte("\xef\xbb\xbf{\"timer_seconds\": 2}"))
	if err != nil || s.TimerSeconds != 2 {
		t.Fatalf("got %v, %v", s.TimerSeconds, err)
	}
}

func TestCorruptFileIsKeptAsideAndDefaultsLoad(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	os.WriteFile(p, []byte("{ not json"), 0o644)
	if s := LoadFrom(p); !reflect.DeepEqual(s, Default()) {
		t.Errorf("loaded %+v", s)
	}
	if got := string(read(t, filepath.Join(dir, "settings.json.broken"))); got != "{ not json" {
		t.Errorf("broken copy holds %q", got)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Errorf("settings.json still exists: %v", err)
	}
	if s := LoadFrom(filepath.Join(dir, "missing.json")); !reflect.DeepEqual(s, Default()) {
		t.Errorf("missing file loaded %+v", s)
	}
}

func TestSaveRoundTripsThroughAReplacedFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "settings.json")
	os.WriteFile(p, []byte("old"), 0o644)
	s := Default()
	s.TimerSeconds = 9
	*s.Hotkeys.Binding(HotkeyArea) = "Ctrl+Alt+A"
	s.LastArea = &Area{1, 2, 3, 4}
	if err := s.SaveTo(p); err != nil {
		t.Fatal(err)
	}
	back := LoadFrom(p)
	if !reflect.DeepEqual(back, s) {
		t.Errorf("loaded %+v\nsaved %+v", back, s)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Errorf("directory holds %v", entries)
	}
}

func TestDataDirHonorsOverride(t *testing.T) {
	t.Setenv("GLINT_DATA_DIR", filepath.Join("x", "y"))
	if DataDir() != filepath.Join("x", "y") || Path() != filepath.Join("x", "y", "settings.json") || PresetsPath() != filepath.Join("x", "y", "presets.json") {
		t.Errorf("DataDir %s, Path %s", DataDir(), Path())
	}
}

func TestEscapingMatchesSerde(t *testing.T) {
	got := string(encode(t, map[string]string{"k": "<&> \u2028 \\u2028 \x01\n"}))
	want := "{\n  \"k\": \"<&> \u2028 \\\\u2028 \\u0001\\n\"\n}"
	if got != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestPresetsRoundTripByteForByte(t *testing.T) {
	path := "testdata/rust/presets.json"
	presets, err := LoadPresets(path)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, p := range presets {
		names = append(names, p.Name)
	}
	if !slices.Equal(names, []string{"Plain", "Dusk \u25d0", "Photo"}) {
		t.Errorf("names %q", names)
	}
	out := filepath.Join(t.TempDir(), "presets.json")
	if err := SavePresets(out, presets); err != nil {
		t.Fatal(err)
	}
	if got, want := read(t, out), read(t, path); !bytes.Equal(got, want) {
		t.Errorf("re-saved presets differ: %s", firstDifference(got, want))
	}
}

func TestPresetsMissingEffectsAndFiles(t *testing.T) {
	bare, err := LoadPresets("testdata/rust/presets-no-effects.json")
	if err != nil || len(bare) != 1 || !reflect.DeepEqual(bare[0].Effects, render.DefaultEffects()) {
		t.Fatalf("got %+v, %v", bare, err)
	}
	// Rust saves the default style and effects it filled in, which is what it wrote for the
	// first preset in presets.json, made from a default document.
	plain, err := LoadPresets("testdata/rust/presets.json")
	if err != nil {
		t.Fatal(err)
	}
	plain[0].Name = bare[0].Name
	if got, want := encode(t, bare), encode(t, plain[:1]); !bytes.Equal(got, want) {
		t.Errorf("re-saved preset without effects differs: %s", firstDifference(got, want))
	}
	if _, err := LoadPresets("testdata/rust/presets-reject-no-style.json"); err == nil {
		t.Error("a preset without a style loaded")
	}
	if p, err := LoadPresets(filepath.Join(t.TempDir(), "none.json")); err != nil || len(p) != 0 {
		t.Errorf("missing file gave %v, %v", p, err)
	}
}

// TestWriteFilesForRust leaves Go-written files where the Rust build's reads_go_written_*
// tests check them: GLINT_GO_WRITTEN=<dir> go test ./internal/settings -run WriteFilesForRust
func TestWriteFilesForRust(t *testing.T) {
	dir := os.Getenv("GLINT_GO_WRITTEN")
	if dir == "" {
		t.Skip("GLINT_GO_WRITTEN is not set")
	}
	if err := Default().SaveTo(filepath.Join(dir, "settings-default.json")); err != nil {
		t.Fatal(err)
	}
	s := Default()
	s.Format, s.JPEGQuality, s.SaveDir = FormatJPEG, 60, "E:\\Exports \u2028 <&>"
	s.Record.Target, s.Record.Area, s.Record.Mic = RecordArea, &Area{-5, 0, 640, 480}, ptr("USB \"Mic\"")
	s.Overlay.Layout.Place(CardDelete, CornerZone(TopLeft), -1)
	s.Overlay.Layout.Place(CardCopyText, CenterZone, 0)
	s.Library.Sort, s.RedactStyle = SortName, render.RedactSolid
	if err := s.SaveTo(filepath.Join(dir, "settings-custom.json")); err != nil {
		t.Fatal(err)
	}
	presets, err := LoadPresets("testdata/rust/presets.json")
	if err != nil {
		t.Fatal(err)
	}
	presets[0].Name = "Renamed in Go"
	if err := SavePresets(filepath.Join(dir, "presets.json"), presets); err != nil {
		t.Fatal(err)
	}
}
