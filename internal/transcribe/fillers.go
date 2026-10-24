package transcribe

import (
	"slices"
	"strings"
)

var hesitations = []string{
	"um", "umm", "ummm", "uh", "uhh", "uhhh", "uhm", "er", "erm", "ah", "ahh", "hmm", "hm", "mm", "mmm", "mhm",
}

var discourse = [][]string{{"you", "know"}, {"i", "mean"}, {"like"}}

const standalonePauseMs = 250

// FillerRanges returns the word index ranges that are filler, in order and non-overlapping.
func FillerRanges(words []Word) []IndexRange {
	normalized := make([]string, len(words))
	for i, w := range words {
		normalized[i] = NormalizeWord(w.Text)
	}
	var ranges []IndexRange
	for i := 0; i < len(words); {
		if slices.Contains(hesitations, normalized[i]) {
			ranges = append(ranges, IndexRange{i, i + 1})
			i++
			continue
		}
		matched := 0
		for _, phrase := range discourse {
			end := i + len(phrase)
			if end <= len(words) && slices.Equal(phrase, normalized[i:end]) && isStandalone(words, IndexRange{i, end}) {
				matched = len(phrase)
				break
			}
		}
		if matched > 0 {
			ranges = append(ranges, IndexRange{i, i + matched})
			i += matched
		} else {
			i++
		}
	}
	return ranges
}

func isStandalone(words []Word, r IndexRange) bool {
	first, last := words[r.Start], words[r.End-1]
	commaBefore := r.Start == 0 || endsWithPauseMark(words[r.Start-1].Text)
	commaAfter := endsWithPauseMark(last.Text)
	pauseBefore := r.Start == 0 || subSat(first.StartMs, words[r.Start-1].EndMs) >= standalonePauseMs
	pauseAfter := r.End == len(words) || subSat(words[r.End].StartMs, last.EndMs) >= standalonePauseMs
	return (commaBefore || pauseBefore) && (commaAfter || pauseAfter)
}

func endsWithPauseMark(text string) bool {
	for _, mark := range []string{",", ".", "?", "!", ";", ":", "-", "…"} {
		if strings.HasSuffix(text, mark) {
			return true
		}
	}
	return false
}
