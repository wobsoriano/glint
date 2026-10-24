package library

import (
	"fmt"
	"time"
)

// date is how the Library writes a time: "Oct 17, 2026 at 6:59 AM".
func date(t time.Time) string { return t.Format("Jan 2, 2006 at 3:04 PM") }

// duration is a recording's length as minutes and seconds: "1:05".
func duration(ms uint64) string { return fmt.Sprintf("%d:%02d", ms/60_000, (ms/1000)%60) }

// humanBytes matches the Rust build's actions::human_bytes.
func humanBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}
