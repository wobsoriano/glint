// Package media encodes and decodes MP4 video and audio, reads and writes WAV, and
// transcodes, for Studio export, Trim & Compress and recording. Everything blocks and is
// meant for background goroutines. Windows uses Media Foundation; other platforms return
// errors.ErrUnsupported for now.
package media

import "errors"

type VideoCodec int

const (
	H264 VideoCodec = iota
	HEVC
)

func (c VideoCodec) String() string {
	switch c {
	case H264:
		return "H.264"
	case HEVC:
		return "HEVC"
	}
	return "unknown codec"
}

type VideoParams struct {
	Width, Height int
	FPS           int
	// Bitrate is the average in bits per second.
	Bitrate int
	Codec   VideoCodec
	// Software skips hardware encoders. Without it the writer prefers the GPU's encoder and
	// falls back to Windows' software encoder when that cannot be set up.
	Software bool
}

// RecordingBitrate keeps screen content crisp: 0.1 bits per pixel per frame, clamped to
// 4..80 Mbps (3840x2160 at 30 fps lands on 25 Mbps).
func RecordingBitrate(width, height, fps int) int {
	bits := int64(width) * int64(height) * int64(fps) / 10
	return int(min(max(bits, 4_000_000), 80_000_000))
}

type AudioParams struct {
	SampleRate int
	Channels   int
}

// AACBytesPerSec is the AAC rate every backend encodes at, the highest the Microsoft AAC
// encoder accepts for 1-2 channels.
const AACBytesPerSec = 24_000

type WriteSummary struct {
	Frames     int64
	DurationMS int64
	Bytes      int64
	// Hardware is true when a GPU encoder wrote the video.
	Hardware bool
}

// ErrNoAudio is returned when a file has no audio track.
var ErrNoAudio = errors.New("media: no audio track")

// ErrNoCamera is returned for a camera id that no connected camera has.
var ErrNoCamera = errors.New("no camera with id")

// frameTime is frame i's presentation time in 100-nanosecond units at fps, exact rather than
// accumulated so long files keep in step with their audio.
func frameTime(i int64, fps int) int64 { return i * 10_000_000 / int64(fps) }

// sampleIndex is the audio frame at ms, rounded to the nearest sample.
func sampleIndex(ms int64, sampleRate int) int64 {
	return (ms*int64(sampleRate) + 500) / 1000
}
