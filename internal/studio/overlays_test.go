package studio

import (
	"testing"

	"github.com/wobsoriano/glint/internal/media/input"
)

func word(text string, start, end float64) CaptionWord {
	return CaptionWord{Text: text, StartMs: start, EndMs: end}
}

func cue(text string, start, end float64) CaptionCue {
	return CaptionCue{Text: text, StartMs: start, EndMs: end}
}

func captionAt(t *testing.T, track CaptionTrack, ms float64) CaptionFrame {
	t.Helper()
	f, ok := track.At(ms)
	if !ok {
		t.Fatalf("no caption at %v", ms)
	}
	return f
}

func TestCaptionsShowCueLinesAndTrackTheSpokenWord(t *testing.T) {
	p := NewProject(20_000)
	p.CaptionCues = []CaptionCue{cue("Open the settings", 1_000, 2_000), cue("Then save", 4_000, 4_800)}
	words := []CaptionWord{
		word("Open", 1_000, 1_300), word("the", 1_300, 1_450), word("settings", 1_450, 2_000),
		word("Then", 4_000, 4_300), word("save", 4_300, 4_800),
	}
	track := BuildCaptionTrack(&p, words)
	eq(t, captionAt(t, track, 1_350), CaptionFrame{Words: []string{"Open", "the", "settings"}, Active: 1, Spoken: 2})
	if _, ok := track.At(3_000); ok {
		t.Fatal("nothing between lines")
	}
	eq(t, captionAt(t, track, 4_500), CaptionFrame{Words: []string{"Then", "save"}, Active: 1, Spoken: 2})
}

func TestCutWordsDisappearFromCaptions(t *testing.T) {
	p := NewProject(10_000)
	p.CaptionCues = []CaptionCue{cue("um hello", 1_200, 2_600)}
	p.CutSourceRanges([]Span{{1_000, 2_000}})
	track := BuildCaptionTrack(&p, []CaptionWord{word("um", 1_200, 1_500), word("hello", 2_100, 2_600)})
	f := captionAt(t, track, 1_200)
	eq(t, f.Words, []string{"hello"})
	eq(t, f.Active, 0)
}

func TestEditedCueTextShowsWithTimingSpreadOverItsWords(t *testing.T) {
	p := NewProject(10_000)
	words := []CaptionWord{word("Glint", 1_000, 1_400), word("is", 1_400, 1_600), word("grate", 2_000, 2_400)}
	p.CaptionCues = []CaptionCue{cue("Glint is great", 1_000, 2_400)}
	track := BuildCaptionTrack(&p, words)
	eq(t, captionAt(t, track, 2_100), CaptionFrame{Words: []string{"Glint", "is", "great"}, Active: 2, Spoken: 3})
	eq(t, captionAt(t, track, 1_800).Active, -1)

	p.CaptionCues[0].Text = "Glint really is great"
	track = BuildCaptionTrack(&p, words)
	lit := func(ms float64) int { return captionAt(t, track, ms).Active }
	eq(t, [4]int{lit(1_100), lit(1_350), lit(1_550), lit(2_300)}, [4]int{0, 1, 2, 3})

	p.CaptionCues[0].Text = "Great"
	track = BuildCaptionTrack(&p, words)
	eq(t, captionAt(t, track, 1_050).Words, []string{"Great"})
	eq(t, captionAt(t, track, 2_300).Active, 0)
}

func TestKeystrokePopsInSwapsAndFades(t *testing.T) {
	p := NewProject(10_000)
	in := input.Timeline{Keys: []input.KeyPress{{T: 1_000, Label: "Ctrl+Shift+S"}, {T: 1_500, Label: "Enter"}}}
	track := BuildKeyTrack(&p, &in)
	if _, ok := track.At(900); ok {
		t.Fatal("nothing before the first key")
	}
	first, _ := track.At(1_080)
	eq(t, [2]string{first.Modifiers, first.Key}, [2]string{"Ctrl+Shift+", "S"})
	if !(first.Opacity > 0.5 && first.Opacity < 1) {
		t.Fatalf("popping in: %v", first.Opacity)
	}
	swapped, _ := track.At(1_520)
	eq(t, [2]string{swapped.Modifiers, swapped.Key}, [2]string{"", "Enter"})
	eq(t, swapped.Opacity, 1.0)
	if f, _ := track.At(3_000); f.Opacity >= 0.5 {
		t.Fatalf("fading: %v", f.Opacity)
	}
	if _, ok := track.At(3_100); ok {
		t.Fatal("gone after its life")
	}
}

func TestLabelsSplitIntoModifiersAndKeyOnBothPlatforms(t *testing.T) {
	for label, want := range map[string][2]string{
		"Ctrl+Shift+S": {"Ctrl+Shift+", "S"},
		"Enter":        {"", "Enter"},
		"⇧⌘S":          {"⇧⌘", "S"},
		"⌘Space":       {"⌘", "Space"},
		"↩":            {"", "↩"},
	} {
		m, k := splitLabel(label)
		eq(t, [2]string{m, k}, want)
	}
}

func TestClickRingExpandsAndFades(t *testing.T) {
	p := NewProject(5_000)
	in := input.Timeline{Clicks: []input.Click{{T: 1_000, Up: 1_100, X: 10, Y: 20, Button: input.ButtonLeft}}}
	track := BuildClickTrack(&p, &in)
	eq(t, len(track.At(999, 10)), 0)
	early := track.At(1_000, 10)[0]
	eq(t, [4]float64{early.X, early.Y, early.ImpactRadius, early.RingAlpha}, [4]float64{10, 20, 3.8, 0})
	late := track.At(1_390, 10)[0]
	if !(late.RingRadius > 15 && late.RingAlpha < 0.05) {
		t.Fatalf("%+v", late)
	}
	eq(t, len(track.At(1_401, 10)), 0)
}
