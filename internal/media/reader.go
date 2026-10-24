package media

import (
	"errors"
	"image"
	"io"
)

type VideoFrame struct {
	// TMS is the presentation time in media milliseconds.
	TMS   int64
	Image *image.RGBA
}

// VideoReader decodes the first video track of an MP4, MOV or M4V to RGBA frames.
type VideoReader struct {
	*FrameReader
}

func OpenVideoReader(path string) (*VideoReader, error) {
	fr, err := OpenFrameReader(path)
	if err != nil {
		return nil, err
	}
	return &VideoReader{fr}, nil
}

// NextFrame decodes the next frame into a new image. It returns io.EOF at the end.
func (r *VideoReader) NextFrame() (VideoFrame, error) {
	f, err := r.FrameReader.NextFrame()
	if err != nil {
		return VideoFrame{}, err
	}
	defer f.Release()
	return VideoFrame{TMS: f.TMS, Image: f.ToRGBA(nil)}, nil
}

// Thumbnails returns n frames spaced evenly across the duration, the first at 0. It stops
// early if the track ends.
func (r *VideoReader) Thumbnails(n int) ([]VideoFrame, error) {
	out := make([]VideoFrame, 0, n)
	for i := range n {
		if err := r.SeekMS(r.DurationMS() * int64(i) / int64(n)); err != nil {
			return out, err
		}
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return out, err
		}
		out = append(out, f)
	}
	return out, nil
}

// ReadMP4Audio decodes the whole first audio track of path, or returns ErrNoAudio. Tests and
// short clips use it; Studio and the Trim preview stream chunks with AudioReader instead.
func ReadMP4Audio(path string) (*WavData, error) {
	r, err := OpenAudioReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var samples []float32
	for {
		samples, err = r.NextChunk(samples)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	return &WavData{SampleRate: r.SampleRate(), Channels: r.Channels(), Samples: samples}, nil
}
