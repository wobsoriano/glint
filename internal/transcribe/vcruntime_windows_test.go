package transcribe

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestMissingLibrariesFindsOnlyWhatNothingProvides(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "glint-beside-whisper.dll"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got := missingLibraries([]string{"kernel32.dll", "glint-beside-whisper.dll", "glint-nowhere.dll"}, dir)
	if !slices.Equal(got, []string{"glint-nowhere.dll"}) {
		t.Fatalf("missing %q, want only glint-nowhere.dll", got)
	}
}

func TestMissingRuntimeSaysWhatToInstall(t *testing.T) {
	saved := vcRuntimeLibraries
	t.Cleanup(func() { vcRuntimeLibraries = saved })
	vcRuntimeLibraries = append(slices.Clone(saved), "glint-nowhere.dll")
	err := checkVCRuntime(t.TempDir())
	if !errors.Is(err, ErrVCRuntime) || !strings.Contains(err.Error(), "glint-nowhere.dll is missing") || !strings.Contains(err.Error(), VCRedistURL) {
		t.Fatalf("checkVCRuntime: %v", err)
	}
	if err := explainLoadError(windows.ERROR_PROC_NOT_FOUND); !errors.Is(err, ErrVCRuntime) || !strings.Contains(err.Error(), VCRedistURL) {
		t.Fatalf("explainLoadError: %v", err)
	}
	if err := explainLoadError(windows.ERROR_MOD_NOT_FOUND); errors.Is(err, ErrVCRuntime) {
		t.Fatalf("a missing whisper.dll blamed on the runtime: %v", err)
	}
}

// This machine's runtime is checked as whisper.cpp v1.8.3 links it, so a check that rejects
// an installed Redistributable fails here.
func TestInstalledRuntimeSatisfiesWhisper(t *testing.T) {
	if missing := missingLibraries(vcRuntimeLibraries, t.TempDir()); len(missing) > 0 {
		t.Skipf("this machine lacks the Visual C++ Redistributable: %q", missing)
	}
	if err := checkVCRuntime(t.TempDir()); err != nil {
		t.Fatal(err)
	}
}
