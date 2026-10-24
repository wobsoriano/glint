package settings

import (
	"encoding/json"
	"fmt"
)

// parseEnum accepts exactly the spellings serde writes for a unit-variant enum, so a file
// Rust would reject is rejected here too instead of loading as a zero value.
func parseEnum[T ~string](data []byte, all []T) (T, error) {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return "", err
	}
	for _, v := range all {
		if string(v) == s {
			return v, nil
		}
	}
	return "", fmt.Errorf("unknown variant %q, expected one of %q", s, all)
}

// fixedArray decodes a JSON array that must hold exactly n elements, as a Rust [T; n] does.
func fixedArray[T any](data []byte, n int) ([]T, error) {
	var xs []T
	if err := json.Unmarshal(data, &xs); err != nil {
		return nil, err
	}
	if len(xs) != n {
		return nil, fmt.Errorf("invalid length %d, expected an array of length %d", len(xs), n)
	}
	return xs, nil
}
