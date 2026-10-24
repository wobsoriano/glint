//go:build !windows

package swatch

import (
	"image/color"

	"github.com/wobsoriano/glint/internal/platform"
)

type Swatch struct{ HWND uintptr }

func Show(r platform.Rect, c color.RGBA, title string, tool bool) (*Swatch, error) {
	return nil, platform.ErrUnsupported
}

func (s *Swatch) Close() {}
