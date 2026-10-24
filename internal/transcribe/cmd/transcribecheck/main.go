// Command transcribecheck proves on-device transcription end to end: it synthesizes speech
// with Windows SAPI, installs the whisper.cpp runtime and models into a scratch data dir,
// transcribes, scores the result, measures cancel latency, and follows a script in live mode.
//
//	go run ./internal/transcribe/cmd/transcribecheck -data <scratch dir> [-wav speech.wav]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/transcribe"
)

const sentence = "The quick brown fox jumps over the lazy dog. Today we are recording a short demo of the new screen recorder, and the captions should follow every word that I say."

var failed bool

func check(ok bool, format string, args ...any) {
	status := "ok  "
	if !ok {
		status, failed = "FAIL", true
	}
	fmt.Printf("%s %s\n", status, fmt.Sprintf(format, args...))
}

func main() {
	data := flag.String("data", "", "scratch data dir (never the real Glint data dir)")
	wav := flag.String("wav", "", "speech WAV; synthesized from the test sentence when empty")
	flag.Parse()
	if *data == "" {
		fmt.Fprintln(os.Stderr, "-data is required")
		os.Exit(2)
	}
	if *wav == "" {
		*wav = filepath.Join(*data, "speech.wav")
		if err := synthesize(*wav, sentence); err != nil {
			fatal(err)
		}
	}
	w, err := media.ReadWAV(*wav)
	if err != nil {
		fatal(err)
	}
	pcm := transcribe.Pcm{Samples: w.Samples, SampleRate: uint32(w.SampleRate), Channels: uint16(w.Channels)}
	durationMs := pcm.DurationMs()
	fmt.Printf("audio %s: %d ms, %d Hz, %d ch\n", *wav, durationMs, w.SampleRate, w.Channels)

	ctx := context.Background()
	base := install(ctx, *data, transcribe.ModelBaseEn)

	var maxProgress float32
	started := time.Now()
	t, err := transcribe.Transcribe(ctx, base, pcm, func(p float32) { maxProgress = max(maxProgress, p) })
	if err != nil {
		fatal(err)
	}
	fmt.Printf("transcribed %d words in %v: %s\n", len(t.Words), time.Since(started).Round(time.Millisecond), t.Text())
	out := filepath.Join(*data, transcribe.TranscriptFile)
	if err := t.Save(out); err != nil {
		fatal(err)
	}
	reloaded, err := transcribe.Load(out)
	check(err == nil && len(reloaded.Words) == len(t.Words), "%s reloads with %d words", out, len(reloaded.Words))
	check(t.Language == "en", "language %q", t.Language)
	check(maxProgress == 1, "progress reached %.0f%%", maxProgress*100)
	errs, n := wordErrors(sentence, t.Words)
	check(errs == 0, "WER %.1f%% (%d errors over %d reference words)", 100*float64(errs)/float64(n), errs, n)
	check(timingsSane(t.Words, durationMs), "word timings monotonic and within %d ms", durationMs)

	measureCancel(base, pcm)

	tiny := install(ctx, *data, transcribe.ModelTinyEn)
	followLive(tiny, w)

	if failed {
		os.Exit(1)
	}
}

func install(ctx context.Context, data string, m transcribe.Model) string {
	var last uint64
	started := time.Now()
	path, err := transcribe.DownloadModel(ctx, data, m, func(done, total uint64) { last = total })
	if err != nil {
		fatal(err)
	}
	fmt.Printf("%s ready at %s (%d bytes fetched in %v)\n", m, path, last, time.Since(started).Round(time.Millisecond))
	return path
}

func synthesize(path, text string) error {
	script := `Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
$f = New-Object System.Speech.AudioFormat.SpeechAudioFormatInfo(48000, [System.Speech.AudioFormat.AudioBitsPerSample]::Sixteen, [System.Speech.AudioFormat.AudioChannel]::Mono)
$s.SetOutputToWaveFile($env:GLINT_CHECK_WAV, $f)
$s.Speak($env:GLINT_CHECK_TEXT)
$s.Dispose()`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.Env = append(os.Environ(), "GLINT_CHECK_WAV="+path, "GLINT_CHECK_TEXT="+text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("SAPI synthesis: %v: %s", err, out)
	}
	return nil
}

func normalized(text string) []string {
	var out []string
	for _, w := range strings.Fields(text) {
		if n := transcribe.NormalizeWord(w); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// wordErrors is the word-level edit distance between the reference text and the recognized
// words, with case and punctuation ignored.
func wordErrors(reference string, words []transcribe.Word) (int, int) {
	ref := normalized(reference)
	var hyp []string
	for _, w := range words {
		hyp = append(hyp, normalized(w.Text)...)
	}
	prev := make([]int, len(hyp)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ref); i++ {
		cur := make([]int, len(hyp)+1)
		cur[0] = i
		for j := 1; j <= len(hyp); j++ {
			sub := prev[j-1]
			if ref[i-1] != hyp[j-1] {
				sub++
			}
			cur[j] = min(sub, prev[j]+1, cur[j-1]+1)
		}
		prev = cur
	}
	return prev[len(hyp)], len(ref)
}

func timingsSane(words []transcribe.Word, durationMs uint64) bool {
	for i, w := range words {
		if w.StartMs > w.EndMs || w.EndMs > durationMs+10 {
			return false
		}
		if i > 0 && w.StartMs < words[i-1].StartMs {
			return false
		}
	}
	return len(words) > 0
}

// measureCancel transcribes three back-to-back copies of the audio, cancels mid-run, and
// times how long Transcribe takes to return.
func measureCancel(model string, pcm transcribe.Pcm) {
	long := pcm
	long.Samples = append(append(append([]float32(nil), pcm.Samples...), pcm.Samples...), pcm.Samples...)
	for _, after := range []time.Duration{100 * time.Millisecond, 1500 * time.Millisecond, 2500 * time.Millisecond} {
		ctx, cancel := context.WithCancel(context.Background())
		at := make(chan time.Time, 1)
		time.AfterFunc(after, func() { at <- time.Now(); cancel() })
		_, err := transcribe.Transcribe(ctx, model, long, nil)
		latency := time.Since(<-at)
		check(err == context.Canceled && latency < time.Second, "cancel at %v returned %v after %v", after, err, latency.Round(time.Millisecond))
	}
}

// followLive streams the WAV into live mode in real time, 100 ms at a time, and feeds the
// words to a ScriptTracker over the test sentence.
func followLive(model string, w *media.WavData) {
	mono := transcribe.Pcm{Samples: w.Samples, SampleRate: uint32(w.SampleRate), Channels: uint16(w.Channels)}.ToMono()
	live, err := transcribe.StartLive(model, uint32(w.SampleRate))
	if err != nil {
		fatal(err)
	}
	tracker := transcribe.NewScriptTracker(sentence)
	chunk := w.SampleRate / 10
	start := time.Now()
	var all []transcribe.Word
	var positions []string
	feed := func(words []transcribe.Word) {
		if len(words) == 0 {
			return
		}
		all = append(all, words...)
		spoken := make([]string, len(words))
		for i, word := range words {
			spoken[i] = word.Text
		}
		idx, ok := tracker.Feed(spoken)
		positions = append(positions, fmt.Sprintf("%.1fs:%d/%v", time.Since(start).Seconds(), idx, ok))
	}
	for i := 0; i < len(mono); i += chunk {
		live.Push(mono[i:min(i+chunk, len(mono))])
		time.Sleep(time.Until(start.Add(time.Duration(i/chunk+1) * 100 * time.Millisecond)))
		feed(live.Poll())
	}
	feed(live.Finish())
	idx, ok := tracker.CurrentIndex()
	var texts []string
	for _, word := range all {
		texts = append(texts, word.Text)
	}
	fmt.Printf("live words: %s\n", strings.Join(texts, " "))
	fmt.Printf("tracker: %s\n", strings.Join(positions, " "))
	check(ok && idx == tracker.DisplayLen()-1, "tracker reached script word %d of %d", idx+1, tracker.DisplayLen())
	errs, n := wordErrors(sentence, all)
	fmt.Printf("     live WER %.1f%% (%d/%d)\n", 100*float64(errs)/float64(n), errs, n)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "transcribecheck:", err)
	os.Exit(1)
}
