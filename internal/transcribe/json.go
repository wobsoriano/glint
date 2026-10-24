package transcribe

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// requireKeys fails when an object lacks one of keys, as serde does for a field without a
// default.
func requireKeys(data []byte, keys ...string) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if m == nil {
		return fmt.Errorf("expected an object, found null")
	}
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return fmt.Errorf("missing field `%s`", k)
		}
	}
	return nil
}

// writeAtomic writes beside path as `<stem>.json.tmp`, the name the Rust build uses, and
// renames over path.
func writeAtomic(path string, data []byte) error {
	tmp := strings.TrimSuffix(path, filepath.Ext(path)) + ".json.tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("renaming into %s: %w", path, err)
	}
	return nil
}
