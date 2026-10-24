package transcribe

import (
	"math"
	"slices"
)

// Pcm is interleaved float32 PCM borrowed from a decoder or recorder.
type Pcm struct {
	Samples    []float32
	SampleRate uint32
	Channels   uint16
}

func MonoPcm(samples []float32, sampleRate uint32) Pcm {
	return Pcm{Samples: samples, SampleRate: sampleRate, Channels: 1}
}

func (p Pcm) DurationMs() uint64 {
	frames := uint64(len(p.Samples)) / uint64(max(p.Channels, 1))
	return frames * 1000 / uint64(max(p.SampleRate, 1))
}

func (p Pcm) ToMono() []float32 {
	channels := int(max(p.Channels, 1))
	if channels == 1 {
		return slices.Clone(p.Samples)
	}
	out := make([]float32, len(p.Samples)/channels)
	for i := range out {
		var sum float32
		for _, s := range p.Samples[i*channels : (i+1)*channels] {
			sum += s
		}
		out[i] = sum / float32(channels)
	}
	return out
}

const WhisperSampleRate = 16_000

// ToWhisperRate converts to mono at 16 kHz, the only input whisper accepts. Downsampling
// averages the source samples each output sample covers, which is enough low-pass for speech.
func ToWhisperRate(p Pcm) []float32 {
	return Resample(p.ToMono(), p.SampleRate, WhisperSampleRate)
}

func Resample(mono []float32, from, to uint32) []float32 {
	if from == to || len(mono) == 0 {
		return slices.Clone(mono)
	}
	ratio := float64(from) / float64(to)
	outLen := int(math.Floor(float64(len(mono)) / ratio))
	last := len(mono) - 1
	out := make([]float32, outLen)
	for i := range out {
		center := float64(i) * ratio
		if ratio > 1 {
			lo := int(math.Max(math.Ceil(center-ratio/2), 0))
			hi := min(int(math.Floor(center+ratio/2)), last)
			span := mono[min(lo, hi) : hi+1]
			var sum float32
			for _, s := range span {
				sum += s
			}
			out[i] = sum / float32(len(span))
		} else {
			i0 := int(math.Floor(center))
			i1 := min(i0+1, last)
			t := float32(center - float64(i0))
			out[i] = float32(mono[i0]*(1-t)) + float32(mono[i1]*t)
		}
	}
	return out
}
