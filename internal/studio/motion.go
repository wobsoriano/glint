package studio

import (
	"cmp"
	"math"
	"slices"
	"sort"

	"github.com/wobsoriano/glint/internal/media/input"
)

// The step constants are variables so derived values such as stepMs/1000 round at run time,
// the way the Rust build computes them, instead of being folded exactly by the compiler.
var (
	stepHz = 120.0
	stepMs = 1000.0 / stepHz
	dt     = 1.0 / stepHz
)

type spring struct{ pos, vel float64 }

type springK struct{ tension, friction, inertia float64 }

func (s *spring) snap(pos float64) { s.pos, s.vel = pos, 0 }

func (s *spring) step(target float64, k springK) {
	acc := (k.tension*(target-s.pos) - k.friction*s.vel) / k.inertia
	// The conversions keep arm64 from fusing these into FMAs, which would drift from the
	// reference integrator.
	s.vel += float64(acc * dt)
	s.pos += float64(s.vel * dt)
}

type lerper[T any] interface{ lerp(b T, f float64) T }

func stepCount(durationMs float64) int {
	return max(int(math.Ceil(max(durationMs/stepMs, 0)))+1, 2)
}

func sample[T lerper[T]](frames []T, tMs float64) (T, bool) {
	var zero T
	if len(frames) == 0 {
		return zero, false
	}
	last := len(frames) - 1
	pos := max(tMs/stepMs, 0)
	if pos >= float64(last) {
		return frames[last], true
	}
	i := int(pos)
	return frames[i].lerp(frames[i+1], pos-float64(i)), true
}

type PointerFrame struct {
	// Screen-master pixels.
	X, Y float64
	// Shrinks to 0.8 while a button is held.
	PressScale float64
	Cursor     uint32
}

func (a PointerFrame) lerp(b PointerFrame, f float64) PointerFrame {
	cursor := b.Cursor
	if f < 0.5 {
		cursor = a.Cursor
	}
	return PointerFrame{
		X:          a.X + (b.X-a.X)*f,
		Y:          a.Y + (b.Y-a.Y)*f,
		PressScale: a.PressScale + (b.PressScale-a.PressScale)*f,
		Cursor:     cursor,
	}
}

var (
	glideK     = springK{tension: 470, friction: 70, inertia: 3}
	interceptK = springK{tension: 538, friction: 40, inertia: 1}
	settleK    = springK{tension: 300, friction: 30, inertia: 0.3}
)

const (
	anticipationMs = 500.0
	interceptMs    = 175.0
	pressLeadMs    = 130.0
)

type pointerEvent struct {
	t, x, y float64
	cursor  uint32
}

func outputPointerEvents(p *Project, in *input.Timeline) []pointerEvent {
	src := in.Pointer
	var events []pointerEvent
	for _, s := range src {
		if t, ok := p.EventOutputTime(float64(s.T)); ok {
			events = append(events, pointerEvent{t, float64(s.X), float64(s.Y), s.Cursor})
		}
	}
	var clipStart float64
	for _, c := range p.Clips {
		i := sort.Search(len(src), func(i int) bool { return !(float64(src[i].T) <= c.StartMs) })
		if j := max(i-1, 0); j < len(src) {
			s := src[j]
			events = append(events, pointerEvent{clipStart, float64(s.X), float64(s.Y), s.Cursor})
		}
		clipStart += c.OutputLen()
	}
	slices.SortStableFunc(events, func(a, b pointerEvent) int { return cmp.Compare(a.t, b.t) })
	return events
}

type press struct{ t, up, x, y float64 }

// PointerTrack is the smoothed cursor on the output timeline.
type PointerTrack struct{ frames []PointerFrame }

func BuildPointerTrack(p *Project, in *input.Timeline) PointerTrack {
	events := outputPointerEvents(p, in)
	if len(events) == 0 {
		return PointerTrack{}
	}
	first := events[0]
	var presses []press
	for _, c := range in.Clicks {
		t, ok := p.EventOutputTime(float64(c.T))
		if !ok {
			continue
		}
		up, ok := p.EventOutputTime(float64(c.Up))
		if !ok || !(up >= t) {
			up = t + 150
		}
		presses = append(presses, press{t, up, float64(c.X), float64(c.Y)})
	}
	slices.SortStableFunc(presses, func(a, b press) int { return cmp.Compare(a.t, b.t) })
	var held []Span
	for _, pr := range presses {
		r := Span{pr.t - pressLeadMs, pr.up}
		if n := len(held); n > 0 && r.Start <= held[n-1].End {
			held[n-1].End = max(held[n-1].End, r.End)
		} else {
			held = append(held, r)
		}
	}
	hi := 0

	smoothing := float64(clamp(p.Design.Cursor.Smoothing, 0, 1))
	glide := glideK
	glide.inertia = max(glideK.inertia*smoothing*2.0, 0.6)
	raw := smoothing < 0.01

	n := stepCount(p.DurationMs())
	frames := make([]PointerFrame, 0, n)
	sx, sy := spring{pos: first.x}, spring{pos: first.y}
	scale := spring{pos: 1}
	ei, pi := 0, 0
	latest := first
	for k := range n {
		t := float64(k) * stepMs
		for ei < len(events) && events[ei].t <= t {
			latest = events[ei]
			ei++
		}
		for pi < len(presses) && presses[pi].t <= t {
			pi++
		}
		tx, ty, spr := latest.x, latest.y, glide
		if pi < len(presses) && presses[pi].t-t <= anticipationMs {
			tx, ty = presses[pi].x, presses[pi].y
			if presses[pi].t-t <= interceptMs {
				spr = interceptK
			}
		}
		if raw {
			sx.snap(latest.x)
			sy.snap(latest.y)
		} else {
			sx.step(tx, spr)
			sy.step(ty, spr)
		}
		for hi < len(held) && held[hi].End < t {
			hi++
		}
		pressed := hi < len(held) && held[hi].Start <= t
		target := 1.0
		if pressed {
			target = 0.8
		}
		scale.step(target, settleK)
		frames = append(frames, PointerFrame{X: sx.pos, Y: sy.pos, PressScale: scale.pos, Cursor: latest.cursor})
	}
	return PointerTrack{frames}
}

func (t PointerTrack) At(outputMs float64) (PointerFrame, bool) { return sample(t.frames, outputMs) }

// Motion holds every track a frame needs, built once per project edit.
type Motion struct {
	Camera []Viewport
	// Reframe is the pointer-following camera for Fill on a non-original aspect; it already
	// folds in zooms. Nil when the project does not reframe.
	Reframe  []Viewport
	Pointer  PointerTrack
	Clicks   ClickTrack
	Keys     KeyTrack
	Captions CaptionTrack
}

func BuildMotion(p *Project, in *input.Timeline, captions []CaptionWord) *Motion {
	pointer := BuildPointerTrack(p, in)
	camera := cameraTrack(p, in)
	return &Motion{
		Camera:   camera,
		Reframe:  reframeTrack(p, in, camera, &pointer),
		Pointer:  pointer,
		Clicks:   BuildClickTrack(p, in),
		Keys:     BuildKeyTrack(p, in),
		Captions: BuildCaptionTrack(p, captions),
	}
}

func (m *Motion) Viewport(outputMs float64) Viewport {
	frames := m.Camera
	if m.Reframe != nil {
		frames = m.Reframe
	}
	if v, ok := sample(frames, outputMs); ok {
		return v
	}
	return IdentityViewport
}

func toCrop(crop CropRect, size [2]uint32, x, y float64) (float64, float64) {
	nx := (x/float64(max(size[0], 1)) - float64(crop.X)) / float64(crop.W)
	ny := (y/float64(max(size[1], 1)) - float64(crop.Y)) / float64(crop.H)
	return nx, ny
}
