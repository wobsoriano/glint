package store

import (
	"os"
	"path/filepath"
	"testing"
)

// A recorder keeps its files open until it stops, however long ago it last wrote, as a
// paused recording does.
func TestAStagingDirectoryWithAFileOpenIsNotAnOrphan(t *testing.T) {
	lib := New(t.TempDir())
	p, err := lib.BeginRecording(at(11, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(p.Dir, "mic.wav"))
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(p.Dir, "screen.mp4"), []byte("mdat"), 0o644)
	aged(t, p.Dir)
	if got := lib.Orphans(); len(got) != 0 {
		f.Close()
		t.Fatalf("a recording with mic.wav open is an orphan: %+v", got)
	}
	f.Close()
	if got := lib.Orphans(); len(got) != 1 || got[0].Dir != p.Dir {
		t.Fatalf("once mic.wav is closed, orphans %+v", got)
	}
}
