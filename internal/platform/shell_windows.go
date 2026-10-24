package platform

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// ExcludeFromCapture works only on windows this process owns.
func ExcludeFromCapture(handle uintptr, exclude bool) error {
	affinity := uintptr(wdaNone)
	if exclude {
		affinity = wdaExcludeFromCapture
	}
	_, err := call(procSetWindowDisplayAffinity, handle, affinity)
	return err
}

func Wallpaper() (string, error) {
	buf := make([]uint16, 520)
	if _, err := call(procSystemParametersInfoW, spiGetDeskWallpaper, uintptr(len(buf)), uintptr(unsafe.Pointer(&buf[0])), 0); err != nil {
		return "", err
	}
	path := windows.UTF16ToString(buf)
	if path == "" {
		return "", fmt.Errorf("platform: no wallpaper file: %w", fs.ErrNotExist)
	}
	if st, err := os.Stat(path); err != nil {
		return "", err
	} else if st.IsDir() {
		return "", fmt.Errorf("platform: wallpaper %s: %w", path, fs.ErrNotExist)
	}
	return path, nil
}

const (
	runKey   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValue = "Glint" // the Rust build's name, so an upgraded install stays registered
)

func SetLaunchAtLogin(enabled bool, args []string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !enabled {
		if err := k.DeleteValue(runValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValue, runCommand(exe, args))
}

// runCommand quotes the program as Rust does ("C:\...\Glint.exe" --background) and each
// argument only when it needs it.
func runCommand(exe string, args []string) string {
	parts := []string{`"` + exe + `"`}
	for _, a := range args {
		parts = append(parts, windows.EscapeArg(a))
	}
	return strings.Join(parts, " ")
}
