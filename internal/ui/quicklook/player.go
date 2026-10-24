package quicklook

import (
	"context"
	"errors"
	"image"
	"io"
	"sync/atomic"
	"time"

	"github.com/wobsoriano/glint/internal/media"
)

// player decodes a recording on its own goroutine and hands each frame to show on time, by the
// wall clock: Glint has no audio output yet, so there is no audio clock to follow and playback
// is silent.
type player struct {
	toggle chan struct{}
	cancel context.CancelFunc
	// shown counts the frames handed to show; posMS is the last one's time.
	shown   atomic.Int64
	posMS   atomic.Int64
	playing atomic.Bool
	err     atomic.Pointer[error]
}

// ring is how many frames are in flight: ui.Bitmap.Update keeps the image it is given until the
// next frame draws, so a frame's pixels are not reused at once.
const ring = 4

func startPlayer(path string, show func(img *image.RGBA)) *player {
	ctx, cancel := context.WithCancel(context.Background())
	p := &player{toggle: make(chan struct{}, 1), cancel: cancel}
	p.playing.Store(true)
	go func() {
		if err := p.run(ctx, path, show); err != nil && !errors.Is(err, context.Canceled) {
			p.err.Store(&err)
		}
		p.playing.Store(false)
	}()
	return p
}

func (p *player) stop() { p.cancel() }

// togglePlay pauses or resumes, and replays from the start once the recording has ended.
func (p *player) togglePlay() {
	select {
	case p.toggle <- struct{}{}:
	default:
	}
}

func (p *player) error() error {
	if e := p.err.Load(); e != nil {
		return *e
	}
	return nil
}

func (p *player) run(ctx context.Context, path string, show func(*image.RGBA)) error {
	r, err := media.OpenFrameReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	var frames [ring]*image.RGBA
	start := time.Now()
	first := int64(-1)
	for n := 0; ; n++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.toggle:
			paused := time.Now()
			p.playing.Store(false)
			if err := p.wait(ctx); err != nil {
				return err
			}
			p.playing.Store(true)
			start = start.Add(time.Since(paused))
		default:
		}
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			p.playing.Store(false)
			if err := p.wait(ctx); err != nil {
				return err
			}
			if err := r.SeekMS(0); err != nil {
				return err
			}
			p.playing.Store(true)
			start, first = time.Now(), -1
			continue
		}
		if err != nil {
			return err
		}
		if first < 0 {
			first = f.TMS
		}
		img := f.ToRGBA(frames[n%ring])
		frames[n%ring] = img
		t := f.TMS
		f.Release()
		if d := time.Until(start.Add(time.Duration(t-first) * time.Millisecond)); d > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(d):
			}
		}
		show(img)
		p.shown.Add(1)
		p.posMS.Store(t)
	}
}

func (p *player) wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-p.toggle:
		return nil
	}
}
