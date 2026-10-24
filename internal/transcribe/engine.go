package transcribe

import (
	"cmp"
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// Whisper drops hesitations unless the prompt shows them, so the prompt is written in the
// disfluent style we want transcribed.
const disfluentPrompt = "Um, so, uh, I was like, you know, thinking. Hmm, okay."

type engine struct {
	lib          *whisperLib
	ctx          uintptr
	multilingual bool
	eot          int32
	dtwLagMs     int64
}

type runOptions struct {
	offsetMs        uint64
	disfluentPrompt bool
}

// loadEngine loads modelPath, one of the models DownloadModel installs, with the whisper.cpp
// runtime beside it. Words are timed by DTW over the model's alignment heads, because the
// timestamp tokens that whisper.cpp's other token timing builds on are often missing or
// seconds off.
func loadEngine(modelPath string) (*engine, error) {
	var info modelInfo
	for _, m := range models {
		if m.file == filepath.Base(modelPath) {
			info = m
		}
	}
	if info.aheads == 0 {
		return nil, fmt.Errorf("%s is not a model Glint downloads", modelPath)
	}
	lib, err := loadWhisper(RuntimeDir(filepath.Dir(modelPath)))
	if err != nil {
		return nil, err
	}
	cp := &contextParams{dtwTokenTimestamps: true, dtwAheadsPreset: info.aheads, dtwNTop: -1, dtwMemSize: 128 << 20}
	ctx := lib.initFromFile(modelPath, cp)
	if ctx == 0 {
		return nil, fmt.Errorf("loading whisper model %s failed", modelPath)
	}
	return &engine{lib: lib, ctx: ctx, multilingual: lib.isMultilingual(ctx), eot: lib.tokenEOT(ctx), dtwLagMs: info.dtwLagMs}, nil
}

func (e *engine) close() { e.lib.free(e.ctx) }

// run recognizes 16 kHz mono samples. A cancelled ctx stops inference at whisper.cpp's next
// abort check and returns ctx.Err().
//
// whisper.cpp's own 30 s windows lose speech two ways. When a window's decode ends on a lone
// timestamp, whisper.cpp skips the rest of the window, though the model may have stopped a
// sentence or more early. And DTW aligns only up to that timestamp, squeezing later words
// against it when it comes early. So run hands whisper.cpp one window at a time, cut at
// pauses, decodes each as a single segment so DTW spans all of it, and decodes again whatever
// sound a window's text left out.
func (e *engine) run(ctx context.Context, samples []float32, o runOptions, progress func(float32)) (string, []Word, error) {
	st := e.lib.initState(e.ctx)
	if st == 0 {
		return "", nil, fmt.Errorf("creating whisper state failed")
	}
	defer e.lib.freeState(st)
	if progress == nil {
		progress = func(float32) {}
	}
	levels := frameLevels(samples)
	r := &recognition{e: e, ctx: ctx, st: st, samples: samples, frames: len(levels),
		runs: soundRuns(levels, noiseFloor(levels)), prompt: o.disfluentPrompt, progress: progress}
	if !e.multilingual {
		r.language = "en"
	}
	if err := r.decodeRuns(0, len(r.runs)); err != nil {
		return "", nil, err
	}
	progress(1)
	words := placeWords(r.tokens, samples)
	for i := range words {
		words[i].StartMs += o.offsetMs
		words[i].EndMs += o.offsetMs
	}
	return cmp.Or(r.language, "en"), words, nil
}

// recognition is one run over a recording: its sound, and the text tokens decoded so far, in
// spoken order and timed from the start of the recording. An empty language is detected by
// the first decode and kept for the rest.
type recognition struct {
	e        *engine
	ctx      context.Context
	st       uintptr
	samples  []float32
	frames   int
	runs     []soundRun
	prompt   bool
	language string
	tokens   []token
	doneTo   int
	progress func(float32)
}

// decodeRuns decodes runs[lo:hi] a window at a time.
func (r *recognition) decodeRuns(lo, hi int) error {
	for lo < hi {
		n, from, to := nextChunk(r.runs, lo, hi, r.frames)
		tokens, err := r.decode(from, to)
		if err != nil {
			return err
		}
		var at []int64
		for _, t := range tokens {
			if letters(t.text) > 0 {
				at = append(at, t.atMs)
			}
		}
		done, holes := unheard(r.runs[lo:lo+n], at)
		if len(holes) == 0 {
			r.tokens = append(r.tokens, tokens...)
		} else {
			// DTW spreads the words of a decode that skipped speech over the skipped sound, so
			// its words are dropped and the runs decoded again in pieces split at the holes.
			start := lo
			for _, h := range holes {
				if err := r.decodeRuns(start, lo+h[0]); err != nil {
					return err
				}
				if err := r.decodeRuns(lo+h[0], lo+h[1]); err != nil {
					return err
				}
				start = lo + h[1]
			}
			if err := r.decodeRuns(start, lo+done); err != nil {
				return err
			}
		}
		lo += done
		r.doneTo = max(r.doneTo, r.runs[lo-1].end)
		r.progress(float32(r.doneTo) / float32(r.frames))
	}
	return nil
}

const frameSamples = WhisperSampleRate * frameMs / 1000

// decode runs whisper.cpp over frames [from, to), at most one 30 s window, and returns the
// tokens of its spoken text.
func (r *recognition) decode(from, to int) ([]token, error) {
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	e := r.e
	p := e.lib.defaultParams(samplingGreedy)
	p.greedyBestOf = 1
	p.nThreads = int32(min(runtime.NumCPU(), 8))
	p.printProgress = false
	p.printRealtime = false
	p.printSpecial = false
	p.printTimestamps = false
	p.suppressBlank = true
	p.singleSegment = true
	p.language = cstr(cmp.Or(r.language, "auto"))
	if r.prompt {
		p.initialPrompt = cstr(disfluentPrompt)
	}
	id := nextDecode.Add(1)
	decodes.Store(id, r.ctx)
	defer decodes.Delete(id)
	if r.ctx.Done() != nil {
		p.abortCallback = e.lib.abortCallback
		p.abortUserData = id
	}

	status := e.lib.full(e.ctx, r.st, p, r.samples[from*frameSamples:min(to*frameSamples, len(r.samples))])
	if err := r.ctx.Err(); err != nil {
		return nil, err
	}
	if status != 0 {
		return nil, fmt.Errorf("whisper inference failed: %d", status)
	}
	if r.language == "" {
		r.language = e.lib.langStr(e.lib.langID(r.st))
	}
	var tokens []token
	for seg := range e.lib.nSegments(r.st) {
		segStart := len(tokens)
		for tok := range e.lib.nTokens(r.st, seg) {
			data := e.lib.tokenData(r.st, seg, tok)
			if data.id >= e.eot {
				continue
			}
			tokens = append(tokens, token{
				text:       strings.ToValidUTF8(string(e.lib.tokenText(e.ctx, r.st, seg, tok)), string(utf8.RuneError)),
				atMs:       int64(from)*frameMs + data.tDtw*10 - e.dtwLagMs,
				p:          data.p,
				startsWord: len(tokens) == segStart,
			})
		}
	}
	return dropAnnotations(tokens), nil
}

// Transcribe recognizes a whole recording with a model file from DownloadModel. It blocks, so
// run it on a background goroutine. progress receives 0..=1. Cancelling ctx stops inference
// and returns ctx.Err().
func Transcribe(ctx context.Context, modelPath string, pcm Pcm, progress func(float32)) (Transcript, error) {
	e, err := loadEngine(modelPath)
	if err != nil {
		return Transcript{}, err
	}
	defer e.close()
	if err := ctx.Err(); err != nil {
		return Transcript{}, err
	}
	if progress == nil {
		progress = func(float32) {}
	}
	language, words, err := e.run(ctx, ToWhisperRate(pcm), runOptions{disfluentPrompt: true}, progress)
	if err != nil {
		return Transcript{}, err
	}
	return NewTranscript(language, words), nil
}

// whisper.cpp's abort callback reaches a decode's context through its user-data handle; C
// cannot hold a Go pointer, so it holds a key into decodes instead.
var (
	decodes    sync.Map
	nextDecode atomic.Uintptr
)

func onAbort(id uintptr) bool {
	ctx, ok := decodes.Load(id)
	return ok && ctx.(context.Context).Err() != nil
}

func cstr(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}
