package app

import (
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// abs is an absolute path on this platform: C:\a\b on Windows, /a/b elsewhere.
func abs(parts string) string {
	root := "/"
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	return filepath.Join(root, filepath.FromSlash(parts))
}

func TestParsesFlagsCapturesAndRelativePaths(t *testing.T) {
	cwd := abs("Users/me/Pictures")
	other := abs("x/y.jpg")
	l, err := ParseArgs([]string{"--background", "--capture", "window", "shot.png", other, "--library", "--record", "--studio", "rec.glintrec"}, cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := Launch{Background: true, Requests: []Request{
		{Kind: RequestCapture, Capture: CaptureWindow},
		{Kind: RequestOpen, Path: filepath.Join(cwd, "shot.png")},
		{Kind: RequestOpen, Path: other},
		{Kind: RequestLibrary},
		{Kind: RequestRecord},
		{Kind: RequestStudio, Path: filepath.Join(cwd, "rec.glintrec")},
	}}
	if l.Background != want.Background || !slices.Equal(l.Requests, want.Requests) {
		t.Fatalf("got %+v\nwant %+v", l, want)
	}
}

func TestRoutesVideosToTrim(t *testing.T) {
	cwd := abs("Users/me/Videos")
	other := abs("clip.MP4")
	l, err := ParseArgs([]string{"--trim", "talk.mov", other, "shot.png"}, cwd)
	if err != nil {
		t.Fatal(err)
	}
	want := []Request{
		{Kind: RequestTrim, Path: filepath.Join(cwd, "talk.mov")},
		{Kind: RequestTrim, Path: other},
		{Kind: RequestOpen, Path: filepath.Join(cwd, "shot.png")},
	}
	if !slices.Equal(l.Requests, want) {
		t.Fatalf("got %+v\nwant %+v", l.Requests, want)
	}
	if _, err := ParseArgs([]string{"--trim"}, cwd); err == nil || !strings.HasPrefix(err.Error(), "--trim needs a video file") {
		t.Fatalf("--trim alone: %v", err)
	}
}

func TestRejectsUnknownCaptureKindsAndFlags(t *testing.T) {
	cwd := abs("")
	for args, prefix := range map[string]string{
		"--capture video": `unknown capture kind "video"`,
		"--capture":       `unknown capture kind ""`,
		"--studio":        "--studio needs a session directory",
		"--nope":          "unknown option --nope",
	} {
		_, err := ParseArgs(strings.Fields(args), cwd)
		if err == nil || !strings.HasPrefix(err.Error(), prefix) {
			t.Errorf("%s: got %v, want %q", args, err, prefix)
		}
	}
}

func TestEveryCaptureKindParses(t *testing.T) {
	for name, want := range map[string]Capture{"area": CaptureArea, "window": CaptureWindow, "screen": CaptureScreen, "text": CaptureText, "previous": CapturePrevious} {
		l, err := ParseArgs([]string{"--capture", name}, abs(""))
		if err != nil || len(l.Requests) != 1 || l.Requests[0] != (Request{Kind: RequestCapture, Capture: want}) {
			t.Errorf("--capture %s: %+v %v", name, l, err)
		}
	}
}
