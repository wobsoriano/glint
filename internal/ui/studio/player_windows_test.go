package studio

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/studio/internal/synth"
)

var (
	sessionOnce sync.Once
	sessionDir  string
	sessionErr  error
)

// testSession copies a synthetic 1280x720 30 fps session with tone bursts into a temp dir.
func testSession(t *testing.T) string {
	t.Helper()
	sessionOnce.Do(func() {
		sessionDir = filepath.Join(os.TempDir(), "glint-studio-ui-test-session")
		if _, err := os.Stat(filepath.Join(sessionDir, "input.json")); err == nil {
			return
		}
		os.RemoveAll(sessionDir)
		_, sessionErr = synth.Write(sessionDir, synth.Options{Width: 1280, Height: 720, FPS: 30})
	})
	if sessionErr != nil {
		t.Fatal(sessionErr)
	}
	dir := t.TempDir()
	for _, name := range []string{"screen.mp4", "system.wav", "input.json", "cursors/0.png"} {
		data, err := os.ReadFile(filepath.Join(sessionDir, name))
		if err != nil {
			t.Fatal(err)
		}
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func silentSession(t *testing.T) *core.Session {
	t.Helper()
	s, err := core.LoadSession(testSession(t))
	if err != nil {
		t.Fatal(err)
	}
	s.Project.Audio.System.Enabled = false
	return s
}

func waitFrame(t *testing.T, p *player, frames <-chan struct{}, want func(shownFrame) bool) shownFrame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-frames:
			if f, ok := p.takeFrame(); ok {
				if want(f) {
					return f
				}
				core.RecycleFrame(f.img)
			}
		case <-deadline:
			t.Fatal("no matching frame in 5 s; player error: " + p.errText())
		}
	}
}

func TestPlayerComposesTheFrameAtEachSeekAndPlaysOnTheClock(t *testing.T) {
	s := silentSession(t)
	frames := make(chan struct{}, 64)
	p, err := newPlayer(s, func() {
		select {
		case frames <- struct{}{}:
		default:
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	p.setPreviewSize(640, 640)
	first := waitFrame(t, p, frames, func(f shownFrame) bool { return f.img.Rect.Dx() == 640 })
	ow, oh := s.Project.OutputSize(s.SourceW, s.SourceH)
	ww, wh := fitSize(ow, oh, 640, 640)
	if w, h := first.img.Rect.Dx(), first.img.Rect.Dy(); w != int(ww) || h != int(wh) {
		t.Errorf("preview frame %dx%d, want %dx%d", w, h, ww, wh)
	}
	for _, at := range []float64{7_000, 2_500, 15_000} {
		p.seek(at)
		f := waitFrame(t, p, frames, func(f shownFrame) bool { return f.outputMs == at })
		core.RecycleFrame(f.img)
		if got := p.positionMs(); got != at {
			t.Errorf("position after seek %v, want %v", got, at)
		}
	}

	p.seek(3_000)
	p.play()
	var times []float64
	var composeEnd []time.Time
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		f := waitFrame(t, p, frames, func(f shownFrame) bool { return !math.IsNaN(f.clockMs) })
		times = append(times, f.outputMs)
		composeEnd = append(composeEnd, time.Now())
		core.RecycleFrame(f.img)
	}
	p.pause()
	if !p.isPlaying() && len(times) < 90 {
		t.Errorf("%d frames in 2 s of playback, want about 120", len(times))
	}
	span := times[len(times)-1] - times[0]
	wall := float64(composeEnd[len(composeEnd)-1].Sub(composeEnd[0]).Milliseconds())
	if math.Abs(span-wall) > 60 {
		t.Errorf("frames span %.0f ms of output over %.0f ms of wall time", span, wall)
	}
	t.Logf("%d frames in 2 s, output %.0f..%.0f ms", len(times), times[0], times[len(times)-1])
}
