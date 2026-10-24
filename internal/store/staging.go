package store

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// fresh is how long a staging directory with nothing in it yet counts as just begun by a
// capture still running.
const fresh = time.Minute

const stagingExt = ".tmp"

// Orphans lists the hidden staging directories that no running capture or recording holds:
// what Glint left when it stopped before finishing one. Each comes back as the
// PendingRecording that would have finished it, created at the time its id names.
func (l Library) Orphans() []PendingRecording {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return nil
	}
	var out []PendingRecording
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || !strings.HasPrefix(name, ".") || !strings.HasSuffix(name, stagingExt) {
			continue
		}
		dir := filepath.Join(l.root, name)
		if held(dir, time.Now()) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(name, "."), stagingExt)
		created, err := time.ParseInLocation(stampLayout, id[:min(len(id), len(stampLayout))], time.Local)
		if err != nil {
			continue
		}
		out = append(out, PendingRecording{ID: id, Dir: dir, Created: created})
	}
	return out
}

// held reports whether a running capture may still be writing into the staging directory.
func held(dir string, now time.Time) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return true
	}
	if len(entries) == 0 {
		info, err := os.Stat(dir)
		return err != nil || now.Sub(info.ModTime()) < fresh
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			return true
		}
		if e.IsDir() {
			if held(filepath.Join(dir, e.Name()), now) {
				return true
			}
			continue
		}
		if inUse(filepath.Join(dir, e.Name()), info, now) {
			return true
		}
	}
	return false
}

// Discard moves a staging directory to the Recycle Bin or the Trash, where its files can
// still be rescued, and deletes it when the bin refuses it. Discarding one already gone
// succeeds.
func Discard(p PendingRecording) error {
	if _, err := os.Lstat(p.Dir); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if Trash([]string{p.Dir}) == nil {
		return nil
	}
	return os.RemoveAll(p.Dir)
}
