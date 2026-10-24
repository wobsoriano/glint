package playback

import (
	"io"
	"math"
	"slices"
	"testing"
	"time"
)

func TestResamplerTurnsChunksInto48kStereo(t *testing.T) {
	mono := NewResampler(24_000, 1)
	out := make([]float32, 12)
	var got []float32
	mono.Push([]float32{0, 1})
	got = append(got, out[:mono.Pull(out, false)]...)
	mono.Push([]float32{0})
	got = append(got, out[:mono.Pull(out, false)]...)
	got = append(got, out[:mono.Pull(out, true)]...)
	want := []float32{0, 0, 0.5, 0.5, 1, 1, 0.5, 0.5, 0, 0, 0, 0}
	if !slices.Equal(got, want) {
		t.Fatalf("24 kHz mono came out as %v, want %v", got, want)
	}
	if n := mono.Pull(out, true); n != 0 {
		t.Fatalf("an empty resampler wrote %d samples", n)
	}

	quad := NewResampler(48_000, 4)
	quad.Push([]float32{0.1, 0.2, 0.3, 0.4})
	if n := quad.Pull(out, true); !slices.Equal(out[:n], []float32{0.1, 0.2}) {
		t.Fatalf("four channels kept as %v, want the first two", out[:n])
	}
}

// ramp is a mono Source whose sample i is i, so positions are visible in the output.
type ramp struct {
	rate, at, frames int
}

func (r *ramp) SampleRate() int { return r.rate }
func (r *ramp) Channels() int   { return 1 }
func (r *ramp) SeekMS(ms int64) error {
	r.at = int(ms) * r.rate / 1000
	return nil
}

func (r *ramp) NextChunk(dst []float32) ([]float32, error) {
	if r.at >= r.frames {
		return dst, io.EOF
	}
	n := min(1000, r.frames-r.at)
	for i := range n {
		dst = append(dst, float32(r.at+i))
	}
	r.at += n
	return dst, nil
}

func TestSoundtrackQueuesHalfASecondAndSeeksDropTheRest(t *testing.T) {
	src := &ramp{rate: SampleRate, frames: 5 * SampleRate}
	s := NewSoundtrack(src)
	if err := s.SeekMS(1000); err != nil {
		t.Fatal(err)
	}
	if q := s.queued(); q < queueAhead || q > queueAhead+4096 {
		t.Fatalf("queued %d samples after a seek, want about %d", q, queueAhead)
	}
	out := make([]float32, 8)
	s.Fill(out)
	if out[0] != SampleRate || out[1] != SampleRate || out[2] != SampleRate+1 {
		t.Fatalf("the first frames after seeking to 1 s are %v", out[:4])
	}
	if err := s.SeekMS(4900); err != nil {
		t.Fatal(err)
	}
	s.Fill(out)
	if out[0] != float32(4900*SampleRate/1000) {
		t.Fatalf("after a second seek the queue starts at %v", out[0])
	}
	big := make([]float32, SampleRate*Channels)
	s.Fill(big)
	frames := 100 * SampleRate / 1000
	if last := big[(frames-4)*2-2]; last != float32(5*SampleRate-1) {
		t.Fatalf("the track ends on %v, want its last sample", last)
	}
	if big[frames*2+10] != 0 {
		t.Fatal("past the end of the track Fill writes silence")
	}
}

func TestWallClockRunsFromItsStart(t *testing.T) {
	c := NewClock(2000, nil)
	time.Sleep(50 * time.Millisecond)
	if now := c.NowMS(); now < 2040 || now > 2150 || c.FollowsAudio() {
		t.Fatalf("a wall clock from 2000 ms reads %.1f ms after 50 ms", now)
	}
}

func TestAudioClockHoldsAtZeroUntilTheDevicePlays(t *testing.T) {
	o := &AudioOutput{readAt: time.Now()}
	if o.PlayedMS() != 0 {
		t.Fatal("no device position yet should read 0")
	}
	o.setPosition(100)
	time.Sleep(60 * time.Millisecond)
	if ms := o.PlayedMS(); ms < 100 || ms > 120.5 {
		t.Fatalf("interpolation runs past one missed read: %.1f", ms)
	}
	o.fail(io.ErrUnexpectedEOF)
	time.Sleep(30 * time.Millisecond)
	if ms := o.PlayedMS(); ms < 125 || math.IsNaN(ms) {
		t.Fatalf("a failed stream keeps the clock going on the wall clock: %.1f", ms)
	}
}
