package capture

import (
	"math"
	"sync/atomic"
)

// SampleRate is the rate every recorded audio track is resampled to.
const SampleRate = 48_000

// DeviceID identifies an audio endpoint across runs on the same machine. On Windows it is the
// MMDevice endpoint id, the string settings.json stores as "mic".
type DeviceID string

type AudioDevice struct {
	ID   DeviceID
	Name string
}

// Source is what an audio track records.
type Source struct {
	loopback bool
	mic      DeviceID
}

// Loopback records what the default output device plays.
var Loopback = Source{loopback: true}

// Microphone records the microphone id, or the default microphone when id is empty.
func Microphone(id DeviceID) Source { return Source{mic: id} }

func (s Source) String() string {
	switch {
	case s.loopback:
		return "loopback"
	case s.mic == "":
		return "default microphone"
	default:
		return "microphone " + string(s.mic)
	}
}

// Stats describes a finished WAV track.
type Stats struct {
	Frames uint64
	Bytes  uint64
}

// LevelMeter holds the RMS of the last buffer, readable from any goroutine.
type LevelMeter struct{ rms atomic.Uint32 }

// RMS is the level of the last buffer, 0 (silence) to about 1 (full scale).
func (m *LevelMeter) RMS() float32 { return math.Float32frombits(m.rms.Load()) }

func (m *LevelMeter) update(samples []float32) {
	if len(samples) == 0 {
		return
	}
	var sum float32
	for _, s := range samples {
		sum += s * s
	}
	m.rms.Store(math.Float32bits(float32(math.Sqrt(float64(sum / float32(len(samples)))))))
}

// converter turns interleaved float32 at a device's rate and channel count into outChannels
// at SampleRate: it averages down to mono, duplicates mono to stereo, keeps the first two
// channels otherwise, and resamples linearly. The read position carries across buffers so
// the output does not drift.
type converter struct {
	rate        int
	inChannels  int
	outChannels int
	pos         float64
	mapped      []float32
}

func newConverter(rate, inChannels, outChannels int) *converter {
	return &converter{rate: rate, inChannels: max(inChannels, 1), outChannels: outChannels}
}

func (c *converter) push(in []float32, out []float32) []float32 {
	n, ch := c.inChannels, c.outChannels
	frames := len(in) / n
	c.mapped = c.mapped[:0]
	for f := range frames {
		frame := in[f*n : f*n+n]
		switch {
		case ch == 1:
			var sum float32
			for _, s := range frame {
				sum += s
			}
			c.mapped = append(c.mapped, sum/float32(n))
		case n == 1:
			c.mapped = append(c.mapped, frame[0], frame[0])
		default:
			c.mapped = append(c.mapped, frame[0], frame[1])
		}
	}
	if c.rate == SampleRate {
		return append(out, c.mapped...)
	}
	step := float64(c.rate) / SampleRate
	for c.pos+1 < float64(frames) {
		i := int(c.pos)
		t := float32(c.pos - float64(i))
		for k := range ch {
			a, b := c.mapped[i*ch+k], c.mapped[(i+1)*ch+k]
			out = append(out, a+(b-a)*t)
		}
		c.pos += step
	}
	c.pos -= float64(frames)
	return out
}

// runner is a capture goroutine's handle. done closes once stats and err are final.
type runner struct {
	level LevelMeter
	stop  atomic.Bool
	done  chan struct{}
	stats Stats
	err   error
}

func (r *runner) finish() (Stats, error) {
	r.stop.Store(true)
	<-r.done
	return r.stats, r.err
}

// Track records one audio source to a WAV file at SampleRate that follows a media clock.
type Track struct {
	Path string
	r    *runner
}

// Level is the RMS of the last buffer the device delivered, paused or not.
func (t *Track) Level() float32 { return t.r.level.RMS() }

// Stop finalizes the WAV, padded or cut to the clock's media time. Calling it again returns
// the same result.
func (t *Track) Stop() (Stats, error) { return t.r.finish() }

// MicMonitor meters a microphone without recording it, for the level bar before recording.
type MicMonitor struct{ r *runner }

func (m *MicMonitor) Level() float32 { return m.r.level.RMS() }

func (m *MicMonitor) Close() { m.r.finish() }
