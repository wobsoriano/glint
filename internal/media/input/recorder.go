package input

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"slices"

	"github.com/wobsoriano/glint/internal/media/capture"
)

const pointerIntervalMS = 8

// Point is a position in screen-master pixels. It may fall outside the frame.
type Point struct{ X, Y float32 }

// cursorImage is a cursor shape with its hotspot, in straight RGBA.
type cursorImage struct {
	width, height int
	hotspot       [2]uint32
	rgba          []byte
}

// hash is FNV-1a over the pixels, mixed with the size and hotspot, as the Rust recorder
// computes it.
func (c cursorImage) hash() uint64 {
	h := uint64(0xcbf29ce484222325)
	for _, b := range c.rgba {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h ^ (uint64(c.width)<<48 | uint64(c.height)<<32 | uint64(c.hotspot[0])<<16 | uint64(c.hotspot[1]))
}

// recorder builds the timeline from pointer, button, key and cursor events. Platform hooks
// feed it positions already mapped to screen-master pixels. Nothing is recorded while the
// clock is paused.
type recorder struct {
	clock      *capture.Clock
	scale      float32
	cursorsDir string
	tl         Timeline
	known      map[uint64]uint32
	current    uint32
	lastPos    Point
	lastMS     uint64
	sampled    bool
	// pending holds the index in tl.Clicks of each button's open click, or -1.
	pending [len(buttonNames)]int
	ignored []string
}

// newRecorder saves cursor sprites to cursorsDir as <id>.png, drawn at the display scale.
// ignored lists shortcut labels to leave out, such as the app's own hotkeys.
func newRecorder(size [2]uint32, scale float32, cursorsDir string, clock *capture.Clock, ignored []string) *recorder {
	return &recorder{
		clock: clock, scale: scale, cursorsDir: cursorsDir,
		tl:      Timeline{Version: 1, Size: size},
		known:   map[uint64]uint32{},
		pending: [...]int{-1, -1, -1},
		ignored: ignored,
	}
}

func (r *recorder) now() (uint64, bool) {
	if r.clock.IsPaused() {
		return 0, false
	}
	return r.clock.NowMS(), true
}

// moved records a pointer move from an input event. Moves closer than 8 ms apart are dropped.
func (r *recorder) moved(p Point) {
	t, ok := r.now()
	if !ok {
		return
	}
	r.lastPos = p
	if r.sampled && t < r.lastMS+pointerIntervalMS {
		return
	}
	r.push(t, p)
}

func (r *recorder) pressed(b MouseButton, p Point) {
	t, ok := r.now()
	if !ok {
		return
	}
	r.lastPos = p
	r.push(t, p)
	r.tl.Clicks = append(r.tl.Clicks, Click{T: t, Up: t, X: p.X, Y: p.Y, Button: b})
	r.pending[b] = len(r.tl.Clicks) - 1
}

func (r *recorder) released(b MouseButton, p Point) {
	t, ok := r.now()
	if !ok {
		return
	}
	r.lastPos = p
	if i := r.pending[b]; i >= 0 {
		r.tl.Clicks[i].Up = t
		r.pending[b] = -1
	}
}

func (r *recorder) key(label string) {
	if slices.Contains(r.ignored, label) {
		return
	}
	if t, ok := r.now(); ok {
		r.tl.Keys = append(r.tl.Keys, KeyPress{T: t, Label: label})
	}
}

// polled records the pointer position from a poll, which catches moves no input event
// reported, such as a program warping the cursor.
func (r *recorder) polled(p Point) {
	if p == r.lastPos {
		return
	}
	if t, ok := r.now(); ok {
		r.lastPos = p
		r.push(t, p)
	}
}

// cursorChanged notes a new cursor shape. Each distinct shape is saved once.
func (r *recorder) cursorChanged(img cursorImage, p Point) {
	id := r.spriteID(img)
	if id == r.current {
		return
	}
	r.current = id
	if t, ok := r.now(); ok {
		r.push(t, p)
	}
}

func (r *recorder) push(t uint64, p Point) {
	r.tl.Pointer = append(r.tl.Pointer, PointerSample{T: t, X: p.X, Y: p.Y, Cursor: r.current})
	r.lastMS, r.sampled = t, true
}

func (r *recorder) spriteID(img cursorImage) uint32 {
	h := img.hash()
	if id, ok := r.known[h]; ok {
		return id
	}
	id := uint32(len(r.known))
	if err := savePNG(filepath.Join(r.cursorsDir, fmt.Sprintf("%d.png", id)), img); err == nil {
		r.known[h] = id
		r.tl.Cursors = append(r.tl.Cursors, CursorSprite{ID: id, File: fmt.Sprintf("cursors/%d.png", id), Hotspot: img.hotspot, Scale: r.scale})
	}
	return id
}

func savePNG(path string, img cursorImage) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	nrgba := &image.NRGBA{Pix: img.rgba, Stride: img.width * 4, Rect: image.Rect(0, 0, img.width, img.height)}
	err = png.Encode(f, nrgba)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// finish closes clicks whose button is still down at the end.
func (r *recorder) finish() Timeline {
	end := r.clock.NowMS()
	for _, i := range r.pending {
		if i >= 0 {
			r.tl.Clicks[i].Up = end
		}
	}
	return r.tl
}
