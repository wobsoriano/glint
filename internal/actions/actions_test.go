package actions

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/transfer"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

func TestSanitizeMakesTitlesFileNames(t *testing.T) {
	for in, want := range map[string]string{
		`Screenshot 2026-10-14 at 14.03.22`: `Screenshot 2026-10-14 at 14.03.22`,
		`a<b>c:d"e/f\g|h?i*j`:               `a_b_c_d_e_f_g_h_i_j`,
		"  tab\there.. ":                    "tab_here",
		"...":                               "Capture",
		"":                                  "Capture",
	} {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	for n, want := range map[uint64]string{0: "0 B", 1023: "1023 B", 1024: "1 KB", 5 << 20: "5.0 MB", 3 << 29: "1.5 GB"} {
		if got := HumanBytes(n); got != want {
			t.Errorf("HumanBytes(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestExportNumbersTakenNamesAndWritesTheFormat(t *testing.T) {
	dir := t.TempDir()
	img := swatch(40, 30, color.RGBA{200, 30, 60, 255})
	first, err := Export(img, dir, "Shot: one", settings.FormatPNG, 85)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Export(img, dir, "Shot: one", settings.FormatPNG, 85)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(first) != "Shot_ one.png" || filepath.Base(second) != "Shot_ one (2).png" {
		t.Fatalf("names %s, %s", first, second)
	}
	if got := decodePNG(t, first); got.Bounds().Dx() != 40 || colorAt(got, 5, 5) != (color.RGBA{200, 30, 60, 255}) {
		t.Fatalf("PNG round trip: %v %v", got.Bounds(), colorAt(got, 5, 5))
	}
	jpg, err := Export(img, dir, "q", settings.FormatJPEG, 90)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(jpg)
	decoded, err := jpeg.Decode(bytes.NewReader(data))
	if err != nil || filepath.Ext(jpg) != ".jpg" {
		t.Fatalf("JPEG %s: %v", jpg, err)
	}
	if c := colorAt(decoded, 20, 15); !near(c, color.RGBA{200, 30, 60, 255}, 12) {
		t.Fatalf("JPEG colour %v", c)
	}
}

func TestJPEGQualityChangesTheFileSize(t *testing.T) {
	img := noise(256, 256)
	low, _ := Encode(img, settings.FormatJPEG, 20)
	high, _ := Encode(img, settings.FormatJPEG, 95)
	if len(low) >= len(high) {
		t.Fatalf("quality 20 gave %d bytes, 95 gave %d", len(low), len(high))
	}
}

func TestSaveWritesIntoTheExportFolderWithoutAsking(t *testing.T) {
	a, rec := fake(t, func(s *settings.Settings) { s.Format = settings.FormatJPEG })
	a.askSavePath = func(string, string, settings.ExportFormat) string { t.Error("asked for a path"); return "" }
	path, err := rec.save(a, "Window title", false)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != a.settings().SaveDir || filepath.Ext(path) != ".jpg" {
		t.Fatalf("saved %s", path)
	}
	a.ReportSave(path, err)
	if got := rec.lastToast(); got != "Saved Window title.jpg" {
		t.Fatalf("toast %q", got)
	}
}

func TestSaveAsksWhenTheSettingsSayAlwaysAsk(t *testing.T) {
	a, rec := fake(t, func(s *settings.Settings) { s.SaveWithoutDialog = false })
	picked := filepath.Join(t.TempDir(), "chosen.jpeg")
	a.askSavePath = func(dir, name string, f settings.ExportFormat) string {
		if name != "Title.png" || f != settings.FormatPNG {
			t.Errorf("dialog proposed %s as %s", name, f)
		}
		return picked
	}
	path, err := rec.save(a, "Title", false)
	if err != nil || path != picked {
		t.Fatalf("saved %s: %v", path, err)
	}
	data, _ := os.ReadFile(path)
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("a .jpeg name writes JPEG: %v", err)
	}
	a.askSavePath = func(string, string, settings.ExportFormat) string { return "" }
	if _, err := rec.save(a, "Title", true); !errors.Is(err, ErrCancelled) {
		t.Fatalf("cancelled dialog: %v", err)
	}
	a.ReportSave("", ErrCancelled)
	if got := rec.lastToast(); got != "" {
		t.Fatalf("a cancelled save toasts %q", got)
	}
}

func TestAFailedSaveOffersRetryAndAnotherFolder(t *testing.T) {
	a, rec := fake(t, nil)
	blocked := filepath.Join(t.TempDir(), "file-not-folder")
	os.WriteFile(blocked, nil, 0o644)
	s := a.settings()
	s.SaveDir = blocked
	a.settings = func() settings.Settings { return s }

	var asked []string
	a.askSaveFailed = func(detail string) SaveChoice {
		asked = append(asked, detail)
		if len(asked) == 1 {
			return SaveRetry
		}
		return SaveChooseFolder
	}
	other := t.TempDir()
	a.askFolder = func(button string) string {
		if button != "Save Here" {
			t.Errorf("folder button %q", button)
		}
		return other
	}
	path, err := rec.save(a, "Retry me", false)
	if err != nil || filepath.Dir(path) != other {
		t.Fatalf("saved %s: %v", path, err)
	}
	if len(asked) != 2 || !strings.Contains(asked[0], "file-not-folder") {
		t.Fatalf("asked %q", asked)
	}

	a.askSaveFailed = func(string) SaveChoice { return SaveCancel }
	if _, err := rec.save(a, "Give up", false); err == nil || errors.Is(err, ErrCancelled) {
		t.Fatalf("cancel after a failure reports the failure: %v", err)
	}
	a.ReportSave("", errors.New("disk full"))
	if got := rec.lastToast(); got != "Not saved: disk full" {
		t.Fatalf("toast %q", got)
	}
}

func TestCopyImageOffersPNG(t *testing.T) {
	a, rec := fake(t, nil)
	img := swatch(8, 6, color.RGBA{1, 2, 3, 255})
	done := make(chan error, 1)
	a.CopyImage(img, func(err error) { done <- err })
	if err := wait(t, done); err != nil {
		t.Fatal(err)
	}
	b, err := rec.clip.Read(transfer.PNG)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(b))
	if err != nil || got.Bounds().Dx() != 8 || colorAt(got, 3, 3) != (color.RGBA{1, 2, 3, 255}) {
		t.Fatalf("clipboard PNG %v %v", got, err)
	}
}

func TestCopyTextOfCopiesReadingOrderAndSaysSo(t *testing.T) {
	a, rec := fake(t, nil)
	a.recognize = func(*image.RGBA) ([]ocr.Line, error) {
		return []ocr.Line{
			{Words: []ocr.Word{{Text: "first"}, {Text: "line"}}, Bounds: ocr.Rect{Y: 0, H: 10}},
			{Words: []ocr.Word{{Text: "second"}}, Bounds: ocr.Rect{Y: 20, H: 10}},
		}, nil
	}
	a.CopyTextOf(swatch(4, 4, color.RGBA{}))
	if got := rec.waitToast(t); got != "Copied text: first line second" {
		t.Fatalf("toast %q", got)
	}
	if rec.text != "first line\nsecond" {
		t.Fatalf("clipboard text %q", rec.text)
	}
	a.recognize = func(*image.RGBA) ([]ocr.Line, error) { return nil, nil }
	a.CopyTextOf(swatch(4, 4, color.RGBA{}))
	if got := rec.waitToast(t); got != "No text found" {
		t.Fatalf("toast %q", got)
	}
}

func TestDeleteSaysWhereTheCapturesWent(t *testing.T) {
	a, rec := fake(t, nil)
	var trashed []string
	a.trash = func(dirs []string) error { trashed = dirs; return nil }
	done := make(chan bool, 1)
	a.Delete([]string{"a", "b"}, func(ok bool) { done <- ok })
	if !wait(t, done) || len(trashed) != 2 {
		t.Fatal("delete did not finish")
	}
	if got := rec.lastToast(); got != "Moved 2 captures to the "+Trash {
		t.Fatalf("toast %q", got)
	}
}

func TestExportToWritesTheScreenshotsInTheExportFormat(t *testing.T) {
	a, rec := fake(t, func(s *settings.Settings) { s.Format = settings.FormatJPEG })
	lib := store.New(t.TempDir())
	var items []store.Item
	for i, title := range []string{"First", "Second"} {
		it, err := lib.ImportScreenshot(store.NewScreenshot{Base: swatch(20+i, 10, color.RGBA{200, 10, 10, 255}), Created: time.Now().Add(time.Duration(i) * time.Second), Title: title})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	items = append(items, store.Item{Meta: store.Meta{Kind: store.Recording, Title: "Talk"}})
	dir := t.TempDir()
	a.ExportTo(items, dir)
	if got := rec.waitToast(t); got != "Exported 2 capture(s)" {
		t.Fatalf("toast %q", got)
	}
	for _, name := range []string{"First.jpg", "Second.jpg"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestCopyTextRefusesRecordings(t *testing.T) {
	a, rec := fake(t, nil)
	a.CopyText(store.Item{Meta: store.Meta{Kind: store.Recording}})
	if got := rec.lastToast(); !strings.HasPrefix(got, "This works on screenshots") {
		t.Fatalf("toast %q", got)
	}
}

type recorder struct {
	mu     sync.Mutex
	toasts []string
	toastC chan string
	clip   transfer.Data
	text   string
	img    image.Image
}

func fake(t *testing.T, change func(*settings.Settings)) (*Actions, *recorder) {
	t.Setenv("GLINT_DATA_DIR", t.TempDir())
	s := settings.Default()
	s.SaveDir = filepath.Join(t.TempDir(), "Exports")
	if change != nil {
		change(&s)
	}
	rec := &recorder{toastC: make(chan string, 16), img: swatch(30, 20, color.RGBA{10, 120, 230, 255})}
	a := New(func() settings.Settings { return s }, func(m string) {
		rec.mu.Lock()
		rec.toasts = append(rec.toasts, m)
		rec.mu.Unlock()
		rec.toastC <- m
	})
	var main sync.Mutex
	a.post = func(fn func()) { main.Lock(); defer main.Unlock(); fn() }
	a.writeClip = func(d transfer.Data) error { rec.clip = d; return nil }
	a.writeText = func(s string) { rec.text = s }
	a.writeFiles = func(...string) error { return nil }
	a.askSaveFailed = func(string) SaveChoice { t.Error("a save failed unexpectedly"); return SaveCancel }
	return a, rec
}

func (r *recorder) save(a *Actions, title string, ask bool) (string, error) {
	type result struct {
		path string
		err  error
	}
	done := make(chan result, 1)
	a.SaveImage(title, r.img, ask, func(p string, err error) { done <- result{p, err} })
	select {
	case res := <-done:
		return res.path, res.err
	case <-time.After(5 * time.Second):
		return "", errors.New("save did not finish")
	}
}

func (r *recorder) lastToast() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.toasts) == 0 {
		return ""
	}
	return r.toasts[len(r.toasts)-1]
}

func (r *recorder) waitToast(t *testing.T) string { return wait(t, r.toastC) }

func wait[T any](t *testing.T, c chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
		var zero T
		return zero
	}
}

func swatch(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func noise(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	v := uint32(1)
	for i := range img.Pix {
		v = v*1664525 + 1013904223
		img.Pix[i] = uint8(v >> 24)
		if i%4 == 3 {
			img.Pix[i] = 255
		}
	}
	return img
}

func decodePNG(t *testing.T, path string) image.Image {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func colorAt(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

func near(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) bool { v := int(x) - int(y); return v <= tol && v >= -tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}
