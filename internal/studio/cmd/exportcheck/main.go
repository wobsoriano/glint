// Command exportcheck proves Studio export on real files. It builds a synthetic session,
// exports edits of it, and checks each render with ffprobe, against ComposeFrame, for audio
// and flash onsets lining up, and (given the Rust exporter) against the Rust build's render.
// It also exports copies of Library sessions and times them. Never point -real at the
// Library itself; copy it first.
//
//	cargo build --release -p glint-studio --example go_export
//	go run ./internal/studio/cmd/exportcheck -work <tmp> -rust <target>/release/examples/go_export.exe
//	go run ./internal/studio/cmd/exportcheck -work <tmp> -real <copies> -rust ...
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/gif"
	"image/png"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/studio"
)

type variant struct {
	name       string
	fps        studio.Fps
	blur       bool
	captions   bool
	resolution studio.Resolution
	format     studio.Format
}

var variants = []variant{
	{"60-blur-captions-1080", studio.Fps60, true, true, studio.Res1080, studio.FormatMp4},
	{"30-noblur-720", studio.Fps30, false, false, studio.Res720, studio.FormatMp4},
	{"30-blur-captions-720", studio.Fps30, true, true, studio.Res720, studio.FormatMp4},
	{"60-noblur-720", studio.Fps60, false, false, studio.Res720, studio.FormatMp4},
	{"gif-blur-captions", studio.Fps60, true, true, studio.Res1080, studio.FormatGif},
}

var failures int

func check(ok bool, format string, args ...any) {
	status := "ok  "
	if !ok {
		status = "FAIL"
		failures++
	}
	fmt.Printf("  %s %s\n", status, fmt.Sprintf(format, args...))
}

func main() {
	work := flag.String("work", "", "scratch directory for sessions and renders")
	rust := flag.String("rust", "", "path to the Rust go_export example, for parity")
	real := flag.String("real", "", "directory of copied *.glintrec sessions to export at 1080p60")
	only := flag.String("only", "", "run only the variant with this name")
	profile := flag.String("cpuprofile", "", "write a CPU profile of the real-session exports")
	flag.Parse()
	if *work == "" {
		log.Fatal("-work is required")
	}
	if *real != "" {
		if err := realSessions(*work, *real, *rust, *profile); err != nil {
			log.Fatal(err)
		}
	} else if err := synthetic(*work, *rust, *only); err != nil {
		log.Fatal(err)
	}
	if failures > 0 {
		fmt.Printf("%d checks failed\n", failures)
		os.Exit(1)
	}
	fmt.Println("all checks passed")
}

func synthetic(work, rust, only string) error {
	session := filepath.Join(work, "session")
	if !exists(filepath.Join(session, "screen.mp4")) {
		if err := synthesize(session); err != nil {
			return fmt.Errorf("synthesizing: %w", err)
		}
	}
	for _, v := range variants {
		if only != "" && v.name != only {
			continue
		}
		if err := runVariant(work, session, rust, v); err != nil {
			return fmt.Errorf("%s: %w", v.name, err)
		}
	}
	if only == "" {
		if err := checkThumbs(work, session); err != nil {
			return err
		}
		return checkCancel(work, session)
	}
	return nil
}

// checkThumbs compares each thumbnail with the source frame it should show, then checks the
// cached strip gives back the same pixels.
func checkThumbs(work, session string) error {
	fmt.Println("== thumbnails")
	dir := filepath.Join(work, "thumbs")
	s, err := prepare(session, dir, variants[0])
	if err != nil {
		return err
	}
	const count, height = 12, 54
	started := time.Now()
	thumbs, err := studio.TimelineThumbnails(s, count, height)
	if err != nil {
		return err
	}
	fresh := time.Since(started)
	feed, err := studio.OpenFeed(s.Tracks.Screen)
	if err != nil {
		return err
	}
	defer feed.Close()
	worst := math.Inf(1)
	for i, th := range thumbs {
		t := s.SourceDurationMs * (float64(i) + 0.5) / count
		src, err := feed.FrameAt(t)
		if err != nil {
			return err
		}
		want := media.Shrink(src.RGBA(), th.Rect.Dx(), th.Rect.Dy())
		worst = min(worst, psnr(th.Pix, want.Pix))
	}
	check(len(thumbs) == count && thumbs[0].Rect.Dx() == 96 && thumbs[0].Rect.Dy() == height, "%d thumbnails of %dx%d in %v", len(thumbs), thumbs[0].Rect.Dx(), thumbs[0].Rect.Dy(), fresh)
	check(worst > 40, "thumbnails against the shrunk source frame: worst PSNR %.1f dB", worst)
	started = time.Now()
	cached, err := studio.TimelineThumbnails(s, count, height)
	same := err == nil && len(cached) == len(thumbs)
	for i := range cached {
		same = same && string(cached[i].Pix) == string(thumbs[i].Pix)
	}
	check(same, "cached strip gives the same thumbnails in %v", time.Since(started))
	check(exists(filepath.Join(s.RenderDir(), fmt.Sprintf("thumbs-%dx%d.png", count, height))), "strip cached in render/")
	return nil
}

// prepare copies the pristine session to dir and writes the variant's project into it.
func prepare(session, dir string, v variant) (*studio.Session, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := copyDir(session, dir); err != nil {
		return nil, err
	}
	s, err := studio.LoadSession(dir)
	if err != nil {
		return nil, err
	}
	p := baseProject(studio.CaptionCues(*s.Transcript))
	p.Captions.Enabled = v.captions
	p.Captions.Karaoke = v.captions
	p.Export = studio.ExportOptions{Fps: v.fps, MotionBlur: v.blur, Resolution: v.resolution, Quality: studio.QualityHigh, Codec: studio.CodecH264, Format: v.format}
	if err := p.Save(dir); err != nil {
		return nil, err
	}
	return studio.LoadSession(dir)
}

type progressLog struct {
	last      float64
	monotonic bool
	calls     int
}

func (l *progressLog) record(p float64) {
	if p < l.last {
		l.monotonic = false
	}
	l.last = p
	l.calls++
}

func runVariant(work, session, rust string, v variant) error {
	fmt.Printf("== %s\n", v.name)
	dir := filepath.Join(work, v.name, "go")
	s, err := prepare(session, dir, v)
	if err != nil {
		return err
	}
	p := s.Project
	fps := v.fps.Value()
	if v.format == studio.FormatGif {
		fps = 15
	}
	frames := int(math.Max(math.Round(p.DurationMs()*float64(fps)/1000), 1))
	prog := &progressLog{monotonic: true}
	started := time.Now()
	out, err := studio.Export(context.Background(), s, p.Export, prog.record)
	wall := time.Since(started)
	if err != nil {
		return err
	}
	fmt.Printf("  go: %s in %.2f s = %.1f frames/s\n", filepath.Base(out), wall.Seconds(), float64(frames)/wall.Seconds())
	check(prog.last == 1 && prog.monotonic, "progress reached %.3f over %d calls, monotonic %v", prog.last, prog.calls, prog.monotonic)
	again := time.Now()
	cached, err := studio.Export(context.Background(), s, p.Export, nil)
	check(err == nil && cached == out && time.Since(again) < 200*time.Millisecond, "second export reuses the render (%v)", time.Since(again))

	pr, err := ffprobe(out)
	if err != nil {
		return err
	}
	w, h := p.OutputSize(s.SourceW, s.SourceH)
	if v.format == studio.FormatGif {
		short := min(w, h)
		if short > 480 {
			k := 480 / float64(short)
			w, h = uint32(math.Round(float64(w)*k))&^1, uint32(math.Round(float64(h)*k))&^1
		}
	}
	vs := pr.stream("video")
	check(vs != nil && vs.Width == int(w) && vs.Height == int(h), "size %dx%d, want %dx%d", vs.Width, vs.Height, w, h)
	check(vs.frames() == frames, "%d frames, want %d (%.1f ms output at %d fps)", vs.frames(), frames, p.DurationMs(), fps)
	if v.format == studio.FormatMp4 {
		check(vs.RFrameRate == fmt.Sprintf("%d/1", fps), "frame rate %s", vs.RFrameRate)
		dur := pr.duration()
		check(math.Abs(dur-p.DurationMs()/1000) < 1.5/float64(fps)+0.03, "duration %.3f s, want %.3f s", dur, p.DurationMs()/1000)
		check(pr.stream("audio") != nil, "audio track present (%s)", pr.streamCodec("audio"))
		if err := compareWithCompose(s, out, frames, fps); err != nil {
			return err
		}
		if err := checkOnsets(s, out, fps); err != nil {
			return err
		}
	}
	if rust == "" {
		return nil
	}
	rdir := filepath.Join(work, v.name, "rust")
	if _, err := prepare(session, rdir, v); err != nil {
		return err
	}
	rout, rinfo, err := runRust(rust, rdir)
	if err != nil {
		return err
	}
	fmt.Printf("  rust: %s in %.2f s = %.1f frames/s\n", filepath.Base(rout), rinfo.Seconds, rinfo.FPS)
	if v.format == studio.FormatGif {
		return compareGIFs(out, rout)
	}
	return compareMP4s(out, rout, fps)
}

// compareWithCompose decodes output frames and composes the same output times from the
// source; the only difference left is the export's own encode.
func compareWithCompose(s *studio.Session, out string, frames int, fps uint32) error {
	feed, err := studio.OpenFeed(s.Tracks.Screen)
	if err != nil {
		return err
	}
	defer feed.Close()
	r, err := media.OpenVideoReader(out)
	if err != nil {
		return err
	}
	defer r.Close()
	p := s.Project
	w, h := p.OutputSize(s.SourceW, s.SourceH)
	var worst float64 = math.Inf(1)
	var report []string
	for _, i := range sampleIndices(frames) {
		outMs := float64(i) * 1000 / float64(fps)
		got, err := frameAt(r, outMs)
		if err != nil {
			return err
		}
		src, err := feed.FrameAt(p.SourceTime(outMs))
		if err != nil {
			return err
		}
		want := studio.ComposeFrameMotionBlur(&p, s.Assets, studio.FrameInputs{Screen: src.RGBA(), OutputMs: outMs}, w, h)
		db := psnr(got.Pix, want.Pix)
		worst = min(worst, db)
		report = append(report, fmt.Sprintf("%d:%.1f", i, db))
		if db < 35 {
			savePNG(filepath.Join(s.Dir, fmt.Sprintf("frame-%d-got.png", i)), got)
			savePNG(filepath.Join(s.Dir, fmt.Sprintf("frame-%d-want.png", i)), want)
		}
	}
	check(worst > 35, "PSNR against ComposeFrame at frame:dB %s", strings.Join(report, " "))
	return nil
}

func sampleIndices(frames int) []int {
	var out []int
	for _, f := range []float64{0, 0.08, 0.3, 0.42, 0.55, 0.7, 0.85, 0.999} {
		out = append(out, min(int(f*float64(frames)), frames-1))
	}
	return out
}

// frameAt decodes the output frame shown at ms.
func frameAt(r *media.VideoReader, ms float64) (*image.RGBA, error) {
	if err := r.SeekMS(int64(ms)); err != nil {
		return nil, err
	}
	f, err := r.NextFrame()
	if err != nil {
		return nil, err
	}
	if math.Abs(float64(f.TMS)-ms) > 1 {
		return nil, fmt.Errorf("asked for the frame at %.1f ms, decoded %d ms", ms, f.TMS)
	}
	return f.Image, nil
}

func psnr(a, b []byte) float64 {
	var sum float64
	n := 0
	for i := 0; i+3 < len(a) && i+3 < len(b); i += 4 {
		for k := range 3 {
			d := float64(a[i+k]) - float64(b[i+k])
			sum += d * d
		}
		n += 3
	}
	if sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(sum/float64(n)))
}

// checkOnsets finds where the white flashes start in the output frames and where the tones
// start in the output audio, and checks both against where the edit puts each flash.
func checkOnsets(s *studio.Session, out string, fps uint32) error {
	video, err := videoOnsets(out)
	if err != nil {
		return err
	}
	pcm, err := media.ReadMP4Audio(out)
	if err != nil {
		return err
	}
	audio := audioOnsets(pcm)
	var want []float64
	for _, f := range flashes {
		if t, ok := s.Project.EventOutputTime(f); ok {
			want = append(want, t)
		}
	}
	frame := 1000 / float64(fps)
	fmt.Printf("  flashes at output ms %v; video onsets %v; audio onsets %v\n", round1(want), round1(video), round1(audio))
	check(len(video) == len(want) && len(audio) == len(want), "%d video and %d audio onsets, want %d", len(video), len(audio), len(want))
	// The audio threshold crosses about 0.3 ms into a tone, so allow 1 ms beyond a frame.
	for i := range min(len(want), len(video), len(audio)) {
		dv, da := video[i]-want[i], audio[i]-want[i]
		check(math.Abs(video[i]-audio[i]) <= frame+1, "flash %d: video %+.1f ms, audio %+.1f ms from the edit; audio minus video %.1f ms (one frame is %.1f)", i, dv, da, audio[i]-video[i], frame)
	}
	return nil
}

func round1(v []float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = math.Round(x*10) / 10
	}
	return out
}

func videoOnsets(path string) ([]float64, error) {
	r, err := media.OpenVideoReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	var onsets []float64
	bright := false
	for {
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) {
			return onsets, nil
		}
		if err != nil {
			return nil, err
		}
		b := f.Image.Rect
		cx, cy := b.Dx()/2, b.Dy()/2
		var sum float64
		for y := cy - 32; y < cy+32; y++ {
			for x := cx - 32; x < cx+32; x++ {
				p := f.Image.Pix[y*f.Image.Stride+x*4:]
				sum += (float64(p[0]) + float64(p[1]) + float64(p[2])) / 3
			}
		}
		now := sum/(64*64) > 200
		if now && !bright {
			onsets = append(onsets, float64(f.TMS))
		}
		bright = now
	}
}

func audioOnsets(w *media.WavData) []float64 {
	var onsets []float64
	gap := w.SampleRate * 30 / 1000
	lastLoud := -gap
	for i := 0; i < w.Frames(); i++ {
		if math.Abs(float64(w.Samples[i*w.Channels])) > 0.05 {
			if i-lastLoud >= gap {
				onsets = append(onsets, float64(i)*1000/float64(w.SampleRate))
			}
			lastLoud = i
		}
	}
	return onsets
}

type rustResult struct {
	Path    string  `json:"path"`
	Seconds float64 `json:"seconds"`
	Frames  int     `json:"frames"`
	FPS     float64 `json:"fps_achieved"`
}

func runRust(exe, dir string) (string, rustResult, error) {
	var res rustResult
	cmd := exec.Command(exe, dir)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", res, fmt.Errorf("rust exporter: %w", err)
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return "", res, fmt.Errorf("rust exporter printed %q: %w", out, err)
	}
	return res.Path, res, nil
}

func compareMP4s(goOut, rustOut string, fps uint32) error {
	gp, err := ffprobe(goOut)
	if err != nil {
		return err
	}
	rp, err := ffprobe(rustOut)
	if err != nil {
		return err
	}
	gv, rv := gp.stream("video"), rp.stream("video")
	check(gv.frames() == rv.frames() && gv.Width == rv.Width && gv.Height == rv.Height,
		"parity: go %d frames %dx%d, rust %d frames %dx%d", gv.frames(), gv.Width, gv.Height, rv.frames(), rv.Width, rv.Height)
	check(math.Abs(gp.duration()-rp.duration()) < 0.001, "parity: duration go %.3f s, rust %.3f s", gp.duration(), rp.duration())
	gr, err := media.OpenVideoReader(goOut)
	if err != nil {
		return err
	}
	defer gr.Close()
	rr, err := media.OpenVideoReader(rustOut)
	if err != nil {
		return err
	}
	defer rr.Close()
	worst := math.Inf(1)
	var report []string
	for _, i := range sampleIndices(gv.frames()) {
		ms := float64(i) * 1000 / float64(fps)
		a, err := frameAt(gr, ms)
		if err != nil {
			return err
		}
		b, err := frameAt(rr, ms)
		if err != nil {
			return err
		}
		db := psnr(a.Pix, b.Pix)
		worst = min(worst, db)
		report = append(report, fmt.Sprintf("%d:%.1f", i, db))
	}
	check(worst > 30, "parity: PSNR go against rust at frame:dB %s", strings.Join(report, " "))
	ga, err := media.ReadMP4Audio(goOut)
	if err != nil {
		return err
	}
	ra, err := media.ReadMP4Audio(rustOut)
	if err != nil {
		return err
	}
	g, r := rmsPerSecond(ga), rmsPerSecond(ra)
	var worstDiff float64
	for i := range min(len(g), len(r)) {
		worstDiff = max(worstDiff, math.Abs(g[i]-r[i]))
	}
	check(len(g) == len(r) && worstDiff < 0.01, "parity: audio RMS per second go %v rust %v (worst difference %.4f)", round3(g), round3(r), worstDiff)
	return nil
}

func round3(v []float64) []float64 {
	out := make([]float64, len(v))
	for i, x := range v {
		out[i] = math.Round(x*1000) / 1000
	}
	return out
}

func rmsPerSecond(w *media.WavData) []float64 {
	var out []float64
	per := w.SampleRate * w.Channels
	for start := 0; start < len(w.Samples); start += per {
		end := min(start+per, len(w.Samples))
		var sum float64
		for _, s := range w.Samples[start:end] {
			sum += float64(s) * float64(s)
		}
		out = append(out, math.Sqrt(sum/float64(end-start)))
	}
	return out
}

func compareGIFs(goOut, rustOut string) error {
	read := func(path string) (*gif.GIF, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return gif.DecodeAll(f)
	}
	g, err := read(goOut)
	if err != nil {
		return err
	}
	r, err := read(rustOut)
	if err != nil {
		return err
	}
	check(len(g.Image) == len(r.Image) && fmt.Sprint(g.Delay) == fmt.Sprint(r.Delay) && g.LoopCount == r.LoopCount,
		"parity: gif go %d frames loop %d, rust %d frames loop %d, delays equal %v", len(g.Image), g.LoopCount, len(r.Image), r.LoopCount, fmt.Sprint(g.Delay) == fmt.Sprint(r.Delay))
	gs, rs := g.Image[0].Palette, r.Image[0].Palette
	same := 0
	for i := range min(len(gs), len(rs)) {
		if gs[i] == rs[i] {
			same++
		}
	}
	fmt.Printf("  gif palettes: %d of %d entries identical\n", same, len(gs))
	worst := math.Inf(1)
	var report []string
	for _, i := range sampleIndices(len(g.Image)) {
		db := psnr(toRGBA(g.Image[i]).Pix, toRGBA(r.Image[i]).Pix)
		worst = min(worst, db)
		report = append(report, fmt.Sprintf("%d:%.1f", i, db))
	}
	check(worst > 30, "parity: gif PSNR go against rust at frame:dB %s", strings.Join(report, " "))
	return nil
}

func toRGBA(p *image.Paletted) *image.RGBA {
	out := image.NewRGBA(p.Rect)
	for i, idx := range p.Pix {
		r, g, b, _ := p.Palette[idx].RGBA()
		out.Pix[i*4], out.Pix[i*4+1], out.Pix[i*4+2], out.Pix[i*4+3] = uint8(r>>8), uint8(g>>8), uint8(b>>8), 255
	}
	return out
}

// checkCancel cancels an export a third of the way in and checks it stops at once and
// leaves nothing in render/.
func checkCancel(work, session string) error {
	fmt.Println("== cancel")
	v := variants[0]
	dir := filepath.Join(work, "cancel")
	s, err := prepare(session, dir, v)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	var cancelledAt time.Time
	_, err = studio.Export(ctx, s, s.Project.Export, func(p float64) {
		if p >= 0.3 && cancelledAt.IsZero() {
			cancelledAt = time.Now()
			cancel()
		}
	})
	stopped := time.Since(cancelledAt)
	check(errors.Is(err, studio.ErrCancelled) && errors.Is(err, context.Canceled), "export returned %v", err)
	check(stopped < 500*time.Millisecond, "stopped %v after cancel", stopped)
	entries, _ := os.ReadDir(s.RenderDir())
	var left []string
	for _, e := range entries {
		left = append(left, e.Name())
	}
	check(len(left) == 0, "render/ holds %v after cancel", left)
	return nil
}

func realSessions(work, copies, rust, profile string) error {
	dirs, err := filepath.Glob(filepath.Join(copies, "*.glintrec"))
	if err != nil || len(dirs) == 0 {
		return fmt.Errorf("no *.glintrec in %s: %v", copies, err)
	}
	if profile != "" {
		f, err := os.Create(profile)
		if err != nil {
			return err
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			return err
		}
		defer pprof.StopCPUProfile()
	}
	for _, src := range dirs {
		name := filepath.Base(src)
		fmt.Printf("== %s\n", name)
		dir := filepath.Join(work, "real", name, "go")
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
		opts := studio.DefaultExportOptions()
		w, h := s.Project.OutputSize(s.SourceW, s.SourceH)
		frames := int(math.Round(s.Project.DurationMs() * 60 / 1000))
		prog := &progressLog{monotonic: true}
		started := time.Now()
		out, err := studio.Export(context.Background(), s, opts, prog.record)
		wall := time.Since(started)
		check(err == nil, "go export of %dx%d %.0f ms source to %dx%d: %v", s.SourceW, s.SourceH, s.SourceDurationMs, w, h, err)
		if err != nil {
			continue
		}
		fmt.Printf("  go: %d frames in %.2f s = %.1f frames/s\n", frames, wall.Seconds(), float64(frames)/wall.Seconds())
		pr, err := ffprobe(out)
		if err != nil {
			return err
		}
		vs := pr.stream("video")
		check(vs.frames() == frames && vs.Width == int(w) && vs.Height == int(h) && pr.stream("audio") != nil == (s.Tracks.Mic != "" || s.Tracks.System != ""),
			"ffprobe: %d frames %dx%d %s, audio %s, %.3f s", vs.frames(), vs.Width, vs.Height, vs.RFrameRate, pr.streamCodec("audio"), pr.duration())
		if rust == "" {
			continue
		}
		rdir := filepath.Join(work, "real", name, "rust")
		if err := os.RemoveAll(rdir); err != nil {
			return err
		}
		if err := copyDir(src, rdir); err != nil {
			return err
		}
		rout, rinfo, err := runRust(rust, rdir)
		if err != nil {
			return err
		}
		fmt.Printf("  rust: %d frames in %.2f s = %.1f frames/s\n", rinfo.Frames, rinfo.Seconds, rinfo.FPS)
		if err := compareMP4s(out, rout, 60); err != nil {
			return err
		}
	}
	return nil
}

type probeStream struct {
	CodecType    string `json:"codec_type"`
	CodecName    string `json:"codec_name"`
	Width        int    `json:"width"`
	Height       int    `json:"height"`
	RFrameRate   string `json:"r_frame_rate"`
	NbReadFrames string `json:"nb_read_frames"`
}

func (s *probeStream) frames() int {
	if s == nil {
		return -1
	}
	n, _ := strconv.Atoi(s.NbReadFrames)
	return n
}

type probe struct {
	Streams []probeStream `json:"streams"`
	Format  struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (p *probe) stream(kind string) *probeStream {
	for i := range p.Streams {
		if p.Streams[i].CodecType == kind {
			return &p.Streams[i]
		}
	}
	return nil
}

func (p *probe) streamCodec(kind string) string {
	if s := p.stream(kind); s != nil {
		return s.CodecName
	}
	return "none"
}

func (p *probe) duration() float64 {
	d, _ := strconv.ParseFloat(p.Format.Duration, 64)
	return d
}

func ffprobe(path string) (*probe, error) {
	out, err := exec.Command("ffprobe", "-v", "error", "-count_frames",
		"-show_entries", "stream=codec_type,codec_name,width,height,r_frame_rate,nb_read_frames:format=duration",
		"-of", "json", path).Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	var p probe
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func copyDir(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() && d.Name() == "render" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func savePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
