package record

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/store"
)

func writeClip(t *testing.T, path string) {
	t.Helper()
	w, err := media.CreateVideoWriter(path, media.VideoParams{Width: 64, Height: 48, FPS: 30, Bitrate: 1_000_000}, nil)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for i := range 30 {
		for p := range img.Pix {
			img.Pix[p] = uint8(i * 8)
		}
		img.Set(0, 0, color.RGBA{255, 0, 0, 255})
		if err := w.WriteFrame(img); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := w.Finish(); err != nil {
		t.Fatal(err)
	}
}

// A recording that stopped but never reached the Library plays, so it is recovered; one
// killed mid-way, cut off before the MP4 index, is not.
func TestOnlyAPlayableOrphanIsRecovered(t *testing.T) {
	lib := store.New(t.TempDir())
	created := time.Date(2026, 10, 24, 9, 0, 0, 0, time.Local)
	stopped, err := lib.BeginRecording(created)
	if err != nil {
		t.Fatal(err)
	}
	screen := filepath.Join(stopped.Dir, "screen.mp4")
	writeClip(t, screen)
	item, err := recoverRecording(lib, stopped)
	if err != nil {
		t.Fatal(err)
	}
	if item.Kind() != store.Recording || item.Meta.Width != 64 || item.Meta.Height != 48 || *item.Meta.DurationMs < 900 {
		t.Errorf("recovered %+v", item.Meta)
	}
	if _, err := os.Stat(item.Thumb()); err != nil {
		t.Errorf("no thumbnail: %v", err)
	}
	if items := lib.Scan(); len(items) != 1 || items[0].Dir != item.Dir {
		t.Errorf("the Library lists %+v", items)
	}

	killed, err := lib.BeginRecording(created.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	whole, err := os.ReadFile(item.Deliverable())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(killed.Dir, "screen.mp4"), whole[:len(whole)/2], 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := recoverRecording(lib, killed); err == nil {
		t.Fatal("half an MP4 was recovered")
	}
	if _, err := os.Stat(killed.Dir); err != nil {
		t.Errorf("a failed recovery moved the folder: %v", err)
	}
}
