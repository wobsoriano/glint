package studio

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const (
	PresetVersion     = 1
	presetDefaultFile = "default.txt"
)

// Preset is a named Studio design saved as `<slug>.json` in the presets directory.
type Preset struct {
	Version uint32 `json:"version"`
	Name    string `json:"name"`
	Design  Design `json:"design"`
}

func NewPreset(name string, design Design) Preset {
	return Preset{Version: PresetVersion, Name: strings.TrimSpace(name), Design: design}
}

func (p *Preset) UnmarshalJSON(data []byte) error {
	type plain Preset
	if err := requireKeys(data, "name"); err != nil {
		return err
	}
	v := plain{Version: PresetVersion, Design: DefaultDesign()}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*p = Preset(v)
	return nil
}

func (p Preset) Apply(project *Project) {
	project.Design = p.Design
	project.Design.Background = cloneBackground(p.Design.Background)
}

type PresetStore struct{ dir string }

func NewPresetStore(dir string) PresetStore { return PresetStore{dir} }

// List returns every readable preset sorted by name. Unreadable files are skipped.
func (s PresetStore) List() []Preset {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil
	}
	var out []Preset
	for _, e := range entries {
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.dir, e.Name()))
		if err != nil {
			continue
		}
		var p Preset
		if json.Unmarshal(data, &p) == nil {
			out = append(out, p)
		}
	}
	slices.SortStableFunc(out, func(a, b Preset) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out
}

// Save writes a preset, replacing one with the same name.
func (s PresetStore) Save(p Preset) error {
	if slug(p.Name) == "" {
		return errors.New("preset name is empty")
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", s.dir, err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	path := s.path(p.Name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

func (s PresetStore) Load(name string) (Preset, error) {
	path := s.path(name)
	data, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return Preset{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return p, nil
}

func (s PresetStore) Delete(name string) error {
	if d, ok := s.defaultName(); ok && d == name {
		if err := s.SetDefault(""); err != nil {
			return err
		}
	}
	path := s.path(name)
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("deleting %s: %w", path, err)
	}
	return nil
}

// SetDefault chooses the preset applied to new recordings; an empty name clears it.
func (s PresetStore) SetDefault(name string) error {
	path := filepath.Join(s.dir, presetDefaultFile)
	if name == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("deleting %s: %w", path, err)
		}
		return nil
	}
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// DefaultPreset is the default preset, if one is set and still exists.
func (s PresetStore) DefaultPreset() (Preset, bool) {
	name, ok := s.defaultName()
	if !ok {
		return Preset{}, false
	}
	p, err := s.Load(name)
	return p, err == nil
}

func (s PresetStore) defaultName() (string, bool) {
	data, err := os.ReadFile(filepath.Join(s.dir, presetDefaultFile))
	if err != nil {
		return "", false
	}
	n := strings.TrimSpace(string(data))
	return n, n != ""
}

func (s PresetStore) path(name string) string {
	return filepath.Join(s.dir, slug(name)+".json")
}

func slug(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(name) {
		switch {
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			b.WriteString(strings.ToLower(string(r)))
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
	}
	return strings.TrimRight(b.String(), "-")
}
