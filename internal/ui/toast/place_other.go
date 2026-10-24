//go:build !windows

package toast

import "github.com/egoist/mygo"

// place puts the toast at the bottom centre of the work area with the pointer, in points.
func (t *Toast) place(w int) {
	work := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	t.win.SetBounds(mygo.Rectangle{X: work.X + (work.Width-w)/2, Y: work.Y + work.Height - height - bottom, Width: w, Height: height})
}
