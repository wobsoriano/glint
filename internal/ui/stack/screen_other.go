//go:build !windows

package stack

import (
	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/platform"
)

// workAreaAtCursor is the work area of the display under the pointer. Here its unit is the
// DIP, so the scale is 1.
func workAreaAtCursor() (platform.Rect, float64) {
	a := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	return platform.Rect{X: a.X, Y: a.Y, W: a.Width, H: a.Height}, 1
}

func place(win *mygo.Window, r platform.Rect) {
	win.SetBounds(mygo.Rectangle{X: r.X, Y: r.Y, Width: r.W, Height: r.H})
}
