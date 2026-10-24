package library

import (
	"context"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/store"
)

// Window is an open Library window.
type Window struct {
	Library *Library
	win     *mygo.Window
	stop    context.CancelFunc
}

// Open shows the Library of lib in a new window that lists it again whenever something in
// it changes. Call it on the main thread once the app runs.
func Open(host Host, lib store.Library) *Window {
	w := &Window{}
	pick := func() (string, bool) {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Title: "Export Here", Directory: true})
		if err != nil || len(paths) == 0 {
			return "", false
		}
		return paths[0], true
	}
	// The first frame asks for thumbnails before NewWindow returns.
	ready := make(chan struct{})
	post := func(f func()) {
		<-ready
		w.win.Update(f)
	}
	w.Library = New(host, lib.Scan, post, pick)
	w.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Glint Library", Width: 1180, Height: 760, MinWidth: 760, MinHeight: 480,
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancyMica,
		Content: ui.View(w.Library.View),
	})
	close(ready)
	var ctx context.Context
	ctx, w.stop = context.WithCancel(context.Background())
	w.win.OnClosed(w.stop)
	w.Library.Reload()
	go w.watch(ctx, lib.Root())
	return w
}

// watch lists the Library again after each burst of changes under root: captures added by
// the app or copied in, titles renamed, edits saved, folders deleted.
func (w *Window) watch(ctx context.Context, root string) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return
	}
	fw, err := mygo.WatchFiles(ctx, root, mygo.FileWatchOptions{Recursive: true, HintsOnly: true})
	if err != nil {
		return
	}
	defer fw.Close()
	for {
		if _, err := fw.Next(ctx); err != nil {
			return
		}
		w.Library.Reload()
	}
}

// Show brings the window to the front.
func (w *Window) Show() {
	w.win.Show()
	w.win.Focus()
}

// IsOpen reports whether the window is still open.
func (w *Window) IsOpen() bool { return !w.win.IsDestroyed() }

// Native is the MyGo window, for the app's own placement and capture exclusion.
func (w *Window) Native() *mygo.Window { return w.win }
