package editor

import "github.com/wobsoriano/glint/internal/render"

// Viewport sizes are in device pixels, so 100% shows one image pixel per screen pixel at any
// display scale.
const (
	maxZoom   float32 = 10
	minZoom   float32 = 0.05
	fitMargin float32 = 28
)

var zoomSteps = [16]float32{0.05, 0.1, 0.25, 1.0 / 3.0, 0.5, 2.0 / 3.0, 0.75, 1, 1.25, 1.5, 2, 3, 4, 6, 8, 10}

// viewport is how the user zoomed and panned the canvas: fit when zoom is nil.
type viewport struct {
	zoom   *float32
	center render.Pt
}

// view is a resolved viewport: device pixels per canvas pixel, and the canvas point at the
// top-left of the view.
type view struct {
	zoom   float32
	origin render.Pt
}

func (v view) toScreen(p render.Pt) render.Pt {
	return render.Pt{X: (p.X - v.origin.X) * v.zoom, Y: (p.Y - v.origin.Y) * v.zoom}
}

func (v view) toCanvas(s render.Pt) render.Pt {
	return render.Pt{X: s.X/v.zoom + v.origin.X, Y: s.Y/v.zoom + v.origin.Y}
}

type size struct{ w, h float32 }

func zoomStep(zoom float32, up bool) float32 {
	if up {
		for _, s := range zoomSteps {
			if s > zoom*1.01 {
				return s
			}
		}
		return maxZoom
	}
	for i := len(zoomSteps) - 1; i >= 0; i-- {
		if zoomSteps[i] < zoom*0.99 {
			return zoomSteps[i]
		}
	}
	return zoomSteps[0]
}

func fitZoom(canvas, vs size, sf float32) float32 {
	m := 2 * fitMargin * sf
	return min(max(vs.w-m, 40)/canvas.w, max(vs.h-m, 40)/canvas.h, 1)
}

func (vp viewport) resolve(canvas, vs size, sf float32) view {
	zoom := fitZoom(canvas, vs, sf)
	if vp.zoom != nil {
		zoom = *vp.zoom
	}
	axis := func(c, length, v float32) float32 {
		half := v / zoom / 2
		if length*zoom <= v {
			return length / 2
		}
		return min(max(c, half), length-half)
	}
	center := render.Pt{X: canvas.w / 2, Y: canvas.h / 2}
	if vp.zoom != nil {
		center = render.Pt{X: axis(vp.center.X, canvas.w, vs.w), Y: axis(vp.center.Y, canvas.h, vs.h)}
	}
	return view{zoom: zoom, origin: render.Pt{X: center.X - vs.w/zoom/2, Y: center.Y - vs.h/zoom/2}}
}

// zoomTo zooms to zoom keeping the canvas point under anchor (view pixels) where it is.
func (vp *viewport) zoomTo(zoom float32, anchor render.Pt, canvas, vs size, sf float32) {
	cur := vp.resolve(canvas, vs, sf)
	zoom = min(max(zoom, min(minZoom, cur.zoom)), maxZoom)
	fixed := cur.toCanvas(anchor)
	origin := render.Pt{X: fixed.X - anchor.X/zoom, Y: fixed.Y - anchor.Y/zoom}
	vp.zoom = &zoom
	vp.center = render.Pt{X: origin.X + vs.w/zoom/2, Y: origin.Y + vs.h/zoom/2}
	vp.settle(canvas, vs, sf)
}

// pan moves the view by (dx, dy) view pixels; it stops at the canvas edges.
func (vp *viewport) pan(dx, dy float32, canvas, vs size, sf float32) {
	v := vp.resolve(canvas, vs, sf)
	z := v.zoom
	vp.zoom = &z
	vp.center = render.Pt{X: v.origin.X + vs.w/z/2 + dx/z, Y: v.origin.Y + vs.h/z/2 + dy/z}
	vp.settle(canvas, vs, sf)
}

func (vp *viewport) settle(canvas, vs size, sf float32) {
	v := vp.resolve(canvas, vs, sf)
	vp.center = render.Pt{X: v.origin.X + vs.w/v.zoom/2, Y: v.origin.Y + vs.h/v.zoom/2}
}
