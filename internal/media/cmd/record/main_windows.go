// Command record records a session package the way the app does: it reserves a pending
// recording in a Library, records screen.mp4 with the system, microphone and input tracks
// beside it, finishes the recording into <id>.glintrec, then checks what it wrote. It
// opens an animated stage window under the pointer to record (it never moves the pointer
// or types), plays tone bursts that the stage flashes with, and verifies size, constant
// frame rate, duration, motion, cursor absence, capture exclusion, A/V sync, pauses,
// pointer mapping and that Studio opens the session.
//
//	go run ./internal/media/cmd/record -source display|area|window -seconds 5 [-fps 60] [-system] [-mic]
package main

import (
	"errors"
	"flag"
	"fmt"
	"image"
	"log"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/media/internal/testwin"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/store"
	"golang.org/x/sys/windows"
)

type config struct {
	source    string
	seconds   float64
	fps       int
	system    bool
	mic       bool
	out       string
	pauseAt   time.Duration
	pauseFor  time.Duration
	restartAt time.Duration
	cancel    bool
	move      bool
	tones     bool
	keep      bool
	dump      string
	exclude   bool
}

func main() {
	var c config
	flag.StringVar(&c.source, "source", "display", "display, area or window")
	flag.Float64Var(&c.seconds, "seconds", 5, "media seconds to record, pauses excluded")
	flag.IntVar(&c.fps, "fps", 30, "30 or 60")
	flag.BoolVar(&c.system, "system", false, "record system audio")
	flag.BoolVar(&c.mic, "mic", false, "record the default microphone")
	flag.StringVar(&c.out, "out", "", "Library root to record into (default: a new temp dir; never %APPDATA%\\Glint)")
	flag.DurationVar(&c.pauseAt, "pause-at", 0, "pause after this much wall time (0: never)")
	flag.DurationVar(&c.pauseFor, "pause-for", time.Second, "how long the pause lasts")
	flag.DurationVar(&c.restartAt, "restart-at", 0, "discard the recording and start again after this much media time, as Restart does")
	flag.BoolVar(&c.cancel, "cancel", false, "delete the recording at the end instead of finishing it, as Delete does")
	flag.BoolVar(&c.move, "move", true, "with -source window, move the stage while recording (during the pause when there is one)")
	flag.BoolVar(&c.tones, "tones", true, "with -system, play tone bursts the stage flashes with")
	flag.BoolVar(&c.keep, "keep", true, "keep the Library after checking it")
	flag.StringVar(&c.dump, "dump", "", "save the first frames whose barcode does not read as PNGs here")
	flag.BoolVar(&c.exclude, "exclude", true, "exclude the magenta window from capture; false is the control that shows the check sees it")
	flag.Parse()
	if err := run(c); err != nil {
		log.Fatal(err)
	}
}

var (
	procSetProcessDpiAwarenessContext = windows.NewLazySystemDLL("user32.dll").NewProc("SetProcessDpiAwarenessContext")
	procGetCursorPos                  = windows.NewLazySystemDLL("user32.dll").NewProc("GetCursorPos")
)

func cursorPos() image.Point {
	var p struct{ X, Y int32 }
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	return image.Pt(int(p.X), int(p.Y))
}

// stage is the window under test and where its content lands in the video.
type stage struct {
	win     *testwin.Window
	flash   atomic.Bool
	painted atomic.Int64
	// origin is the stage's top-left in video pixels. It stays put even while a recorded
	// window moves on the desktop.
	origin image.Point
	// captureOrigin is the desktop position of video pixel (0, 0) while the stage's
	// top-left is at stageMin.
	captureOrigin func(stageMin image.Point) image.Point
	// rate is how many frames per second the stage painted while recording.
	rate float64

	rec atomic.Pointer[media.Recorder]

	mu    sync.Mutex
	trace []sample
	// soundAt and paintAt are the media times at which playback reached each burst and the
	// stage first painted its flash, which split an A/V offset into its audio and video parts.
	soundAt, paintAt []int64
	// loopback holds what a second loopback capture delivered, and loopbackEnd the media
	// time at which each buffer's last sample arrived.
	loopback    []float32
	loopbackEnd []bufferEnd
}

type bufferEnd struct {
	frames int
	tms    float64
}

func (st *stage) tapped(buf []float32) {
	rec := st.rec.Load()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.loopback = append(st.loopback, buf...)
	st.loopbackEnd = append(st.loopbackEnd, bufferEnd{len(st.loopback) / 2, float64(rec.Elapsed().Microseconds()) / 1000})
}

// mixedAt is the media time the loopback sample at frame f arrived, less the time the
// rest of its buffer took to play.
func (st *stage) mixedAt(f int) float64 {
	for _, e := range st.loopbackEnd {
		if e.frames > f {
			return e.tms - float64(e.frames-f)*1000/48000
		}
	}
	return math.NaN()
}

func (st *stage) log(to *[]int64) {
	rec := st.rec.Load()
	if rec == nil || rec.IsPaused() {
		return
	}
	st.mu.Lock()
	*to = append(*to, rec.Elapsed().Milliseconds())
	st.mu.Unlock()
}

// sample is where the pointer and the stage were at one media time. The person at the
// desktop may move the pointer at any moment, so every check compares against this trace
// rather than against where the pointer was when the tool started.
type sample struct {
	tms    int64
	paused bool
	cursor image.Point
	stage  image.Point
}

func (st *stage) record(rec *media.Recorder, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		s := sample{tms: rec.Elapsed().Milliseconds(), paused: rec.IsPaused(), cursor: cursorPos(), stage: st.win.Rect().Min}
		st.mu.Lock()
		st.trace = append(st.trace, s)
		st.mu.Unlock()
		time.Sleep(time.Millisecond)
	}
}

// stableAt is the pointer and stage position throughout media times [from, to], when
// neither moved then.
func (st *stage) stableAt(from, to int64) (sample, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	var got sample
	n := 0
	for _, s := range st.trace {
		if s.tms < from || s.tms > to {
			continue
		}
		if n > 0 && (s.cursor != got.cursor || s.stage != got.stage) {
			return sample{}, false
		}
		got = s
		n++
	}
	return got, n > 0
}

// inVideo is where desktop point p shows in the video when the trace says s.
func (st *stage) inVideo(p image.Point, s sample) image.Point {
	return p.Sub(st.captureOrigin(s.stage))
}

func run(c config) error {
	// The app runs per-monitor DPI aware (MyGo sets it at startup), so the tool does too.
	procSetProcessDpiAwarenessContext.Call(^uintptr(3))
	if c.fps != 30 && c.fps != 60 {
		return fmt.Errorf("-fps must be 30 or 60")
	}
	root := c.out
	if root == "" {
		var err error
		if root, err = os.MkdirTemp("", "glint-record-"); err != nil {
			return err
		}
	}
	if strings.Contains(strings.ToLower(root), `appdata\roaming\glint`) {
		return errors.New("refusing to record into the real Library")
	}
	lib := store.New(root)

	displays, err := platform.Displays()
	if err != nil {
		return err
	}
	cur := cursorPos()
	d := displays[0]
	for _, x := range displays {
		if x.Bounds.Contains(cur.X, cur.Y) {
			d = x
		}
	}
	db := image.Rect(d.Bounds.X, d.Bounds.Y, d.Bounds.X+d.Bounds.W, d.Bounds.Y+d.Bounds.H)
	place := image.Rectangle{Min: cur.Sub(cursorAt)}
	place.Max = place.Min.Add(image.Pt(stageW, stageH))
	place = clampInto(place, db.Inset(40))
	st := &stage{}
	lit := false
	st.win, err = testwin.Open(place, testwin.Options{Title: "Glint record stage", Paint: func(cv *testwin.Canvas, frame int64) {
		on := st.flash.Load()
		paintStage(cv, frame, on)
		if on && !lit {
			st.log(&st.paintAt)
		}
		lit = on
		st.painted.Store(frame)
	}})
	if err != nil {
		return err
	}
	defer st.win.Close()
	excluded, err := testwin.Open(excludeRect.Add(place.Min), testwin.Options{Title: "Glint excluded window", Fill: magenta, ExcludeFromCapture: c.exclude})
	if err != nil {
		return err
	}
	defer excluded.Close()
	time.Sleep(300 * time.Millisecond)

	var src media.Source
	var size image.Point
	switch c.source {
	case "display":
		src, st.origin, size = media.DisplaySource{Display: d.ID}, place.Min.Sub(db.Min), db.Size()
		st.captureOrigin = func(image.Point) image.Point { return db.Min }
	case "area":
		area := place.Inset(-24).Intersect(db)
		area.Max = area.Min.Add(image.Pt(area.Dx()&^1, area.Dy()&^1))
		src = media.AreaSource{Display: d.ID, Rect: platform.Rect{X: area.Min.X, Y: area.Min.Y, W: area.Dx(), H: area.Dy()}}
		st.origin, size = place.Min.Sub(area.Min), area.Size()
		st.captureOrigin = func(image.Point) image.Point { return area.Min }
	case "window":
		src, st.origin, size = media.WindowSource{Window: int64(st.win.HWND)}, image.Point{}, place.Size()
		st.captureOrigin = func(stageMin image.Point) image.Point { return stageMin }
	default:
		return fmt.Errorf("-source must be display, area or window")
	}
	fmt.Printf("display %s %v scale %.2f; stage at %v; cursor at %v\n", d.Name, d.Bounds, d.Scale, place, cur)

	opts := media.RecordOptions{Source: src, FPS: media.FPS(c.fps), SystemAudio: c.system, RecordInput: true, IgnoredKeys: []string{"Ctrl+Shift+7"}}
	if c.mic {
		def := capture.DeviceID("")
		opts.Mic = &def
	}
	length := time.Duration(c.seconds * float64(time.Second))
	plan := schedule{media: length, pauseAt: c.pauseAt, pauseFor: c.pauseFor}

	pending, rec, err := begin(lib, opts)
	if err != nil {
		return err
	}
	if c.restartAt > 0 {
		for rec.Elapsed() < c.restartAt {
			time.Sleep(5 * time.Millisecond)
		}
		rec.Cancel()
		if _, err := os.Stat(pending.Dir); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("restart left %s behind: %v", pending.Dir, err)
		}
		fmt.Printf("restart: discarded %s\n", pending.Dir)
		if pending, rec, err = begin(lib, opts); err != nil {
			return err
		}
	}
	st.rec.Store(rec)
	if c.system && c.tones {
		// A second loopback track whose clock never runs writes nothing, but its tap hands
		// over every buffer as it arrives: stamping each with media time places the bursts
		// as the audio engine mixed them, independently of system.wav.
		tap := make(chan []float32, 4096)
		probe, err := capture.StartTrack(capture.Loopback, filepath.Join(os.TempDir(), "glint-record-tap.wav"), 2, capture.NewClock(), tap)
		if err != nil {
			return err
		}
		tapDone := make(chan struct{})
		go func() {
			defer close(tapDone)
			for buf := range tap {
				st.tapped(buf)
			}
		}()
		defer func() {
			probe.Stop()
			close(tap)
			<-tapDone
		}()
		plan.bursts = burstTimes(length, c.pauseAt, c.pauseFor)
		tone, err := playTones(length+c.pauseFor+2*time.Second, plan.bursts, 200*time.Millisecond)
		if err != nil {
			return err
		}
		stop, done := make(chan struct{}), make(chan struct{})
		go func() {
			defer close(done)
			for was := false; ; time.Sleep(time.Millisecond) {
				select {
				case <-stop:
					return
				default:
				}
				on := tone.sounding()
				if on && !was {
					st.log(&st.soundAt)
				}
				st.flash.Store(on)
				was = on
			}
		}()
		defer func() {
			close(stop)
			<-done
			tone.close()
		}()
	}
	moved := drive(rec, plan, st, c)
	if c.cancel {
		rec.Cancel()
		if _, err := os.Stat(pending.Dir); !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cancel left %s behind: %v", pending.Dir, err)
		}
		fmt.Printf("cancel: %s deleted\n", pending.Dir)
		return nil
	}
	sum := rec.Stop()
	fmt.Printf("summary: %dx%d, %d ms, %d bytes, hardware %v, screen %+v\n", sum.Width, sum.Height, sum.DurationMS, sum.Bytes, sum.Hardware, sum.Screen)
	for _, w := range sum.Warnings {
		fmt.Printf("warning: %s\n", w)
	}
	var first image.Image
	if vr, err := media.OpenVideoReader(sum.Files[0]); err == nil {
		if f, err := vr.NextFrame(); err == nil {
			first = f.Image
		}
		vr.Close()
	}
	item, err := lib.FinishRecording(pending, store.RecordingInfo{Width: sum.Width, Height: sum.Height, DurationMs: sum.DurationMS, Source: "Glint record stage"}, first)
	if err != nil {
		return err
	}
	fmt.Printf("session: %s\n", item.Dir)
	if !c.keep {
		defer os.RemoveAll(root)
	}
	return check(item.Dir, sum, size, c, plan, st, moved)
}

func begin(lib store.Library, opts media.RecordOptions) (store.PendingRecording, *media.Recorder, error) {
	pending, err := lib.BeginRecording(time.Now())
	if err != nil {
		return pending, nil, err
	}
	opts.SessionDir = pending.Dir
	rec, err := media.StartRecorder(opts)
	if err != nil {
		os.RemoveAll(pending.Dir)
		return pending, nil, err
	}
	for _, w := range rec.Warnings() {
		fmt.Printf("started with warning: %s\n", w)
	}
	return pending, rec, nil
}

func clampInto(r, bounds image.Rectangle) image.Rectangle {
	d := image.Point{}
	if r.Min.X < bounds.Min.X {
		d.X = bounds.Min.X - r.Min.X
	} else if r.Max.X > bounds.Max.X {
		d.X = bounds.Max.X - r.Max.X
	}
	if r.Min.Y < bounds.Min.Y {
		d.Y = bounds.Min.Y - r.Min.Y
	} else if r.Max.Y > bounds.Max.Y {
		d.Y = bounds.Max.Y - r.Max.Y
	}
	return r.Add(d)
}

func paintStage(cv *testwin.Canvas, frame int64, flash bool) {
	cv.Fill(image.Rect(0, 0, cv.W, cv.H), stageBG)
	cv.Fill(zoneRect, zoneColour)
	for i := range 2 * barcodeBits {
		bit := frame >> (i % barcodeBits) & 1
		if i >= barcodeBits {
			bit ^= 1
		}
		col := black
		if bit == 1 {
			col = white
		}
		cv.Fill(barcodeCell(i), col)
	}
	cv.Fill(boxRect(frame), boxColour)
	col := black
	if flash {
		col = white
	}
	cv.Fill(flashRect, col)
}

// schedule is when things happen, in wall time from the start of recording.
type schedule struct {
	media    time.Duration
	pauseAt  time.Duration
	pauseFor time.Duration
	bursts   []time.Duration
}

// burstTimes puts a burst every 1.5 s of wall time from 1 s on, until 1 s before the end.
func burstTimes(media, pauseAt, pauseFor time.Duration) []time.Duration {
	var out []time.Duration
	end := media
	if pauseAt > 0 {
		end += pauseFor
	}
	for t := time.Second; t < end-time.Second; t += 1500 * time.Millisecond {
		out = append(out, t)
	}
	return out
}

// moveInfo records how the stage was moved during a window recording.
type moveInfo struct {
	delta image.Point
	// atMS and backMS are the media times of the move and the move back; both are the
	// pause's media time when it happened while paused.
	atMS, backMS int64
	duringPause  bool
}

func drive(rec *media.Recorder, p schedule, st *stage, c config) *moveInfo {
	start := time.Now()
	painted := st.painted.Load()
	stop := make(chan struct{})
	go st.record(rec, stop)
	defer func() {
		close(stop)
		st.rate = float64(st.painted.Load()-painted) / time.Since(start).Seconds()
	}()
	var mv *moveInfo
	home := st.win.Rect()
	shift := func(d image.Point) { st.win.SetRect(home.Add(d)) }
	paused := false
	for rec.Elapsed() < p.media && rec.IsLive() {
		wall := time.Since(start)
		if p.pauseAt > 0 && !paused && wall >= p.pauseAt {
			paused = true
			rec.Pause()
			pausedAt := rec.Elapsed().Milliseconds()
			if c.source == "window" && c.move {
				time.Sleep(p.pauseFor / 3)
				mv = &moveInfo{delta: image.Pt(60, 40), atMS: pausedAt, backMS: pausedAt, duringPause: true}
				shift(mv.delta)
				time.Sleep(p.pauseFor / 3)
				shift(image.Point{})
				time.Sleep(p.pauseFor - 2*(p.pauseFor/3))
			} else {
				time.Sleep(p.pauseFor)
			}
			rec.Resume()
			fmt.Printf("paused at %d ms media for %v\n", pausedAt, p.pauseFor)
			continue
		}
		if c.source == "window" && c.move && p.pauseAt == 0 {
			el := rec.Elapsed()
			if mv == nil && el >= p.media*2/5 {
				mv = &moveInfo{delta: image.Pt(40, 30), atMS: el.Milliseconds()}
				shift(mv.delta)
			} else if mv != nil && mv.backMS == 0 && el >= p.media*7/10 {
				mv.backMS = el.Milliseconds()
				shift(image.Point{})
			}
		}
		time.Sleep(2 * time.Millisecond)
	}
	return mv
}
