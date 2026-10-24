package toast

import (
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReasonLeadsWithTheCauseAndNamesTheFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Pictures", "Glint")
	missing := filepath.Join(dir, "shot 1.png")
	_, openErr := os.Open(missing)
	notFound := "The system cannot find the path specified"
	if runtime.GOOS != "windows" {
		notFound = "no such file or directory"
	}
	for _, c := range []struct {
		err  error
		want string
	}{
		{openErr, notFound + " (shot 1.png)"},
		{fmt.Errorf("reading %s: %w", missing, openErr), notFound + " (shot 1.png)"},
		{fmt.Errorf(`C:\Users\Jo Smith\Pictures\notes.json is not a readable image: %w`, image.ErrFormat), "notes.json is not a readable image: image: unknown format"},
		{errors.New(`moving \\nas\share\Glint\2026-10-24_ab12 to the Recycle Bin: Access is denied.`), "moving 2026-10-24_ab12 to the Recycle Bin: Access is denied."},
		{errors.New("reading /Users/jo/Library/Glint/a b.png: bad header"), "reading a b.png: bad header"},
		{errors.New("no text found"), "no text found"},
	} {
		got := Reason(c.err)
		if got != c.want {
			t.Errorf("Reason(%q)\n got %q\nwant %q", c.err, got, c.want)
		}
		if strings.Contains(got, dir) {
			t.Errorf("%q still holds the folder", got)
		}
	}
}
