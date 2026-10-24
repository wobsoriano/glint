package studio

import (
	"context"
	"errors"
	"image"
	"image/gif"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media"
)

const testFrames = 60

// writeCountingScreen writes a 2 s, 30 fps screen.mp4 whose frame i is grey level 4*i, so a
// decoded frame says which one it is.
func writeCountingScreen(t *testing.T, path string) {
	t.Helper()
	w, err := media.CreateVideoWriter(path, media.VideoParams{Width: 320, Height: 180, FPS: 30, Bitrate: 2_000_000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for i := range testFrames {
		for k := range img.Pix {
			img.Pix[k] = uint8(4 * i)
		}
		if err := w.WriteFrame(img); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Finish(); err != nil {
		t.Fatal(err)
	}
}

func frameIndex(f *SourceFrame) int {
	px := f.RGBA().Pix
	return int(math.Round(float64(px[len(px)/2]) / 4))
}

func TestFeedServesFramesBySourceTime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "screen.mp4")
	writeCountingScreen(t, path)
	feed, err := OpenFeed(path)
	if err != nil {
		t.Fatal(err)
	}
	defer feed.Close()
	for _, c := range []struct {
		ms   float64
		want int
	}{
		{0, 0}, {16, 0}, {17.5, 1}, {100, 3}, {500, 15},
		{100, 3},    // back: seeks
		{1_900, 57}, // more than 1.5 s ahead: seeks
		{1_910, 57}, {3_000, testFrames - 1},
	} {
		f, err := feed.FrameAt(c.ms)
		if err != nil {
			t.Fatal(err)
		}
		if got := frameIndex(f); got != c.want {
			t.Errorf("frame at %v ms is %d, want %d", c.ms, got, c.want)
		}
	}
}

func testSession(t *testing.T) *Session {
	t.Helper()
	dir := t.TempDir()
	writeCountingScreen(t, filepath.Join(dir, "screen.mp4"))
	wav, err := media.CreateWAV(filepath.Join(dir, "system.wav"), SampleRate, 2)
	if err != nil {
		t.Fatal(err)
	}
	tone := make([]float32, 2*SampleRate*2)
	for i := range tone {
		tone[i] = float32(0.5 * math.Sin(2*math.Pi*440*float64(i/2)/SampleRate))
	}
	if err := wav.Write(tone); err != nil {
		t.Fatal(err)
	}
	if _, err := wav.Finish(); err != nil {
		t.Fatal(err)
	}
	s, err := LoadSession(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Project.Clips = []Clip{{0, 1_000, 1}, {1_000, 2_000, 2}}
	return s
}

func countFrames(t *testing.T, path string) int {
	t.Helper()
	r, err := media.OpenFrameReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	n := 0
	for {
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			return n
		}
		if err != nil {
			t.Fatal(err)
		}
		f.Release()
		n++
	}
}

func TestExportRendersTheEditWithAudio(t *testing.T) {
	s := testSession(t)
	opts := ExportOptions{Fps: Fps30, MotionBlur: true, Resolution: ResSource, Quality: QualityLow, Codec: CodecH264, Format: FormatMp4}
	var last float64
	out, err := Export(context.Background(), s, opts, func(p float64) { last = p })
	if err != nil {
		t.Fatal(err)
	}
	eq(t, last, 1.0)
	eq(t, countFrames(t, out), 45)
	audio, err := media.ReadMP4Audio(out)
	if err != nil {
		t.Fatal(err)
	}
	if d := audio.DurationMS(); d < 1_490 || d > 1_560 {
		t.Errorf("audio lasts %d ms, want 1500", d)
	}
	started := time.Now()
	again, err := Export(context.Background(), s, opts, nil)
	if err != nil || again != out || time.Since(started) > 100*time.Millisecond {
		t.Errorf("second export: %s %v after %v, want the cached %s", again, err, time.Since(started), out)
	}
}

func TestExportWritesAGIF(t *testing.T) {
	s := testSession(t)
	opts := DefaultExportOptions()
	opts.Format = FormatGif
	out, err := Export(context.Background(), s, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	g, err := gif.DecodeAll(f)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(g.Image), 23)
	eq(t, g.Delay[:3], []int{7, 7, 6})
	eq(t, g.Image[0].Rect.Dy(), gifMaxShortSide)
}

func TestCancelledExportLeavesNothingBehind(t *testing.T) {
	s := testSession(t)
	ctx, cancel := context.WithCancel(context.Background())
	var at time.Time
	_, err := Export(ctx, s, DefaultExportOptions(), func(p float64) {
		if p >= 0.3 && at.IsZero() {
			at = time.Now()
			cancel()
		}
	})
	if !errors.Is(err, ErrCancelled) || !errors.Is(err, context.Canceled) {
		t.Fatalf("export returned %v", err)
	}
	if d := time.Since(at); d > 500*time.Millisecond {
		t.Errorf("stopped %v after cancel", d)
	}
	entries, _ := os.ReadDir(s.RenderDir())
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "export-") {
			t.Errorf("render/ still holds %s", e.Name())
		}
	}
}
