package record

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/transcribe"
)

const (
	// tapRate is the rate of media.RecordOptions.MicTap's buffers.
	tapRate = 48_000
	// audioQueue is about 4 s of the mic's 20 ms buffers, enough to ride out one slow pass.
	audioQueue = 200
	// followModel is the smallest model: recognition must keep up with the speaker.
	followModel = transcribe.ModelTinyEn
)

// voice is where the teleprompter hears the narration.
type voice int

const (
	// noVoice leaves the teleprompter a static script the wheel scrolls.
	noVoice voice = iota
	micVoice
	// wavVoice plays a WAV file into the follower in real time instead of the mic, for
	// drivers (GLINT_TELEPROMPTER_WAV, as in the Rust build).
	wavVoice
)

func voiceFor(prompting, mic bool, testWAV string) voice {
	switch {
	case !prompting:
		return noVoice
	case testWAV != "":
		return wavVoice
	case mic:
		return micVoice
	}
	return noVoice
}

type followStage int

const (
	followDownloading followStage = iota
	followLoading
	followListening
	followFailed
)

type followStatus struct {
	stage followStage
	// done and total are the download's bytes while downloading.
	done, total uint64
	// err says why following failed.
	err string
}

func (s followStatus) note() string {
	switch s.stage {
	case followDownloading:
		return fmt.Sprintf("Downloading the speech model · %d%%", s.done*100/max(s.total, 1))
	case followLoading:
		return "Loading the speech model…"
	case followListening:
		return "Following your voice"
	}
	return "Voice following is unavailable · scroll to move"
}

// recognizer is live speech recognition: transcribe.LiveTranscriber.
type recognizer interface {
	Push(mono []float32)
	Poll() []transcribe.Word
	Finish() []transcribe.Word
}

// speech is how the follower gets its recognizer, swapped out by tests.
type speech struct {
	// model makes the speech model usable, downloading it if needed, and returns its path.
	model func(ctx context.Context, progress func(done, total uint64)) (string, error)
	start func(modelPath string) (recognizer, error)
}

func dataDirSpeech(dataDir string) speech {
	return speech{
		model: func(ctx context.Context, progress func(done, total uint64)) (string, error) {
			return transcribe.DownloadModel(ctx, dataDir, followModel, progress)
		},
		start: func(path string) (recognizer, error) { return transcribe.StartLive(path, tapRate) },
	}
}

// follower tracks how far through the script the narration is. Anything that goes wrong
// leaves it Failed and the teleprompter static; the recording never waits on it, since the
// mic tap drops buffers it cannot queue.
type follower struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	status  followStatus
	current int // the index of the script word last read, or -1
	redraw  func()
}

func startFollower(script string, audio <-chan []float32, sp speech) *follower {
	ctx, cancel := context.WithCancel(context.Background())
	f := &follower{ctx: ctx, cancel: cancel, done: make(chan struct{}), current: -1}
	go func() {
		defer close(f.done)
		f.run(script, audio, sp)
	}()
	return f
}

// onChange sets what runs after the status or position changes, from the follower's
// goroutine.
func (f *follower) onChange(redraw func()) {
	f.mu.Lock()
	f.redraw = redraw
	f.mu.Unlock()
}

// state is the status and the index of the script word last read, or -1.
func (f *follower) state() (followStatus, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.status, f.current
}

func (f *follower) set(update func()) {
	f.mu.Lock()
	update()
	redraw := f.redraw
	f.mu.Unlock()
	if redraw != nil {
		redraw()
	}
}

func (f *follower) stop() {
	f.onChange(nil)
	f.cancel()
}

func (f *follower) run(script string, audio <-chan []float32, sp speech) {
	f.set(func() { f.status = followStatus{stage: followDownloading, total: followModel.SizeBytes()} })
	path, err := sp.model(f.ctx, func(done, total uint64) {
		f.set(func() { f.status = followStatus{stage: followDownloading, done: done, total: total} })
	})
	if errors.Is(err, context.Canceled) {
		return
	}
	if err != nil {
		f.fail("speech model download failed: " + err.Error())
		return
	}
	f.set(func() { f.status = followStatus{stage: followLoading} })
	live, err := sp.start(path)
	if err != nil {
		f.fail(err.Error())
		return
	}
	defer live.Finish()
	drain(audio)
	f.set(func() { f.status = followStatus{stage: followListening} })
	tracker := transcribe.NewScriptTracker(script)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-f.ctx.Done():
			return
		case chunk := <-audio:
			live.Push(chunk)
			for _, c := range drain(audio) {
				live.Push(c)
			}
		case <-tick.C:
		}
		words := live.Poll()
		if len(words) == 0 {
			continue
		}
		spoken := make([]string, len(words))
		for i, w := range words {
			spoken[i] = w.Text
		}
		if i, ok := tracker.Feed(spoken); ok {
			f.set(func() { f.current = i })
		}
	}
}

func (f *follower) fail(why string) {
	f.set(func() { f.status = followStatus{stage: followFailed, err: why} })
}

// drain empties the queue without waiting, returning what it held: audio queued while the
// model loaded is stale by the time recognition starts.
func drain(audio <-chan []float32) [][]float32 {
	var out [][]float32
	for {
		select {
		case c := <-audio:
			out = append(out, c)
		default:
			return out
		}
	}
}

// feedWAV plays a WAV file into audio in 20 ms buffers at real-time pace, as a microphone
// would, until it ends or ctx is done. A full queue drops the buffer, as the mic tap does.
func feedWAV(ctx context.Context, path string, audio chan<- []float32) error {
	wav, err := media.ReadWAV(path)
	if err != nil {
		return err
	}
	mono := transcribe.Resample(transcribe.Pcm{Samples: wav.Samples, SampleRate: uint32(wav.SampleRate), Channels: uint16(wav.Channels)}.ToMono(), uint32(wav.SampleRate), tapRate)
	const step = 20 * time.Millisecond
	chunk := tapRate / 50
	start := time.Now()
	for i := 0; i*chunk < len(mono); i++ {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Until(start.Add(time.Duration(i) * step))):
		}
		select {
		case audio <- mono[i*chunk : min((i+1)*chunk, len(mono))]:
		default:
		}
	}
	return nil
}
