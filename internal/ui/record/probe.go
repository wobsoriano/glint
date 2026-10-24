package record

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
)

// Surface names a window of the recording UI.
type Surface string

const (
	SurfacePicker    Surface = "picker"
	SurfacePanel     Surface = "panel"
	SurfaceCountdown Surface = "countdown"
	SurfaceGuide     Surface = "guide"
	SurfaceBar       Surface = "bar"
	SurfacePrompter  Surface = "prompter"
	SurfaceCamera    Surface = "camera"
)

type control struct {
	surface Surface
	name    string
}

// probe remembers where a control was in the last frame, for Point.
func (c *Controller) probe(s Surface, name string, e ui.Element) {
	if c.controls == nil {
		c.controls = map[control]ui.Rect{}
	}
	c.controls[control{s, name}] = e.Bounds()
}

func (c *Controller) window(s Surface) *mygo.Window {
	switch s {
	case SurfacePicker:
		if c.picker != nil {
			return c.picker.win
		}
	case SurfacePanel:
		if c.picker != nil {
			return c.picker.panelWin
		}
	case SurfaceCountdown:
		return c.countdown
	case SurfaceGuide:
		return c.guide
	case SurfaceBar:
		return c.bar
	case SurfacePrompter:
		return c.prompter
	case SurfaceCamera:
		return c.camera
	}
	return nil
}

// Window is the surface's window while it shows, as for a toast placed above the bar.
func (c *Controller) Window(s Surface) (w *mygo.Window) {
	mygo.RunOnMain(func() { w = c.window(s) })
	return w
}

// Point is where a control of a surface was in the last frame, in physical pixels of the
// desktop, for drivers that click it as the user would. origin is the window's top-left
// corner on the desktop, which the caller reads from the window.
func (c *Controller) Point(s Surface, name string, origin platform.Rect, scale float64) (x, y int, ok bool) {
	mygo.RunOnMain(func() {
		r, found := c.controls[control{s, name}]
		if !found || r.W == 0 || c.window(s) == nil {
			return
		}
		x = origin.X + int(float64(r.X+r.W/2)*scale)
		y = origin.Y + int(float64(r.Y+r.H/2)*scale)
		ok = true
	})
	return x, y, ok
}
