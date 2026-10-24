package store

// Trash moves capture directories to the Recycle Bin on Windows or the Trash on macOS, where
// the user can restore them. It refuses rather than delete anything permanently. When it fails
// partway some directories may already be gone; DropTrashed tells which.
func Trash(dirs []string) error {
	if len(dirs) == 0 {
		return nil
	}
	return trash(dirs)
}
