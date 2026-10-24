package media

import "fmt"

// writeNV12 appends a frame already converted to NV12, shown at t (100 ns units) for one
// frame period. A screen recording repeats its last frame whenever the screen did not
// change, so converting once and writing the same bytes again is what keeps 60 fps cheap.
// The calling thread must already be in the multithreaded apartment.
func (w *VideoWriter) writeNV12(nv12 []byte, t int64) error {
	n := w.video.Width * w.video.Height * 3 / 2
	if len(nv12) < n {
		return fmt.Errorf("NV12 frame of %d bytes does not hold %dx%d", len(nv12), w.video.Width, w.video.Height)
	}
	if w.sink.IsNil() {
		return fmt.Errorf("video writer is closed")
	}
	if err := w.writeSample(w.videoStream, n, t, frameTime(1, w.video.FPS), func(dst []byte) { copy(dst, nv12) }); err != nil {
		return fmt.Errorf("write video frame %d: %w", w.frames, err)
	}
	w.frames++
	return nil
}
