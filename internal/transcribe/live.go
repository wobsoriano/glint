package transcribe

import (
	"context"
	"sync"
	"time"
)

const (
	liveWindowMs   = 8_000
	liveStepMs     = 1_000
	liveEdgeHoldMs = 250
)

func whisperSamples(ms uint64) int { return int(ms * WhisperSampleRate / 1000) }

// LiveTranscriber is near-real-time recognition for the teleprompter. Feed mic audio with
// Push; a worker goroutine re-recognizes the last few seconds about once a second, and Poll
// returns words it has not returned before, timed from the first pushed sample.
//
// When inference falls behind real time the worker jumps to the newest audio, so words in the
// skipped stretch are lost rather than delivered late.
type LiveTranscriber struct {
	sampleRate uint32
	wake       chan struct{}
	done       chan struct{}

	mu     sync.Mutex
	queued [][]float32
	closed bool
	words  []Word
}

// StartLive loads the model (ModelTinyEn for speed) and starts the worker. sampleRate is the
// rate of the mono chunks passed to Push.
func StartLive(modelPath string, sampleRate uint32) (*LiveTranscriber, error) {
	e, err := loadEngine(modelPath)
	if err != nil {
		return nil, err
	}
	l := &LiveTranscriber{sampleRate: sampleRate, wake: make(chan struct{}, 1), done: make(chan struct{})}
	go func() {
		defer close(l.done)
		defer e.close()
		(&liveWorker{engine: e, out: l}).run()
	}()
	return l, nil
}

func (l *LiveTranscriber) Push(mono []float32) {
	chunk := Resample(mono, l.sampleRate, WhisperSampleRate)
	l.mu.Lock()
	if !l.closed {
		l.queued = append(l.queued, chunk)
	}
	l.mu.Unlock()
	l.signal()
}

// Poll returns the words recognized since the last call, in spoken order.
func (l *LiveTranscriber) Poll() []Word {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.words
	l.words = nil
	return w
}

// Finish stops input, waits for a last pass over the remaining audio, and returns its new
// words. Calling it again returns nothing.
func (l *LiveTranscriber) Finish() []Word {
	l.mu.Lock()
	l.closed = true
	l.mu.Unlock()
	l.signal()
	<-l.done
	return l.Poll()
}

func (l *LiveTranscriber) signal() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// take hands the worker everything pushed so far and whether input has ended.
func (l *LiveTranscriber) take() ([][]float32, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	q := l.queued
	l.queued = nil
	return q, l.closed
}

func (l *LiveTranscriber) emit(words []Word) {
	l.mu.Lock()
	l.words = append(l.words, words...)
	l.mu.Unlock()
}

type liveWorker struct {
	engine         *engine
	out            *LiveTranscriber
	window         []float32
	totalSamples   int
	pendingSamples int
	emittedUntilMs uint64
}

func (w *liveWorker) run() {
	timer := time.NewTimer(50 * time.Millisecond)
	defer timer.Stop()
	for {
		select {
		case <-w.out.wake:
		case <-timer.C:
		}
		timer.Reset(50 * time.Millisecond)
		chunks, closed := w.out.take()
		for _, c := range chunks {
			w.append(c)
		}
		if closed {
			if w.pendingSamples > 0 {
				w.pass(true)
			}
			return
		}
		if w.pendingSamples >= whisperSamples(liveStepMs) {
			w.pass(false)
		}
	}
}

func (w *liveWorker) append(chunk []float32) {
	w.totalSamples += len(chunk)
	w.pendingSamples += len(chunk)
	w.window = append(w.window, chunk...)
	if excess := len(w.window) - whisperSamples(liveWindowMs); excess > 0 {
		w.window = append(w.window[:0], w.window[excess:]...)
	}
}

func (w *liveWorker) pass(last bool) {
	w.pendingSamples = 0
	offsetMs := uint64(w.totalSamples-len(w.window)) * 1000 / WhisperSampleRate
	windowMs := uint64(len(w.window)) * 1000 / WhisperSampleRate
	o := runOptions{offsetMs: offsetMs, audioCtx: int32(min(windowMs/20+64, 1500))}
	_, words, err := w.engine.run(context.Background(), w.window, o, nil)
	if err != nil {
		return
	}
	edgeMs := offsetMs + windowMs
	var fresh []Word
	for _, word := range words {
		if (word.StartMs+word.EndMs)/2 <= w.emittedUntilMs {
			continue
		}
		if !last && word.EndMs+liveEdgeHoldMs >= edgeMs {
			break
		}
		fresh = append(fresh, word)
	}
	if len(fresh) > 0 {
		w.emittedUntilMs = fresh[len(fresh)-1].EndMs
		w.out.emit(fresh)
	}
}
