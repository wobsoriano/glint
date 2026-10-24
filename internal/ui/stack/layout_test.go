package stack

import (
	"slices"
	"testing"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
)

var workArea = platform.Rect{X: 0, Y: 0, W: 1920, H: 1032}

func TestStackSitsInTheBottomCornerOfItsSide(t *testing.T) {
	for _, tc := range []struct {
		name      string
		side      settings.Side
		scale     float64
		n         int
		collapsed bool
		want      platform.Rect
	}{
		// 244 x (20 + 28 + 12 + 3*160 - 12) = 244 x 528, 12 px in from the corner.
		{"left", settings.SideLeft, 1, 3, false, platform.Rect{X: 12, Y: 1032 - 528 - 12, W: 244, H: 528}},
		{"right", settings.SideRight, 1, 3, false, platform.Rect{X: 1920 - 244 - 12, Y: 1032 - 528 - 12, W: 244, H: 528}},
		{"one card at 150%", settings.SideLeft, 1.5, 1, false, platform.Rect{X: 18, Y: 1032 - 312 - 18, W: 366, H: 312}},
		// The tab is flush with the edge and lifted by 18% of the work area.
		{"peek left", settings.SideLeft, 1, 3, true, platform.Rect{X: 0, Y: 1032 - 84 - 185, W: 30, H: 84}},
		{"peek right", settings.SideRight, 1, 3, true, platform.Rect{X: 1920 - 30, Y: 1032 - 84 - 185, W: 30, H: 84}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := stackRect(workArea, tc.scale, tc.side, tc.n, tc.collapsed); got != tc.want {
				t.Errorf("stackRect = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestStackFollowsAWorkAreaOffTheOrigin(t *testing.T) {
	area := platform.Rect{X: -1280, Y: 200, W: 1280, H: 984}
	got := stackRect(area, 1, settings.SideRight, 1, false)
	want := platform.Rect{X: -244 - 12, Y: 200 + 984 - 208 - 12, W: 244, H: 208}
	if got != want {
		t.Errorf("stackRect = %+v, want %+v", got, want)
	}
}

func TestCapacityKeepsTheStackOnScreen(t *testing.T) {
	for _, h := range []float64{1032, 600, 100} {
		n := capacity(h)
		if n < 1 {
			t.Fatalf("capacity(%v) = %d, want at least one card", h, n)
		}
		if _, sh := stackSize(n, false); n > 1 && sh+2*edge > h {
			t.Errorf("capacity(%v) = %d cards, %v DIPs tall with its margins", h, n, sh+2*edge)
		}
		if _, sh := stackSize(n+1, false); sh+2*edge <= h {
			t.Errorf("capacity(%v) = %d, but %d cards fit", h, n, n+1)
		}
	}
}

func TestPastEdgeIsTheEdgeOfTheStacksSide(t *testing.T) {
	if !pastEdge(0, workArea, settings.SideLeft) || pastEdge(40, workArea, settings.SideLeft) {
		t.Error("left: the leftmost pixels dismiss and the cards' own area does not")
	}
	if !pastEdge(1919, workArea, settings.SideRight) || pastEdge(1880, workArea, settings.SideRight) || pastEdge(0, workArea, settings.SideRight) {
		t.Error("right: only the rightmost pixels dismiss")
	}
}

func TestSlotsFollowTheLayout(t *testing.T) {
	s := SlotsFor(settings.DefaultCardLayout(), false)
	want := [4]settings.CardAction{settings.CardDismiss, settings.CardEdit, settings.CardPin, settings.CardMore}
	if s.Corners != want {
		t.Errorf("corners = %v, want %v", s.Corners, want)
	}
	if !slices.Equal(s.Center, []settings.CardAction{settings.CardCopy, settings.CardSave}) {
		t.Errorf("center = %v", s.Center)
	}
	wantMenu := []settings.CardAction{settings.CardSaveAs, settings.CardCompress, settings.CardQuickLook, settings.CardCopyText, settings.CardDelete}
	if !slices.Equal(s.Menu, wantMenu) {
		t.Errorf("menu = %v, want %v", s.Menu, wantMenu)
	}
}

func TestSlotsMoveWithPlace(t *testing.T) {
	l := settings.DefaultCardLayout()
	l.Place(settings.CardDelete, settings.CornerZone(settings.BottomLeft), -1)
	l.Place(settings.CardQuickLook, settings.CenterZone, 0)
	s := SlotsFor(l, false)
	if s.Corners[settings.BottomLeft] != settings.CardDelete {
		t.Errorf("bottom left = %q, want delete", s.Corners[settings.BottomLeft])
	}
	if !slices.Equal(s.Center, []settings.CardAction{settings.CardQuickLook, settings.CardCopy, settings.CardSave}) {
		t.Errorf("center = %v", s.Center)
	}
	if !slices.Contains(s.Menu, settings.CardPin) || slices.Contains(s.Menu, settings.CardDelete) {
		t.Errorf("menu = %v: the corner's old pin goes behind More, and delete leaves it", s.Menu)
	}
}

func TestRecordingsLeaveOutImageActions(t *testing.T) {
	l := settings.DefaultCardLayout()
	l.Place(settings.CardCopyText, settings.CenterZone, -1)
	s := SlotsFor(l, true)
	for _, a := range []settings.CardAction{settings.CardPin, settings.CardCompress, settings.CardCopyText} {
		if s.Has(a) {
			t.Errorf("a recording's card shows %q", a)
		}
	}
	if s.Corners[settings.BottomLeft] != "" {
		t.Errorf("the pin corner holds %q on a recording, want it empty", s.Corners[settings.BottomLeft])
	}
	for _, a := range []settings.CardAction{settings.CardCopy, settings.CardSave, settings.CardQuickLook, settings.CardEdit, settings.CardDelete} {
		if !s.Has(a) {
			t.Errorf("a recording's card lacks %q", a)
		}
	}
}

func TestCountLabel(t *testing.T) {
	for _, tc := range []struct {
		rec, n int
		want   string
	}{{0, 1, "1 Screenshot"}, {0, 2, "2 Screenshots"}, {1, 1, "1 Recording"}, {2, 2, "2 Recordings"}, {1, 3, "3 Captures"}} {
		if got := countLabel(tc.rec, tc.n); got != tc.want {
			t.Errorf("countLabel(%d, %d) = %q, want %q", tc.rec, tc.n, got, tc.want)
		}
	}
}

func TestClock(t *testing.T) {
	for ms, want := range map[uint64]string{0: "00:00", 65_400: "01:05", 3_725_000: "1:02:05"} {
		if got := clock(ms); got != want {
			t.Errorf("clock(%d) = %q, want %q", ms, got, want)
		}
	}
}
