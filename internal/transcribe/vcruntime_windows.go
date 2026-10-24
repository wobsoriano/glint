package transcribe

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// whisper.cpp's release libraries link the Visual C++ runtime as DLLs, which Windows does not
// ship. Microsoft lets only licensed Visual Studio users redistribute those DLLs and recommends
// its installer over copies beside an app, which Windows Update cannot service, so Glint asks
// the user to install the Redistributable rather than shipping the DLLs.
var vcRuntimeLibraries = []string{"vcruntime140.dll", "vcruntime140_1.dll", "msvcp140.dll", "vcomp140.dll"}

// VCRedistURL is Microsoft's permanent link to the latest Visual C++ Redistributable for x64.
const VCRedistURL = "https://aka.ms/vc14/vc_redist.x64.exe"

// ErrVCRuntime is the error transcription fails with when the Visual C++ Redistributable is
// missing or too old for whisper.cpp.
var ErrVCRuntime = errors.New("the Microsoft Visual C++ Redistributable (x64) is needed")

// missingLibraries returns the libraries that neither the system's search nor dir provides.
func missingLibraries(names []string, dir string) []string {
	var missing []string
	for _, name := range names {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			continue
		}
		h, err := windows.LoadLibraryEx(name, 0, windows.LOAD_LIBRARY_SEARCH_DEFAULT_DIRS)
		if err != nil {
			missing = append(missing, name)
			continue
		}
		windows.FreeLibrary(h)
	}
	return missing
}

// checkVCRuntime fails with ErrVCRuntime, saying what to install, when whisper.cpp in dir
// could not find the runtime it links.
func checkVCRuntime(dir string) error {
	if missing := missingLibraries(vcRuntimeLibraries, dir); len(missing) > 0 {
		return fmt.Errorf("transcription needs the Microsoft Visual C++ Redistributable (x64), and %s is missing. Install it from %s, then try again: %w",
			strings.Join(missing, ", "), VCRedistURL, ErrVCRuntime)
	}
	return nil
}

// explainLoadError names the Redistributable when whisper.dll failed to load because the
// installed runtime lacks a function it needs, which an older Redistributable does.
func explainLoadError(err error) error {
	if errors.Is(err, windows.ERROR_PROC_NOT_FOUND) {
		return fmt.Errorf("loading whisper.dll: the Microsoft Visual C++ Redistributable (x64) is older than whisper.cpp needs. Install the latest from %s, then try again: %w",
			VCRedistURL, ErrVCRuntime)
	}
	return fmt.Errorf("loading whisper.dll: %w", err)
}
