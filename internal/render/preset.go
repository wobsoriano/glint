package render

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

const (
	PresetFormat    = "glint-presets"
	PresetVersion   = uint32(1)
	PresetExtension = "glintpreset"

	maxPresetFileBytes = 1_000_000
	maxPresets         = 100
	omittedImage       = "background_image"
)

// Preset is a saved look: a style and its effects.
type Preset struct {
	Name    string  `json:"name"`
	Style   Style   `json:"style"`
	Effects Effects `json:"effects"`
}

func (p *Preset) UnmarshalJSON(data []byte) error {
	type plain Preset
	v := plain{Style: DefaultStyle(), Effects: DefaultEffects()}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*p = Preset(v)
	return nil
}

func PresetFromDoc(name string, doc EditDoc) Preset {
	d := doc.Clone()
	return Preset{name, d.Style, d.Effects}
}

// Apply sets the look of doc; crop and annotations are untouched.
func (p Preset) Apply(doc *EditDoc) {
	doc.Style = p.Style.Clone()
	doc.Effects = p.Effects
}

type presetFile struct {
	Format  string           `json:"format"`
	Version uint32           `json:"version"`
	Presets []portablePreset `json:"presets"`
}

type portablePreset struct {
	Name    string   `json:"name"`
	Style   Style    `json:"style"`
	Effects Effects  `json:"effects"`
	Omitted []string `json:"omitted"`
}

func (p *portablePreset) UnmarshalJSON(data []byte) error {
	type plain portablePreset
	v := plain{Style: DefaultStyle(), Effects: DefaultEffects(), Omitted: []string{}}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*p = portablePreset(v)
	return nil
}

func portableStyle(s Style) (Style, []string) {
	s = s.Clone()
	omitted := []string{}
	if _, ok := s.Background.(ImageBackground); ok {
		s.Background = NoBackground{}
		omitted = append(omitted, omittedImage)
	}
	return s, omitted
}

// ExportPresets serializes presets as a .glintpreset document. Local image paths are never
// written.
func ExportPresets(presets []Preset) []byte {
	f := presetFile{Format: PresetFormat, Version: PresetVersion, Presets: []portablePreset{}}
	for _, p := range presets {
		s, omitted := portableStyle(p.Style)
		f.Presets = append(f.Presets, portablePreset{p.Name, s, p.Effects, omitted})
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("presets serialize: %v", err))
	}
	return b
}

// ImportPresets reads a .glintpreset document.
func ImportPresets(data []byte) ([]Preset, error) {
	if strings.TrimSpace(string(data)) == "" {
		return nil, errors.New("The preset file is empty")
	}
	if len(data) > maxPresetFileBytes {
		return nil, errors.New("The preset file is too large")
	}
	var f presetFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("Not a valid preset file: %w", err)
	}
	if f.Format != PresetFormat {
		return nil, errors.New("Not a Glint preset file")
	}
	if f.Version > PresetVersion {
		return nil, fmt.Errorf("This preset file needs a newer version of Glint (format %d)", f.Version)
	}
	if len(f.Presets) == 0 {
		return nil, errors.New("The preset file has no presets")
	}
	if len(f.Presets) > maxPresets {
		return nil, errors.New("The preset file has too many presets")
	}
	out := make([]Preset, len(f.Presets))
	for i, p := range f.Presets {
		name := []rune(strings.TrimSpace(p.Name))
		if len(name) > 80 {
			name = name[:80]
		}
		s, _ := portableStyle(p.Style)
		out[i] = Preset{string(name), s, p.Effects}
	}
	return out, nil
}

// LoadPresetStore reads the user's saved presets; a missing file is an empty list.
func LoadPresetStore(path string) ([]Preset, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Preset{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var presets []Preset
	if err := json.Unmarshal(data, &presets); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return presets, nil
}

// SavePresetStore writes presets through a temporary file so a crash never leaves half a file.
func SavePresetStore(path string, presets []Preset) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if presets == nil {
		presets = []Preset{}
	}
	data, err := json.MarshalIndent(presets, "", "  ")
	if err != nil {
		return err
	}
	tmp := strings.TrimSuffix(path, filepath.Ext(path)) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}
