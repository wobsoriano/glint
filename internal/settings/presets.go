package settings

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings/serde"
)

func PresetsPath() string { return filepath.Join(DataDir(), "presets.json") }

// LoadPresets reads the user's saved editor looks; a missing file is an empty list. An absent
// effects object means the default effects.
func LoadPresets(path string) ([]render.Preset, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return []render.Preset{}, nil
	}
	if err != nil {
		return nil, err
	}
	// render.Preset fills in a missing or null style, which serde rejects.
	var raw []struct {
		Style   json.RawMessage `json:"style"`
		Effects json.RawMessage `json:"effects"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	for i, p := range raw {
		if !isStruct(p.Style) || (p.Effects != nil && !isStruct(p.Effects)) {
			return nil, fmt.Errorf("reading %s: preset %d needs a style object and an optional effects object", path, i)
		}
	}
	var presets []render.Preset
	if err := json.Unmarshal(data, &presets); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return presets, nil
}

func SavePresets(path string, presets []render.Preset) error {
	if presets == nil {
		presets = []render.Preset{}
	}
	data, err := serde.MarshalPretty(presets)
	if err != nil {
		return err
	}
	return writeAtomic(path, withExtension(path, "tmp"), data)
}
