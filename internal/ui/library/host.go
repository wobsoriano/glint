// Package library is the Library window: every capture in the Library, filtered, searched,
// sorted and shown as a grid or a list, with an inspector, multi-select and the actions that
// act on the selection.
package library

import (
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// Host is what the Library window needs from the app. Every method runs on the main thread.
type Host interface {
	// Settings is the app's one settings value. The window changes Library (layout, sort,
	// inspector) in place and then calls SettingsChanged, which saves and applies it.
	Settings() *settings.Settings
	SettingsChanged()

	// QuickLook shows a capture in Quick Look; CloseQuickLook closes it and reports whether
	// it was open, so Space toggles it as in the Rust build.
	QuickLook(item store.Item)
	CloseQuickLook() bool
	// Edit opens a screenshot in the editor and a recording in Studio.
	Edit(item store.Item)
	// Copy puts the items on the clipboard: one screenshot as its pixels, one recording
	// as its video file, several as their files.
	Copy(items []store.Item)
	// Export writes the screenshots among items into dir, in the export format of the
	// settings, and reports the outcome to the user.
	Export(items []store.Item, dir string)
	// Reveal shows the capture directories in Explorer or Finder.
	Reveal(items []store.Item)
	// Trash moves the capture directories to the Recycle Bin or the Trash. When it fails,
	// the window keeps listing the items that are still on disk.
	Trash(items []store.Item) error

	// Capture starts a capture as its global shortcut does: HotkeyArea, HotkeyWindow,
	// HotkeyFullscreen or HotkeyText.
	Capture(a settings.HotkeyAction)
	// OpenSettings shows the Settings window.
	OpenSettings()
	// OpenFiles opens files dropped on the window: images in the editor, videos in the
	// trim view.
	OpenFiles(paths []string)
}
