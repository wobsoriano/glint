package store

import (
	"errors"
	"io/fs"
	"time"

	"golang.org/x/sys/windows"
)

// inUse reports whether another handle has the file open, as a recorder's writers keep
// theirs until the recording stops: opening it with no sharing then fails. A file that
// cannot be opened for another reason counts as in use too, since it could not be moved.
func inUse(path string, _ fs.FileInfo, _ time.Time) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return true
	}
	h, err := windows.CreateFile(p, windows.GENERIC_READ, 0, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return !errors.Is(err, windows.ERROR_FILE_NOT_FOUND)
	}
	windows.CloseHandle(h)
	return false
}
