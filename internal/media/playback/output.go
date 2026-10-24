// Package playback plays audio on the default output device and keeps the clock that video
// follows while it plays: Trim & Compress and Studio show the frame the sound has reached.
// On Windows the output is WASAPI in shared mode; other platforms return
// errors.ErrUnsupported for now, and the clock then runs on the wall clock.
package playback

import (
	"sync"
	"time"
)

// SampleRate and Channels are the format every AudioOutput plays: interleaved float32 PCM.
const (
	SampleRate = 48_000
	Channels   = 2
)

// AudioOutput is a running output stream. fill is called on the stream's own goroutine with
// a buffer of interleaved samples to write in full; write zeros for silence.
type AudioOutput struct {
	stop chan struct{}
	done chan struct{}
	once sync.Once

	mu       sync.Mutex
	playedMS float64
	readAt   time.Time
	err      error
}

// StartAudioOutput opens the default output device and starts playing what fill writes. It
// returns once the device is open and the first buffer is queued.
func StartAudioOutput(fill func(out []float32)) (*AudioOutput, error) {
	o := &AudioOutput{stop: make(chan struct{}), done: make(chan struct{}), readAt: time.Now()}
	if err := o.start(fill); err != nil {
		return nil, err
	}
	return o, nil
}

// PlayedMS is how much audio the device has played, in milliseconds. It stays at 0 until the
// first samples reach the device and stops once the output is closed. If the stream fails it
// keeps going on the wall clock, so video following it does not freeze.
func (o *AudioOutput) PlayedMS() float64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	since := float64(time.Since(o.readAt).Microseconds()) / 1000
	switch {
	case o.err != nil:
		return o.playedMS + since
	case o.playedMS == 0:
		return 0
	}
	// The device position is read every few milliseconds; between reads it advances with
	// the wall clock, but never past one missed read, so a stalled device stops the clock.
	return o.playedMS + min(since, 20)
}

// Err is the error that stopped the stream, or nil while it plays.
func (o *AudioOutput) Err() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.err
}

func (o *AudioOutput) setPosition(ms float64) {
	o.mu.Lock()
	o.playedMS, o.readAt = ms, time.Now()
	o.mu.Unlock()
}

func (o *AudioOutput) fail(err error) {
	o.mu.Lock()
	o.err, o.readAt = err, time.Now()
	o.mu.Unlock()
}

// Close stops playback at once and waits for the stream to shut down. It may be called more
// than once.
func (o *AudioOutput) Close() {
	o.once.Do(func() { close(o.stop) })
	<-o.done
}

// Clock is the media time of one run of playback: where it started, plus what the audio
// device has played since, or plus the wall clock when no audio plays.
type Clock struct {
	startMS float64
	out     *AudioOutput
	began   time.Time
}

// NewClock starts a clock at startMS that follows out, or the wall clock when out is nil.
func NewClock(startMS int64, out *AudioOutput) Clock {
	return Clock{startMS: float64(startMS), out: out, began: time.Now()}
}

// NowMS is the media time the listener hears now.
func (c Clock) NowMS() float64 {
	if c.out != nil {
		return c.startMS + c.out.PlayedMS()
	}
	return c.startMS + float64(time.Since(c.began).Microseconds())/1000
}

// FollowsAudio reports whether the clock runs on an audio device.
func (c Clock) FollowsAudio() bool { return c.out != nil }
