// Package ocr recognizes text with the engine built into the system, Windows.Media.Ocr on
// Windows, and puts the lines in the reading order Glint copies them in.
package ocr

import (
	"cmp"
	"slices"
	"strings"
)

// Rect is in pixels of the recognized image, in the image's own coordinate space.
type Rect struct{ X, Y, W, H float32 }

func (r Rect) union(o Rect) Rect {
	x0, y0 := min(r.X, o.X), min(r.Y, o.Y)
	x1, y1 := max(r.X+r.W, o.X+o.W), max(r.Y+r.H, o.Y+o.H)
	return Rect{x0, y0, x1 - x0, y1 - y0}
}

// overlap is the area r and o share.
func (r Rect) overlap(o Rect) float32 {
	w := min(r.X+r.W, o.X+o.W) - max(r.X, o.X)
	h := min(r.Y+r.H, o.Y+o.H) - max(r.Y, o.Y)
	if w <= 0 || h <= 0 {
		return 0
	}
	return w * h
}

func (r Rect) area() float32 { return r.W * r.H }

type Word struct {
	Text   string
	Bounds Rect
}

// Line is one run of words the engine read together. Bounds is the union of the words'.
type Line struct {
	Words  []Word
	Bounds Rect
}

// Text is the line's words separated by single spaces.
func (l Line) Text() string {
	parts := make([]string, len(l.Words))
	for i, w := range l.Words {
		parts[i] = w.Text
	}
	return strings.Join(parts, " ")
}

// Text is lines as plain text: lines that share a row are joined by two spaces, rows by a
// newline. Pass lines in the order Recognize returns them.
func Text(lines []Line) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			if sameRow(lines[i-1].Bounds, l.Bounds) {
				b.WriteString("  ")
			} else {
				b.WriteByte('\n')
			}
		}
		b.WriteString(l.Text())
	}
	return b.String()
}

// sameRow reports whether two lines' tops are within half the shorter line's height.
func sameRow(a, b Rect) bool {
	d := a.Y - b.Y
	if d < 0 {
		d = -d
	}
	return d < min(a.H, b.H)*0.5
}

// sortReadingOrder orders lines top to bottom, and left to right within a row.
func sortReadingOrder(lines []Line) {
	slices.SortStableFunc(lines, func(a, b Line) int {
		if sameRow(a.Bounds, b.Bounds) {
			return cmp.Compare(a.Bounds.X, b.Bounds.X)
		}
		return cmp.Compare(a.Bounds.Y, b.Bounds.Y)
	})
}
