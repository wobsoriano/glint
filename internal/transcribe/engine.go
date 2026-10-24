package transcribe

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unicode"
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
}

type runOptions struct {
	offsetMs uint64
	// audioCtx is the encoder context in 20 ms frames; shortening it speeds up short windows.
	// Zero keeps the model's.
	audioCtx        int32
	disfluentPrompt bool
}

// loadEngine loads modelPath with the whisper.cpp runtime that DownloadModel installs beside
// it, using the context settings whisper-rs passes by default.
func loadEngine(modelPath string) (*engine, error) {
	lib, err := loadWhisper(RuntimeDir(filepath.Dir(modelPath)))
	if err != nil {
		return nil, err
	}
	cp := &contextParams{dtwNTop: -1, dtwMemSize: 128 << 20}
	ctx := lib.initFromFile(modelPath, cp)
	if ctx == 0 {
		return nil, fmt.Errorf("loading whisper model %s failed", modelPath)
	}
	return &engine{lib: lib, ctx: ctx, multilingual: lib.isMultilingual(ctx), eot: lib.tokenEOT(ctx)}, nil
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
	p.tokenTimestamps = true
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
	if o.audioCtx != 0 {
		p.audioCtx = o.audioCtx
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
	var words []Word
	var current *wordBuilder
	flush := func() {
		if current != nil {
			if w, ok := current.finish(); ok {
				words = append(words, w)
			}
			current = nil
		}
	}
	for seg := range e.lib.nSegments(st) {
		flush()
		for tok := range e.lib.nTokens(st, seg) {
			data := e.lib.tokenData(st, seg, tok)
			if data.id >= e.eot {
				continue
			}
			piece := strings.ToValidUTF8(string(e.lib.tokenText(e.ctx, st, seg, tok)), string(utf8.RuneError))
			if r, _ := utf8.DecodeRuneInString(piece); unicode.IsSpace(r) || current == nil {
				flush()
				current = &wordBuilder{}
			}
			current.push(piece, data.t0, data.t1, data.p, o.offsetMs)
		}
	}
	flush()
	return language, words, nil
}

type wordBuilder struct {
	text     strings.Builder
	startMs  uint64
	started  bool
	endMs    uint64
	pSum     float32
	pieceCnt uint32
}

// push adds a token; t0 and t1 arrive in whisper's 10 ms ticks.
func (w *wordBuilder) push(piece string, t0, t1 int64, p float32, offsetMs uint64) {
	w.text.WriteString(piece)
	start := offsetMs + uint64(max(t0, 0))*10
	end := offsetMs + uint64(max(t1, 0))*10
	if !w.started {
		w.startMs, w.started = start, true
	}
	w.endMs = max(w.endMs, end)
	w.pSum += p
	w.pieceCnt++
}

func (w *wordBuilder) finish() (Word, bool) {
	text := strings.TrimSpace(w.text.String())
	if !w.started || !strings.ContainsFunc(text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsNumber(r) }) {
		return Word{}, false
	}
	return Word{Text: text, StartMs: w.startMs, EndMs: max(w.endMs, w.startMs), Confidence: w.pSum / float32(w.pieceCnt)}, true
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
