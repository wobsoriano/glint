package media

import (
	"errors"
	"image"
	"io"
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"
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

func TestWriterEncodesHEVC(t *testing.T) {
	needFFmpeg(t)
	enc, dec := AvailableCodecs(), DecodableCodecs()
	t.Logf("encodable %v, decodable %v", enc, dec)
	if !slices.Contains(enc, H264) || !slices.Contains(dec, H264) {
		t.Errorf("H.264 is missing from the codec lists")
	}
	if !slices.Contains(enc, HEVC) {
		t.Skip("no HEVC encoder")
	}
	path := filepath.Join(outDir(t), "hevc.mp4")
	vw, err := CreateVideoWriter(path, VideoParams{Width: 1920, Height: 1080, FPS: 30, Bitrate: RecordingBitrate(1920, 1080, 30), Codec: HEVC}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		if err := vw.WriteFrame(pattern(i, 1920, 1080)); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := vw.Finish()
	if err != nil {
		t.Fatal(err)
	}
	p := ffprobe(t, path)
	t.Logf("summary %+v; ffprobe: %s", sum, p.summary())
	v := p.Streams[p.stream("video")]
	if v.CodecName != "hevc" || v.Width != 1920 || v.Height != 1080 || v.ColorSpace != "bt709" || v.ColorRange != "tv" {
		t.Errorf("HEVC file is %s %dx%d space=%s range=%s", v.CodecName, v.Width, v.Height, v.ColorSpace, v.ColorRange)
	}
	ff := rgbaFrom(1920, 1080, ffmpegFrames(t, path, 0.5, 1, "rgba"))
	if p := psnr(pattern(15, 1920, 1080), ff); p < 35 {
		t.Errorf("ffmpeg decodes HEVC frame 15 at %.2f dB", p)
	} else {
		t.Logf("ffmpeg decodes HEVC frame 15 at %.2f dB", p)
	}
	if !slices.Contains(dec, HEVC) {
		t.Skip("no HEVC decoder to read it back")
	}
	r, err := OpenVideoReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.SeekMS(500); err != nil {
		t.Fatal(err)
	}
	f, err := r.NextFrame()
	if err != nil {
		t.Fatal(err)
	}
	if p := psnr(pattern(15, 1920, 1080), f.Image); f.TMS != 500 || p < 35 {
		t.Errorf("HEVC frame at %d ms has PSNR %.2f dB against frame 15", f.TMS, p)
	} else {
		t.Logf("HEVC frame 15 decodes at %.2f dB", p)
	}
}

// WriteBGRA is the camera path: BGRA rows with padding and the capture clock's timestamps.
func TestWriterTakesTimestampedBGRA(t *testing.T) {
	path := filepath.Join(outDir(t), "bgra.mp4")
	const w, h, stride = 640, 360, 640*4 + 64
	vw, err := CreateVideoWriter(path, VideoParams{Width: w, Height: h, FPS: 30, Bitrate: 2_000_000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	src := make([]*image.RGBA, 10)
	for i := range src {
		src[i] = pattern(i, w, h)
		bgra := make([]byte, stride*h)
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				s, d := src[i].Pix[y*src[i].Stride+x*4:], bgra[y*stride+x*4:]
				d[0], d[1], d[2], d[3] = s[2], s[1], s[0], 255
			}
		}
		if err := vw.WriteBGRA(bgra, stride, time.Duration(i)*50*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := vw.Finish(); err != nil {
		t.Fatal(err)
	}
	r, err := OpenVideoReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := 0; ; i++ {
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			if i != len(src) {
				t.Fatalf("read %d frames, wrote %d", i, len(src))
			}
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if f.TMS != int64(i)*50 {
			t.Errorf("frame %d at %d ms, written at %d", i, f.TMS, i*50)
		}
		// 360 rows are below 720, so this also checks the BT.709 tag overrides the size rule.
		if p := psnr(src[i], f.Image); p < 33 {
			t.Errorf("BGRA frame %d decodes at %.2f dB", i, p)
		}
	}
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
