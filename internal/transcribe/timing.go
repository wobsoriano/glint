package transcribe

import (
	"math"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// token is a text token of a decode. atMs is its DTW time, which lands near the middle of the
// token's sound rather than at either edge.
type token struct {
	text string
	atMs int64
	p    float32
	// startsWord is set on the first token of a segment, which starts a word even without a
	// leading space.
	startsWord bool
}

const (
	frameMs = 10
	// A frame is a pause, not part of a word, when it is this far below the word's loudest
	// frame or this close to the recording's noise floor.
	quietBelowPeakDb  = 40
	quietAboveFloorDb = 6
	// Bounds on the speaking rate estimated from the DTW times, in ms per letter, so a short
	// transcript spanning a long pause cannot stretch its words across the pause.
	minMsPerLetter, maxMsPerLetter, defaultMsPerLetter = 40, 120, 70
)

type wordSpan struct {
	text strings.Builder
	// first and last index the word's spoken tokens; a word with none is punctuation only.
	first, last int
	hasSpoken   bool
	pSum        float32
	pieces      uint32
}

type spokenToken struct {
	atMs    int64
	letters int
}

// placeWords joins tokens into words and times them on samples (16 kHz mono). Each spoken
// token extends halfway, by letter count, toward its neighbors' DTW times, and no further than
// one typical token length from its own; a word spans its spoken tokens, less any quiet audio
// at its edges.
func placeWords(tokens []token, samples []float32) []Word {
	var spans []*wordSpan
	var spoken []spokenToken
	for _, t := range tokens {
		r, _ := utf8.DecodeRuneInString(t.text)
		if len(spans) == 0 || t.startsWord || unicode.IsSpace(r) {
			spans = append(spans, &wordSpan{})
		}
		w := spans[len(spans)-1]
		w.text.WriteString(t.text)
		w.pSum += t.p
		w.pieces++
		if n := letters(t.text); n > 0 {
			at := max(t.atMs, 0)
			if len(spoken) > 0 {
				at = max(at, spoken[len(spoken)-1].atMs)
			}
			spoken = append(spoken, spokenToken{atMs: at, letters: n})
			if !w.hasSpoken {
				w.first, w.hasSpoken = len(spoken)-1, true
			}
			w.last = len(spoken) - 1
		}
	}

	lo, hi := tokenExtents(spoken, int64(len(samples))*1000/WhisperSampleRate)
	levels := frameLevels(samples)
	floor := noiseFloor(levels)
	var words []Word
	for _, w := range spans {
		if !w.hasSpoken {
			continue
		}
		start, end := trimQuiet(levels, floor, lo[w.first], hi[w.last], spoken[w.first].atMs, spoken[w.last].atMs)
		words = append(words, Word{Text: strings.TrimSpace(w.text.String()), StartMs: uint64(start), EndMs: uint64(end), Confidence: w.pSum / float32(w.pieces)})
	}
	return words
}

func letters(s string) int {
	n := 0
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			n++
		}
	}
	return n
}

// tokenExtents is how far each spoken token's sound may reach on either side of its DTW time.
func tokenExtents(spoken []spokenToken, durationMs int64) (lo, hi []int64) {
	var rates []float64
	for i := 1; i < len(spoken); i++ {
		a, b := spoken[i-1], spoken[i]
		rates = append(rates, float64(b.atMs-a.atMs)/(float64(a.letters+b.letters)/2))
	}
	rate := float64(defaultMsPerLetter)
	if len(rates) > 0 {
		slices.Sort(rates)
		rate = min(max(rates[len(rates)/2], minMsPerLetter), maxMsPerLetter)
	}
	lo, hi = make([]int64, len(spoken)), make([]int64, len(spoken))
	for i, t := range spoken {
		reach := int64(rate * float64(t.letters))
		lo[i], hi[i] = max(t.atMs-reach, 0), min(t.atMs+reach, max(durationMs, t.atMs))
	}
	for i := 1; i < len(spoken); i++ {
		a, b := spoken[i-1], spoken[i]
		split := a.atMs + (b.atMs-a.atMs)*int64(a.letters)/int64(a.letters+b.letters)
		hi[i-1], lo[i] = min(hi[i-1], split), max(lo[i], split)
	}
	return lo, hi
}

// frameLevels is the RMS level of each 10 ms frame of 16 kHz samples, in dBFS.
func frameLevels(samples []float32) []float64 {
	const n = WhisperSampleRate * frameMs / 1000
	levels := make([]float64, 0, len(samples)/n+1)
	for i := 0; i < len(samples); i += n {
		var sum float64
		chunk := samples[i:min(i+n, len(samples))]
		for _, s := range chunk {
			sum += float64(s) * float64(s)
		}
		levels = append(levels, 10*math.Log10(sum/float64(len(chunk))+1e-12))
	}
	return levels
}

// noiseFloor is the level a tenth of the frames stay under; pauses in narration take up more
// than that, so it is the level of the background between words.
func noiseFloor(levels []float64) float64 {
	if len(levels) == 0 {
		return math.Inf(-1)
	}
	sorted := slices.Sorted(slices.Values(levels))
	return sorted[len(sorted)/10]
}

// trimQuiet pulls start forward and end back over quiet frames, but never past the first and
// last DTW times.
func trimQuiet(levels []float64, floor float64, start, end, firstAt, lastAt int64) (int64, int64) {
	if len(levels) == 0 || end <= start {
		return start, end
	}
	frame := func(ms int64) int { return min(int(ms/frameMs), len(levels)-1) }
	peak := math.Inf(-1)
	for f := frame(start); f <= frame(end-1); f++ {
		peak = max(peak, levels[f])
	}
	threshold := max(peak-quietBelowPeakDb, floor+quietAboveFloorDb)
	quiet := func(ms int64) bool { return levels[frame(ms)] < threshold }
	for start+frameMs <= firstAt && quiet(start) {
		start += frameMs
	}
	for end-frameMs >= lastAt && quiet(end-frameMs) {
		end -= frameMs
	}
	return start, end
}
