package studio

import (
	"errors"
	"fmt"
	"image"
	"math"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wobsoriano/glint/internal/media/playback"
	core "github.com/wobsoriano/glint/internal/studio"
)

// frameInterval paces composing while playing: the display's 60 Hz, whatever the recording's
// rate, so the cursor and zooms move at the rate the screen shows.
const frameInterval = time.Second / 60

// player composes preview frames on a goroutine of its own: one for each seek or edit, and one
// a frame interval while it plays, at the time the audio device has reached. onFrame runs on
// that goroutine after each new frame; the window takes it with takeFrame.
type player struct {
	onFrame func()

	mu   sync.Mutex
	wake *sync.Cond
	ctl  control

	position atomic.Uint64
	playing  atomic.Bool
	live     atomic.Pointer[liveClock]
	err      atomic.Pointer[string]

	frames frameBox
	done   chan struct{}
}

type control struct {
	project            *core.Project
	assets             *core.Assets
	previewW, previewH uint32
	redraw             bool
	seek               bool
	seekMs             float64
	play               bool
	quit               bool
}

// liveClock is the clock of the run of playback going on, and where the timeline ends.
type liveClock struct {
	clock      playback.Clock
	durationMs float64
}

// shownFrame is a composed preview frame and the output time it shows.
type shownFrame struct {
	img      *image.NRGBA
	outputMs float64
	// clockMs is what the playback clock read as the frame was composed; NaN while paused.
	clockMs float64
}

// newPlayer opens the session's video tracks and starts composing. It fails when the screen
// track cannot be opened; missing audio only costs the sound.
func newPlayer(s *core.Session, onFrame func()) (*player, error) {
	screen, err := core.OpenFeed(s.Tracks.Screen)
	if err != nil {
		return nil, err
	}
	var camera *core.Feed
	if s.Tracks.Camera != "" {
		if camera, err = core.OpenFeed(s.Tracks.Camera); err != nil {
			screen.Close()
			return nil, err
		}
	}
	p := &player{onFrame: onFrame, done: make(chan struct{})}
	p.wake = sync.NewCond(&p.mu)
	project := s.Project.Clone()
	p.ctl = control{project: &project, assets: s.Assets, previewW: 960, previewH: 540}
	e := &engine{p: p, screen: screen, camera: camera, srcW: s.SourceW, srcH: s.SourceH}
	tracks := s.Tracks
	go func() {
		defer close(p.done)
		defer screen.Close()
		if camera != nil {
			defer camera.Close()
		}
		audio, err := core.LoadAudioSources(tracks)
		if err != nil {
			p.setErr(fmt.Sprintf("audio unavailable: %v", err))
			audio = nil
		}
		e.audio = audio
		e.run()
	}()
	return p, nil
}

func (p *player) send(fn func(*control)) {
	p.mu.Lock()
	fn(&p.ctl)
	p.mu.Unlock()
	p.wake.Broadcast()
}

func (p *player) seek(outputMs float64) {
	p.position.Store(math.Float64bits(outputMs))
	p.send(func(c *control) { c.seek, c.seekMs = true, outputMs })
}

// play starts playback, from the start when the playhead is at the end.
func (p *player) play() {
	p.playing.Store(true)
	p.send(func(c *control) { c.play = true })
}

func (p *player) pause() {
	if l := p.live.Load(); l != nil {
		p.position.Store(math.Float64bits(min(l.clock.NowMS(), l.durationMs)))
	}
	p.playing.Store(false)
	p.live.Store(nil)
	p.send(func(c *control) { c.play = false })
}

func (p *player) setProject(project core.Project) {
	p.send(func(c *control) { c.project = &project })
}

func (p *player) setAssets(a *core.Assets) { p.send(func(c *control) { c.assets = a }) }

// setPreviewSize is the box, in pixels, frames are composed to fit.
func (p *player) setPreviewSize(w, h uint32) {
	p.send(func(c *control) {
		if c.previewW != w || c.previewH != h {
			c.previewW, c.previewH, c.redraw = w, h, true
		}
	})
}

// positionMs is the playhead in output milliseconds. While playing it reads the playback
// clock, so it runs ahead of the latest frame by however long that frame took.
func (p *player) positionMs() float64 {
	if l := p.live.Load(); l != nil {
		return min(l.clock.NowMS(), l.durationMs)
	}
	return math.Float64frombits(p.position.Load())
}

func (p *player) isPlaying() bool { return p.playing.Load() }

// clockMs is what the playback clock reads now and whether it follows the audio device; ok is
// false while paused.
func (p *player) clockMs() (ms float64, audio, ok bool) {
	l := p.live.Load()
	if l == nil {
		return 0, false, false
	}
	return l.clock.NowMS(), l.clock.FollowsAudio(), true
}

func (p *player) errText() string {
	if s := p.err.Load(); s != nil {
		return *s
	}
	return ""
}

func (p *player) setErr(s string) { p.err.Store(&s) }

func (p *player) takeFrame() (shownFrame, bool) { return p.frames.take() }

// close stops the player and waits for its goroutine, which closes the files and the audio
// device.
func (p *player) close() {
	p.send(func(c *control) { c.quit = true })
	<-p.done
}

// engine is the player goroutine's own state.
type engine struct {
	p            *player
	screen       *core.Feed
	camera       *core.Feed
	audio        *core.AudioSources
	srcW, srcH   uint32
	project      core.Project
	assets       *core.Assets
	pw, ph       uint32
	position     float64
	current      *playRun
	rendered     float64
	haveRendered bool
}

// playRun is one run of playback: its clock and the audio output feeding it, nil when no audio
// plays.
type playRun struct {
	clock playback.Clock
	out   *playback.AudioOutput
}

func (r *playRun) stop() {
	if r != nil && r.out != nil {
		r.out.Close()
	}
}

func (e *engine) startRun() *playRun {
	from := int64(math.Round(e.position))
	if e.audio == nil || !e.audio.Audible(e.project.Audio) {
		return &playRun{clock: playback.NewClock(from, nil)}
	}
	mixer := core.NewAudioMixer(e.audio, &e.project, float64(from))
	out, err := playback.StartAudioOutput(mixer.Render)
	if err != nil {
		e.p.setErr(fmt.Sprintf("audio output failed: %v", err))
		return &playRun{clock: playback.NewClock(from, nil)}
	}
	return &playRun{clock: playback.NewClock(from, out), out: out}
}

func (e *engine) run() {
	p := e.p
	nextDue := time.Now()
	defer func() { e.current.stop() }()
	for {
		p.mu.Lock()
		for {
			c := &p.ctl
			if c.quit {
				p.mu.Unlock()
				return
			}
			changed := c.project != nil || c.assets != nil || c.redraw || c.seek || c.play != (e.current != nil)
			if changed || (e.current == nil && !e.haveRendered) {
				break
			}
			if e.current == nil {
				p.wake.Wait()
				continue
			}
			now := time.Now()
			if !now.Before(nextDue) {
				break
			}
			// A short sleep keeps the next frame on time; waiting on the condition for the whole
			// interval would wake only on a command.
			p.mu.Unlock()
			time.Sleep(min(nextDue.Sub(now), 2*time.Millisecond))
			p.mu.Lock()
		}
		c := &p.ctl
		restartClock := false
		if c.project != nil {
			restartClock = !slices.Equal(c.project.Clips, e.project.Clips) || c.project.Audio != e.project.Audio
			e.project, c.project = *c.project, nil
			e.haveRendered = false
		}
		if c.assets != nil {
			e.assets, c.assets = c.assets, nil
			e.haveRendered = false
		}
		if c.redraw {
			c.redraw, e.haveRendered = false, false
		}
		if c.previewW > 0 && c.previewH > 0 {
			e.pw, e.ph = c.previewW, c.previewH
		}
		seek, seekMs := c.seek, c.seekMs
		c.seek = false
		play := c.play
		p.mu.Unlock()

		duration := e.project.DurationMs()
		if e.current != nil {
			e.position = e.current.clock.NowMS()
			if e.current.out != nil {
				if err := e.current.out.Err(); err != nil {
					p.setErr(fmt.Sprintf("audio output stopped: %v", err))
				}
			}
		}
		if seek {
			e.position, restartClock = seekMs, true
		}
		e.position = math.Max(0, math.Min(e.position, duration))
		start := false
		switch {
		case play && e.current == nil:
			if e.position >= duration-1 {
				e.position = 0
			}
			start = true
		case play:
			start = restartClock
		default:
			e.current.stop()
			e.current = nil
		}
		if start {
			e.current.stop()
			e.current = e.startRun()
		}
		if e.current != nil && e.position >= duration {
			e.current.stop()
			e.current = nil
			p.mu.Lock()
			p.ctl.play = false
			p.mu.Unlock()
		}
		p.position.Store(math.Float64bits(e.position))
		p.playing.Store(e.current != nil)
		if e.current != nil {
			p.live.Store(&liveClock{clock: e.current.clock, durationMs: duration})
		} else {
			p.live.Store(nil)
		}

		if e.assets == nil || (e.haveRendered && e.rendered == e.position) || e.pw == 0 {
			e.haveRendered = true
			continue
		}
		nextDue = time.Now().Add(frameInterval)
		clock := math.NaN()
		if e.current != nil {
			clock = e.position
		}
		img, err := e.render(e.position)
		switch {
		case err != nil:
			p.setErr(err.Error())
		default:
			p.frames.put(shownFrame{img: img, outputMs: e.position, clockMs: clock})
			if p.onFrame != nil {
				p.onFrame()
			}
		}
		e.rendered, e.haveRendered = e.position, true
	}
}

func (e *engine) render(outputMs float64) (*image.NRGBA, error) {
	src := e.project.SourceTime(outputMs)
	screen, err := e.screen.FrameAt(src)
	if err != nil {
		return nil, err
	}
	if screen == nil {
		return nil, errors.New("screen.mp4 has no frames")
	}
	var camera *image.RGBA
	if e.camera != nil {
		f, err := e.camera.FrameAt(src)
		if err != nil {
			return nil, err
		}
		if f != nil {
			camera = f.RGBA()
		}
	}
	ow, oh := e.project.OutputSize(e.srcW, e.srcH)
	w, h := fitSize(ow, oh, e.pw, e.ph)
	img := core.ComposeFrame(&e.project, e.assets, core.FrameInputs{Screen: screen.RGBA(), Camera: camera, OutputMs: outputMs}, w, h)
	premultiply(img)
	return img, nil
}

// premultiply turns a composed frame's straight alpha into the premultiplied alpha the GPU
// bitmap takes, in place. Frames are opaque but for rare edge pixels, so it mostly reads.
func premultiply(img *image.NRGBA) {
	px := img.Pix
	for i := 3; i < len(px); i += 4 {
		if a := uint32(px[i]); a != 255 {
			px[i-3] = uint8(uint32(px[i-3]) * a / 255)
			px[i-2] = uint8(uint32(px[i-2]) * a / 255)
			px[i-1] = uint8(uint32(px[i-1]) * a / 255)
		}
	}
}

// asRGBA views a premultiplied frame as the *image.RGBA a bitmap takes without a copy.
func asRGBA(img *image.NRGBA) *image.RGBA {
	return &image.RGBA{Pix: img.Pix, Stride: img.Stride, Rect: img.Rect}
}

// frameBox hands the newest frame from the player to the window. A frame replaced before the
// window took it goes straight back to the compositor.
type frameBox struct {
	mu   sync.Mutex
	next *shownFrame
}

func (b *frameBox) put(f shownFrame) {
	b.mu.Lock()
	old := b.next
	b.next = &f
	b.mu.Unlock()
	if old != nil {
		core.RecycleFrame(old.img)
	}
}

func (b *frameBox) take() (shownFrame, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.next == nil {
		return shownFrame{}, false
	}
	f := *b.next
	b.next = nil
	return f, true
}
