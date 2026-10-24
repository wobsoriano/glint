package studio

import (
	"math"
	"testing"
)

func renderLeft(m *AudioMixer, frames int) []float32 {
	out := make([]float32, frames*2)
	m.Render(out)
	left := make([]float32, frames)
	for i := range left {
		left[i] = out[2*i]
	}
	return left
}

func TestUnitSpeedClipsCopySourceSamplesAcrossACut(t *testing.T) {
	ramp := make([]float32, 48_000)
	for i := range ramp {
		ramp[i] = float32(i) / 48_000
	}
	p := NewProject(1000)
	p.Clips = []Clip{{0, 100, 1}, {500, 600, 1}}
	p.Audio.Mic.Volume = 2
	m := NewAudioMixer(&AudioSources{mic: ramp}, &p, 99)
	out := renderLeft(m, 5_000)
	eq(t, out[0], ramp[4752]*2)
	eq(t, out[48], ramp[24_000]*2)
	eq(t, out[4847], ramp[28_799]*2)
	eq(t, out[4848], float32(0))
}

func TestStretchedClipKeepsPitchAndLevel(t *testing.T) {
	const hz = 440.0
	tone := make([]float32, 96_000)
	for i := range tone {
		tone[i] = float32(math.Sin(float64(float32(i)*hz*2*math.Pi/48_000))) * 0.5
	}
	p := NewProject(2000)
	p.Clips = []Clip{{0, 2000, 2}}
	m := NewAudioMixer(&AudioSources{mic: tone}, &p, 0)
	body := renderLeft(m, 48_000)[4_800:43_200]
	crossings := 0
	for i := 1; i < len(body); i++ {
		if body[i-1] < 0 && body[i] >= 0 {
			crossings++
		}
	}
	measured := float64(crossings) / (float64(len(body)) / 48_000)
	if math.Abs(measured-hz) >= 6 {
		t.Errorf("pitch %.1f Hz, want %v", measured, hz)
	}
	var sum float64
	for _, s := range body {
		sum += float64(s * s)
	}
	if rms := math.Sqrt(sum / float64(len(body))); math.Abs(rms-0.5/math.Sqrt2) >= 0.03 {
		t.Errorf("rms %.4f, want %.4f", rms, 0.5/math.Sqrt2)
	}
	eq(t, OutputFrames(p.Clips), uint64(48_000))
}

func TestDisabledTrackIsSilent(t *testing.T) {
	p := NewProject(100)
	p.Audio.Mic.Enabled = false
	s := &AudioSources{mic: make([]float32, 4800)}
	for i := range s.mic {
		s.mic[i] = 0.5
	}
	eq(t, s.Audible(p.Audio), false)
	for _, v := range renderLeft(NewAudioMixer(s, &p, 0), 100) {
		if v != 0 {
			t.Fatalf("disabled mic rendered %v", v)
		}
	}
}

func TestSystemAudioMixesOverMicInStereo(t *testing.T) {
	p := NewProject(100)
	p.Audio.System.Volume = 0.5
	s := &AudioSources{mic: []float32{0.25, 0.25}, system: []float32{0.5, -0.25, 0.5, -0.25}}
	out := make([]float32, 6)
	NewAudioMixer(s, &p, 0).Render(out)
	eq(t, out, []float32{0.5, 0.125, 0.5, 0.125, 0, 0})
}
