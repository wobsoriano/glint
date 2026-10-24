package editor

import (
	"math"
	"testing"

	"github.com/wobsoriano/glint/internal/render"
)

var (
	testCanvas = size{2000, 1000}
	testView   = size{1000, 800}
)

func near(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-2 }

func TestFitLeavesBreathingRoomAndCenters(t *testing.T) {
	v := viewport{}.resolve(testCanvas, testView, 1)
	if v.zoom != (1000-56)/2000.0 {
		t.Errorf("fit zoom %v", v.zoom)
	}
	mid := v.toCanvas(render.P(500, 400))
	if !near(mid.X, 1000) || !near(mid.Y, 500) {
		t.Errorf("view center shows %v, want the canvas center", mid)
	}
	if z := (viewport{}).resolve(size{100, 50}, testView, 1).zoom; z != 1 {
		t.Errorf("a small image fits at %v, want 1 (never enlarged)", z)
	}
}

func TestZoomStepsWalkStandardLevels(t *testing.T) {
	for _, c := range []struct {
		from float32
		up   bool
		want float32
	}{{1, true, 1.25}, {6, true, 8}, {0.43, true, 0.5}, {0.43, false, 1.0 / 3.0}, {10, true, maxZoom}} {
		if got := zoomStep(c.from, c.up); got != c.want {
			t.Errorf("zoomStep(%v, %v) = %v, want %v", c.from, c.up, got, c.want)
		}
	}
}

func TestZoomAroundPointerKeepsThatPointFixed(t *testing.T) {
	var vp viewport
	anchor := render.P(700, 300)
	before := vp.resolve(testCanvas, testView, 1).toCanvas(anchor)
	vp.zoomTo(2, anchor, testCanvas, testView, 1)
	after := vp.resolve(testCanvas, testView, 1)
	if after.zoom != 2 {
		t.Fatalf("zoom %v, want 2", after.zoom)
	}
	if p := after.toCanvas(anchor); !near(p.X, before.X) || !near(p.Y, before.Y) {
		t.Errorf("the point under the pointer moved from %v to %v", before, p)
	}
	vp.zoomTo(50, anchor, testCanvas, testView, 1)
	if *vp.zoom != maxZoom {
		t.Errorf("zoom past the limit is %v, want %v", *vp.zoom, maxZoom)
	}
}

func TestPanStopsAtEdgesAndFittingAxesStayCentered(t *testing.T) {
	one := float32(1)
	vp := viewport{zoom: &one, center: render.P(1000, 500)}
	vp.pan(-5000, 0, testCanvas, testView, 1)
	if v := vp.resolve(testCanvas, testView, 1); v.origin.X != 0 {
		t.Errorf("left edge: origin %v", v.origin.X)
	}
	vp.pan(10_000, 300, testCanvas, testView, 1)
	if v := vp.resolve(testCanvas, testView, 1); v.origin.X != 1000 {
		t.Errorf("right edge: origin %v", v.origin.X)
	}
	half := float32(0.5)
	vp = viewport{zoom: &half, center: render.P(1000, 0)}
	vp.pan(0, 400, testCanvas, testView, 1)
	if y := vp.resolve(testCanvas, testView, 1).toCanvas(render.P(500, 400)).Y; y != 500 {
		t.Errorf("a fitting axis stays centered, got %v", y)
	}
}
