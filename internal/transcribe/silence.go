package transcribe

import "math"

type SilenceOptions struct {
	// RMS level below which a window counts as silent, in dBFS.
	ThresholdDb   float32
	MinDurationMs uint64
	WindowMs      uint64
}

func DefaultSilenceOptions() SilenceOptions {
	return SilenceOptions{ThresholdDb: -42, MinDurationMs: 1_000, WindowMs: 20}
}

// SilenceSpans finds spans where the audio stays below the threshold for at least
// MinDurationMs.
func SilenceSpans(p Pcm, o SilenceOptions) []TimeRange {
	mono := p.ToMono()
	window := max(int(uint64(p.SampleRate)*o.WindowMs/1000), 1)
	threshold := float32(math.Pow(10, float64(o.ThresholdDb/20)))
	toMs := func(sample int) uint64 { return uint64(sample) * 1000 / uint64(p.SampleRate) }

	var spans []TimeRange
	push := func(start, end int) {
		span := TimeRange{toMs(start), toMs(end)}
		if span.DurationMs() >= o.MinDurationMs {
			spans = append(spans, span)
		}
	}
	runStart := -1
	for offset := 0; offset < len(mono); offset += window {
		chunk := mono[offset:min(offset+window, len(mono))]
		var sum float32
		for _, s := range chunk {
			sum += float32(s * s)
		}
		rms := float32(math.Sqrt(float64(sum / float32(len(chunk)))))
		switch quiet := rms < threshold; {
		case quiet && runStart < 0:
			runStart = offset
		case !quiet && runStart >= 0:
			push(runStart, offset)
			runStart = -1
		}
	}
	if runStart >= 0 {
		push(runStart, len(mono))
	}
	return spans
}
