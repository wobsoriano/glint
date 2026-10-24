package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/settings/serde"
)

// Preset is a saved editor look. Style and Effects stay as the JSON the file holds until
// internal/render's types replace them; an absent Effects means the default effects.
type Preset struct {
	Name    string          `json:"name"`
	Style   json.RawMessage `json:"style"`
	Effects json.RawMessage `json:"effects,omitempty"`
}

func PresetsPath() string { return filepath.Join(DataDir(), "presets.json") }

// LoadPresets reads the user's saved presets; a missing file is an empty list.
func LoadPresets(path string) ([]Preset, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []Preset{}, nil
	}
	if err != nil {
		return nil, err
	}
	var presets []Preset
	if err := json.Unmarshal(data, &presets); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	for i, p := range presets {
		if !isStruct(p.Style) || (p.Effects != nil && !isStruct(p.Effects)) {
			return nil, fmt.Errorf("reading %s: preset %d needs a style object and an optional effects object", path, i)
		}
	}
	return presets, nil
}

func SavePresets(path string, presets []Preset) error {
	if presets == nil {
		presets = []Preset{}
	}
	data, err := serde.MarshalPretty(presets)
	if err != nil {
		return err
	}
	return writeAtomic(path, withExtension(path, "tmp"), data)
}
