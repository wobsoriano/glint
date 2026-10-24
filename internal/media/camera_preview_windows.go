package media

import (
	"fmt"
	"image"
	"sync/atomic"

	"github.com/wobsoriano/glint/internal/media/internal/mf"
)

// CameraPreview is a live feed of a camera, for the picker's camera bubble. It reads the
// same mode the recording track would pick.
type CameraPreview struct {
	frames        chan *image.RGBA
	stop          atomic.Bool
	done          chan struct{}
	err           error
	width, height int
}

// StartCameraPreview opens the camera and starts reading it. It fails when the camera is
// missing or busy, before any frame arrives.
func StartCameraPreview(id CameraID) (*CameraPreview, error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	p := &CameraPreview{frames: make(chan *image.RGBA, 1), done: make(chan struct{})}
	opened := make(chan error, 1)
	go func() {
		defer close(p.done)
		defer close(p.frames)
		leave, err := mf.Enter()
		if err != nil {
			opened <- err
			return
		}
		defer leave()
		cam, err := openCamera(id)
		if err != nil {
			opened <- err
			return
		}
		defer cam.close()
		p.width, p.height = cam.width, cam.height
		opened <- nil
		p.err = p.pump(cam)
	}()
	if err := <-opened; err != nil {
		<-p.done
		return nil, err
	}
	return p, nil
}

// Frames delivers the camera's pictures as they arrive. It holds only the newest one, so a
// slow reader skips frames instead of falling behind. It is closed when the feed ends.
func (p *CameraPreview) Frames() <-chan *image.RGBA { return p.frames }

// Size is the frames' pixel size.
func (p *CameraPreview) Size() (width, height int) { return p.width, p.height }

// Stop releases the camera and returns why the feed ended early, if it did.
func (p *CameraPreview) Stop() error {
	p.stop.Store(true)
	<-p.done
	return p.err
}

func (p *CameraPreview) pump(c *camera) error {
	bgra := make([]byte, c.width*c.height*4)
	for !p.stop.Load() {
		s, flags, _, err := c.reader.ReadSample(mf.FirstVideoStream)
		if err != nil {
			return fmt.Errorf("camera ReadSample: %w", err)
		}
		if flags&mf.ReaderFlagEndOfStream != 0 {
			return nil
		}
		if s.IsNil() {
			continue
		}
		err = c.copyRows(s, bgra)
		s.Release()
		if err != nil {
			return err
		}
		p.offer(bgraToRGBA(bgra, c.width, c.height))
	}
	return nil
}

// offer replaces a frame nobody has taken yet; the pump is the only sender.
func (p *CameraPreview) offer(img *image.RGBA) {
	select {
	case p.frames <- img:
		return
	default:
	}
	select {
	case <-p.frames:
	default:
	}
	p.frames <- img
}

// bgraToRGBA swaps the camera's BGRA rows into an opaque RGBA picture; RGB32's fourth byte
// is padding, not alpha.
func bgraToRGBA(bgra []byte, width, height int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	px := img.Pix
	for i := 0; i+3 < len(bgra) && i+3 < len(px); i += 4 {
		px[i], px[i+1], px[i+2], px[i+3] = bgra[i+2], bgra[i+1], bgra[i], 0xFF
	}
	return img
}
