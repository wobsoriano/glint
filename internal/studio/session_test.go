package studio

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/transcribe"
)

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenSessionRenamesABrokenProjectAndUsesDefaults(t *testing.T) {
	dir := t.TempDir()
	if _, err := OpenSession(dir, 1920, 1080, 10_000); err == nil || !strings.Contains(err.Error(), "screen.mp4") {
		t.Fatalf("a session without screen.mp4 fails: %v", err)
	}
	writeFile(t, filepath.Join(dir, "screen.mp4"), "")
	writeFile(t, filepath.Join(dir, "mic.wav"), "")
	writeFile(t, filepath.Join(dir, ProjectFile), `{ "clips": [`)
	writeFile(t, filepath.Join(dir, input.File), `{"version":1,"size":[1920,1080],"pointer":[[0,10.0,10.0,0]],
		"clicks":[{"t":2000,"up":2100,"x":100.0,"y":100.0,"button":"left"}],"keys":[],"cursors":[]}`)
	tr := transcribe.NewTranscript("en", []transcribe.Word{
		{Text: "Hello,", StartMs: 100, EndMs: 400, Confidence: 1},
		{Text: "world.", StartMs: 450, EndMs: 900, Confidence: 1},
	})
	if err := tr.Save(filepath.Join(dir, transcribe.TranscriptFile)); err != nil {
		t.Fatal(err)
	}

	s, err := OpenSession(dir, 1920, 1080, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(s.Notices), 1)
	if _, err := os.Stat(filepath.Join(dir, ProjectFile+".broken")); err != nil {
		t.Fatalf("broken project kept for the user: %v", err)
	}
	eq(t, s.Project.Clips, []Clip{{0, 10_000, 1}})
	eq(t, len(s.Project.Zooms), 1)
	eq(t, s.Project.Zooms[0].StartMs, 1_700.0)
	eq(t, s.Project.CaptionCues, []CaptionCue{{StartMs: 100, EndMs: 900, Text: "Hello, world."}})
	eq(t, s.Captions[1], CaptionWord{Text: "world.", StartMs: 450, EndMs: 900})
	eq(t, s.Tracks.Mic, filepath.Join(dir, "mic.wav"))
	eq(t, s.Tracks.Camera, "")

	s.Project.SetSpeed(0, 2)
	if err := s.SaveProject(); err != nil {
		t.Fatal(err)
	}
	again, err := OpenSession(dir, 1920, 1080, 10_000)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, len(again.Notices), 0)
	eq(t, again.Project.DurationMs(), 5_000.0)
}

func TestOpenSessionWithoutInputSizesItFromTheScreen(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "screen.mp4"), "")
	s, err := OpenSession(dir, 1280, 720, 3_000)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, s.Input.Size, [2]uint32{1280, 720})
	eq(t, len(s.Project.Zooms), 0)
	eq(t, s.Transcript == nil, true)
}
