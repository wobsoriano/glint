package editor

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
)

func writePNG(t *testing.T, path string, w, h int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
}

func TestAddedBackgroundsAreCopiedUnderUniqueNames(t *testing.T) {
	data := t.TempDir()
	t.Setenv("GLINT_DATA_DIR", data)
	src := t.TempDir()
	writePNG(t, filepath.Join(src, "a", "beach.png"), 400, 300)
	writePNG(t, filepath.Join(src, "b", "beach.png"), 300, 400)
	g := &gallery{listeners: map[*func()]bool{}}
	first, err := g.add(filepath.Join(src, "a", "beach.png"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := g.add(filepath.Join(src, "b", "beach.png"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(data, "Wallpapers")
	if first != filepath.Join(dir, "beach.png") || second != filepath.Join(dir, "beach (1).png") {
		t.Fatalf("copied to %s and %s", first, second)
	}
	if walls := g.list(); len(walls) != 2 || walls[0].path != second {
		t.Fatalf("the newest should come first: %+v", walls)
	}
	if w, h := g.list()[0].thumb.Size(); w != thumbW || h != thumbH {
		t.Errorf("thumbnail %dx%d", w, h)
	}
	if _, err := g.add(filepath.Join(src, "notes.txt")); err == nil {
		t.Error("a text file should be refused")
	}
}

func TestSaveAsDefaultWritesTheStyleToSettings(t *testing.T) {
	t.Setenv("GLINT_DATA_DIR", t.TempDir())
	e, _, tt := testEditor(t)
	tt.SetSize(1360, 2000)
	tt.Click("Solid")
	if err := tt.Click("Save as default"); err != nil {
		t.Fatal(err)
	}
	st := settings.Load().Style
	if _, ok := st.Background.(render.SolidBackground); !ok || st.Padding != e.m.doc.Style.Padding {
		t.Fatalf("settings hold %+v", st)
	}
}
