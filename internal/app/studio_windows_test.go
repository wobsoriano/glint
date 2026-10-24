package app

import (
	"context"
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/studio"
)

// recordSolid puts a recording of a solid red screen into the Library, as the recorder does.
func recordSolid(t *testing.T, lib store.Library) store.Item {
	t.Helper()
	created := time.Date(2026, 1, 2, 10, 0, 0, 0, time.Local)
	p, err := lib.BeginRecording(created)
	if err != nil {
		t.Fatal(err)
	}
	w, err := media.CreateVideoWriter(filepath.Join(p.Dir, "screen.mp4"), media.VideoParams{Width: 320, Height: 180, FPS: 30, Bitrate: 2_000_000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 320, 180))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+3] = 220, 255
	}
	for range 30 {
		if err := w.WriteFrame(img); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Finish(); err != nil {
		t.Fatal(err)
	}
	item, err := lib.FinishRecording(p, store.RecordingInfo{Width: 320, Height: 180, DurationMs: 1000, Source: "Display 1"}, img)
	if err != nil {
		t.Fatal(err)
	}
	return item
}

func readThumb(t *testing.T, item store.Item) image.Image {
	t.Helper()
	f, err := os.Open(item.Thumb())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestAStudioExportBecomesTheRecordingsThumbnailAndDeliverable(t *testing.T) {
	h := newHarness(t, nil)
	var listed []store.Item
	h.Surfaces.Imported = func(it store.Item) { listed = append(listed, it) }
	item := recordSolid(t, h.library)
	if c := color.RGBAModel.Convert(readThumb(t, item).At(0, 0)).(color.RGBA); c.R < 200 || c.G > 30 {
		t.Fatalf("the recorded thumbnail's corner is %v, not the red screen", c)
	}

	s, err := studio.LoadSession(item.Dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Project.Design.Padding = 0.2
	if err := s.SaveProject(); err != nil {
		t.Fatal(err)
	}
	render, err := studio.Export(context.Background(), s, s.Project.Export, nil)
	if err != nil {
		t.Fatal(err)
	}
	saved := filepath.Join(t.TempDir(), "Recording.mp4")
	data, err := os.ReadFile(render)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(saved, data, 0o644); err != nil {
		t.Fatal(err)
	}

	h.do(1, func() { studioHost{h.App, item.Dir}.RecordingExported(saved) })

	if len(listed) != 1 || listed[0].Dir != item.Dir {
		t.Fatalf("the Library heard of %d item(s)", len(listed))
	}
	after := listed[0]
	if !after.Meta.Modified.After(item.Meta.Modified) {
		t.Errorf("modified %v, not after %v: the Library would keep the old thumbnail", after.Meta.Modified, item.Meta.Modified)
	}
	if c := color.RGBAModel.Convert(readThumb(t, after).At(0, 0)).(color.RGBA); c.R >= 200 && c.G <= 30 {
		t.Errorf("the thumbnail's corner is still the red screen %v, not the export's background", c)
	}
	frame, err := firstFrame(saved)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := readThumb(t, after).Bounds().Size(), store.Thumbnail(frame).Bounds().Size(); got != want {
		t.Errorf("thumbnail is %v, the export's first frame scales to %v", got, want)
	}

	file, _, err := studioDeliverable(after)
	if err != nil {
		t.Fatal(err)
	}
	if file != render {
		t.Errorf("Copy and Save hand out %s, not the render Studio exported, %s", file, render)
	}
}
