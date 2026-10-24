package record

import (
	"errors"
	"image"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const cameraSide = 200

// CameraPreview starts a live feed of the camera, calling frame from any goroutine with each
// new picture, until stop. internal/media records cameras but has no preview yet, so this
// reports that until one is plugged in here.
var CameraPreview = func(id media.CameraID, frame func(*image.RGBA)) (stop func(), err error) {
	return nil, errors.New("live camera preview is not available yet")
}

// showCamera shows the chosen camera in a round bubble above the bar's corner while the
// user picks; the recording itself writes camera.mp4.
func (c *Controller) showCamera() {
	c.closeWindow(&c.camera)
	id := c.prefs().Camera
	if id == nil || c.target == nil {
		return
	}
	var bmp *ui.Bitmap
	var win *mygo.Window
	stop, err := CameraPreview(media.CameraID(*id), func(img *image.RGBA) {
		mygo.RunOnMain(func() {
			if win == nil {
				return
			}
			if bmp == nil {
				bmp = ui.NewBitmap(img)
			} else {
				bmp.Update(img)
			}
			win.Invalidate()
		})
	})
	if err != nil {
		c.host.Toast("Camera preview unavailable: " + err.Error())
		return
	}
	d := c.target.Display
	a := workArea(d)
	side, margin := scaled(cameraSide, d.Scale), scaled(pickerMargin, d.Scale)
	r := platform.Rect{X: a.X + a.W - side - margin, Y: a.Y + a.H - side - scaled(pickerH, d.Scale) - 2*margin, W: side, H: side}
	win = c.openChrome(passive, r, d.Scale, func(ctx *ui.Context) {
		p := theme.Apply(ctx)
		ctx.Root().Background(ui.Transparent)
		ui.Box(ctx).Fill().Radius(cameraSide/2).Clip().Background(p.Field).Border(2, ui.RGBA(255, 255, 255, 0.25)).Children(func() {
			if bmp != nil {
				ui.Image(ctx, bmp).Fill().Fit(ui.Cover)
			}
		})
	})
	win.OnClosed(func() { stop() })
	c.camera = win
}
