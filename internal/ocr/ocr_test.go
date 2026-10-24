package ocr

import (
	"strings"
	"testing"
)

// line makes a line of single-space separated words, 10 pixels per character, at (x, y).
func line(text string, x, y, h float32) Line {
	var l Line
	for _, w := range strings.Fields(text) {
		r := Rect{x, y, float32(len(w)) * 10, h}
		l.Words = append(l.Words, Word{w, r})
		if len(l.Words) == 1 {
			l.Bounds = r
		} else {
			l.Bounds = l.Bounds.union(r)
		}
		x += r.W + 10
	}
	return l
}

func TestReadingOrderInterleavesColumnsRowByRow(t *testing.T) {
	lines := []Line{
		line("right two", 600, 62, 20),
		line("left one", 40, 20, 20),
		line("footer", 40, 140, 20),
		line("left two", 40, 60, 20),
		// Nine pixels below "left one" is still its row, since 9 is under half of 20.
		line("right one", 600, 29, 20),
	}
	sortReadingOrder(lines)
	want := "left one  right one\nleft two  right two\nfooter"
	if got := Text(lines); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestReadingOrderStartsANewRowAtHalfALineDown(t *testing.T) {
	lines := []Line{
		line("lower", 40, 30, 20),
		line("upper", 600, 20, 20),
	}
	sortReadingOrder(lines)
	if got, want := Text(lines), "upper\nlower"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestTextOfNoLinesIsEmpty(t *testing.T) {
	if got := Text(nil); got != "" {
		t.Fatalf("got %q", got)
	}
}
