package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/wobsoriano/glint/internal/settings/serde"
)

// DataDir holds settings.json, presets.json and the Library. GLINT_DATA_DIR overrides it,
// which tests and side-by-side builds use to stay away from the real one.
func DataDir() string {
	if d, ok := os.LookupEnv("GLINT_DATA_DIR"); ok {
		return d
	}
	return filepath.Join(configDir(), "Glint")
}

func Path() string { return filepath.Join(DataDir(), "settings.json") }

// DefaultSaveDir is the export folder before the user picks one: Glint in the Pictures folder.
func DefaultSaveDir() string { return filepath.Join(picturesDir(), "Glint") }

// Parse decodes settings.json. Fields the file leaves out keep their defaults and unknown
// fields are ignored. A leading byte-order mark is allowed, since Notepad writes one.
func Parse(data []byte) (Settings, error) {
	s := Default()
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if err := json.Unmarshal(data, &s); err != nil {
		return Default(), err
	}
	if !isStruct(s.Style) {
		return Default(), fmt.Errorf("style: expected an object, found %s", s.Style)
	}
	s.Overlay.Layout = s.Overlay.Layout.Normalized()
	return s, nil
}

func Load() Settings { return LoadFrom(Path()) }

// LoadFrom never fails: a missing file gives the defaults, and a file that does not parse is
// renamed to settings.json.broken so the user's edits survive, and the defaults load.
func LoadFrom(path string) Settings {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			log.Printf("reading %s: %v; using default settings", path, err)
		}
		s := Default()
		s.Overlay.Layout = s.Overlay.Layout.Normalized()
		return s
	}
	s, err := Parse(data)
	if err != nil {
		broken := withExtension(path, "json.broken")
		if rerr := os.Rename(path, broken); rerr != nil {
			log.Printf("%s is not valid (%v) and could not be moved aside (%v); using default settings", path, err, rerr)
		} else {
			log.Printf("%s is not valid (%v); moved it to %s and using default settings", path, err, broken)
		}
	}
	return s
}

func (s Settings) Save() error { return s.SaveTo(Path()) }

// SaveTo replaces the file atomically, so a crash mid-write never leaves half a file.
func (s Settings) SaveTo(path string) error {
	data, err := serde.MarshalPretty(s)
	if err != nil {
		return err
	}
	return writeAtomic(path, withExtension(path, "json.tmp"), data)
}

func writeAtomic(path, tmp string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// withExtension mirrors Rust's Path::with_extension.
func withExtension(path, ext string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + "." + ext
}

// isStruct reports whether raw could decode into a Rust struct, which serde accepts as a JSON
// object or array but never as null or a scalar.
func isStruct(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) > 0 && (t[0] == '{' || t[0] == '[')
}
