//go:build !windows

package overlay

import "github.com/wobsoriano/glint/internal/platform"

// prepare does nothing yet: the macOS overlay places itself with WindowOptions.
func prepare(uintptr, platform.Rect) {}
