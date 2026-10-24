package theme

import (
	"image"
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestEveryIconParses(t *testing.T) {
	for _, n := range Icons() {
		if _, err := ui.ParseSVG([]byte(IconSource(n))); err != nil {
			t.Errorf("%s: %v", n, err)
		}
	}
	if _, err := ui.ParseSVG([]byte(MarkSource(false))); err != nil {
		t.Errorf("mark: %v", err)
	}
	if _, err := ui.ParseSVG([]byte(MarkSource(true))); err != nil {
		t.Errorf("template mark: %v", err)
	}
}

// Every icon draws ink in the text color when MyGo renders it.
func TestEveryIconDrawsInk(t *testing.T) {
	for _, n := range Icons() {
		img := ui.Render(func(c *ui.Context) {
			c.Root().Background(ui.RGB(255, 255, 255))
			ui.Icon(c, Icon(n)).Size(24, 24).TextColor(ui.RGB(0, 0, 0))
		}, 24, 24, 2)
		ink := 0
		for i := 0; i < len(img.Pix); i += 4 {
			if img.Pix[i] < 128 {
				ink++
			}
		}
		if ink < 20 {
			t.Errorf("%s drew %d dark pixels of 48x48", n, ink)
		}
	}
}

// The SVG mark matches the PNG the Rust build rendered with tiny-skia.
func TestMarkMatchesRustPNG(t *testing.T) {
	f, err := os.Open("../../../assets/glint-1024.png")
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	want, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	got := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Image(c, Mark(false)).Size(64, 64)
	}, 64, 64, 16)
	if got.Bounds() != want.Bounds() {
		t.Fatalf("rendered %v, want %v", got.Bounds(), want.Bounds())
	}
	var sum, worst float64
	off := 0
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			wr, wg, wb, wa := want.At(x, y).RGBA()
			gr, gg, gb, ga := got.At(x, y).RGBA()
			d := (absDiff(wr, gr) + absDiff(wg, gg) + absDiff(wb, gb) + absDiff(wa, ga)) / 4 / 257
			sum += d
			worst = max(worst, d)
			if d > 24 {
				off++
			}
		}
	}
	mean := sum / float64(b.Dx()*b.Dy())
	t.Logf("mean channel difference %.2f/255, worst %.0f, %d of %d pixels off by more than 24", mean, worst, off, b.Dx()*b.Dy())
	if mean > 2 || off > b.Dx()*b.Dy()/200 {
		writePNG(t, "mark-go.png", got)
		t.Errorf("the SVG mark differs from glint-1024.png: mean %.2f, %d pixels off", mean, off)
	}
}

func TestPalette(t *testing.T) {
	light := New(false, ui.Color{}, false)
	if light.Accent != FallbackAccent || light.OnAccent != ui.RGB(255, 255, 255) {
		t.Errorf("no desktop accent: got %v on %v", light.Accent, light.OnAccent)
	}
	if light.Base.A != 255 || light.Layer.A != 255 {
		t.Errorf("without a backdrop the surfaces are opaque: base %v layer %v", light.Base, light.Layer)
	}
	if m := New(false, ui.Color{}, true); m.Base != hex(0xF3F3F3D8) {
		t.Errorf("over Mica the base stays translucent: %v", m.Base)
	}
	dark := New(true, ui.RGB(0x0F, 0x34, 0x60), false)
	if want := ui.RGB(0x5F, 0x77, 0x95); dark.Accent != want {
		t.Errorf("a dim accent in the dark is lifted a third to white: got %v, want %v", dark.Accent, want)
	}
	if yellow := New(true, ui.RGB(0xFF, 0xD7, 0x00), false); yellow.OnAccent != ui.RGB(0, 0, 0) {
		t.Errorf("text on a light accent is black: %v", yellow.OnAccent)
	}
}

// Apply follows the window's appearance as the system switches it.
func TestApplyFollowsAppearance(t *testing.T) {
	var p Palette
	tt := ui.NewTester(func(c *ui.Context) {
		p = Apply(c)
		ui.Text(c, "Glint")
	}, 80, 40)
	tt.SetPreferences(ui.Preferences{Accent: ui.RGB(0xE8, 0x11, 0x23), TextScale: 1})
	for _, dark := range []bool{false, true, false} {
		tt.SetDark(dark)
		tt.Frame()
		want := New(dark, ui.RGB(0xE8, 0x11, 0x23), false)
		if p != want {
			t.Fatalf("dark=%v: palette %+v, want %+v", dark, p, want)
		}
		px := tt.Image().RGBAAt(78, 38)
		if px.R != want.Base.R || px.G != want.Base.G || px.B != want.Base.B {
			t.Errorf("dark=%v: window background %v, want %v", dark, px, want.Base)
		}
	}
}

func absDiff(a, b uint32) float64 {
	if a > b {
		return float64(a - b)
	}
	return float64(b - a)
}

func writePNG(t *testing.T, name string, img image.Image) {
	f, err := os.Create(t.TempDir() + "/" + name)
	if err != nil {
		return
	}
	defer f.Close()
	_ = png.Encode(f, img)
	t.Log("wrote", f.Name())
}
