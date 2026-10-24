//go:build !windows

// Command record records and checks a session package; it runs on Windows only for now.
package main

import "log"

func main() { log.Fatal("record: Windows only for now") }
