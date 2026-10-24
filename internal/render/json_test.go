package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// sameJSON reports where a and b differ, comparing numbers as f32 like serde does.
func sameJSON(a, b []byte) error {
	var x, y any
	for _, p := range []struct {
		src []byte
		dst *any
	}{{a, &x}, {b, &y}} {
		d := json.NewDecoder(bytes.NewReader(p.src))
		d.UseNumber()
		if err := d.Decode(p.dst); err != nil {
			return err
		}
	}
	return diffJSON("$", x, y)
}

func diffJSON(path string, x, y any) error {
	switch xv := x.(type) {
	case map[string]any:
		yv, ok := y.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: object vs %T", path, y)
		}
		keys := map[string]bool{}
		for k := range xv {
			keys[k] = true
		}
		for k := range yv {
			keys[k] = true
		}
		sorted := make([]string, 0, len(keys))
		for k := range keys {
			sorted = append(sorted, k)
		}
		sort.Strings(sorted)
		for _, k := range sorted {
			a, okA := xv[k]
			b, okB := yv[k]
			if okA != okB {
				return fmt.Errorf("%s.%s present on one side only", path, k)
			}
			if err := diffJSON(path+"."+k, a, b); err != nil {
				return err
			}
		}
		return nil
	case []any:
		yv, ok := y.([]any)
		if !ok || len(xv) != len(yv) {
			return fmt.Errorf("%s: array length differs", path)
		}
		for i := range xv {
			if err := diffJSON(fmt.Sprintf("%s[%d]", path, i), xv[i], yv[i]); err != nil {
				return err
			}
		}
		return nil
	case json.Number:
		yv, ok := y.(json.Number)
		if !ok {
			return fmt.Errorf("%s: number vs %T", path, y)
		}
		fx, err1 := strconv.ParseFloat(xv.String(), 32)
		fy, err2 := strconv.ParseFloat(yv.String(), 32)
		if err1 != nil || err2 != nil || float32(fx) != float32(fy) {
			return fmt.Errorf("%s: %s vs %s", path, xv, yv)
		}
		return nil
	}
	if !reflect.DeepEqual(x, y) {
		return fmt.Errorf("%s: %v vs %v", path, x, y)
	}
	return nil
}

// rustDocs are the EditDoc JSON files the Rust serde code wrote, keyed by the name the Go
// output is stored under in testdata/golden/json/go.
func rustDocs(t *testing.T) map[string]string {
	out := map[string]string{}
	for _, fx := range goldenFixtures(t) {
		out[fx.Name+".json"] = filepath.Join(goldenDir, fx.Name+".json")
	}
	out["doc_everything.json"] = filepath.Join(goldenDir, "json", "doc_everything.json")
	return out
}

func writeOrCheckGoJSON(t *testing.T, name string, data []byte) {
	t.Helper()
	path := filepath.Join(goldenDir, "json", "go", name)
	if os.Getenv("GLINT_WRITE_GO_JSON") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v (run with GLINT_WRITE_GO_JSON=1, then the Rust verify step)", name, err)
	}
	if !bytes.Equal(bytes.ReplaceAll(committed, []byte("\r\n"), []byte("\n")), data) {
		t.Errorf("%s: Go output changed since Rust last verified it; rerun GLINT_WRITE_GO_JSON=1 and the Rust verify step", name)
	}
}

func TestEditDocJSONRoundTripsRustOutput(t *testing.T) {
	for name, path := range rustDocs(t) {
		rust, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		doc, err := ParseEditDoc(rust)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		goJSON := doc.JSON()
		if err := sameJSON(goJSON, rust); err != nil {
			t.Errorf("%s: Go re-encoding differs from Rust: %v", name, err)
		}
		again, err := ParseEditDoc(goJSON)
		if err != nil || !reflect.DeepEqual(again, doc) {
			t.Errorf("%s: Go output does not decode to the same document (%v)", name, err)
		}
		writeOrCheckGoJSON(t, name, goJSON)
	}
}

func TestMissingFieldsTakeRustDefaults(t *testing.T) {
	ins, _ := filepath.Glob(filepath.Join(goldenDir, "json", "partial_*.in.json"))
	if len(ins) == 0 {
		t.Fatal("no partial fixtures")
	}
	for _, in := range ins {
		input, _ := os.ReadFile(in)
		want, err := os.ReadFile(strings.TrimSuffix(in, ".in.json") + ".out.json")
		if err != nil {
			t.Fatal(err)
		}
		doc, err := ParseEditDoc(input)
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if err := sameJSON(doc.JSON(), want); err != nil {
			t.Errorf("%s: defaults differ from Rust: %v", filepath.Base(in), err)
		}
		writeOrCheckGoJSON(t, strings.TrimSuffix(filepath.Base(in), ".in.json")+".out.json", doc.JSON())
	}
}

func TestPresetFilesRoundTripRustOutput(t *testing.T) {
	storePath := filepath.Join(goldenDir, "json", "presets_store.json")
	rustStore, _ := os.ReadFile(storePath)
	presets, err := LoadPresetStore(storePath)
	if err != nil {
		t.Fatal(err)
	}
	goStore, _ := json.MarshalIndent(presets, "", "  ")
	if err := sameJSON(goStore, rustStore); err != nil {
		t.Errorf("preset store: %v", err)
	}
	writeOrCheckGoJSON(t, "presets_store.json", goStore)

	rustExport, _ := os.ReadFile(filepath.Join(goldenDir, "json", "presets_export.glintpreset"))
	goExport := ExportPresets(presets)
	if err := sameJSON(goExport, rustExport); err != nil {
		t.Errorf("preset export: %v", err)
	}
	if bytes.Contains(goExport, []byte("beach.jpg")) || !bytes.Contains(goExport, []byte(`"background_image"`)) {
		t.Errorf("export must drop local image paths and record the omission:\n%s", goExport)
	}
	writeOrCheckGoJSON(t, "presets_export.glintpreset", goExport)

	imported, err := ImportPresets(rustExport)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := imported[0].Style.Background.(NoBackground); !ok {
		t.Errorf("an omitted image imports as no background, got %#v", imported[0].Style.Background)
	}
	if !reflect.DeepEqual(imported[1], presets[1]) {
		t.Errorf("a portable preset imports unchanged:\n%#v\n%#v", imported[1], presets[1])
	}
}

func TestPresetImportRejectsForeignAndFutureFiles(t *testing.T) {
	for _, bad := range []string{"", "{}", `{"format":"other","version":1,"presets":[]}`, `{"format":"glint-presets","version":1,"presets":[]}`} {
		if _, err := ImportPresets([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
	future := bytes.Replace(ExportPresets([]Preset{PresetFromDoc("A", DefaultEditDoc())}), []byte(`"version": 1`), []byte(`"version": 7`), 1)
	if _, err := ImportPresets(future); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("future file: %v", err)
	}
	sneaky := `{"format":"glint-presets","version":1,"presets":[{"name":"  Sneaky  ","style":{"background":{"Image":"C:\\secret.png"}}}]}`
	got, err := ImportPresets([]byte(sneaky))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[0].Style.Background.(NoBackground); !ok || got[0].Name != "Sneaky" {
		t.Errorf("hand-edited image path survived import: %#v", got[0])
	}
}

func TestPresetStoreSavesAndLoads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sub", "presets.json")
	if got, err := LoadPresetStore(path); err != nil || len(got) != 0 {
		t.Fatalf("missing store: %v %v", got, err)
	}
	p := PresetFromDoc("Mine", DefaultEditDoc())
	p.Style.Padding = 12
	if err := SavePresetStore(path, []Preset{p}); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPresetStore(path)
	if err != nil || !reflect.DeepEqual(got, []Preset{p}) {
		t.Fatalf("got %#v, %v", got, err)
	}
}

func TestNewerDocVersionIsRejected(t *testing.T) {
	if _, err := ParseEditDoc([]byte(`{"version":99}`)); err == nil {
		t.Error("accepted a newer edit.json")
	}
	d, err := ParseEditDoc([]byte(`{"version":1,"dpi":2.0}`))
	if err != nil || d.DPI != 2 || !reflect.DeepEqual(d.Effects, DefaultEffects()) {
		t.Errorf("%#v %v", d, err)
	}
}
