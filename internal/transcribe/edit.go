package transcribe

import (
	"cmp"
	"slices"
)

const maxWordPaddingMs = 120

// WordCutRanges returns source-time ranges that remove the selected words, sorted and
// merged. Each cut extends into the surrounding pauses by up to half the gap, never into a
// neighboring word.
func WordCutRanges(words []Word, selections []IndexRange, sourceDurationMs uint64) []TimeRange {
	var cuts []TimeRange
	for _, r := range selections {
		if r.Start >= r.End || r.End > len(words) {
			continue
		}
		first, last := words[r.Start], words[r.End-1]
		var prevEnd uint64
		if r.Start > 0 {
			prevEnd = words[r.Start-1].EndMs
		}
		nextStart := sourceDurationMs
		if r.End < len(words) {
			nextStart = words[r.End].StartMs
		}
		padBefore := min(maxWordPaddingMs, subSat(first.StartMs, prevEnd)/2)
		padAfter := min(maxWordPaddingMs, subSat(nextStart, last.EndMs)/2)
		cut := TimeRange{subSat(first.StartMs, padBefore), min(last.EndMs+padAfter, sourceDurationMs)}
		if cut.EndMs > cut.StartMs {
			cuts = append(cuts, cut)
		}
	}
	return Merge(cuts)
}

// SilenceCutRanges turns silence spans into cuts that keep keepMs of quiet on each side of
// the splice, so trimmed narration still breathes. Spans too short to leave minCutMs are
// skipped.
func SilenceCutRanges(spans []TimeRange, keepMs, minCutMs uint64) []TimeRange {
	var cuts []TimeRange
	for _, s := range spans {
		cut := TimeRange{s.StartMs + keepMs, subSat(s.EndMs, keepMs)}
		if cut.EndMs > cut.StartMs && cut.DurationMs() >= minCutMs {
			cuts = append(cuts, cut)
		}
	}
	return Merge(cuts)
}

// Merge sorts ranges by start and joins the ones that overlap or touch.
func Merge(ranges []TimeRange) []TimeRange {
	ranges = slices.Clone(ranges)
	slices.SortStableFunc(ranges, func(a, b TimeRange) int {
		return cmp.Compare(a.StartMs, b.StartMs)
	})
	merged := make([]TimeRange, 0, len(ranges))
	for _, r := range ranges {
		if n := len(merged); n > 0 && r.StartMs <= merged[n-1].EndMs {
			merged[n-1].EndMs = max(merged[n-1].EndMs, r.EndMs)
		} else {
			merged = append(merged, r)
		}
	}
	return merged
}
