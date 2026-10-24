package media

import (
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media/internal/mf"
)

// TestThroughput measures encode and decode frames per second at 1080p and 4K, on the GPU
// and in software, alternating the two so machine noise hits both. It takes a few minutes;
// set GLINT_MEDIA_BENCH=1 to run it.
func TestThroughput(t *testing.T) {
	if os.Getenv("GLINT_MEDIA_BENCH") == "" {
		t.Skip("set GLINT_MEDIA_BENCH=1 to measure throughput")
	}
	needFFmpeg(t)
	runs := 5
	if n, err := strconv.Atoi(os.Getenv("GLINT_MEDIA_BENCH_RUNS")); err == nil {
		runs = n
	}
	sizes := []struct {
		name         string
		w, h, frames int
		pool         int
	}{
		{"1080p", 1920, 1080, 300, 30},
		{"2160p", 3840, 2160, 240, 10},
	}
	dir := outDir(t)
	for _, s := range sizes {
		pool := make([]*image.RGBA, s.pool)
		for i := range pool {
			pool[i] = pattern(i*3, s.w, s.h)
		}
		results := map[bool][]encodeRun{}
		for run := 0; run < runs; run++ {
			for _, software := range []bool{false, true} {
				path := filepath.Join(dir, fmt.Sprintf("bench-%s-%s-%d.mp4", s.name, kind(software), run))
				r := encodeOnce(t, path, s.w, s.h, s.frames, pool, software)
				if r.hardware == software {
					t.Fatalf("%s software=%v ran with hardware=%v", s.name, software, r.hardware)
				}
				results[software] = append(results[software], r)
			}
		}
		for _, software := range []bool{false, true} {
			rs := results[software]
			path := filepath.Join(dir, fmt.Sprintf("bench-%s-%s-%d.mp4", s.name, kind(software), runs-1))
			p := ffprobe(t, path)
			if got := p.Streams[p.stream("video")].NbFrames; got != strconv.Itoa(s.frames) {
				t.Errorf("%s software=%v wrote %s frames, want %d", s.name, software, got, s.frames)
			}
			whole, frames, create := spread(rs, func(r encodeRun) float64 { return float64(s.frames) / r.total.Seconds() }),
				spread(rs, func(r encodeRun) float64 { return float64(s.frames) / (r.write + r.finish).Seconds() }),
				spread(rs, func(r encodeRun) float64 { return float64(r.create.Milliseconds()) })
			t.Logf("encode %s %-8s, %d runs of %d frames: whole file %s fps; frames only (WriteFrame+Finish) %s fps; encoder start %s ms",
				s.name, kind(software), len(rs), s.frames, whole, frames, create)
		}
		hw := filepath.Join(dir, fmt.Sprintf("bench-%s-%s-%d.mp4", s.name, kind(false), runs-1))
		dec := map[bool][]float64{}
		for run := 0; run < runs; run++ {
			for _, gpu := range []bool{true, false} {
				dec[gpu] = append(dec[gpu], decodeOnce(t, hw, gpu, s.frames))
			}
		}
		for _, gpu := range []bool{true, false} {
			t.Logf("decode %s %-8s, %d runs: %s fps to NV12, open included",
				s.name, map[bool]string{true: "GPU", false: "software"}[gpu], len(dec[gpu]), spread(dec[gpu], func(v float64) float64 { return v }))
		}
	}
}

func kind(software bool) string {
	if software {
		return "software"
	}
	return "hardware"
}

// spread is "median (min..max)" of f over the runs.
func spread[T any](runs []T, f func(T) float64) string {
	v := make([]float64, len(runs))
	for i, r := range runs {
		v[i] = f(r)
	}
	slices.Sort(v)
	return fmt.Sprintf("%.1f (%.1f..%.1f)", v[len(v)/2], v[0], v[len(v)-1])
}

type encodeRun struct {
	total, create, write, finish time.Duration
	hardware                     bool
}

func encodeOnce(t *testing.T, path string, w, h, frames int, pool []*image.RGBA, software bool) encodeRun {
	start := time.Now()
	vw, err := CreateVideoWriter(path, VideoParams{Width: w, Height: h, FPS: 30, Bitrate: RecordingBitrate(w, h, 30), Software: software}, nil)
	if err != nil {
		t.Fatal(err)
	}
	create := time.Since(start)
	var write time.Duration
	for i := 0; i < frames; i++ {
		t0 := time.Now()
		if err := vw.WriteFrame(pool[i%len(pool)]); err != nil {
			t.Fatal(err)
		}
		write += time.Since(t0)
	}
	f0 := time.Now()
	sum, err := vw.Finish()
	if err != nil {
		t.Fatal(err)
	}
	total := time.Since(start)
	if sum.Frames != int64(frames) {
		t.Fatalf("wrote %d frames, want %d", sum.Frames, frames)
	}
	return encodeRun{total: total, create: create, write: write, finish: time.Since(f0), hardware: sum.Hardware}
}

func decodeOnce(t *testing.T, path string, gpu bool, want int) float64 {
	var fr *FrameReader
	var err error
	start := time.Now()
	if gpu {
		fr, err = OpenFrameReader(path)
	} else {
		fr, err = openSoftware(path)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()
	n := 0
	for {
		f, err := fr.NextFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		f.Release()
		n++
	}
	if n != want || (gpu && fr.gpuFrames != n) || (!gpu && fr.gpuFrames != 0) {
		t.Fatalf("decoded %d frames, %d from the GPU, want %d (gpu=%v)", n, fr.gpuFrames, want, gpu)
	}
	return float64(n) / time.Since(start).Seconds()
}

func openSoftware(path string) (*FrameReader, error) {
	if err := mf.Startup(); err != nil {
		return nil, err
	}
	leave, err := mf.Enter()
	if err != nil {
		return nil, err
	}
	defer leave()
	return openFrameReader(path, nil)
}
