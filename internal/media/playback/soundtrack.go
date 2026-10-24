package playback

import (
	"errors"
	"io"
	"sync"
)

// Source is a decoded audio track read in chunks, as media.AudioReader reads an MP4's.
// NextChunk appends interleaved samples to dst and returns io.EOF at the end of the track.
type Source interface {
	SampleRate() int
	Channels() int
	SeekMS(ms int64) error
	NextChunk(dst []float32) ([]float32, error)
}

// queueAhead is half a second of output: enough to ride out a slow video frame, short
// enough that a seek drops little.
const queueAhead = SampleRate * Channels / 2

// Soundtrack streams a Source to an AudioOutput at SampleRate in stereo. The goroutine that
// plays it calls SeekMS and TopUp; the output's goroutine drains it through Fill.
type Soundtrack struct {
	src     Source
	rs      *Resampler
	chunk   []float32
	scratch []float32
	ended   bool

	mu    sync.Mutex
	queue []float32
}

func NewSoundtrack(src Source) *Soundtrack {
	return &Soundtrack{src: src, rs: NewResampler(src.SampleRate(), src.Channels()), scratch: make([]float32, 4096)}
}

// SeekMS drops what is queued and fills the queue from ms.
func (s *Soundtrack) SeekMS(ms int64) error {
	if err := s.src.SeekMS(ms); err != nil {
		return err
	}
	s.rs.Reset()
	s.mu.Lock()
	s.queue = s.queue[:0]
	s.mu.Unlock()
	s.ended = false
	return s.TopUp()
}

// TopUp decodes until half a second is queued or the track ends.
func (s *Soundtrack) TopUp() error {
	for s.queued() < queueAhead {
		if n := s.rs.Pull(s.scratch, s.ended); n > 0 {
			s.mu.Lock()
			s.queue = append(s.queue, s.scratch[:n]...)
			s.mu.Unlock()
			continue
		}
		if s.ended {
			return nil
		}
		var err error
		s.chunk, err = s.src.NextChunk(s.chunk[:0])
		switch {
		case errors.Is(err, io.EOF):
			s.ended = true
		case err != nil:
			return err
		}
		s.rs.Push(s.chunk)
	}
	return nil
}

func (s *Soundtrack) queued() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.queue)
}

// Fill writes the queued samples to out and silence after them: the AudioOutput callback.
func (s *Soundtrack) Fill(out []float32) {
	s.mu.Lock()
	n := copy(out, s.queue)
	s.queue = s.queue[n:]
	s.mu.Unlock()
	clear(out[n:])
}

// Resampler turns interleaved chunks of any rate and channel count into stereo at
// SampleRate by linear interpolation: mono is doubled, and channels past two are dropped.
// The read position carries across chunks, so long tracks do not drift.
type Resampler struct {
	channels int
	step     float64
	pending  []float32
	pos      float64
}

func NewResampler(sampleRate, channels int) *Resampler {
	return &Resampler{channels: max(channels, 1), step: float64(max(sampleRate, 1)) / SampleRate}
}

func (r *Resampler) Reset() {
	r.pending = r.pending[:0]
	r.pos = 0
}

func (r *Resampler) Push(chunk []float32) { r.pending = append(r.pending, chunk...) }

// Pull writes as many output samples as the pending input allows and returns their count.
// Until ended, it keeps the last input frame back to interpolate toward the next chunk.
func (r *Resampler) Pull(out []float32, ended bool) int {
	ch := r.channels
	frames := len(r.pending) / ch
	written := 0
	for ; written+2 <= len(out); written += 2 {
		i0 := int(r.pos)
		if i0 >= frames || (i0+1 >= frames && !ended) {
			break
		}
		i1 := min(i0+1, frames-1)
		f := float32(r.pos - float64(i0))
		for c := range 2 {
			a, b := r.pending[i0*ch+min(c, ch-1)], r.pending[i1*ch+min(c, ch-1)]
			out[written+c] = a + (b-a)*f
		}
		r.pos += r.step
	}
	used := min(int(r.pos), frames)
	r.pending = append(r.pending[:0], r.pending[used*ch:]...)
	r.pos -= float64(used)
	return written
}
