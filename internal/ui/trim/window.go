// Package trim is Glint's Trim & Compress window: open a video, play it with its sound, set
// the in and out points on a strip of thumbnails, pick size, quality, codec and whether to keep
// the audio, and export with progress. It ports src/clip.
package trim

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Config says where exports go and lets tools watch the window.
type Config struct {
	// SaveDir is where Export writes; empty is the source's folder.
	SaveDir string
	// OnFrame, when set, runs on the main thread as each frame reaches the preview:
	// cmd/trimdemo checks seeking and measures audio and video sync with it.
	OnFrame func(Shown)
}

// Shown is a frame as it reaches the preview.
type Shown struct {
	FrameMS int64
	// ClockMS is the time the player's clock reads, the audio device's when Audio is set;
	// it is 0 unless Playing.
	ClockMS        float64
	Playing, Audio bool
}

// Window is an open Trim & Compress window.
type Window struct {
	win *mygo.Window
	ed  *editor
}

var fileManager = map[string]string{"windows": "Explorer", "darwin": "Finder"}[runtime.GOOS]

// Open shows the window for the video at path. It returns an error, worded for a toast, when
// path is not a video Glint trims. Call it on the main thread once the app is ready.
func Open(path string, cfg Config) (*Window, error) {
	if !IsVideo(path) {
		return nil, fmt.Errorf("%s is not a video Glint can trim (%s)", filepath.Base(path), strings.ToUpper(strings.Join(VideoExtensions, ", ")))
	}
	w := &Window{}
	h := &mygoHost{}
	w.ed = newEditor(h, path, cfg)
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title: Title(path), Width: 980, Height: 760, MinWidth: 760, MinHeight: 600,
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancyMica,
		Content: ui.View(w.ed.view),
	})
	h.win = w.win
	go w.ed.load()
	w.win.OnClosed(w.ed.close)
	w.win.Focus()
	return w, nil
}

// Native is the MyGo window.
func (w *Window) Native() *mygo.Window { return w.win }

func (w *Window) Close() { w.win.Close() }

type mygoHost struct{ win *mygo.Window }

func (h *mygoHost) update(fn func()) { h.win.Update(fn) }

func (h *mygoHost) saveAs(dir, name string) (string, error) {
	return mygo.Dialog.Save(mygo.SaveDialogOptions{
		Parent: h.win, Title: "Save the trimmed video", DefaultPath: filepath.Join(dir, name),
		Filters: []mygo.FileFilter{{Name: "MP4 video", Extensions: []string{"mp4"}}},
	})
}

func (h *mygoHost) reveal(path string) { mygo.Shell.ShowItemInFolder(path) }
