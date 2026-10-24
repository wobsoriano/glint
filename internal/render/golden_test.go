package render

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

const goldenDir = "testdata/golden"

type goldenFixture struct {
	Name   string      `json:"name"`
	Scale  float32     `json:"scale"`
	Region *[4]float32 `json:"region"`
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

func goldenFixtures(t testing.TB) []goldenFixture {
	data, err := os.ReadFile(filepath.Join(goldenDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fs []goldenFixture
	if err := json.Unmarshal(data, &fs); err != nil {
		t.Fatal(err)
	}
	return fs
}

func renderFixture(t testing.TB, base *image.RGBA, fx goldenFixture) *image.RGBA {
	data, err := os.ReadFile(filepath.Join(goldenDir, fx.Name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseEditDoc(data)
	if err != nil {
		t.Fatalf("%s: %v", fx.Name, err)
	}
	src := Sources{Base: base}
	switch bg := doc.Style.Background.(type) {
	case ImageBackground:
		if src.BgImage, err = LoadBackground(filepath.Join(goldenDir, bg.Path)); err != nil {
			t.Fatal(err)
		}
	case BlurredBackground:
		src.BlurBg = MakeBlurBg(base)
	}
	var region *R
	if fx.Region != nil {
		region = &R{fx.Region[0], fx.Region[1], fx.Region[2], fx.Region[3]}
	}
	var c Compositor
	return c.Render(doc, src, fx.Scale, region)
}

type pixelDiff struct {
	mean    float64
	max     int
	over16  float64 // share of pixels with a channel off by more than 16
	heatmap *image.RGBA
}

func comparePixels(got *image.NRGBA, want image.Image) pixelDiff {
	b := want.Bounds()
	heat := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	var sum, over int
	worst := 0
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			r, g, bb, a := want.At(b.Min.X+x, b.Min.Y+y).RGBA()
			w := [4]int{int(r >> 8), int(g >> 8), int(bb >> 8), int(a >> 8)}
			if nw, ok := want.(*image.NRGBA); ok {
				p := nw.Pix[nw.PixOffset(b.Min.X+x, b.Min.Y+y):]
				w = [4]int{int(p[0]), int(p[1]), int(p[2]), int(p[3])}
			}
			p := got.Pix[got.PixOffset(x, y):]
			px := 0
			for k := range 4 {
				d := abs(int(p[k]) - w[k])
				sum += d
				px = max(px, d)
			}
			worst = max(worst, px)
			if px > 16 {
				over++
			}
			v := uint8(min(px*8, 255))
			heat.Pix[heat.PixOffset(x, y)+0] = v
			heat.Pix[heat.PixOffset(x, y)+3] = 255
		}
	}
	n := b.Dx() * b.Dy()
	return pixelDiff{float64(sum) / float64(n*4), worst, float64(over) / float64(n), heat}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// goldenTolerance is the most each fixture may differ from the Rust render, in channel levels.
// Fills and masks use analytic coverage where tiny-skia supersamples 4x per axis, so edge
// pixels differ by up to about 1/8 coverage; glyphs, gradients, blurs, pixel effects and
// interiors match within rounding. The worst fixtures measured a mean of 0.075, 0.17% of
// pixels off by more than 16, and a max of 41 at a stroke corner. Moving every annotation by
// one pixel measured a mean above 1.1; one changed title character, a max of 134.
var goldenTolerance = struct {
	mean   float64
	over16 float64
	max    int
}{mean: 0.2, over16: 0.004, max: 64}

func TestGoldenParityWithRust(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the Rust goldens were rendered with Windows system fonts")
	}
	base := Premultiply(readPNG(t, filepath.Join(goldenDir, "base.png")))
	out := os.Getenv("GLINT_GOLDEN_OUT")
	if out != "" {
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("%-28s %9s %5s %8s", "fixture", "mean", "max", ">16 px")
	for _, fx := range goldenFixtures(t) {
		got := Unpremultiply(renderFixture(t, base, fx))
		want := readPNG(t, filepath.Join(goldenDir, fx.Name+".png"))
		if got.Rect.Size() != want.Bounds().Size() {
			t.Errorf("%s: size %v, Rust %v", fx.Name, got.Rect.Size(), want.Bounds().Size())
			continue
		}
		d := comparePixels(got, want)
		t.Logf("%-28s %9.4f %5d %7.3f%%", fx.Name, d.mean, d.max, d.over16*100)
		if out != "" {
			writePNG(t, filepath.Join(out, fx.Name+".go.png"), got)
			writePNG(t, filepath.Join(out, fx.Name+".diff.png"), d.heatmap)
		}
		if d.mean > goldenTolerance.mean || d.over16 > goldenTolerance.over16 || d.max > goldenTolerance.max {
			t.Errorf("%s: mean %.4f (limit %.2f), %.3f%% of pixels off by >16 (limit %.2f%%), max %d (limit %d)",
				fx.Name, d.mean, goldenTolerance.mean, d.over16*100, goldenTolerance.over16*100, d.max, goldenTolerance.max)
		}
	}
}

func TestGoldenSizesMatchLayout(t *testing.T) {
	base := Premultiply(readPNG(t, filepath.Join(goldenDir, "base.png")))
	for _, fx := range goldenFixtures(t) {
		if fx.Region != nil {
			continue
		}
		data, _ := os.ReadFile(filepath.Join(goldenDir, fx.Name+".json"))
		doc, err := ParseEditDoc(data)
		if err != nil {
			t.Fatal(err)
		}
		f := NewFrame(doc, uint32(base.Rect.Dx()), uint32(base.Rect.Dy()), fx.Scale)
		want := readPNG(t, filepath.Join(goldenDir, fx.Name+".png")).Bounds().Size()
		if got := (image.Point{f.Layout.Width, f.Layout.Height}); got != want {
			t.Errorf("%s: layout %v, Rust %v", fx.Name, got, want)
		}
	}
}

func ExampleRenderDoc() {
	base := image.NewRGBA(image.Rect(0, 0, 320, 200))
	Fill(base, RGB(0x336699))
	doc := DefaultEditDoc()
	doc.Annotations = []Annotation{{ID: 1, Shape: Arrow{P(20, 180), P(200, 40)}, Color: RGB(0xFF3B30), Width: 4}}
	out := RenderDoc(base, doc)
	fmt.Println(out.Rect.Dx(), out.Rect.Dy())
	// Output: 448 328
}
