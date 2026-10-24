package studio

import (
	"errors"
	"image"
	"io"
	"sync"

	"github.com/wobsoriano/glint/internal/media"
)

// SourceFrame is one decoded frame of a session track. It converts to RGBA on first use, on
// whichever goroutine asks, so the conversion runs beside the compositor instead of holding
// up the decoder.
type SourceFrame struct {
	tMs  float64
	raw  *media.Nv12Frame
	once sync.Once
	rgba *image.RGBA
	// shown is set once FrameAt has handed the frame out, so a frame the feed skips past
	// without showing can give its decode buffer straight back.
	shown bool
}

// RGBA is the frame as opaque RGBA. Safe for concurrent use.
func (f *SourceFrame) RGBA() *image.RGBA {
	f.once.Do(func() {
		f.rgba = f.raw.ToRGBA(nil)
		f.raw.Release()
		f.raw = nil
	})
	return f.rgba
}

// seekAheadMs is how far past the current frame FrameAt decodes forward before it seeks
// instead.
const seekAheadMs = 1_500.0

// Feed serves the frames of one video track by source time, decoding forward and seeking
// only when asked to go back or far ahead. It keeps the frame on screen and the next one.
type Feed struct {
	reader      *media.FrameReader
	halfFrameMs float64
	current     *SourceFrame
	next        *media.Nv12Frame
	ended       bool
	originMs    float64
}

func OpenFeed(path string) (*Feed, error) {
	r, err := media.OpenFrameReader(path)
	if err != nil {
		return nil, err
	}
	return &Feed{reader: r, halfFrameMs: 500 / max(r.FPS(), 1)}, nil
}

// FrameAt returns the frame showing at sourceMs: the last one whose time is at most half a
// frame past it. It is nil only when the track has no frames.
func (f *Feed) FrameAt(sourceMs float64) (*SourceFrame, error) {
	head := f.originMs
	if f.current != nil {
		head = f.current.tMs
	}
	behind := f.current != nil && sourceMs+f.halfFrameMs < f.current.tMs
	if behind || sourceMs > head+seekAheadMs {
		if err := f.reader.SeekMS(int64(max(sourceMs, 0))); err != nil {
			return nil, err
		}
		f.replace(nil)
		release(f.next)
		f.next, f.ended, f.originMs = nil, false, sourceMs
	}
	for {
		if f.next == nil && !f.ended {
			raw, err := f.reader.NextFrame()
			switch {
			case errors.Is(err, io.EOF):
				f.ended = true
			case err != nil:
				return nil, err
			default:
				f.next = raw
			}
		}
		if f.next == nil || (f.current != nil && float64(f.next.TMS) > sourceMs+f.halfFrameMs) {
			break
		}
		f.replace(&SourceFrame{tMs: float64(f.next.TMS), raw: f.next})
		f.next = nil
	}
	if f.current != nil {
		f.current.shown = true
	}
	return f.current, nil
}

func (f *Feed) replace(next *SourceFrame) {
	if old := f.current; old != nil && !old.shown {
		release(old.raw)
	}
	f.current = next
}

func release(raw *media.Nv12Frame) {
	if raw != nil {
		raw.Release()
	}
}

func (f *Feed) Close() {
	f.replace(nil)
	release(f.next)
	f.next = nil
	f.reader.Close()
}
