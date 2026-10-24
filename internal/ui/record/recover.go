package record

import (
	"log"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/store"
)

// Recover deals with what an earlier Glint left in the Library's staging directories, the
// orphans Library.Orphans lists: a recording whose screen.mp4 plays goes into the Library,
// and anything else to the Recycle Bin. A recording killed mid-way has no playable
// screen.mp4, since its MP4 index is written when it stops. Recover returns the recordings
// it saved; it can take a while, so call it off the main thread.
func Recover(lib store.Library, orphans []store.PendingRecording) []store.Item {
	var saved []store.Item
	for _, p := range orphans {
		item, err := recoverRecording(lib, p)
		if err == nil {
			log.Printf("recovered %s", item.Dir)
			saved = append(saved, item)
			continue
		}
		log.Printf("discarding %s: %v", p.Dir, err)
		if err := store.Discard(p); err != nil {
			log.Printf("discard %s: %v", p.Dir, err)
		}
	}
	return saved
}

func recoverRecording(lib store.Library, p store.PendingRecording) (store.Item, error) {
	r, err := media.OpenVideoReader(filepath.Join(p.Dir, "screen.mp4"))
	if err != nil {
		return store.Item{}, err
	}
	first, err := r.NextFrame()
	w, h := r.Size()
	info := store.RecordingInfo{Width: uint32(w), Height: uint32(h), DurationMs: uint64(max(r.DurationMS(), 0))}
	// Windows cannot rename the folder while the reader has a file in it open.
	r.Close()
	if err != nil {
		return store.Item{}, err
	}
	return lib.FinishRecording(p, info, first.Image)
}
