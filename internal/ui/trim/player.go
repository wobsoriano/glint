package trim

import (
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/playback"
)

// previewW and previewH bound the frames the preview shows; larger videos are shrunk.
const previewW, previewH = 1280, 720

// Frame is a preview frame. Its Image belongs to the player again once handed to Recycle.
type Frame struct {
	Image *image.RGBA
	TMS   int64
}

// Player decodes a video on its own goroutine: a frame on each seek, and frames in time with
// the audio while it plays. onFrame runs on that goroutine after each new frame; the window
// then takes it with TakeFrame.
type Player struct {
	onFrame func()

	mu   sync.Mutex
	wake *sync.Cond
	ctl  control

	position atomic.Int64
	playing  atomic.Bool
	audio    atomic.Int32
	clock    atomic.Pointer[playback.Clock]
	err      atomic.Pointer[string]

	frames frameBox
	done   chan struct{}
}

const noSeek = -1

type control struct {
	seekMS int64
	// untilMS is where the current play stops, or noSeek when paused.
	untilMS int64
	muted   bool
	quit    bool
}

// Audio track states, as Player.Audio reports them.
const (
	AudioUnknown int32 = iota
	AudioPresent
	AudioAbsent
)

func NewPlayer(path string, onFrame func()) *Player {
	p := &Player{onFrame: onFrame, ctl: control{seekMS: 0, untilMS: noSeek}, done: make(chan struct{})}
	p.wake = sync.NewCond(&p.mu)
	go func() {
		defer close(p.done)
		if err := p.run(path); err != nil {
			p.setErr(err.Error())
			p.playing.Store(false)
		}
	}()
	return p
}

// SeekMS shows the frame at ms, replacing a seek not yet done.
func (p *Player) SeekMS(ms int64) {
	p.position.Store(ms)
	p.command(func(c *control) { c.seekMS = ms })
}

// Play plays from the position until untilMS, without sound when muted.
func (p *Player) Play(untilMS int64, muted bool) {
	p.playing.Store(true)
	p.command(func(c *control) { c.untilMS, c.muted = untilMS, muted })
}

func (p *Player) Pause() {
	p.playing.Store(false)
	p.command(func(c *control) { c.untilMS = noSeek })
}

func (p *Player) Playing() bool     { return p.playing.Load() }
func (p *Player) PositionMS() int64 { return p.position.Load() }

// Audio is AudioUnknown until the file's audio track has been opened.
func (p *Player) Audio() int32 { return p.audio.Load() }

// Err is the last decoding or audio error, or "".
func (p *Player) Err() string {
	if s := p.err.Load(); s != nil {
		return *s
	}
	return ""
}

// ClockMS is the media time the current play has reached by the clock it follows, the audio
// device when sound plays; ok is false while paused.
func (p *Player) ClockMS() (ms float64, audio, ok bool) {
	c := p.clock.Load()
	if c == nil {
		return 0, false, false
	}
	return c.NowMS(), c.FollowsAudio(), true
}

// TakeFrame returns the newest frame not yet taken.
func (p *Player) TakeFrame() (Frame, bool) { return p.frames.take() }

// Recycle hands back the image of a frame no longer shown.
func (p *Player) Recycle(img *image.RGBA) { p.frames.recycle(img) }

// Close stops the player and waits for its goroutine, which closes the files.
func (p *Player) Close() {
	p.command(func(c *control) { c.quit = true })
	<-p.done
}

func (p *Player) setErr(s string) { p.err.Store(&s) }

func (p *Player) command(fn func(*control)) {
	p.mu.Lock()
	fn(&p.ctl)
	p.mu.Unlock()
	p.wake.Broadcast()
}

type next int

const (
	nextQuit next = iota
	nextSeek
	nextPlay
)

func (p *Player) nextCommand() (next, control) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for {
		c := p.ctl
		switch {
		case c.quit:
			return nextQuit, c
		case c.seekMS != noSeek:
			p.ctl.seekMS = noSeek
			return nextSeek, c
		case c.untilMS != noSeek:
			return nextPlay, c
		}
		p.wake.Wait()
	}
}

func (p *Player) interrupted() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ctl.quit || p.ctl.seekMS != noSeek || p.ctl.untilMS == noSeek
}

func (p *Player) run(path string) error {
	reader, err := media.OpenFrameReader(path)
	if err != nil {
		return err
	}
	defer reader.Close()
	var track *playback.Soundtrack
	switch ar, err := media.OpenAudioReader(path); {
	case err == nil:
		defer ar.Close()
		track = playback.NewSoundtrack(ar)
		p.audio.Store(AudioPresent)
	case errors.Is(err, media.ErrNoAudio):
		p.audio.Store(AudioAbsent)
	default:
		p.audio.Store(AudioAbsent)
		p.audioUnavailable(err)
	}
	frameMS := 1000 / max(reader.FPS(), 1)
	var scratch *image.RGBA
	publish := func(f *media.Nv12Frame) {
		w, h := previewSize(f.Width, f.Height)
		var img *image.RGBA
		if w == f.Width && h == f.Height {
			img = f.ToRGBA(p.frames.spare(w, h))
		} else {
			scratch = f.ToRGBA(scratch)
			img = media.Shrink(scratch, w, h)
		}
		p.frames.put(Frame{Image: img, TMS: f.TMS})
		if p.onFrame != nil {
			p.onFrame()
		}
	}
	for {
		cmd, c := p.nextCommand()
		switch cmd {
		case nextQuit:
			return nil
		case nextSeek:
			if err := reader.SeekMS(c.seekMS); err != nil {
				return err
			}
			f, err := reader.NextFrame()
			if errors.Is(err, io.EOF) {
				continue
			}
			if err != nil {
				return err
			}
			publish(f)
			f.Release()
		case nextPlay:
			if err := p.play(reader, track, c, frameMS, publish); err != nil {
				return err
			}
		}
	}
}

// play shows frames from the position until c.untilMS, each when the clock reaches it, and
// drops frames that are already late.
func (p *Player) play(reader *media.FrameReader, track *playback.Soundtrack, c control, frameMS float64, publish func(*media.Nv12Frame)) error {
	start := p.position.Load()
	var out *playback.AudioOutput
	if track != nil && !c.muted {
		var err error
		if err = track.SeekMS(start); err == nil {
			out, err = playback.StartAudioOutput(track.Fill)
		}
		if err != nil {
			p.audioUnavailable(err)
			track = nil
		}
	} else {
		track = nil
	}
	clock := playback.NewClock(start, out)
	p.clock.Store(&clock)
	defer func() {
		p.clock.Store(nil)
		if out != nil {
			out.Close()
		}
	}()
	topUp := func() {
		if track == nil {
			return
		}
		if err := track.TopUp(); err != nil {
			p.audioUnavailable(err)
			track = nil
		}
	}
	finished, err := func() (bool, error) {
		for !p.interrupted() {
			topUp()
			f, err := reader.NextFrame()
			if errors.Is(err, io.EOF) {
				return true, nil
			}
			if err != nil {
				return false, err
			}
			if f.TMS >= c.untilMS {
				f.Release()
				return true, nil
			}
			if float64(f.TMS)+frameMS < clock.NowMS() {
				f.Release()
				continue
			}
			for clock.NowMS() < float64(f.TMS) && !p.interrupted() {
				topUp()
				time.Sleep(2 * time.Millisecond)
			}
			publish(f)
			f.Release()
			p.position.Store(max(f.TMS, start))
		}
		return false, nil
	}()
	if err != nil {
		return err
	}
	if finished {
		p.position.Store(c.untilMS)
		p.playing.Store(false)
		p.mu.Lock()
		if p.ctl.untilMS == c.untilMS {
			p.ctl.untilMS = noSeek
		}
		p.mu.Unlock()
		if p.onFrame != nil {
			p.onFrame()
		}
	}
	return nil
}

// audioUnavailable costs the sound, not the picture: it says so and the video plays on.
func (p *Player) audioUnavailable(err error) {
	p.setErr(fmt.Sprintf("audio unavailable: %v", err))
}

func previewSize(w, h int) (int, int) {
	k := min(float64(previewW)/float64(w), float64(previewH)/float64(h), 1)
	if k == 1 {
		return w, h
	}
	return max(int(math.Floor(float64(w)*k)), 1), max(int(math.Floor(float64(h)*k)), 1)
}

// frameBox hands frames from the decoder to the window. The window shows each image without a
// copy (ui.Bitmap.Update), so an image is reused only after the window recycles it.
type frameBox struct {
	mu     sync.Mutex
	next   *Frame
	spares []*image.RGBA
}

func (b *frameBox) put(f Frame) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.next != nil {
		b.spares = append(b.spares, b.next.Image)
	}
	b.next = &f
}

func (b *frameBox) take() (Frame, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.next == nil {
		return Frame{}, false
	}
	f := *b.next
	b.next = nil
	return f, true
}

func (b *frameBox) recycle(img *image.RGBA) {
	if img == nil {
		return
	}
	b.mu.Lock()
	b.spares = append(b.spares, img)
	b.mu.Unlock()
}

// spare returns a recycled image of w x h, or nil for ToRGBA to allocate one.
func (b *frameBox) spare(w, h int) *image.RGBA {
	b.mu.Lock()
	defer b.mu.Unlock()
	for len(b.spares) > 0 {
		img := b.spares[len(b.spares)-1]
		b.spares = b.spares[:len(b.spares)-1]
		if img.Rect.Dx() == w && img.Rect.Dy() == h {
			return img
		}
	}
	return nil
}
