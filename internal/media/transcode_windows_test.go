package media

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestTranscodeTrimsScalesAndLowersBitrate(t *testing.T) {
	needFFmpeg(t)
	src := writeFixture(t, "1080p30.mp4", 1920, 1080, 30, 120, true, false)
	dst := filepath.Join(outDir(t), "trimmed-720p.mp4")
	var progress []float64
	sum, err := Transcode(context.Background(), src.path, dst, TranscodeOptions{
		Width: 1280, Height: 720, Bitrate: 2_000_000, Trim: &Span{StartMS: 1000, EndMS: 3000},
	}, func(p float64) { progress = append(progress, p) })
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("summary: %+v", sum)
	if sum.Frames != 60 || sum.DurationMS != 2000 {
		t.Errorf("wrote %d frames, %d ms; want 60 frames, 2000 ms", sum.Frames, sum.DurationMS)
	}
	for i := 1; i < len(progress); i++ {
		if progress[i] < progress[i-1] {
			t.Fatalf("progress went back from %g to %g", progress[i-1], progress[i])
		}
	}
	if len(progress) == 0 || progress[len(progress)-1] != 1 {
		t.Errorf("progress did not end at 1: %v", progress[max(len(progress)-3, 0):])
	}

	before, after := ffprobe(t, src.path), ffprobe(t, dst)
	t.Logf("source ffprobe: %s", before.summary())
	t.Logf("output ffprobe: %s", after.summary())
	v := after.Streams[after.stream("video")]
	if v.CodecName != "h264" || v.Width != 1280 || v.Height != 720 || v.RFrameRate != "30/1" {
		t.Errorf("output video is %s %dx%d at %s", v.CodecName, v.Width, v.Height, v.RFrameRate)
	}
	if math.Abs(seconds(v.Duration)-2) > 0.04 {
		t.Errorf("output video lasts %s s, want 2", v.Duration)
	}
	if seconds(v.BitRate) >= seconds(before.Streams[before.stream("video")].BitRate)/2 {
		t.Errorf("output bitrate %s is not well below the source's", v.BitRate)
	}
	if v.ColorSpace != "bt709" || v.ColorRange != "tv" {
		t.Errorf("output colour space=%s range=%s", v.ColorSpace, v.ColorRange)
	}
	a := after.stream("audio")
	if a < 0 {
		t.Fatal("output lost the audio track")
	}
	if d := seconds(after.Streams[a].Duration); math.Abs(d-2) > 0.05 {
		t.Errorf("output audio lasts %g s, want 2", d)
	}

	r, err := OpenVideoReader(dst)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, k := range []int{0, 59} {
		if err := r.SeekMS(int64(k) * 1000 / 30); err != nil {
			t.Fatal(err)
		}
		f, err := r.NextFrame()
		if err != nil {
			t.Fatal(err)
		}
		right := psnr(Shrink(pattern(30+k, 1920, 1080), 1280, 720), f.Image)
		early := psnr(Shrink(pattern(29+k, 1920, 1080), 1280, 720), f.Image)
		t.Logf("output frame %d: PSNR %.2f dB against source frame %d, %.2f against frame %d", k, right, 30+k, early, 29+k)
		if right < 30 || right < early+3 {
			t.Errorf("output frame %d does not show source frame %d", k, 30+k)
		}
	}

	audio, err := ReadMP4Audio(dst)
	if err != nil {
		t.Fatal(err)
	}
	// One second holds whole periods of the tone, so the trimmed track starts in phase with
	// sample 0; this catches sub-millisecond slips, not whole-period ones.
	lag, snr := alignTone(channel(audio.Samples, 2, 0)[:toneRate])
	t.Logf("output audio: %d frames; vs the source tone from 1 s: lag %d samples, SNR %.1f dB", audio.Frames(), lag, snr)
	if lag != 0 || snr < 25 {
		t.Errorf("trimmed audio is %d samples off the source with SNR %.1f dB", lag, snr)
	}
}

func TestTranscodeCancelRemovesThePartialFile(t *testing.T) {
	src := writeFixture(t, "1080p30.mp4", 1920, 1080, 30, 120, true, false)
	dst := filepath.Join(t.TempDir(), "cancelled.mp4")
	ctx, cancel := context.WithCancel(context.Background())
	_, err := Transcode(ctx, src.path, dst, TranscodeOptions{}, func(p float64) {
		if p > 0.2 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Transcode after cancel: %v", err)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("partial file is still there: %v", err)
	}
}

func TestTranscodeRejectsAnEmptyTrim(t *testing.T) {
	src := writeFixture(t, "1080p30.mp4", 1920, 1080, 30, 120, true, false)
	_, err := Transcode(context.Background(), src.path, filepath.Join(t.TempDir(), "x.mp4"),
		TranscodeOptions{Trim: &Span{StartMS: 2000, EndMS: 2000}}, nil)
	if err == nil {
		t.Fatal("empty trim range was accepted")
	}
}
