package store

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/settings/serde"
)

// testdata/rust was written by the Rust build's ignored go_fixtures test in src/library.rs, and
// testdata/real holds meta.json files from a real Library with the window title replaced. Both
// came from a machine in America/Los_Angeles, so the tests run in that zone.
func TestMain(m *testing.M) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		panic(err)
	}
	time.Local = loc
	os.Exit(m.Run())
}

func checker(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.NRGBA{0, 0, 255, 255}
			if (x/8+y/8)%2 == 0 {
				c = color.NRGBA{255, 0, 0, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func at(h, m, s int) time.Time { return time.Date(2026, 10, 14, h, m, s, 0, time.Local) }

func samePixels(t *testing.T, what string, got image.Image, want *image.NRGBA) {
	t.Helper()
	if got.Bounds() != want.Bounds() {
		t.Fatalf("%s is %v, want %v", what, got.Bounds(), want.Bounds())
	}
	for y := range want.Rect.Dy() {
		for x := range want.Rect.Dx() {
			if g := color.NRGBAModel.Convert(got.At(x, y)); g != want.NRGBAAt(x, y) {
				t.Fatalf("%s pixel %d,%d is %v, want %v", what, x, y, g, want.NRGBAAt(x, y))
			}
		}
	}
}

func decodeFile(t *testing.T, path string) image.Image {
	t.Helper()
	img, err := decodeImage(path)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func size(img image.Image) [2]int { return [2]int{img.Bounds().Dx(), img.Bounds().Dy()} }

func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestImportWritesTheCapturePackage(t *testing.T) {
	root := t.TempDir()
	lib := New(root)
	base := checker(1200, 600)
	redacted := checker(1200, 600)
	redacted.SetNRGBA(0, 0, color.NRGBA{0, 0, 0, 255})
	doc := []byte(`{"version":1}`)
	item, err := lib.ImportScreenshot(NewScreenshot{Base: base, Edit: &Edit{Image: redacted, Doc: doc}, Created: at(14, 3, 22), Source: "Display 1"})
	if err != nil {
		t.Fatal(err)
	}
	if id := item.ID(); !strings.HasPrefix(id, "2026-10-14_14-03-22_") || len(id) != len("2026-10-14_14-03-22_xxxx") ||
		strings.Trim(id[len(id)-4:], "0123456789abcdef") != "" {
		t.Errorf("id %s", id)
	}
	if item.Meta.Title != "Screenshot 2026-10-14 at 14.03.22" || item.Meta.Width != 1200 || item.Meta.Height != 600 ||
		item.Meta.Source == nil || *item.Meta.Source != "Display 1" || item.Meta.ImportedFrom != nil || item.Meta.DurationMs != nil {
		t.Errorf("meta %+v", item.Meta)
	}
	samePixels(t, "base.png", decodeFile(t, filepath.Join(item.Dir, "base.png")), base)
	samePixels(t, "image.png", decodeFile(t, item.Deliverable()), redacted)
	if s := size(decodeFile(t, item.Thumb())); s != [2]int{512, 256} {
		t.Errorf("thumb is %v", s)
	}
	if !item.IsEdited() {
		t.Error("automatic redactions stay editable")
	}
	if got, _ := os.ReadFile(filepath.Join(item.Dir, "edit.json")); !bytes.Equal(got, doc) {
		t.Errorf("edit.json holds %s", got)
	}
	if n := names(t, root); !slices.Equal(n, []string{item.ID()}) {
		t.Errorf("root holds %v; no staging directory may be left behind", n)
	}
	if n := names(t, item.Dir); !slices.Equal(n, []string{"base.png", "edit.json", "image.png", "meta.json", "thumb.png"}) {
		t.Errorf("capture holds %v", n)
	}
}

func TestImportFileCopiesAndNeverTouchesTheOriginal(t *testing.T) {
	root, srcDir := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "Diagram.jpg")
	f, _ := os.Create(src)
	if err := jpeg.Encode(f, checker(64, 32), nil); err != nil {
		t.Fatal(err)
	}
	f.Close()
	before, _ := os.ReadFile(src)

	item, err := New(root).ImportFile(src)
	if err != nil {
		t.Fatal(err)
	}
	if item.Meta.Title != "Diagram" || item.Meta.ImportedFrom == nil || *item.Meta.ImportedFrom != src || item.Meta.Width != 64 || item.Meta.Height != 32 {
		t.Errorf("meta %+v", item.Meta)
	}
	if after, _ := os.ReadFile(src); !bytes.Equal(after, before) {
		t.Error("the original changed")
	}

	bogus := filepath.Join(srcDir, "notes.png")
	os.WriteFile(bogus, []byte("not an image"), 0o644)
	if _, err := New(root).ImportFile(bogus); err == nil || !strings.Contains(err.Error(), "is not a readable image") {
		t.Errorf("bogus file gave %v", err)
	}
}

func TestScanListsNewestFirstWithRecordingsAndSkipsJunk(t *testing.T) {
	root := t.TempDir()
	lib := New(root)
	img := checker(40, 40)
	older, _ := lib.ImportScreenshot(NewScreenshot{Base: img, Created: at(9, 0, 0)})
	newer, _ := lib.ImportScreenshot(NewScreenshot{Base: img, Created: at(18, 30, 0)})

	recDir := filepath.Join(root, "2026-10-14_12-00-00_beef.glintrec")
	os.MkdirAll(recDir, 0o755)
	dur, src := uint64(42_000), "Display 1"
	recMeta, _ := json.Marshal(Meta{Version: 1, Kind: Recording, Title: "Demo take", Created: at(12, 0, 0), Modified: at(12, 5, 0),
		Width: 2560, Height: 1440, DurationMs: &dur, Source: &src})
	os.WriteFile(filepath.Join(recDir, "meta.json"), recMeta, 0o644)
	os.WriteFile(filepath.Join(recDir, "screen.mp4"), make([]byte, 1000), 0o644)
	os.MkdirAll(filepath.Join(root, ".2026-10-14_13-00-00_dead.tmp"), 0o755)
	os.MkdirAll(filepath.Join(root, "no-meta"), 0o755)

	items := lib.Scan()
	var titles []string
	for _, i := range items {
		titles = append(titles, i.Meta.Title)
	}
	if !slices.Equal(titles, []string{newer.Meta.Title, "Demo take", older.Meta.Title}) {
		t.Fatalf("titles %q", titles)
	}
	rec := items[1]
	if rec.Kind() != Recording || *rec.Meta.DurationMs != 42_000 || rec.Bytes != 1000+uint64(len(recMeta)) || rec.Deliverable() != filepath.Join(recDir, "screen.mp4") {
		t.Errorf("recording %+v", rec)
	}
}

func TestWrongKindForTheDirectoryIsRejected(t *testing.T) {
	root := t.TempDir()
	item, _ := New(root).ImportScreenshot(NewScreenshot{Base: checker(4, 4), Created: at(8, 0, 0)})
	moved := item.Dir + recordingExt
	os.Rename(item.Dir, moved)
	if _, err := Load(moved); err == nil || !strings.Contains(err.Error(), "wrong directory type") {
		t.Errorf("got %v", err)
	}
	if len(New(root).Scan()) != 0 {
		t.Error("a mismatched capture was listed")
	}
}

func TestRenameChangesTitleInMetaOnly(t *testing.T) {
	root := t.TempDir()
	lib := New(root)
	item, _ := lib.ImportScreenshot(NewScreenshot{Base: checker(16, 16), Created: time.Date(2001, 2, 3, 10, 0, 0, 0, time.Local)})

	renamed, err := Rename(item, "  Login bug  ")
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Meta.Title != "Login bug" || renamed.Dir != item.Dir || !renamed.Meta.Modified.After(item.Meta.Modified) || !renamed.Meta.Created.Equal(item.Meta.Created) {
		t.Errorf("renamed %+v", renamed.Meta)
	}
	if got := lib.Scan()[0].Meta.Title; got != "Login bug" {
		t.Errorf("scan sees %q", got)
	}
	if _, err := Rename(item, "   "); !errors.Is(err, ErrEmptyTitle) {
		t.Errorf("empty title gave %v", err)
	}
	if got := lib.Scan()[0].Meta.Title; got != "Login bug" {
		t.Errorf("a rejected rename changed the title to %q", got)
	}
}

func TestFinishedRecordingIsRenamedIntoPlaceWithMetaAndThumb(t *testing.T) {
	root := t.TempDir()
	lib := New(root)
	pending, err := lib.BeginRecording(at(15, 4, 5))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(pending.Dir), ".") {
		t.Error("staging is not hidden")
	}
	os.WriteFile(filepath.Join(pending.Dir, "screen.mp4"), make([]byte, 300), 0o644)
	if len(lib.Scan()) != 0 {
		t.Error("an unfinished recording is in the Library")
	}

	item, err := lib.FinishRecording(pending, RecordingInfo{Width: 1280, Height: 720, DurationMs: 4_250, Source: "Display 1"}, checker(1280, 720))
	if err != nil {
		t.Fatal(err)
	}
	if item.Dir != filepath.Join(root, pending.ID+".glintrec") || exists(pending.Dir) || item.Kind() != Recording {
		t.Errorf("item %+v", item)
	}
	if item.Meta.Title != "Recording 2026-10-14 at 15.04.05" || item.Meta.Width != 1280 || item.Meta.Height != 720 || *item.Meta.DurationMs != 4_250 || *item.Meta.Source != "Display 1" {
		t.Errorf("meta %+v", item.Meta)
	}
	if s := size(decodeFile(t, item.Thumb())); s != [2]int{512, 288} {
		t.Errorf("thumb is %v", s)
	}
	if got := lib.Scan(); !reflect.DeepEqual(got, []Item{item}) {
		t.Errorf("scan %+v", got)
	}
}

func TestSaveEditReplacesTheDeliverableAndReopensTheSameDoc(t *testing.T) {
	lib := New(t.TempDir())
	base := checker(800, 400)
	item, _ := lib.ImportScreenshot(NewScreenshot{Base: base, Created: at(8, 0, 0)})
	gotBase, doc, err := LoadEdit(item)
	if err != nil || doc != nil {
		t.Fatalf("unedited capture gave doc %s, %v", doc, err)
	}
	samePixels(t, "base", gotBase, base)

	newDoc := []byte(`{"window_title":"Login bug"}`)
	framed := checker(1000, 600)
	saved, err := SaveEdit(item, framed, newDoc)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.IsEdited() || !saved.Meta.Modified.After(item.Meta.Modified) || !saved.Meta.Created.Equal(item.Meta.Created) {
		t.Errorf("saved %+v", saved.Meta)
	}
	samePixels(t, "image.png", decodeFile(t, saved.Deliverable()), framed)
	if s := size(decodeFile(t, saved.Thumb())); s != [2]int{512, 307} {
		t.Errorf("thumb is %v", s)
	}
	gotBase, doc, _ = LoadEdit(saved)
	samePixels(t, "base after edit", gotBase, base)
	if !bytes.Equal(doc, newDoc) {
		t.Errorf("doc %s", doc)
	}
	if n := names(t, saved.Dir); !slices.Equal(n, []string{"base.png", "edit.json", "image.png", "meta.json", "thumb.png"}) {
		t.Errorf("capture holds %v", n)
	}
}

func TestItemDirFindsTheCaptureAPathBelongsTo(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Library")
	lib := New(root)
	dir := filepath.Join(root, "2026-10-14_14-03-22_ab12")
	for _, c := range []struct {
		path string
		want string
		ok   bool
	}{
		{dir, dir, true},
		{filepath.Join(dir, "image.png"), dir, true},
		{filepath.Join(dir, "cursors", "0.png"), dir, true},
		{root, "", false},
		{filepath.Join(filepath.Dir(root), "shot.png"), "", false},
		{filepath.Join(filepath.Dir(root), "LibraryX", "a"), "", false},
	} {
		if got, ok := lib.ItemDir(c.path); got != c.want || ok != c.ok {
			t.Errorf("ItemDir(%s) = %s, %v", c.path, got, ok)
		}
	}
}

func TestThumbnailSizes(t *testing.T) {
	for _, c := range []struct{ w, h, tw, th int }{
		{1200, 600, 512, 256}, {600, 1200, 256, 512}, {100, 50, 100, 50}, {4000, 3, 512, 1}, {513, 513, 512, 512},
	} {
		if s := size(Thumbnail(checker(c.w, c.h))); s != [2]int{c.tw, c.th} {
			t.Errorf("%dx%d thumbnail is %v", c.w, c.h, s)
		}
	}
	solid := image.NewNRGBA(image.Rect(0, 0, 1024, 1024))
	for i := range solid.Pix {
		solid.Pix[i] = []uint8{10, 200, 30, 255}[i%4]
	}
	if c := Thumbnail(solid).NRGBAAt(100, 100); c != (color.NRGBA{10, 200, 30, 255}) {
		t.Errorf("box filter shifted a flat color to %v", c)
	}
}

func TestRustMetaFilesRoundTripByteForByte(t *testing.T) {
	paths, _ := filepath.Glob("testdata/rust/meta-*.json")
	real, _ := filepath.Glob("testdata/real/meta-*.json")
	paths = append(paths, real...)
	checked := 0
	for _, p := range paths {
		base := filepath.Base(p)
		if strings.Contains(base, "reject") || base == "meta-minimal.json" {
			continue
		}
		want, _ := os.ReadFile(p)
		var m Meta
		if err := json.Unmarshal(want, &m); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		got, _ := serde.MarshalPretty(m)
		if !bytes.Equal(got, want) {
			t.Errorf("%s re-encoded\n got: %s\nwant: %s", p, got, want)
		}
		checked++
	}
	if checked < 7 {
		t.Errorf("checked %d meta fixtures", checked)
	}
	var m Meta
	if err := json.Unmarshal(mustRead(t, "testdata/rust/meta-reject-missing-kind.json"), &m); err == nil {
		t.Error("a meta.json without a kind decoded")
	}
	if err := json.Unmarshal(mustRead(t, "testdata/rust/meta-minimal.json"), &m); err != nil {
		t.Fatal(err)
	}
	got, _ := serde.MarshalPretty(m)
	if want := mustRead(t, "testdata/rust/meta-minimal-decoded.json"); !bytes.Equal(got, want) {
		t.Errorf("minimal meta re-encoded\n got: %s\nwant: %s", got, want)
	}
}

func mustRead(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRustWrittenLibraryLists(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Library")
	copyTree(t, "testdata/rust/Library", root)
	items := New(root).Scan()
	var got []string
	for _, i := range items {
		got = append(got, string(i.Kind())+" "+i.Meta.Title)
	}
	want := []string{"recording Recording 2026-10-14 at 11.00.00", "screenshot Diagram", "screenshot Screenshot 2026-10-14 at 09.00.00"}
	if !slices.Equal(got, want) {
		t.Fatalf("listed %q", got)
	}
	if from := items[1].Meta.ImportedFrom; from == nil || *from != `C:\Users\me\Pictures\Diagram.jpg` {
		t.Errorf("imported_from %v", from)
	}
	base, doc, err := LoadEdit(items[2])
	if err != nil || doc == nil || size(base) != [2]int{40, 20} || !items[2].IsEdited() || items[1].IsEdited() {
		t.Errorf("edited screenshot: %v, %d bytes of doc, %v", size(base), len(doc), err)
	}
	samePixels(t, "Rust base.png", base, checker(40, 20))
	if s := size(decodeFile(t, items[0].Thumb())); s != [2]int{64, 36} {
		t.Errorf("recording thumb is %v", s)
	}
}

func TestVisibleFiltersSearchesAndSorts(t *testing.T) {
	mk := func(dir string, kind Kind, title string, created, modified time.Time) Item {
		return Item{Dir: dir, Meta: Meta{Kind: kind, Title: title, Created: created, Modified: modified}}
	}
	items := []Item{
		mk("c", Screenshot, "banana", at(12, 0, 0), at(12, 0, 0)),
		mk("b", Recording, "Apple pie", at(11, 0, 0), at(19, 0, 0)),
		mk("a", Screenshot, "cherry APPLE", at(10, 0, 0), at(13, 0, 0)),
	}
	dirs := func(xs []Item) string {
		var s string
		for _, x := range xs {
			s += x.Dir
		}
		return s
	}
	for _, c := range []struct {
		f      Filter
		search string
		sort   settings.SortKey
		want   string
	}{
		{FilterAll, "", settings.SortCaptured, "cba"},
		{FilterAll, "", settings.SortModified, "bac"},
		{FilterAll, "", settings.SortName, "bca"},
		{FilterScreenshots, "", settings.SortCaptured, "ca"},
		{FilterRecordings, "", settings.SortName, "b"},
		{FilterAll, "  apple ", settings.SortCaptured, "ba"},
		{FilterScreenshots, "APPLE", settings.SortName, "a"},
		{FilterAll, "kiwi", settings.SortName, ""},
	} {
		if got := dirs(Visible(items, c.f, c.search, c.sort)); got != c.want {
			t.Errorf("Visible(%v, %q, %s) = %s, want %s", c.f, c.search, c.sort, got, c.want)
		}
	}
	if dirs(items) != "cba" {
		t.Error("Visible reordered its input")
	}
}

func TestUpsertKeepsNewestFirst(t *testing.T) {
	mk := func(dir string, h int, title string) Item {
		return Item{Dir: dir, Meta: Meta{Title: title, Created: at(h, 0, 0)}}
	}
	items := []Item{mk("c", 12, ""), mk("a", 10, "")}
	items = Upsert(items, mk("b", 11, ""))
	items = Upsert(items, mk("d", 13, ""))
	items = Upsert(items, mk("e", 9, ""))
	items = Upsert(items, mk("a", 10, "renamed"))
	var got []string
	for _, i := range items {
		got = append(got, i.Dir+i.Meta.Title)
	}
	if !slices.Equal(got, []string{"d", "c", "b", "arenamed", "e"}) {
		t.Errorf("got %q", got)
	}
}

func TestTrashMovesCapturesToTheRecycleBin(t *testing.T) {
	requireRealTrash(t)
	root := t.TempDir()
	lib := New(root)
	keep, _ := lib.ImportScreenshot(NewScreenshot{Base: checker(8, 8), Created: at(8, 0, 0), Title: "glint store test keep"})
	gone, _ := lib.ImportScreenshot(NewScreenshot{Base: checker(8, 8), Created: at(9, 0, 0), Title: "glint store test trash"})
	pending, _ := lib.BeginRecording(at(10, 0, 0))
	rec, _ := lib.FinishRecording(pending, RecordingInfo{Width: 8, Height: 8}, nil)
	items := lib.Scan()

	if err := Trash([]string{gone.Dir, rec.Dir}); err != nil {
		t.Fatal(err)
	}
	if exists(gone.Dir) || exists(rec.Dir) || !exists(keep.Dir) {
		t.Fatalf("after trash: %v", names(t, root))
	}
	items = DropTrashed(items, []string{gone.Dir, rec.Dir, keep.Dir})
	if len(items) != 1 || items[0].Dir != keep.Dir {
		t.Errorf("listed after trash: %+v", items)
	}
	if err := Trash(nil); err != nil {
		t.Errorf("trashing nothing: %v", err)
	}
	if err := Trash([]string{filepath.Join(root, "missing")}); err == nil {
		t.Error("trashing a missing directory succeeded")
	}
}

// TestRealLibraryCopy loads a copy of a real Library the Rust build wrote:
// GLINT_REAL_LIBRARY=%APPDATA%\Glint\Library go test ./internal/store -run RealLibrary -v
// The original is only read.
func TestRealLibraryCopy(t *testing.T) {
	src := os.Getenv("GLINT_REAL_LIBRARY")
	if src == "" {
		t.Skip("GLINT_REAL_LIBRARY is not set")
	}
	root := filepath.Join(t.TempDir(), "Library")
	copyTree(t, src, root)
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[Kind]int{}
	captures := 0
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		captures++
		item, err := Load(filepath.Join(root, e.Name()))
		if err != nil {
			t.Errorf("%s: %v", e.Name(), err)
			continue
		}
		kinds[item.Kind()]++
		if _, err := decodeImage(item.Thumb()); err != nil {
			t.Errorf("%s thumb: %v", e.Name(), err)
		}
		if item.Kind() == Screenshot {
			if _, _, err := LoadEdit(item); err != nil {
				t.Errorf("%s: %v", e.Name(), err)
			}
		}
		data, _ := os.ReadFile(filepath.Join(item.Dir, "meta.json"))
		if again, _ := serde.MarshalPretty(item.Meta); !bytes.Equal(again, data) {
			t.Errorf("%s meta.json does not re-encode byte for byte", e.Name())
		}
	}
	items := New(root).Scan()
	if len(items) != captures {
		t.Errorf("Scan listed %d of %d captures", len(items), captures)
	}
	if !slices.IsSortedFunc(items, func(a, b Item) int { return b.Meta.Created.Compare(a.Meta.Created) }) {
		t.Error("Scan is not newest first")
	}
	t.Logf("real Library copy: %d captures listed (%d screenshots, %d recordings)", len(items), kinds[Screenshot], kinds[Recording])
}

// TestWriteLibraryForRust leaves a Go-written Library where the Rust build's
// reads_go_written_library test checks it: GLINT_GO_WRITTEN=<dir> go test ./internal/store -run WriteLibraryForRust
func TestWriteLibraryForRust(t *testing.T) {
	dir := os.Getenv("GLINT_GO_WRITTEN")
	if dir == "" {
		t.Skip("GLINT_GO_WRITTEN is not set")
	}
	root := filepath.Join(dir, "Library")
	os.RemoveAll(root)
	lib := New(root)
	doc := mustRead(t, "testdata/rust/Library/2026-10-14_09-00-00_f853/edit.json")
	edited := checker(30, 20)
	edited.SetNRGBA(1, 1, color.NRGBA{0, 0, 0, 128})
	created := time.Date(2026, 1, 5, 7, 8, 9, 123_456_789, time.Local)
	if _, err := lib.ImportScreenshot(NewScreenshot{Base: checker(30, 20), Edit: &Edit{Image: edited, Doc: doc}, Created: created, Source: "Notes \u2028 \"draft\" <&>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := lib.ImportScreenshot(NewScreenshot{Base: checker(700, 300), Created: at(10, 0, 0), ImportedFrom: `C:\Users\me\Pictures\Diagram.jpg`, Title: "Diagram"}); err != nil {
		t.Fatal(err)
	}
	pending, _ := lib.BeginRecording(at(11, 0, 0).Add(120 * time.Millisecond))
	os.WriteFile(filepath.Join(pending.Dir, "screen.mp4"), make([]byte, 10), 0o644)
	if _, err := lib.FinishRecording(pending, RecordingInfo{Width: 1280, Height: 720, DurationMs: 4_250, Source: "Display 1"}, checker(1280, 720)); err != nil {
		t.Fatal(err)
	}
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
	if err != nil {
		t.Fatal(err)
	}
}
