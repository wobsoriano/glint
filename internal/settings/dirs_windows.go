package settings

import (
	"os"

	"golang.org/x/sys/windows"
)

// configDir and picturesDir ask the shell for the known folders, as Rust's dirs crate does, so
// a Pictures folder redirected to OneDrive or another drive is honored.
func configDir() string {
	if d, err := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, 0); err == nil {
		return d
	}
	return "."
}

func picturesDir() string {
	if d, err := windows.KnownFolderPath(windows.FOLDERID_Pictures, 0); err == nil {
		return d
	}
	if d, err := os.UserHomeDir(); err == nil {
		return d
	}
	return "."
}
