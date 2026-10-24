package main

import (
	"image"
	"math/rand/v2"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A frozen screenshot drawn over a window's whole box lands 1:1 on the device pixels at the
// display scales Windows offers, on MyGo's CPU renderer (ui.Render). The window itself is placed
// in physical pixels; this checks the drawing half.
func TestScreenshotDrawsOneToOneAtEveryScale(t *testing.T) {
	for _, tc := range []struct {
		scale float32
		w, h  int // DIPs
	}{{1, 640, 360}, {1.25, 512, 288}, {1.5, 640, 360}, {1.75, 640, 360}, {2, 320, 180}, {2.25, 320, 180}} {
		pw, ph := int(float32(tc.w)*tc.scale+0.5), int(float32(tc.h)*tc.scale+0.5)
		shot := image.NewRGBA(image.Rect(0, 0, pw, ph))
		r := rand.New(rand.NewPCG(3, 4))
		for i := 0; i < len(shot.Pix); i += 4 {
			v := r.Uint32()
			shot.Pix[i], shot.Pix[i+1], shot.Pix[i+2], shot.Pix[i+3] = uint8(v), uint8(v>>8), uint8(v>>16), 255
		}
		bmp := ui.NewBitmap(shot).Pixelated()
		got := ui.Render(func(c *ui.Context) {
			ui.Box(c).Fill().Draw(func(p *ui.Painter, box ui.Rect) { p.Image(bmp, box, ui.FillBox) })
		}, tc.w, tc.h, tc.scale)
		if got.Bounds().Size() != shot.Bounds().Size() {
			t.Errorf("scale %v: rendered %v, screenshot %v", tc.scale, got.Bounds().Size(), shot.Bounds().Size())
			continue
		}
		n, first := diff(got, shot, 0)
		t.Logf("scale %.2f: %dx%d px, %d pixels differ", tc.scale, pw, ph, n)
		if n > 0 {
			t.Errorf("scale %v: %d of %d pixels differ, first at %v", tc.scale, n, pw*ph, first)
		}
	}
}
