package studio

import (
	"math"

	"github.com/wobsoriano/glint/internal/media"
)

// The mix is 48 kHz stereo, what export muxes and playback opens the device at.
const (
	SampleRate = 48_000
	Channels   = 2
)

// The time stretch for clips off 1x speed is WSOLA: 40 ms grains, half overlapped, each
// placed within 10 ms of its nominal spot where it best continues the previous one.
const (
	grainWin  = 1920
	grainHop  = grainWin / 2
	tolerance = 480
)

// AudioSources holds a session's audio tracks at 48 kHz: mic as mono, system audio as
// interleaved stereo. A nil slice means the session has no such track.
type AudioSources struct {
	mic, system []float32
}

func LoadAudioSources(t Tracks) (*AudioSources, error) {
	var s AudioSources
	for _, track := range []struct {
		path     string
		dst      *[]float32
		channels int
	}{{t.Mic, &s.mic, 1}, {t.System, &s.system, 2}} {
		if track.path == "" {
			continue
		}
		w, err := media.ReadWAV(track.path)
		if err != nil {
			return nil, err
		}
		*track.dst = toLayout(w, track.channels)
	}
	return &s, nil
}

// Audible reports whether a mix with these settings has anything to hear.
func (s *AudioSources) Audible(a AudioSettings) bool {
	return (s.mic != nil && a.Mic.Enabled && a.Mic.Volume > 0) ||
		(s.system != nil && a.System.Enabled && a.System.Volume > 0)
}

type gains struct{ mic, system float32 }

// frame is source frame i mixed to stereo; silence outside the tracks.
func (s *AudioSources) frame(i int64, g gains) [2]float32 {
	var out [2]float32
	if i < 0 {
		return out
	}
	if s.mic != nil && g.mic != 0 {
		var v float32
		if i < int64(len(s.mic)) {
			v = s.mic[i] * g.mic
		}
		out = [2]float32{v, v}
	}
	if s.system != nil && g.system != 0 && 2*i+2 <= int64(len(s.system)) {
		out[0] += s.system[2*i] * g.system
		out[1] += s.system[2*i+1] * g.system
	}
	return out
}

// toLayout maps w to channels channels at SampleRate, resampling linearly.
func toLayout(w *media.WavData, channels int) []float32 {
	ch := max(w.Channels, 1)
	frames := len(w.Samples) / ch
	mapped := make([]float32, 0, frames*channels)
	for f := range frames {
		px := w.Samples[f*ch : f*ch+ch]
		switch {
		case channels == 1:
			var sum float32
			for _, v := range px {
				sum += v
			}
			mapped = append(mapped, sum/float32(ch))
		case ch == 1:
			mapped = append(mapped, px[0], px[0])
		default:
			mapped = append(mapped, px[0], px[1])
		}
	}
	if w.SampleRate == SampleRate || frames < 2 {
		return mapped
	}
	step := float64(w.SampleRate) / SampleRate
	outFrames := int(float64(frames-1) / step)
	out := make([]float32, 0, outFrames*channels)
	for k := range outFrames {
		pos := float64(k) * step
		i := int(pos)
		t := float32(pos - float64(i))
		for c := range channels {
			a := mapped[i*channels+c]
			b := mapped[(i+1)*channels+c]
			out = append(out, a+(b-a)*t)
		}
	}
	return out
}

// AudioMixer renders a project's audio in output time: each clip's stretch of the sources,
// time-stretched at its speed with the pitch kept, at the project's volumes.
type AudioMixer struct {
	sources *AudioSources
	clips   []clipFrames
	gains   gains
	pos     uint64
	clip    int
	// stretch carries the time stretch across Render calls while output stays inside one
	// clip off 1x speed.
	stretch     *stretch
	stretchClip int
}

// clipFrames is a clip in 48 kHz frames: where it sits in the output and where it reads.
type clipFrames struct {
	outStart, outEnd uint64
	srcStart, speed  float64
}

func msToFrames(ms float64) float64 { return ms * SampleRate / 1000 }

// NewAudioMixer starts a mix of p at startOutputMs.
func NewAudioMixer(sources *AudioSources, p *Project, startOutputMs float64) *AudioMixer {
	clips := make([]clipFrames, 0, len(p.Clips))
	var outMs float64
	for _, c := range p.Clips {
		endMs := outMs + c.OutputLen()
		clips = append(clips, clipFrames{
			outStart: uint64(math.Round(msToFrames(outMs))),
			outEnd:   uint64(math.Round(msToFrames(endMs))),
			srcStart: msToFrames(c.StartMs),
			speed:    c.Speed,
		})
		outMs = endMs
	}
	gain := func(t AudioTrack) float32 {
		if !t.Enabled {
			return 0
		}
		return clamp(t.Volume, 0, 2)
	}
	return &AudioMixer{
		sources: sources,
		clips:   clips,
		gains:   gains{gain(p.Audio.Mic), gain(p.Audio.System)},
		pos:     uint64(math.Round(msToFrames(max(startOutputMs, 0)))),
	}
}

// Render fills out, interleaved stereo, with the next frames; silence past the end.
func (m *AudioMixer) Render(out []float32) {
	for i := 0; i+1 < len(out); i += 2 {
		f := m.nextFrame()
		out[i], out[i+1] = f[0], f[1]
	}
}

func (m *AudioMixer) nextFrame() [2]float32 {
	pos := m.pos
	m.pos++
	inside := func(c clipFrames) bool { return pos >= c.outStart && pos < c.outEnd }
	if m.clip >= len(m.clips) || !inside(m.clips[m.clip]) {
		found := false
		for i, c := range m.clips {
			if inside(c) {
				m.clip, found = i, true
				break
			}
		}
		if !found {
			return [2]float32{}
		}
	}
	c := m.clips[m.clip]
	offset := float64(pos - c.outStart)
	if c.speed == 1 {
		return m.sources.frame(int64(math.Round(c.srcStart+offset)), m.gains)
	}
	if m.stretch == nil || m.stretchClip != m.clip || m.stretch.outPos != pos {
		m.stretch, m.stretchClip = newStretch(c.srcStart+offset*c.speed, c.speed, pos), m.clip
	}
	return m.stretch.frame(m.sources, m.gains)
}

type stretch struct {
	speed   float64
	nominal float64
	prev    int64
	hasPrev bool
	tail    [grainHop][2]float32
	// ready holds the frames of the last grain; next is the first one not yet handed out.
	ready  [grainHop][2]float32
	next   int
	outPos uint64
}

func newStretch(srcPos, speed float64, outPos uint64) *stretch {
	return &stretch{speed: speed, nominal: srcPos, next: grainHop, outPos: outPos}
}

func (s *stretch) frame(src *AudioSources, g gains) [2]float32 {
	if s.next == grainHop {
		s.grain(src, g)
	}
	s.outPos++
	s.next++
	return s.ready[s.next-1]
}

func (s *stretch) grain(src *AudioSources, g gains) {
	nominal := int64(math.Round(s.nominal))
	pos := nominal
	if s.hasPrev {
		var target [grainHop]float32
		for k := range int64(grainHop) {
			target[k] = mono(src.frame(s.prev+grainHop+k, g))
		}
		lo := nominal - tolerance
		var span [2*tolerance + grainHop]float32
		for k := range int64(len(span)) {
			span[k] = mono(src.frame(lo+k, g))
		}
		pos = int64(bestOffset(target[:], span[:])) + lo
	}
	for k := range grainHop {
		a, b := hann(k), hann(k+grainHop)
		head := src.frame(pos+int64(k), g)
		tail := s.tail[k]
		s.ready[k] = [2]float32{tail[0] + head[0]*a, tail[1] + head[1]*a}
		t := src.frame(pos+int64(k+grainHop), g)
		s.tail[k] = [2]float32{t[0] * b, t[1] * b}
	}
	s.next = 0
	s.prev, s.hasPrev = pos, true
	s.nominal += grainHop * s.speed
}

func mono(f [2]float32) float32 { return f[0] + f[1] }

func hann(k int) float32 {
	x := float32(math.Pi) * float32(k) / grainWin
	sin := float32(math.Sin(float64(x)))
	return sin * sin
}

// bestOffset is where in span target continues best, by correlation over every fourth
// sample at even offsets.
func bestOffset(target, span []float32) int {
	bestCorr, best := float32(-math.MaxFloat32), tolerance
	for off := 0; off <= len(span)-len(target); off += 2 {
		var corr float32
		for k := 0; k < len(target); k += 4 {
			corr += target[k] * span[off+k]
		}
		if corr > bestCorr {
			bestCorr, best = corr, off
		}
	}
	return best
}

// OutputFrames is the mix's length in 48 kHz frames.
func OutputFrames(clips []Clip) uint64 {
	var ms float64
	for _, c := range clips {
		ms += c.OutputLen()
	}
	return uint64(math.Round(msToFrames(ms)))
}
