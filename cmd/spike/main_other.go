//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "the spike prototypes run on Windows")
	os.Exit(2)
}
