package media

import (
	"context"
	"errors"
	"fmt"
	"image"
	"io"
	"math"
	"os"
)

// TranscodeOptions says how to re-encode a video. Zero fields keep the source's values;
// a zero Codec is H.264.
type TranscodeOptions struct {
	// Width and Height are the output size; odd values are rounded down to even.
	Width, Height int
	Bitrate       int
	Codec         VideoCodec
	// Trim keeps only [Trim.StartMS, Trim.EndMS) of the source when set.
	Trim      *Span
	DropAudio bool
	// Software skips hardware encoders.
	Software bool
}

// Span is a stretch of media time in milliseconds.
type Span struct{ StartMS, EndMS int64 }

// Transcode re-encodes src into dst, optionally resized, at a new bitrate, or trimmed. It
// carries the source's audio along unless DropAudio is set. progress receives 0..1.
// Cancelling ctx stops early and removes the partial file.
func Transcode(ctx context.Context, src, dst string, opts TranscodeOptions, progress func(float64)) (WriteSummary, error) {
	reader, err := OpenFrameReader(src)
	if err != nil {
		return WriteSummary{}, err
	}
	defer reader.Close()
	sw, sh := reader.Size()
	w, h := sw, sh
	if opts.Width > 0 && opts.Height > 0 {
		w, h = opts.Width, opts.Height
	}
	w, h = w&^1, h&^1
	fps := max(int(math.Round(reader.FPS())), 1)
	span := Span{0, reader.DurationMS()}
	if opts.Trim != nil {
		span = *opts.Trim
	}
	if span.EndMS <= span.StartMS {
		return WriteSummary{}, fmt.Errorf("trim range %d..%d is empty", span.StartMS, span.EndMS)
	}
	video := VideoParams{Width: w, Height: h, FPS: fps, Bitrate: opts.Bitrate, Codec: opts.Codec, Software: opts.Software}
	if video.Bitrate <= 0 {
		video.Bitrate = RecordingBitrate(w, h, fps)
	}

	var audio *audioCopy
	var params *AudioParams
	if !opts.DropAudio {
		a, err := OpenAudioReader(src)
		switch {
		case err == nil:
			defer a.Close()
			params = &AudioParams{SampleRate: a.SampleRate(), Channels: a.Channels()}
			if audio, err = newAudioCopy(a, span); err != nil {
				return WriteSummary{}, err
			}
		case !errors.Is(err, ErrNoAudio):
			return WriteSummary{}, err
		}
	}
	writer, err := CreateVideoWriter(dst, video, params)
	if err != nil {
		return WriteSummary{}, err
	}
	abandon := func(err error) (WriteSummary, error) {
		writer.Close()
		os.Remove(dst)
		return WriteSummary{}, err
	}
	if err := reader.SeekMS(span.StartMS); err != nil {
		return abandon(err)
	}
	length := float64(span.EndMS - span.StartMS)
	var rgba *image.RGBA
	for {
		if err := ctx.Err(); err != nil {
			return abandon(fmt.Errorf("transcode cancelled: %w", err))
		}
		frame, err := reader.NextFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return abandon(err)
		}
		if frame.TMS >= span.EndMS {
			frame.Release()
			break
		}
		rgba = frame.ToRGBA(rgba)
		img := rgba
		if w != sw || h != sh {
			img = resize(rgba, w, h)
		}
		t := frame.TMS - span.StartMS
		frame.Release()
		if err := writer.WriteFrame(img); err != nil {
			return abandon(err)
		}
		if audio != nil {
			if err := audio.copyUntil(writer, max(t, 0)+1000); err != nil {
				return abandon(err)
			}
		}
		if progress != nil {
			progress(min(max(float64(t)/length, 0), 1))
		}
	}
	if audio != nil {
		if err := audio.copyFrames(writer, audio.frames); err != nil {
			return abandon(err)
		}
	}
	if progress != nil {
		progress(1)
	}
	return writer.Finish()
}

// audioCopy carries a span of the source's audio into the writer, a second at a time.
type audioCopy struct {
	r       *AudioReader
	frames  int64
	written int64
	eof     bool
	chunk   []float32
}

func newAudioCopy(r *AudioReader, span Span) (*audioCopy, error) {
	if err := r.SeekMS(span.StartMS); err != nil {
		return nil, err
	}
	rate := r.SampleRate()
	return &audioCopy{r: r, frames: sampleIndex(span.EndMS, rate) - sampleIndex(span.StartMS, rate)}, nil
}

func (a *audioCopy) copyUntil(w *VideoWriter, ms int64) error {
	return a.copyFrames(w, min(sampleIndex(ms, a.r.SampleRate()), a.frames))
}

func (a *audioCopy) copyFrames(w *VideoWriter, until int64) error {
	channels := max(a.r.Channels(), 1)
	perSecond := a.r.SampleRate() * channels
	for a.written < until && !a.eof {
		a.chunk = a.chunk[:0]
		for len(a.chunk) < perSecond {
			var err error
			a.chunk, err = a.r.NextChunk(a.chunk)
			if errors.Is(err, io.EOF) {
				a.eof = true
				break
			}
			if err != nil {
				return err
			}
		}
		keep := min(int64(len(a.chunk)/channels), a.frames-a.written)
		if keep == 0 {
			return nil
		}
		if err := w.WriteAudio(a.chunk[:keep*int64(channels)]); err != nil {
			return err
		}
		a.written += keep
	}
	return nil
}
