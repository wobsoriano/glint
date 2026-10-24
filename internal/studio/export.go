package studio

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"image"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/transcribe"
)

const (
	// renderVersion is part of the cache key; bump it when the same project renders
	// differently, so stale renders are not reused.
	renderVersion   = 3
	gifFPS          = 15
	gifMaxShortSide = 480
	// compositors is how many frames compose at once; each also spreads its screen draw
	// across cores.
	compositors = 2
)

// ErrCancelled is returned when Export's context is cancelled. It wraps the context's error.
var ErrCancelled = errors.New("export cancelled")

// Export renders the session's project with opts and returns the file in render/, reusing a
// finished render of the same state. progress, which may be nil, receives 0..1 on the
// calling goroutine and reaches 1 on success. Cancelling ctx stops the render, removes the
// partial file and returns an error wrapping ErrCancelled.
//
// Export reads s until it returns, so hand it a session nothing else changes meanwhile: the
// Rust Studio exports a copy it takes on the UI thread.
func Export(ctx context.Context, s *Session, opts ExportOptions, progress func(float64)) (string, error) {
	if progress == nil {
		progress = func(float64) {}
	}
	project := s.Project.Clone()
	project.Export = opts
	ext := "mp4"
	if opts.Format == FormatGif {
		ext = "gif"
	}
	key := cacheKey(s, &project)
	dir := s.RenderDir()
	out := filepath.Join(dir, fmt.Sprintf("export-%s.%s", key, ext))
	if isFile(out) {
		progress(1)
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	partial := filepath.Join(dir, fmt.Sprintf("export-%s.%d-%d.partial.%s", key, os.Getpid(), partialSeq.Add(1)-1, ext))
	var err error
	if opts.Format == FormatGif {
		err = writeGIF(ctx, s, &project, partial, progress)
	} else {
		err = writeMP4(ctx, s, &project, partial, progress)
	}
	if err != nil {
		os.Remove(partial)
		return "", err
	}
	if os.Rename(partial, out) != nil {
		os.Remove(partial)
		if !isFile(out) {
			return "", fmt.Errorf("could not move the render into %s", out)
		}
	}
	removeStaleExports(dir, out, ext)
	progress(1)
	return out, nil
}

var partialSeq atomic.Uint64

// Deliverable is the render of the project with its own export options.
func Deliverable(ctx context.Context, s *Session) (string, error) {
	return Export(ctx, s, s.Project.Export, nil)
}

// Bitrate is the encoder's average bits per second for a size, rate and quality, from 0.1,
// 0.065 or 0.04 bits a pixel. A small video gets a floor so it stays legible, 1.5 Mbps at
// High. The floor scales with the bits a pixel, as the Rust build's single 1.5 Mbps floor did
// not, so each quality stays smaller than the one above it however small the video.
func Bitrate(width, height, fps uint32, q Quality) int {
	bpp := 0.1
	switch q {
	case QualityMedium:
		bpp = 0.065
	case QualityLow:
		bpp = 0.04
	}
	return int(clamp(float64(width)*float64(height)*float64(fps)*bpp, 15_000_000*bpp, 80_000_000))
}

// CompressOptions says how Trim & Compress re-encodes a video.
type CompressOptions struct {
	// Resolution shrinks the short side to this; it never upscales.
	Resolution Resolution
	Quality    Quality
	Codec      Codec
	// Trim keeps only this stretch when set.
	Trim *media.Span
}

func CompressVideo(ctx context.Context, src, dst string, opts CompressOptions, progress func(float64)) (media.WriteSummary, error) {
	reader, err := media.OpenFrameReader(src)
	if err != nil {
		return media.WriteSummary{}, err
	}
	w, h := reader.Size()
	fps := uint32(max(math.Round(reader.FPS()), 1))
	reader.Close()
	tw, th := w&^1, h&^1
	if side, ok := opts.Resolution.ShortSide(); ok && int(side) < min(w, h) {
		k := float64(side) / float64(min(w, h))
		tw, th = int(math.Round(float64(w)*k))&^1, int(math.Round(float64(h)*k))&^1
	}
	if progress == nil {
		progress = func(float64) {}
	}
	return media.Transcode(ctx, src, dst, media.TranscodeOptions{
		Width:   tw,
		Height:  th,
		Bitrate: Bitrate(uint32(tw), uint32(th), fps, opts.Quality),
		Codec:   pickCodec(opts.Codec),
		Trim:    opts.Trim,
	}, progress)
}

func pickCodec(c Codec) media.VideoCodec {
	if c == CodecHevc && slices.Contains(media.AvailableCodecs(), media.HEVC) {
		return media.HEVC
	}
	return media.H264
}

func frameCount(durationMs float64, fps uint32) int {
	return max(int(math.Round(durationMs*float64(fps)/1000)), 1)
}

func writeMP4(ctx context.Context, s *Session, p *Project, path string, progress func(float64)) error {
	opts := p.Export
	fps := opts.Fps.Value()
	w, h := p.OutputSize(s.SourceW, s.SourceH)
	frames := frameCount(p.DurationMs(), fps)
	sources, err := LoadAudioSources(s.Tracks)
	if err != nil {
		return err
	}
	audible := sources.Audible(p.Audio)
	video := media.VideoParams{Width: int(w), Height: int(h), FPS: int(fps), Bitrate: Bitrate(w, h, fps, opts.Quality), Codec: pickCodec(opts.Codec)}
	var audio *media.AudioParams
	var mixer *AudioMixer
	if audible {
		audio = &media.AudioParams{SampleRate: SampleRate, Channels: Channels}
		mixer = NewAudioMixer(sources, p, 0)
	}
	writer, err := media.CreateVideoWriter(path, video, audio)
	if err != nil {
		return err
	}
	defer writer.Close()
	var audioFrames uint64
	var pcm []float32
	err = renderFrames(ctx, s, p, w, h, fps, frames, func(i int, frame *image.NRGBA) error {
		err := writer.WriteFrame(asRGBA(frame))
		RecycleFrame(frame)
		if err != nil {
			return err
		}
		if mixer != nil {
			target := uint64(i+1) * SampleRate / uint64(fps)
			n := int(target-audioFrames) * Channels
			if cap(pcm) < n {
				pcm = make([]float32, n)
			}
			pcm = pcm[:n]
			mixer.Render(pcm)
			if err := writer.WriteAudio(pcm); err != nil {
				return err
			}
			audioFrames = target
		}
		progress(float64(i+1) / float64(frames))
		return nil
	})
	if err != nil {
		return err
	}
	if _, err := writer.Finish(); err != nil {
		return fmt.Errorf("finishing %s: %w", path, err)
	}
	return nil
}

// asRGBA views an opaque frame as the RGBA the encoder takes; for alpha 255 the two layouts
// hold the same bytes.
func asRGBA(f *image.NRGBA) *image.RGBA {
	return &image.RGBA{Pix: f.Pix, Stride: f.Stride, Rect: f.Rect}
}

type renderJob struct {
	index          int
	outputMs       float64
	screen, camera *SourceFrame
}

type renderedFrame struct {
	index int
	frame *image.NRGBA
}

// renderFrames composes frames output frames at fps and hands them to sink in order. One
// goroutine decodes, compositors compose, and the caller's goroutine runs sink, so encoding
// overlaps composing.
func renderFrames(ctx context.Context, s *Session, p *Project, w, h, fps uint32, frames int, sink func(int, *image.NRGBA) error) error {
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	jobs := make(chan renderJob, compositors*2)
	done := make(chan renderedFrame, compositors*2)
	var decodeErr error
	var decoder sync.WaitGroup
	decoder.Go(func() {
		defer close(jobs)
		decodeErr = decodeFrames(ctx, s.Tracks, p, fps, frames, jobs)
	})
	var workers sync.WaitGroup
	for range compositors {
		workers.Go(func() {
			for j := range jobs {
				if ctx.Err() != nil {
					continue
				}
				in := FrameInputs{Screen: j.screen.RGBA(), OutputMs: j.outputMs}
				if j.camera != nil {
					in.Camera = j.camera.RGBA()
				}
				done <- renderedFrame{j.index, ComposeFrameMotionBlur(p, s.Assets, in, w, h)}
			}
		})
	}
	go func() {
		workers.Wait()
		close(done)
	}()

	pending := map[int]*image.NRGBA{}
	next := 0
	var sinkErr error
	for r := range done {
		if sinkErr != nil || ctx.Err() != nil {
			RecycleFrame(r.frame)
			continue
		}
		pending[r.index] = r.frame
		for f, ok := pending[next]; ok; f, ok = pending[next] {
			delete(pending, next)
			if sinkErr = sink(next, f); sinkErr != nil {
				stop()
				break
			}
			next++
		}
	}
	decoder.Wait()
	for _, f := range pending {
		RecycleFrame(f)
	}
	switch {
	case sinkErr != nil:
		return sinkErr
	case ctx.Err() != nil:
		return fmt.Errorf("%w: %w", ErrCancelled, ctx.Err())
	case decodeErr != nil:
		return decodeErr
	case next != frames:
		return fmt.Errorf("rendered %d of %d frames", next, frames)
	}
	return nil
}

func decodeFrames(ctx context.Context, t Tracks, p *Project, fps uint32, frames int, jobs chan<- renderJob) error {
	screen, err := OpenFeed(t.Screen)
	if err != nil {
		return err
	}
	defer screen.Close()
	var camera *Feed
	if t.Camera != "" {
		if camera, err = OpenFeed(t.Camera); err != nil {
			return err
		}
		defer camera.Close()
	}
	for index := range frames {
		if ctx.Err() != nil {
			return nil
		}
		outputMs := float64(index) * 1000 / float64(fps)
		sourceMs := p.SourceTime(outputMs)
		j := renderJob{index: index, outputMs: outputMs}
		if j.screen, err = screen.FrameAt(sourceMs); err != nil {
			return err
		}
		if j.screen == nil {
			return errors.New("screen.mp4 has no frames")
		}
		if camera != nil {
			if j.camera, err = camera.FrameAt(sourceMs); err != nil {
				return err
			}
		}
		select {
		case jobs <- j:
		case <-ctx.Done():
			return nil
		}
	}
	return nil
}

func writeGIF(ctx context.Context, s *Session, p *Project, path string, progress func(float64)) error {
	w, h := gifSize(p.OutputSize(s.SourceW, s.SourceH))
	frames := frameCount(p.DurationMs(), gifFPS)
	samples, err := sampleFrames(s, p, w, h, 8)
	if err != nil {
		return err
	}
	pal := trainPalette(samples)
	for _, f := range samples {
		RecycleFrame(f)
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	defer file.Close()
	enc, err := newGIFWriter(file, int(w), int(h), pal.rgb)
	if err != nil {
		return err
	}
	var indices []uint8
	err = renderFrames(ctx, s, p, w, h, gifFPS, frames, func(i int, frame *image.NRGBA) error {
		ceil := func(i int) int { return (i*100 + gifFPS - 1) / gifFPS }
		indices = pal.mapFrame(frame, indices)
		RecycleFrame(frame)
		if err := enc.writeFrame(indices, int(w), int(h), uint16(ceil(i+1)-ceil(i))); err != nil {
			return err
		}
		progress(float64(i+1) / float64(frames))
		return nil
	})
	if err != nil {
		return err
	}
	if err := enc.finish(); err != nil {
		return err
	}
	return file.Close()
}

func gifSize(w, h uint32) (uint32, uint32) {
	short := min(w, h)
	if short <= gifMaxShortSide {
		return w, h
	}
	k := float64(gifMaxShortSide) / float64(short)
	even := func(v uint32) uint32 { return max(uint32(math.Round(float64(v)*k))&^1, 2) }
	return even(w), even(h)
}

// sampleFrames composes n frames from the middles of n equal slices of the output, the
// sample the GIF palette is trained on.
func sampleFrames(s *Session, p *Project, w, h uint32, n int) ([]*image.NRGBA, error) {
	screen, err := OpenFeed(s.Tracks.Screen)
	if err != nil {
		return nil, err
	}
	defer screen.Close()
	var camera *Feed
	if s.Tracks.Camera != "" {
		if camera, err = OpenFeed(s.Tracks.Camera); err != nil {
			return nil, err
		}
		defer camera.Close()
	}
	duration := p.DurationMs()
	out := make([]*image.NRGBA, 0, n)
	for i := range n {
		outputMs := duration * (float64(i) + 0.5) / float64(n)
		sourceMs := p.SourceTime(outputMs)
		frame, err := screen.FrameAt(sourceMs)
		if err != nil {
			return nil, err
		}
		if frame == nil {
			return nil, errors.New("screen.mp4 has no frames")
		}
		in := FrameInputs{Screen: frame.RGBA(), OutputMs: outputMs}
		if camera != nil {
			cam, err := camera.FrameAt(sourceMs)
			if err != nil {
				return nil, err
			}
			if cam != nil {
				in.Camera = cam.RGBA()
			}
		}
		out = append(out, ComposeFrameMotionBlur(p, s.Assets, in, w, h))
	}
	return out, nil
}

// cacheKey names a render by everything it is made from: the project with its export
// options and the size and modification time of every file the session reads.
func cacheKey(s *Session, p *Project) string {
	stamp := func(path string) *[2]int64 {
		st, err := os.Stat(path)
		if err != nil {
			return nil
		}
		return &[2]int64{st.Size(), st.ModTime().UnixMilli()}
	}
	t := s.Tracks
	var files []*[2]int64
	for _, path := range []string{t.Screen, t.Camera, t.Mic, t.System} {
		if path != "" {
			files = append(files, stamp(path))
		}
	}
	files = append(files, stamp(filepath.Join(s.Dir, input.File)), stamp(filepath.Join(s.Dir, transcribe.TranscriptFile)))
	data, _ := json.Marshal([]any{renderVersion, p, files})
	h := fnv.New64a()
	h.Write(data)
	return fmt.Sprintf("%016x", h.Sum64())
}

func removeStaleExports(dir, keep, ext string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		p := filepath.Join(dir, name)
		if p != keep && strings.HasPrefix(name, "export-") && !strings.Contains(name, ".partial.") && strings.HasSuffix(name, "."+ext) {
			os.Remove(p)
		}
	}
}
