package quicklook

import (
	"context"
	"errors"
	"image"
	"io"
	"log"
	"sync/atomic"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/playback"
	"github.com/wobsoriano/glint/internal/store"
)

// player decodes a recording on its own goroutine and hands each frame to show when the
// sound reaches it: the audio device's clock while the recording has sound and an output
// device opens, the wall clock otherwise.
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

func startPlayer(item store.Item, show func(img *image.RGBA)) *player {
	ctx, cancel := context.WithCancel(context.Background())
	p := &player{toggle: make(chan struct{}, 1), cancel: cancel}
	p.playing.Store(true)
	go func() {
		if err := p.run(ctx, item, show); err != nil && !errors.Is(err, context.Canceled) {
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

// speaker is the sound of one run of playback, from a position to a pause or the end.
type speaker struct {
	track *playback.Soundtrack
	out   *playback.AudioOutput
	clock playback.Clock
}

// speak starts the sound at ms. Without sound, or when the output fails, the clock is the
// wall clock and the picture plays on.
func speak(track *playback.Soundtrack, ms int64) *speaker {
	s := &speaker{track: track}
	if track != nil {
		err := track.SeekMS(ms)
		if err == nil {
			s.out, err = playback.StartAudioOutput(track.Fill)
		}
		if err != nil {
			log.Printf("quicklook: playing without sound: %v", err)
			s.track = nil
		}
	}
	s.clock = playback.NewClock(ms, s.out)
	return s
}

func (s *speaker) topUp() {
	if s.track != nil && s.track.TopUp() != nil {
		s.track = nil
	}
}

func (s *speaker) close() {
	if s.out != nil {
		s.out.Close()
	}
}

func (p *player) run(ctx context.Context, item store.Item, show func(*image.RGBA)) error {
	r, err := media.OpenFrameReader(item.Deliverable())
	if err != nil {
		return err
	}
	defer r.Close()
	var track *playback.Soundtrack
	sound, closeSound, err := openSound(item)
	switch {
	case err != nil:
		log.Printf("quicklook: playing without sound: %v", err)
	case sound != nil:
		defer closeSound()
		track = playback.NewSoundtrack(sound)
	}
	var frames [ring]*image.RGBA
	pos := int64(0)
	sp := speak(track, pos)
	defer func() { sp.close() }()
	for n := 0; ; n++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.toggle:
			sp.close()
			p.playing.Store(false)
			if err := p.wait(ctx); err != nil {
				return err
			}
			p.playing.Store(true)
			sp = speak(track, pos)
		default:
		}
		sp.topUp()
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			sp.close()
			p.playing.Store(false)
			if err := p.wait(ctx); err != nil {
				return err
			}
			if err := r.SeekMS(0); err != nil {
				return err
			}
			p.playing.Store(true)
			pos = 0
			sp = speak(track, pos)
			continue
		}
		if err != nil {
			return err
		}
		img := f.ToRGBA(frames[n%ring])
		frames[n%ring] = img
		t := f.TMS
		f.Release()
		for sp.clock.NowMS() < float64(t) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Millisecond):
			}
			sp.topUp()
		}
		show(img)
		pos = t
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
