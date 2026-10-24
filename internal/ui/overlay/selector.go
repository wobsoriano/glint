package overlay

import "image"

// dragSlop is how far, in physical pixels, a press must move before it selects an area
// rather than clicking a window, as in the Rust build.
const dragSlop = 3

// minSide is the smallest side, in physical pixels, a capture may have.
const minSide = 2

// Key is a key the overlay acts on.
type Key int

const (
	KeyOther Key = iota
	KeyEscape
	KeySpace
	KeyEnter
	KeyF
)

// Verdict is what an input decided.
type Verdict int

const (
	Continue Verdict = iota
	Cancel
	PickArea
	PickWindow
)

// Result is a Verdict with what was picked: Rect for an area, Window (an index into the
// windows the selector was given) for a window. Rect is in the display's own pixels.
type Result struct {
	Verdict Verdict
	Rect    image.Rectangle
	Window  int
}

type phase int

const (
	idle phase = iota
	pressed
	dragging
)

// selector is one display's selection, in that display's physical pixels with the origin at
// its top-left corner. It holds no window or drawing state, so it runs in tests as is.
type selector struct {
	size    image.Point
	windows []image.Rectangle // front to back, in the display's pixels
	// windowMode picks only windows: Space switches to it and back.
	windowMode bool

	phase  phase
	press  image.Point
	cursor image.Point
	inside bool
	sel    image.Rectangle
	hover  int
}

func newSelector(size image.Point, windows []image.Rectangle) *selector {
	return &selector{size: size, windows: windows, hover: -1}
}

func (s *selector) windowAt(p image.Point) int {
	for i, w := range s.windows {
		if p.In(w) {
			return i
		}
	}
	return -1
}

func (s *selector) down(p image.Point) {
	s.phase, s.press, s.cursor, s.inside = pressed, p, p, true
	s.sel = image.Rectangle{}
}

func (s *selector) move(p image.Point) {
	s.cursor, s.inside = p, true
	switch s.phase {
	case idle:
		s.hover = s.windowAt(p)
	case pressed, dragging:
		if s.windowMode {
			s.hover = s.windowAt(p)
			return
		}
		r := image.Rectangle{s.press, p}.Canon()
		if r.Dx() > dragSlop || r.Dy() > dragSlop {
			s.phase, s.sel, s.hover = dragging, r, -1
		}
	}
}

func (s *selector) up(p image.Point) Result {
	if s.phase == idle {
		return Result{}
	}
	dragged := s.phase == dragging
	s.phase = idle
	if dragged {
		return s.complete(s.sel, -1)
	}
	if i := s.windowAt(p); i >= 0 {
		return s.complete(s.windows[i], i)
	}
	return Result{}
}

func (s *selector) key(k Key) Result {
	switch k {
	case KeyEscape:
		return Result{Verdict: Cancel}
	case KeySpace:
		s.windowMode = !s.windowMode
		s.phase, s.sel = idle, image.Rectangle{}
		s.hover = s.windowAt(s.cursor)
	case KeyEnter, KeyF:
		r := s.sel
		if r.Empty() {
			r = image.Rectangle{Max: s.size}
		}
		return s.complete(r, -1)
	}
	return Result{}
}

// complete clips r to the display and refuses slivers.
func (s *selector) complete(r image.Rectangle, window int) Result {
	r = r.Intersect(image.Rectangle{Max: s.size})
	if r.Dx() < minSide || r.Dy() < minSide {
		return Result{}
	}
	if window >= 0 {
		return Result{Verdict: PickWindow, Rect: r, Window: window}
	}
	return Result{Verdict: PickArea, Rect: r}
}

// focus is the rectangle the overlay leaves undimmed: the selection while dragging, else the
// window under the pointer. isSelection tells which.
func (s *selector) focus() (r image.Rectangle, isSelection bool, ok bool) {
	if s.phase == dragging {
		return s.sel, true, true
	}
	if s.hover >= 0 && s.inside {
		return s.windows[s.hover].Intersect(image.Rectangle{Max: s.size}), false, true
	}
	return image.Rectangle{}, false, false
}

func (s *selector) leave() {
	if s.phase == idle {
		s.inside, s.hover = false, -1
	}
}
