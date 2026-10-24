package media

import (
	"errors"
	"io"
	"math"
	"strconv"
	"testing"
)

func TestWriterEncodes1080pWithAAC(t *testing.T) {
	needFFmpeg(t)
	f := writeFixture(t, "1080p30.mp4", 1920, 1080, 30, 120, true, false)
	checkEncoded(t, f, true)
}

func TestWriterEncodes4K(t *testing.T) {
	needFFmpeg(t)
	f := writeFixture(t, "2160p30.mp4", 3840, 2160, 30, 60, true, false)
	checkEncoded(t, f, true)
}

func TestWriterSoftwareEncoder(t *testing.T) {
	needFFmpeg(t)
	f := writeFixture(t, "1080p30-software.mp4", 1920, 1080, 30, 60, false, true)
	if f.summary.Hardware {
		t.Fatal("Software was set but the summary says a hardware encoder ran")
	}
	checkEncoded(t, f, false)
}

// AMD's H.264 encoder doubles the bitrate at 60 fps unless the writer corrects its budget.
func TestWriterHoldsBitrateAt60FPS(t *testing.T) {
	needFFmpeg(t)
	f := writeFixture(t, "1080p60.mp4", 1920, 1080, 60, 120, false, false)
	p := ffprobe(t, f.path)
	v := p.Streams[p.stream("video")]
	target := float64(RecordingBitrate(1920, 1080, 60))
	ratio := seconds(v.BitRate) / target
	t.Logf("hardware %v, r=%s, %s bps for a %.0f bps target (%.2fx)", f.summary.Hardware, v.RFrameRate, v.BitRate, target, ratio)
	if v.RFrameRate != "60/1" || ratio < 0.7 || ratio > 1.3 {
		t.Errorf("60 fps file runs at %s with %.2fx its bitrate", v.RFrameRate, ratio)
	}
}

func checkEncoded(t *testing.T, f fixture, audio bool) {
	t.Helper()
	if f.summary.Frames != int64(f.frames) || f.summary.DurationMS != int64(f.frames)*1000/int64(f.fps) || f.summary.Bytes == 0 {
		t.Fatalf("summary %+v for %d frames", f.summary, f.frames)
	}
	t.Logf("summary: %+v", f.summary)

	p := ffprobe(t, f.path)
	t.Logf("ffprobe: %s", p.summary())
	v := p.stream("video")
	if v < 0 {
		t.Fatal("no video stream")
	}
	vs := p.Streams[v]
	wantDur := float64(f.frames) / float64(f.fps)
	if vs.CodecName != "h264" || vs.Width != f.w || vs.Height != f.h {
		t.Errorf("video is %s %dx%d, want h264 %dx%d", vs.CodecName, vs.Width, vs.Height, f.w, f.h)
	}
	if vs.RFrameRate != "30/1" || vs.AvgFrameRate != "30/1" {
		t.Errorf("frame rate r=%s avg=%s, want 30/1", vs.RFrameRate, vs.AvgFrameRate)
	}
	if math.Abs(seconds(vs.Duration)-wantDur) > 0.04 {
		t.Errorf("video duration %s s, want %.3f", vs.Duration, wantDur)
	}
	if vs.NbFrames != "" && vs.NbFrames != strconv.Itoa(f.frames) {
		t.Errorf("nb_frames %s, want %d", vs.NbFrames, f.frames)
	}
	if vs.ColorSpace != "bt709" || vs.ColorTransfer != "bt709" || vs.ColorPrimaries != "bt709" || vs.ColorRange != "tv" {
		t.Errorf("colour tags space=%s transfer=%s primaries=%s range=%s, want bt709 and tv",
			vs.ColorSpace, vs.ColorTransfer, vs.ColorPrimaries, vs.ColorRange)
	}
	if a := p.stream("audio"); audio {
		if a < 0 {
			t.Fatal("no audio stream")
		}
		as := p.Streams[a]
		if as.CodecName != "aac" || as.SampleRate != "48000" || as.Channels != 2 {
			t.Errorf("audio is %s %s Hz %d ch, want aac 48000 Hz 2 ch", as.CodecName, as.SampleRate, as.Channels)
		}
		if math.Abs(seconds(as.Duration)-wantDur) > 0.05 {
			t.Errorf("audio duration %s s, want %.3f", as.Duration, wantDur)
		}
	} else if a >= 0 {
		t.Error("silent file has an audio stream")
	}

	r, err := OpenVideoReader(f.path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if w, h := r.Size(); w != f.w || h != f.h || r.FPS() != float64(f.fps) {
		t.Errorf("reader says %dx%d at %g fps", w, h, r.FPS())
	}
	ours, theirs, agree := newStats(), newStats(), newStats()
	next, stop := ffmpegStream(t, f.path, f.w*f.h*4)
	defer stop()
	for i := 0; ; i++ {
		frame, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			if i != f.frames {
				t.Fatalf("reader ended after %d frames, want %d", i, f.frames)
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if want := int64(i) * 1000 / int64(f.fps); frame.TMS < want-1 || frame.TMS > want+1 {
			t.Errorf("frame %d at %d ms, want %d", i, frame.TMS, want)
		}
		src := pattern(i, f.w, f.h)
		ff := next()
		if ff == nil {
			t.Fatalf("ffmpeg decoded only %d frames", i)
		}
		theirsImg := rgbaFrom(f.w, f.h, ff)
		ours.add(psnr(src, frame.Image))
		theirs.add(psnr(src, theirsImg))
		agree.add(psnr(frame.Image, theirsImg))
	}
	n := float64(f.frames)
	t.Logf("PSNR vs source, our decode: min %.2f mean %.2f dB; ffmpeg decode: min %.2f mean %.2f dB; ours vs ffmpeg: min %.2f dB",
		ours.min, ours.sum/n, theirs.min, theirs.sum/n, agree.min)
	if ours.min < 35 || theirs.min < 35 {
		t.Errorf("PSNR below 35 dB")
	}
}
