package editor

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
)

type fakeHost struct {
	saveErr error
	saved   []render.EditDoc
	img     *image.RGBA
	exports []bool
	copies  int
	copied  *image.RGBA
	styles  []render.Style
}

func (h *fakeHost) SaveEdit(img *image.RGBA, doc render.EditDoc) error {
	if h.saveErr != nil {
		return h.saveErr
	}
	h.saved, h.img = append(h.saved, doc), img
	return nil
}

func (h *fakeHost) Export(title string, _ *image.RGBA, ask bool) (string, error) {
	h.exports = append(h.exports, ask)
	return title + ".png", nil
}

func (h *fakeHost) Copy(img *image.RGBA) error {
	h.copies++
	h.copied = img
	return nil
}
func (h *fakeHost) Pin(*image.RGBA, float32) error { return nil }
func (h *fakeHost) SaveStyle(s render.Style) error { h.styles = append(h.styles, s); return nil }
func (h *fakeHost) Closed()                        {}

func testEditor(t *testing.T) (*Editor, *fakeHost, *ui.Tester) {
	t.Helper()
	if os.Getenv("GLINT_DATA_DIR") == "" {
		t.Setenv("GLINT_DATA_DIR", t.TempDir())
	}
	base := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	for i := range base.Pix {
		base.Pix[i] = uint8(i * 7)
	}
	for i := 3; i < len(base.Pix); i += 4 {
		base.Pix[i] = 255
	}
	host := &fakeHost{}
	e := newEditor(Options{
		Title: "Test capture", Base: base, Doc: render.NewEditDoc(render.DefaultStyle(), 1, nil),
		Host: host, PresetsPath: filepath.Join(t.TempDir(), "presets.json"),
	}, nil)
	tt := ui.NewTester(e.view, 1360, 880)
	return e, host, tt
}

func canvasCenter(t *testing.T, tt *ui.Tester) (float32, float32) {
	t.Helper()
	r, ok := tt.Find("Canvas")
	if !ok {
		t.Fatal("no canvas")
	}
	return r.X + r.W/2, r.Y + r.H/2
}

func dragOn(tt *ui.Tester, x0, y0, x1, y1 float32) {
	tt.Press(x0, y0)
	for i := 1; i <= 4; i++ {
		f := float32(i) / 4
		tt.Move(x0+(x1-x0)*f, y0+(y1-y0)*f)
	}
	tt.Release(x1, y1)
}

func TestToolButtonsAndLetterKeysSwitchTools(t *testing.T) {
	e, _, tt := testEditor(t)
	if err := tt.Click("Rectangle (R)"); err != nil {
		t.Fatal(err)
	}
	if e.m.tool != render.ToolRect {
		t.Fatalf("the rectangle button chose %v", e.m.tool)
	}
	for key, want := range map[ui.Key]render.Tool{ui.KeyO: render.ToolEllipse, ui.KeyN: render.ToolCounter, ui.KeyX: render.ToolPixelate, ui.KeyV: render.ToolSelect} {
		tt.Key(0, key)
		if e.m.tool != want {
			t.Errorf("key %v chose %v, want %v", key, e.m.tool, want)
		}
	}
	tt.Key(0, ui.KeyC)
	if e.m.activeTool() != render.ToolCrop || !tt.HasText("Apply crop") {
		t.Fatalf("C should start a crop, tool %v", e.m.activeTool())
	}
	tt.Key(0, ui.KeyEscape)
	if e.m.crop != nil {
		t.Fatal("Escape should cancel the crop")
	}
}

func TestDrawingUndoAndRedoThroughTheWindow(t *testing.T) {
	e, _, tt := testEditor(t)
	x, y := canvasCenter(t, tt)
	tt.Key(0, ui.KeyR)
	dragOn(tt, x-100, y-60, x+100, y+60)
	if len(e.m.doc.Annotations) != 1 {
		t.Fatalf("dragging with the rectangle tool made %d annotations", len(e.m.doc.Annotations))
	}
	r := e.m.doc.Annotations[0].Shape.(render.RectShape).Rect
	if r.W < 50 || r.H < 30 {
		t.Fatalf("rect %v is smaller than the drag", r)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if len(e.m.doc.Annotations) != 0 {
		t.Fatal("Ctrl+Z should undo the rect")
	}
	tt.Key(ui.Cmd, ui.KeyY)
	if len(e.m.doc.Annotations) != 1 {
		t.Fatal("Ctrl+Y should redo it")
	}
	tt.Key(0, ui.KeyV)
	dragOn(tt, x-100, y, x-40, y+30) // grab its left edge
	moved := e.m.doc.Annotations[0].Shape.(render.RectShape).Rect
	if moved.X <= r.X || moved.W != r.W {
		t.Fatalf("dragging the edge should move the rect: %v then %v", r, moved)
	}
	tt.Key(0, ui.KeyDelete)
	if len(e.m.doc.Annotations) != 0 {
		t.Fatal("Delete should remove the selected rect")
	}
}

func TestTextToolTypesFromTheKeyboard(t *testing.T) {
	e, _, tt := testEditor(t)
	x, y := canvasCenter(t, tt)
	tt.Click("Text (T)")
	tt.ClickAt(x, y)
	tt.Type("Read this")
	if e.m.tool != render.ToolText {
		t.Fatalf("typing letters switched the tool to %v", e.m.tool)
	}
	tt.Key(0, ui.KeyEnter)
	if len(e.m.doc.Annotations) != 1 || e.m.typing != 0 {
		t.Fatalf("annotations %d, typing %d", len(e.m.doc.Annotations), e.m.typing)
	}
	if got := e.m.doc.Annotations[0].Shape.(render.Text).Text; got != "Read this" {
		t.Fatalf("typed %q", got)
	}
}

func TestCropPresetFromTheStyleBarRemapsAnnotations(t *testing.T) {
	e, _, tt := testEditor(t)
	x, y := canvasCenter(t, tt)
	tt.Key(0, ui.KeyR)
	dragOn(tt, x-40, y-40, x+40, y+40)
	before := e.m.doc.Annotations[0].Shape.(render.RectShape).Rect
	tt.Click("Crop (C)")
	if err := tt.Click("1:1"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Apply crop"); err != nil {
		t.Fatal(err)
	}
	crop := e.m.doc.CropRect(1600, 900)
	if crop.W != 900 || crop.H != 900 || crop.X != 350 {
		t.Fatalf("1:1 crop is %v", crop)
	}
	after := e.m.doc.Annotations[0].Shape.(render.RectShape).Rect
	if after.X+crop.X != before.X || after.Y+crop.Y != before.Y {
		t.Fatalf("rect moved from %v to %v under crop %v", before, after, crop)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if e.m.doc.Crop != nil {
		t.Fatal("undo should remove the crop")
	}
}

func TestFailedSaveKeepsTheEditorOpenWithTheChanges(t *testing.T) {
	e, host, tt := testEditor(t)
	x, y := canvasCenter(t, tt)
	dragOn(tt, x-80, y, x+80, y-40)
	host.saveErr = errors.New("disk full")
	tt.Click("Done")
	if e.closed || !e.m.dirty() || len(e.m.doc.Annotations) != 1 {
		t.Fatalf("closed %v, dirty %v, annotations %d", e.closed, e.m.dirty(), len(e.m.doc.Annotations))
	}
	tt.Frame()
	if !tt.HasText("Couldn't save the edit: disk full") {
		t.Errorf("no error toast in %q", tt.Texts())
	}
	host.saveErr = nil
	tt.Click("Save")
	if len(host.saved) != 1 || len(host.exports) != 1 || host.exports[0] || e.m.dirty() || e.closed {
		t.Fatalf("save: saved %d, exports %v, dirty %v, closed %v", len(host.saved), host.exports, e.m.dirty(), e.closed)
	}
	if !sameDoc(host.saved[0], e.m.doc) {
		t.Error("the saved document is not the one shown")
	}
	want := render.RenderDoc(e.m.base, e.m.doc)
	if host.img.Rect != want.Rect || string(host.img.Pix) != string(want.Pix) {
		t.Error("the saved image is not RenderDoc of the saved document")
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	if !e.closed {
		t.Error("Ctrl+Enter (Done) should close after saving")
	}
}

func TestCopySavesTheEditIntoTheCaptureFirst(t *testing.T) {
	e, host, tt := testEditor(t)
	x, y := canvasCenter(t, tt)
	tt.Key(0, ui.KeyA)
	dragOn(tt, x-80, y, x+80, y-40)
	host.saveErr = errors.New("disk full")
	tt.Click("Copy")
	tt.Frame()
	if host.copies != 0 || !e.m.dirty() || e.closed || !tt.HasText("Couldn't save the edit: disk full") {
		t.Fatalf("a failed save copied %d times, dirty %v, closed %v, texts %q", host.copies, e.m.dirty(), e.closed, tt.Texts())
	}
	host.saveErr = nil
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if len(host.saved) != 1 || host.copies != 1 || e.m.dirty() || e.closed {
		t.Fatalf("Ctrl+C: saved %d, copied %d, dirty %v, closed %v", len(host.saved), host.copies, e.m.dirty(), e.closed)
	}
	if !sameDoc(host.saved[0], e.m.doc) || host.copied != host.img {
		t.Error("the copy is not the image just saved")
	}
	want := fmt.Sprintf("Copied %d × %d image", host.img.Rect.Dx(), host.img.Rect.Dy())
	if !tt.HasText(want) {
		t.Errorf("no %q in %q", want, tt.Texts())
	}
	if len(host.exports) != 0 {
		t.Errorf("Copy exported %v", host.exports)
	}
}

func TestPresetsSaveApplyAndRoundTrip(t *testing.T) {
	e, _, tt := testEditor(t)
	tt.Click("Solid")
	tt.Click("Save look")
	if len(e.m.presets) != 1 || e.m.presets[0].Name != "Preset 1" {
		t.Fatalf("presets %+v", e.m.presets)
	}
	tt.Click("Gradient")
	if _, ok := e.m.doc.Style.Background.(render.GradientBackground); !ok {
		t.Fatal("Gradient should change the background")
	}
	tt.Click("Preset 1")
	if _, ok := e.m.doc.Style.Background.(render.SolidBackground); !ok {
		t.Fatalf("applying the preset gives %T", e.m.doc.Style.Background)
	}
	stored, err := render.LoadPresetStore(e.presetsPath)
	if err != nil || len(stored) != 1 {
		t.Fatalf("presets.json holds %v (%v)", stored, err)
	}
	list, err := render.ImportPresets(render.ExportPresets(e.m.presets))
	if err != nil {
		t.Fatal(err)
	}
	e.m.importPresets(list)
	if len(e.m.presets) != 2 || e.m.presets[1].Name != "Preset 1 2" {
		t.Fatalf("imported presets %+v", e.m.presets)
	}
}
