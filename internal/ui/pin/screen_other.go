//go:build !windows

package pin

import (
	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/platform"
)

// Here geometry is in DIPs, so the scale is 1.
func workAreaAtCursor() (platform.Rect, float64) {
	a := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	return platform.Rect{X: a.X, Y: a.Y, W: a.Width, H: a.Height}, 1
}

func bounds(win *mygo.Window) platform.Rect {
	b := win.Bounds()
	return platform.Rect{X: b.X, Y: b.Y, W: b.Width, H: b.Height}
}

func setBounds(win *mygo.Window, r platform.Rect) {
	win.SetBounds(mygo.Rectangle{X: r.X, Y: r.Y, Width: r.W, Height: r.H})
}

func cursor() (int, int) {
	p := mygo.Screen.CursorScreenPoint()
	return int(p.X), int(p.Y)
}
