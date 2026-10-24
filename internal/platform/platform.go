// Package platform reaches the operating system for what MyGo does not cover: the screen's
// pixels, the windows on it, keeping Glint's windows out of captures, the
// wallpaper and launching at login. Every OS implements the same functions in its own files.
package platform

import "errors"

// ErrUnsupported is returned where the running OS has no implementation yet.
var ErrUnsupported = errors.ErrUnsupported

// Rect is in physical pixels of the virtual desktop, with the origin at the primary display's
// top-left corner. MyGo's own rectangles are in DIPs; convert with Display.Scale.
type Rect struct{ X, Y, W, H int }

func (r Rect) Empty() bool            { return r.W <= 0 || r.H <= 0 }
func (r Rect) Contains(x, y int) bool { return x >= r.X && y >= r.Y && x < r.X+r.W && y < r.Y+r.H }

// Intersect returns the overlap of r and o, empty when they do not overlap.
func (r Rect) Intersect(o Rect) Rect {
	x0, y0 := max(r.X, o.X), max(r.Y, o.Y)
	x1, y1 := min(r.X+r.W, o.X+o.W), min(r.Y+r.H, o.Y+o.H)
	if x1 <= x0 || y1 <= y0 {
		return Rect{}
	}
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

type Display struct {
	// ID is the HMONITOR on Windows and the CGDirectDisplayID on macOS.
	ID      int64
	Name    string // "Display 1", numbered left to right
	Bounds  Rect
	Scale   float64 // physical pixels per DIP
	Primary bool
}

type Window struct {
	// ID is the HWND on Windows and the CGWindowID on macOS.
	ID    int64
	Title string
	App   string // the owning executable's name without extension, or the app's name
	PID   int
	// Bounds is the visible frame, without the invisible resize borders Windows adds.
	Bounds Rect
}
