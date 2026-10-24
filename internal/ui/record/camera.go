package record

import (
	"image"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const cameraSide = 200

// CameraPreview starts a live feed of the camera, calling frame from another goroutine with
// each new picture, until stop. frame must not wait on the main thread: stop runs there and
// waits for the feed to end.
var CameraPreview = func(id media.CameraID, frame func(*image.RGBA)) (stop func(), err error) {
	p, err := media.StartCameraPreview(id)
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for img := range p.Frames() {
			frame(img)
		}
	}()
	return func() {
		p.Stop()
		<-done
	}, nil
}

// cameraFeed keeps the newest picture of a camera preview for the bubble to paint.
type cameraFeed struct {
	stop func()

	mu     sync.Mutex
	latest *image.RGBA
	redraw func()
}

func startCameraFeed(id media.CameraID) (*cameraFeed, error) {
	f := &cameraFeed{}
	stop, err := CameraPreview(id, f.put)
	if err != nil {
		return nil, err
	}
	f.stop = stop
	return f, nil
}

func (f *cameraFeed) put(img *image.RGBA) {
	f.mu.Lock()
	f.latest = img
	redraw := f.redraw
	f.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

// onFrame sets what runs after each new picture, from the feed's goroutine.
func (f *cameraFeed) onFrame(redraw func()) {
	f.mu.Lock()
	f.redraw = redraw
	f.mu.Unlock()
}

// take returns the picture that arrived since the last take, or nil.
func (f *cameraFeed) take() *image.RGBA {
	f.mu.Lock()
	defer f.mu.Unlock()
	img := f.latest
	f.latest = nil
	return img
}

func (f *cameraFeed) close() {
	f.onFrame(nil)
	f.stop()
}

// showCamera shows the chosen camera in a round bubble above the bar's corner while the
// user picks. Like the Rust build, it closes when recording starts, which opens the camera
// for camera.mp4 instead.
func (c *Controller) showCamera() {
	c.closeWindow(&c.camera)
	id := c.prefs().Camera
	if id == nil || c.target == nil {
		return
	}
	feed, err := startCameraFeed(media.CameraID(*id))
	if err != nil {
		c.host.Toast("Camera preview unavailable: " + err.Error())
		return
	}
	d := c.target.Display
	a := workArea(d)
	side, margin := scaled(cameraSide, d.Scale), scaled(pickerMargin, d.Scale)
	r := platform.Rect{X: a.X + a.W - side - margin, Y: a.Y + a.H - side - scaled(pickerH, d.Scale) - 2*margin, W: side, H: side}
	var bmp *ui.Bitmap
	win := c.openChrome(passive, r, d.Scale, func(ctx *ui.Context) {
		p := theme.Apply(ctx)
		ctx.Root().Background(ui.Transparent)
		if img := feed.take(); img != nil {
			if bmp == nil {
				bmp = ui.NewBitmap(img)
			} else {
				bmp.Update(img)
			}
		}
		bubble := ui.Box(ctx).Fill().Radius(cameraSide/2).Clip().Background(p.Field).Border(2, ui.RGBA(255, 255, 255, 0.25))
		bubble.Children(func() {
			if bmp != nil {
				ui.Image(ctx, bmp).Fill().Fit(ui.Cover)
			}
		})
		c.probe(SurfaceCamera, "bubble", bubble)
	})
	feed.onFrame(win.Invalidate)
	win.OnClosed(feed.close)
	c.camera = win
}
