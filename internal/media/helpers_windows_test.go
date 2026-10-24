package media

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// outDir holds the files the tests write. GLINT_MEDIA_TEST_OUT keeps them for inspection.
func outDir(t testing.TB) string {
	if d := os.Getenv("GLINT_MEDIA_TEST_OUT"); d != "" {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		return d
	}
	return t.TempDir()
}

// pattern is frame i of a moving test card: smooth colour waves drifting at different
// speeds, a checkerboard square crossing the frame, and thin bars, so both motion and fine
// edges are present.
func pattern(i, w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	wave := func(n int, period, shift float64) []uint8 {
		out := make([]uint8, n)
		for k := range out {
			out[k] = uint8(128 + 100*math.Sin((float64(k)+shift)/period))
		}
		return out
	}
	rx := wave(w, 97, float64(8*i))
	gy := wave(h, 71, float64(-5*i))
	bxy := wave(w+h, 53, float64(3*i))
	size := h / 5
	sx, sy := (i*w/90)%(w-size), h/3
	for y := 0; y < h; y++ {
		row := img.Pix[y*img.Stride:]
		for x := 0; x < w; x++ {
			p := row[x*4 : x*4+4]
			p[0], p[1], p[2], p[3] = rx[x], gy[y], bxy[x+y], 255
			if x >= sx && x < sx+size && y >= sy && y < sy+size {
				v := uint8(0)
				if ((x-sx)/(size/8)+(y-sy)/(size/8))%2 == 0 {
					v = 255
				}
				p[0], p[1], p[2] = v, v, v
			}
			if y > h*4/5 && (x/6)%2 == 0 {
				p[0], p[1], p[2] = 230, 40, 40
			}
		}
	}
	return img
}

func sine(frames, rate, channels int, hz, amp float64, start int) []float32 {
	out := make([]float32, frames*channels)
	for f := 0; f < frames; f++ {
		v := float32(amp * math.Sin(2*math.Pi*hz*float64(start+f)/float64(rate)))
		for c := 0; c < channels; c++ {
			out[f*channels+c] = v
		}
	}
	return out
}

func needFFmpeg(t testing.TB) {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not on PATH", tool)
		}
	}
}

type probe struct {
	Streams []struct {
		CodecType      string `json:"codec_type"`
		CodecName      string `json:"codec_name"`
		Profile        string `json:"profile"`
		Width          int    `json:"width"`
		Height         int    `json:"height"`
		RFrameRate     string `json:"r_frame_rate"`
		AvgFrameRate   string `json:"avg_frame_rate"`
		ColorRange     string `json:"color_range"`
		ColorSpace     string `json:"color_space"`
		ColorTransfer  string `json:"color_transfer"`
		ColorPrimaries string `json:"color_primaries"`
		PixFmt         string `json:"pix_fmt"`
		SampleRate     string `json:"sample_rate"`
		Channels       int    `json:"channels"`
		Duration       string `json:"duration"`
		BitRate        string `json:"bit_rate"`
		NbFrames       string `json:"nb_frames"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		BitRate  string `json:"bit_rate"`
	} `json:"format"`
	raw string
}

func ffprobe(t testing.TB, path string) probe {
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", "-show_format", path).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", path, err)
	}
	var p probe
	if err := json.Unmarshal(out, &p); err != nil {
		t.Fatal(err)
	}
	p.raw = string(out)
	return p
}

func (p probe) stream(kind string) int {
	for i, s := range p.Streams {
		if s.CodecType == kind {
			return i
		}
	}
	return -1
}

func seconds(s string) float64 {
	v, _ := strconv.ParseFloat(s, 64)
	return v
}

// summary is the ffprobe line the report quotes.
func (p probe) summary() string {
	var b strings.Builder
	for _, s := range p.Streams {
		switch s.CodecType {
		case "video":
			fmt.Fprintf(&b, "video %s (%s) %dx%d %s r=%s avg=%s range=%s space=%s transfer=%s primaries=%s dur=%s frames=%s bps=%s; ",
				s.CodecName, s.Profile, s.Width, s.Height, s.PixFmt, s.RFrameRate, s.AvgFrameRate, s.ColorRange, s.ColorSpace,
				s.ColorTransfer, s.ColorPrimaries, s.Duration, s.NbFrames, s.BitRate)
		case "audio":
			fmt.Fprintf(&b, "audio %s %s Hz %d ch dur=%s bps=%s; ", s.CodecName, s.SampleRate, s.Channels, s.Duration, s.BitRate)
		}
	}
	fmt.Fprintf(&b, "format dur=%s bps=%s", p.Format.Duration, p.Format.BitRate)
	return b.String()
}

// ffmpegFrames decodes n frames from path starting at the first frame at or after ss
// seconds, in pixFmt (rgba or nv12).
func ffmpegFrames(t testing.TB, path string, ss float64, n int, pixFmt string) []byte {
	args := []string{"-v", "error"}
	if ss > 0 {
		args = append(args, "-ss", strconv.FormatFloat(ss, 'f', 6, 64))
	}
	args = append(args, "-i", path, "-frames:v", strconv.Itoa(n), "-f", "rawvideo", "-pix_fmt", pixFmt, "-")
	var stderr bytes.Buffer
	cmd := exec.Command("ffmpeg", args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v\n%s", args, err, stderr.String())
	}
	return out
}

// ffmpegStream decodes every frame of path as rgba; each call to next returns the next one,
// or nil at the end.
func ffmpegStream(t testing.TB, path string, frameBytes int) (next func() []byte, stop func()) {
	cmd := exec.Command("ffmpeg", "-v", "error", "-i", path, "-f", "rawvideo", "-pix_fmt", "rgba", "-")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReaderSize(out, 1<<20)
	next = func() []byte {
		b := make([]byte, frameBytes)
		if _, err := io.ReadFull(r, b); err != nil {
			return nil
		}
		return b
	}
	return next, func() {
		io.Copy(io.Discard, r)
		cmd.Wait()
	}
}

func ffmpegAudio(t testing.TB, path string) []float32 {
	out, err := exec.Command("ffmpeg", "-v", "error", "-i", path, "-f", "f32le", "-acodec", "pcm_f32le", "-").Output()
	if err != nil {
		t.Fatalf("ffmpeg audio: %v", err)
	}
	s := make([]float32, len(out)/4)
	for i := range s {
		s[i] = math.Float32frombits(uint32(out[i*4]) | uint32(out[i*4+1])<<8 | uint32(out[i*4+2])<<16 | uint32(out[i*4+3])<<24)
	}
	return s
}

func rgbaFrom(w, h int, pix []byte) *image.RGBA {
	return &image.RGBA{Pix: pix, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
}

// planePSNR compares two tightly packed planes byte for byte.
func planePSNR(a, b []byte) float64 {
	var sum float64
	for i := range a {
		d := float64(a[i]) - float64(b[i])
		sum += d * d
	}
	if sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(sum/float64(len(a))))
}

// packedNV12 is f's planes without row padding, the layout ffmpeg's rawvideo nv12 uses.
func packedNV12(f *Nv12Frame) []byte {
	out := make([]byte, 0, f.Width*f.Height*3/2)
	for y := 0; y < f.Height; y++ {
		out = append(out, f.Data[y*f.Stride:y*f.Stride+f.Width]...)
	}
	for y := 0; y < (f.Height+1)/2; y++ {
		at := (f.Height + y) * f.Stride
		out = append(out, f.Data[at:at+f.Width]...)
	}
	return out
}

type stats struct{ min, max, sum float64 }

func newStats() stats { return stats{min: math.Inf(1), max: math.Inf(-1)} }

func (s *stats) add(v float64) {
	s.min, s.max, s.sum = math.Min(s.min, v), math.Max(s.max, v), s.sum+v
}

// fixtures are the MP4s several tests read, written once per run.
var fixtures struct {
	sync.Mutex
	files map[string]fixture
}

type fixture struct {
	path    string
	summary WriteSummary
	frames  int
	w, h    int
	fps     int
}

const (
	toneHz   = 1000
	toneAmp  = 0.5
	toneRate = 48000
)

// writeFixture encodes frames of pattern at w x h and fps, with a 1 kHz stereo tone when
// audio is set, and remembers the file for later tests.
func writeFixture(t testing.TB, name string, w, h, fps, frames int, audio, software bool) fixture {
	fixtures.Lock()
	defer fixtures.Unlock()
	if f, ok := fixtures.files[name]; ok {
		return f
	}
	dir := os.Getenv("GLINT_MEDIA_TEST_OUT")
	if dir == "" {
		dir = fixtureDir
	}
	path := filepath.Join(dir, name)
	var ap *AudioParams
	if audio {
		ap = &AudioParams{SampleRate: toneRate, Channels: 2}
	}
	vw, err := CreateVideoWriter(path, VideoParams{Width: w, Height: h, FPS: fps, Bitrate: RecordingBitrate(w, h, fps), Software: software}, ap)
	if err != nil {
		t.Fatal(err)
	}
	written := 0
	for i := 0; i < frames; i++ {
		if err := vw.WriteFrame(pattern(i, w, h)); err != nil {
			vw.Close()
			t.Fatal(err)
		}
		if audio {
			target := (i + 1) * toneRate / fps
			if err := vw.WriteAudio(sine(target-written, toneRate, 2, toneHz, toneAmp, written)); err != nil {
				vw.Close()
				t.Fatal(err)
			}
			written = target
		}
	}
	sum, err := vw.Finish()
	if err != nil {
		t.Fatal(err)
	}
	f := fixture{path: path, summary: sum, frames: frames, w: w, h: h, fps: fps}
	if fixtures.files == nil {
		fixtures.files = map[string]fixture{}
	}
	fixtures.files[name] = f
	return f
}

var fixtureDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "glint-media-test")
	if err != nil {
		panic(err)
	}
	fixtureDir = dir
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
