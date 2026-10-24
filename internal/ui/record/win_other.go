//go:build !windows

package record

import (
	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/platform"
)

// Here platform rects and MyGo's agree on the DIP, so MyGo places the windows.
func place(win *mygo.Window, r platform.Rect) {
	win.SetBounds(mygo.Rectangle{X: r.X, Y: r.Y, Width: r.W, Height: r.H})
}

func workArea(d platform.Display) platform.Rect {
	a := mygo.Screen.DisplayNearestPoint(mygo.Point{X: d.Bounds.X + d.Bounds.W/2, Y: d.Bounds.Y + d.Bounds.H/2}).WorkArea
	return platform.Rect{X: a.X, Y: a.Y, W: a.Width, H: a.Height}
}

func windowRect(id int64) (platform.Rect, bool) { return platform.Rect{}, false }

func squareCorners(win *mygo.Window) {}
