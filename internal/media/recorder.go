package media

import (
	"fmt"
	"image"
	"time"

	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/platform"
)

// Source is what the screen master records: a DisplaySource, WindowSource or AreaSource.
type Source interface{ source() }

// DisplaySource records a whole display. Display is its platform.Display.ID.
type DisplaySource struct{ Display int64 }

// WindowSource records one top-level window, following it as it moves. Window is its
// platform.Window.ID. The video keeps the size the window had when recording started.
type WindowSource struct{ Window int64 }

// AreaSource records Rect, in physical desktop pixels, clipped to the display it lies on.
type AreaSource struct {
	Display int64
	Rect    platform.Rect
}

func (DisplaySource) source() {}
func (WindowSource) source()  {}
func (AreaSource) source()    {}

type FPS int

const (
	FPS30 FPS = 30
	FPS60 FPS = 60
)

// CameraID identifies a camera across runs on the same machine: its symbolic link on
// Windows, the string settings.json stores as "camera".
type CameraID string

type CameraDevice struct {
	ID   CameraID
	Name string
}

type RecordOptions struct {
	Source Source
	// SessionDir is created if missing. The session package files are written directly
	// into it.
	SessionDir string
	FPS        FPS
	// SystemAudio records what the default output device plays to system.wav.
	SystemAudio bool
	// Mic records this microphone to mic.wav. An empty id is the default microphone.
	Mic *capture.DeviceID
	// Camera records this camera to camera.mp4.
	Camera *CameraID
	// RecordInput records pointer, clicks, shortcuts and cursor sprites to input.json.
	RecordInput bool
	// MicTap is also offered every microphone buffer (48 kHz mono, paused or not), for
	// live speech recognition. A full channel drops the buffer; the recording never waits.
	MicTap chan<- []float32
	// IgnoredKeys are shortcut labels left out of input.json, written as the input track
	// labels them (Ctrl+Shift+7 on Windows).
	IgnoredKeys []string
}

type RecordSummary struct {
	// Files are inside the session directory, screen master first.
	Files []string
	// DurationMS is media time, paused spans excluded.
	DurationMS uint64
	Bytes      uint64
	// Width and Height are the screen master's pixel size.
	Width, Height uint32
	Hardware      bool
	// Warnings name optional tracks that were skipped or cut short, and anything else
	// worth telling the user.
	Warnings []string
	Screen   ScreenStats
}

// ScreenStats counts what the screen master did. Every frame slot of the constant frame
// rate is written; a slot whose source frame had not changed repeats the last one.
type ScreenStats struct {
	// Frames is how many frames screen.mp4 holds.
	Frames int64
	// Captured is how many frames the capture API delivered.
	Captured int64
	// Converted is how many distinct frames were read back and converted for the encoder.
	Converted int64
	// Late counts slots written more than one frame period after they were due, because
	// encoding fell behind. They repeat the newest frame instead of being dropped.
	Late int64
	// Busy is the time spent reading frames back, converting them and handing them to the
	// encoder; against the recording's length it says how close 60 fps is to the limit.
	Busy time.Duration
}

// cropFor is the part of a w×h capture item the screen master shows: all of it, or area
// (in item pixels) clipped to it, each side rounded down to even pixels for H.264.
func cropFor(w, h int, area *image.Rectangle) (image.Rectangle, error) {
	r := image.Rect(0, 0, w, h)
	if area != nil {
		r = area.Intersect(r)
	}
	r.Max = r.Min.Add(image.Pt(r.Dx()&^1, r.Dy()&^1))
	if r.Dx() < 2 || r.Dy() < 2 {
		return image.Rectangle{}, fmt.Errorf("capture area is empty (%dx%d)", r.Dx(), r.Dy())
	}
	return r, nil
}

// areaInDisplay is an area of the virtual desktop in the pixels of the display at bounds.
func areaInDisplay(area, bounds platform.Rect) image.Rectangle {
	return image.Rect(area.X-bounds.X, area.Y-bounds.Y, area.X-bounds.X+area.W, area.Y-bounds.Y+area.H)
}
