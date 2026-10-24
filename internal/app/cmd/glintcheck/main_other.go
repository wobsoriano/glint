//go:build !windows

package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "glintcheck runs on Windows only")
	os.Exit(2)
}
