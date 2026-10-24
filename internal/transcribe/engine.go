package transcribe

import (
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
func (e *engine) run(ctx context.Context, samples []float32, o runOptions, progress func(float32)) (string, []Word, error) {
	st := e.lib.initState(e.ctx)
	if st == 0 {
		return "", nil, fmt.Errorf("creating whisper state failed")
	}
	defer e.lib.freeState(st)

	p := e.lib.defaultParams(samplingGreedy)
	p.greedyBestOf = 1
	p.nThreads = int32(min(runtime.NumCPU(), 8))
	p.printProgress = false
	p.printRealtime = false
	p.printSpecial = false
	p.printTimestamps = false
	p.suppressBlank = true
	lang := "en"
	if e.multilingual {
		lang = "auto"
	}
	p.language = cstr(lang)
	if o.disfluentPrompt {
		p.initialPrompt = cstr(disfluentPrompt)
	}
	hooks := &runHooks{ctx: ctx, progress: progress}
	id := registerRun(hooks)
	defer runs.Delete(id)
	if progress != nil {
		p.progressCallback = e.lib.progressCallback
		p.progressUserData = id
	}
	if ctx.Done() != nil {
		p.abortCallback = e.lib.abortCallback
		p.abortUserData = id
	}

	status := e.lib.full(e.ctx, st, p, samples)
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}
	if status != 0 {
		return "", nil, fmt.Errorf("whisper inference failed: %d", status)
	}

	language := e.lib.langStr(e.lib.langID(st))
	if language == "" {
		language = "en"
	}
	var tokens []token
	for seg := range e.lib.nSegments(st) {
		segStart := len(tokens)
		for tok := range e.lib.nTokens(st, seg) {
			data := e.lib.tokenData(st, seg, tok)
			if data.id >= e.eot {
				continue
			}
			tokens = append(tokens, token{
				text:       strings.ToValidUTF8(string(e.lib.tokenText(e.ctx, st, seg, tok)), string(utf8.RuneError)),
				atMs:       data.tDtw*10 - e.dtwLagMs,
				p:          data.p,
				startsWord: len(tokens) == segStart,
			})
		}
	}
	words := placeWords(tokens, samples)
	for i := range words {
		words[i].StartMs += o.offsetMs
		words[i].EndMs += o.offsetMs
	}
	return language, words, nil
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

// runHooks is what whisper.cpp's callbacks reach through their user-data handle; C cannot
// hold a Go pointer, so it holds a key into runs instead.
type runHooks struct {
	ctx      context.Context
	progress func(float32)
}

var (
	runs    sync.Map
	nextRun atomic.Uintptr
)

func registerRun(h *runHooks) uintptr {
	id := nextRun.Add(1)
	runs.Store(id, h)
	return id
}

func onProgress(id uintptr, percent int32) {
	if h, ok := runs.Load(id); ok {
		h.(*runHooks).progress(float32(percent) / 100)
	}
}

func onAbort(id uintptr) bool {
	h, ok := runs.Load(id)
	return ok && h.(*runHooks).ctx.Err() != nil
}

func cstr(s string) *byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return &b[0]
}
