package media

import (
	"io"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReaderSeeksToTheRightFrame(t *testing.T) {
	needFFmpeg(t)
	f := writeFixture(t, "1080p30.mp4", 1920, 1080, 30, 120, true, false)
	r, err := OpenFrameReader(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	t.Logf("opened %dx%d %.3f fps %d ms, GPU device %v", r.width, r.height, r.FPS(), r.DurationMS(), r.Hardware())
	defer func() { t.Logf("%d frames came from GPU textures", r.gpuFrames) }()
	lat := newStats()
	for _, k := range []int{70, 0, 45, 119, 30, 15, 89, 1} {
		target := int64(k) * 1000 / int64(f.fps)
		start := time.Now()
		if err := r.SeekMS(target); err != nil {
			t.Fatal(err)
		}
		frame, err := r.NextFrame()
		took := time.Since(start)
		if err != nil {
			t.Fatalf("seek to %d ms: %v", target, err)
		}
		lat.add(float64(took.Microseconds()) / 1000)
		if frame.TMS != target {
			t.Errorf("seek to frame %d (%d ms) gave the frame at %d ms", k, target, frame.TMS)
		}
		img := frame.ToRGBA(nil)
		right := psnr(pattern(k, f.w, f.h), img)
		neighbour := psnr(pattern(min(k+1, f.frames-1), f.w, f.h), img)
		if k+1 < f.frames && right < neighbour+3 {
			t.Errorf("frame %d: PSNR %.2f dB against itself, %.2f against the next frame", k, right, neighbour)
		}
		ff := ffmpegFrames(t, f.path, float64(frame.TMS)/1000, 1, "nv12")
		ours := packedNV12(frame)
		luma := f.w * f.h
		yp, cp := planePSNR(ours[:luma], ff[:luma]), planePSNR(ours[luma:], ff[luma:])
		t.Logf("frame %3d: %3d ms, seek+decode %6.1f ms, PSNR vs source %.2f dB, vs ffmpeg NV12 Y %.1f dB chroma %.1f dB",
			k, frame.TMS, float64(took.Microseconds())/1000, right, yp, cp)
		if yp < 50 || cp < 50 {
			t.Errorf("frame %d differs from ffmpeg's decode: Y %.1f dB, chroma %.1f dB", k, yp, cp)
		}
		frame.Release()
	}
	t.Logf("seek latency: min %.1f ms, mean %.1f ms, max %.1f ms over 8 seeks (keyframe every 30 frames)", lat.min, lat.sum/8, lat.max)
}

func TestReaderPlaysToTheEndAfterSeeking(t *testing.T) {
	f := writeFixture(t, "1080p30.mp4", 1920, 1080, 30, 120, true, false)
	r, err := OpenFrameReader(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.SeekMS(3500); err != nil {
		t.Fatal(err)
	}
	n := 0
	for {
		frame, err := r.NextFrame()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		frame.Release()
		n++
	}
	if n != 15 {
		t.Fatalf("decoded %d frames from 3.5 s to the end, want 15", n)
	}
}

// TestReaderOnLibraryRecording decodes a copy of a screen recording from the real Library,
// which the Rust build wrote, and compares it with ffmpeg at several seek points.
func TestReaderOnLibraryRecording(t *testing.T) {
	needFFmpeg(t)
	matches, _ := filepath.Glob(filepath.Join(os.Getenv("APPDATA"), "Glint", "Library", "*.glintrec", "screen.mp4"))
	if len(matches) == 0 {
		t.Skip("no recordings in the Library")
	}
	for _, src := range matches[:min(len(matches), 2)] {
		path := filepath.Join(t.TempDir(), "screen.mp4")
		copyFile(t, src, path)
		p := ffprobe(t, path)
		t.Logf("%s: %s", filepath.Base(filepath.Dir(src)), p.summary())
		r, err := OpenFrameReader(path)
		if err != nil {
			t.Fatal(err)
		}
		w, h := r.Size()
		t.Logf("reader: %dx%d %.3f fps %d ms", w, h, r.FPS(), r.DurationMS())
		for _, frac := range []float64{0, 0.5, 0.9, 0.25} {
			target := int64(float64(r.DurationMS()) * frac)
			start := time.Now()
			if err := r.SeekMS(target); err != nil {
				t.Fatal(err)
			}
			frame, err := r.NextFrame()
			took := time.Since(start)
			if err != nil {
				t.Fatalf("seek to %d ms: %v", target, err)
			}
			ff := ffmpegFrames(t, path, float64(frame.TMS)/1000, 1, "nv12")
			ours := packedNV12(frame)
			if len(ff) != len(ours) {
				t.Fatalf("ffmpeg frame is %d bytes, ours %d", len(ff), len(ours))
			}
			yp, cp := planePSNR(ours[:w*h], ff[:w*h]), planePSNR(ours[w*h:], ff[w*h:])
			t.Logf("  seek %5d ms -> frame at %5d ms in %6.1f ms; vs ffmpeg NV12 Y %.1f dB chroma %.1f dB", target, frame.TMS, float64(took.Microseconds())/1000, yp, cp)
			if yp < 50 || cp < 50 {
				t.Errorf("frame at %d ms differs from ffmpeg's decode", frame.TMS)
			}
			frame.Release()
		}
		r.Close()
	}
}

func copyFile(t *testing.T, src, dst string) {
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestAudioReaderDecodesTheTone(t *testing.T) {
	needFFmpeg(t)
	f := writeFixture(t, "1080p30.mp4", 1920, 1080, 30, 120, true, false)
	d, err := ReadMP4Audio(f.path)
	if err != nil {
		t.Fatal(err)
	}
	if d.SampleRate != toneRate || d.Channels != 2 {
		t.Fatalf("decoded %d Hz %d ch", d.SampleRate, d.Channels)
	}
	want := toneRate * f.frames / f.fps
	t.Logf("decoded %d frames (%d ms), wrote %d", d.Frames(), d.DurationMS(), want)
	if d.Frames() < want || d.Frames() > want+2048 {
		t.Errorf("decoded %d frames, wrote %d", d.Frames(), want)
	}
	left := channel(d.Samples, 2, 0)
	lag, snr := alignTone(left[:toneRate])
	t.Logf("our decode vs the written tone: lag %d samples, SNR %.1f dB", lag, snr)
	if lag != 0 || snr < 30 {
		t.Errorf("decoded tone is %d samples off with SNR %.1f dB", lag, snr)
	}
	theirs := channel(ffmpegAudio(t, f.path), 2, 0)
	tlag, tsnr := alignTone(theirs[:toneRate])
	t.Logf("ffmpeg decode vs the written tone: lag %d samples, SNR %.1f dB, %d frames", tlag, tsnr, len(theirs))

	r, err := OpenAudioReader(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, ms := range []int64{1500, 250, 3000} {
		if err := r.SeekMS(ms); err != nil {
			t.Fatal(err)
		}
		chunk, err := r.NextChunk(nil)
		if err != nil {
			t.Fatal(err)
		}
		at := int(sampleIndex(ms, toneRate)) * 2
		worst := 0.0
		for i, v := range chunk {
			worst = math.Max(worst, math.Abs(float64(v-d.Samples[at+i])))
		}
		t.Logf("seek to %d ms: %d samples, largest difference from the full decode %.2g", ms, len(chunk)/2, worst)
		if worst > 1e-3 {
			t.Errorf("chunk after seeking to %d ms differs from the full decode by %g", ms, worst)
		}
	}
}

func TestAudioReaderReportsNoAudio(t *testing.T) {
	f := writeFixture(t, "1080p30-software.mp4", 1920, 1080, 30, 60, false, true)
	if _, err := OpenAudioReader(f.path); err != ErrNoAudio {
		t.Fatalf("OpenAudioReader on a silent file: %v, want ErrNoAudio", err)
	}
}

func channel(interleaved []float32, channels, c int) []float32 {
	out := make([]float32, len(interleaved)/channels)
	for i := range out {
		out[i] = interleaved[i*channels+c]
	}
	return out
}

// alignTone finds the shift, within one period, that best fits the samples to the tone the
// fixtures write, and the signal-to-noise ratio at that shift, skipping the encoder's
// fade-in.
func alignTone(s []float32) (lag int, snr float64) {
	period := toneRate / toneHz
	best := math.Inf(1)
	var sig float64
	for l := -period / 2; l < period/2; l++ {
		var noise float64
		sig = 0
		for i := 2048; i < len(s); i++ {
			want := toneAmp * math.Sin(2*math.Pi*toneHz*float64(i-l)/toneRate)
			d := float64(s[i]) - want
			noise += d * d
			sig += want * want
		}
		if noise < best {
			best, lag = noise, l
		}
	}
	return lag, 10 * math.Log10(sig/best)
}
