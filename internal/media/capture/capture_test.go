package capture

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func TestClockStartsPausedAtZero(t *testing.T) {
	c := NewClock()
	time.Sleep(20 * time.Millisecond)
	if c.Now() != 0 || !c.IsPaused() {
		t.Fatalf("now %v paused %v", c.Now(), c.IsPaused())
	}
}

func TestClockExcludesPausedSpans(t *testing.T) {
	c := NewClock()
	c.Resume()
	time.Sleep(30 * time.Millisecond)
	c.Pause()
	atPause := c.Now()
	time.Sleep(60 * time.Millisecond)
	if c.Now() != atPause {
		t.Fatal("time moved while paused")
	}
	c.Resume()
	time.Sleep(30 * time.Millisecond)
	total := c.Now()
	if total < 60*time.Millisecond || total >= 100*time.Millisecond {
		t.Fatalf("ran twice for 30ms, the 60ms pause must not leak in: %v", total)
	}
}

func TestEndedClockReadsEndTimeAndIgnoresResume(t *testing.T) {
	c := NewClock()
	c.Resume()
	time.Sleep(30 * time.Millisecond)
	c.EndAt(10 * time.Millisecond)
	if c.Now() != 10*time.Millisecond || !c.IsPaused() {
		t.Fatalf("now %v paused %v", c.Now(), c.IsPaused())
	}
	c.Resume()
	time.Sleep(10 * time.Millisecond)
	if c.Now() != 10*time.Millisecond || !c.IsPaused() {
		t.Fatalf("resume moved an ended clock: %v", c.Now())
	}
}

func TestRepeatedPauseAndResumeAreIdempotent(t *testing.T) {
	c := NewClock()
	c.Resume()
	c.Resume()
	time.Sleep(10 * time.Millisecond)
	c.Pause()
	at := c.Now()
	c.Pause()
	time.Sleep(10 * time.Millisecond)
	if c.Now() != at {
		t.Fatal("a second pause moved time")
	}
}

func TestLevelMeterReportsRMSOfLastBuffer(t *testing.T) {
	var m LevelMeter
	m.update([]float32{0.5, -0.5, 0.5, -0.5})
	if m.RMS() != 0.5 {
		t.Fatal(m.RMS())
	}
	m.update(make([]float32, 8))
	if m.RMS() != 0 {
		t.Fatal(m.RMS())
	}
	m.update(nil)
	if m.RMS() != 0 {
		t.Fatal("an empty buffer keeps the last level")
	}
}

func TestConverterMapsChannels(t *testing.T) {
	cases := []struct {
		name    string
		in, out int
		samples []float32
		want    []float32
	}{
		{"stereo averages to mono", 2, 1, []float32{0.2, 0.4, -1, 1}, []float32{0.3, 0}},
		{"mono duplicates to stereo", 1, 2, []float32{0.5, -0.5}, []float32{0.5, 0.5, -0.5, -0.5}},
		{"surround keeps front left and right", 6, 2, []float32{0.1, 0.2, 0.3, 0.4, 0.5, 0.6}, []float32{0.1, 0.2}},
	}
	for _, c := range cases {
		got := newConverter(SampleRate, c.in, c.out).push(c.samples, nil)
		if !slices.EqualFunc(got, c.want, func(a, b float32) bool { return math.Abs(float64(a-b)) < 1e-6 }) {
			t.Errorf("%s: %v", c.name, got)
		}
	}
}

func TestConverterResamplesAcrossBuffersWithoutDrift(t *testing.T) {
	c := newConverter(44_100, 1, 1)
	in := make([]float32, 4_410)
	for i := range in {
		in[i] = 0.25
	}
	var out []float32
	for range 10 {
		out = c.push(in, out)
	}
	if d := len(out) - 48_000; d < -10 || d > 10 {
		t.Fatalf("%d frames for one second", len(out))
	}
	for _, s := range out {
		if math.Abs(float64(s-0.25)) > 1e-6 {
			t.Fatalf("sample %v", s)
		}
	}
}

type wavFile struct {
	rate, channels int
	samples        []float32
	size           int64
}

// readFloatWAV parses the writer's own 46-byte float header strictly, so a header change
// fails here.
func readFloatWAV(t *testing.T, path string) wavFile {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if len(b) < wavHeaderSize || string(b[0:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || string(b[38:42]) != "data" {
		t.Fatalf("bad header % x", b[:min(len(b), wavHeaderSize)])
	}
	if le.Uint32(b[16:]) != 18 || le.Uint16(b[20:]) != 3 || le.Uint16(b[34:]) != 32 || le.Uint16(b[36:]) != 0 {
		t.Fatalf("fmt chunk % x", b[12:38])
	}
	if int(le.Uint32(b[4:])) != len(b)-8 || int(le.Uint32(b[42:])) != len(b)-wavHeaderSize {
		t.Fatalf("sizes riff=%d data=%d file=%d", le.Uint32(b[4:]), le.Uint32(b[42:]), len(b))
	}
	w := wavFile{rate: int(le.Uint32(b[24:])), channels: int(le.Uint16(b[22:])), size: int64(len(b))}
	if le.Uint16(b[32:]) != uint16(w.channels*4) || le.Uint32(b[28:]) != uint32(w.rate*w.channels*4) {
		t.Fatal("block align or byte rate wrong")
	}
	for i := wavHeaderSize; i+4 <= len(b); i += 4 {
		w.samples = append(w.samples, math.Float32frombits(le.Uint32(b[i:])))
	}
	return w
}

func TestWAVRoundTripKeepsFormatAndSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rt.wav")
	w, err := createWAV(path, 48000, 2)
	if err != nil {
		t.Fatal(err)
	}
	must(t, w.write([]float32{0.25, -0.5, 1, 0}))
	must(t, w.writeSilence(2))
	n, err := w.finish()
	must(t, err)
	if n != 46+8*4 {
		t.Fatal(n)
	}
	got := readFloatWAV(t, path)
	if got.rate != 48000 || got.channels != 2 || got.size != int64(n) ||
		!slices.Equal(got.samples, []float32{0.25, -0.5, 1, 0, 0, 0, 0, 0}) {
		t.Fatalf("%+v", got)
	}
}

func TestWAVTruncateCutsTheTailAndTheHeaderFollows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cut.wav")
	w, err := createWAV(path, 48000, 2)
	must(t, err)
	must(t, w.write([]float32{0.25, -0.5, 1, 0, 0.5, 0.5}))
	must(t, w.truncate(5))
	must(t, w.truncate(2))
	n, err := w.finish()
	must(t, err)
	if n != 46+4*4 {
		t.Fatal(n)
	}
	if got := readFloatWAV(t, path); !slices.Equal(got.samples, []float32{0.25, -0.5, 1, 0}) || got.size != int64(n) {
		t.Fatalf("%+v", got)
	}
}

// clockOf is a clock that ran over each [from, to) pair of offsets from base and is now
// paused; a final open span runs on.
func clockOf(base Instant, offsets ...time.Duration) *Clock {
	c := NewClock()
	var media time.Duration
	for i := 0; i < len(offsets); i += 2 {
		s := span{from: base + Instant(offsets[i]/100), to: forever, media: media}
		if i+1 < len(offsets) {
			s.to = base + Instant(offsets[i+1]/100)
			media += offsets[i+1] - offsets[i]
		} else {
			c.running = true
		}
		c.spans = append(c.spans, s)
	}
	return c
}

func at(base Instant, d time.Duration) Instant { return base + Instant(d/100) }

func TestClockMapsInstantsAcrossPausesAndTheEnd(t *testing.T) {
	base := Now()
	c := clockOf(base, time.Second, 2*time.Second, 3*time.Second)
	cases := []struct {
		q    time.Duration
		want time.Duration
		ok   bool
	}{
		{500 * time.Millisecond, 0, false},
		{1500 * time.Millisecond, 500 * time.Millisecond, true},
		{2500 * time.Millisecond, time.Second, false},
		{3250 * time.Millisecond, 1250 * time.Millisecond, true},
	}
	for _, tc := range cases {
		if got, ok := c.At(at(base, tc.q)); got != tc.want || ok != tc.ok {
			t.Errorf("At(+%v) = %v %v, want %v %v", tc.q, got, ok, tc.want, tc.ok)
		}
	}
	c.EndAt(1100 * time.Millisecond)
	if got, ok := c.At(at(base, 3050*time.Millisecond)); got != 1050*time.Millisecond || !ok {
		t.Errorf("before the end: %v %v", got, ok)
	}
	if got, ok := c.At(at(base, 3250*time.Millisecond)); got != 1100*time.Millisecond || ok {
		t.Errorf("after the end: %v %v", got, ok)
	}
	if c.Now() != 1100*time.Millisecond {
		t.Errorf("an ended clock reads its end: %v", c.Now())
	}
}

func filled(n int, v float32) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = v
	}
	return s
}

func clockedFile(t *testing.T, clock *Clock, writes func(w *clockedWAV)) []float32 {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clocked.wav")
	w, err := createClockedWAV(path, 1, clock)
	must(t, err)
	writes(w)
	stats, err := w.finish()
	must(t, err)
	got := readFloatWAV(t, path).samples
	if uint64(len(got)) != stats.Frames || int64(len(got)) != framesAt(clock.Now()) {
		t.Fatalf("%d frames, stats %d, media time %v", len(got), stats.Frames, clock.Now())
	}
	return got
}

func TestClockedWAVPlacesBuffersByTheirTimestamps(t *testing.T) {
	base := Now()
	got := clockedFile(t, clockOf(base, 0, time.Second), func(w *clockedWAV) {
		must(t, w.write(filled(480, 0.5), at(base, -5*time.Millisecond)))
		must(t, w.write(filled(480, 1), at(base, 100*time.Millisecond)))
		must(t, w.write(filled(480, 0.25), at(base, 111*time.Millisecond)))
	})
	if len(got) != 48_000 {
		t.Fatalf("the file ends at media time: %d frames", len(got))
	}
	if !slices.Equal(got[:240], filled(240, 0.5)) || got[240] != 0 {
		t.Error("the half buffer stamped before the start is left out and the rest starts the file")
	}
	if !slices.Equal(got[4800:5280], filled(480, 1)) || got[4799] != 0 {
		t.Error("a buffer stamped 100 ms in lands at frame 4800 after silence")
	}
	if !slices.Equal(got[5280:5760], filled(480, 0.25)) {
		t.Error("a buffer stamped 1 ms late follows its predecessor without a gap")
	}
}

func TestClockedWAVLeavesOutWhatWasCapturedWhilePaused(t *testing.T) {
	base := Now()
	got := clockedFile(t, clockOf(base, 0, time.Second, 2*time.Second, 3*time.Second), func(w *clockedWAV) {
		must(t, w.write(filled(960, 1), at(base, 990*time.Millisecond)))
		must(t, w.write(filled(960, 0.5), at(base, 1990*time.Millisecond)))
	})
	if len(got) != 96_000 {
		t.Fatalf("two seconds of media time: %d frames", len(got))
	}
	if !slices.Equal(got[47_520:48_000], filled(480, 1)) {
		t.Error("the 10 ms captured before the pause end the first span")
	}
	if !slices.Equal(got[48_000:48_480], filled(480, 0.5)) || got[48_480] != 0 {
		t.Error("the 10 ms captured after resuming start the second span, and the 10 ms in the pause are gone")
	}
}

func TestClockedWAVPullsADriftingDeviceBack(t *testing.T) {
	base := Now()
	stamp := func(i int) time.Duration { return time.Duration(i) * 10_050 * time.Microsecond }
	got := clockedFile(t, clockOf(base, 0, 2*time.Second), func(w *clockedWAV) {
		for i := range 150 {
			must(t, w.write(filled(480, float32(i+1)), at(base, stamp(i))))
		}
	})
	for i := range 150 {
		landed := slices.Index(got, float32(i+1))
		if want := framesAt(stamp(i)); landed < 0 || math.Abs(float64(int64(landed)-want)) > placementSlack {
			t.Fatalf("buffer %d landed at frame %d, its stamp says %d", i, landed, want)
		}
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
