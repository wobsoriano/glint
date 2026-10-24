// Command trimdemo opens the Trim & Compress window on a video and logs every frame the preview
// shows, with the player's clock, so a driver can check seeking and audio and video sync on
// the real desktop. drive.ps1 runs the whole acceptance pass.
//
//	trimdemo -make clip.mp4 -seconds 20
//	trimdemo -open clip.mp4 -savedir out
//	trimdemo -check "out/clip (trimmed).mp4"
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/ui/trim"
	"github.com/wobsoriano/glint/internal/ui/trim/internal/testclip"
)

var (
	makeClip = flag.String("make", "", "write the test clip to this path and exit")
	seconds  = flag.Int("seconds", 20, "length of the test clip")
	open     = flag.String("open", "", "video to open")
	saveDir  = flag.String("savedir", "", "where Export writes (default: beside the video)")
	check    = flag.String("check", "", "print the size, length and first and last frame numbers of an export of the test clip")
	loopback = flag.Bool("loopback", false, "record the system mix and time the clip's clicks against its whole-second frames")
)

// checkExport decodes an export of the test clip. The bar codes of its first and last frames
// say which span of the source it kept.
func checkExport(path string) error {
	r, err := media.OpenVideoReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	w, h := r.Size()
	var first, last, frames int
	for ; ; frames++ {
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		n, ok := testclip.FrameNumber(f.Image, f.Image.Rect)
		if !ok {
			return fmt.Errorf("frame %d at %d ms has no code", frames, f.TMS)
		}
		if frames == 0 {
			first = n
		}
		last = n
	}
	audio := "none"
	if a, err := media.ReadMP4Audio(path); err == nil {
		audio = fmt.Sprintf("%d Hz %d ch %.3f s", a.SampleRate, a.Channels, float64(len(a.Samples)/a.Channels)/float64(a.SampleRate))
	}
	fmt.Printf("%s: %dx%d, %d ms, %d frames, first frame %d, last frame %d, audio %s\n", path, w, h, r.DurationMS(), frames, first, last, audio)
	return nil
}

func main() {
	flag.Parse()
	if *makeClip != "" {
		if err := testclip.Write(*makeClip, 1920, 1080, 30, *seconds); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote", *makeClip)
		return
	}
	if *check != "" {
		if err := checkExport(*check); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *open == "" {
		flag.Usage()
		os.Exit(2)
	}
	runs := &runLog{}
	if *loopback {
		c, err := startClicks()
		if err != nil {
			log.Fatal(err)
		}
		runs.clicks = c
	}
	go runs.watch()
	mygo.App.WhenReady(func() {
		if _, err := trim.Open(*open, trim.Config{SaveDir: *saveDir, OnFrame: runs.shown}); err != nil {
			log.Fatal(err)
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		runs.end()
		if runs.clicks != nil {
			runs.clicks.stop()
		}
		mygo.App.Exit(0)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// runLog prints each frame and, for each run of playing frames, how far the frames were
// from the clock they followed.
type runLog struct {
	mu      sync.Mutex
	drift   []float64
	audio   bool
	first   trim.Shown
	firstAt time.Time
	last    trim.Shown
	lastAt  time.Time
	// seconds holds when each whole-second frame of the run was shown.
	seconds map[int64]time.Time
	clicks  *clicks
}

func (l *runLog) shown(s trim.Shown) {
	now := time.Now()
	if !s.Playing {
		fmt.Printf("%s shown %d ms (paused)\n", now.Format("15:04:05.000"), s.FrameMS)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drift == nil {
		l.first, l.firstAt = s, now
		l.seconds = map[int64]time.Time{}
	}
	if s.FrameMS%1000 == 0 {
		l.seconds[s.FrameMS] = now
	}
	l.drift = append(l.drift, float64(s.FrameMS)-s.ClockMS)
	l.audio = s.Audio
	l.last, l.lastAt = s, now
}

// watch ends a run once no playing frame has come for 700 ms.
func (l *runLog) watch() {
	for range time.Tick(100 * time.Millisecond) {
		l.mu.Lock()
		idle := l.drift != nil && time.Since(l.lastAt) > 700*time.Millisecond
		l.mu.Unlock()
		if idle {
			l.end()
		}
	}
}

func (l *runLog) end() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drift == nil {
		return
	}
	abs := make([]float64, len(l.drift))
	var sum float64
	for i, d := range l.drift {
		abs[i] = math.Abs(d)
		sum += d
	}
	slices.Sort(abs)
	wall := l.lastAt.Sub(l.firstAt).Seconds() * 1000
	clock := l.last.ClockMS - l.first.ClockMS
	fmt.Printf("%s run: %d frames from %d to %d ms, audio clock %v; frame time minus clock: mean %.1f ms, p50 %.1f, p95 %.1f, worst %.1f ms; clock advanced %.0f ms over %.0f ms of wall time\n",
		time.Now().Format("15:04:05.000"), len(l.drift), l.first.FrameMS, l.last.FrameMS, l.audio,
		sum/float64(len(l.drift)), abs[len(abs)/2], abs[len(abs)*95/100], abs[len(abs)-1], clock, wall)
	if l.clicks != nil {
		l.clicks.report(l.seconds)
	}
	l.drift = nil
}
