package media

import (
	"errors"
	"fmt"
	"image"
	"os"
	"strconv"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/media/internal/mf"
	"golang.org/x/sys/windows"
)

var (
	user32                           = windows.NewLazySystemDLL("user32.dll")
	procIsWindow                     = user32.NewProc("IsWindow")
	procGetWindowRect                = user32.NewProc("GetWindowRect")
	procGetMonitorInfoW              = user32.NewProc("GetMonitorInfoW")
	procMonitorFromWindow            = user32.NewProc("MonitorFromWindow")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procDwmGetWindowAttribute        = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmGetWindowAttribute")
)

const dwmwaExtendedFrameBounds = 9

var dpiPerMonitorV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, (HANDLE)-4

// physicalPixels makes the calling thread per-monitor DPI aware, so monitor and window
// rectangles come back in physical pixels whatever the process manifest says. The thread
// must stay locked until the returned func restores it.
func physicalPixels() (restore func()) {
	prev, _, _ := procSetThreadDpiAwarenessContext.Call(dpiPerMonitorV2)
	return func() {
		if prev != 0 {
			procSetThreadDpiAwarenessContext.Call(prev)
		}
	}
}

// windowBounds reads the window rectangle bounds names, in physical pixels on a
// per-monitor aware thread.
func windowBounds(hwnd uintptr, bounds input.WindowBounds) (image.Rectangle, bool) {
	var r [4]int32
	var ok bool
	if bounds == input.BoundsDWM {
		hr, _, _ := procDwmGetWindowAttribute.Call(hwnd, dwmwaExtendedFrameBounds, uintptr(unsafe.Pointer(&r)), unsafe.Sizeof(r))
		ok = hr == 0
	} else {
		res, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
		ok = res != 0
	}
	return image.Rect(int(r[0]), int(r[1]), int(r[2]), int(r[3])), ok
}

func monitorExists(hmonitor uintptr) bool {
	var info [40]byte // MONITORINFO
	*(*uint32)(unsafe.Pointer(&info[0])) = uint32(len(info))
	ok, _, _ := procGetMonitorInfoW.Call(hmonitor, uintptr(unsafe.Pointer(&info[0])))
	return ok != 0
}

// screenSource is a resolved capture source: a monitor, optionally cropped to area in its
// own pixels, or a window.
type screenSource struct {
	monitor uintptr
	area    *image.Rectangle
	window  uintptr
}

// screenGeometry is what the screen master shows of its capture item.
type screenGeometry struct {
	// crop is in the capture item's pixels; its size is the video's.
	crop image.Rectangle
	// bounds is the window rectangle the frames match, for a window source.
	bounds input.WindowBounds
}

type screenTrack struct {
	path     string
	warnings []string
	stop     atomic.Bool
	done     chan struct{}
	stats    ScreenStats
	hardware bool
	closed   bool
	err      error
}

// startScreen opens the capture and the encoder on their own locked thread and returns once
// the first frame has arrived, so the caller can start media time knowing frames flow.
func startScreen(src screenSource, fps FPS, path string, clock *capture.Clock) (*screenTrack, screenGeometry, error) {
	t := &screenTrack{path: path, done: make(chan struct{})}
	type opened struct {
		geometry screenGeometry
		err      error
	}
	ready := make(chan opened, 1)
	go func() {
		defer close(t.done)
		leave, err := mf.Enter()
		if err != nil {
			ready <- opened{err: err}
			return
		}
		defer leave()
		defer physicalPixels()()
		c, err := openScreen(src, fps, path)
		if err == nil {
			err = c.waitFirstFrame(3 * time.Second)
			if err != nil {
				c.release()
				c.writer.Close()
				os.Remove(path)
			}
		}
		if err != nil {
			ready <- opened{err: err}
			return
		}
		t.warnings = c.warnings
		ready <- opened{geometry: c.geometry}
		t.closed, t.err = c.run(clock, &t.stop)
		t.stats, t.hardware = c.stats, c.writer.Hardware()
	}()
	o := <-ready
	if o.err != nil {
		<-t.done
		return nil, screenGeometry{}, o.err
	}
	return t, o.geometry, nil
}

// live is false once the capture ended on its own, because its source went away or failed.
func (t *screenTrack) live() bool {
	select {
	case <-t.done:
		return false
	default:
		return true
	}
}

func (t *screenTrack) finish() (ScreenStats, error) {
	t.stop.Store(true)
	<-t.done
	return t.stats, t.err
}

type screenCapture struct {
	dev      *mf.Device
	item     mf.CaptureItem
	pool     *mf.FramePool
	session  mf.CaptureSession
	poolSize mf.Size
	// latest holds the newest frame's crop on the GPU; staging is where it is read back.
	latest, staging mf.Texture2D
	geometry        screenGeometry
	haveFrame       bool
	// dirty is set when latest changed since nv12 was converted from it.
	dirty    bool
	nv12     []byte
	writer   *VideoWriter
	fps      FPS
	alive    func() bool
	warnings []string
	stats    ScreenStats
	// failAfter injects a write failure after this many frames, for tests of how a broken
	// screen track ends a recording.
	failAfter int64
}

func openScreen(src screenSource, fps FPS, path string) (c *screenCapture, err error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	c = &screenCapture{fps: fps, failAfter: -1}
	defer func() {
		if err != nil {
			c.release()
		}
	}()
	if n, err := strconv.ParseInt(os.Getenv("GLINT_FAIL_SCREEN_AFTER"), 10, 64); err == nil {
		c.failAfter = n
	}
	if c.dev, err = mf.CreateDevice(); err != nil {
		return nil, err
	}
	if src.window != 0 {
		c.item, err = mf.CaptureWindow(src.window)
		c.alive = func() bool { ok, _, _ := procIsWindow.Call(src.window); return ok != 0 }
	} else {
		c.item, err = mf.CaptureMonitor(src.monitor)
		c.alive = func() bool { return monitorExists(src.monitor) }
	}
	if err != nil {
		return nil, err
	}
	size, err := c.item.Size()
	if err != nil {
		return nil, err
	}
	if c.geometry.crop, err = cropFor(int(size.W), int(size.H), src.area); err != nil {
		return nil, err
	}
	if src.window != 0 {
		// The item matches the visible frame on Windows 11, but not every window has one, so
		// pick whichever rectangle the item's size agrees with.
		c.geometry.bounds = input.BoundsWindowRect
		if r, ok := windowBounds(src.window, input.BoundsDWM); ok && r.Dx() == int(size.W) && r.Dy() == int(size.H) {
			c.geometry.bounds = input.BoundsDWM
		}
	}
	if c.pool, err = c.dev.FramePool(size); err != nil {
		return nil, err
	}
	c.poolSize = size
	if c.session, err = c.pool.Session(c.item); err != nil {
		return nil, err
	}
	if err := c.session.SetCursorCapture(false); err != nil {
		c.warnings = append(c.warnings, "could not disable cursor capture; the cursor is burned into screen.mp4")
	}
	if err := c.session.SetBorderRequired(false); err != nil {
		c.warnings = append(c.warnings, "could not disable the capture border")
	}
	w, h := c.geometry.crop.Dx(), c.geometry.crop.Dy()
	if c.latest, err = c.dev.Texture(w, h); err != nil {
		return nil, err
	}
	if c.staging, err = c.dev.StagingTexture(c.latest.Desc()); err != nil {
		return nil, err
	}
	c.nv12 = make([]byte, w*h*3/2)
	params := VideoParams{Width: w, Height: h, FPS: int(fps), Bitrate: RecordingBitrate(w, h, int(fps)), Codec: H264}
	if c.writer, err = CreateVideoWriter(path, params, nil); err != nil {
		return nil, err
	}
	if err := c.session.Start(); err != nil {
		c.writer.Close()
		os.Remove(path)
		return nil, err
	}
	return c, nil
}

func (c *screenCapture) release() {
	if !c.session.IsNil() {
		c.session.Close()
		c.session = mf.CaptureSession{}
	}
	if c.pool != nil {
		c.pool.Close()
		c.pool = nil
	}
	c.item.Release()
	c.item = mf.CaptureItem{}
	c.latest.Release()
	c.latest = mf.Texture2D{}
	c.staging.Release()
	c.staging = mf.Texture2D{}
	if c.dev != nil {
		c.dev.Release()
		c.dev = nil
	}
}

func (c *screenCapture) waitFirstFrame(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for !c.haveFrame {
		if time.Now().After(deadline) {
			return fmt.Errorf("no frame from Windows.Graphics.Capture within %v", timeout)
		}
		if !c.alive() {
			return errors.New("capture source closed before the first frame")
		}
		if err := c.drain(); err != nil {
			return err
		}
		if !c.haveFrame {
			time.Sleep(2 * time.Millisecond)
		}
	}
	return nil
}

// drain copies every waiting frame's crop into latest. When the source changes size the
// pool is resized and latest starts black, so a shrunken window leaves no stale pixels.
func (c *screenCapture) drain() error {
	for {
		f, ok, err := c.pool.NextFrame()
		if err != nil || !ok {
			return err
		}
		if err := c.take(f); err != nil {
			return err
		}
	}
}

func (c *screenCapture) take(f mf.CaptureFrame) error {
	defer f.Close()
	content, err := f.ContentSize()
	if err != nil {
		return err
	}
	if content != c.poolSize && content.W > 0 && content.H > 0 {
		if err := c.pool.Recreate(content); err != nil {
			return err
		}
		c.poolSize = content
		c.latest.Release()
		c.latest, err = c.dev.Texture(c.geometry.crop.Dx(), c.geometry.crop.Dy())
		return err
	}
	tex, err := f.Texture()
	if err != nil {
		return err
	}
	defer tex.Release()
	crop := c.geometry.crop
	right, bottom := min(crop.Max.X, int(content.W)), min(crop.Max.Y, int(content.H))
	if right > crop.Min.X && bottom > crop.Min.Y {
		c.dev.CopyRegion(c.latest, 0, 0, tex, mf.Box{Left: uint32(crop.Min.X), Top: uint32(crop.Min.Y), Right: uint32(right), Bottom: uint32(bottom), Back: 1})
		c.haveFrame, c.dirty = true, true
		c.stats.Captured++
	}
	return nil
}

// convert reads latest back and converts it to NV12, once per changed frame.
func (c *screenCapture) convert() error {
	c.dev.CopyResource(c.staging, c.latest)
	data, pitch, err := c.dev.Map(c.staging)
	if err != nil {
		return err
	}
	w, h := c.geometry.crop.Dx(), c.geometry.crop.Dy()
	px := unsafe.Slice((*byte)(data), int(pitch)*(h-1)+w*4)
	toNV12(px, int(pitch), w, h, orderBGRA, c.nv12)
	c.dev.Unmap(c.staging)
	c.dirty = false
	c.stats.Converted++
	return nil
}

func frameIndex(at time.Duration, fps FPS) int64 { return int64(at) * int64(fps) / int64(time.Second) }

// catchUp writes every frame slot up to media time now, each showing the newest frame.
func (c *screenCapture) catchUp(now time.Duration, next *int64) error {
	start := time.Now()
	defer func() { c.stats.Busy += time.Since(start) }()
	for target := frameIndex(now, c.fps); *next <= target; *next++ {
		if c.dirty {
			if err := c.convert(); err != nil {
				return err
			}
		}
		if c.failAfter >= 0 && c.writer.Frames() >= c.failAfter {
			return fmt.Errorf("GLINT_FAIL_SCREEN_AFTER: injected failure after %d frames", c.writer.Frames())
		}
		at := frameTime(*next, int(c.fps))
		if now-time.Duration(at*100) > time.Second/time.Duration(c.fps) {
			c.stats.Late++
		}
		if err := c.writer.writeNV12(c.nv12, at); err != nil {
			return err
		}
	}
	return nil
}

// run captures until stop or until the source goes away, then finalizes screen.mp4. When
// it ends on its own it ends media time at its last frame, so every track stops there.
func (c *screenCapture) run(clock *capture.Clock, stop *atomic.Bool) (closed bool, err error) {
	period := time.Second / time.Duration(c.fps)
	var next int64
	for !stop.Load() && err == nil {
		if !c.alive() {
			closed = true
			break
		}
		if err = c.drain(); err != nil {
			break
		}
		if clock.IsPaused() {
			time.Sleep(4 * time.Millisecond)
			continue
		}
		now := clock.Now()
		if err = c.catchUp(now, &next); err != nil {
			break
		}
		due := time.Duration(frameTime(next, int(c.fps)) * 100)
		time.Sleep(min(max(due-clock.Now(), 500*time.Microsecond), period))
	}
	if err == nil && !closed {
		if err = c.drain(); err == nil {
			err = c.catchUp(clock.Now(), &next)
		}
	}
	c.release()
	if !stop.Load() {
		clock.EndAt(time.Duration(frameTime(c.writer.Frames(), int(c.fps)) * 100))
	}
	sum, ferr := c.writer.Finish()
	c.stats.Frames = sum.Frames
	if err == nil {
		err = ferr
	}
	return closed, err
}
