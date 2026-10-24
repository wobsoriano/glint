package trim

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	thumbCount  = 12
	thumbHeight = 112
	stripHeight = 56
	handleWidth = 12
	// stripPad leaves room beside the strip for the handles that sit outside it.
	stripPad = handleWidth + 12
)

// host is what the editor needs from its window, so tests can stand in for one.
type host interface {
	// update runs fn on the UI thread and redraws. Any goroutine may call it.
	update(fn func())
	// saveAs asks where to save, from a goroutine; "" means cancelled.
	saveAs(dir, name string) (string, error)
	reveal(path string)
}

type grab int

const (
	grabNone grab = iota
	grabStart
	grabEnd
	grabPlayhead
)

// job is the export's state: jobIdle, *jobRunning, jobDone or jobFailed.
type job interface{ isJob() }

type jobIdle struct{}

type jobRunning struct {
	progress float64
	cancel   context.CancelFunc
}

type jobDone struct {
	path  string
	bytes int64
}

type jobFailed struct{ err string }

func (jobIdle) isJob()     {}
func (*jobRunning) isJob() {}
func (jobDone) isJob()     {}
func (jobFailed) isJob()   {}

// editor is the Trim & Compress window's state. Its fields change on the UI thread only.
type editor struct {
	host host
	cfg  Config
	path string

	// openErr is set when the video cannot be opened; src is nil until it has been.
	openErr string
	src     *Source
	player  *Player
	trim    Trim
	opts    Options
	hevc    bool
	thumbs  []*ui.Bitmap
	video   *ui.Bitmap
	shown   Frame
	drag    grab
	stripW  float32
	job     job
	closed  bool
}

func newEditor(h host, path string, cfg Config) *editor {
	e := &editor{host: h, cfg: cfg, path: path, opts: DefaultOptions(), job: jobIdle{}}
	e.hevc = slices.Contains(media.AvailableCodecs(), media.HEVC)
	return e
}

// load probes the video and reads the thumbnails, off the UI thread.
func (e *editor) load() {
	src, err := probe(e.path)
	e.host.update(func() {
		if e.closed {
			return
		}
		if err != nil {
			e.openErr = err.Error()
			return
		}
		e.src, e.trim = &src, FullTrim(src.DurationMS)
		e.player = NewPlayer(src.Path, func() { e.host.update(e.showFrame) })
	})
	if err != nil {
		return
	}
	thumbs, err := thumbnails(e.path)
	if err != nil {
		return
	}
	e.host.update(func() { e.thumbs = thumbs })
}

func probe(path string) (Source, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Source{}, fmt.Errorf("reading %s: %w", path, err)
	}
	r, err := media.OpenFrameReader(path)
	if err != nil {
		return Source{}, fmt.Errorf("Windows cannot decode this video: %w", err)
	}
	defer r.Close()
	if r.DurationMS() < MinTrimMS {
		return Source{}, errors.New("the video is too short to trim")
	}
	w, h := r.Size()
	return Source{Path: path, Width: w, Height: h, FPS: max(int(math.Round(r.FPS())), 1), DurationMS: r.DurationMS(), Bytes: st.Size()}, nil
}

func thumbnails(path string) ([]*ui.Bitmap, error) {
	r, err := media.OpenVideoReader(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	frames, err := r.Thumbnails(thumbCount)
	out := make([]*ui.Bitmap, 0, len(frames))
	for _, f := range frames {
		w, h := f.Image.Rect.Dx(), f.Image.Rect.Dy()
		tw := max(w*thumbHeight/max(h, 1), 1)
		out = append(out, ui.NewBitmap(media.Shrink(f.Image, min(tw, w), min(thumbHeight, h))))
	}
	return out, err
}

// showFrame puts the player's newest frame in the preview.
func (e *editor) showFrame() {
	if e.player == nil {
		return
	}
	f, ok := e.player.TakeFrame()
	if !ok {
		return
	}
	if e.video == nil {
		e.video = ui.NewBitmap(f.Image)
	} else {
		e.video.Update(f.Image)
	}
	e.player.Recycle(e.shown.Image)
	e.shown = f
	if e.cfg.OnFrame != nil {
		shown := Shown{FrameMS: f.TMS}
		shown.ClockMS, shown.Audio, shown.Playing = e.player.ClockMS()
		e.cfg.OnFrame(shown)
	}
}

func (e *editor) close() {
	e.closed = true
	if r, ok := e.job.(*jobRunning); ok {
		r.cancel()
	}
	if p := e.player; p != nil {
		go p.Close()
	}
}

func (e *editor) togglePlay() {
	p := e.player
	if p == nil {
		return
	}
	if p.Playing() {
		p.Pause()
		return
	}
	if pos := p.PositionMS(); pos < e.trim.StartMS || pos+40 >= e.trim.EndMS {
		p.SeekMS(e.trim.StartMS)
	}
	p.Play(e.trim.EndMS, !e.opts.KeepAudio)
}

func (e *editor) mark(g grab) {
	if e.player == nil {
		return
	}
	at := e.player.PositionMS()
	switch g {
	case grabStart:
		e.trim = e.trim.WithStart(at)
	case grabEnd:
		e.trim = e.trim.WithEnd(at, e.src.DurationMS)
	}
}

// timeAt maps x, in DIPs from the strip's left edge, to media time.
func (e *editor) timeAt(x float32) int64 {
	if e.stripW <= 0 {
		return 0
	}
	f := min(max(x/e.stripW, 0), 1)
	return int64(float64(f) * float64(e.src.DurationMS))
}

func (e *editor) xOf(ms int64) float32 {
	return e.stripW * float32(ms) / float32(max(e.src.DurationMS, 1))
}

// grabAt picks what a press at x takes: a handle within a few DIPs of it, else the playhead.
func (e *editor) grabAt(x float32) grab {
	xs, xe := e.xOf(e.trim.StartMS), e.xOf(e.trim.EndMS)
	switch {
	case x <= xs+4 && x >= xs-handleWidth-4:
		return grabStart
	case x >= xe-4 && x <= xe+handleWidth+4:
		return grabEnd
	}
	return grabPlayhead
}

func (e *editor) press(x float32) {
	e.player.Pause()
	e.drag = e.grabAt(x)
	e.dragTo(x)
}

func (e *editor) dragTo(x float32) {
	t := e.timeAt(x)
	frameMS := int64(1000 / max(e.src.FPS, 1))
	switch e.drag {
	case grabStart:
		e.trim = e.trim.WithStart(t)
		e.player.SeekMS(e.trim.StartMS)
	case grabEnd:
		e.trim = e.trim.WithEnd(t, e.src.DurationMS)
		e.player.SeekMS(max(e.trim.EndMS-frameMS, 0))
	case grabPlayhead:
		e.player.SeekMS(min(t, max(e.src.DurationMS-frameMS, 0)))
	}
}

func (e *editor) setOptions(fn func(*Options)) {
	fn(&e.opts)
	switch e.job.(type) {
	case jobDone, jobFailed:
		e.job = jobIdle{}
	}
}

func (e *editor) saveDir() string {
	if e.cfg.SaveDir != "" {
		return e.cfg.SaveDir
	}
	return filepath.Dir(e.src.Path)
}

func (e *editor) export(saveAs bool) {
	if _, running := e.job.(*jobRunning); running || e.src == nil {
		return
	}
	e.player.Pause()
	topts := transcodeOptions(*e.src, e.trim, e.opts)
	src, stem, dir := e.src.Path, OutputStem(e.src.Path), e.saveDir()
	if !saveAs {
		e.start(src, uniquePath(dir, sanitize(stem), "mp4"), topts)
		return
	}
	go func() {
		os.MkdirAll(dir, 0o755)
		dest, err := e.host.saveAs(dir, stem+".mp4")
		e.host.update(func() {
			switch {
			case err != nil:
				e.job = jobFailed{err.Error()}
			case dest != "" && !e.closed:
				e.start(src, dest, topts)
			}
		})
	}()
}

func (e *editor) start(src, dest string, topts media.TranscodeOptions) {
	ctx, cancel := context.WithCancel(context.Background())
	run := &jobRunning{cancel: cancel}
	e.job = run
	go func() {
		defer cancel()
		var last time.Time
		bytes, err := writeClip(ctx, src, dest, topts, func(p float64) {
			if time.Since(last) < 30*time.Millisecond && p < 1 {
				return
			}
			last = time.Now()
			e.host.update(func() { run.progress = p })
		})
		e.host.update(func() {
			if e.job != run {
				return
			}
			switch {
			case err == nil:
				e.job = jobDone{dest, bytes}
			case ctx.Err() != nil:
				e.job = jobIdle{}
			default:
				e.job = jobFailed{err.Error()}
			}
		})
	}()
}

func (e *editor) cancel() {
	if r, ok := e.job.(*jobRunning); ok {
		r.cancel()
	}
}

// writeClip transcodes into a partial file beside dest and renames it into place, so dest
// only ever holds a finished video. The source is never overwritten.
func writeClip(ctx context.Context, src, dest string, topts media.TranscodeOptions, progress func(float64)) (int64, error) {
	if sameFile(src, dest) {
		return 0, errors.New("choose another name: the original is never overwritten")
	}
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("creating %s: %w", dir, err)
	}
	stem := filepath.Base(dest)
	stem = stem[:len(stem)-len(filepath.Ext(stem))]
	partial := filepath.Join(dir, stem+".partial.mp4")
	if _, err := media.Transcode(ctx, src, partial, topts, progress); err != nil {
		os.Remove(partial)
		return 0, err
	}
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		os.Remove(partial)
		return 0, fmt.Errorf("replacing %s: %w", dest, err)
	}
	if err := os.Rename(partial, dest); err != nil {
		os.Remove(partial)
		return 0, fmt.Errorf("writing %s: %w", dest, err)
	}
	st, err := os.Stat(dest)
	if err != nil {
		return 0, err
	}
	return st.Size(), nil
}

func sameFile(a, b string) bool {
	sa, err := os.Stat(a)
	if err != nil {
		return false
	}
	sb, err := os.Stat(b)
	return err == nil && os.SameFile(sa, sb)
}

func (e *editor) view(c *ui.Context) {
	p := theme.Apply(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	} else {
		c.Root().Background(p.Base)
	}
	if e.src != nil {
		switch {
		case c.Shortcut(0, ui.KeySpace):
			e.togglePlay()
		case c.Shortcut(0, ui.KeyI):
			e.mark(grabStart)
		case c.Shortcut(0, ui.KeyO):
			e.mark(grabEnd)
		}
	}
	bar := c.TitleBar()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(max(bar.Height, 40)).Padding(0, bar.Right+12, 0, bar.Left+14).Gap(10).DragWindow().Children(func() {
			ui.Image(c, theme.Mark(false)).Size(18, 18)
			ui.Text(c, Title(e.path)).FontSize(theme.Caption).SingleLine()
		})
		panel := ui.Column(c).Grow(1).Background(p.Base).Radius(theme.RadiusWindow).Border(1, p.Border).Margin(0, 8, 8, 8)
		panel.Children(func() {
			switch {
			case e.openErr != "":
				message(c, p, "This video can't be opened: "+e.openErr)
			case e.src == nil:
				message(c, p, "Opening…")
			default:
				e.preview(c)
				e.transport(c, p)
				e.scrubber(c, p)
				e.settings(c, p)
				e.footer(c, p)
			}
		})
	})
}

// Title is the window's title for a video at path.
func Title(path string) string { return "Trim & Compress — " + filepath.Base(path) }

func message(c *ui.Context, p theme.Palette, text string) {
	ui.Column(c).Grow(1).Center().Padding(32).Children(func() {
		ui.Text(c, text).TextColor(p.Muted)
	})
}

func (e *editor) preview(c *ui.Context) {
	ui.Box(c).Grow(1).MinHeight(120).Margin(12, 12, 0, 12).Radius(theme.RadiusWindow).Background(ui.RGB(0, 0, 0)).Clip().
		Label("Preview").Draw(func(p *ui.Painter, r ui.Rect) {
		if e.video != nil {
			p.Image(e.video, r, ui.Contain)
		}
	})
}

func (e *editor) transport(c *ui.Context, p theme.Palette) {
	pl := e.player
	ui.Row(c).Gap(12).Padding(8, 12, 0, 12).Children(func() {
		icon := theme.IconPlay
		if pl.Playing() {
			icon = theme.IconPause
		}
		if ui.Button(c, "").Label("Play/Pause").Tooltip("Play/Pause (Space)").Children(func() {
			ui.Icon(c, theme.Icon(icon)).Size(16, 16)
		}).Clicked() {
			e.togglePlay()
		}
		ui.Text(c, Clock(pl.PositionMS())+" / "+Clock(e.src.DurationMS))
		ui.Text(c, pl.Err()).Grow(1).FontSize(theme.Caption).TextColor(p.Muted).SingleLine()
		if ui.Button(c, "Set Start").Tooltip("I").Clicked() {
			e.mark(grabStart)
		}
		if ui.Button(c, "Set End").Tooltip("O").Clicked() {
			e.mark(grabEnd)
		}
		ui.Text(c, fmt.Sprintf("Selection %s – %s · %.1f s", Clock(e.trim.StartMS), Clock(e.trim.EndMS), float64(e.trim.DurationMS())/1000)).
			TextColor(p.Muted)
	})
}

func (e *editor) scrubber(c *ui.Context, p theme.Palette) {
	box := ui.Box(c).Height(stripHeight+8).Margin(8, 0, 0, 0).Padding(4, stripPad).Label("Timeline")
	cursor := ui.CursorPointer
	if x, _, over := box.PointerPosition(); over && e.grabAt(x-stripPad) != grabPlayhead || e.drag == grabStart || e.drag == grabEnd {
		cursor = ui.CursorResizeEW
	}
	box.Cursor(cursor).HandleInput(func(ev ui.InputEvent) bool {
		switch {
		case ev.Kind == ui.InputPointerDown && ev.Button == 0:
			e.press(ev.X - stripPad)
		case ev.Kind == ui.InputPointerMove && e.drag != grabNone:
			e.dragTo(ev.X - stripPad)
		case ev.Kind == ui.InputPointerUp && e.drag != grabNone:
			e.drag = grabNone
		default:
			return false
		}
		return true
	})
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		strip := ui.Rect{X: r.X + stripPad, Y: r.Y + 4, W: r.W - 2*stripPad, H: stripHeight}
		e.stripW = strip.W
		e.drawStrip(pt, p, strip)
	})
}

func (e *editor) drawStrip(pt *ui.Painter, p theme.Palette, s ui.Rect) {
	pt.Fill(s, p.Canvas, theme.RadiusControl)
	pt.Clip(s, theme.RadiusControl, func() {
		if n := len(e.thumbs); n > 0 {
			w := s.W / float32(n)
			for i, th := range e.thumbs {
				cell := ui.Rect{X: s.X + w*float32(i), Y: s.Y, W: w, H: s.H}
				pt.Clip(cell, 0, func() { pt.Image(th, cell, ui.Cover) })
			}
		}
		dim := ui.RGBA(0, 0, 0, 0.63)
		xs, xe := s.X+e.xOf(e.trim.StartMS), s.X+e.xOf(e.trim.EndMS)
		pt.Fill(ui.Rect{X: s.X, Y: s.Y, W: xs - s.X, H: s.H}, dim, 0)
		pt.Fill(ui.Rect{X: xe, Y: s.Y, W: s.X + s.W - xe, H: s.H}, dim, 0)
		pt.Fill(ui.Rect{X: xs, Y: s.Y, W: xe - xs, H: 2}, p.Accent, 0)
		pt.Fill(ui.Rect{X: xs, Y: s.Y + s.H - 2, W: xe - xs, H: 2}, p.Accent, 0)
	})
	xs, xe := s.X+e.xOf(e.trim.StartMS), s.X+e.xOf(e.trim.EndMS)
	for _, h := range []ui.Rect{{X: xs - handleWidth, Y: s.Y, W: handleWidth, H: s.H}, {X: xe, Y: s.Y, W: handleWidth, H: s.H}} {
		pt.Fill(h, p.Accent, theme.RadiusControl)
		pt.Fill(ui.Rect{X: h.X + h.W/2 - 1, Y: h.Y + h.H/2 - 9, W: 2, H: 18}, p.OnAccent, 1)
	}
	xp := s.X + e.xOf(min(max(e.player.PositionMS(), 0), e.src.DurationMS))
	pt.Fill(ui.Rect{X: xp - 1, Y: s.Y - 4, W: 2, H: s.H + 8}, ui.RGB(255, 255, 255), 1)
}

func (e *editor) settings(c *ui.Context, p theme.Palette) {
	row := func(label string, fn func()) {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, label).Width(84).TextColor(p.Muted)
			fn()
		})
	}
	audio := e.player.Audio()
	ui.Column(c).Margin(12, 12, 0, 12).Padding(12).Radius(theme.RadiusWindow).Background(p.Layer).Border(1, p.Border).Gap(8).Children(func() {
		row("Resolution", func() {
			labels, tips := make([]string, len(Resolutions)), make([]string, len(Resolutions))
			for i, r := range Resolutions {
				w, h := OutputSize(e.src.Width, e.src.Height, r)
				labels[i], tips[i] = r.Label(), fmt.Sprintf("%d × %d", w, h)
			}
			sel := int(e.opts.Resolution)
			if choice(c.Key("resolution"), p, "Resolution", &sel, labels, tips) {
				e.setOptions(func(o *Options) { o.Resolution = Resolutions[sel] })
			}
		})
		row("Quality", func() {
			labels := make([]string, len(Qualities))
			for i, q := range Qualities {
				o := e.opts
				o.Quality = q
				labels[i] = fmt.Sprintf("%s · ≈%s", q.Label(), HumanBytes(EstimateBytes(*e.src, e.trim, o, audio == AudioPresent)))
			}
			sel := int(e.opts.Quality)
			if choice(c.Key("quality"), p, "Quality", &sel, labels, nil) {
				e.setOptions(func(o *Options) { o.Quality = Qualities[sel] })
			}
		})
		row("Codec", func() {
			codecs := []media.VideoCodec{media.H264}
			if e.hevc {
				codecs = append(codecs, media.HEVC)
			}
			labels := make([]string, len(codecs))
			for i, k := range codecs {
				labels[i] = k.String()
			}
			sel := max(slices.Index(codecs, e.opts.Codec), 0)
			if choice(c.Key("codec"), p, "Codec", &sel, labels, nil) {
				e.setOptions(func(o *Options) { o.Codec = codecs[sel] })
			}
		})
		row("Audio", func() {
			switch audio {
			case AudioUnknown:
				ui.Text(c, "Reading audio…").TextColor(p.Muted)
			case AudioAbsent:
				ui.Text(c, "No audio track").TextColor(p.Muted)
			default:
				sel := 0
				if !e.opts.KeepAudio {
					sel = 1
				}
				if choice(c.Key("audio"), p, "Audio", &sel, []string{"Keep", "Remove"}, nil) {
					e.setOptions(func(o *Options) { o.KeepAudio = sel == 0 })
				}
			}
		})
	})
}

// choice is a segmented control in Glint's look: the chosen segment in the accent color.
func choice(c *ui.Context, p theme.Palette, name string, sel *int, labels, tips []string) bool {
	seg := ui.SegmentedBase(c, sel, len(labels))
	seg.Track.Label(name).Padding(2).Gap(2).Radius(theme.RadiusControl).Background(p.Control).Border(1, p.Border).Children(func() {
		for i, label := range labels {
			s := seg.Segment(i).Padding(4, 12).Radius(theme.RadiusControl - 1).Label(label)
			switch {
			case i == *sel:
				s.Background(p.Accent).TextColor(p.OnAccent)
			case s.Hovered():
				s.Background(p.ControlHover)
			}
			if tips != nil {
				s.Tooltip(tips[i])
			}
			s.Children(func() { ui.Text(c, label).SingleLine() })
		}
	})
	return seg.Track.Changed()
}

func (e *editor) footer(c *ui.Context, p theme.Palette) {
	ui.Row(c).Gap(8).Padding(12).Children(func() {
		switch j := e.job.(type) {
		case *jobRunning:
			ui.Text(c, fmt.Sprintf("Exporting %.0f%%", j.progress*100)).Width(110)
			ui.Progress(c, j.progress).Label("Export progress").Grow(1)
			if ui.Button(c, "Cancel").Clicked() {
				e.cancel()
			}
		case jobDone:
			saved := 1 - float64(j.bytes)/float64(max(e.src.Bytes, 1))
			ui.Icon(c, theme.Icon(theme.IconCheck)).Size(16, 16).TextColor(p.Accent)
			ui.Text(c, fmt.Sprintf("Saved %s · %s (%+.0f%%)", filepath.Base(j.path), HumanBytes(j.bytes), -saved*100)).Grow(1).SingleLine()
			if iconButton(c, "Show in "+fileManager, theme.IconFolder, false).Clicked() {
				e.host.reveal(j.path)
			}
			if iconButton(c, "Copy", theme.IconCopy, false).Clicked() {
				if data, err := transfer.FileData(j.path); err == nil {
					c.Services().WriteClipboardData(data)
				}
			}
			if iconButton(c, "Export Again", theme.IconSave, true).Clicked() {
				e.export(false)
			}
		default:
			status := ui.Text(c, fmt.Sprintf("Source %d × %d · %d fps · %s", e.src.Width, e.src.Height, e.src.FPS, HumanBytes(e.src.Bytes))).TextColor(p.Muted)
			if f, ok := j.(jobFailed); ok {
				status = ui.Text(c, "Export failed: "+f.err).TextColor(p.Danger)
			}
			status.Grow(1).SingleLine()
			if iconButton(c, "Save As…", theme.IconSaveAs, false).Clicked() {
				e.export(true)
			}
			if iconButton(c, "Export", theme.IconSave, true).Clicked() {
				e.export(false)
			}
		}
	})
}

func iconButton(c *ui.Context, label string, icon theme.IconName, primary bool) ui.Element {
	var b ui.Element
	if primary {
		b = ui.PrimaryButton(c.Key(label), "")
	} else {
		b = ui.Button(c.Key(label), "")
	}
	return b.Label(label).Children(func() {
		ui.Icon(c, theme.Icon(icon)).Size(16, 16)
		ui.Text(c, label).SingleLine()
	})
}
