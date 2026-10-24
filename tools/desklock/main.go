// Command desklock runs a command while holding the desktop: only one holder at a time may
// move the mouse, type or put windows in front. Workers that drive the desktop wrap each burst:
//
//	go run ./tools/desklock -- powershell -File drive.ps1
//
// The lock is a file whose content is the holder's pid. A lock whose pid is gone is taken over.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func lockPath() string {
	if p := os.Getenv("GLINT_DESKLOCK"); p != "" {
		return p
	}
	return filepath.Join(os.TempDir(), "glint-desktop.lock")
}

func alive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return processAlive(p)
}

func acquire(path string, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_, err = f.WriteString(strconv.Itoa(os.Getpid()))
			f.Close()
			return err
		}
		if !errors.Is(err, os.ErrExist) {
			return err
		}
		b, _ := os.ReadFile(path)
		if pid, perr := strconv.Atoi(strings.TrimSpace(string(b))); perr == nil && !alive(pid) {
			os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("desktop still held by pid %s after %s", strings.TrimSpace(string(b)), wait)
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func main() {
	args := os.Args[1:]
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: desklock -- <command> [args...]")
		os.Exit(2)
	}
	path := lockPath()
	if err := acquire(path, 30*time.Minute); err != nil {
		fmt.Fprintln(os.Stderr, "desklock:", err)
		os.Exit(1)
	}
	defer os.Remove(path)
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	switch {
	case errors.As(err, &exit):
		os.Remove(path)
		os.Exit(exit.ExitCode())
	case err != nil:
		fmt.Fprintln(os.Stderr, "desklock:", err)
		os.Remove(path)
		os.Exit(1)
	}
}
