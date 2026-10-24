// Command wordtimecheck proves that transcribed words land where they are spoken. Windows SAPI
// speaks scripts with pauses into WAV files and reports where each word starts and where its
// last phoneme ends; the check transcribes each WAV with -model and asserts that every word's
// start and end are within -tol of that truth, that cutting a phrase or the filler words
// removes them and nothing of their neighbors, and that silence trims remove no word.
//
// Clips that show a known open bug with base.en run only with -open.
//
// With -wav it checks a recording of SAPI speaking -text at -rate instead (such as the
// system.wav of tools/verify/transcribe.ps1): the truth is a fresh synthesis of the same text,
// shifted to the recording's speech onset.
//
//	go run ./internal/transcribe/cmd/wordtimecheck -data <scratch dir> [-model base-en] [-open] [-wav rec.wav -text "..." -rate -1]
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"math"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/transcribe"
)

// clip is a SAPI script: text to speak, or a pause in milliseconds.
type clip struct {
	name string
	// voice is an installed SAPI voice; empty is the default, Microsoft David.
	voice string
	rate  int
	parts []string
	// noiseDb, when set, adds white noise at that RMS level in dBFS.
	noiseDb float64
	cuts    []string
	fillers []string
	// open names the known decode bug this clip shows with base.en; it runs only with -open.
	open string
}

var pauses = []string{"1200", "Glint turns this narration into captions on the device.", "900",
	"Cut a phrase and the video follows the words.", "1500"}

// loneTimestamp is how whisper.cpp ends a window when the decode closes on a single timestamp
// token: it takes that as the end of speech, skips the rest of the 30 s window, and stops DTW
// there, so words after it are lost or squeezed against it.
const loneTimestamp = "whisper.cpp ends the window on an early lone timestamp"

var clips = []clip{
	{name: "pauses", parts: pauses, cuts: []string{"this narration into", "Cut a phrase", "device"}, open: loneTimestamp + " and drops the second sentence"},
	{name: "pauses-noise", parts: pauses, noiseDb: -40, cuts: []string{"this narration into", "Cut a phrase", "device"}},
	{
		name: "fillers",
		parts: []string{"800", "Um, so I was thinking, uh, we could, like, try this.", "700",
			"Hmm, okay. Uh, let us start the recording.", "1000"},
		fillers: []string{"um", "uh", "like", "hmm", "uh"},
		open:    loneTimestamp + " 1.5 s before the last words end",
	},
	{name: "pauses-zira", voice: "Microsoft Zira Desktop", parts: pauses, cuts: []string{"this narration into", "Cut a phrase", "device"}},
	{
		name:  "fillers-zira",
		voice: "Microsoft Zira Desktop",
		parts: []string{"800", "Um, so I was thinking, uh, we could, like, try this.", "700",
			"Hmm, okay. Uh, let us start the recording.", "1000"},
		fillers: []string{"um", "uh", "like", "hmm", "uh"},
	},
	{
		name: "silence",
		parts: []string{"25000", "Glint turns this narration into captions on the device.", "20000",
			"Cut a phrase and the video follows the words.", "3000"},
		cuts: []string{"the video follows"},
	},
	{
		name: "long",
		rate: -1,
		parts: []string{"1000", "Welcome to the demo of the new screen recorder.", "600",
			"Today we will record a short tutorial and turn the narration into captions.", "1200",
			"First, open the studio and pick the recording you want to edit.", "500",
			"Every word you say shows up in the transcript on the right.", "900",
			"Click a word to jump to that moment in the video.", "700",
			"Select a few words and press delete to cut them out.", "1500",
			"The silence between sentences can be trimmed with one click.", "800",
			"When you are happy with the result, export the video and share it.", "1200"},
		cuts: []string{"pick the recording", "with one click"},
	},
	{
		name: "minutes",
		parts: []string{"800",
			"This is a longer narration that runs for a few minutes, so the recognizer has to cross several of its thirty second windows.", "700",
			"I am going to walk through a typical editing session from start to finish.", "1100",
			"First I open the library and find yesterday's recording of the product demo.", "400",
			"The thumbnail shows the first frame, and the title is the date and time.", "900",
			"Double clicking it opens the studio with the timeline at the bottom of the window.", "1300",
			"Before cutting anything, I transcribe the audio so I can edit by text.", "600",
			"The model downloads once, and after that it runs entirely on this computer.", "1000",
			"Each word appears in the transcript with its own place on the timeline.", "500",
			"When I click a word, the playhead jumps to the moment it was spoken.", "1200",
			"There is a long pause here where I was looking for the right window.", "2500",
			"Trimming silences removes stretches like that without touching the words.", "800",
			"Next I select a sentence that went wrong and delete it from the transcript.", "700",
			"The video loses exactly that part, and the cut is smooth.", "1500",
			"Filler words can go too, but I like to keep a few so it still sounds natural.", "600",
			"Then I add a zoom on the settings panel, because the text there is small.", "900",
			"The cursor gets a little bigger, and clicks show a soft ring.", "1100",
			"Captions are on by default, and I move them to the top of the frame.", "400",
			"Every caption line follows the words as they are spoken.", "1000",
			"Finally I export the video at full resolution and share the link with my team.", "700",
			"That is the whole workflow, and it took about five minutes.", "1500"},
		cuts: []string{"a sentence that went wrong", "five minutes"},
	},
}

type span struct {
	text       string
	start, end uint64
}

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
	tol := flag.Uint64("tol", 150, "allowed error of each word's start and end, in ms")
	wav := flag.String("wav", "", "recording of SAPI speaking -text; checks it instead of the built-in clips")
	text := flag.String("text", "Glint turns this narration into captions on the device.", "what the -wav recording says")
	rate := flag.Int("rate", -1, "SAPI rate the -wav recording was spoken at")
	modelName := flag.String("model", string(transcribe.ModelBaseEn), "model to transcribe with")
	open := flag.Bool("open", false, "also run the clips that show a known open bug")
	flag.Parse()
	if *data == "" || !slices.Contains(transcribe.AllModels, transcribe.Model(*modelName)) {
		fmt.Fprintf(os.Stderr, "-data is required and -model is one of %v\n", transcribe.AllModels)
		os.Exit(2)
	}
	model, err := transcribe.DownloadModel(context.Background(), *data, transcribe.Model(*modelName), nil)
	if err != nil {
		fatal(err)
	}
	if *wav != "" {
		checkRecording(model, *data, *wav, *text, *rate, *tol)
	} else {
		for _, c := range clips {
			if c.open != "" && !*open {
				fmt.Printf("\nskip %s without -open: %s\n", c.name, c.open)
				continue
			}
			path := filepath.Join(*data, c.name+".wav")
			truth, err := synthesize(path, c.voice, c.rate, c.parts)
			if err != nil {
				fatal(err)
			}
			pcm := readPcm(path)
			if c.noiseDb != 0 {
				addNoise(pcm.Samples, c.noiseDb)
			}
			checkClip(model, c, pcm, truth, *tol)
		}
	}
	if failed {
		os.Exit(1)
	}
}

func checkRecording(model, data, wav, text string, rate int, tol uint64) {
	synth := filepath.Join(data, "reference.wav")
	truth, err := synthesize(synth, "", rate, []string{"500", text, "500"})
	if err != nil {
		fatal(err)
	}
	pcm := readPcm(wav)
	rec0, rec1 := speechRun(pcm)
	syn0, syn1 := speechRun(readPcm(synth))
	// Played through the audio device, SAPI does not keep the file synthesis's pace exactly,
	// so this truth is good to about the difference in length, which grows toward the end.
	fmt.Printf("speech: %d-%d ms in %s, %d-%d ms in the synthesis\n", rec0, rec1, wav, syn0, syn1)
	for i := range truth {
		truth[i].start = truth[i].start + rec0 - syn0
		truth[i].end = truth[i].end + rec0 - syn0
	}
	checkClip(model, clip{name: filepath.Base(wav), cuts: []string{"this narration into"}}, pcm, truth, tol)
}

func checkClip(model string, c clip, pcm transcribe.Pcm, truth []span, tol uint64) {
	name := c.name
	dur := pcm.DurationMs()
	started := time.Now()
	t, err := transcribe.Transcribe(context.Background(), model, pcm, nil)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("\n%s: %d ms of audio, %d words in %v: %s\n", name, dur, len(t.Words), time.Since(started).Round(time.Millisecond), t.Text())
	pairs := align(truth, t.Words)
	saidAs := map[int][]int{}
	for i := range truth {
		if h, ok := pairs[i]; ok {
			saidAs[h] = append(saidAs[h], i)
		}
	}
	fmt.Printf("  %-12s %-12s %11s %11s %7s %7s\n", "said", "heard", "truth", "got", "start", "end")
	var worst uint64
	matched := 0
	for i, s := range truth {
		h, ok := pairs[i]
		if !ok {
			fmt.Printf("  %-12s %-12s %5d-%-5d\n", s.text, "(missing)", s.start, s.end)
			continue
		}
		matched++
		group := saidAs[h]
		if group[0] != i {
			continue
		}
		said, end := s.text, s.end
		for _, j := range group[1:] {
			said, end = said+" "+truth[j].text, truth[j].end
		}
		w := t.Words[h]
		ds, de := signed(w.StartMs, s.start), signed(w.EndMs, end)
		worst = max(worst, abs(ds), abs(de))
		mark := ""
		if abs(ds) > tol || abs(de) > tol {
			mark = " <"
		}
		fmt.Printf("  %-12s %-12s %5d-%-5d %5d-%-5d %+7d %+7d%s\n", said, w.Text, s.start, end, w.StartMs, w.EndMs, ds, de, mark)
	}
	check(matched == len(truth), "%s: %d of %d spoken words recognized (%d heard)", name, matched, len(truth), len(t.Words))
	check(worst <= tol, "%s: every word's start and end within %d ms of the truth (worst %d ms)", name, tol, worst)

	for _, phrase := range c.cuts {
		first := find(truth, normalized(phrase))
		if first < 0 {
			fatal(fmt.Errorf("%q is not in the %s script", phrase, name))
		}
		last := first + len(normalized(phrase)) - 1
		hs, ok1 := pairs[first]
		he, ok2 := pairs[last]
		if !ok1 || !ok2 {
			check(false, "%s: cut %q: the phrase was not recognized", name, phrase)
			continue
		}
		checkCut(name, fmt.Sprintf("cut %q", phrase), truth, first, last, t.Words, transcribe.IndexRange{Start: hs, End: he + 1}, dur, tol)
	}
	if c.fillers != nil {
		var said, found []int
		for i, s := range truth {
			if len(said) < len(c.fillers) && transcribe.NormalizeWord(s.text) == c.fillers[len(said)] {
				said = append(said, i)
			}
		}
		for _, r := range transcribe.FillerRanges(t.Words) {
			first, last := saidAs[r.Start], saidAs[r.End-1]
			heard := t.Words[r.Start].Text
			if len(first) == 0 || len(last) == 0 {
				check(false, "%s: filler %q was not said", name, heard)
				continue
			}
			found = append(found, first[0])
			checkCut(name, fmt.Sprintf("filler %q (said %q)", heard, truth[first[0]].text), truth, first[0], last[len(last)-1], t.Words, r, dur, tol)
		}
		check(slices.Equal(found, said), "%s: fillers found at said words %v, said at %v", name, found, said)
	}

	opts := transcribe.DefaultSilenceOptions()
	opts.MinDurationMs = 500
	silences := transcribe.SilenceCutRanges(transcribe.SilenceSpans(pcm, opts), 150, 200)
	var hit []string
	for _, cut := range silences {
		for i, s := range truth {
			if cut.StartMs < s.end && s.start < cut.EndMs {
				hit = append(hit, fmt.Sprintf("%q in %d-%d", s.text, cut.StartMs, cut.EndMs))
			}
			if h, ok := pairs[i]; ok && cut.StartMs+tol < t.Words[h].EndMs && t.Words[h].StartMs+tol < cut.EndMs {
				hit = append(hit, fmt.Sprintf("heard %q in %d-%d", t.Words[h].Text, cut.StartMs, cut.EndMs))
			}
		}
	}
	check(len(hit) == 0, "%s: %d silence trims over 500 ms remove no word %v", name, len(silences), hit)
}

// checkCut cuts the heard words sel, which were said as truth[first..last], and requires the
// cut to cover what was said and to stay off the neighbors, each within tol.
func checkCut(name, what string, truth []span, first, last int, words []transcribe.Word, sel transcribe.IndexRange, dur, tol uint64) {
	cuts := transcribe.WordCutRanges(words, []transcribe.IndexRange{sel}, dur)
	if len(cuts) != 1 {
		check(false, "%s: %s gave %d ranges", name, what, len(cuts))
		return
	}
	c := cuts[0]
	lo, hi := uint64(0), dur
	if first > 0 {
		lo = truth[first-1].end
	}
	if last+1 < len(truth) {
		hi = truth[last+1].start
	}
	covers := c.StartMs <= truth[first].start+tol && c.EndMs+tol >= truth[last].end
	spares := c.StartMs+tol >= lo && c.EndMs <= hi+tol
	check(covers && spares, "%s: %s removes %d-%d; said %d-%d between neighbors ending %d and starting %d",
		name, what, c.StartMs, c.EndMs, truth[first].start, truth[last].end, lo, hi)
}

func find(truth []span, words []string) int {
	ref := normalizedSpans(truth)
	for i := 0; i+len(words) <= len(ref); i++ {
		if slices.Equal(ref[i:i+len(words)], words) {
			return i
		}
	}
	return -1
}

// align pairs each truth word with the heard word it was recognized as, by word-level edit
// distance, so substitutions such as "Glint" heard as "Glenn" still pair up. One heard word
// may stand for two said ones, as "30-second" for "thirty second"; that costs more than a match
// and a missing word, so it is chosen only over a substitution and a missing word.
func align(truth []span, words []transcribe.Word) map[int]int {
	const edit, merge = 2, 3
	ref := normalizedSpans(truth)
	hyp := make([]string, len(words))
	for i, w := range words {
		hyp[i] = transcribe.NormalizeWord(w.Text)
	}
	d := make([][]int, len(ref)+1)
	for i := range d {
		d[i] = make([]int, len(hyp)+1)
		d[i][0] = i * edit
	}
	for j := range d[0] {
		d[0][j] = j * edit
	}
	sub := func(i, j int) int {
		if ref[i-1] == hyp[j-1] {
			return 0
		}
		return edit
	}
	merged := func(i, j int) int {
		if i < 2 {
			return math.MaxInt
		}
		return d[i-2][j-1] + merge
	}
	for i := 1; i <= len(ref); i++ {
		for j := 1; j <= len(hyp); j++ {
			d[i][j] = min(d[i-1][j-1]+sub(i, j), d[i-1][j]+edit, d[i][j-1]+edit, merged(i, j))
		}
	}
	pairs := map[int]int{}
	for i, j := len(ref), len(hyp); i > 0 && j > 0; {
		switch d[i][j] {
		case d[i-1][j-1] + sub(i, j):
			pairs[i-1] = j - 1
			i, j = i-1, j-1
		case merged(i, j):
			pairs[i-2], pairs[i-1] = j-1, j-1
			i, j = i-2, j-1
		case d[i-1][j] + edit:
			i--
		default:
			j--
		}
	}
	return pairs
}

// synthesize speaks parts with a SAPI voice into a 16 kHz mono WAV at path and returns each word's
// span: from SAPI's word event to the end of the word's last non-silent phoneme.
func synthesize(path, voice string, rate int, parts []string) ([]span, error) {
	script := `Add-Type -AssemblyName System.Speech
Add-Type -ReferencedAssemblies System.Speech -TypeDefinition @'
using System;
using System.Speech.Synthesis;
using System.Speech.AudioFormat;
public static class GlintSynth {
    public static void Run(string path, string voice, int rate, string[] parts) {
        using (var s = new SpeechSynthesizer()) {
            if (voice != "") s.SelectVoice(voice);
            s.Rate = rate;
            s.SetOutputToWaveFile(path, new SpeechAudioFormatInfo(16000, AudioBitsPerSample.Sixteen, AudioChannel.Mono));
            s.SpeakProgress += (o, e) => Console.WriteLine("word {0} {1}", (long)e.AudioPosition.TotalMilliseconds, e.Text);
            s.PhonemeReached += (o, e) => Console.WriteLine("phone {0} {1} {2}", (long)e.AudioPosition.TotalMilliseconds, (long)e.Duration.TotalMilliseconds, e.Phoneme.Length == 1 ? (int)e.Phoneme[0] : -1);
            var p = new PromptBuilder();
            foreach (var part in parts) {
                int ms;
                if (int.TryParse(part, out ms)) p.AppendBreak(TimeSpan.FromMilliseconds(ms)); else p.AppendText(part);
            }
            s.Speak(p);
        }
    }
}
'@
[GlintSynth]::Run($env:GLINT_SYNTH_WAV, "$env:GLINT_SYNTH_VOICE", [int]$env:GLINT_SYNTH_RATE, $env:GLINT_SYNTH_PARTS.Split('|'))`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.Env = append(os.Environ(), "GLINT_SYNTH_WAV="+path, "GLINT_SYNTH_VOICE="+voice, "GLINT_SYNTH_RATE="+strconv.Itoa(rate), "GLINT_SYNTH_PARTS="+strings.Join(parts, "|"))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("SAPI synthesis: %v: %s", err, out)
	}
	// SAPI's English phone id 7 is "_", silence.
	const silence = 7
	var spans []span
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		switch {
		case len(f) >= 3 && f[0] == "word":
			at, _ := strconv.ParseUint(f[1], 10, 64)
			spans = append(spans, span{text: strings.Join(f[2:], " "), start: at, end: at})
		case len(f) == 4 && f[0] == "phone" && len(spans) > 0 && f[3] != strconv.Itoa(silence):
			at, _ := strconv.ParseUint(f[1], 10, 64)
			d, _ := strconv.ParseUint(f[2], 10, 64)
			spans[len(spans)-1].end = max(spans[len(spans)-1].end, at+d)
		}
	}
	if len(spans) == 0 {
		return nil, fmt.Errorf("SAPI reported no words: %s", out)
	}
	return spans, nil
}

// addNoise mixes in uniform white noise with the given RMS level, from a fixed seed so runs
// compare.
func addNoise(samples []float32, rmsDb float64) {
	amp := math.Pow(10, rmsDb/20) * math.Sqrt(3)
	r := rand.New(rand.NewPCG(1, 2))
	for i := range samples {
		samples[i] += float32((r.Float64()*2 - 1) * amp)
	}
}

// speechRun is the first stretch of 10 ms frames louder than -40 dBFS, up to the first 300 ms
// of quieter frames.
func speechRun(p transcribe.Pcm) (start, end uint64) {
	mono := p.ToMono()
	frame := int(p.SampleRate / 100)
	started := false
	quiet := 0
	for i := 0; i+frame <= len(mono); i += frame {
		var sum float64
		for _, s := range mono[i : i+frame] {
			sum += float64(s) * float64(s)
		}
		ms := uint64(i) * 1000 / uint64(p.SampleRate)
		switch loud := 10*math.Log10(sum/float64(frame)+1e-12) > -40; {
		case loud && !started:
			start, end, started = ms, ms+10, true
		case loud:
			end, quiet = ms+10, 0
		case started:
			if quiet += 10; quiet >= 300 {
				return start, end
			}
		}
	}
	return start, end
}

func readPcm(path string) transcribe.Pcm {
	w, err := media.ReadWAV(path)
	if err != nil {
		fatal(err)
	}
	return transcribe.Pcm{Samples: w.Samples, SampleRate: uint32(w.SampleRate), Channels: uint16(w.Channels)}
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

func normalizedSpans(spans []span) []string {
	out := make([]string, len(spans))
	for i, s := range spans {
		out[i] = transcribe.NormalizeWord(s.text)
	}
	return out
}

func signed(got, want uint64) int64 { return int64(got) - int64(want) }

func abs(v int64) uint64 {
	if v < 0 {
		return uint64(-v)
	}
	return uint64(v)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "wordtimecheck:", err)
	os.Exit(1)
}
