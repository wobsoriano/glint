// Package capture records a session's audio tracks and owns the media clock every track
// shares. Times are media time: 0 is the first recorded frame and paused spans are removed.
package capture

import (
	"math"
	"sync"
	"time"
)

// Instant is a moment on the performance counter in 100 ns units, the basis of the
// timestamps WASAPI and Windows.Graphics.Capture put on what they deliver. Tracks place
// what they record by those timestamps through Clock.At, not by when it reached them.
type Instant int64

const forever = Instant(math.MaxInt64)

func (q Instant) sub(r Instant) time.Duration { return time.Duration(q-r) * 100 }

// span is one stretch of running media time: instants [from, to) map to media time
// starting at media.
type span struct {
	from, to Instant
	media    time.Duration
}

func (s span) end() time.Duration { return s.media + s.to.sub(s.from) }

// Clock is the media clock. It starts paused at zero.
type Clock struct {
	mu      sync.Mutex
	spans   []span
	running bool
	// ended means the master track ended; nothing resumes an ended clock.
	ended bool
}

func NewClock() *Clock { return &Clock{} }

// Now is the media time elapsed so far, excluding paused spans.
func (c *Clock) Now() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		at, _, _ := c.locate(Now())
		return at
	}
	return c.frozen()
}

func (c *Clock) NowMS() uint64 { return uint64(c.Now().Milliseconds()) }

func (c *Clock) frozen() time.Duration {
	if len(c.spans) == 0 {
		return 0
	}
	return c.spans[len(c.spans)-1].end()
}

// At is the media time of instant q. ok is false when q lies before the start, in a pause or
// after the end; at is then the media time standing still there.
func (c *Clock) At(q Instant) (at time.Duration, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	at, ok, _ = c.locate(q)
	return at, ok
}

// locate is At, plus the next instant after q at which media time starts or stops running,
// forever when none is known yet.
func (c *Clock) locate(q Instant) (at time.Duration, ok bool, next Instant) {
	for _, s := range c.spans {
		if q < s.from {
			return s.media, false, s.from
		}
		if q < s.to {
			return s.media + q.sub(s.from), true, s.to
		}
	}
	return c.frozen(), false, forever
}

// IsPaused is true whenever media time stands still.
func (c *Clock) IsPaused() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.running
}

// EndAt ends media time at elapsed, which may lie before now when the master track's last
// frame is already behind the wall clock. Every track stops writing there, and Resume no
// longer does anything.
func (c *Clock) EndAt(elapsed time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running, c.ended = false, true
	for i, s := range c.spans {
		if s.to == forever || i == len(c.spans)-1 || elapsed <= s.end() {
			s.to = s.from + Instant(max(elapsed-s.media, 0)/100)
			c.spans = append(c.spans[:i], s)
			return
		}
	}
}

// Pause freezes media time. Pausing a paused clock is a no-op.
func (c *Clock) Pause() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running {
		c.running = false
		c.spans[len(c.spans)-1].to = Now()
	}
}

// Resume lets media time run again. Resuming a running clock is a no-op.
func (c *Clock) Resume() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.running && !c.ended {
		c.running = true
		c.spans = append(c.spans, span{from: Now(), to: forever, media: c.frozen()})
	}
}
