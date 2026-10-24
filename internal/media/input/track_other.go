//go:build !windows

package input

import (
	"errors"
	"image"
)

type Track struct{}

func Start(o Options) (*Track, error) { return nil, errors.ErrUnsupported }

func (t *Track) Stop() (Timeline, error) { return Timeline{}, errors.ErrUnsupported }

func windowOrigin(window int64, bounds WindowBounds) (image.Point, bool) { return image.Point{}, false }
