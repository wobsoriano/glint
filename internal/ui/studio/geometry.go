package studio

import (
	"fmt"
	"math"

	core "github.com/wobsoriano/glint/internal/studio"
)

// rect is a box in DIPs of the preview frame or the timeline tracks.
type rect struct{ x, y, w, h float32 }

func (r rect) contains(x, y float32) bool {
	return x >= r.x && x <= r.x+r.w && y >= r.y && y <= r.y+r.h
}

// cardRect is where the cropped screen sits inside a frame of size w by h, padded by the
// project's padding and keeping the crop's aspect.
func cardRect(p *core.Project, srcW, srcH uint32, w, h float32) rect {
	inset := clampf(p.Design.Padding, 0, 0.475) * min(w, h)
	aw, ah := w-2*inset, h-2*inset
	content := float32(srcW) * p.Crop.W / max(float32(srcH)*p.Crop.H, 1)
	cw, ch := aw, aw/content
	if aw/ah > content {
		cw, ch = ah*content, ah
	}
	return rect{(w - cw) / 2, (h - ch) / 2, cw, ch}
}

// bubbleRect is the camera bubble inside a frame of size w by h, kept a margin clear of the
// edges.
func bubbleRect(cam core.CameraSettings, w, h float32) rect {
	short := min(w, h)
	dia := cam.Size * short
	margin := short * 0.03
	hx := min(dia/2+margin, w/2)
	hy := min(dia/2+margin, h/2)
	cx := clampf(cam.Center[0]*w, hx, w-hx)
	cy := clampf(cam.Center[1]*h, hy, h-hy)
	return rect{cx - dia/2, cy - dia/2, dia, dia}
}

// cropRatio is a crop preset; cropFree keeps no ratio.
type cropRatio int

const (
	cropFree cropRatio = iota
	cropOriginal
	cropSquare
	cropWide
	cropTall
	cropClassic
	cropPhoto
)

var cropRatios = []cropRatio{cropFree, cropOriginal, cropSquare, cropWide, cropTall, cropClassic, cropPhoto}

func (r cropRatio) label() string {
	return [...]string{"Freeform", "Original", "1:1", "16:9", "9:16", "4:3", "3:2"}[r]
}

// value is width over height in source pixels; ok is false for freeform.
func (r cropRatio) value(srcW, srcH uint32) (float32, bool) {
	switch r {
	case cropOriginal:
		return float32(srcW) / float32(max(srcH, 1)), true
	case cropSquare:
		return 1, true
	case cropWide:
		return 16.0 / 9, true
	case cropTall:
		return 9.0 / 16, true
	case cropClassic:
		return 4.0 / 3, true
	case cropPhoto:
		return 3.0 / 2, true
	}
	return 0, false
}

const minCrop = 0.05

// normalizedRatio turns a pixel ratio into one of the crop's normalized sides.
func normalizedRatio(ratio float32, srcW, srcH uint32) float32 {
	return ratio * float32(srcH) / float32(max(srcW, 1))
}

// cropFromDrag is the crop spanned from anchor to to, both normalized to the source, kept
// inside it, at least minCrop on a side, and at the ratio when one is given.
func cropFromDrag(ax, ay, tx, ty float32, ratio float32, fixed bool, srcW, srcH uint32) core.CropRect {
	sx, sy := float32(1), float32(1)
	if tx < ax {
		sx = -1
	}
	if ty < ay {
		sy = -1
	}
	roomW, roomH := ax, ay
	if sx > 0 {
		roomW = 1 - ax
	}
	if sy > 0 {
		roomH = 1 - ay
	}
	w := clampf(absf(tx-ax), minCrop, max(roomW, minCrop))
	h := clampf(absf(ty-ay), minCrop, max(roomH, minCrop))
	if fixed {
		r := normalizedRatio(ratio, srcW, srcH)
		if w/h > r {
			w = h * r
		} else {
			h = w / r
		}
		k := min(roomW/w, roomH/h, 1)
		w, h = w*k, h*k
	}
	x, y := ax-w, ay-h
	if sx > 0 {
		x = ax
	}
	if sy > 0 {
		y = ay
	}
	return core.CropRect{X: clampf(x, 0, 1-w), Y: clampf(y, 0, 1-h), W: w, H: h}
}

// cropForRatio is the largest crop at ratio centered on around, or around unchanged for none.
func cropForRatio(around core.CropRect, ratio float32, fixed bool, srcW, srcH uint32) core.CropRect {
	if !fixed {
		return around
	}
	r := normalizedRatio(ratio, srcW, srcH)
	w, h := r, float32(1)
	if r >= 1 {
		w, h = 1, 1/r
	}
	cx, cy := around.X+around.W/2, around.Y+around.H/2
	return core.CropRect{X: clampf(cx-w/2, 0, 1-w), Y: clampf(cy-h/2, 0, 1-h), W: w, H: h}
}

// timeScale maps output milliseconds to x in the timeline's tracks and back.
type timeScale struct{ startMs, msPerPx float64 }

func newTimeScale(durationMs float64, width float32, zoom, startMs float64) timeScale {
	visible := max(durationMs, 1) / max(zoom, 1)
	start := math.Max(0, math.Min(startMs, durationMs-visible))
	return timeScale{start, visible / float64(max(width, 1))}
}

func (s timeScale) x(ms float64) float32 { return float32((ms - s.startMs) / s.msPerPx) }
func (s timeScale) ms(x float32) float64 { return s.startMs + float64(x)*s.msPerPx }

// zoomAround is the start of a timeline zoomed to zoom that keeps anchorMs at anchorX.
func zoomAround(durationMs float64, width float32, zoom, anchorMs float64, anchorX float32) float64 {
	visible := durationMs / max(zoom, 1)
	return math.Max(0, math.Min(anchorMs-float64(anchorX)*visible/float64(max(width, 1)), durationMs-visible))
}

func formatTime(ms float64) string {
	tenths := uint64(math.Floor(math.Max(ms, 0) / 100))
	return fmt.Sprintf("%d:%02d.%d", tenths/600, tenths/10%60, tenths%10)
}

// fitSize is out scaled to fit bounds, with even sides of at least 2, as encoders and the
// compositor want.
func fitSize(outW, outH, boundW, boundH uint32) (uint32, uint32) {
	k := math.Min(float64(boundW)/float64(outW), float64(boundH)/float64(outH))
	even := func(v uint32) uint32 { return max(uint32(math.Floor(float64(v)*k))&^1, 2) }
	return even(outW), even(outH)
}

func clampf(v, lo, hi float32) float32 { return max(lo, min(v, hi)) }

func absf(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}
