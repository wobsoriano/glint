package media

import (
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"

	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/media/internal/mf"
)

// cameraMaxPixels caps the camera mode at 1080p, as the Rust recorder does.
const cameraMaxPixels = 1920 * 1080

// Cameras lists the attached cameras in Media Foundation's order.
func Cameras() ([]CameraDevice, error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	leave, err := mf.Enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	list, err := mf.VideoCaptureDevices()
	if err != nil {
		return nil, err
	}
	out := make([]CameraDevice, 0, len(list))
	for _, a := range list {
		name, _ := a.GetString(&mf.MF_DEVSOURCE_ATTRIBUTE_FRIENDLY_NAME)
		link, _ := a.GetString(&mf.MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_SYMBOLIC_LINK)
		out = append(out, CameraDevice{ID: CameraID(link), Name: name})
		a.Release()
	}
	return out, nil
}

// cameraTrack records a camera to camera.mp4, each frame timed by the media clock when it
// arrives. Nothing is written while the clock is paused.
type cameraTrack struct {
	path string
	stop atomic.Bool
	done chan struct{}
	err  error
}

func startCamera(id CameraID, path string, clock *capture.Clock) (*cameraTrack, error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	t := &cameraTrack{path: path, done: make(chan struct{})}
	opened := make(chan error, 1)
	go func() {
		defer close(t.done)
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
		w, err := CreateVideoWriter(path, VideoParams{Width: cam.width, Height: cam.height, FPS: cam.fps,
			Bitrate: RecordingBitrate(cam.width, cam.height, cam.fps), Codec: H264}, nil)
		if err != nil {
			opened <- err
			return
		}
		opened <- nil
		t.err = cam.pump(w, clock, &t.stop)
	}()
	if err := <-opened; err != nil {
		<-t.done
		return nil, err
	}
	return t, nil
}

func (t *cameraTrack) finish() error {
	t.stop.Store(true)
	<-t.done
	return t.err
}

type camera struct {
	source        mf.LiveSource
	reader        mf.SourceReader
	width, height int
	fps           int
	// stride is a native row's bytes, wider than width*4 when the mode's width is odd.
	stride int
}

func openCamera(id CameraID) (*camera, error) {
	list, err := mf.VideoCaptureDevices()
	if err != nil {
		return nil, err
	}
	var found mf.Activate
	for _, a := range list {
		if link, _ := a.GetString(&mf.MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_SYMBOLIC_LINK); found.IsNil() && link == string(id) {
			found = a
			continue
		}
		a.Release()
	}
	if found.IsNil() {
		return nil, fmt.Errorf("%w %s", ErrNoCamera, id)
	}
	defer found.Release()
	c := &camera{}
	if c.source, err = found.Source(); err != nil {
		return nil, fmt.Errorf("open camera %s: %w", id, err)
	}
	if err := c.selectMode(); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

// selectMode picks the largest native mode up to 1080p at 24 fps or more, then asks the
// reader to convert it to RGB32.
func (c *camera) selectMode() error {
	attrs, err := mf.CreateAttributes(1)
	if err != nil {
		return err
	}
	defer attrs.Release()
	if err := attrs.SetUINT32(&mf.MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING, 1); err != nil {
		return err
	}
	if c.reader, err = mf.SourceReaderFor(c.source, attrs); err != nil {
		return err
	}
	var best mf.MediaType
	var bw, bh, bf uint32
	for i := uint32(0); ; i++ {
		t, err := c.reader.NativeMediaType(mf.FirstVideoStream, i)
		if err != nil {
			break
		}
		size, _ := t.GetUINT64(&mf.MF_MT_FRAME_SIZE)
		rate, err := t.GetUINT64(&mf.MF_MT_FRAME_RATE)
		if err != nil {
			rate = mf.Pack2(30, 1)
		}
		w, h := mf.Unpack2(size)
		n, d := mf.Unpack2(rate)
		fps := n / max(d, 1)
		if w == 0 || w*h > cameraMaxPixels || fps < 24 || (!best.IsNil() && (w*h < bw*bh || w*h == bw*bh && fps <= bf)) {
			t.Release()
			continue
		}
		best.Release()
		best, bw, bh, bf = t, w, h, fps
	}
	if best.IsNil() {
		return errors.New("camera offers no mode up to 1080p at 24 fps or more")
	}
	err = c.reader.SetCurrentMediaType(mf.FirstVideoStream, best)
	best.Release()
	if err != nil {
		return fmt.Errorf("select camera mode: %w", err)
	}
	out, err := mf.NewMediaType(
		mf.TypeSetting{Key: &mf.MF_MT_MAJOR_TYPE, Value: &mf.MFMediaType_Video},
		mf.TypeSetting{Key: &mf.MF_MT_SUBTYPE, Value: &mf.MFVideoFormat_RGB32},
		mf.TypeSetting{Key: &mf.MF_MT_FRAME_SIZE, Value: mf.Pack2(bw, bh)},
	)
	if err != nil {
		return err
	}
	err = c.reader.SetCurrentMediaType(mf.FirstVideoStream, out)
	out.Release()
	if err != nil {
		return fmt.Errorf("camera RGB32 output: %w", err)
	}
	c.width, c.height, c.fps, c.stride = int(bw)&^1, int(bh)&^1, int(bf), int(bw)*4
	return nil
}

func (c *camera) close() {
	c.reader.Release()
	if !c.source.IsNil() {
		c.source.Shutdown()
		c.source.Release()
	}
}

func (c *camera) pump(w *VideoWriter, clock *capture.Clock, stop *atomic.Bool) error {
	bgra := make([]byte, c.width*c.height*4)
	var err error
	for !stop.Load() && err == nil {
		s, flags, _, rerr := c.reader.ReadSample(mf.FirstVideoStream)
		if rerr != nil {
			err = fmt.Errorf("camera ReadSample: %w", rerr)
			break
		}
		if flags&mf.ReaderFlagEndOfStream != 0 {
			break
		}
		if s.IsNil() {
			continue
		}
		err = c.copyRows(s, bgra)
		s.Release()
		if err == nil && !clock.IsPaused() {
			err = w.WriteBGRA(bgra, c.width*4, clock.Now())
		}
	}
	if _, ferr := w.Finish(); err == nil {
		err = ferr
	}
	if err != nil {
		w.Close()
	}
	return err
}

// copyRows copies the frame's top width×height pixels into out. A 2D buffer's pitch is
// negative for a bottom-up image, with scan0 still on the top row.
func (c *camera) copyRows(s mf.Sample, out []byte) error {
	buf, err := s.ContiguousBuffer()
	if err != nil {
		return err
	}
	defer buf.Release()
	row := c.width * 4
	if b2, ok := buf.As2D(); ok {
		defer b2.Release()
		scan0, pitch, err := b2.Lock2D()
		if err != nil {
			return err
		}
		for y := range c.height {
			copy(out[y*row:(y+1)*row], unsafe.Slice((*byte)(unsafe.Add(scan0, y*int(pitch))), row))
		}
		return b2.Unlock2D()
	}
	data, _, cur, err := buf.Lock()
	if err != nil {
		return err
	}
	defer buf.Unlock()
	if int(cur) < c.stride*(c.height-1)+row {
		return fmt.Errorf("camera frame of %d bytes is smaller than %dx%d", cur, c.width, c.height)
	}
	src := unsafe.Slice((*byte)(data), cur)
	for y := range c.height {
		copy(out[y*row:(y+1)*row], src[y*c.stride:])
	}
	return nil
}
