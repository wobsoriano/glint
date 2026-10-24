package studio

import (
	"encoding/json"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/render"
)

// testdata/compose holds frames the Rust compositor drew from a synthetic session, written by
// (in a checkout of f1d58d2, the last Rust commit) cargo run --release -p glint-studio --example go_compose -- synthetic internal/studio/testdata/compose
type composeManifest struct {
	Session  string        `json:"session"`
	Screen   string        `json:"screen"`
	Camera   *string       `json:"camera"`
	Captions []CaptionWord `json:"captions"`
	Frames   []struct {
		Name       string          `json:"name"`
		Project    json.RawMessage `json:"project"`
		T          float64         `json:"t"`
		Size       [2]uint32       `json:"size"`
		Camera     bool            `json:"camera"`
		MotionBlur bool            `json:"motion_blur"`
	} `json:"frames"`
}

func readPNG(t testing.TB, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return img
}

func writePNG(t testing.TB, path string, img image.Image) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

type pixelDiff struct {
	mean   float64
	max    int
	over16 float64 // share of pixels with a channel off by more than 16
	// strays counts pixels with a channel outside the range of the Rust pixel and its eight
	// neighbors, widened by 1. Anti-aliasing an edge differently stays inside that range; a
	// wrong color, alpha or position does not.
	strays int
	heat   *image.RGBA
}

func comparePixels(got *image.NRGBA, want image.Image) pixelDiff {
	b := want.Bounds()
	w := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(w, w.Rect, want, b.Min, draw.Src)
	if nw, ok := want.(*image.NRGBA); ok {
		w = nw
	}
	heat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	var sum, over, worst, strays int
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			wp := w.Pix[w.PixOffset(x, y):]
			p := got.Pix[got.PixOffset(x, y):]
			px := 0
			stray := false
			for k := range 4 {
				d := int(p[k]) - int(wp[k])
				sum += max(d, -d)
				px = max(px, d, -d)
				lo, hi := 255, 0
				for ny := max(y-1, 0); ny <= min(y+1, b.Dy()-1); ny++ {
					for nx := max(x-1, 0); nx <= min(x+1, b.Dx()-1); nx++ {
						v := int(w.Pix[w.PixOffset(nx, ny)+k])
						lo, hi = min(lo, v), max(hi, v)
					}
				}
				stray = stray || int(p[k]) < lo-1 || int(p[k]) > hi+1
			}
			worst = max(worst, px)
			if px > 16 {
				over++
			}
			o := heat.PixOffset(x, y)
			heat.Pix[o], heat.Pix[o+3] = uint8(min(px*8, 255)), 255
			if stray {
				strays++
				heat.Pix[o+1] = 255
			}
		}
	}
	n := b.Dx() * b.Dy()
	return pixelDiff{float64(sum) / float64(n*4), worst, float64(over) / float64(n), strays, heat}
}

// composeTolerance is the most a frame may differ from the Rust compositor, in channel levels.
// The screen, the cursor, the text and every interior match within rounding. The card mask,
// the bubble and the pills are anti-aliased analytically where tiny-skia supersamples 4x, so
// an edge pixel can differ by 1/8 coverage times the contrast across it: about 20 levels where
// the white screen meets a saturated background. Of the pixels off by more than 16, 92% to
// 100% sit within a pixel of the card's edge, which is 0.6% of a 360-pixel-wide fixture; the
// worst measured 0.61%, a mean of 0.10 and a max of 44 (an edge over a transparent
// background, where unpremultiplying amplifies rounding). Those bounds miss a wrong text alpha
// or a karaoke color, which strays catch: the worst frame, mid click ring, had 7.
var composeTolerance = struct {
	mean   float64
	over16 float64
	max    int
	strays float64
}{mean: 0.2, over16: 0.008, max: 64, strays: 0.0002}

// composeAgainst composes every frame a go_compose manifest lists and compares it with the
// Rust frame beside it, writing Go frames and heat maps to GLINT_COMPOSE_OUT when set.
func composeAgainst(t *testing.T, dir string) {
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m composeManifest
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	session := m.Session
	if !filepath.IsAbs(session) {
		session = filepath.Join(dir, session)
	}
	assets, err := LoadAssets(session, m.Captions)
	if err != nil {
		t.Fatal(err)
	}
	screen := render.Premultiply(readPNG(t, filepath.Join(dir, m.Screen)))
	var camera *image.RGBA
	if m.Camera != nil {
		camera = render.Premultiply(readPNG(t, filepath.Join(dir, *m.Camera)))
	}
	out := os.Getenv("GLINT_COMPOSE_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%-36s %9s %9s %5s %8s %7s", "frame", "size", "mean", "max", ">16 px", "strays")
	for _, f := range m.Frames {
		var p Project
		if err := json.Unmarshal(f.Project, &p); err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		in := FrameInputs{Screen: screen, OutputMs: f.T}
		if f.Camera {
			in.Camera = camera
		}
		compose := ComposeFrame
		if f.MotionBlur {
			compose = ComposeFrameMotionBlur
		}
		got := compose(&p, assets, in, f.Size[0], f.Size[1])
		want := readPNG(t, filepath.Join(dir, f.Name+".png"))
		if got.Rect.Size() != want.Bounds().Size() {
			t.Errorf("%s: size %v, Rust %v", f.Name, got.Rect.Size(), want.Bounds().Size())
			continue
		}
		d := comparePixels(got, want)
		t.Logf("%-36s %4dx%-4d %9.4f %5d %7.3f%% %7d", f.Name, f.Size[0], f.Size[1], d.mean, d.max, d.over16*100, d.strays)
		if out != "" {
			writePNG(t, filepath.Join(out, f.Name+".go.png"), got)
			writePNG(t, filepath.Join(out, f.Name+".diff.png"), d.heat)
		}
		strays := float64(d.strays) / float64(f.Size[0]*f.Size[1])
		if d.mean > composeTolerance.mean || d.over16 > composeTolerance.over16 || d.max > composeTolerance.max || strays > composeTolerance.strays {
			t.Errorf("%s: mean %.4f (limit %.2f), %.3f%% of pixels off by >16 (limit %.2f%%), max %d (limit %d), %.3f%% strays (limit %.2f%%)",
				f.Name, d.mean, composeTolerance.mean, d.over16*100, composeTolerance.over16*100, d.max, composeTolerance.max,
				strays*100, composeTolerance.strays*100)
		}
		RecycleFrame(got)
	}
}

func TestComposeMatchesRustOnSyntheticSession(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the Rust frames were drawn with Windows system fonts")
	}
	composeAgainst(t, filepath.Join("testdata", "compose"))
}

// TestComposeMatchesRustOnRealSessions compares frames go_compose drew from copies of
// Library sessions. They hold the user's screen, so they stay out of the repository:
//
//	ffmpeg -ss 2 -i <copy>/screen.mp4 -frames:v 1 <scratch>/screen.png
//	(in a checkout of f1d58d2, the last Rust commit) cargo run --release -p glint-studio --example go_compose -- real <copy> <scratch>/screen.png - <out>
//	GLINT_COMPOSE_REAL=<out>[;<out>...] go test ./internal/studio -run RealSessions -v
func TestComposeMatchesRustOnRealSessions(t *testing.T) {
	dirs := filepath.SplitList(os.Getenv("GLINT_COMPOSE_REAL"))
	if len(dirs) == 0 {
		t.Skip("GLINT_COMPOSE_REAL is not set")
	}
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) { composeAgainst(t, dir) })
	}
}

func TestCursorIsSizedRelativeToTheCard(t *testing.T) {
	size := func(multiplier float32) [2]int {
		in := input.Timeline{
			Version: 1,
			Size:    [2]uint32{3840, 2160},
			Pointer: []input.PointerSample{{T: 0, X: 1000, Y: 1000}, {T: 5000, X: 1000, Y: 1000}},
			Cursors: []input.CursorSprite{{ID: 0, File: "cursors/0.png", Scale: 1.5}},
		}
		sprite := image.NewRGBA(image.Rect(0, 0, 48, 48))
		render.Fill(sprite, render.Black)
		assets := NewAssets(in, map[uint32]CursorImage{0: {Pixmap: sprite, Scale: 1.5}}, nil)
		p := NewProject(5000)
		p.Design.Padding, p.Design.Shadow = 0, 0
		p.Design.Cursor.Size, p.Design.Cursor.Smoothing = multiplier, 0
		p.Export.Resolution = Res1080
		w, h := p.OutputSize(3840, 2160)
		eq(t, [2]uint32{w, h}, [2]uint32{1920, 1080})
		screen := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
		render.Fill(screen, render.RGB(0x787878))
		frame := FrameInputs{Screen: screen, OutputMs: 1000}
		shown := ComposeFrame(&p, assets, frame, w, h)
		p.Design.Cursor.Visible = false
		hidden := ComposeFrame(&p, assets, frame, w, h)
		x0, y0, x1, y1 := 1<<30, 1<<30, 0, 0
		for y := range int(h) {
			for x := range int(w) {
				o := shown.PixOffset(x, y)
				if !slices.Equal(shown.Pix[o:o+4], hidden.Pix[o:o+4]) {
					x0, y0, x1, y1 = min(x0, x), min(y0, y), max(x1, x), max(y1, y)
				}
			}
		}
		eq(t, [2]int{x0, y0}, [2]int{500, 500})
		return [2]int{x1 - x0 + 1, y1 - y0 + 1}
	}
	eq(t, size(1), [2]int{69, 69})
	eq(t, size(0.5), [2]int{35, 35})
}

// BenchmarkComposeFrame1080p composes the synthetic session at 1920x1080 over its first four
// seconds at 60 fps, as go_compose's Rust benchmark does: zoom in and out, the cursor, a click
// ring, the camera bubble, the caption bar and the keystroke caption, recycling each frame as
// export does.
func BenchmarkComposeFrame1080p(b *testing.B) {
	dir := filepath.Join("testdata", "compose")
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		b.Fatal(err)
	}
	var m composeManifest
	if err := json.Unmarshal(data, &m); err != nil {
		b.Fatal(err)
	}
	assets, err := LoadAssets(filepath.Join(dir, m.Session), m.Captions)
	if err != nil {
		b.Fatal(err)
	}
	screen := render.Premultiply(readPNG(b, filepath.Join(dir, m.Screen)))
	camera := render.Premultiply(readPNG(b, filepath.Join(dir, *m.Camera)))
	var p Project
	if err := json.Unmarshal(m.Frames[0].Project, &p); err != nil {
		b.Fatal(err)
	}
	p.Export.Resolution = Res1080
	w, h := p.OutputSize(uint32(screen.Rect.Dx()), uint32(screen.Rect.Dy()))
	in := FrameInputs{Screen: screen, Camera: camera}
	RecycleFrame(ComposeFrame(&p, assets, in, w, h))
	var times []time.Duration
	for b.Loop() {
		in.OutputMs = float64(len(times)%240) * 1000 / 60
		start := time.Now()
		f := ComposeFrame(&p, assets, in, w, h)
		times = append(times, time.Since(start))
		RecycleFrame(f)
	}
	slices.Sort(times)
	b.ReportMetric(float64(times[len(times)/2].Microseconds())/1000, "p50-ms")
	b.ReportMetric(float64(times[len(times)*95/100].Microseconds())/1000, "p95-ms")
}
