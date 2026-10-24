// Package serde encodes JSON the way the Rust build's serde_json did, so files Glint writes
// stay byte-for-byte what the Rust build would have written for the same values.
package serde

import (
	"bytes"
	"encoding/json"
)

// MarshalPretty writes JSON as serde_json's to_string_pretty does: two-space indent, no
// trailing newline, and no escaping of <, >, & or U+2028/U+2029.
func MarshalPretty(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return unescapeLineSeparators(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// unescapeLineSeparators undoes encoding/json's 2028 and 2029 escapes, which serde_json
// does not write. Escapes are walked in pairs so an escaped backslash before "u2028" is kept.
func unescapeLineSeparators(b []byte) []byte {
	if !bytes.Contains(b, []byte(`\u202`)) {
		return b
	}
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] != '\\' || i+1 >= len(b) {
			out = append(out, b[i])
			continue
		}
		if b[i+1] == 'u' && i+6 <= len(b) {
			switch string(b[i+2 : i+6]) {
			case "2028":
				out = append(out, "\xe2\x80\xa8"...)
				i += 5
				continue
			case "2029":
				out = append(out, "\xe2\x80\xa9"...)
				i += 5
				continue
			}
		}
		out = append(out, b[i], b[i+1])
		i++
	}
	return out
}
