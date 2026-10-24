//go:build !windows

package app

import (
	"log"
	"os/exec"
)

// reveal selects paths in Finder: open -R selects every path it is given, as the Rust build
// did, where NSWorkspace through MyGo selects one.
func reveal(paths []string) {
	if err := exec.Command("open", append([]string{"-R"}, paths...)...).Start(); err != nil {
		log.Printf("app: reveal: %v", err)
	}
}
