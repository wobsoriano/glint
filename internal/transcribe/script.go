package transcribe

import (
	"strings"
	"unicode/utf8"
)

const lookahead = 8

type scriptWord struct {
	normalized   string
	displayIndex int
}

// ScriptTracker follows a teleprompter script from recognized speech. Progress only moves
// forward.
type ScriptTracker struct {
	words          []scriptWord
	displayLen     int
	position       int
	previousSpoken string
}

func NewScriptTracker(script string) *ScriptTracker {
	display := strings.Fields(script)
	t := &ScriptTracker{displayLen: len(display)}
	for i, w := range display {
		if n := NormalizeWord(w); n != "" {
			t.words = append(t.words, scriptWord{n, i})
		}
	}
	return t
}

// DisplayLen is the number of whitespace-separated words in the script, the range of
// CurrentIndex.
func (t *ScriptTracker) DisplayLen() int { return t.displayLen }

// CurrentIndex is the index (into the script's whitespace-separated words) of the last word
// read; ok is false before the first match.
func (t *ScriptTracker) CurrentIndex() (index int, ok bool) {
	if t.position == 0 {
		return 0, false
	}
	return t.words[t.position-1].displayIndex, true
}

// Feed takes newly recognized words in spoken order and returns the updated index.
func (t *ScriptTracker) Feed(spoken []string) (index int, ok bool) {
	for _, w := range spoken {
		word := NormalizeWord(w)
		if word == "" {
			continue
		}
		joined := t.previousSpoken + word
		end := min(t.position+lookahead, len(t.words))
		hit := -1
		for i := t.position; i < end && hit < 0; i++ {
			if matches(t.words[i].normalized, word) {
				hit = i
			}
		}
		for i := t.position; i < end && hit < 0; i++ {
			if t.words[i].normalized == joined {
				hit = i
			}
		}
		if hit >= 0 {
			t.position = hit + 1
		}
		t.previousSpoken = word
	}
	return t.CurrentIndex()
}

func matches(script, spoken string) bool {
	if script == spoken {
		return true
	}
	longest := max(utf8.RuneCountInString(script), utf8.RuneCountInString(spoken))
	return longest >= 5 && editDistance(script, spoken)*4 <= longest
}

func editDistance(a, b string) int {
	br := []rune(b)
	row := make([]int, len(br)+1)
	for j := range row {
		row[j] = j
	}
	i := 0
	for _, ca := range a {
		diagonal := row[0]
		row[0] = i + 1
		for j, cb := range br {
			above := row[j+1]
			cost := 0
			if ca != cb {
				cost = 1
			}
			row[j+1] = min(diagonal+cost, above+1, row[j]+1)
			diagonal = above
		}
		i++
	}
	return row[len(br)]
}
