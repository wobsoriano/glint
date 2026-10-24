// Package studio is the Recording Studio window: a large preview that plays the edit with its
// sound, the timeline across the bottom (clips, trims, speed, zooms), the settings sidebar on
// the right (look, layout, cursor, keystrokes, camera, audio, zooms, captions, presets), the
// transcript tools, and export. It ports src/studio; the project model and the compositor
// are internal/studio.
package studio

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/settings"
)

// Host is what Studio needs from the app. Every method runs on the main thread.
type Host interface {
	// Settings is the app's settings; Studio reads where exports are saved.
	Settings() *settings.Settings
	// Toast shows a message outside the window, for a recording that cannot be opened.
	Toast(text string)
	// Reveal shows a file in Explorer or Finder.
	Reveal(path string)
	// CopyFile puts a file on the clipboard, as a copy of the file.
	CopyFile(path string) error
	// RecordingExported reports a video Studio saved, for the app's own bookkeeping.
	RecordingExported(path string)
}

// Window is an open Studio window.
type Window struct {
	win *mygo.Window
	ed  *editor
	dir string
}

var open = map[string]*Window{}

// Open shows Studio for the recording in sessionDir, or brings its window to the front when
// one is open already. When the recording cannot be opened, it tells the user with a toast
// and returns nil. Call it on the main thread once the app runs.
func Open(host Host, sessionDir string) *Window {
	return OpenWith(host, sessionDir, Options{})
}

// Options are for tools that watch the window; the app leaves them empty.
type Options struct {
	// OnFrame runs on the main thread as each frame reaches the preview: cmd/studiodemo
	// measures playback against the audio clock with it.
	OnFrame func(Shown)
}

// Shown is a preview frame as it reaches the screen.
type Shown struct {
	OutputMs float64
	// ClockMs is what the playback clock reads as the frame is shown, the audio device's when
	// Audio is set; it is 0 unless Playing.
	ClockMs        float64
	Playing, Audio bool
}

// OpenWith is Open with options for tools.
func OpenWith(host Host, sessionDir string, o Options) *Window {
	key := filepath.Clean(sessionDir)
	if w := open[key]; w != nil && !w.win.IsDestroyed() {
		w.win.Show()
		w.win.Focus()
		return w
	}
	w := &Window{dir: key}
	sh := &mygoShell{}
	ed, err := newEditor(host, sh, sessionDir, o)
	if err != nil {
		host.Toast(fmt.Sprintf("Couldn't open the recording: %v", err))
		return nil
	}
	w.ed = ed
	ready := make(chan struct{})
	sh.post = func(fn func()) {
		<-ready
		w.win.Update(fn)
	}
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title: ed.title + " - Glint Studio", Width: 1440, Height: 900, MinWidth: 1040, MinHeight: 680,
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancyMica,
		Content: ui.View(ed.view),
	})
	sh.win = w.win
	close(ready)
	w.win.OnClose(func(e *mygo.CloseEvent) {
		if !ed.closeRequested() {
			e.PreventDefault()
		}
	})
	w.win.OnClosed(func() {
		ed.closed()
		if open[key] == w {
			delete(open, key)
		}
	})
	ed.start()
	w.win.Focus()
	open[key] = w
	return w
}

// Native is the MyGo window, for the app's own placement and capture exclusion.
func (w *Window) Native() *mygo.Window { return w.win }

// IsOpen reports whether the window is still open.
func (w *Window) IsOpen() bool { return !w.win.IsDestroyed() }

func (w *Window) Close() { w.win.Close() }

// shell is what the editor needs from its window, so tests can stand in for one.
type shell interface {
	// update runs fn on the UI thread and redraws. Any goroutine may call it.
	update(fn func())
	// pickImage asks for a background image, from a goroutine; "" means cancelled.
	pickImage() string
	// destroy closes the window without asking.
	destroy()
}

type mygoShell struct {
	win  *mygo.Window
	post func(func())
}

func (s *mygoShell) update(fn func()) { s.post(fn) }

func (s *mygoShell) pickImage() string {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Title: "Choose background", Parent: s.win,
		Filters: []mygo.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "webp", "bmp"}}},
	})
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0]
}

func (s *mygoShell) destroy() { s.win.Destroy() }

// fileStem is a file's name without its extension.
func fileStem(path string) string {
	base := filepath.Base(path)
	return strings.TrimSuffix(base, filepath.Ext(base))
}
