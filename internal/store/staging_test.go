package store

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// aged makes a staging directory and its files look written an hour ago, as one is when Glint
// starts again after a crash.
func aged(t *testing.T, dir string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	filepath.WalkDir(dir, func(path string, _ os.DirEntry, _ error) error {
		os.Chtimes(path, old, old)
		return nil
	})
}

func TestOrphansAreTheStagingDirectoriesNothingFinished(t *testing.T) {
	lib := New(t.TempDir())
	shot, err := lib.ImportScreenshot(NewScreenshot{Base: checker(8, 8), Created: at(8, 0, 0)})
	if err != nil {
		t.Fatal(err)
	}
	killed, err := lib.BeginRecording(at(9, 30, 5))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(killed.Dir, "screen.mp4"), []byte("mdat"), 0o644); err != nil {
		t.Fatal(err)
	}
	aged(t, killed.Dir)
	empty, err := lib.BeginRecording(at(9, 40, 0))
	if err != nil {
		t.Fatal(err)
	}
	aged(t, empty.Dir)
	begun, err := lib.BeginRecording(at(9, 50, 0))
	if err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(lib.Root(), ".not-staging"), 0o755)

	got := lib.Orphans()
	ids := make([]string, len(got))
	for i, p := range got {
		ids[i] = p.ID
	}
	if !slices.Equal(ids, []string{killed.ID, empty.ID}) {
		t.Fatalf("orphans %q, want the killed recording %s and the old empty one %s", ids, killed.ID, empty.ID)
	}
	if got[0] != killed {
		t.Errorf("orphan %+v, want %+v", got[0], killed)
	}
	if slices.ContainsFunc(got, func(p PendingRecording) bool { return p.Dir == begun.Dir || p.Dir == shot.Dir }) {
		t.Errorf("a recording begun just now or a finished capture is listed: %+v", got)
	}

	item, err := lib.FinishRecording(got[0], RecordingInfo{Width: 64, Height: 48, DurationMs: 2_000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if item.Meta.Source != nil || item.Meta.Title != "Recording 2026-10-14 at 09.30.05" {
		t.Errorf("recovered meta %+v", item.Meta)
	}
	if again := lib.Orphans(); len(again) != 1 || again[0].ID != empty.ID {
		t.Errorf("after recovering, orphans %+v", again)
	}
}

func TestDiscardingWhatIsGoneSucceeds(t *testing.T) {
	if err := Discard(PendingRecording{Dir: filepath.Join(t.TempDir(), ".gone.tmp")}); err != nil {
		t.Fatal(err)
	}
}

func TestDiscardMovesAnOrphanToTheBin(t *testing.T) {
	requireRealTrash(t)
	lib := New(t.TempDir())
	p, err := lib.BeginRecording(at(10, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(p.Dir, "screen.mp4"), []byte("mdat"), 0o644)
	if err := Discard(p); err != nil {
		t.Fatal(err)
	}
	if exists(p.Dir) {
		t.Fatalf("%s is still there", p.Dir)
	}
	if err := Discard(p); err != nil {
		t.Fatalf("discarding it again: %v", err)
	}
}
