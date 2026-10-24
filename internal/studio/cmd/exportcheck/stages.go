package main

import (
	"context"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"sync"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/studio"
)

type stageRun struct {
	name   string
	fps    []float64
	cores  []float64
	frames int
}

// stages times each part of an export of one session alone, with the export options its
// project holds, runs times each, so the end-to-end rate can be pinned on the slowest part.
func stages(work, src, profilePath string, runs int) error {
	var profile *os.File
	if profilePath != "" {
		f, err := os.Create(profilePath)
		if err != nil {
			return err
		}
		profile = f
	}
	dir := filepath.Join(work, "stages", filepath.Base(src))
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	if err := copyDir(src, dir); err != nil {
		return err
	}
	s, err := studio.LoadSession(dir)
	if err != nil {
		return err
	}
	p := s.Project
	w, h := p.OutputSize(s.SourceW, s.SourceH)
	fps := p.Export.Fps.Value()
	frames := int(math.Round(p.DurationMs() * float64(fps) / 1000))
	fmt.Printf("== stages of %s: %dx%d source, %d frames at %dx%d %d fps, motion blur %v, GOMAXPROCS %d\n",
		filepath.Base(filepath.Dir(src))+"/"+filepath.Base(src), s.SourceW, s.SourceH, frames, w, h, fps, p.Export.MotionBlur, runtime.GOMAXPROCS(0))

	samples, err := composedSamples(s, &p, w, h, 8)
	if err != nil {
		return err
	}
	results := []*stageRun{{name: "decode (feed + NV12 to RGBA)"}, {name: "decode + compose, 2 compositors"}, {name: "encode only (RGBA to NV12 + MF H.264)"}, {name: "full export"}}
	measure := func(r *stageRun, n int, run func() error) error {
		runtime.GC()
		cpu, t0 := processCPU(), time.Now()
		if err := run(); err != nil {
			return err
		}
		wall := time.Since(t0)
		r.fps = append(r.fps, float64(n)/wall.Seconds())
		r.cores = append(r.cores, (processCPU()-cpu).Seconds()/wall.Seconds())
		r.frames = n
		return nil
	}
	for range runs {
		if err := measure(results[0], frames, func() error { return decodeOnly(s, &p, fps, frames) }); err != nil {
			return err
		}
		if err := measure(results[1], frames, func() error { return decodeCompose(s, &p, w, h, fps, frames) }); err != nil {
			return err
		}
		if err := measure(results[2], frames, func() error { return encodeOnly(filepath.Join(dir, "encode-only.mp4"), samples, w, h, fps, frames) }); err != nil {
			return err
		}
		os.RemoveAll(s.RenderDir())
		if profile != nil {
			pprof.StartCPUProfile(profile)
		}
		if err := measure(results[3], frames, func() error {
			_, err := studio.Export(context.Background(), s, p.Export, nil)
			return err
		}); err != nil {
			return err
		}
		if profile != nil {
			pprof.StopCPUProfile()
			profile.Close()
			profile = nil
		}
	}
	for _, r := range results {
		med, lo, hi := spread(r.fps)
		cmed, _, _ := spread(r.cores)
		fmt.Printf("  %-40s median %6.1f frames/s (range %.1f to %.1f over %d runs), %.1f cores busy\n", r.name, med, lo, hi, len(r.fps), cmed)
	}
	return nil
}

func spread(v []float64) (median, lo, hi float64) {
	s := append([]float64(nil), v...)
	sort.Float64s(s)
	return s[len(s)/2], s[0], s[len(s)-1]
}

func composedSamples(s *studio.Session, p *studio.Project, w, h uint32, n int) ([]*image.NRGBA, error) {
	feed, err := studio.OpenFeed(s.Tracks.Screen)
	if err != nil {
		return nil, err
	}
	defer feed.Close()
	var out []*image.NRGBA
	for i := range n {
		t := p.DurationMs() * (float64(i) + 0.5) / float64(n)
		f, err := feed.FrameAt(p.SourceTime(t))
		if err != nil {
			return nil, err
		}
		out = append(out, studio.ComposeFrameMotionBlur(p, s.Assets, studio.FrameInputs{Screen: f.RGBA(), OutputMs: t}, w, h))
	}
	return out, nil
}

func decodeOnly(s *studio.Session, p *studio.Project, fps uint32, frames int) error {
	feed, err := studio.OpenFeed(s.Tracks.Screen)
	if err != nil {
		return err
	}
	defer feed.Close()
	for i := range frames {
		f, err := feed.FrameAt(p.SourceTime(float64(i) * 1000 / float64(fps)))
		if err != nil {
			return err
		}
		f.RGBA()
	}
	return nil
}

// decodeCompose is the export pipeline without the encoder: one decoding goroutine feeding
// two compositors, frames dropped once composed.
func decodeCompose(s *studio.Session, p *studio.Project, w, h, fps uint32, frames int) error {
	type job struct {
		ms    float64
		frame *studio.SourceFrame
	}
	jobs := make(chan job, 4)
	var decodeErr error
	go func() {
		defer close(jobs)
		feed, err := studio.OpenFeed(s.Tracks.Screen)
		if err != nil {
			decodeErr = err
			return
		}
		defer feed.Close()
		for i := range frames {
			ms := float64(i) * 1000 / float64(fps)
			f, err := feed.FrameAt(p.SourceTime(ms))
			if err != nil {
				decodeErr = err
				return
			}
			jobs <- job{ms, f}
		}
	}()
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			for j := range jobs {
				studio.RecycleFrame(studio.ComposeFrameMotionBlur(p, s.Assets, studio.FrameInputs{Screen: j.frame.RGBA(), OutputMs: j.ms}, w, h))
			}
		})
	}
	wg.Wait()
	return decodeErr
}

func encodeOnly(path string, samples []*image.NRGBA, w, h, fps uint32, frames int) error {
	writer, err := media.CreateVideoWriter(path, media.VideoParams{Width: int(w), Height: int(h), FPS: int(fps), Bitrate: studio.Bitrate(w, h, fps, studio.QualityHigh)}, nil)
	if err != nil {
		return err
	}
	defer writer.Close()
	started := time.Now()
	for i := range frames {
		f := samples[i%len(samples)]
		if err := writer.WriteFrame(&image.RGBA{Pix: f.Pix, Stride: f.Stride, Rect: f.Rect}); err != nil {
			return err
		}
	}
	queued := time.Since(started)
	sum, err := writer.Finish()
	fmt.Printf("    encode only: frames queued in %v, finish drained for %v, hardware encoder %v\n",
		queued.Round(time.Millisecond), (time.Since(started) - queued).Round(time.Millisecond), sum.Hardware)
	return err
}
