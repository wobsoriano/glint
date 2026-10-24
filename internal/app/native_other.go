//go:build !windows

package app

import "time"

func foregroundWindow() int64 { return 0 }

func doubleClickTime() time.Duration { return 500 * time.Millisecond }

// excludeOwnWindows is not implemented outside Windows yet.
func excludeOwnWindows(include bool) {}
