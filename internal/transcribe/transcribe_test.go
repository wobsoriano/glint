package transcribe

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func eq[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), TranscriptFile)
	tr := NewTranscript("en", []Word{{Text: "Hello,", StartMs: 120, EndMs: 480, Confidence: 0.9}})
	if err := tr.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, got, tr)
	data, _ := os.ReadFile(path)
	for _, want := range []string{`"version": 1`, `"start_ms": 120`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("saved json lacks %s:\n%s", want, data)
		}
	}
}

// testdata/transcript.json is Transcript::save output from
// `cargo run -p glint-studio --example go_fixtures`.
func TestRustTranscriptRoundTrips(t *testing.T) {
	want, err := os.ReadFile(filepath.Join("testdata", "transcript.json"))
	if err != nil {
		t.Fatal(err)
	}
	tr, err := Parse(want)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, tr.Text(), "Hello, world.")
	eq(t, tr.Words[1], Word{Text: "world.", StartMs: 520, EndMs: 910, Confidence: 0.73})
	path := filepath.Join(t.TempDir(), TranscriptFile)
	if err := tr.Save(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	eq(t, string(got), string(want))
}

func TestNormalizeStripsCaseAndPunctuation(t *testing.T) {
	eq(t, NormalizeWord("Like,"), "like")
	eq(t, NormalizeWord("it's"), "its")
	eq(t, NormalizeWord("--"), "")
}

func cw(text string, start, end uint64) Word {
	return Word{Text: text, StartMs: start, EndMs: end, Confidence: 1}
}

type lineView struct {
	Text       string
	Start, End uint64
	Words      IndexRange
}

func TestCaptionsBreakOnPauseAndLength(t *testing.T) {
	words := []Word{
		cw("Welcome", 0, 400), cw("to", 450, 550), cw("Glint.", 600, 1000), cw("Today", 2500, 2800),
		cw("we", 2850, 2950), cw("record", 3000, 3300), cw("something", 3350, 3700), cw("wonderful", 3750, 4100),
	}
	o := DefaultCaptionOptions()
	o.MaxChars = 20
	var got []lineView
	for _, l := range CaptionLines(words, o) {
		got = append(got, lineView{l.Text, l.StartMs, l.EndMs, l.Words})
	}
	eq(t, got, []lineView{
		{"Welcome to Glint.", 0, 1000, IndexRange{0, 3}},
		{"Today we record", 2500, 3300, IndexRange{3, 6}},
		{"something wonderful", 3350, 4150, IndexRange{6, 8}},
	})
}

func TestShortCaptionLineGetsMinimumDuration(t *testing.T) {
	lines := CaptionLines([]Word{cw("Hi.", 100, 300)}, DefaultCaptionOptions())
	eq(t, lines[0].EndMs, uint64(900))
}

func TestCaptionsBreakOnDuration(t *testing.T) {
	var words []Word
	for i := uint64(0); i < 6; i++ {
		words = append(words, cw("a", i*1000, i*1000+900))
	}
	var got []IndexRange
	for _, l := range CaptionLines(words, DefaultCaptionOptions()) {
		got = append(got, l.Words)
	}
	eq(t, got, []IndexRange{{0, 4}, {4, 6}})
}

func TestEmptyInputGivesNoCaptionLines(t *testing.T) {
	if len(CaptionLines(nil, DefaultCaptionOptions())) != 0 {
		t.Fatal("expected no lines")
	}
}

func spoken(text string) []Word {
	var out []Word
	for i, s := range strings.Fields(text) {
		out = append(out, Word{Text: s, StartMs: uint64(i) * 300, EndMs: uint64(i)*300 + 250, Confidence: 1})
	}
	return out
}

func TestHesitationsAlwaysCount(t *testing.T) {
	eq(t, FillerRanges(spoken("So um this is uh, a Test")), []IndexRange{{1, 2}, {4, 5}})
	eq(t, FillerRanges(spoken("Umm. Hmm?")), []IndexRange{{0, 1}, {1, 2}})
}

func TestLikeCountsOnlyWhenSetApart(t *testing.T) {
	eq(t, FillerRanges(spoken("I like this tool")), nil)
	eq(t, FillerRanges(spoken("it was, like, really fast")), []IndexRange{{2, 3}})
}

func TestYouKnowCountsOnlyWhenSetApart(t *testing.T) {
	eq(t, FillerRanges(spoken("do you know her")), nil)
	eq(t, FillerRanges(spoken("it is, you know, fine")), []IndexRange{{2, 4}})
}

func TestAPauseSetsLikeApart(t *testing.T) {
	ws := spoken("it was like really fast")
	for i := range ws[2:] {
		ws[2+i].StartMs += 400
		ws[2+i].EndMs += 400
	}
	for i := range ws[3:] {
		ws[3+i].StartMs += 400
		ws[3+i].EndMs += 400
	}
	eq(t, FillerRanges(ws), []IndexRange{{2, 3}})
}

func TestUmAndLikeInsideOtherWordsDoNotCount(t *testing.T) {
	eq(t, FillerRanges(spoken("umbrella likely")), nil)
}

func tone(ms, rate int) []float32 {
	out := make([]float32, ms*rate/1000)
	for i := range out {
		out[i] = 0.3 * float32(math.Sin(float64(float32(i)*0.05)))
	}
	return out
}

func repeat(v float32, n int) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = v
	}
	return out
}

func TestFindsLongQuietSpansOnly(t *testing.T) {
	const rate = 16_000
	var s []float32
	s = append(s, tone(500, rate)...)
	s = append(s, repeat(0.0005, 1_200*rate/1000)...)
	s = append(s, tone(300, rate)...)
	s = append(s, repeat(0, 400*rate/1000)...)
	s = append(s, tone(300, rate)...)
	s = append(s, repeat(0, 1_500*rate/1000)...)
	eq(t, SilenceSpans(MonoPcm(s, rate), DefaultSilenceOptions()), []TimeRange{{500, 1_700}, {2_700, 4_200}})
}

func ew(start, end uint64) Word { return Word{Text: "w", StartMs: start, EndMs: end, Confidence: 1} }

func cutOne(words []Word, sel IndexRange, dur uint64) []TimeRange {
	return WordCutRanges(words, []IndexRange{sel}, dur)
}

func TestCutBitesIntoPausesButNotNeighbors(t *testing.T) {
	words := []Word{ew(0, 400), ew(500, 700), ew(1_500, 1_800), ew(1_820, 2_000)}
	eq(t, cutOne(words, IndexRange{1, 2}, 3_000), []TimeRange{{450, 820}})
	eq(t, cutOne(words, IndexRange{2, 3}, 3_000), []TimeRange{{1_380, 1_810}})
}

func TestEdgesPadTowardMediaBounds(t *testing.T) {
	words := []Word{ew(100, 400), ew(500, 900)}
	eq(t, cutOne(words, IndexRange{0, 1}, 1_000), []TimeRange{{50, 450}})
	eq(t, cutOne(words, IndexRange{1, 2}, 1_000), []TimeRange{{450, 950}})
}

func TestAdjacentSelectionsMergeAndBadRangesAreIgnored(t *testing.T) {
	words := []Word{ew(0, 400), ew(400, 800), ew(800, 1_200)}
	eq(t, WordCutRanges(words, []IndexRange{{1, 2}, {0, 1}, {2, 9}, {3, 3}}, 2_000), []TimeRange{{0, 800}})
}

func TestSilenceCutsKeepBreathingRoom(t *testing.T) {
	spans := []TimeRange{{1_000, 3_000}, {5_000, 5_900}}
	eq(t, SilenceCutRanges(spans, 350, 300), []TimeRange{{1_350, 2_650}})
}

const script = "Hello and welcome! Today, we're recording a short demo of the new editor."

type idx struct {
	I  int
	Ok bool
}

func feed(tr *ScriptTracker, words ...string) idx {
	i, ok := tr.Feed(words)
	return idx{i, ok}
}

func TestFollowsSpeechIgnoringCaseAndPunctuation(t *testing.T) {
	tr := NewScriptTracker(script)
	i, ok := tr.CurrentIndex()
	eq(t, idx{i, ok}, idx{0, false})
	eq(t, feed(tr, "hello", "AND", "Welcome"), idx{2, true})
	eq(t, feed(tr, "today", "were", "recording"), idx{5, true})
}

func TestSkipsMisheardAndMissingWords(t *testing.T) {
	tr := NewScriptTracker(script)
	eq(t, feed(tr, "hello", "and", "welcome", "toady", "we're", "recorded", "short"), idx{7, true})
	eq(t, feed(tr, "demo", "of", "the", "new", "editor"), idx{12, true})
}

func TestNeverMovesBackwardAndIgnoresFarJumps(t *testing.T) {
	tr := NewScriptTracker(script)
	feed(tr, "hello", "and", "welcome", "today")
	eq(t, feed(tr, "hello", "and"), idx{3, true})
	eq(t, feed(tr, "editor"), idx{3, true})
}

func TestRejoinsWordsTheRecognizerSplit(t *testing.T) {
	tr := NewScriptTracker("Open the teleprompter now")
	eq(t, feed(tr, "open", "the", "tele", "prompter"), idx{2, true})
}

func TestFuzzyMatchingIsLimitedToLongWords(t *testing.T) {
	if !matches("recognize", "recognise") {
		t.Fatal("recognize ~ recognise")
	}
	if matches("cat", "cut") {
		t.Fatal("cat !~ cut")
	}
	eq(t, editDistance("kitten", "sitting"), 3)
}

func TestDownsampling48kStereoHalvesAndThirdsLength(t *testing.T) {
	stereo := make([]float32, 0, 96_000)
	for range 48_000 {
		stereo = append(stereo, 0.5, -0.5)
	}
	out := ToWhisperRate(Pcm{Samples: stereo, SampleRate: 48_000, Channels: 2})
	eq(t, len(out), 16_000)
	for _, s := range out {
		if math.Abs(float64(s)) >= 1e-6 {
			t.Fatalf("sample %v", s)
		}
	}
}

func TestDownsamplingPreservesALowTone(t *testing.T) {
	sine := func(n int, rate float32) []float32 {
		out := make([]float32, n)
		for i := range out {
			out[i] = float32(math.Sin(float64(float32(i) / rate * 440 * 2 * math.Pi)))
		}
		return out
	}
	out := Resample(sine(48_000, 48_000), 48_000, 16_000)
	want := sine(16_000, 16_000)
	var maxErr float64
	for i := range out {
		maxErr = math.Max(maxErr, math.Abs(float64(out[i]-want[i])))
	}
	if maxErr >= 0.05 {
		t.Fatalf("max error %v", maxErr)
	}
}

func TestUpsamplingInterpolates(t *testing.T) {
	eq(t, Resample([]float32{0, 1}, 8_000, 16_000), []float32{0, 0.5, 1, 1})
}
