package drive

import (
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/store"
)

// Screenshot imports a w x h screenshot of diagonal stripes in two colors, titled title.
func Screenshot(lib store.Library, title string, w, h int, a, b color.RGBA, at time.Time) (store.Item, error) {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := a
			if (x+y)/40%2 == 1 {
				c = b
			}
			img.SetRGBA(x, y, c)
		}
	}
	return lib.ImportScreenshot(store.NewScreenshot{Base: img, Created: at, Title: title, Source: "Glint demo"})
}

// Recording writes a recording of seconds at 30 fps, w x h, whose white bar sweeps left to
// right, so each frame differs from the one before.
func Recording(lib store.Library, w, h int, seconds int, at time.Time) (store.Item, error) {
	p, err := lib.BeginRecording(at)
	if err != nil {
		return store.Item{}, err
	}
	const fps = 30
	wr, err := media.CreateVideoWriter(filepath.Join(p.Dir, "screen.mp4"), media.VideoParams{Width: w, Height: h, FPS: fps, Bitrate: media.RecordingBitrate(w, h, fps)}, nil)
	if err != nil {
		return store.Item{}, err
	}
	defer wr.Close()
	frames := seconds * fps
	var first *image.RGBA
	for i := range frames {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		bar := i * (w - 40) / max(frames-1, 1)
		for y := range h {
			for x := range w {
				c := color.RGBA{0x20, 0x30, uint8(0x60 + y*0x60/h), 255}
				if x >= bar && x < bar+40 {
					c = color.RGBA{255, 255, 255, 255}
				}
				img.SetRGBA(x, y, c)
			}
		}
		if first == nil {
			first = img
		}
		if err := wr.WriteFrame(img); err != nil {
			return store.Item{}, fmt.Errorf("frame %d: %w", i, err)
		}
	}
	sum, err := wr.Finish()
	if err != nil {
		return store.Item{}, err
	}
	return lib.FinishRecording(p, store.RecordingInfo{Width: uint32(w), Height: uint32(h), DurationMs: uint64(sum.DurationMS), Source: "Glint demo"}, first)
}
