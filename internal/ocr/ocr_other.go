//go:build !windows

package ocr

import (
	"errors"
	"image"
)

// Recognize reads the text in img and returns its lines in reading order. Only Windows
// has an implementation; macOS needs Vision through purego.
func Recognize(img *image.RGBA) ([]Line, error) { return nil, errors.ErrUnsupported }
