package studio

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/wobsoriano/glint/internal/transcribe"
)

// The fixtures under testdata/rust come from `cargo run -p glint-studio --example go_fixtures`.

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}

func generic(t *testing.T, data []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%v in %s", err, data)
	}
	return v
}

func sameJSON(t *testing.T, got, want []byte) {
	t.Helper()
	if !reflect.DeepEqual(generic(t, got), generic(t, want)) {
		t.Fatalf("json differs\ngot  %s\nwant %s", got, want)
	}
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "rust", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// knownDeviations are inputs the Rust build accepts that Go rejects, each for a stated reason.
var knownDeviations = map[string]string{
	// serde lets a struct with #[serde(default)] be read from a JSON array, field by position.
	"[]": "positional struct arrays are not supported",
}

func TestSerdeCasesMatchRust(t *testing.T) {
	var cases []struct {
		Kind     string   `json:"kind"`
		Input    string   `json:"input"`
		Duration *float64 `json:"duration"`
		Ok       bool     `json:"ok"`
		Output   string   `json:"output"`
		Error    string   `json:"error"`
	}
	if err := json.Unmarshal(readFixture(t, "serde_cases.json"), &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		t.Run(c.Kind+" "+c.Input, func(t *testing.T) {
			var out any
			var err error
			switch c.Kind {
			case "project":
				var p Project
				err = json.Unmarshal([]byte(c.Input), &p)
				out = p
			case "project_parse":
				out, err = ParseProject([]byte(c.Input), *c.Duration)
			case "preset":
				var p Preset
				err = json.Unmarshal([]byte(c.Input), &p)
				out = p
			case "transcript":
				out, err = transcribe.Parse([]byte(c.Input))
			default:
				t.Fatalf("unknown kind %s", c.Kind)
			}
			if reason, ok := knownDeviations[c.Input]; ok {
				if err == nil {
					t.Fatalf("expected the known deviation (%s) to still reject", reason)
				}
				return
			}
			if (err == nil) != c.Ok {
				t.Fatalf("go err %v, rust ok %v (%s)", err, c.Ok, c.Error)
			}
			if !c.Ok {
				return
			}
			got, err := json.Marshal(out)
			if err != nil {
				t.Fatal(err)
			}
			sameJSON(t, got, []byte(c.Output))
		})
	}
}

func TestDefaultProjectMatchesRust(t *testing.T) {
	got, err := json.Marshal(DefaultProject())
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, got, readFixture(t, "project_default.json"))
}

func TestRustProjectRoundTrips(t *testing.T) {
	for _, name := range []string{"project_default.json", "project_full.json"} {
		t.Run(name, func(t *testing.T) {
			want := readFixture(t, name)
			var p Project
			if err := json.Unmarshal(want, &p); err != nil {
				t.Fatal(err)
			}
			got, err := json.MarshalIndent(p, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			sameJSON(t, got, want)
			var again Project
			if err := json.Unmarshal(got, &again); err != nil {
				t.Fatal(err)
			}
			eq(t, again, p)
		})
	}
}

func TestSaveWritesAProjectRustReads(t *testing.T) {
	var p Project
	if err := json.Unmarshal(readFixture(t, "project_full.json"), &p); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := p.Save(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "project.json.tmp")); !os.IsNotExist(err) {
		t.Fatalf("temp file left behind: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ProjectFile))
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, data, readFixture(t, "project_full.json"))
}

func TestMissingFieldsTakeDefaults(t *testing.T) {
	data := `{ "version": 1, "clips": [{ "start_ms": 0, "end_ms": 5000 }],
	           "zooms": [{ "start_ms": 100, "end_ms": 900 }] }`
	var p Project
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatal(err)
	}
	eq(t, p.Clips[0].Speed, 1.0)
	eq(t, p.Zooms[0].Scale, float32(1.5))
	eq(t, p.Zooms[0].Focus, ZoomFocus{})
	eq(t, p.Zooms[0].Enabled, true)
	eq(t, p.Export.Fps, Fps60)
	eq(t, p.Keystrokes.Position, KeysBottomCenter)
}

func TestParseRepairsOverlappingAndOutOfRangeClips(t *testing.T) {
	data := `{ "clips": [{ "start_ms": 3000, "end_ms": 9000, "speed": 20 },
	                     { "start_ms": 0, "end_ms": 4000 }] }`
	p, err := ParseProject([]byte(data), 8000)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, p.Clips, []Clip{{0, 4000, 1}, {4000, 8000, 8}})
}

func TestOutputSizePerAspect(t *testing.T) {
	p := NewProject(1000)
	p.Export.Resolution = Res1080
	size := func() [2]uint32 {
		w, h := p.OutputSize(2560, 1440)
		return [2]uint32{w, h}
	}
	p.Aspect = AspectWide16x9
	eq(t, size(), [2]uint32{1920, 1080})
	p.Aspect = AspectVertical9x16
	eq(t, size(), [2]uint32{1080, 1920})
	p.Aspect = AspectSquare
	eq(t, size(), [2]uint32{1080, 1080})
	p.Aspect = AspectOriginal
	p.Design.Padding = 0
	p.Export.Resolution = ResSource
	eq(t, size(), [2]uint32{2560, 1440})
}

func TestPresetsRoundTripAndDefaultAppliesToNewProjects(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "presets")
	store := NewPresetStore(dir)
	eq(t, len(store.List()), 0)

	design := DefaultDesign()
	design.Background = json.RawMessage(`{"Solid":287454207}`)
	design.Padding = 0.1
	design.Cursor.Size = 2.5
	design.Camera = CameraSettings{Visible: false, Center: [2]float32{0.1, 0.2}, Size: 0.3, Roundness: 0.1}
	dark := NewPreset("  Dark Demo! ", design)
	if err := store.Save(dark); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(NewPreset("Bright", DefaultDesign())); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dark-demo.json")); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range store.List() {
		names = append(names, p.Name)
	}
	eq(t, names, []string{"Bright", "Dark Demo!"})
	loaded, err := store.Load("Dark Demo!")
	if err != nil {
		t.Fatal(err)
	}
	sameJSON(t, loaded.Design.Background, design.Background)
	loaded.Design.Background = design.Background
	eq(t, loaded.Design, design)

	if _, ok := store.DefaultPreset(); ok {
		t.Fatal("no default yet")
	}
	if err := store.SetDefault("Dark Demo!"); err != nil {
		t.Fatal(err)
	}
	project := NewProject(1000)
	def, ok := store.DefaultPreset()
	if !ok {
		t.Fatal("default preset missing")
	}
	def.Apply(&project)
	eq(t, project.Design.Padding, float32(0.1))
	eq(t, project.Design.Camera.Center, [2]float32{0.1, 0.2})

	if err := store.Delete("Dark Demo!"); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.DefaultPreset(); ok {
		t.Fatal("deleting the default preset clears it")
	}
	eq(t, len(store.List()), 1)
}
