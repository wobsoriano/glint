package platform

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"testing"
)

func TestShadowPadsRoundsCornersAndKeepsTheMiddle(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 200, 100))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []uint8{10, 200, 30, 255})
	}
	out := withShadow(img, 1)
	if b := out.Bounds(); b.Dx() != 256 || b.Dy() != 162 {
		t.Fatalf("size %v, want 256x162", b.Size())
	}
	if got := out.RGBAAt(28+100, 28+50); got != (color.RGBA{10, 200, 30, 255}) {
		t.Errorf("middle %v", got)
	}
	if got := out.RGBAAt(28, 28); got.G != 0 {
		t.Errorf("corner pixel is outside the rounded window, got %v", got)
	}
	if got := out.RGBAAt(0, 0); got.A != 0 {
		t.Errorf("far margin is transparent, got %v", got)
	}
	if below := out.RGBAAt(128, 28+100+3).A; below < 40 || below >= 120 {
		t.Errorf("shadow under the window, alpha %d", below)
	}
}

// testdata/shadow-rust-*.png come from with_shadow in src/capture.rs, run on shadow-in.png at
// 100%, 150% and 200%.
func TestShadowMatchesTheRustBuild(t *testing.T) {
	in := readPNG(t, "testdata/shadow-in.png")
	for _, c := range []struct {
		dpi  float64
		file string
	}{{1, "100"}, {1.5, "150"}, {2, "200"}} {
		want := readPNG(t, "testdata/shadow-rust-"+c.file+".png")
		got := withShadow(in, c.dpi)
		if got.Bounds() != want.Bounds() {
			t.Fatalf("%v: size %v, Rust %v", c.dpi, got.Bounds(), want.Bounds())
		}
		worst, at := 0, 0
		for i := range got.Pix {
			d := int(got.Pix[i]) - int(want.Pix[i])
			if d < 0 {
				d = -d
			}
			if d > worst {
				worst, at = d, i
			}
		}
		t.Logf("dpi %v: largest channel difference %d at byte %d", c.dpi, worst, at)
		if worst > 1 {
			t.Errorf("dpi %v: differs from Rust by %d at pixel (%d, %d)", c.dpi, worst, (at%got.Stride)/4, at/got.Stride)
		}
	}
}

func readPNG(t *testing.T, path string) *image.RGBA {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	out := image.NewRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, image.Point{}, draw.Src)
	return out
}

func TestShadowScalesWithDPIAndTakesASubImage(t *testing.T) {
	big := image.NewRGBA(image.Rect(0, 0, 300, 200))
	sub := big.SubImage(image.Rect(50, 40, 250, 140)).(*image.RGBA)
	for y := 40; y < 140; y++ {
		for x := 50; x < 250; x++ {
			sub.SetRGBA(x, y, color.RGBA{200, 10, 10, 255})
		}
	}
	out := withShadow(sub, 2)
	if b := out.Bounds(); b.Dx() != 200+112 || b.Dy() != 100+112+12 {
		t.Fatalf("size %v, want 312x224", b.Size())
	}
	if got := out.RGBAAt(56+100, 56+50); got != (color.RGBA{200, 10, 10, 255}) {
		t.Errorf("middle %v", got)
	}
}
