package app

import (
	"context"
	"errors"
	"image"
	"strings"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/actions"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/library"
	"github.com/wobsoriano/glint/internal/ui/pin"
	"github.com/wobsoriano/glint/internal/ui/prefs"
	"github.com/wobsoriano/glint/internal/ui/quicklook"
	"github.com/wobsoriano/glint/internal/ui/stack"
	"github.com/wobsoriano/glint/internal/ui/trim"
)

// views are the surfaces Glint opens itself. Main thread only.
type views struct {
	cards    *stack.Stack
	look     *quicklook.Viewer
	library  *library.Window
	settings *prefs.Window
}

var (
	_ library.Host   = (*App)(nil)
	_ prefs.Host     = (*App)(nil)
	_ stack.Host     = cardHost{}
	_ quicklook.Host = lookHost{}
)

// wire builds the surfaces this package owns. The editor, the recorder and Studio are other
// packages' and stay nil until they exist.
func (a *App) wire() {
	a.win.cards = stack.New(cardHost{a}, a.settings.Overlay)
	a.win.look = quicklook.New(lookHost{a})
	a.Surfaces = Surfaces{
		ShowCard:    a.win.cards.Push,
		Pin:         func(img *image.RGBA, dpi float64) { a.pin(img, dpi) },
		Imported:    a.imported,
		OpenLibrary: a.openLibrary,
		OpenPrefs:   a.openPrefs,
		OpenTrim:    a.openTrimWindow,
	}
}

func (a *App) pin(img image.Image, dpi float64) {
	p := pin.Open(img, dpi)
	p.Copied = func(err error) {
		a.post(func() {
			if err != nil {
				a.toast("Copy failed: " + err.Error())
				return
			}
			a.toast("Copied to clipboard")
		})
	}
}

// imported lists the Library window again, which also watches the folder, so a capture shows
// the moment the app writes it.
func (a *App) imported(store.Item) {
	if w := a.win.library; w != nil && w.IsOpen() {
		w.Library.Reload()
	}
}

func (a *App) openLibrary() {
	if w := a.win.library; w == nil || !w.IsOpen() {
		a.win.library = library.Open(a, a.library)
	}
	a.win.library.Show()
}

var paneNames = map[string]prefs.Pane{
	"general": prefs.General, "screenshots": prefs.Screenshots, "recordings": prefs.Recordings,
	"overlay": prefs.Overlay, "shortcuts": prefs.Shortcuts, "privacy": prefs.Privacy, "about": prefs.About,
}

// openPrefs shows Settings on the pane named, General for "" or a name it does not know.
func (a *App) openPrefs(name string) {
	pane := paneNames[strings.ToLower(name)]
	if w := a.win.settings; w != nil && w.IsOpen() {
		w.Show(pane)
		return
	}
	a.win.settings = prefs.Open(a, pane)
}

func (a *App) openTrimWindow(path string) {
	if _, err := trim.Open(path, trim.Config{SaveDir: a.settings.SaveDir}); err != nil {
		a.toast(err.Error())
	}
}

// The Library window's host methods. Edit, Capture, OpenSettings, OpenFiles, Settings and
// SettingsChanged are in app.go and flow.go.

func (a *App) QuickLook(item store.Item) { a.win.look.Open(item) }
func (a *App) CloseQuickLook() bool      { return a.win.look.Close() }

func (a *App) Copy(items []store.Item) {
	switch len(items) {
	case 0:
	case 1:
		a.Actions.Copy(items[0])
	default:
		a.Actions.CopyFiles(items)
	}
}

func (a *App) Export(items []store.Item, dir string) { a.Actions.ExportTo(items, dir) }

// Reveal shows the first of the items in Explorer or Finder: MyGo selects one item a call,
// and a window for each of many would bury the desktop.
func (a *App) Reveal(items []store.Item) {
	if len(items) > 0 {
		mygo.Shell.ShowItemInFolder(items[0].Dir)
	}
}

func (a *App) Trash(items []store.Item) error {
	dirs := make([]string, len(items))
	for i, it := range items {
		dirs[i] = it.Dir
	}
	return store.Trash(dirs)
}

// onMain runs start on the main thread and waits for it to call done: the blocking shape the
// card and Quick Look hosts have, over the actions' callbacks.
func (a *App) onMain(start func(done func(error))) error {
	ch := make(chan error, 1)
	a.post(func() { start(func(err error) { ch <- err }) })
	return <-ch
}

// errReported is an outcome a toast has told the user about already; a card says nothing
// more of it.
var errReported = context.Canceled

// cardHost does what a preview card's buttons ask. Each method runs on a goroutine of the
// stack's.
type cardHost struct{ a *App }

func (h cardHost) Copy(item store.Item) error {
	return h.a.onMain(func(done func(error)) { h.a.Actions.CopyItem(item, done) })
}

func (h cardHost) Save(item store.Item, choose bool) error {
	return h.a.onMain(func(done func(error)) {
		h.a.Actions.Save(item, choose, func(path string, err error) {
			h.a.Actions.ReportSave(path, err)
			if errors.Is(err, actions.ErrCancelled) {
				err = context.Canceled
			}
			done(err)
		})
	})
}

func (h cardHost) Compress(item store.Item) error {
	return h.a.onMain(func(done func(error)) { h.a.Actions.Compress(item, done) })
}

func (h cardHost) CopyText(item store.Item) error {
	return h.a.onMain(func(done func(error)) { h.a.Actions.CopyText(item, done) })
}

func (h cardHost) Edit(item store.Item) error {
	return h.a.onMain(func(done func(error)) { done(h.a.edit(item)) })
}

// Pin floats the screenshot at the scale of the display under the pointer, which is where
// the card is: a capture's own scale is not kept in the Library.
func (h cardHost) Pin(item store.Item) error {
	img, err := actions.LoadPixels(item)
	if err != nil {
		return err
	}
	return h.a.onMain(func(done func(error)) {
		scale := 1.0
		if ds, err := h.a.sys.displays(); err == nil && len(ds) > 0 {
			scale = displayAt(ds, h.a.sys.cursor()).Scale
		}
		h.a.pin(img, scale)
		done(nil)
	})
}

func (h cardHost) QuickLook(item store.Item) error {
	h.a.win.look.Open(item)
	return nil
}

func (h cardHost) Delete(item store.Item) error {
	return h.a.onMain(func(done func(error)) {
		h.a.Actions.Delete([]string{item.Dir}, func(ok bool) {
			if !ok {
				done(errReported)
				return
			}
			done(nil)
		})
	})
}

func (h cardHost) DragFile(item store.Item) (string, error) { return h.a.Actions.DragFile(item) }

// lookHost does what Quick Look's buttons ask, on a goroutine of the viewer's.
type lookHost struct{ a *App }

func (h lookHost) Copy(item store.Item) error {
	h.a.post(func() { h.a.Actions.Copy(item) })
	return nil
}

func (h lookHost) Edit(item store.Item) error {
	return h.a.onMain(func(done func(error)) { done(h.a.edit(item)) })
}

func (h lookHost) Reveal(item store.Item) error {
	mygo.Shell.ShowItemInFolder(item.Dir)
	return nil
}
