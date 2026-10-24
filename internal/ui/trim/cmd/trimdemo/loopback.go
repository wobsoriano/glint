package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/ui/trim/internal/testclip"
)

// clicks records, by the wall clock, when the system mix plays each of the test clip's
// clicks. Paired with when each whole-second frame reached the preview, it measures audio and
// video sync end to end, as a listener would meet it.
type clicks struct {
	mu    sync.Mutex
	at    []time.Time
	track *capture.Track
	dir   string
}

func startClicks() (*clicks, error) {
	dir, err := os.MkdirTemp("", "trimdemo-loopback")
	if err != nil {
		return nil, err
	}
	tap := make(chan []float32, 512)
	clock := capture.NewClock()
	track, err := capture.StartTrack(capture.Loopback, filepath.Join(dir, "loopback.wav"), 2, clock, tap)
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	clock.Resume()
	c := &clicks{track: track, dir: dir}
	go c.listen(tap)
	return c, nil
}

// listen dates each click from when its buffer arrived, less the samples after it. A click is
// the first sample louder than the tone can be, half a second after the last click.
func (c *clicks) listen(tap <-chan []float32) {
	var quietUntil time.Time
	for buf := range tap {
		arrived := time.Now()
		n := len(buf) / 2
		for i := range n {
			if math.Abs(float64(buf[2*i]+buf[2*i+1])/2) < 0.5 {
				continue
			}
			t := arrived.Add(-time.Duration(n-i) * time.Second / testclip.SampleRate)
			if t.After(quietUntil) {
				c.mu.Lock()
				c.at = append(c.at, t)
				c.mu.Unlock()
				quietUntil = t.Add(500 * time.Millisecond)
			}
		}
	}
}

// report pairs each whole-second frame with the nearest click and prints how much later the
// click was heard than the frame was shown.
func (c *clicks) report(frames map[int64]time.Time) {
	c.mu.Lock()
	at := slices.Clone(c.at)
	c.mu.Unlock()
	var offsets []float64
	for ms, shown := range frames {
		best := math.Inf(1)
		for _, t := range at {
			if d := float64(t.Sub(shown).Microseconds()) / 1000; math.Abs(d) < math.Abs(best) {
				best = d
			}
		}
		if math.Abs(best) < 300 {
			offsets = append(offsets, best)
		} else {
			fmt.Printf("no click within 300 ms of the frame at %d ms\n", ms)
		}
	}
	if len(offsets) == 0 {
		fmt.Printf("loopback: %d clicks heard, none near the %d whole-second frames\n", len(at), len(frames))
		return
	}
	slices.Sort(offsets)
	var sum float64
	for _, o := range offsets {
		sum += o
	}
	fmt.Printf("loopback: %d whole-second frames paired with clicks; click heard minus frame shown: mean %.1f ms, min %.1f, max %.1f ms\n",
		len(offsets), sum/float64(len(offsets)), offsets[0], offsets[len(offsets)-1])
}

func (c *clicks) stop() {
	c.track.Stop()
	os.RemoveAll(c.dir)
}
