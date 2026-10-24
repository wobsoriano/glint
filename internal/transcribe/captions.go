package transcribe

import (
	"strings"
	"unicode/utf8"
)

type CaptionOptions struct {
	MaxChars int
	// A pause longer than this starts a new line.
	MaxGapMs      uint64
	MaxDurationMs uint64
	// Short lines stay on screen at least this long.
	MinDurationMs uint64
}

func DefaultCaptionOptions() CaptionOptions {
	return CaptionOptions{MaxChars: 42, MaxGapMs: 900, MaxDurationMs: 4_000, MinDurationMs: 800}
}

type CaptionLine struct {
	StartMs uint64
	EndMs   uint64
	Text    string
	// Indices into the transcript's words, for karaoke highlighting and seeking.
	Words IndexRange
}

func CaptionLines(words []Word, o CaptionOptions) []CaptionLine {
	var lines []CaptionLine
	start := 0
	for i := 1; i <= len(words); i++ {
		breaks := i == len(words)
		if !breaks {
			line, next := words[start:i], words[i]
			chars := -1
			for _, w := range line {
				chars += utf8.RuneCountInString(w.Text) + 1
			}
			gap := subSat(next.StartMs, line[len(line)-1].EndMs)
			breaks = chars+1+utf8.RuneCountInString(next.Text) > o.MaxChars ||
				gap > o.MaxGapMs ||
				subSat(next.EndMs, line[0].StartMs) > o.MaxDurationMs
		}
		if breaks {
			line := words[start:i]
			startMs, endMs := line[0].StartMs, line[0].StartMs
			texts := make([]string, len(line))
			for k, w := range line {
				endMs = max(endMs, w.EndMs)
				texts[k] = w.Text
			}
			lines = append(lines, CaptionLine{
				StartMs: startMs,
				EndMs:   max(endMs, startMs+o.MinDurationMs),
				Text:    strings.Join(texts, " "),
				Words:   IndexRange{start, i},
			})
			start = i
		}
	}
	return lines
}
