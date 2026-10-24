//go:build !windows

package settings

import (
	"os"
	"path/filepath"
)

func configDir() string {
	if d, err := os.UserConfigDir(); err == nil {
		return d
	}
	return "."
}

func picturesDir() string {
	if d, err := os.UserHomeDir(); err == nil {
		return filepath.Join(d, "Pictures")
	}
	return "."
}
