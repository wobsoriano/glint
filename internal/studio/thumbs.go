package studio

import (
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"os"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/media"
)

// TimelineThumbnails returns count frames height pixels tall, thumbnail i taken at the
// middle of the i-th of count equal slices of source time. The strip is cached in render/.
// It blocks; run it off the UI goroutine.
func TimelineThumbnails(s *Session, count, height uint32) ([]*image.RGBA, error) {
	width := max(uint32(uint64(s.SourceW)*uint64(height)/uint64(max(s.SourceH, 1))), 1)
	w, h, n := int(width), int(height), int(count)
	dir := s.RenderDir()
	path := filepath.Join(dir, fmt.Sprintf("thumbs-%dx%d.png", count, height))
	if strip, err := readStrip(path); err == nil && strip.Rect.Dx() == w*n && strip.Rect.Dy() == h {
		thumbs := make([]*image.RGBA, n)
		for i := range thumbs {
			thumbs[i] = image.NewRGBA(image.Rect(0, 0, w, h))
			draw.Draw(thumbs[i], thumbs[i].Rect, strip, image.Pt(i*w, 0), draw.Src)
		}
		return thumbs, nil
	}

	reader, err := media.OpenFrameReader(s.Tracks.Screen)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	thumbs := make([]*image.RGBA, 0, n)
	for i := range n {
		t := s.SourceDurationMs * (float64(i) + 0.5) / float64(n)
		if err := reader.SeekMS(int64(t)); err != nil {
			return nil, err
		}
		raw, err := reader.NextFrame()
		var frame *image.RGBA
		switch {
		case errors.Is(err, io.EOF):
			if len(thumbs) == 0 {
				return nil, errors.New("screen.mp4 has no frames")
			}
			frame = thumbs[len(thumbs)-1]
		case err != nil:
			return nil, err
		default:
			frame = raw.ToRGBA(nil)
			raw.Release()
		}
		if frame.Rect.Dx() != w || frame.Rect.Dy() != h {
			frame = media.Shrink(frame, w, h)
		}
		thumbs = append(thumbs, frame)
	}

	strip := image.NewRGBA(image.Rect(0, 0, w*n, h))
	for i, t := range thumbs {
		draw.Draw(strip, image.Rect(i*w, 0, i*w+w, h), t, image.Point{}, draw.Src)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	tmp := filepath.Join(dir, fmt.Sprintf("thumbs-%dx%d.%d.tmp.png", count, height, os.Getpid()))
	if err := writePNGFile(tmp, strip); err != nil {
		return nil, fmt.Errorf("writing %s: %w", tmp, err)
	}
	if os.Rename(tmp, path) != nil {
		os.Remove(tmp)
	}
	return thumbs, nil
}

func readStrip(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	rgba := image.NewRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
	draw.Draw(rgba, rgba.Rect, img, img.Bounds().Min, draw.Src)
	return rgba, nil
}

func writePNGFile(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(path)
		return err
	}
	return f.Close()
}
