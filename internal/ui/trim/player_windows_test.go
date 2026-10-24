package trim

import (
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/ui/trim/internal/testclip"
)

var clipDir string

func TestMain(m *testing.M) {
	var err error
	if clipDir, err = os.MkdirTemp("", "glint-trim-test"); err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(clipDir)
	os.Exit(code)
}

var clip struct {
	once sync.Once
	path string
	err  error
}

// testClip is a 4 s 1920x1080 clip at 30 fps, written once per test run.
func testClip(t *testing.T) string {
	clip.once.Do(func() {
		clip.path = filepath.Join(clipDir, "clip.mp4")
		clip.err = testclip.Write(clip.path, 1920, 1080, 30, 4)
	})
	if clip.err != nil {
		t.Fatal(clip.err)
	}
	return clip.path
}

// collect gathers frames as the player publishes them, with the clock reading at each.
type collect struct {
	p     *Player
	mu    sync.Mutex
	got   []Frame
	drift []float64
	audio bool
}

func (c *collect) onFrame() {
	f, ok := c.p.TakeFrame()
	if !ok {
		return
	}
	now, audio, playing := c.p.ClockMS()
	c.mu.Lock()
	c.got = append(c.got, f)
	if playing {
		c.drift = append(c.drift, float64(f.TMS)-now)
		c.audio = audio
	}
	c.mu.Unlock()
}

func (c *collect) frames() []Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]Frame(nil), c.got...)
}

func newCollect(t *testing.T) *collect {
	c := &collect{}
	c.p = NewPlayer(testClip(t), c.onFrame)
	t.Cleanup(c.p.Close)
	return c
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func TestSeekShowsTheFrameAtThatTimeShrunkForThePreview(t *testing.T) {
	c := newCollect(t)
	waitFor(t, "the first frame", func() bool { return len(c.frames()) >= 1 })
	c.p.SeekMS(2_000)
	waitFor(t, "the frame at 2 s", func() bool { return len(c.frames()) >= 2 })
	f := c.frames()[1]
	n, ok := testclip.FrameNumber(f.Image, f.Image.Rect)
	if f.TMS != 2_000 || !ok || n != 60 {
		t.Fatalf("seeking to 2 s showed the frame at %d ms whose code reads %d (%v)", f.TMS, n, ok)
	}
	if w, h := f.Image.Rect.Dx(), f.Image.Rect.Dy(); w != 1280 || h != 720 {
		t.Fatalf("the preview frame is %dx%d, want 1280x720", w, h)
	}
	if got := c.p.Audio(); got != AudioPresent {
		t.Fatalf("the clip's audio track reads as %d", got)
	}
}

func TestMutedPlayFollowsTheWallClockAndStopsAtTheEnd(t *testing.T) {
	c := newCollect(t)
	waitFor(t, "the first frame", func() bool { return len(c.frames()) >= 1 })
	c.p.SeekMS(1_000)
	waitFor(t, "the seek", func() bool { return len(c.frames()) >= 2 })
	began := time.Now()
	c.p.Play(2_000, true)
	waitFor(t, "the end of the play", func() bool { return !c.p.Playing() })
	took := time.Since(began)
	frames := c.frames()[2:]
	if len(frames) < 25 || frames[len(frames)-1].TMS < 1_900 || frames[len(frames)-1].TMS >= 2_000 {
		t.Fatalf("played %d frames, the last at %d ms", len(frames), frames[len(frames)-1].TMS)
	}
	if took < 900*time.Millisecond || took > 1300*time.Millisecond {
		t.Fatalf("a second of video played in %v", took)
	}
	if c.p.PositionMS() != 2_000 {
		t.Fatalf("the position after the play is %d", c.p.PositionMS())
	}
}

// TestPlayKeepsFramesOnTheAudioClock plays through the speakers: run it inside the desktop lock.
func TestPlayKeepsFramesOnTheAudioClock(t *testing.T) {
	if testing.Short() {
		t.Skip("plays the clip's sound")
	}
	c := newCollect(t)
	waitFor(t, "the first frame", func() bool { return len(c.frames()) >= 1 })
	c.p.Play(3_500, false)
	waitFor(t, "the end of the play", func() bool { return !c.p.Playing() })
	if err := c.p.Err(); err != "" {
		t.Fatal(err)
	}
	c.mu.Lock()
	drift, audio := c.drift, c.audio
	c.mu.Unlock()
	worst := 0.0
	for _, d := range drift {
		worst = max(worst, math.Abs(d))
	}
	t.Logf("%d frames on the audio clock (%v), worst |frame time - clock| %.1f ms", len(drift), audio, worst)
	if !audio || len(drift) < 90 || worst > 50 {
		t.Fatalf("frames %d, audio clock %v, worst drift %.1f ms", len(drift), audio, worst)
	}
}
