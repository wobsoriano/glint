// Command rec records system loopback, and the default microphone when one exists, to the
// session WAVs, then decodes what it wrote and checks it against media time. With -tone it
// plays a sine through the default output while recording and measures it in system.wav.
//
//	go run ./internal/media/capture/cmd/rec -seconds 5 -tone 1000 -pause-at 2s -pause-for 1s
package main

import (
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/wobsoriano/glint/internal/media/capture"
)

func main() {
	seconds := flag.Float64("seconds", 5, "media seconds to record, pauses excluded")
	out := flag.String("out", "", "session directory (default: a new temp dir)")
	tone := flag.Float64("tone", 0, "play a sine of this many Hz through the default output while recording")
	pauseAt := flag.Duration("pause-at", 0, "pause after this much media time (0: never)")
	pauseFor := flag.Duration("pause-for", time.Second, "how long the pause lasts on the wall clock")
	flag.Parse()

	dir := *out
	if dir == "" {
		var err error
		if dir, err = os.MkdirTemp("", "glint-rec-"); err != nil {
			log.Fatal(err)
		}
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		log.Fatal(err)
	}
	if o, err := capture.DefaultOutput(); err == nil {
		fmt.Printf("output: %s (%s)\n", o.Name, o.ID)
	} else {
		log.Fatalf("no default output: %v", err)
	}
	mics, err := capture.Microphones()
	if err != nil {
		log.Fatal(err)
	}
	for _, m := range mics {
		fmt.Printf("mic:    %s (%s)\n", m.Name, m.ID)
	}

	clock := capture.NewClock()
	system, err := capture.StartTrack(capture.Loopback, filepath.Join(dir, "system.wav"), 2, clock, nil)
	if err != nil {
		log.Fatal(err)
	}
	var mic *capture.Track
	if len(mics) > 0 {
		if mic, err = capture.StartTrack(capture.Microphone(""), filepath.Join(dir, "mic.wav"), 1, clock, nil); err != nil {
			log.Printf("microphone skipped: %v", err)
		}
	}

	var player *exec.Cmd
	if *tone > 0 {
		tonePath := filepath.Join(dir, "tone.wav")
		if err := writeTone(tonePath, *tone, *seconds-1); err != nil {
			log.Fatal(err)
		}
		player = exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("(New-Object System.Media.SoundPlayer '%s').PlaySync()", tonePath))
		if err := player.Start(); err != nil {
			log.Fatal(err)
		}
	}

	media := time.Duration(*seconds * float64(time.Second))
	clock.Resume()
	if *pauseAt > 0 && *pauseAt < media {
		time.Sleep(*pauseAt)
		clock.Pause()
		time.Sleep(*pauseFor)
		clock.Resume()
		time.Sleep(media - *pauseAt)
	} else {
		time.Sleep(media)
	}
	clock.Pause()
	mediaNow := clock.Now()
	if player != nil {
		player.Wait()
	}

	stats, err := system.Stop()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("media time %.1f ms\n", ms(mediaNow))
	ok := report("system.wav", filepath.Join(dir, "system.wav"), stats, mediaNow, 2, *tone)
	if mic != nil {
		mstats, err := mic.Stop()
		if err != nil {
			log.Fatal(err)
		}
		ok = report("mic.wav", filepath.Join(dir, "mic.wav"), mstats, mediaNow, 1, 0) && ok
	}
	fmt.Println("session:", dir)
	if !ok {
		os.Exit(1)
	}
}

func ms(d time.Duration) float64 { return float64(d) / float64(time.Millisecond) }

func report(name, path string, stats capture.Stats, media time.Duration, channels int, tone float64) bool {
	w, err := readWAV(path)
	if err != nil {
		fmt.Printf("%s: %v\n", name, err)
		return false
	}
	frames := len(w.samples) / max(w.channels, 1)
	dur := float64(frames) * 1000 / float64(w.rate)
	drift := dur - ms(media)
	ok := w.rate == capture.SampleRate && w.channels == channels && uint64(frames) == stats.Frames && math.Abs(drift) <= 50
	fmt.Printf("%s: %d Hz, %d ch, float32, %d frames, %.1f ms (%+.1f ms vs media time), %d bytes, RMS %.4f\n",
		name, w.rate, w.channels, frames, dur, drift, stats.Bytes, rms(w.samples))
	if tone > 0 {
		start, end, freq, level := measureTone(w)
		fmt.Printf("%s: tone from %.0f ms to %.0f ms, %.1f Hz (want %.0f), RMS %.4f\n",
			name, start*1000/float64(w.rate), end*1000/float64(w.rate), freq, tone, level)
		ok = ok && math.Abs(freq-tone) <= tone*0.01 && level > 0.01
	}
	if probe, err := exec.Command("ffprobe", "-v", "error", "-show_entries",
		"stream=codec_name,sample_rate,channels,sample_fmt:format=duration", "-of", "compact", path).CombinedOutput(); err == nil {
		fmt.Printf("%s: ffprobe %s", name, probe)
	} else {
		fmt.Printf("%s: ffprobe failed: %v %s\n", name, err, probe)
		ok = false
	}
	if !ok {
		fmt.Printf("%s: FAILED\n", name)
	}
	return ok
}

type wav struct {
	rate, channels int
	samples        []float32
}

// readWAV reads the 46-byte IEEE float header capture writes, and nothing else.
func readWAV(path string) (wav, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return wav{}, err
	}
	le := binary.LittleEndian
	if len(b) < 46 || string(b[0:4]) != "RIFF" || string(b[8:16]) != "WAVEfmt " || le.Uint16(b[20:]) != 3 || string(b[38:42]) != "data" {
		return wav{}, errors.New("not the 46-byte float WAV header")
	}
	if int(le.Uint32(b[42:])) != len(b)-46 || int(le.Uint32(b[4:])) != len(b)-8 {
		return wav{}, fmt.Errorf("header sizes do not match the %d-byte file", len(b))
	}
	w := wav{rate: int(le.Uint32(b[24:])), channels: int(le.Uint16(b[22:]))}
	for i := 46; i+4 <= len(b); i += 4 {
		w.samples = append(w.samples, math.Float32frombits(le.Uint32(b[i:])))
	}
	return w, nil
}

func rms(s []float32) float64 {
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(max(len(s), 1)))
}

// measureTone finds the span of the left channel louder than -40 dBFS and returns its frame
// range, its frequency from zero crossings and its RMS.
func measureTone(w wav) (start, end, freq, level float64) {
	left := make([]float64, len(w.samples)/w.channels)
	for i := range left {
		left[i] = float64(w.samples[i*w.channels])
	}
	first, last := -1, -1
	for i, v := range left {
		if math.Abs(v) > 0.01 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return 0, 0, 0, 0
	}
	// Skip the edges, where the player's start and stop can click.
	margin := (last - first) / 10
	span := left[first+margin : last-margin]
	crossings := 0
	var sum float64
	for i := 1; i < len(span); i++ {
		if (span[i-1] < 0) != (span[i] < 0) {
			crossings++
		}
		sum += span[i] * span[i]
	}
	secs := float64(len(span)) / float64(w.rate)
	return float64(first), float64(last), float64(crossings) / 2 / secs, math.Sqrt(sum / float64(len(span)))
}

// writeTone writes a 16-bit PCM stereo sine at amplitude 0.5, which SoundPlayer can play.
func writeTone(path string, hz, seconds float64) error {
	const rate = 48000
	frames := int(seconds * rate)
	le := binary.LittleEndian
	b := []byte("RIFF")
	b = le.AppendUint32(b, uint32(36+frames*4))
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 16)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint16(b, 2)
	b = le.AppendUint32(b, rate)
	b = le.AppendUint32(b, rate*4)
	b = le.AppendUint16(b, 4)
	b = le.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = le.AppendUint32(b, uint32(frames*4))
	for i := range frames {
		s := int16(0.5 * 32767 * math.Sin(2*math.Pi*hz*float64(i)/rate))
		b = le.AppendUint16(b, uint16(s))
		b = le.AppendUint16(b, uint16(s))
	}
	return os.WriteFile(path, b, 0o644)
}
