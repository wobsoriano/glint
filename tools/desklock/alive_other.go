//go:build !windows

package main

import (
	"os"
	"syscall"
)

func processAlive(p *os.Process) bool { return p.Signal(syscall.Signal(0)) == nil }
