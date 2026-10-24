package transcribe

import (
	"slices"
	"testing"
)

// levels is n frames of a -60 dBFS floor with sound at -20 dBFS over spans of frames.
func levels(n int, spans ...soundRun) []float64 {
	l := slices.Repeat([]float64{-60}, n)
	for _, s := range spans {
		for f := s.start; f < s.end; f++ {
			l[f] = -20
		}
	}
	return l
}

func TestSoundRunsJoinWordsAndBreakAtPauses(t *testing.T) {
	l := levels(600, soundRun{100, 150}, soundRun{160, 200}, soundRun{300, 400})
	eq(t, soundRuns(l, noiseFloor(l)), []soundRun{{100, 200}, {300, 400}})
	eq(t, soundRuns(levels(600), -60), []soundRun(nil))
}

func TestSoundLongerThanAWindowIsCutAtItsQuietestFrame(t *testing.T) {
	l := levels(5200, soundRun{100, 5100})
	l[2000] = -40
	runs := soundRuns(l, -60)
	if runs[0] != (soundRun{100, 2000}) || runs[len(runs)-1].end != 5100 {
		t.Fatalf("got %v", runs)
	}
	for i, r := range runs {
		if r.end-r.start > windowFrames-2*padFrames || i > 0 && r.start != runs[i-1].end {
			t.Fatalf("got %v", runs)
		}
	}
}

func TestWindowsHoldRunsUpToALongPauseOrTheirLength(t *testing.T) {
	runs := []soundRun{{100, 300}, {400, 600}, {1200, 1400}, {1500, 3000}, {3100, 4300}}
	type window struct{ n, from, to int }
	got := func(lo int) window {
		n, from, to := nextChunk(runs, lo, len(runs), 4400)
		return window{n, from, to}
	}
	eq(t, got(0), window{2, 100 - padFrames, 600 + padFrames})
	eq(t, got(2), window{2, 1200 - padFrames, 3000 + padFrames})
	eq(t, got(4), window{1, 3100 - padFrames, 4300 + padFrames})
	if n, _, to := nextChunk(runs, 0, 1, 4400); n != 1 || to != 300+padFrames {
		t.Fatalf("a window past hi: %d runs to frame %d", n, to)
	}
	near := []soundRun{{100, 300}, {310, 500}}
	if _, from, _ := nextChunk(near, 1, 2, 600); from != 305 {
		t.Fatalf("a window reaches back to frame %d, past the middle of the pause", from)
	}
}

func TestUnheardFindsWhereADecodeStoppedOrSkipped(t *testing.T) {
	runs := []soundRun{{100, 200}, {300, 400}, {500, 560}, {700, 900}}
	cases := []struct {
		name  string
		at    []int64
		done  int
		holes [][2]int
	}{
		{"all heard", []int64{1500, 3500, 5500, 8000}, 4, nil},
		{"stopped after the second run", []int64{1200, 1500, 3200}, 2, nil},
		{"skipped the first three runs", []int64{7500, 8000}, 4, [][2]int{{0, 3}}},
		{"missed one short run", []int64{1500, 3500, 8000}, 4, nil},
		{"heard nothing", nil, 4, nil},
		{"heard only in a pause", []int64{2500}, 4, nil},
	}
	for _, c := range cases {
		done, holes := unheard(runs, c.at)
		if done != c.done || !slices.Equal(holes, c.holes) {
			t.Errorf("%s: done %d, holes %v; want %d, %v", c.name, done, holes, c.done, c.holes)
		}
	}
}
