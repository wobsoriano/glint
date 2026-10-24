package library

import (
	"os"
	"runtime"
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// Library is the Library window's state. Build it with New and show View in a window; Open
// does both. All methods run on the main thread.
type Library struct {
	host Host
	// post runs f on the main thread and draws a frame: the window's Update. Thumbnails and
	// rescans finish on goroutines and land through it.
	post func(f func())
	// pickFolder asks for the folder Export writes to.
	pickFolder func() (string, bool)
	scan       func() []store.Item

	items  []store.Item
	loaded bool

	filter  store.Filter
	search  string
	searchH ui.Handle

	grid   ui.GridState
	list   ui.ListState
	chosen ui.Selection[string]
	// cursor indexes the visible item last chosen, -1 for none; the grid and the list share it.
	cursor int
	// clicks is how many clicks the last press on the grid or the list made: the widgets
	// report Enter and a double click both as Submitted, and this tells them apart.
	clicks int

	renaming *rename
	trashing []store.Item
	asking   bool
	notice   string

	// thumbs holds each thumbnail asked for, nil while it decodes and when it cannot be read.
	thumbs map[thumbKey]*ui.Bitmap
	// shown is the visible items as this frame lists them, which the widgets key by.
	shown []store.Item
}

type rename struct{ dir, draft string }

type thumbKey struct {
	dir      string
	modified int64
}

// New makes a Library over the captures scan lists. post must run its function on the main
// thread; pickFolder may be nil, which leaves Export… out.
func New(host Host, scan func() []store.Item, post func(func()), pickFolder func() (string, bool)) *Library {
	l := &Library{host: host, scan: scan, post: post, pickFolder: pickFolder, cursor: -1, thumbs: map[thumbKey]*ui.Bitmap{}}
	l.grid.Key = l.key
	l.list.Key = l.key
	l.grid.Selected, l.list.Selected = &l.cursor, &l.cursor
	l.grid.Selection, l.list.Selection = &l.chosen, &l.chosen
	return l
}

// Reload lists the Library again on a goroutine and shows the result.
func (l *Library) Reload() {
	go func() {
		items := l.scan()
		l.post(func() { l.SetItems(items) })
	}()
}

// SetItems replaces the captures shown, keeping the selection and the cursor on the items
// that remain.
func (l *Library) SetItems(items []store.Item) {
	var at string
	if v := l.visible(); l.cursor >= 0 && l.cursor < len(v) {
		at = v[l.cursor].Dir
	}
	l.items, l.loaded = items, true
	for dir := range l.chosen.All() {
		if !slices.ContainsFunc(items, func(i store.Item) bool { return i.Dir == dir }) {
			l.chosen.Remove(dir)
		}
	}
	if l.renaming != nil && !l.chosen.Has(l.renaming.dir) {
		l.renaming = nil
	}
	l.cursor = slices.IndexFunc(l.visible(), func(i store.Item) bool { return i.Dir == at })
	live := map[thumbKey]bool{}
	for _, it := range items {
		live[keyOf(it)] = true
	}
	for k := range l.thumbs {
		if !live[k] {
			delete(l.thumbs, k)
		}
	}
}

func keyOf(it store.Item) thumbKey { return thumbKey{it.Dir, it.Meta.Modified.UnixMilli()} }

func (l *Library) key(i int) any {
	if v := l.shown; i < len(v) {
		return v[i].Dir
	}
	return ""
}

func (l *Library) prefs() *settings.LibraryPrefs { return &l.host.Settings().Library }

func (l *Library) visible() []store.Item {
	return store.Visible(l.items, l.filter, l.search, l.prefs().Sort)
}

// selected is the visible items chosen, in the order shown.
func (l *Library) selected() []store.Item {
	var out []store.Item
	for _, it := range l.visible() {
		if l.chosen.Has(it.Dir) {
			out = append(out, it)
		}
	}
	return out
}

func (l *Library) setFilter(f store.Filter) {
	if l.filter != f {
		l.filter, l.cursor = f, -1
	}
}

func (l *Library) updatePrefs(f func(p *settings.LibraryPrefs)) {
	f(l.prefs())
	l.host.SettingsChanged()
}

func (l *Library) selectAll() {
	for _, it := range l.visible() {
		l.chosen.Add(it.Dir)
	}
}

func (l *Library) clearSelection() {
	l.chosen.Clear()
	l.cursor = -1
	l.renaming = nil
}

func (l *Library) startRename() {
	if sel := l.selected(); len(sel) == 1 {
		l.renaming = &rename{dir: sel[0].Dir, draft: sel[0].Meta.Title}
	}
}

func (l *Library) commitRename() {
	r := l.renaming
	l.renaming = nil
	i := slices.IndexFunc(l.items, func(it store.Item) bool { return it.Dir == r.dir })
	if i < 0 || l.items[i].Meta.Title == r.draft {
		return
	}
	item, err := store.Rename(l.items[i], r.draft)
	if err != nil {
		l.notice = "Rename failed: " + err.Error()
		return
	}
	l.items[i] = item
}

func (l *Library) askTrash() {
	if sel := l.selected(); len(sel) > 0 {
		l.trashing, l.asking = sel, true
	}
}

func (l *Library) trash() {
	items := l.trashing
	l.trashing = nil
	dirs := make([]string, len(items))
	for i, it := range items {
		dirs[i] = it.Dir
	}
	err := l.host.Trash(items)
	l.items = store.DropTrashed(l.items, dirs)
	for _, d := range dirs {
		if !slices.ContainsFunc(l.items, func(it store.Item) bool { return it.Dir == d }) {
			l.chosen.Remove(d)
		}
	}
	l.cursor = -1
	if err != nil {
		l.notice = "Delete failed: " + err.Error()
	}
}

func (l *Library) export(items []store.Item) {
	if l.pickFolder == nil {
		return
	}
	if dir, ok := l.pickFolder(); ok {
		l.host.Export(items, dir)
	}
}

// thumb returns the item's thumbnail, decoding it on a goroutine the first time; nil until
// it is ready or when it cannot be read.
func (l *Library) thumb(it store.Item) *ui.Bitmap {
	k := keyOf(it)
	if bmp, ok := l.thumbs[k]; ok {
		return bmp
	}
	l.thumbs[k] = nil
	path := it.Thumb()
	go func() {
		var bmp *ui.Bitmap
		if data, err := os.ReadFile(path); err == nil {
			bmp, _ = ui.DecodeBitmap(data)
		}
		l.post(func() {
			if _, wanted := l.thumbs[k]; wanted {
				l.thumbs[k] = bmp
			}
		})
	}()
	return nil
}

var (
	trashName       = map[bool]string{true: "Trash", false: "Recycle Bin"}[runtime.GOOS == "darwin"]
	fileManagerName = map[bool]string{true: "Finder", false: "Explorer"}[runtime.GOOS == "darwin"]
)
