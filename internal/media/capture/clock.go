// Package capture records a session's audio tracks and owns the media clock every track
// shares. Times are media time: 0 is the first recorded frame and paused spans are removed.
package capture

import (
	"sync"
	"time"
)

type clockState uint8

const (
	clockPaused clockState = iota
	clockRunning
	// clockEnded means the master track ended at elapsed; nothing resumes an ended clock.
	clockEnded
)

// Clock is the media clock. It starts paused at zero.
type Clock struct {
	mu      sync.Mutex
	state   clockState
	elapsed time.Duration // media time up to since, or the frozen time when not running
	since   time.Time
}

func NewClock() *Clock { return &Clock{} }

// Now is the media time elapsed so far, excluding paused spans.
func (c *Clock) Now() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == clockRunning {
		return c.elapsed + time.Since(c.since)
	}
	return c.elapsed
}

func (c *Clock) NowMS() uint64 { return uint64(c.Now().Milliseconds()) }

// IsPaused is true whenever media time stands still, so tracks stop writing.
func (c *Clock) IsPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state != clockRunning
}

// EndAt ends media time at elapsed, which may lie before now when the master track's last
// frame is already behind the wall clock. Every track stops writing there, and Resume no
// longer does anything.
func (c *Clock) EndAt(elapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state, c.elapsed = clockEnded, elapsed
}

// Pause freezes media time. Pausing a paused clock is a no-op.
func (c *Clock) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == clockRunning {
		c.state, c.elapsed = clockPaused, c.elapsed+time.Since(c.since)
	}
}

// Resume lets media time run again. Resuming a running clock is a no-op.
func (c *Clock) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == clockPaused {
		c.state, c.since = clockRunning, time.Now()
	}
}
