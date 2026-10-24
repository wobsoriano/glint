package overlay

import (
	"image"
	"testing"
)

func display() *selector {
	return newSelector(image.Pt(1920, 1080), []image.Rectangle{
		image.Rect(100, 100, 700, 500),  // front
		image.Rect(400, 300, 1400, 900), // behind it
	})
}

func drag(s *selector, from, to image.Point) Result {
	s.move(from)
	s.down(from)
	for i := 1; i <= 8; i++ {
		s.move(from.Add(to.Sub(from).Mul(i).Div(8)))
	}
	return s.up(to)
}

func TestDraggingSelectsTheAreaInEitherDirection(t *testing.T) {
	s := display()
	if got := drag(s, image.Pt(941, 683), image.Pt(301, 203)); got != (Result{Verdict: PickArea, Rect: image.Rect(301, 203, 941, 683)}) {
		t.Fatalf("got %+v", got)
	}
}

func TestASmallMoveIsAClickOnTheWindowInFront(t *testing.T) {
	s := display()
	got := drag(s, image.Pt(500, 400), image.Pt(502, 401))
	if got.Verdict != PickWindow || got.Window != 0 || got.Rect != image.Rect(100, 100, 700, 500) {
		t.Fatalf("got %+v", got)
	}
	if got := drag(display(), image.Pt(1600, 50), image.Pt(1601, 50)); got.Verdict != Continue {
		t.Fatalf("a click on no window picks nothing: %+v", got)
	}
}

func TestHoverHighlightsTheWindowInFrontUntilADragStarts(t *testing.T) {
	s := display()
	s.move(image.Pt(1000, 600))
	if r, sel, ok := s.focus(); !ok || sel || r != image.Rect(400, 300, 1400, 900) {
		t.Fatalf("hover focus %v %v %v", r, sel, ok)
	}
	s.down(image.Pt(1000, 600))
	s.move(image.Pt(1100, 700))
	if r, sel, ok := s.focus(); !ok || !sel || r != image.Rect(1000, 600, 1100, 700) {
		t.Fatalf("drag focus %v %v %v", r, sel, ok)
	}
}

func TestWindowModePicksWindowsEvenWhenDragged(t *testing.T) {
	s := display()
	s.key(KeySpace)
	if !s.windowMode {
		t.Fatal("Space switches to window mode")
	}
	got := drag(s, image.Pt(1300, 850), image.Pt(1200, 700))
	if got.Verdict != PickWindow || got.Window != 1 {
		t.Fatalf("got %+v", got)
	}
	s.key(KeySpace)
	if s.windowMode {
		t.Fatal("Space switches back to area mode")
	}
}

func TestPicksAreClippedToTheDisplayAndSliversRefused(t *testing.T) {
	s := newSelector(image.Pt(800, 600), []image.Rectangle{image.Rect(-50, -40, 300, 200)})
	if got := drag(s, image.Pt(10, 10), image.Pt(12, 11)); got.Rect != image.Rect(0, 0, 300, 200) {
		t.Fatalf("window clipped to %v", got.Rect)
	}
	s = newSelector(image.Pt(800, 600), nil)
	s.move(image.Pt(100, 100))
	s.down(image.Pt(100, 100))
	s.move(image.Pt(101, 200))
	if got := s.up(image.Pt(101, 200)); got.Verdict != Continue {
		t.Fatalf("a 1 px wide selection is refused: %+v", got)
	}
}

func TestKeys(t *testing.T) {
	s := display()
	if got := s.key(KeyEscape); got.Verdict != Cancel {
		t.Fatalf("Escape: %+v", got)
	}
	if got := s.key(KeyEnter); got != (Result{Verdict: PickArea, Rect: image.Rect(0, 0, 1920, 1080)}) {
		t.Fatalf("Enter with no selection takes the display: %+v", got)
	}
	s.down(image.Pt(10, 10))
	s.move(image.Pt(110, 60))
	if got := s.key(KeyF); got.Rect != image.Rect(10, 10, 110, 60) {
		t.Fatalf("F mid-drag takes the selection: %+v", got)
	}
	if got := s.key(KeyOther); got.Verdict != Continue {
		t.Fatalf("other keys: %+v", got)
	}
}
