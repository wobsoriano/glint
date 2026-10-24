package app

import (
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

func TestRecordingPlanFollowsTheRustOrder(t *testing.T) {
	for _, c := range []struct {
		after settings.AfterRecording
		want  []step
	}{
		{settings.AfterRecording{Preview: true}, []step{stepCard}},
		{settings.AfterRecording{Copy: true, Save: true, Studio: true, Preview: true}, []step{stepCopy, stepSave, stepEdit, stepCard}},
		{settings.AfterRecording{Studio: true}, []step{stepEdit}},
		{settings.AfterRecording{}, nil},
	} {
		if got := recordingPlan(c.after); !slices.Equal(got, c.want) {
			t.Errorf("%+v: got %v, want %v", c.after, got, c.want)
		}
	}
}

func TestAFinishedRecordingRunsAfterRecording(t *testing.T) {
	h := newHarness(t, func(s *settings.Settings) {
		s.AfterRecording = settings.AfterRecording{Copy: true, Save: true, Studio: true, Preview: true}
	})
	var listed []store.Item
	h.Surfaces.Imported = func(it store.Item) { listed = append(listed, it) }
	item := store.Item{Dir: filepath.Join(t.TempDir(), "2026-10-23_10-00-00_ab12.glintrec"), Meta: store.Meta{Kind: store.Recording, Title: "Recording"}}
	h.RecordingFinished(item, []string{"microphone not recorded: gone"})
	if h.last != item.Dir || len(listed) != 1 {
		t.Fatalf("last %q, listed %d", h.last, len(listed))
	}
	if len(h.acts.items) != 1 || len(h.acts.saved) != 1 || len(h.cards) != 1 || h.cards[0].Dir != item.Dir {
		t.Fatalf("copied %d, saved %d, cards %d", len(h.acts.items), len(h.acts.saved), len(h.cards))
	}
	for _, want := range []string{"microphone not recorded: gone", "Studio is not available yet"} {
		if !slices.Contains(h.toasts, want) {
			t.Errorf("no toast %q in %q", want, h.toasts)
		}
	}
	var studio []string
	h.Surfaces.OpenStudio = func(dir string) { studio = append(studio, dir) }
	h.settings.AfterRecording = settings.AfterRecording{Studio: true}
	h.RecordingFinished(item, nil)
	if !slices.Equal(studio, []string{item.Dir}) {
		t.Fatalf("Studio opened %q", studio)
	}
}

func TestQuitWithoutARecorderExitsAtOnce(t *testing.T) {
	h := newHarness(t, nil)
	quit := false
	h.quit(func() { quit = true })
	if !quit {
		t.Fatal("quit did not exit")
	}
}

func TestTheEditorSavesIntoTheCaptureAndTheLibraryHears(t *testing.T) {
	h := newHarness(t, nil)
	item, err := h.library.ImportScreenshot(store.NewScreenshot{Base: solid(40, 30, color.RGBA{200, 10, 10, 255}), Created: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	var listed []store.Item
	h.Surfaces.Imported = func(it store.Item) { listed = append(listed, it) }
	eh := &editorHost{a: h.App, dir: item.Dir, item: item}
	base, _, err := store.LoadEdit(item)
	if err != nil {
		t.Fatal(err)
	}
	doc := render.NewEditDoc(render.DefaultStyle(), 1, nil)
	doc.Annotations = append(doc.Annotations, render.Annotation{ID: 1, Shape: render.Arrow{From: render.P(2, 2), To: render.P(30, 20)}, Color: render.Black, Width: 4})
	img := render.RenderDoc(render.Premultiply(base), doc)
	h.do(1, func() {
		if err := eh.SaveEdit(img, doc); err != nil {
			t.Fatal(err)
		}
	})
	if !samePixels(readPNG(t, item.Deliverable()), img) {
		t.Error("image.png is not the flattened edit")
	}
	raw, err := os.ReadFile(filepath.Join(item.Dir, "edit.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := render.ParseEditDoc(raw); err != nil || len(got.Annotations) != 1 {
		t.Fatalf("edit.json holds %d annotations (%v)", len(got.Annotations), err)
	}
	if len(listed) != 1 || !listed[0].IsEdited() || !listed[0].Meta.Modified.After(item.Meta.Modified) {
		t.Fatalf("the Library heard %d saves", len(listed))
	}

	os.RemoveAll(item.Dir)
	if err := eh.SaveEdit(img, doc); err != errGone {
		t.Fatalf("saving into a deleted capture: %v", err)
	}
}
