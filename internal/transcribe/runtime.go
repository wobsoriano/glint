package transcribe

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// The Rust build links whisper.cpp 1.8.3 through whisper-rs 0.16, so the Go build loads the
// same release's prebuilt libraries to decode identically.
const whisperVersion = "v1.8.3"

// runtimeAsset is the release archive holding the whisper.cpp libraries for this platform,
// or nil where none is published.
type runtimeAsset struct {
	url    string
	size   uint64
	sha256 string
	// files are the libraries to keep, by their path inside the archive.
	files []runtimeFile
}

type runtimeFile struct {
	zipPath, name string
	size          uint64
	sha256        string
}

// RuntimeDir is where the whisper.cpp libraries live, beside the models they run.
func RuntimeDir(modelsDir string) string {
	return filepath.Join(modelsDir, "whisper.cpp-"+whisperVersion)
}

func runtimeInstalled(modelsDir string) bool {
	if whisperRuntime == nil {
		return false
	}
	for _, f := range whisperRuntime.files {
		fi, err := os.Stat(filepath.Join(RuntimeDir(modelsDir), f.name))
		if err != nil || uint64(fi.Size()) != f.size {
			return false
		}
	}
	return true
}

func runtimeDownloadSize(modelsDir string) uint64 {
	if whisperRuntime == nil || runtimeInstalled(modelsDir) {
		return 0
	}
	return whisperRuntime.size
}

// installRuntime downloads the release archive, checks its pinned hash, and unpacks the
// libraries through a temporary folder so a half-written runtime never looks installed.
func installRuntime(ctx context.Context, modelsDir string, progress func(done uint64)) error {
	if whisperRuntime == nil {
		return errors.ErrUnsupported
	}
	dir := RuntimeDir(modelsDir)
	archive := dir + ".zip.part"
	defer os.Remove(archive)
	if err := fetch(ctx, whisperRuntime.url, archive, whisperRuntime.size, stall, progress); err != nil {
		return err
	}
	if err := checkHash(archive, whisperRuntime.sha256); err != nil {
		return err
	}
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("opening %s: %w", whisperRuntime.url, err)
	}
	defer zr.Close()
	tmp := dir + ".tmp"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	for _, f := range whisperRuntime.files {
		dest := filepath.Join(tmp, f.name)
		if err := unzipFile(&zr.Reader, f.zipPath, dest); err != nil {
			return err
		}
		if err := checkHash(dest, f.sha256); err != nil {
			return err
		}
	}
	os.RemoveAll(dir)
	return os.Rename(tmp, dir)
}

func unzipFile(zr *zip.Reader, name, dest string) error {
	src, err := zr.Open(name)
	if err != nil {
		return fmt.Errorf("whisper.cpp archive: %w", err)
	}
	defer src.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func checkHash(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%s has SHA-256 %s, expected %s", filepath.Base(path), got, want)
	}
	return nil
}

// verifyRuntime checks every library against its pinned hash before it is loaded into the
// process.
func verifyRuntime(dir string) error {
	if whisperRuntime == nil {
		return errors.ErrUnsupported
	}
	for _, f := range whisperRuntime.files {
		if err := checkHash(filepath.Join(dir, f.name), f.sha256); err != nil {
			return fmt.Errorf("whisper.cpp runtime: %w", err)
		}
	}
	return nil
}
