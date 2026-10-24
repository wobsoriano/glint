//go:build !windows

package capture

import "time"

var start = time.Now()

// Now is the monotonic clock now, standing in for the performance counter.
func Now() Instant { return Instant(time.Since(start) / 100) }
