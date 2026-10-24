package toast

import (
	"errors"
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
)

// Reason is err as a toast or a card shows it: the cause first, then the base name of the
// file it concerns. Errors carry full paths for the log, and a path at the front of a
// one-line message pushes the cause out of sight.
func Reason(err error) string {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return strings.TrimSuffix(pe.Err.Error(), ".") + " (" + filepath.Base(pe.Path) + ")"
	}
	return dirs.ReplaceAllString(err.Error(), "$1")
}

// dirs matches the folder part of an absolute path in a message, so removing it leaves the
// file's base name. A Windows folder ends at the last separator before a character no path
// may hold, so a name with spaces is kept whole. Group 1 is the space before a Unix path.
var dirs = regexp.MustCompile(`(?:[A-Za-z]:|\\\\[^\\:*?"<>|\r\n]+)\\(?:[^:*?"<>|\r\n]*\\)?|(^|\s)/[^:\r\n]*/`)
