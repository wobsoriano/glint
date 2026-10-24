package media

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/platform"
)

// Recorder is a running recording. Every track follows one media clock, so pausing stops
// them all and the files line up at media time 0.
type Recorder struct {
	clock    *capture.Clock
	dir      string
	screen   *screenTrack
	system   *capture.Track
	mic      *capture.Track
	camera   *cameraTrack
	input    *input.Track
	width    uint32
	height   uint32
	warnings []string

	stopOnce sync.Once
	summary  RecordSummary
}

// StartRecorder starts every track, then media time. The screen master is required; a
// missing or failing optional device becomes a warning and the recording goes on without it.
func StartRecorder(o RecordOptions) (*Recorder, error) {
	if o.FPS != FPS30 && o.FPS != FPS60 {
		return nil, fmt.Errorf("record at 30 or 60 fps, not %d", o.FPS)
	}
	if err := os.MkdirAll(o.SessionDir, 0o755); err != nil {
		return nil, err
	}
	res, err := resolveSource(o.Source)
	if err != nil {
		return nil, err
	}
	clock := capture.NewClock()
	screen, geo, err := startScreen(res.screen, o.FPS, filepath.Join(o.SessionDir, "screen.mp4"), clock)
	if err != nil {
		return nil, err
	}
	r := &Recorder{
		clock: clock, dir: o.SessionDir, screen: screen,
		width: uint32(geo.crop.Dx()), height: uint32(geo.crop.Dy()),
		warnings: append([]string(nil), screen.warnings...),
	}
	if o.RecordInput {
		r.input = startOptional(&r.warnings, "input", func() (*input.Track, error) {
			return input.Start(input.Options{
				Map:         pointerMap(res, geo),
				Size:        [2]uint32{r.width, r.height},
				Scale:       res.scale,
				SessionDir:  o.SessionDir,
				Clock:       clock,
				IgnoredKeys: o.IgnoredKeys,
			})
		})
	}
	if o.SystemAudio {
		r.system = startOptional(&r.warnings, "system audio", func() (*capture.Track, error) {
			return capture.StartTrack(capture.Loopback, filepath.Join(o.SessionDir, "system.wav"), 2, clock, nil)
		})
	}
	if o.Mic != nil {
		r.mic = startOptional(&r.warnings, "microphone", func() (*capture.Track, error) {
			return capture.StartTrack(capture.Microphone(*o.Mic), filepath.Join(o.SessionDir, "mic.wav"), 1, clock, o.MicTap)
		})
	}
	if o.Camera != nil {
		r.camera = startOptional(&r.warnings, "camera", func() (*cameraTrack, error) {
			return startCamera(*o.Camera, filepath.Join(o.SessionDir, "camera.mp4"), clock)
		})
	}
	clock.Resume()
	return r, nil
}

// pointerMap maps desktop pixels onto the exact region the screen master shows: the crop's
// top-left on its display, or the matching rectangle of the window wherever it is now.
func pointerMap(res resolvedSource, geo screenGeometry) input.PointerMap {
	if res.screen.window != 0 {
		return input.WindowOrigin(int64(res.screen.window), geo.bounds)
	}
	o := res.origin.Add(geo.crop.Min)
	return input.FixedOrigin(o.X, o.Y)
}

func startOptional[T any](warnings *[]string, what string, start func() (*T, error)) *T {
	t, err := start()
	if err != nil {
		*warnings = append(*warnings, fmt.Sprintf("%s not recorded: %v", what, err))
		return nil
	}
	return t
}

// resolvedSource is a Source looked up on the desktop.
type resolvedSource struct {
	screen screenSource
	// origin is the desktop position of a monitor item's top-left, in physical pixels.
	origin image.Point
	// scale is the display scale cursor sprites are drawn at.
	scale float32
}

func resolveSource(s Source) (resolvedSource, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer physicalPixels()()
	displays, err := platform.Displays()
	if err != nil {
		return resolvedSource{}, err
	}
	find := func(id int64) (platform.Display, error) {
		for _, d := range displays {
			if d.ID == id {
				return d, nil
			}
		}
		return platform.Display{}, fmt.Errorf("display %#x does not exist (%d attached)", id, len(displays))
	}
	switch s := s.(type) {
	case DisplaySource:
		d, err := find(s.Display)
		if err != nil {
			return resolvedSource{}, err
		}
		return resolvedSource{screen: screenSource{monitor: uintptr(d.ID)}, origin: image.Pt(d.Bounds.X, d.Bounds.Y), scale: float32(d.Scale)}, nil
	case AreaSource:
		d, err := find(s.Display)
		if err != nil {
			return resolvedSource{}, err
		}
		area := areaInDisplay(s.Rect, d.Bounds)
		return resolvedSource{screen: screenSource{monitor: uintptr(d.ID), area: &area}, origin: image.Pt(d.Bounds.X, d.Bounds.Y), scale: float32(d.Scale)}, nil
	case WindowSource:
		hwnd := uintptr(s.Window)
		if ok, _, _ := procIsWindow.Call(hwnd); ok == 0 {
			return resolvedSource{}, fmt.Errorf("no window %#x", s.Window)
		}
		const monitorDefaultToNearest = 2
		monitor, _, _ := procMonitorFromWindow.Call(hwnd, monitorDefaultToNearest)
		scale := float32(1)
		if d, err := find(int64(monitor)); err == nil {
			scale = float32(d.Scale)
		}
		return resolvedSource{screen: screenSource{window: hwnd}, scale: scale}, nil
	}
	return resolvedSource{}, errors.New("no capture source")
}

func (r *Recorder) Pause()             { r.clock.Pause() }
func (r *Recorder) Resume()            { r.clock.Resume() }
func (r *Recorder) IsPaused() bool     { return r.clock.IsPaused() }
func (r *Recorder) SessionDir() string { return r.dir }

// Elapsed is the media time recorded so far.
func (r *Recorder) Elapsed() time.Duration { return r.clock.Now() }

// MicLevel is the RMS of the last microphone buffer, when a microphone is recording.
func (r *Recorder) MicLevel() (float32, bool) {
	if r.mic == nil {
		return 0, false
	}
	return r.mic.Level(), true
}

// Warnings name the optional tracks that failed to start, so the UI can say so while the
// recording continues.
func (r *Recorder) Warnings() []string { return r.warnings }

// IsLive is false once the screen capture has ended on its own, because its window closed
// or it failed. Stop still finalizes what was written and says why in its warnings.
func (r *Recorder) IsLive() bool { return r.screen.live() }

// Stop finalizes every file and returns what was written. A track that failed, the screen
// master included, keeps what it wrote before failing and is reported in Warnings.
// Calling it again returns the same summary.
func (r *Recorder) Stop() RecordSummary {
	r.stopOnce.Do(func() { r.summary = r.stop() })
	return r.summary
}

func (r *Recorder) stop() RecordSummary {
	r.clock.Pause()
	s := RecordSummary{DurationMS: r.clock.NowMS(), Width: r.width, Height: r.height, Warnings: r.warnings}
	var files []string
	if r.input != nil {
		tl, err := r.input.Stop()
		if err == nil {
			err = tl.Save(r.dir)
		}
		if err != nil {
			s.Warnings = append(s.Warnings, fmt.Sprintf("%s: %v", input.File, err))
		} else {
			files = append(files, filepath.Join(r.dir, input.File))
			for _, c := range tl.Cursors {
				files = append(files, filepath.Join(r.dir, filepath.FromSlash(c.File)))
			}
		}
	}
	for _, t := range []*capture.Track{r.system, r.mic} {
		if t == nil {
			continue
		}
		if _, err := t.Stop(); err != nil {
			s.Warnings = append(s.Warnings, fmt.Sprintf("%s: %v", t.Path, err))
		} else {
			files = append(files, t.Path)
		}
	}
	if r.camera != nil {
		if err := r.camera.finish(); err != nil {
			s.Warnings = append(s.Warnings, fmt.Sprintf("%s: %v", r.camera.path, err))
		} else {
			files = append(files, r.camera.path)
		}
	}
	stats, err := r.screen.finish()
	s.Screen, s.Hardware = stats, r.screen.hardware
	switch {
	case err != nil:
		s.Warnings = append(s.Warnings, fmt.Sprintf("the screen capture failed; the recording ends there: %v", err))
	case r.screen.closed:
		s.Warnings = append(s.Warnings, "the captured window closed; the recording ends there")
	}
	s.Files = append([]string{r.screen.path}, files...)
	for _, f := range s.Files {
		if st, err := os.Stat(f); err == nil {
			s.Bytes += uint64(st.Size())
		}
	}
	return s
}

// Cancel stops and deletes everything this recording wrote, including the session
// directory when it is empty afterwards.
func (r *Recorder) Cancel() {
	for _, f := range r.Stop().Files {
		os.Remove(f)
	}
	os.Remove(filepath.Join(r.dir, "cursors"))
	os.Remove(r.dir)
}
