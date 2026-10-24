package app

import (
	"errors"
	"image"
	"os"
	"sync"

	"github.com/wobsoriano/glint/internal/actions"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/editor"
	"github.com/wobsoriano/glint/internal/ui/toast"
)

var _ editor.Host = (*editorHost)(nil)

// openEditor shows the screenshot in its editor: the one already open for it, or a new one
// with its saved edit. The map holds nil while a capture's files load, so a second ask in the
// meantime opens no second window.
func (a *App) openEditor(item store.Item) {
	if e, ok := a.win.editors[item.Dir]; ok {
		if e != nil {
			w := e.Window()
			if w.IsMinimized() {
				w.Restore()
			}
			w.Show()
			w.Focus()
		}
		return
	}
	a.win.editors[item.Dir] = nil
	doc := render.NewEditDoc(a.settings.Style, float32(a.cursorScale()), item.Meta.Source)
	a.background(func() func() {
		base, raw, err := store.LoadEdit(item)
		if err == nil && raw != nil {
			doc, err = render.ParseEditDoc(raw)
		}
		return func() {
			if err != nil {
				delete(a.win.editors, item.Dir)
				a.toast("Could not open the editor: " + toast.Reason(err))
				return
			}
			h := &editorHost{a: a, dir: item.Dir, item: item}
			h.ed = editor.Open(editor.Options{Title: item.Meta.Title, Base: render.Premultiply(base), Doc: doc, Host: h})
			a.win.editors[item.Dir] = h.ed
		}
	})
}

// cursorScale is the scale of the display with the pointer, as the Rust build gave a new edit
// and a pin from a card.
func (a *App) cursorScale() float64 {
	if ds, err := a.sys.displays(); err == nil && len(ds) > 0 {
		return displayAt(ds, a.sys.cursor()).Scale
	}
	return 1
}

// edited shows a saved edit wherever the capture shows: its preview card, Quick Look and the
// Library.
func (a *App) edited(item store.Item) {
	if a.win.cards != nil {
		a.win.cards.Refresh(item)
	}
	if a.win.look != nil {
		a.win.look.Refresh(item)
	}
	if a.Surfaces.Imported != nil {
		a.Surfaces.Imported(item)
	}
}

// editorHost is one editor's capture. Its methods run on the editor's goroutines.
type editorHost struct {
	a   *App
	dir string
	ed  *editor.Editor // main thread only

	mu   sync.Mutex
	item store.Item
}

var errGone = errors.New("this capture is no longer in the Library")

func (h *editorHost) SaveEdit(img *image.RGBA, doc render.EditDoc) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, err := os.Stat(h.item.Dir); err != nil {
		return errGone
	}
	saved, err := store.SaveEdit(h.item, img, doc.JSON())
	if err != nil {
		return err
	}
	h.item = saved
	h.a.post(func() { h.a.edited(saved) })
	return nil
}

func (h *editorHost) Export(title string, img *image.RGBA, ask bool) (string, error) {
	var path string
	err := h.a.onMain(func(done func(error)) {
		h.a.Actions.SaveImage(title, img, ask, func(p string, err error) {
			path = p
			done(err)
		})
	})
	if errors.Is(err, actions.ErrCancelled) {
		return "", editor.ErrCanceled
	}
	return path, err
}

func (h *editorHost) Copy(img *image.RGBA) error {
	return h.a.onMain(func(done func(error)) { h.a.Actions.CopyImage(img, done) })
}

func (h *editorHost) Pin(img *image.RGBA, dpi float32) error {
	h.a.post(func() { h.a.pin(img, float64(dpi)) })
	return nil
}

func (h *editorHost) SaveStyle(style render.Style) error {
	return h.a.onMain(func(done func(error)) {
		h.a.settings.Style = style
		done(h.a.settings.Save())
	})
}

func (h *editorHost) Closed() {
	h.a.post(func() {
		if e, ok := h.a.win.editors[h.dir]; ok && e == h.ed {
			delete(h.a.win.editors, h.dir)
		}
	})
}
