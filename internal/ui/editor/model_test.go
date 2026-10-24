package editor

import (
	"image"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/redact"
	"github.com/wobsoriano/glint/internal/render"
)

func testModel(w, h int) *model {
	base := image.NewRGBA(image.Rect(0, 0, w, h))
	return newModel(base, render.NewEditDoc(render.DefaultStyle(), 1, nil))
}

func drawRect(m *model, from, to render.Pt) uint64 {
	m.tool = render.ToolRect
	m.pointerDown(from, 6, false, 1)
	m.dragTo(to, false)
	m.dragEnd()
	return m.sel[0]
}

func rectOf(t *testing.T, m *model, id uint64) render.R {
	t.Helper()
	a := m.ann(id)
	if a == nil {
		t.Fatalf("annotation %d is gone", id)
	}
	return a.Shape.(render.RectShape).Rect
}

func TestCropPresetsKeepAnnotationsOnTheirPixels(t *testing.T) {
	for i, p := range cropPresets {
		if p.ratio == 0 {
			continue
		}
		t.Run(p.name, func(t *testing.T) {
			m := testModel(2560, 1440)
			id := drawRect(m, render.P(1200, 700), render.P(1300, 760))
			before := m.doc.JSON()
			m.setTool(render.ToolCrop)
			m.setCropPreset(i)
			m.applyCrop()
			crop := m.doc.CropRect(2560, 1440)
			want := float32(16.0 / 9)
			if p.ratio > 0 {
				want = p.ratio
			}
			if got := crop.W / crop.H; abs(got-want) > 0.01 {
				t.Errorf("crop %v has ratio %v, want %v", crop, got, want)
			}
			r := rectOf(t, m, id)
			if r.X+crop.X != 1200 || r.Y+crop.Y != 700 {
				t.Errorf("rect at %v in a crop at %v sits on base pixel (%v, %v), want (1200, 700)", r, crop, r.X+crop.X, r.Y+crop.Y)
			}
			if m.doc.Crop == nil {
				if string(m.doc.JSON()) != string(before) {
					t.Errorf("a crop of the whole image changed the document")
				}
				return
			}
			m.undo()
			if string(m.doc.JSON()) != string(before) {
				t.Errorf("undo left crop %v and rect %v", m.doc.Crop, rectOf(t, m, id))
			}
		})
	}
}

func TestFreeformCropDragAndCancel(t *testing.T) {
	m := testModel(1000, 800)
	id := drawRect(m, render.P(400, 300), render.P(500, 400))
	m.setTool(render.ToolCrop)
	m.pointerDown(render.P(-50, -50), 6, false, 1) // outside the image: a new crop from the corner
	m.dragTo(render.P(600, 500), false)
	m.dragEnd()
	if m.crop.rect != (render.R{W: 600, H: 500}) {
		t.Fatalf("crop dragged to %v", m.crop.rect)
	}
	m.escape()
	if m.crop != nil || m.doc.Crop != nil {
		t.Fatalf("escape kept the crop: %v %v", m.crop, m.doc.Crop)
	}
	m.setTool(render.ToolCrop)
	m.pointerDown(render.P(2, 1), 6, false, 1) // the top-left handle
	m.dragTo(render.P(100, 100), false)
	m.dragEnd()
	m.pointerDown(render.P(999, 798), 6, false, 1) // the bottom-right one
	m.dragTo(render.P(900, 700), false)
	m.dragEnd()
	if m.crop.rect != (render.R{X: 100, Y: 100, W: 800, H: 600}) {
		t.Fatalf("handles dragged the crop to %v", m.crop.rect)
	}
	m.applyCrop()
	if r := rectOf(t, m, id); r.X != 300 || r.Y != 200 {
		t.Errorf("rect at %v after cropping at (100, 100), want (300, 200)", r)
	}
	if m.tool != render.ToolSelect {
		t.Errorf("applying a crop leaves tool %v", m.tool)
	}
}

func TestUndoRedoWalkTheStack(t *testing.T) {
	m := testModel(800, 600)
	a := drawRect(m, render.P(10, 10), render.P(110, 60))
	b := drawRect(m, render.P(200, 200), render.P(300, 260))
	m.undo()
	if m.ann(b) != nil || m.ann(a) == nil {
		t.Fatalf("one undo should remove only the last rect")
	}
	m.redo()
	if m.ann(b) == nil {
		t.Fatalf("redo should bring the rect back")
	}
	m.undo()
	m.undo()
	if len(m.doc.Annotations) != 0 {
		t.Fatalf("two undos leave %d annotations", len(m.doc.Annotations))
	}
	m.undo()
	m.redo()
	m.redo()
	if len(m.doc.Annotations) != 2 {
		t.Fatalf("two redos give %d annotations", len(m.doc.Annotations))
	}
	m.undo()
	drawRect(m, render.P(400, 400), render.P(450, 450))
	m.redo()
	if m.ann(b) != nil {
		t.Errorf("a new edit after undo must clear redo")
	}
}

func TestSliderEditsCoalesceIntoOneStep(t *testing.T) {
	m := testModel(400, 300)
	clock := time.Unix(1000, 0)
	m.now = func() time.Time { return clock }
	for _, v := range []float32{70, 80, 90} {
		m.edit("pad", func(d *render.EditDoc) { d.Style.Padding = v })
		clock = clock.Add(100 * time.Millisecond)
	}
	clock = clock.Add(2 * time.Second)
	m.edit("pad", func(d *render.EditDoc) { d.Style.Padding = 120 })
	m.undo()
	if m.doc.Style.Padding != 90 {
		t.Fatalf("after a pause, a drag is a new step: padding %v, want 90", m.doc.Style.Padding)
	}
	m.undo()
	if m.doc.Style.Padding != 64 {
		t.Fatalf("one drag undoes at once: padding %v, want 64", m.doc.Style.Padding)
	}
}

func TestTinyAnnotationsAreDroppedWithTheirUndoStep(t *testing.T) {
	m := testModel(800, 600)
	m.tool = render.ToolArrow
	m.pointerDown(render.P(100, 100), 6, false, 1)
	m.dragTo(render.P(102, 101), false)
	m.dragEnd()
	if len(m.doc.Annotations) != 0 || len(m.hist.undo) != 0 {
		t.Fatalf("a click with the arrow left %d annotations and %d undo steps", len(m.doc.Annotations), len(m.hist.undo))
	}
}

func TestMoveResizeAndDeleteSelection(t *testing.T) {
	m := testModel(800, 600)
	id := drawRect(m, render.P(100, 100), render.P(200, 160))
	m.tool = render.ToolSelect
	m.pointerDown(render.P(100, 130), 6, false, 1)
	m.dragTo(render.P(150, 180), false)
	m.dragEnd()
	if r := rectOf(t, m, id); r != (render.R{X: 150, Y: 150, W: 100, H: 60}) {
		t.Fatalf("moved rect is %v", r)
	}
	m.pointerDown(render.P(250, 210), 6, false, 1) // the bottom-right handle
	m.dragTo(render.P(300, 260), false)
	m.dragEnd()
	if r := rectOf(t, m, id); r != (render.R{X: 150, Y: 150, W: 150, H: 110}) {
		t.Fatalf("resized rect is %v", r)
	}
	m.deleteSelected()
	if len(m.doc.Annotations) != 0 {
		t.Fatal("delete kept the rect")
	}
	m.undo()
	m.undo()
	if r := rectOf(t, m, id); r != (render.R{X: 150, Y: 150, W: 100, H: 60}) {
		t.Fatalf("undoing delete and resize gives %v", r)
	}
}

func TestMarqueeAndShiftClickSelectSeveral(t *testing.T) {
	m := testModel(800, 600)
	a := drawRect(m, render.P(10, 10), render.P(60, 60))
	b := drawRect(m, render.P(100, 10), render.P(150, 60))
	c := drawRect(m, render.P(400, 400), render.P(450, 450))
	m.tool = render.ToolSelect
	m.pointerDown(render.P(250, 150), 6, false, 1)
	m.dragTo(render.P(0, 0), false)
	m.dragEnd()
	if len(m.sel) != 2 || !m.selected(a) || !m.selected(b) {
		t.Fatalf("marquee selected %v", m.sel)
	}
	m.pointerDown(render.P(400, 425), 6, true, 1)
	m.dragEnd()
	if len(m.sel) != 3 || !m.selected(c) {
		t.Fatalf("shift-click added: %v", m.sel)
	}
}

func TestTextToolTypesAndEmptyTextVanishes(t *testing.T) {
	m := testModel(800, 600)
	m.setTool(render.ToolText)
	m.pointerDown(render.P(100, 100), 6, false, 1)
	m.typeText("Hi")
	m.typeKey("enter", true)
	m.typeText("there")
	m.typeKey("backspace", false)
	m.typeKey("enter", false)
	if len(m.doc.Annotations) != 1 || m.doc.Annotations[0].Shape.(render.Text).Text != "Hi\nther" {
		t.Fatalf("typed %+v", m.doc.Annotations)
	}
	m.pointerDown(render.P(500, 500), 6, false, 1)
	m.escape()
	if len(m.doc.Annotations) != 1 {
		t.Fatalf("an empty text should vanish, have %d annotations", len(m.doc.Annotations))
	}
	if !m.dirty() {
		t.Error("typed text should make the edit dirty")
	}
}

func TestFindingsBecomeOneUndoStepOfSelectedRedactions(t *testing.T) {
	m := testModel(800, 600)
	m.doc.DPI = 1.5
	found := []redact.Finding{
		{Kind: redact.Email, Rect: render.R{X: 10, Y: 10, W: 200, H: 20}},
		{Kind: redact.Email, Rect: render.R{X: 10, Y: 40, W: 180, H: 20}},
		{Kind: redact.Card, Rect: render.R{X: 10, Y: 70, W: 220, H: 20}},
	}
	if msg := m.addFindings(found, render.RedactPixelate); msg != "Hid 2 emails, 1 card number; strength stays editable" {
		t.Errorf("summary %q", msg)
	}
	if len(m.doc.Annotations) != 3 || len(m.sel) != 3 || m.tool != render.ToolSelect {
		t.Fatalf("annotations %d, selected %d, tool %v", len(m.doc.Annotations), len(m.sel), m.tool)
	}
	if r := m.doc.Annotations[0].Shape.(render.Redact); r.Kind != render.RedactPixelate || r.Strength != render.RedactStrength*1.5 {
		t.Errorf("redaction %+v", r)
	}
	if msg := m.addFindings(found, render.RedactPixelate); msg != "No sensitive info found" || len(m.doc.Annotations) != 3 {
		t.Errorf("a second scan said %q and left %d annotations", msg, len(m.doc.Annotations))
	}
	m.undo()
	if len(m.doc.Annotations) != 0 {
		t.Errorf("one undo should remove every redaction, %d left", len(m.doc.Annotations))
	}
}

func TestReopenedIDsStayAndNewOnesFollow(t *testing.T) {
	doc := render.NewEditDoc(render.DefaultStyle(), 1, nil)
	doc.Annotations = []render.Annotation{{ID: 7, Shape: render.Line{To: render.P(50, 50)}, Width: 3}, {ID: 7, Shape: render.Line{To: render.P(90, 90)}, Width: 3}}
	m := newModel(image.NewRGBA(image.Rect(0, 0, 100, 100)), doc)
	if m.doc.Annotations[0].ID != 7 || m.doc.Annotations[1].ID != 8 || m.dirty() {
		t.Fatalf("ids %d %d, dirty %v", m.doc.Annotations[0].ID, m.doc.Annotations[1].ID, m.dirty())
	}
	if id := drawRect(m, render.P(1, 1), render.P(40, 40)); id != 9 {
		t.Fatalf("new id %d, want 9", id)
	}
}
