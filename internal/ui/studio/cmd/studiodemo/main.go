// Command studiodemo opens the Studio window on a session and logs what the preview shows, so a
// driver can check editing, playback sync and export on the real desktop. drive.ps1 beside
// it runs the acceptance pass.
//
//	studiodemo -make <dir> [-speech] [-width 1920 -height 1080 -fps 60]
//	studiodemo -open <dir> -savedir <dir>
//	studiodemo -info <dir>
//
// Run it with GLINT_DATA_DIR set to a scratch directory: presets and models land there.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"

	"github.com/wobsoriano/glint/internal/settings"
	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/studio"
	"github.com/wobsoriano/glint/internal/ui/studio/internal/synth"
)

var (
	makeDir = flag.String("make", "", "write a synthetic session to this directory and exit")
	speech  = flag.Bool("speech", true, "narrate the synthetic session with SAPI")
	width   = flag.Int("width", 1920, "synthetic session width")
	height  = flag.Int("height", 1080, "synthetic session height")
	fps     = flag.Int("fps", 60, "synthetic session frame rate")
	openDir = flag.String("open", "", "session to open")
	saveDir = flag.String("savedir", "", "where Save and Export write")
	info    = flag.String("info", "", "print the session's project as Studio reads it and exit")
)

func main() {
	flag.Parse()
	if os.Getenv("GLINT_DATA_DIR") == "" {
		log.Fatal("set GLINT_DATA_DIR to a scratch directory")
	}
	switch {
	case *makeDir != "":
		ms, err := synth.Write(*makeDir, synth.Options{Width: *width, Height: *height, FPS: *fps, Speech: *speech})
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("wrote %s: %dx%d at %d fps, %d ms\n", *makeDir, *width, *height, *fps, ms)
	case *info != "":
		if err := printInfo(*info); err != nil {
			log.Fatal(err)
		}
	case *openDir != "":
		run(*openDir)
	default:
		flag.Usage()
		os.Exit(2)
	}
}

func printInfo(dir string) error {
	s, err := core.LoadSession(dir)
	if err != nil {
		return err
	}
	p := s.Project
	out := map[string]any{
		"duration_ms": math.Round(p.DurationMs()),
		"clips":       p.Clips,
		"zooms":       p.Zooms,
		"aspect":      p.Aspect,
		"aspect_mode": p.AspectMode,
		"crop":        p.Crop,
		"padding":     p.Design.Padding,
		"radius":      p.Design.CornerRadius,
		"shadow":      p.Design.Shadow,
		"keystrokes":  p.Keystrokes,
		"captions":    p.Captions,
		"cues":        len(p.CaptionCues),
		"export":      p.Export,
		"transcript":  s.Transcript != nil,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

type host struct{ s settings.Settings }

func (h *host) Settings() *settings.Settings { return &h.s }
func (h *host) Toast(text string)            { fmt.Println("toast:", text) }
func (h *host) Reveal(path string)           { fmt.Println("reveal:", path); mygo.Shell.ShowItemInFolder(path) }
func (h *host) RecordingExported(path string) {
	fmt.Println("exported:", path)
}

func (h *host) CopyFile(path string) error {
	data, err := transfer.FileData(path)
	if err != nil {
		return err
	}
	fmt.Println("copied:", path)
	return mygo.Clipboard.Write(data)
}

func run(dir string) {
	h := &host{s: settings.Load()}
	if *saveDir != "" {
		h.s.SaveDir = *saveDir
	}
	runs := &runLog{}
	go runs.watch()
	mygo.App.WhenReady(func() {
		if studio.OpenWith(h, dir, studio.Options{OnFrame: runs.shown}) == nil {
			mygo.App.Exit(1)
		}
	})
	mygo.App.OnWindowAllClosed(func() {
		runs.end()
		mygo.App.Exit(0)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// runLog prints each paused frame and, for each run of playing frames, how far the frames
// were from the clock they followed and how evenly they came.
type runLog struct {
	mu      sync.Mutex
	drift   []float64
	at      []time.Time
	audio   bool
	first   studio.Shown
	last    studio.Shown
	lastAt  time.Time
	stamped time.Time
}

func (l *runLog) shown(s studio.Shown) {
	now := time.Now()
	if !s.Playing {
		fmt.Printf("%s shown %.0f ms (paused)\n", now.Format("15:04:05.000"), s.OutputMs)
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.drift == nil {
		l.first = s
	}
	l.drift = append(l.drift, s.OutputMs-s.ClockMs)
	l.at = append(l.at, now)
	l.audio = s.Audio
	l.last, l.lastAt = s, now
}

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

func percentile(sorted []float64, p int) float64 {
	return sorted[min(len(sorted)*p/100, len(sorted)-1)]
}

func (l *runLog) end() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.drift) < 2 {
		l.drift, l.at = nil, nil
		return
	}
	abs := make([]float64, len(l.drift))
	var sum float64
	for i, d := range l.drift {
		abs[i] = math.Abs(d)
		sum += d
	}
	slices.Sort(abs)
	gaps := make([]float64, 0, len(l.at)-1)
	for i := 1; i < len(l.at); i++ {
		gaps = append(gaps, float64(l.at[i].Sub(l.at[i-1]).Microseconds())/1000)
	}
	slices.Sort(gaps)
	wall := l.at[len(l.at)-1].Sub(l.at[0]).Seconds()
	fmt.Printf("%s run: %d frames from %.0f to %.0f ms in %.2f s = %.1f frames/s, audio clock %v; frame time minus clock: mean %.1f ms, p50 %.1f, p95 %.1f, worst %.1f ms; frame gaps p50 %.1f, p95 %.1f, max %.1f ms\n",
		time.Now().Format("15:04:05.000"), len(l.drift), l.first.OutputMs, l.last.OutputMs, wall, float64(len(l.drift)-1)/wall, l.audio,
		sum/float64(len(l.drift)), percentile(abs, 50), percentile(abs, 95), abs[len(abs)-1],
		percentile(gaps, 50), percentile(gaps, 95), gaps[len(gaps)-1])
	l.drift, l.at = nil, nil
}
