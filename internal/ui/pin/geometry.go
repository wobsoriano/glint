package pin

import (
	"math"

	"github.com/wobsoriano/glint/internal/platform"
)

const (
	// fitShare is how much of the work area a new pin may cover at most.
	fitShare = 0.6
	// minW and maxW bound a pin's width in DIPs as it zooms.
	minW, maxW = 80, 4000
	// zoomStep and opacityStep are one wheel notch's change.
	zoomStep    = 1.08
	opacityStep = 0.08
	minOpacity  = 0.2
)

// initialRect centers an image of w x h pixels on the work area, at one image pixel per
// physical pixel times k, shrunk to fit fitShare of the area. k is the display's scale over the
// capture's, so a capture shows at the size it was taken.
func initialRect(area platform.Rect, w, h int, k float64) platform.Rect {
	pw, ph := float64(w)*k, float64(h)*k
	fit := min(float64(area.W)*fitShare/pw, float64(area.H)*fitShare/ph, 1)
	rw, rh := max(int(math.Round(pw*fit)), 1), max(int(math.Round(ph*fit)), 1)
	return platform.Rect{X: area.X + (area.W-rw)/2, Y: area.Y + (area.H-rh)/2, W: rw, H: rh}
}

// zoomRect scales r by k around the pointer at (px, py), keeping the aspect of the image and a
// width between minPx and maxPx: the point under the pointer stays under it.
func zoomRect(r platform.Rect, px, py int, k, aspect float64, minPx, maxPx int) platform.Rect {
	w := min(max(float64(r.W)*k, float64(minPx)), float64(maxPx))
	h := w / aspect
	kx, ky := w/float64(r.W), h/float64(r.H)
	x := float64(px) - float64(px-r.X)*kx
	y := float64(py) - float64(py-r.Y)*ky
	return platform.Rect{X: int(math.Round(x)), Y: int(math.Round(y)), W: int(math.Round(w)), H: int(math.Round(h))}
}

// notches turns a scroll into wheel notches, positive away from the user (zoom in): a mouse
// wheel counts each event as one, a touchpad its distance over a notch's 99 DIPs.
func notches(dy float32, precise bool) float64 {
	if !precise {
		switch {
		case dy < 0:
			return 1
		case dy > 0:
			return -1
		}
		return 0
	}
	return -float64(dy) / 99
}

func nextOpacity(o, notches float64) float64 {
	return min(max(o+notches*opacityStep, minOpacity), 1)
}
