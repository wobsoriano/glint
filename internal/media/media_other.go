//go:build !windows

package media

import (
	"errors"
	"image"
	"time"
)

func unsupported(what string) error {
	return errors.Join(errors.New("media: "+what+" is not implemented on this platform"), errors.ErrUnsupported)
}

type VideoWriter struct{}

func CreateVideoWriter(path string, video VideoParams, audio *AudioParams) (*VideoWriter, error) {
	return nil, unsupported("video encoding")
}

func (w *VideoWriter) Hardware() bool                   { return false }
func (w *VideoWriter) Frames() int64                    { return 0 }
func (w *VideoWriter) WriteFrame(img *image.RGBA) error { return unsupported("video encoding") }
func (w *VideoWriter) WriteBGRA(px []byte, stride int, at time.Duration) error {
	return unsupported("video encoding")
}
func (w *VideoWriter) WriteAudio(interleaved []float32) error { return unsupported("video encoding") }
func (w *VideoWriter) Finish() (WriteSummary, error) {
	return WriteSummary{}, unsupported("video encoding")
}
func (w *VideoWriter) Close() {}

type FrameReader struct{}

func OpenFrameReader(path string) (*FrameReader, error) { return nil, unsupported("video decoding") }

func (fr *FrameReader) Size() (width, height int)      { return 0, 0 }
func (fr *FrameReader) FPS() float64                   { return 0 }
func (fr *FrameReader) DurationMS() int64              { return 0 }
func (fr *FrameReader) SeekMS(ms int64) error          { return unsupported("video decoding") }
func (fr *FrameReader) NextFrame() (*Nv12Frame, error) { return nil, unsupported("video decoding") }
func (fr *FrameReader) Close()                         {}

type AudioReader struct{}

func OpenAudioReader(path string) (*AudioReader, error) { return nil, unsupported("audio decoding") }

func (ar *AudioReader) SampleRate() int       { return 0 }
func (ar *AudioReader) Channels() int         { return 0 }
func (ar *AudioReader) SeekMS(ms int64) error { return unsupported("audio decoding") }
func (ar *AudioReader) NextChunk(dst []float32) ([]float32, error) {
	return dst, unsupported("audio decoding")
}
func (ar *AudioReader) Close() {}

func AvailableCodecs() []VideoCodec { return nil }
func DecodableCodecs() []VideoCodec { return nil }
