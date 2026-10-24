package app

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// The Rust build registered "Open with Glint" itself, on every start, under these ProgIDs and
// under Applications\glint.exe. The installer now registers the same file types under
// com.glint.app.<ext>, and removes them again on uninstall. Left in place, the old entries
// would list a second Glint under "Open with" that starts the old build.
var (
	rustProgIDs       = []string{"Glint.Image", "Glint.Video"}
	rustOpenWithTypes = []string{".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif", ".mp4", ".mov", ".m4v"}
)

var (
	procRegDeleteTree  = windows.NewLazySystemDLL("advapi32.dll").NewProc("RegDeleteTreeW")
	procSHChangeNotify = windows.NewLazySystemDLL("shell32.dll").NewProc("SHChangeNotify")
)

// removeRustOpenWith deletes the Rust build's registrations from the classes key at classes,
// a path under HKEY_CURRENT_USER, and returns what it deleted. Applications\glint.exe stays
// when its command starts exe, which Windows writes when the user picks this Glint.exe in
// "Choose another app".
func removeRustOpenWith(classes, exe string) []string {
	var removed []string
	for _, id := range rustProgIDs {
		if deleteTree(classes + `\` + id) {
			removed = append(removed, classes+`\`+id)
		}
	}
	app := classes + `\Applications\glint.exe`
	if cmd, ok := defaultValue(app + `\shell\open\command`); ok && !commandStarts(cmd, exe) && deleteTree(app) {
		removed = append(removed, app)
	}
	for _, ext := range rustOpenWithTypes {
		path := classes + `\` + ext + `\OpenWithProgids`
		k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.SET_VALUE)
		if err != nil {
			continue
		}
		for _, id := range rustProgIDs {
			if k.DeleteValue(id) == nil {
				removed = append(removed, path+`\`+id)
			}
		}
		k.Close()
	}
	return removed
}

// deleteTree deletes a key of HKEY_CURRENT_USER with its subkeys, and reports whether it did.
func deleteTree(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	r, _, _ := procRegDeleteTree.Call(uintptr(registry.CURRENT_USER), uintptr(unsafe.Pointer(p)))
	return r == 0
}

func defaultValue(path string) (string, bool) {
	k, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		return "", false
	}
	defer k.Close()
	v, _, err := k.GetStringValue("")
	return v, err == nil
}

// commandStarts reports whether an open command, as `"C:\path\Glint.exe" "%1"`, runs exe.
func commandStarts(cmd, exe string) bool {
	quoted, ok := strings.CutPrefix(cmd, `"`)
	if !ok {
		return false
	}
	path, _, ok := strings.Cut(quoted, `"`)
	return ok && strings.EqualFold(path, exe)
}

// installedAt reports whether the installer registered this exe as the open command of
// Glint's own PNG type. IsPackaged alone is true for any built Glint.exe, installed or not.
func installedAt(classes, exe string) bool {
	cmd, ok := defaultValue(classes + `\com.glint.app.png\shell\open\command`)
	return ok && commandStarts(cmd, exe)
}

// forgetRustOpenWith removes the Rust build's "Open with" entries once this Glint runs
// installed, and tells Explorer the associations changed.
func forgetRustOpenWith() {
	exe, err := os.Executable()
	if err != nil || !installedAt(`Software\Classes`, exe) {
		return
	}
	if len(removeRustOpenWith(`Software\Classes`, exe)) > 0 {
		const shcneAssocChanged = 0x08000000
		procSHChangeNotify.Call(shcneAssocChanged, 0, 0, 0)
	}
}
