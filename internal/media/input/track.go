package input

import (
	"image"

	"github.com/wobsoriano/glint/internal/media/capture"
)

// WindowBounds says which rectangle of a window the screen master captures.
type WindowBounds uint8

const (
	// BoundsDWM is the visible frame (DWMWA_EXTENDED_FRAME_BOUNDS).
	BoundsDWM WindowBounds = iota
	// BoundsWindowRect is GetWindowRect, with the invisible resize borders.
	BoundsWindowRect
)

// PointerMap maps desktop pixels to the capture's pixels: by a fixed origin for a display
// or area, or by a window's origin, looked up on every event because the window may move.
type PointerMap struct {
	window int64
	bounds WindowBounds
	// origin is the fixed origin, or the window's last known one.
	origin image.Point
}

func FixedOrigin(x, y int) PointerMap { return PointerMap{origin: image.Pt(x, y)} }

// WindowOrigin follows the window whose platform id is window.
func WindowOrigin(window int64, bounds WindowBounds) PointerMap {
	return PointerMap{window: window, bounds: bounds}
}

func (m *PointerMap) toCapture(x, y int) Point {
	if m.window != 0 {
		if o, ok := windowOrigin(m.window, m.bounds); ok {
			m.origin = o
		}
	}
	return Point{float32(x - m.origin.X), float32(y - m.origin.Y)}
}

type Options struct {
	Map PointerMap
	// Size is the screen master's pixel size.
	Size [2]uint32
	// Scale is the display scale cursor sprites are drawn at.
	Scale float32
	// SessionDir receives cursors/<n>.png. The caller saves the timeline Stop returns.
	SessionDir string
	Clock      *capture.Clock
	// IgnoredKeys are shortcut labels left out of the timeline, written as the track labels
	// them (Ctrl+Shift+7 on Windows), such as Glint's own hotkeys.
	IgnoredKeys []string
}
