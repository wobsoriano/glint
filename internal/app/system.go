package app

import (
	"image"
	"os"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/overlay"
)

// system is the desktop the capture flow reads: displays, pixels, windows and text. Tests
// replace it, so the flow runs without a desktop.
type system interface {
	displays() ([]platform.Display, error)
	captureRect(r platform.Rect) (*image.RGBA, error)
	captureWindow(id int64, shadow bool) (*image.RGBA, error)
	// windows lists other apps' windows, front to back.
	windows() []platform.Window
	cursor() image.Point
	// foreground is the window of another app that has the keyboard.
	foreground() (platform.Window, bool)
	recognize(img *image.RGBA) ([]ocr.Line, error)
}

// picker is the capture overlay.
type picker interface {
	Open(shots []overlay.Shot, opts overlay.Options)
	IsOpen() bool
}

type desktop struct{}

func (desktop) displays() ([]platform.Display, error)            { return platform.Displays() }
func (desktop) captureRect(r platform.Rect) (*image.RGBA, error) { return platform.CaptureRect(r) }
func (desktop) captureWindow(id int64, shadow bool) (*image.RGBA, error) {
	return platform.CaptureWindow(id, shadow)
}
func (desktop) cursor() image.Point                           { return image.Pt(platform.CursorPos()) }
func (desktop) recognize(img *image.RGBA) ([]ocr.Line, error) { return ocr.Recognize(img) }

func (desktop) windows() []platform.Window {
	all, _ := platform.Windows()
	out := all[:0]
	for _, w := range all {
		if w.PID != os.Getpid() {
			out = append(out, w)
		}
	}
	return out
}

func (d desktop) foreground() (platform.Window, bool) {
	id := foregroundWindow()
	for _, w := range d.windows() {
		if w.ID == id {
			return w, true
		}
	}
	return platform.Window{}, false
}
