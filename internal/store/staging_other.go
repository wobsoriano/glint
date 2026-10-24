//go:build !windows

package store

import (
	"io/fs"
	"time"
)

// inUse reports whether the file changed within fresh. Without Windows' sharing modes an
// open file cannot be told from a closed one, and Glint records only on Windows; this
// keeps a recording the Rust build is making on a Mac out of reach.
func inUse(_ string, info fs.FileInfo, now time.Time) bool {
	return now.Sub(info.ModTime()) < fresh
}
