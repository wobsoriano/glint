package app

import (
	"fmt"
	"image"
	"image/gif"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	studiowin "github.com/wobsoriano/glint/internal/ui/studio"
)

var _ studiowin.Host = studioHost{}

// openStudio shows Studio for a recording's session; Studio keeps one window a session.
func (a *App) openStudio(dir string) { studiowin.Open(studioHost{a, dir}, dir) }

// studioHost is one Studio window's recording. Its methods run on the main thread.
type studioHost struct {
	a   *App
	dir string
}

func (h studioHost) Settings() *settings.Settings  { return h.a.Settings() }
func (h studioHost) Toast(text string)             { h.a.toast(text) }
func (h studioHost) Reveal(path string)            { mygo.Shell.ShowItemInFolder(path) }
func (h studioHost) CopyFile(path string) error    { return mygo.Clipboard.WriteFiles(path) }
func (h studioHost) RecordingExported(path string) { h.a.recordingExported(h.dir, path) }

// recordingExported shows the recording as Studio exported it, wherever it shows: its
// thumbnail becomes the export's first frame. The export is the render Copy and Save hand
// out from then on, as studioDeliverable finds it in the session's render cache.
func (a *App) recordingExported(dir, video string) {
	a.background(func() func() {
		item, err := retakeThumb(dir, video)
		return func() {
			if err != nil {
				log.Printf("app: thumbnail of %s: %v", dir, err)
				return
			}
			a.edited(item)
		}
	})
}

// retakeThumb writes thumb.png from the video's first frame and marks the recording modified,
// which is what the Library keys its thumbnails by.
func retakeThumb(dir, video string) (store.Item, error) {
	item, err := store.Load(dir)
	if err != nil {
		return store.Item{}, err
	}
	frame, err := firstFrame(video)
	if err != nil {
		return store.Item{}, err
	}
	tmp := item.Thumb() + ".tmp"
	if err := encodePNG(tmp, store.Thumbnail(frame)); err != nil {
		os.Remove(tmp)
		return store.Item{}, err
	}
	if err := os.Rename(tmp, item.Thumb()); err != nil {
		os.Remove(tmp)
		return store.Item{}, err
	}
	// Rename to the same title is the store's one write of meta.json that stamps Modified.
	return store.Rename(item, item.Meta.Title)
}

func firstFrame(video string) (image.Image, error) {
	if strings.EqualFold(filepath.Ext(video), ".gif") {
		f, err := os.Open(video)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return gif.Decode(f)
	}
	r, err := media.OpenVideoReader(video)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	f, err := r.NextFrame()
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", video, err)
	}
	return f.Image, nil
}

func encodePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
