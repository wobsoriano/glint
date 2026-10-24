package pin

import (
	"testing"

	"github.com/wobsoriano/glint/internal/platform"
)

var area = platform.Rect{X: 0, Y: 0, W: 1920, H: 1032}

func TestInitialRectCentersAtTheCapturedSize(t *testing.T) {
	got := initialRect(area, 800, 400, 1)
	if want := (platform.Rect{X: 560, Y: 316, W: 800, H: 400}); got != want {
		t.Errorf("initialRect = %+v, want %+v", got, want)
	}
}

func TestInitialRectFitsLargeCaptures(t *testing.T) {
	got := initialRect(area, 2560, 1440, 1)
	if got.W > 1152 || got.H > 620 {
		t.Errorf("initialRect = %+v, larger than 60%% of the work area", got)
	}
	if d := float64(got.W)/float64(got.H) - 16.0/9; d > 0.01 || d < -0.01 {
		t.Errorf("initialRect = %+v, lost the 16:9 aspect", got)
	}
	if cx, cy := got.X+got.W/2, got.Y+got.H/2; cx < 958 || cx > 962 || cy < 514 || cy > 518 {
		t.Errorf("initialRect = %+v, centered on (%d,%d)", got, cx, cy)
	}
}

func TestInitialRectFollowsTheScale(t *testing.T) {
	// A capture taken at 200% shown on a 100% display is half its pixels.
	if got := initialRect(area, 800, 400, 0.5); got.W != 400 || got.H != 200 {
		t.Errorf("initialRect at k=0.5 = %+v, want 400x200", got)
	}
}

func TestZoomKeepsThePointUnderThePointer(t *testing.T) {
	r := platform.Rect{X: 100, Y: 100, W: 400, H: 200}
	got := zoomRect(r, 200, 150, 1.5, 2, 80, 4000)
	if got.W != 600 || got.H != 300 {
		t.Fatalf("zoomRect size = %dx%d, want 600x300", got.W, got.H)
	}
	// (200,150) is a quarter across and a quarter down; it stays there.
	if got.X+got.W/4 != 200 || got.Y+got.H/4 != 150 {
		t.Errorf("zoomRect = %+v moved the point under the pointer", got)
	}
}

func TestZoomStopsAtItsBounds(t *testing.T) {
	r := platform.Rect{X: 0, Y: 0, W: 100, H: 50}
	if got := zoomRect(r, 0, 0, 0.1, 2, 80, 4000); got.W != 80 || got.H != 40 {
		t.Errorf("zoom out = %+v, want 80x40", got)
	}
	if got := zoomRect(r, 0, 0, 100, 2, 80, 4000); got.W != 4000 || got.H != 2000 {
		t.Errorf("zoom in = %+v, want 4000x2000", got)
	}
}

func TestNotches(t *testing.T) {
	if notches(-99, false) != 1 || notches(99, false) != -1 || notches(-300, false) != 1 {
		t.Error("a wheel event is one notch, up zooming in")
	}
	if n := notches(-49.5, true); n != 0.5 {
		t.Errorf("a touchpad's half notch = %v", n)
	}
}

func TestOpacityStaysVisible(t *testing.T) {
	if o := nextOpacity(1, -1); o < 0.919 || o > 0.921 {
		t.Errorf("one notch down from 1 = %v", o)
	}
	if o := nextOpacity(0.25, -3); o != minOpacity {
		t.Errorf("opacity went to %v, below %v", o, minOpacity)
	}
	if o := nextOpacity(0.95, 2); o != 1 {
		t.Errorf("opacity went to %v, above 1", o)
	}
}
