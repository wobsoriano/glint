package studio

import (
	"math"
	"testing"

	core "github.com/wobsoriano/glint/internal/studio"
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-3 }

func TestCardFitsInsidePaddingWithTheCropAspect(t *testing.T) {
	p := core.NewProject(1000)
	p.Design.Padding = 0.1
	if got := cardRect(&p, 1920, 1080, 1000, 1000); got != (rect{100, 275, 800, 450}) {
		t.Errorf("padded card %+v", got)
	}
	p.Crop = core.CropRect{X: 0, Y: 0, W: 0.5, H: 1}
	if r := cardRect(&p, 1920, 1080, 1000, 1000); !near(r.w, 711.111) || r.h != 800 {
		t.Errorf("cropped card %+v", r)
	}
}

func TestBubbleIsClampedInsideTheFrame(t *testing.T) {
	cam := core.CameraSettings{Visible: true, Center: [2]float32{1, 1}, Size: 0.2, Roundness: 0.5}
	if got := bubbleRect(cam, 1000, 500); got != (rect{885, 385, 100, 100}) {
		t.Errorf("bubble %+v", got)
	}
}

func TestCropDragKeepsRatioAndBounds(t *testing.T) {
	free := cropFromDrag(0.5, 0.5, 0.2, 0.9, 0, false, 1600, 900)
	if !near(free.X, 0.2) || !near(free.W, 0.3) || !near(free.H, 0.4) {
		t.Errorf("freeform %+v", free)
	}
	sq := cropFromDrag(0, 0, 1, 1, 1, true, 1600, 900)
	if !near(sq.W*1600, sq.H*900) || sq.H != 1 || sq.X != 0 || sq.Y != 0 {
		t.Errorf("square %+v", sq)
	}
	full := cropForRatio(core.FullCrop, 9.0/16, true, 1920, 1080)
	if !near(full.H, 1) || math.Abs(float64(full.W*1920/1080-9.0/16)) > 1e-4 || !near(full.X+full.W/2, 0.5) {
		t.Errorf("9:16 of the full frame %+v", full)
	}
}

func TestTimeScaleRoundTripsAndZoomsAroundThePointer(t *testing.T) {
	s := newTimeScale(10_000, 1000, 1, 0)
	if s.x(5_000) != 500 || s.ms(250) != 2_500 {
		t.Errorf("scale %+v", s)
	}
	start := zoomAround(10_000, 1000, 4, 5_000, 500)
	if z := newTimeScale(10_000, 1000, 4, start); z.x(5_000) != 500 {
		t.Errorf("zoomed x %v", z.x(5_000))
	}
	if got := newTimeScale(10_000, 1000, 2, 9_000).startMs; got != 5_000 {
		t.Errorf("clamped start %v", got)
	}
	if got := formatTime(83_456); got != "1:23.4" {
		t.Errorf("formatTime %q", got)
	}
}

func TestFitKeepsAspectAndEvenSides(t *testing.T) {
	for _, c := range []struct{ ow, oh, bw, bh, w, h uint32 }{
		{1920, 1080, 960, 960, 960, 540},
		{1080, 1920, 1000, 600, 336, 600},
		{2049, 1001, 1025, 1025, 1024, 500},
	} {
		if w, h := fitSize(c.ow, c.oh, c.bw, c.bh); w != c.w || h != c.h {
			t.Errorf("fit %dx%d in %dx%d = %dx%d, want %dx%d", c.ow, c.oh, c.bw, c.bh, w, h, c.w, c.h)
		}
	}
}
