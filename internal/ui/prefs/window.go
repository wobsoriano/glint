package prefs

import (
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Window is an open Settings window.
type Window struct {
	Prefs *Prefs
	win   *mygo.Window
}

// Open shows the Settings window on pane. Call it on the main thread once the app runs.
func Open(host Host, pane Pane) *Window {
	pick := func() (string, bool) {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Choose Folder", Directory: true})
		if err != nil || len(paths) == 0 {
			return "", false
		}
		return paths[0], true
	}
	open := func(path string) {
		_ = os.MkdirAll(path, 0o755)
		_ = mygo.Shell.OpenPath(path)
	}
	w := &Window{Prefs: New(host, pane, pick, open)}
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Glint Settings", Width: 860, Height: 640, MinWidth: 720, MinHeight: 520,
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancyMica,
		Content: ui.View(w.Prefs.View),
	})
	w.win.OnClosed(w.Prefs.Close)
	return w
}

// Show brings the window to the front on pane.
func (w *Window) Show(pane Pane) {
	w.win.Update(func() { w.Prefs.show(pane) })
	w.win.Show()
	w.win.Focus()
}

// IsOpen reports whether the window is still open.
func (w *Window) IsOpen() bool { return !w.win.IsDestroyed() }

// Native is the MyGo window, for the app's own placement and capture exclusion.
func (w *Window) Native() *mygo.Window { return w.win }
