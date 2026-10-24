package transcribe

import (
	"math"
	"math/rand/v2"
	"testing"
)

// speech is 3 s of 16 kHz audio with sound at 500-900 ms and 1500-2100 ms, and optional white
// noise at about -40 dBFS everywhere.
func speech(noise bool) []float32 {
	const rate = WhisperSampleRate
	var s []float32
	s = append(s, repeat(0, 500*rate/1000)...)
	s = append(s, tone(400, rate)...)
	s = append(s, repeat(0, 600*rate/1000)...)
	s = append(s, tone(600, rate)...)
	s = append(s, repeat(0, 900*rate/1000)...)
	if noise {
		r := rand.New(rand.NewPCG(1, 2))
		for i := range s {
			s[i] += float32(r.Float64()*2-1) * 0.017
		}
	}
	return s
}

func TestWordsLandOnTheirSoundAndPausesFallBetweenThem(t *testing.T) {
	tokens := []token{
		{text: " hello", atMs: 700, p: 1, startsWord: true},
		{text: " big", atMs: 1600, p: 1},
		{text: " world", atMs: 1900, p: 1},
		{text: ".", atMs: 2400, p: 1},
	}
	want := []Word{
		{Text: "hello", StartMs: 500, EndMs: 900, Confidence: 1},
		{Text: "big", StartMs: 1500, EndMs: 1700, Confidence: 1},
		{Text: "world.", StartMs: 1700, EndMs: 2100, Confidence: 1},
	}
	for _, noise := range []bool{false, true} {
		got := placeWords(tokens, speech(noise))
		if len(got) != len(want) {
			t.Fatalf("noise %v: got %+v", noise, got)
		}
		for i, w := range want {
			g := got[i]
			if g.Text != w.Text || absDiff(g.StartMs, w.StartMs) > 20 || absDiff(g.EndMs, w.EndMs) > 20 {
				t.Errorf("noise %v: word %d is %+v, want %+v within 20 ms", noise, i, g, w)
			}
		}
	}
}

func TestTokensJoinIntoWordsAndPunctuationAloneIsNoWord(t *testing.T) {
	tokens := []token{
		{text: " capt", atMs: 700, p: 0.7, startsWord: true},
		{text: "ions", atMs: 800, p: 1},
		{text: ",", atMs: 850, p: 1},
		{text: "next", atMs: 1600, p: 1, startsWord: true},
		{text: " -", atMs: 1700, p: 1},
	}
	got := placeWords(tokens, speech(false))
	if len(got) != 2 || got[0].Text != "captions," || got[1].Text != "next" || math.Abs(float64(got[0].Confidence)-0.9) > 1e-6 {
		t.Fatalf("got %+v", got)
	}
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}
