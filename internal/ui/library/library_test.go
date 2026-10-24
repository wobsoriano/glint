package library

import (
	"image"
	"image/color"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

type fakeHost struct {
	s         settings.Settings
	saves     int
	copied    [][]string
	quick     []string
	quickOpen bool
	edited    []string
	trashed   []string
	exported  string
}

func (h *fakeHost) Settings() *settings.Settings { return &h.s }
func (h *fakeHost) SettingsChanged()             { h.saves++ }
func (h *fakeHost) QuickLook(it store.Item) {
	h.quick = append(h.quick, it.Meta.Title)
	h.quickOpen = true
}
func (h *fakeHost) CloseQuickLook() bool {
	was := h.quickOpen
	h.quickOpen = false
	return was
}
func (h *fakeHost) Edit(it store.Item) { h.edited = append(h.edited, it.Meta.Title) }
func (h *fakeHost) Copy(items []store.Item) {
	h.copied = append(h.copied, titles(items))
}
func (h *fakeHost) Export(items []store.Item, dir string) { h.exported = dir }
func (h *fakeHost) Reveal(items []store.Item)             {}
func (h *fakeHost) Trash(items []store.Item) error {
	for _, it := range items {
		h.trashed = append(h.trashed, it.Meta.Title)
		os.RemoveAll(it.Dir)
	}
	return nil
}
func (h *fakeHost) Capture(settings.HotkeyAction) {}
func (h *fakeHost) OpenSettings()                 {}
func (h *fakeHost) OpenFiles([]string)            {}

func titles(items []store.Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Meta.Title
	}
	return out
}

// fixture is a Library of three screenshots, Alpha the oldest and Charlie the newest, shown
// in a tester.
func fixture(t *testing.T) (*Library, *fakeHost, *ui.Tester, store.Library) {
	t.Helper()
	lib := store.New(t.TempDir())
	base := time.Date(2026, 10, 17, 9, 0, 0, 0, time.Local)
	for i, title := range []string{"Alpha", "Bravo", "Charlie"} {
		img := image.NewNRGBA(image.Rect(0, 0, 40+i, 30))
		for p := range img.Pix {
			img.Pix[p] = 200
		}
		img.Set(0, 0, color.Black)
		if _, err := lib.ImportScreenshot(store.NewScreenshot{Base: img, Created: base.Add(time.Duration(i) * time.Minute), Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	h := &fakeHost{s: settings.Default()}
	l := New(h, lib.Scan, func(func()) {}, func() (string, bool) { return "C:/out", true })
	l.SetItems(lib.Scan())
	tt := ui.NewTester(l.View, 1180, 760)
	return l, h, tt, lib
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func chosen(l *Library) []string { return titles(l.selected()) }

func TestSelectionFollowsClicksAndKeys(t *testing.T) {
	l, h, tt, _ := fixture(t)
	must(t, tt.Click("Charlie"))
	must(t, tt.ClickWith(ui.Ctrl, "Alpha"))
	if got := chosen(l); !slices.Equal(got, []string{"Charlie", "Alpha"}) {
		t.Fatalf("Ctrl-click adds: %q", got)
	}
	if !tt.HasText("2 of 3 selected") {
		t.Errorf("status bar: %q", tt.Texts())
	}
	must(t, tt.ClickWith(ui.Ctrl, "Charlie"))
	if got := chosen(l); !slices.Equal(got, []string{"Alpha"}) {
		t.Fatalf("Ctrl-click takes out: %q", got)
	}
	must(t, tt.Click("Charlie"))
	must(t, tt.ClickWith(ui.Shift, "Alpha"))
	if got := chosen(l); len(got) != 3 {
		t.Fatalf("Shift-click chooses the range: %q", got)
	}
	tt.Key(0, ui.KeyEscape)
	if got := chosen(l); len(got) != 0 {
		t.Fatalf("Escape clears: %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyA)
	if got := chosen(l); len(got) != 3 {
		t.Fatalf("Ctrl+A chooses all: %q", got)
	}
	tt.Key(ui.Cmd, ui.KeyC)
	if len(h.copied) != 1 || len(h.copied[0]) != 3 {
		t.Fatalf("Ctrl+C copies the selection: %q", h.copied)
	}
}

func TestDoubleClickAndSpaceQuickLook(t *testing.T) {
	_, h, tt, _ := fixture(t)
	must(t, tt.Click("Bravo"))
	must(t, tt.Click("Bravo"))
	if !slices.Equal(h.quick, []string{"Bravo"}) {
		t.Fatalf("double click opens Quick Look: %q", h.quick)
	}
	tt.Key(0, ui.KeySpace)
	if h.quickOpen || len(h.quick) != 1 {
		t.Fatalf("Space closes an open Quick Look: open %v %q", h.quickOpen, h.quick)
	}
	tt.Key(0, ui.KeySpace)
	if !h.quickOpen || len(h.quick) != 2 {
		t.Fatalf("Space opens it again: %q", h.quick)
	}
}

func TestEnterRenamesInMeta(t *testing.T) {
	l, _, tt, _ := fixture(t)
	must(t, tt.Click("Bravo"))
	tt.Advance(time.Second)
	tt.Key(0, ui.KeyEnter)
	if l.renaming == nil {
		t.Fatal("Enter starts renaming the chosen capture")
	}
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Renamed")
	tt.Key(0, ui.KeyEnter)
	if l.renaming != nil {
		t.Fatal("Enter commits the new name")
	}
	dir := l.selected()[0].Dir
	it, err := store.Load(dir)
	must(t, err)
	if it.Meta.Title != "Renamed" {
		t.Fatalf("meta.json title: %q", it.Meta.Title)
	}
	if !tt.HasText("Renamed") {
		t.Errorf("the grid shows the new name: %q", tt.Texts())
	}
}

func TestPrefsPersistThroughHost(t *testing.T) {
	l, h, tt, _ := fixture(t)
	must(t, tt.Click("Name"))
	if h.s.Library.Sort != settings.SortName || h.saves == 0 {
		t.Fatalf("sort by name saved: %+v after %d saves", h.s.Library, h.saves)
	}
	if got := titles(l.visible()); !slices.Equal(got, []string{"Alpha", "Bravo", "Charlie"}) {
		t.Fatalf("A to Z: %q", got)
	}
	must(t, tt.Click("List"))
	if h.s.Library.View != settings.ViewList {
		t.Fatalf("list view saved: %+v", h.s.Library)
	}
	if !tt.HasText("Dimensions") {
		t.Errorf("the list shows its columns: %q", tt.Texts())
	}
	must(t, tt.Click("Inspector"))
	if h.s.Library.Inspector {
		t.Fatalf("inspector hidden and saved: %+v", h.s.Library)
	}
	tt.Key(ui.Cmd|ui.Alt, ui.KeyI)
	if !h.s.Library.Inspector {
		t.Fatalf("Ctrl+Alt+I shows it again: %+v", h.s.Library)
	}
	tt.Key(ui.Cmd, ui.KeyF)
	tt.Type("char")
	if got := titles(l.visible()); !slices.Equal(got, []string{"Charlie"}) {
		t.Fatalf("search by title: %q", got)
	}
	tt.Key(0, ui.KeyEscape)
	if l.search != "" {
		t.Fatalf("Escape clears the search: %q", l.search)
	}
}

func TestFiltersAndInspector(t *testing.T) {
	l, _, tt, _ := fixture(t)
	must(t, tt.Click("Recordings"))
	if len(l.visible()) != 0 {
		t.Fatalf("no recordings: %q", titles(l.visible()))
	}
	must(t, tt.Click("Screenshots"))
	must(t, tt.Click("Alpha"))
	for _, want := range []string{"40 \u00d7 30", "Original, no edits", "Oct 17, 2026 at 9:00 AM", "Location"} {
		if !tt.HasText(want) {
			t.Errorf("inspector shows %q: %q", want, tt.Texts())
		}
	}
}

func TestTrashAfterConfirm(t *testing.T) {
	l, h, tt, lib := fixture(t)
	must(t, tt.Click("Bravo"))
	tt.Key(0, ui.KeyDelete)
	if !tt.HasText("Move \u201cBravo\u201d to the Recycle Bin?") && !tt.HasText("Move \u201cBravo\u201d to the Trash?") {
		t.Fatalf("asks first: %q", tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	if !slices.Equal(h.trashed, []string{"Bravo"}) {
		t.Fatalf("trashed: %q", h.trashed)
	}
	if got := titles(l.visible()); slices.Contains(got, "Bravo") {
		t.Fatalf("Bravo left the window: %q", got)
	}
	if got := titles(lib.Scan()); slices.Contains(got, "Bravo") {
		t.Fatalf("Bravo left the Library: %q", got)
	}
}

func TestNewCapturesAppear(t *testing.T) {
	l, _, tt, lib := fixture(t)
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	_, err := lib.ImportScreenshot(store.NewScreenshot{Base: img, Created: time.Date(2026, 10, 18, 9, 0, 0, 0, time.Local), Title: "Delta"})
	must(t, err)
	l.SetItems(lib.Scan())
	tt.Frame()
	if !tt.HasText("Delta") || !tt.HasText("4 capture(s)") {
		t.Fatalf("new capture listed: %q", tt.Texts())
	}
}
