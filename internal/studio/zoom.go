package studio

import (
	"cmp"
	"math"
	"slices"
	"sort"

	"github.com/wobsoriano/glint/internal/media/input"
)

// Viewport is a virtual camera state. X and Y are the point, normalized inside the cropped
// screen, that sits at the card center; Scale is the magnification (1 shows the whole crop).
type Viewport struct{ Scale, X, Y float64 }

var IdentityViewport = Viewport{Scale: 1, X: 0.5, Y: 0.5}

func (a Viewport) lerp(b Viewport, f float64) Viewport {
	return Viewport{
		Scale: a.Scale + (b.Scale-a.Scale)*f,
		X:     a.X + (b.X-a.X)*f,
		Y:     a.Y + (b.Y-a.Y)*f,
	}
}

const (
	leadInMs        = 300.0
	holdMs          = 2_500.0
	joinMs          = 2_500.0
	tailExclusionMs = 1_000.0
	trailingGuardMs = 800.0
	AutoZoomScale   = float32(1.5)
)

// AutoZooms builds auto zooms in source time from the recorded clicks: each click inside the
// frame gets a lead-in and hold, and zooms that overlap or nearly touch merge into one.
func AutoZooms(in *input.Timeline, sourceDurationMs float64) []Zoom {
	w, h := float32(in.Size[0]), float32(in.Size[1])
	var clicks []input.Click
	for _, c := range in.Clicks {
		if float64(c.T) < sourceDurationMs-tailExclusionMs && c.X >= 0 && c.X <= w && c.Y >= 0 && c.Y <= h {
			clicks = append(clicks, c)
		}
	}
	slices.SortStableFunc(clicks, func(a, b input.Click) int { return cmp.Compare(a.T, b.T) })
	var zooms []Zoom
	for _, c := range clicks {
		start := max(float64(c.T)-leadInMs, 0)
		end := min(float64(c.T)+holdMs, sourceDurationMs-trailingGuardMs)
		if end <= start {
			continue
		}
		if n := len(zooms); n > 0 && start <= zooms[n-1].EndMs+joinMs {
			zooms[n-1].EndMs = max(zooms[n-1].EndMs, end)
		} else {
			zooms = append(zooms, Zoom{StartMs: start, EndMs: end, Scale: AutoZoomScale, Enabled: true, Auto: true})
		}
	}
	return zooms
}

// ApplyAutoZoom regenerates auto zooms from the clicks, keeping manual ones.
func (p *Project) ApplyAutoZoom(in *input.Timeline, sourceDurationMs float64) {
	p.Zooms = slices.DeleteFunc(p.Zooms, func(z Zoom) bool { return z.Auto })
	p.Zooms = append(p.Zooms, AutoZooms(in, sourceDurationMs)...)
	slices.SortStableFunc(p.Zooms, func(a, b Zoom) int { return cmp.Compare(a.StartMs, b.StartMs) })
}

var cameraK = springK{tension: 200, friction: 40, inertia: 2.25}

const (
	comfortWidths = 1.4
	boundsBias    = 0.25
	clickGuardMs  = 150.0
	clickMargin   = 0.9
)

// activeZoom picks the zoom that drives the camera at sourceMs: manual over auto, fixed over
// pointer, then the latest start. Ties go to the later zoom, as Rust's max_by does.
func activeZoom(zooms []Zoom, sourceMs float64) *Zoom {
	rank := func(z *Zoom) int {
		r := 0
		if !z.Auto {
			r += 2
		}
		if z.Focus.Kind == FocusFixed {
			r++
		}
		return r
	}
	var best *Zoom
	for i := range zooms {
		z := &zooms[i]
		if !z.Enabled || sourceMs < z.StartMs || sourceMs > z.EndMs {
			continue
		}
		if best == nil {
			best = z
			continue
		}
		c := rank(z) - rank(best)
		if c == 0 {
			c = cmp.Compare(z.StartMs, best.StartMs)
		}
		if c >= 0 {
			best = z
		}
	}
	return best
}

func clampToFrame(v, scale float64) float64 {
	half := 0.5 / max(scale, 1)
	return clamp(v, half, 1-half)
}

type timedPoint struct{ t, x, y float64 }

func cameraTrack(p *Project, in *input.Timeline) []Viewport {
	n := stepCount(p.DurationMs())
	if !slices.ContainsFunc(p.Zooms, func(z Zoom) bool { return z.Enabled }) {
		return []Viewport{IdentityViewport, IdentityViewport}
	}
	inside := func(x, y float64) bool { return x >= 0 && x <= 1 && y >= 0 && y <= 1 }
	var pointer []timedPoint
	for _, s := range in.Pointer {
		x, y := toCrop(p.Crop, in.Size, float64(s.X), float64(s.Y))
		if inside(x, y) {
			pointer = append(pointer, timedPoint{float64(s.T), x, y})
		}
	}
	var clicks []timedPoint
	for _, c := range in.Clicks {
		x, y := toCrop(p.Crop, in.Size, float64(c.X), float64(c.Y))
		if _, ok := p.EventOutputTime(float64(c.T)); inside(x, y) && ok {
			clicks = append(clicks, timedPoint{float64(c.T), x, y})
		}
	}
	slices.SortStableFunc(clicks, func(a, b timedPoint) int { return cmp.Compare(a.t, b.t) })

	half, ax, ay := spring{pos: 0.5}, spring{pos: 0.5}, spring{pos: 0.5}
	frames := make([]Viewport, 0, n)
	for k := range n {
		src := p.SourceTime(float64(k) * stepMs)
		active := activeZoom(p.Zooms, src)
		scale := 1.0
		if active != nil {
			scale = max(float64(active.Scale), 1)
		}
		tx, ty := 0.5, 0.5
		switch {
		case active == nil:
		case active.Focus.Kind == FocusFixed:
			tx, ty = clampToFrame(float64(active.Focus.X), scale), clampToFrame(float64(active.Focus.Y), scale)
		default:
			i := sort.Search(len(pointer), func(i int) bool { return !(pointer[i].t <= src) })
			px, py := 0.5, 0.5
			if j := max(i-1, 0); j < len(pointer) {
				px, py = pointer[j].x, pointer[j].y
			}
			keep := func(v float64) float64 {
				h := 0.5 / scale
				return v + (h+v*(1-2*h)-v)*boundsBias
			}
			tx, ty = clampToFrame(keep(px), scale), clampToFrame(keep(py), scale)
		}
		travel := math.Hypot(tx-ax.pos, ty-ay.pos)
		pursuit := scale
		if travel > 1e-4 {
			pursuit = min(scale, max(comfortWidths/travel, 1))
		}
		if k > 0 {
			half.step(0.5/pursuit, cameraK)
			ax.step(tx, cameraK)
			ay.step(ty, cameraK)
		}
		s := max(0.5/clamp(half.pos, 1e-6, 0.5), 1)
		x, y := clampToFrame(ax.pos, s), clampToFrame(ay.pos, s)

		guarded := active != nil && active.Focus.Kind == FocusPointer
		ci := sort.Search(len(clicks), func(i int) bool { return !(clicks[i].t <= src) })
		if guarded && ci > 0 {
			if c := clicks[ci-1]; src-c.t <= clickGuardMs {
				x = keepVisible(c.x, x, s)
				y = keepVisible(c.y, y, s)
				ax.pos, ay.pos = x, y
			}
		}
		frames = append(frames, Viewport{Scale: s, X: x, Y: y})
	}
	return frames
}

func keepVisible(p, anchor, scale float64) float64 {
	half := 0.5 / scale
	safe := half * clickMargin
	lo, hi := max(p-safe, half), min(p+safe, 1-half)
	if lo <= hi {
		return clamp(anchor, lo, hi)
	}
	return clamp(anchor, half, 1-half)
}

func cardAspect(p *Project, canvasAspect float64) float64 {
	pad := float64(clamp(p.Design.Padding, 0, 0.475))
	short := min(canvasAspect, 1)
	return (canvasAspect - 2*pad*short) / (1 - 2*pad*short)
}

const (
	deadZone    = 0.28
	reframeTauS = 0.45
	handoffSpan = 0.2
)

func reframeTrack(p *Project, in *input.Timeline, camera []Viewport, pointer *PointerTrack) []Viewport {
	r, ok := p.Aspect.Ratio()
	if !ok || p.AspectMode != AspectFill {
		return nil
	}
	ratio := float64(r)
	w, h := in.Size[0], in.Size[1]
	content := (float64(w) * float64(p.Crop.W)) / max(float64(h)*float64(p.Crop.H), 1)
	card := cardAspect(p, ratio)
	fillScale := max(card/content, 1)
	fillW, fillH := content*fillScale, fillScale
	focus := func(t float64) (float64, float64, bool) {
		f, ok := pointer.At(t)
		if !ok {
			return 0, 0, false
		}
		x, y := toCrop(p.Crop, in.Size, f.X, f.Y)
		return x, y, true
	}

	n := stepCount(p.DurationMs())
	kSmooth := 1 - math.Exp(-(stepMs/1000)/reframeTauS)
	cx, cy, ok := focus(0)
	if !ok {
		cx, cy = 0.5, 0.5
	}
	follow := func(c, f, window float64) float64 {
		switch {
		case f > c+window:
			return f - window
		case f < c-window:
			return f + window
		}
		return c
	}
	frames := make([]Viewport, 0, n)
	for k := range n {
		t := float64(k) * stepMs
		vp, ok := sample(camera, t)
		if !ok {
			vp = IdentityViewport
		}
		m := max(vp.Scale, 1)
		visX := min(card/(fillW*m), 1)
		visY := min(1/(fillH*m), 1)
		fx, fy, ok := focus(t)
		if !ok {
			fx, fy = vp.X, vp.Y
		}
		cx += (follow(cx, fx, visX*deadZone) - cx) * kSmooth
		cy += (follow(cy, fy, visY*deadZone) - cy) * kSmooth
		hand := clamp((m-1)/handoffSpan, 0, 1)
		fit := func(c, z, vis float64) float64 {
			if vis >= 1 {
				return 0.5
			}
			return clamp(c+(z-c)*hand, vis/2, 1-vis/2)
		}
		cx, cy = fit(cx, vp.X, visX), fit(cy, vp.Y, visY)
		frames = append(frames, Viewport{Scale: m, X: cx, Y: cy})
	}
	return frames
}
