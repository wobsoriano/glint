//go:build !windows

package toast

import "github.com/egoist/mygo"

// place puts the toast t.bottom points above the bottom centre of the work area with the
// pointer.
func (t *Toast) place(w int) {
	work := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	t.win.SetBounds(mygo.Rectangle{X: work.X + (work.Width-w)/2, Y: work.Y + work.Height - height - int(t.bottom), Width: w, Height: height})
}
