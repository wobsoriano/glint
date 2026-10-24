package media

import (
	"testing"
	"time"
)

func TestFramesLandInTheSlotNearestToWhenTheyWereShown(t *testing.T) {
	ms := func(f float64) time.Duration { return time.Duration(f * float64(time.Millisecond)) }
	cases := []struct {
		shown time.Duration
		fps   FPS
		first int64
	}{
		{0, FPS60, 0},
		{ms(8.3), FPS60, 0},
		{ms(8.4), FPS60, 1},
		{ms(1063.1), FPS60, 64},
		{ms(1079.8), FPS60, 65},
		{ms(16.6), FPS30, 0},
		{ms(16.7), FPS30, 1},
		{-time.Second, FPS30, 0},
	}
	for _, c := range cases {
		if got := slotsBefore(c.shown, c.fps); got != c.first {
			t.Errorf("a frame shown at %v first fills slot %d at %d fps, want %d", c.shown, got, c.fps, c.first)
		}
	}
}
