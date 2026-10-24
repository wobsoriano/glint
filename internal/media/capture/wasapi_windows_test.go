package capture

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMicrophoneEnumeration(t *testing.T) {
	mics, err := Microphones()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range mics {
		if !strings.HasPrefix(string(m.ID), "{0.0.1.00000000}") || m.Name == "" {
			t.Errorf("endpoint %q named %q", m.ID, m.Name)
		}
	}
	if out, err := DefaultOutput(); err == nil && (!strings.HasPrefix(string(out.ID), "{0.0.0.00000000}") || out.Name == "") {
		t.Errorf("default output %q named %q", out.ID, out.Name)
	}
}

func TestLoopbackTrackMatchesMediaTimeAndSkipsPauses(t *testing.T) {
	if _, err := DefaultOutput(); err != nil {
		t.Skip("no audio output device:", err)
	}
	path := filepath.Join(t.TempDir(), "system.wav")
	clock := NewClock()
	track, err := StartTrack(Loopback, path, 2, clock, nil)
	if err != nil {
		t.Fatal(err)
	}
	clock.Resume()
	time.Sleep(700 * time.Millisecond)
	clock.Pause()
	time.Sleep(300 * time.Millisecond)
	clock.Resume()
	time.Sleep(300 * time.Millisecond)
	clock.Pause()
	media := clock.Now()
	stats, err := track.Stop()
	if err != nil {
		t.Fatal(err)
	}
	wav := readFloatWAV(t, path)
	if wav.rate != 48000 || wav.channels != 2 {
		t.Fatalf("format %d Hz %d ch", wav.rate, wav.channels)
	}
	frames := uint64(len(wav.samples) / 2)
	if frames != stats.Frames {
		t.Fatalf("file has %d frames, stats say %d", frames, stats.Frames)
	}
	ms := int64(frames * 1000 / SampleRate)
	if d := ms - media.Milliseconds(); d < -70 || d > 70 {
		t.Fatalf("wav is %d ms, media time %v", ms, media)
	}
	if ms >= 1200 {
		t.Fatalf("the 300 ms pause leaked into the file: %d ms", ms)
	}
}

func TestMicTapReceivesBuffersWithoutStallingTheFile(t *testing.T) {
	clock := NewClock()
	tap := make(chan []float32, 1)
	track, err := StartTrack(Microphone(""), filepath.Join(t.TempDir(), "mic.wav"), 1, clock, tap)
	if err != nil {
		t.Skip("no microphone:", err)
	}
	clock.Resume()
	time.Sleep(600 * time.Millisecond)
	clock.Pause()
	media := clock.Now()
	stats, err := track.Stop()
	if err != nil {
		t.Fatal(err)
	}
	if len(tap) != 1 || len(<-tap) == 0 {
		t.Fatal("a one-slot tap that nobody reads holds one buffer and drops the rest")
	}
	ms := int64(stats.Frames * 1000 / SampleRate)
	if d := ms - media.Milliseconds(); d < -70 || d > 70 {
		t.Fatalf("the full tap stalled the file: %d ms of %v", ms, media)
	}
}

func TestMicMonitorMetersTheDefaultMicrophone(t *testing.T) {
	m, err := StartMicMonitor("")
	if err != nil {
		t.Skip("no microphone:", err)
	}
	time.Sleep(300 * time.Millisecond)
	level := m.Level()
	m.Close()
	if level < 0 || level > 2 {
		t.Fatalf("level %v", level)
	}
	t.Logf("default microphone RMS %.5f", level)
}
