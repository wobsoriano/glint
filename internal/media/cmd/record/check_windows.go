package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/studio"
)

type checker struct{ failed []string }

func (k *checker) ok(cond bool, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if cond {
		fmt.Println("ok   " + msg)
		return
	}
	fmt.Println("FAIL " + msg)
	k.failed = append(k.failed, msg)
}

func (k *checker) info(format string, args ...any) {
	fmt.Println("     " + fmt.Sprintf(format, args...))
}

type probe struct {
	Streams []struct {
		Codec    string `json:"codec_name"`
		Width    int    `json:"width"`
		Height   int    `json:"height"`
		Rate     string `json:"r_frame_rate"`
		TimeBase string `json:"time_base"`
		Frames   string `json:"nb_frames"`
	} `json:"streams"`
	Packets []struct {
		PTS int64 `json:"pts"`
	} `json:"packets"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func ffprobe(path string) (probe, error) {
	var p probe
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=codec_name,width,height,r_frame_rate,time_base,nb_frames:format=duration:packet=pts",
		"-of", "json", path).Output()
	if err != nil {
		return p, fmt.Errorf("ffprobe %s: %w", path, err)
	}
	if err := json.Unmarshal(out, &p); err != nil {
		return p, err
	}
	if len(p.Streams) == 0 {
		return p, errors.New("ffprobe found no video stream")
	}
	return p, nil
}

func ratio(s string) float64 {
	a, b, _ := strings.Cut(s, "/")
	n, _ := strconv.ParseFloat(a, 64)
	d, _ := strconv.ParseFloat(b, 64)
	if d == 0 {
		return 0
	}
	return n / d
}

// frameInfo is what one decoded frame of the stage shows.
type frameInfo struct {
	tms     int64
	counter int64
	valid   bool
	flash   bool
}

func check(dir string, sum media.RecordSummary, size image.Point, c config, p schedule, st *stage, mv *moveInfo) error {
	k := &checker{}
	screen := filepath.Join(dir, "screen.mp4")
	period := 1000 / float64(c.fps)

	pr, err := ffprobe(screen)
	if err != nil {
		return err
	}
	s := pr.Streams[0]
	k.ok(s.Codec == "h264", "codec %s", s.Codec)
	k.ok(s.Width == size.X&^1 && s.Height == size.Y&^1, "size %dx%d, want %dx%d", s.Width, s.Height, size.X&^1, size.Y&^1)
	k.ok(uint32(s.Width) == sum.Width && uint32(s.Height) == sum.Height, "summary size %dx%d", sum.Width, sum.Height)
	k.ok(ratio(s.Rate) == float64(c.fps), "frame rate %s", s.Rate)
	nb, _ := strconv.ParseInt(s.Frames, 10, 64)
	k.ok(nb == sum.Screen.Frames, "%d frames in the file, %d written", nb, sum.Screen.Frames)
	tb := ratio(s.TimeBase)
	pts := make([]int64, len(pr.Packets))
	for i, pk := range pr.Packets {
		pts[i] = pk.PTS
	}
	slices.Sort(pts)
	irregular := 0
	for i := 1; i < len(pts); i++ {
		if d := float64(pts[i]-pts[i-1]) * tb * 1000; math.Abs(d-period) > 1 {
			irregular++
		}
	}
	k.ok(irregular == 0 && len(pts) > 0, "constant frame rate: %d of %d frame steps off 1/%d s by more than 1 ms", irregular, max(len(pts)-1, 0), c.fps)
	if len(pts) > 0 {
		k.ok(pts[0] == 0, "first frame at %d", pts[0])
	}
	dur, _ := strconv.ParseFloat(pr.Format.Duration, 64)
	k.ok(math.Abs(dur*1000-float64(sum.DurationMS)) <= period+1, "file lasts %.1f ms, media time %d ms (one frame is %.1f ms)", dur*1000, sum.DurationMS, period)
	k.ok(math.Abs(float64(sum.DurationMS)-p.media.Seconds()*1000) < 50, "media time %d ms, asked for %v", sum.DurationMS, p.media)

	frames, err := decode(screen, st, c, mv, k)
	if err != nil {
		return err
	}
	counters(frames, st, c, p, k)
	if c.system {
		audio(dir, frames, sum, c, p, st, k)
	}
	pointer(dir, sum, st, c, mv, k)

	in, err := studio.OpenSession(dir, sum.Width, sum.Height, float64(sum.DurationMS))
	k.ok(err == nil, "studio.OpenSession: %v", err)
	if err == nil {
		k.ok(in.Tracks.Screen != "" && (in.Tracks.System != "") == c.system && (in.Tracks.Mic != "") == c.mic,
			"Studio sees tracks %+v", in.Tracks)
		k.ok(in.Input.Size == [2]uint32{sum.Width, sum.Height}, "Studio reads input size %v", in.Input.Size)
		k.ok(len(in.Notices) == 0, "Studio notices %v", in.Notices)
	}
	if len(k.failed) > 0 {
		return fmt.Errorf("%d checks failed: %s", len(k.failed), strings.Join(k.failed, "; "))
	}
	fmt.Println("all checks passed")
	return nil
}

func decode(path string, st *stage, c config, mv *moveInfo, k *checker) ([]frameInfo, error) {
	fr, err := media.OpenFrameReader(path)
	if err != nil {
		return nil, err
	}
	defer fr.Close()
	var out []frameInfo
	var img *image.RGBA
	cursorBad, cursorChecked, magentaFrames, dumped := 0, 0, 0, 0
	for {
		f, err := fr.NextFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		img = f.ToRGBA(img)
		tms := f.TMS
		f.Release()
		n, ok := readBarcode(img, st.origin)
		out = append(out, frameInfo{tms: tms, counter: n, valid: ok, flash: flashOn(img, st.origin)})
		if !ok && c.dump != "" && dumped < 4 {
			dumped++
			crop := img.SubImage(image.Rectangle{Min: st.origin, Max: st.origin.Add(image.Pt(stageW, stageH))})
			if err := savePNG(filepath.Join(c.dump, fmt.Sprintf("unread-%05d.png", tms)), crop); err != nil {
				return nil, err
			}
		}
		if r, g, b := mean(img, excludeRect.Inset(8).Add(st.origin)); r > 200 && g < 80 && b > 200 {
			magentaFrames++
		}
		// Only frames whose pointer sat still over the plain zone say anything about the
		// cursor: there the video must show nothing but the zone's colour.
		if s, ok := st.stableAt(tms-100, tms+20); ok {
			on := s.cursor.Sub(s.stage)
			if on.In(zoneRect.Inset(30)) && !on.In(excludeRect.Inset(-30)) {
				p := st.inVideo(s.cursor, s)
				cursorChecked++
				if worstDistance(img, image.Rectangle{Min: p, Max: p}.Inset(-24), zoneColour) > 48 {
					cursorBad++
				}
			}
		}
	}
	k.ok(len(out) > 0, "decoded %d frames", len(out))
	switch {
	case c.source == "window":
	case c.exclude:
		k.ok(magentaFrames == 0, "the window excluded from capture shows in %d of %d frames", magentaFrames, len(out))
	default:
		k.ok(magentaFrames == len(out), "control: the same window not excluded shows in %d of %d frames", magentaFrames, len(out))
	}
	if cursorChecked > 0 {
		k.ok(cursorBad == 0, "no cursor under the pointer: %d of %d frames differ from the plain zone there", cursorBad, cursorChecked)
	} else {
		k.info("the pointer never sat still over the stage's plain zone; cursor absence not checked")
	}
	return out, nil
}

func counters(frames []frameInfo, st *stage, c config, p schedule, k *checker) {
	valid, dup, drops := 0, 0, int64(0)
	distinct := map[int64]bool{}
	step := st.rate / float64(c.fps)
	type delta struct{ d, tms int64 }
	var deltas []delta
	var prev *frameInfo
	for i := range frames {
		f := &frames[i]
		if !f.valid {
			continue
		}
		valid++
		distinct[f.counter] = true
		if prev != nil {
			deltas = append(deltas, delta{(f.counter - prev.counter) & (1<<barcodeBits - 1), f.tms})
		}
		prev = f
	}
	jump := slices.MaxFunc(append(deltas, delta{}), func(a, b delta) int { return int(a.d - b.d) })
	for _, d := range deltas {
		if d.d == 0 {
			dup++
		}
		if p.pauseAt > 0 && d == jump {
			continue
		}
		if extra := d.d - int64(math.Ceil(step+0.25)); extra > 0 {
			drops += extra
		}
	}
	k.ok(valid*100 >= len(frames)*98, "barcode read in %d of %d frames", valid, len(frames))
	k.ok(len(distinct) > len(frames)/3, "motion: %d distinct stage frames in %d video frames", len(distinct), len(frames))
	secs := float64(len(frames)) / float64(c.fps)
	k.info("stage painted %.1f fps; video shows %.1f distinct frames per second, %d repeated slots, %d stage frames skipped beyond the expected %.2f per slot",
		st.rate, float64(len(distinct))/secs, dup, drops, step)
	if p.pauseAt > 0 {
		want := p.pauseFor.Seconds() * st.rate
		k.ok(math.Abs(float64(jump.d)-want) < 0.15*want+3, "pause: the stage jumps %d frames at %d ms, %.0f expected for %v", jump.d, jump.tms, want, p.pauseFor)
	}
}

func audio(dir string, frames []frameInfo, sum media.RecordSummary, c config, p schedule, st *stage, k *checker) {
	wav, err := media.ReadWAV(filepath.Join(dir, "system.wav"))
	k.ok(err == nil, "system.wav reads: %v", err)
	if err != nil {
		return
	}
	k.ok(wav.SampleRate == 48000 && wav.Channels == 2, "system.wav %d Hz, %d channels", wav.SampleRate, wav.Channels)
	k.ok(math.Abs(float64(wav.DurationMS())-float64(sum.DurationMS)) <= 2, "system.wav lasts %d ms, media time %d ms", wav.DurationMS(), sum.DurationMS)
	if !c.tones {
		return
	}
	onsets := toneOnsets(wav.Samples, wav.Channels, wav.SampleRate)
	var flashes []int64
	for i, f := range frames {
		if f.flash && (i == 0 || !frames[i-1].flash) {
			flashes = append(flashes, f.tms)
		}
	}
	k.info("tone onsets %v ms; flash onsets %v ms", rounded(onsets), flashes)
	st.mu.Lock()
	k.info("playback reached the bursts at %v ms and the stage painted the flashes at %v ms of media time", st.soundAt, st.paintAt)
	var mixed []int64
	for _, ms := range toneOnsets(st.loopback, 2, 48000) {
		mixed = append(mixed, int64(math.Round(st.mixedAt(int(ms*48)))))
	}
	k.info("a second loopback capture received the bursts at %v ms of media time", mixed)
	st.mu.Unlock()
	want := 0
	for _, b := range p.bursts {
		if p.pauseAt == 0 || b < p.pauseAt || b >= p.pauseAt+p.pauseFor {
			want++
		}
	}
	k.ok(len(onsets) == want && len(flashes) == want, "%d tone bursts and %d flashes recorded, %d played outside the pause", len(onsets), len(flashes), want)
	period := 1000 / float64(c.fps)
	for i := range min(len(onsets), len(flashes)) {
		off := onsets[i] - float64(flashes[i])
		k.ok(math.Abs(off) <= 2*period, "sync: burst %d sounds %+.1f ms after its flash shows (%.2f frames)", i, off, off/period)
	}
}

func savePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

func rounded(v []float64) []int64 {
	out := make([]int64, len(v))
	for i, x := range v {
		out[i] = int64(math.Round(x))
	}
	return out
}

func pointer(dir string, sum media.RecordSummary, st *stage, c config, mv *moveInfo, k *checker) {
	tl, err := input.Load(dir)
	k.ok(err == nil, "input.json reads: %v", err)
	if err != nil {
		return
	}
	k.ok(tl.Size == [2]uint32{sum.Width, sum.Height}, "input.json size %v", tl.Size)
	late := 0
	for _, s := range tl.Pointer {
		if s.T > sum.DurationMS {
			late++
		}
	}
	k.ok(late == 0, "%d pointer samples after the end of media time", late)
	if len(tl.Pointer) == 0 {
		k.info("no pointer samples (the cursor was hidden?)")
		return
	}
	// Each sample must sit on the video pixel showing the desktop point the pointer was
	// over: the trace's cursor minus the desktop position of video pixel (0, 0) then.
	checked, wrong, inside := 0, 0, 0
	var firstWrong string
	for _, p := range tl.Pointer {
		s, ok := st.stableAt(int64(p.T)-10, int64(p.T)+10)
		if !ok {
			continue
		}
		want := st.inVideo(s.cursor, s)
		checked++
		if math.Abs(float64(p.X)-float64(want.X)) > 0.5 || math.Abs(float64(p.Y)-float64(want.Y)) > 0.5 {
			wrong++
			if firstWrong == "" {
				firstWrong = fmt.Sprintf("; first at %d ms: (%.0f, %.0f), want %v", p.T, p.X, p.Y, want)
			}
		}
		if want.In(image.Rect(0, 0, int(sum.Width), int(sum.Height))) {
			inside++
		}
	}
	if checked == 0 {
		k.info("pointer mapping not checked: the pointer moved around every sample")
	}
	k.ok(wrong == 0, "pointer mapping: %d of %d samples taken while the pointer sat still land on the video pixel under it (%d inside the frame)%s",
		checked-wrong, checked, inside, firstWrong)
	if mv == nil {
		return
	}
	if !mv.duringPause {
		var at, back bool
		for _, p := range tl.Pointer {
			at = at || int64(p.T) >= mv.atMS && int64(p.T) < mv.backMS
			back = back || int64(p.T) >= mv.backMS
		}
		k.ok(at && back, "the window moved by %v at %d ms and back at %d ms, and the pointer was resampled after each", mv.delta, mv.atMS, mv.backMS)
		return
	}
	// Positions only reachable while paused, with the window moved away, must not be in
	// the timeline.
	st.mu.Lock()
	pauseOnly, seen := map[image.Point]bool{}, map[image.Point]bool{}
	for _, s := range st.trace {
		p := s.cursor.Sub(s.stage)
		if s.paused {
			pauseOnly[p] = true
		} else {
			seen[p] = true
		}
	}
	st.mu.Unlock()
	leaked := 0
	for _, p := range tl.Pointer {
		q := image.Pt(int(p.X), int(p.Y))
		if pauseOnly[q] && !seen[q] {
			leaked++
		}
	}
	k.ok(leaked == 0 && len(pauseOnly) > 0, "the window moved and came back while paused; %d samples of %d pause-only positions leaked into input.json", leaked, len(pauseOnly))
}
