// Package transcribe holds the transcript model and the speech-free logic built on it:
// caption lines, filler words, silences, cut ranges and teleprompter tracking.
package transcribe

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode"
)

const (
	TranscriptFile    = "transcript.json"
	TranscriptVersion = 1
)

type Transcript struct {
	Version uint32 `json:"version"`
	// ISO 639-1 code reported by the model, e.g. "en".
	Language string `json:"language"`
	Words    []Word `json:"words"`
}

// Word is one recognized word on the source timeline. Text has no surrounding whitespace and
// keeps attached punctuation ("know," or "Okay.").
type Word struct {
	Text    string `json:"text"`
	StartMs uint64 `json:"start_ms"`
	EndMs   uint64 `json:"end_ms"`
	// Mean token probability, 0..=1.
	Confidence float32 `json:"confidence"`
}

// TimeRange is a half-open span of source media time in milliseconds.
type TimeRange struct {
	StartMs uint64 `json:"start_ms"`
	EndMs   uint64 `json:"end_ms"`
}

func (r TimeRange) DurationMs() uint64 { return subSat(r.EndMs, r.StartMs) }

// IndexRange is a half-open range of indices into a word slice.
type IndexRange struct{ Start, End int }

func NewTranscript(language string, words []Word) Transcript {
	return Transcript{Version: TranscriptVersion, Language: language, Words: words}
}

func (t Transcript) Text() string {
	parts := make([]string, len(t.Words))
	for i, w := range t.Words {
		parts[i] = w.Text
	}
	return strings.Join(parts, " ")
}

func (t *Transcript) UnmarshalJSON(data []byte) error {
	type plain Transcript
	if err := requireKeys(data, "version", "language", "words"); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(t))
}

func (t Transcript) MarshalJSON() ([]byte, error) {
	type plain Transcript
	if t.Words == nil {
		t.Words = []Word{}
	}
	return json.Marshal(plain(t))
}

func (w *Word) UnmarshalJSON(data []byte) error {
	type plain Word
	if err := requireKeys(data, "text", "start_ms", "end_ms", "confidence"); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(w))
}

func Load(path string) (Transcript, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Transcript{}, fmt.Errorf("reading %s: %w", path, err)
	}
	t, err := Parse(data)
	if err != nil {
		return Transcript{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return t, nil
}

func Parse(data []byte) (Transcript, error) {
	var t Transcript
	if err := json.Unmarshal(data, &t); err != nil {
		return Transcript{}, err
	}
	if t.Version > TranscriptVersion {
		return Transcript{}, fmt.Errorf("transcript version %d is newer than supported %d", t.Version, TranscriptVersion)
	}
	return t, nil
}

// Save writes through a temp file and rename so a crash never leaves a half-written transcript.
func (t Transcript) Save(path string) error {
	data, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, data)
}

// NormalizeWord keeps lowercase letters and digits only, so "Like," compares equal to "like"
// and "it's" to "its".
func NormalizeWord(text string) string {
	var b strings.Builder
	for _, r := range text {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteString(strings.ToLower(string(r)))
		}
	}
	return b.String()
}

func subSat(a, b uint64) uint64 {
	if a < b {
		return 0
	}
	return a - b
}
