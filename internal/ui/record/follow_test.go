package record

import (
	"context"
	"errors"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/transcribe"
)

func TestTheLineBeingReadScrollsToTheTop(t *testing.T) {
	lines := []span{{0, 3}, {3, 6}, {6, 9}, {9, 12}}
	for _, c := range []struct {
		active, want int
		why          string
	}{
		{-1, 0, "nothing read yet"},
		{1, 0, "inside the first line"},
		{2, 1, "the last word of a line brings the next up"},
		{4, 1, "inside the second line"},
		{11, 2, "never past the last full page"},
	} {
		if got := topLine(lines, c.active, 2); got != c.want {
			t.Errorf("%s: word %d puts line %d first, want %d", c.why, c.active, got, c.want)
		}
	}
}

func TestTheTeleprompterListensOnlyWhenItHasAVoice(t *testing.T) {
	for _, c := range []struct {
		prompting, mic bool
		wav            string
		want           voice
	}{
		{true, false, "", noVoice},
		{true, true, "", micVoice},
		{true, false, "narration.wav", wavVoice},
		{true, true, "narration.wav", wavVoice},
		{false, true, "narration.wav", noVoice},
	} {
		if got := voiceFor(c.prompting, c.mic, c.wav); got != c.want {
			t.Errorf("prompting %v, mic %v, wav %q: voice %v, want %v", c.prompting, c.mic, c.wav, got, c.want)
		}
	}
}

// echo recognizes one more word of its script for each buffer pushed.
type echo struct {
	mu      sync.Mutex
	script  []string
	next    int
	pending []transcribe.Word
}

func (e *echo) Push([]float32) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.next < len(e.script) {
		e.pending = append(e.pending, transcribe.Word{Text: e.script[e.next]})
		e.next++
	}
}

func (e *echo) Poll() []transcribe.Word {
	e.mu.Lock()
	defer e.mu.Unlock()
	w := e.pending
	e.pending = nil
	return w
}

func (e *echo) Finish() []transcribe.Word { return e.Poll() }

func installed(path string) func(context.Context, func(uint64, uint64)) (string, error) {
	return func(context.Context, func(uint64, uint64)) (string, error) { return path, nil }
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestTheTeleprompterFollowsTheNarrationLineByLine(t *testing.T) {
	const script = "one two three four five six seven eight"
	audio := make(chan []float32, audioQueue)
	var loaded string
	sp := speech{model: installed("tiny.bin"), start: func(path string) (recognizer, error) {
		loaded = path
		return &echo{script: strings.Fields(script)}, nil
	}}
	f := startFollower(script, audio, sp)
	defer f.stop()
	pr := newPrompter(script, 2, f)
	pr.lines = []span{{0, 3}, {3, 6}, {6, 8}}
	waitFor(t, "the follower listens", func() bool { note, listening, _ := pr.status(); return listening && note == "Following your voice" })
	if loaded != "tiny.bin" {
		t.Fatalf("the recognizer loaded %q", loaded)
	}
	for range 3 {
		audio <- []float32{0}
	}
	waitFor(t, "three words are read", func() bool { pr.status(); return pr.current == 2 })
	if pr.first() != 1 {
		t.Fatalf("with the first line read, line %d is first; want 1", pr.first())
	}
	pr.scroll(-prompterLineH)
	if pr.first() != 0 {
		t.Fatalf("the wheel still moves back while following, first line %d", pr.first())
	}
	audio <- []float32{0}
	waitFor(t, "the fourth word is read", func() bool { pr.status(); return pr.current == 3 })
	if pr.first() != 1 {
		t.Fatalf("a new word brings the line being read back to the top, first line %d", pr.first())
	}
}

func TestWithoutTheModelTheTeleprompterStaysAScrollableScript(t *testing.T) {
	audio := make(chan []float32, 1)
	started := false
	sp := speech{
		model: func(context.Context, func(uint64, uint64)) (string, error) {
			return "", errors.New("no network")
		},
		start: func(string) (recognizer, error) { started = true; return nil, nil },
	}
	f := startFollower("one two three four five six", audio, sp)
	defer f.stop()
	pr := newPrompter("one two three four five six", 1, f)
	pr.lines = []span{{0, 2}, {2, 4}, {4, 6}}
	var why string
	waitFor(t, "following fails", func() bool { _, _, why = pr.status(); return why != "" })
	if note, listening, _ := pr.status(); listening || note != "Voice following is unavailable · scroll to move" {
		t.Fatalf("status %q, listening %v", note, listening)
	}
	if !strings.Contains(why, "no network") || started {
		t.Fatalf("the tooltip says %q; recognizer started %v", why, started)
	}
	for range 3 {
		select {
		case audio <- []float32{0}:
		default:
		}
	}
	pr.scroll(prompterLineH)
	if pr.status(); pr.current != -1 || pr.first() != 1 {
		t.Fatalf("a failed follower leaves the wheel in charge: word %d, first line %d", pr.current, pr.first())
	}
}

func TestWithoutAMicTheTeleprompterIsAStaticScript(t *testing.T) {
	pr := newPrompter("one two three", 1, nil)
	pr.lines = []span{{0, 1}, {1, 2}, {2, 3}}
	if note, listening, _ := pr.status(); listening || note != "Scroll to move through the script" {
		t.Fatalf("status %q, listening %v", note, listening)
	}
	pr.scroll(prompterLineH)
	if pr.first() != 1 {
		t.Fatalf("the wheel moves the script, first line %d", pr.first())
	}
}

func TestStoppingTheFollowerCancelsTheDownload(t *testing.T) {
	cancelled := make(chan struct{})
	sp := speech{model: func(ctx context.Context, progress func(uint64, uint64)) (string, error) {
		progress(10, 100)
		<-ctx.Done()
		close(cancelled)
		return "", ctx.Err()
	}}
	f := startFollower("one", make(chan []float32), sp)
	waitFor(t, "the download reports progress", func() bool { s, _ := f.state(); return s.done == 10 })
	if s, _ := f.state(); s.note() != "Downloading the speech model · 10%" {
		t.Fatalf("status %q", s.note())
	}
	f.stop()
	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("the download kept going after the teleprompter closed")
	}
	<-f.done
	if s, _ := f.state(); s.stage == followFailed {
		t.Fatal("a cancelled download is not a failure")
	}
}

func TestTheBubbleShowsTheCameraNewestFrameAndStopsTheCamera(t *testing.T) {
	stopped := make(chan struct{})
	defer func(old func(media.CameraID, func(*image.RGBA)) (func(), error)) { CameraPreview = old }(CameraPreview)
	var gotID media.CameraID
	CameraPreview = func(id media.CameraID, frame func(*image.RGBA)) (func(), error) {
		gotID = id
		quit := make(chan struct{})
		done := make(chan struct{})
		go func() {
			defer close(done)
			for i := 1; ; i++ {
				img := image.NewRGBA(image.Rect(0, 0, 4, 4))
				img.Set(0, 0, color.RGBA{uint8(i), 0, 0, 255})
				frame(img)
				select {
				case <-quit:
					return
				case <-time.After(time.Millisecond):
				}
			}
		}()
		return func() { close(quit); <-done; close(stopped) }, nil
	}
	feed, err := startCameraFeed(`\\?\usb#webcam`)
	if err != nil {
		t.Fatal(err)
	}
	if gotID != `\\?\usb#webcam` {
		t.Fatalf("previewed %q", gotID)
	}
	var redraws sync.WaitGroup
	redraws.Add(1)
	var once sync.Once
	feed.onFrame(func() { once.Do(redraws.Done) })
	redraws.Wait()
	first := feed.take()
	if first == nil {
		t.Fatal("a redraw came with no frame to paint")
	}
	waitFor(t, "a newer frame", func() bool {
		img := feed.take()
		return img != nil && img.RGBAAt(0, 0).R > first.RGBAAt(0, 0).R
	})
	feed.close()
	select {
	case <-stopped:
	default:
		t.Fatal("closing the bubble left the camera running")
	}
}

func TestABubbleForAMissingCameraFails(t *testing.T) {
	defer func(old func(media.CameraID, func(*image.RGBA)) (func(), error)) { CameraPreview = old }(CameraPreview)
	CameraPreview = func(media.CameraID, func(*image.RGBA)) (func(), error) {
		return nil, errors.New("no camera with id gone")
	}
	if _, err := startCameraFeed("gone"); err == nil {
		t.Fatal("a missing camera opened a bubble")
	}
}
