package trim

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wobsoriano/glint/internal/media"
)

func source(w, h int) Source {
	return Source{Path: `C:\v\clip.mp4`, Width: w, Height: h, FPS: 30, DurationMS: 10_000, Bytes: 50_000_000}
}

func TestRecognizesVideoExtensionsCaseInsensitively(t *testing.T) {
	for path, want := range map[string]bool{`C:\a\b.MP4`: true, "b.mov": true, "b.wmv": true, "b.png": false, "mp4": false} {
		if IsVideo(path) != want {
			t.Errorf("IsVideo(%q) = %v", path, !want)
		}
	}
}

func TestOutputSizeShrinksTheShortSideAndNeverUpscales(t *testing.T) {
	for _, c := range []struct {
		w, h   int
		r      Resolution
		ow, oh int
	}{
		{3840, 2160, P720, 1280, 720},
		{1080, 1920, P720, 720, 1280},
		{2560, 1080, P480, 1138, 480},
		{1280, 720, P1080, 1280, 720},
		{1281, 721, Original, 1280, 720},
	} {
		if w, h := OutputSize(c.w, c.h, c.r); w != c.ow || h != c.oh {
			t.Errorf("%dx%d at %s = %dx%d, want %dx%d", c.w, c.h, c.r.Label(), w, h, c.ow, c.oh)
		}
	}
}

func TestTrimHandlesKeepAMinimumSpanInsideTheSource(t *testing.T) {
	full := FullTrim(10_000)
	if got := full.WithStart(9_900); got != (Trim{9_750, 10_000}) {
		t.Errorf("start past the end: %v", got)
	}
	if got := full.WithEnd(100, 10_000); got != (Trim{0, 250}) {
		t.Errorf("end before the minimum: %v", got)
	}
	if got := full.WithEnd(20_000, 10_000); got != (Trim{0, 10_000}) {
		t.Errorf("end past the source: %v", got)
	}
	if mid := full.WithStart(4_000).WithEnd(6_000, 10_000); mid != (Trim{4_000, 6_000}) || mid.DurationMS() != 2_000 {
		t.Errorf("a middle span: %v", mid)
	}
}

func TestTranscodeOptionsFollowTheSettings(t *testing.T) {
	opts := Options{Resolution: P720, Quality: Small, Codec: media.HEVC, KeepAudio: false}
	got := transcodeOptions(source(3840, 2160), Trim{3_000, 5_000}, opts)
	if got.Width != 1280 || got.Height != 720 || got.Bitrate != 1_105_920 || got.Codec != media.HEVC || !got.DropAudio ||
		got.Trim == nil || *got.Trim != (media.Span{StartMS: 3_000, EndMS: 5_000}) {
		t.Fatalf("transcode options %+v trim %v", got, got.Trim)
	}
}

func TestEstimateScalesWithQualitySpanAndAudio(t *testing.T) {
	s := source(1920, 1080)
	two := Trim{0, 2_000}
	high := DefaultOptions()
	high.Quality = High
	if got := EstimateBytes(s, two, high, true); got != 1_555_200+48_000 {
		t.Errorf("high with audio: %d", got)
	}
	silent := high
	silent.KeepAudio = false
	if got := EstimateBytes(s, two, silent, true); got != 1_555_200 {
		t.Errorf("audio removed: %d", got)
	}
	if got := EstimateBytes(s, two, high, false); got != 1_555_200 {
		t.Errorf("no audio track: %d", got)
	}
	small := high
	small.Quality = Small
	if EstimateBytes(s, two, small, false) >= EstimateBytes(s, two, high, false) {
		t.Error("Small is not smaller than High")
	}
}

func TestNamesClockAndBytes(t *testing.T) {
	if got := OutputStem(filepath.Join("v", "My clip.mov")); got != "My clip (trimmed)" {
		t.Errorf("stem %q", got)
	}
	for ms, want := range map[int64]string{0: "0:00.0", 65_432: "1:05.4", 3_725_000: "1:02:05"} {
		if got := Clock(ms); got != want {
			t.Errorf("Clock(%d) = %q, want %q", ms, got, want)
		}
	}
	for n, want := range map[int64]string{512: "512 B", 2048: "2 KB", 3 << 20: "3.0 MB", 5 << 30: "5.0 GB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
	if got := sanitize(` a:b?. `); got != "a_b_" {
		t.Errorf("sanitize = %q", got)
	}
}

func TestUniquePathCountsFromTwo(t *testing.T) {
	dir := t.TempDir()
	first := uniquePath(dir, "clip (trimmed)", "mp4")
	if filepath.Base(first) != "clip (trimmed).mp4" {
		t.Fatalf("first name %q", first)
	}
	os.WriteFile(first, nil, 0o644)
	if second := uniquePath(dir, "clip (trimmed)", "mp4"); filepath.Base(second) != "clip (trimmed) (2).mp4" {
		t.Fatalf("second name %q", second)
	}
}
