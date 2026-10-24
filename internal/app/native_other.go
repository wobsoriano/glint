//go:build !windows

package app

import "time"

func foregroundWindow() int64 { return 0 }

func doubleClickTime() time.Duration { return 500 * time.Millisecond }

// AllowForeground needs nothing outside Windows: macOS activates the app that opens a file.
func AllowForeground() {}

// excludeOwnWindows is not implemented outside Windows yet.
func excludeOwnWindows(include bool) {}
