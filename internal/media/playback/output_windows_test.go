package playback

import (
	"math"
	"path/filepath"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media/capture"
)

// TestToneReachesTheSystemMix plays a tone through AudioOutput while loopback capture records
// what the default output device plays, and finds the tone's frequency and level in the
// recording. It makes a sound: run it inside the desktop lock.
func TestToneReachesTheSystemMix(t *testing.T) {
	if testing.Short() {
		t.Skip("plays a tone on the speakers")
	}
	if _, err := capture.DefaultOutput(); err != nil {
		t.Skip("no audio output device:", err)
	}
	const freq, amp = 997.0, 0.2
	tap := make(chan []float32, 1024)
	clock := capture.NewClock()
	track, err := capture.StartTrack(capture.Loopback, filepath.Join(t.TempDir(), "loopback.wav"), 2, clock, tap)
	if err != nil {
		t.Fatal(err)
	}
	clock.Resume()
	time.Sleep(200 * time.Millisecond)

	var n int64
	out, err := StartAudioOutput(func(buf []float32) {
		for i := 0; i+1 < len(buf); i += 2 {
			v := float32(amp * math.Sin(2*math.Pi*freq*float64(n)/SampleRate))
			buf[i], buf[i+1] = v, v
			n++
		}
	})
	if err != nil {
		track.Stop()
		t.Fatal(err)
	}
	began := time.Now()
	time.Sleep(1500 * time.Millisecond)
	played, wall := out.PlayedMS(), float64(time.Since(began).Microseconds())/1000
	out.Close()
	time.Sleep(200 * time.Millisecond)
	track.Stop()
	close(tap)
	if err := out.Err(); err != nil {
		t.Fatal(err)
	}

	var mono []float64
	for buf := range tap {
		for i := 0; i+1 < len(buf); i += 2 {
			mono = append(mono, float64(buf[i]+buf[i+1])/2)
		}
	}
	loud := loudest(mono, SampleRate/2)
	if loud == nil {
		t.Fatalf("loopback recorded %d frames, none of them the tone", len(mono))
	}
	peak := peakFrequency(loud, 100, 4000)
	level := rms(loud)
	t.Logf("device clock %.1f ms over %.1f ms of wall time; loopback peak %.1f Hz, RMS %.4f (played %.4f)", played, wall, peak, level, amp/math.Sqrt2)
	if math.Abs(peak-freq) > 3 {
		t.Errorf("the loudest frequency in the loopback is %.1f Hz, want %.0f Hz", peak, freq)
	}
	if level < 0.1*amp/math.Sqrt2 || level > 1.5*amp/math.Sqrt2 {
		t.Errorf("the tone's level in the loopback is %.4f, played at %.4f", level, amp/math.Sqrt2)
	}
	if played < wall-150 || played > wall+5 {
		t.Errorf("the device clock read %.1f ms after %.1f ms of playing", played, wall)
	}
}

// loudest returns the window of n samples with the highest RMS, stepping by a tenth of it,
// or nil when nothing rises above the noise floor.
func loudest(x []float64, n int) []float64 {
	var best []float64
	bestRMS := 0.005
	for i := 0; i+n <= len(x); i += n / 10 {
		if r := rms(x[i : i+n]); r > bestRMS {
			best, bestRMS = x[i:i+n], r
		}
	}
	return best
}

func rms(x []float64) float64 {
	var sum float64
	for _, v := range x {
		sum += v * v
	}
	return math.Sqrt(sum / float64(max(len(x), 1)))
}

// peakFrequency scans [lo, hi] Hz with the Goertzel filter, coarse then fine.
func peakFrequency(x []float64, lo, hi float64) float64 {
	scan := func(lo, hi, step float64) float64 {
		best, bestP := lo, -1.0
		for f := lo; f <= hi; f += step {
			if p := goertzel(x, f); p > bestP {
				best, bestP = f, p
			}
		}
		return best
	}
	f := scan(lo, hi, 10)
	return scan(f-10, f+10, 0.5)
}

func goertzel(x []float64, f float64) float64 {
	k := 2 * math.Cos(2*math.Pi*f/SampleRate)
	var s1, s2 float64
	for _, v := range x {
		s1, s2 = v+k*s1-s2, s1
	}
	return s1*s1 + s2*s2 - k*s1*s2
}
