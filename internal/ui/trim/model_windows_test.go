package trim

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/ui/trim/internal/testclip"
)

// TestEachQualityComesOutNearItsEstimate encodes a small clip at every quality, as the
// Quality buttons offer it, and holds each estimate to the file the export writes.
func TestEachQualityComesOutNearItsEstimate(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	if err := testclip.Write(src, 640, 360, 30, 4); err != nil {
		t.Fatal(err)
	}
	s := Source{Path: src, Width: 640, Height: 360, FPS: 30, DurationMS: 4_000}
	span := Trim{1_000, 4_000}
	var last int64
	for i, q := range Qualities {
		opts := DefaultOptions()
		opts.Quality = q
		est := EstimateBytes(s, span, opts, true)
		out := filepath.Join(dir, q.Label()+".mp4")
		if _, err := media.Transcode(context.Background(), src, out, transcodeOptions(s, span, opts), nil); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(out)
		if err != nil {
			t.Fatal(err)
		}
		got := st.Size()
		t.Logf("%s: estimate %s, wrote %s (%+.0f%%)", q.Label(), HumanBytes(est), HumanBytes(got), (float64(got)/float64(est)-1)*100)
		if r := float64(got) / float64(est); r < 0.75 || r > 1.25 {
			t.Errorf("%s wrote %d bytes against an estimate of %d", q.Label(), got, est)
		}
		if i > 0 && est >= last {
			t.Errorf("%s is estimated at %s, no smaller than the quality above it", q.Label(), HumanBytes(est))
		}
		last = est
	}
}
