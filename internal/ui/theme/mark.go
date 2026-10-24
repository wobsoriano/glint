package theme

import (
	"fmt"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

// The mark, "Glare": two warm streaks of light across a dark glass squircle, on the 64-unit grid
// of the Rust build's src/mark.rs.
var streaks = [2]struct {
	pts     [4][2]float64
	opacity float64
}{
	{[4][2]float64{{30, -6}, {46, -6}, {14, 70}, {-2, 70}}, 1},
	{[4][2]float64{{52, -6}, {58, -6}, {26, 70}, {20, 70}}, 0.75},
}

// MarkSource returns the mark as an SVG document 64 units square. A template mark is white
// streaks inside a white outline on transparency, which macOS tints for the menu bar.
func MarkSource(template bool) string {
	card := roundedRect(2, 2, 60, 60, 15)
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="64" height="64" viewBox="0 0 64 64"><defs>`)
	b.WriteString(`<linearGradient id="bg" gradientUnits="userSpaceOnUse" x1="2" y1="2" x2="62" y2="62"><stop offset="0" stop-color="#1D2033"/><stop offset="1" stop-color="#0E0F18"/></linearGradient>`)
	for i, s := range streaks {
		from, to := "#FFD27A", "#FF7A59"
		if template {
			from, to = "#FFFFFF", "#FFFFFF"
		}
		fmt.Fprintf(&b, `<linearGradient id="s%d" gradientUnits="userSpaceOnUse" x1="%g" y1="0" x2="%g" y2="0"><stop offset="0" stop-color="%s" stop-opacity="%g"/><stop offset="1" stop-color="%s" stop-opacity="%g"/></linearGradient>`,
			i, s.pts[3][0], s.pts[1][0], from, s.opacity, to, s.opacity)
	}
	fmt.Fprintf(&b, `<clipPath id="card"><path d="%s"/></clipPath></defs>`, card)
	if !template {
		fmt.Fprintf(&b, `<path d="%s" fill="url(#bg)"/>`, card)
	}
	b.WriteString(`<g clip-path="url(#card)">`)
	for i, s := range streaks {
		p := s.pts
		fmt.Fprintf(&b, `<path d="M%g %gL%g %gL%g %gL%g %gZ" fill="url(#s%d)"/>`, p[0][0], p[0][1], p[1][0], p[1][1], p[2][0], p[2][1], p[3][0], p[3][1], i)
	}
	b.WriteString(`</g>`)
	opacity, width := 0.12, 1.0
	if template {
		opacity, width = 1, 4
	}
	fmt.Fprintf(&b, `<path d="%s" fill="none" stroke="#FFFFFF" stroke-opacity="%g" stroke-width="%g"/></svg>`, roundedRect(2.5, 2.5, 59, 59, 14.5), opacity, width)
	return b.String()
}

// roundedRect is the path of render::rounded_rect: quarter circles drawn as cubics.
func roundedRect(x, y, w, h, r float64) string {
	k := r * 0.4477
	rr, bb := x+w, y+h
	return fmt.Sprintf("M%g %gL%g %gC%g %g %g %g %g %gL%g %gC%g %g %g %g %g %gL%g %gC%g %g %g %g %g %gL%g %gC%g %g %g %g %g %gZ",
		x+r, y, rr-r, y,
		rr-k, y, rr, y+k, rr, y+r,
		rr, bb-r,
		rr, bb-k, rr-k, bb, rr-r, bb,
		x+r, bb,
		x+k, bb, x, bb-k, x, bb-r,
		x, y+r,
		x, y+k, x+k, y, x+r, y)
}

var (
	markOnce sync.Once
	marks    [2]*ui.SVG
)

// Mark returns the parsed mark, for ui.Image (in its own colors) at any size.
func Mark(template bool) *ui.SVG {
	markOnce.Do(func() {
		marks[0] = ui.MustParseSVG([]byte(MarkSource(false)))
		marks[1] = ui.MustParseSVG([]byte(MarkSource(true)))
	})
	if template {
		return marks[1]
	}
	return marks[0]
}
