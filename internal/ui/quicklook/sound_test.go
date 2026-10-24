package quicklook

import (
	"errors"
	"io"
	"math"
	"path/filepath"
	"testing"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/store"
)

func writeWAV(t *testing.T, path string, rate, channels int, samples []float32) {
	t.Helper()
	w, err := media.CreateWAV(path, rate, channels)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(samples); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Finish(); err != nil {
		t.Fatal(err)
	}
}

func readAll(t *testing.T, src interface {
	NextChunk([]float32) ([]float32, error)
}) []float32 {
	t.Helper()
	var out []float32
	for {
		var err error
		out, err = src.NextChunk(out)
		if errors.Is(err, io.EOF) {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

// A recording's sound is its system audio and its microphone together: a stereo system.wav
// and a mono mic.wav, as the recorder writes them, play as one stereo track at 48 kHz.
func TestRecordingSoundMixesSystemAndMic(t *testing.T) {
	dir := t.TempDir()
	const frames = 48_000 / 2
	sys := make([]float32, 0, frames*2)
	mic := make([]float32, 0, frames)
	for range frames {
		sys = append(sys, 0.25, -0.25)
		mic = append(mic, 0.5)
	}
	writeWAV(t, filepath.Join(dir, "system.wav"), 48_000, 2, sys)
	writeWAV(t, filepath.Join(dir, "mic.wav"), 48_000, 1, mic)

	src, closeSound, err := openSound(store.Item{Dir: dir, Meta: store.Meta{Kind: store.Recording}})
	if err != nil || src == nil {
		t.Fatalf("openSound: %v, %v", src, err)
	}
	defer closeSound()
	if src.SampleRate() != 48_000 || src.Channels() != 2 {
		t.Fatalf("format %d Hz, %d channels", src.SampleRate(), src.Channels())
	}
	got := readAll(t, src)
	if n := len(got) / 2; math.Abs(float64(n-frames)) > 2 {
		t.Fatalf("%d frames, want about %d", n, frames)
	}
	mid := len(got) / 2 &^ 1
	if l, r := got[mid], got[mid+1]; math.Abs(float64(l-0.75)) > 1e-4 || math.Abs(float64(r-0.25)) > 1e-4 {
		t.Fatalf("mixed frame (%v, %v), want (0.75, 0.25)", l, r)
	}

	if err := src.SeekMS(250); err != nil {
		t.Fatal(err)
	}
	if rest := readAll(t, src); math.Abs(float64(len(rest)/2-frames/2)) > 2 {
		t.Fatalf("after seeking to 250 ms, %d frames are left, want about %d", len(rest)/2, frames/2)
	}
}

func TestMixClipsAndResamples(t *testing.T) {
	loud := &media.WavData{SampleRate: 24_000, Channels: 1, Samples: []float32{0.8, 0.8, 0.8, 0.8}}
	p := mix([]*media.WavData{loud, loud})
	if len(p.samples) < 12 {
		t.Fatalf("%d samples from 4 frames at 24 kHz, want about 16 at 48 kHz stereo", len(p.samples))
	}
	for i, v := range p.samples {
		if v != 1 {
			t.Fatalf("sample %d is %v, want the sum clipped to 1", i, v)
		}
	}
}

func TestARecordingWithoutSoundIsSilent(t *testing.T) {
	src, _, err := openSound(store.Item{Dir: t.TempDir(), Meta: store.Meta{Kind: store.Recording}})
	if src != nil || err != nil {
		t.Fatalf("openSound of a recording with no audio: %v, %v", src, err)
	}
}
