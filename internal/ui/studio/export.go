package studio

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/actions"
	"github.com/wobsoriano/glint/internal/settings"
	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

type jobKind int

const (
	jobExport jobKind = iota
	jobSave
	jobCopy
)

// exportState is the export overlay: exportClosed, *exportDialog or *exportRunning.
type exportState interface{ isExport() }

type exportClosed struct{}

type exportDialog struct {
	opts  core.ExportOptions
	audio bool
}

type exportRunning struct {
	job      jobKind
	progress float64
	cancel   context.CancelFunc
}

func (exportClosed) isExport()   {}
func (*exportDialog) isExport()  {}
func (*exportRunning) isExport() {}

var resolutions = []struct {
	res   core.Resolution
	label string
}{{core.ResSource, "Source"}, {core.Res720, "720p"}, {core.Res1080, "1080p"}, {core.Res1440, "1440p"}, {core.Res2160, "4K"}}

func (e *editor) exportRunning() bool {
	_, ok := e.export.(*exportRunning)
	return ok
}

func (e *editor) openExport() {
	if e.exportRunning() {
		return
	}
	opts := e.project().Export
	if !e.hevc {
		opts.Codec = core.CodecH264
	}
	e.export = &exportDialog{opts: opts, audio: true}
}

func (e *editor) cancelExport() {
	if r, ok := e.export.(*exportRunning); ok {
		r.cancel()
		return
	}
	e.export = exportClosed{}
}

func (e *editor) saveDir() string {
	if dir := e.host.Settings().SaveDir; dir != "" {
		return dir
	}
	return settings.DefaultSaveDir()
}

// runJob renders the project and saves the render to the export folder, or puts it on the
// clipboard. Export takes the dialog's options and remembers them; Save and Copy use the
// project's own.
func (e *editor) runJob(job jobKind) {
	if e.exportRunning() {
		return
	}
	snap := e.snapshot()
	var opts core.ExportOptions
	if d, ok := e.export.(*exportDialog); ok && job == jobExport {
		if !d.audio {
			snap.Project.Audio.Mic.Enabled = false
			snap.Project.Audio.System.Enabled = false
		}
		opts = d.opts
		if e.session.Project.Export != opts {
			e.session.Project.Export = opts
			e.scheduleSave()
		}
	} else if job == jobExport {
		e.openExport()
		return
	} else {
		opts = e.project().Export
	}
	e.pause()
	ctx, cancel := context.WithCancel(context.Background())
	run := &exportRunning{job: job, cancel: cancel}
	e.export = run
	dir, stem, id := e.saveDir(), actions.Sanitize(e.title), filepath.Base(e.session.Dir)
	var mu sync.Mutex
	var last time.Time
	progress := func(p float64) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(last) < 50*time.Millisecond && p < 1 {
			return
		}
		last = time.Now()
		e.sh.update(func() { run.progress = p })
	}
	go func() {
		defer cancel()
		out, err := func() (string, error) {
			rendered, err := core.Export(ctx, snap, opts, progress)
			if err != nil {
				return "", err
			}
			if job == jobCopy {
				return actions.HardLinkOutsideCache(rendered, id, stem)
			}
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return "", err
			}
			out := actions.UniquePath(dir, stem, strings.TrimPrefix(filepath.Ext(rendered), "."))
			return out, copyFile(rendered, out)
		}()
		e.sh.update(func() {
			if e.export == exportState(run) {
				e.export = exportClosed{}
			}
			e.jobDone(job, out, err)
		})
	}()
}

func (e *editor) jobDone(job jobKind, out string, err error) {
	switch {
	case errors.Is(err, core.ErrCancelled):
		e.notify("Export cancelled", false)
	case err != nil:
		e.notify("Export failed: "+err.Error(), true)
	case job == jobCopy:
		if err := e.host.CopyFile(out); err != nil {
			e.notify("Copy failed: "+err.Error(), true)
			return
		}
		e.notify("Copied "+filepath.Base(out), false)
	default:
		e.showNotice(notice{text: "Saved " + filepath.Base(out), file: out})
		// The app hands out the render of the saved project, so the project on disk has to
		// be the one just rendered.
		if e.dirty {
			e.saveNow()
		}
		e.host.RecordingExported(out)
	}
}

// copyFile writes src to dst through a partial file, so dst only ever holds a whole video.
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	partial := dst + ".partial"
	out, err := os.Create(partial)
	if err != nil {
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(partial)
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(partial)
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	if err := os.Rename(partial, dst); err != nil {
		os.Remove(partial)
		return fmt.Errorf("writing %s: %w", dst, err)
	}
	return nil
}

func (e *editor) exportOverlay(c *ui.Context, pal theme.Palette) {
	if _, closed := e.export.(exportClosed); closed {
		return
	}
	// The overlay is modal so the keyboard stays with the dialog: the field or button that had
	// the focus behind it would otherwise take Enter. The card holds the focus itself, since
	// a focused chip would take Enter too, and the chips leave it there when clicked.
	ui.Overlay(c, func() {
		back := ui.Box(c.Key("export-overlay")).Absolute().Top(0).Left(0).Right(0).Bottom(0).Modal()
		escape, enter := back.OverlayShortcut(0, ui.KeyEscape), back.OverlayShortcut(0, ui.KeyEnter)
		// The scrim takes presses so nothing behind the card reacts; the card is its sibling,
		// not its child, or the scrim would take the card's presses too.
		back.Children(func() {
			ui.Box(c.Key("export-scrim")).Fill().Background(ui.RGBA(0, 0, 0, 0.5)).
				HandleInput(func(ev ui.InputEvent) bool { return ev.Kind == ui.InputPointerDown })
			ui.Column(c.Key("export-card")).Attach(ui.AnchorCenter, ui.AnchorCenter).Width(440).Padding(20).Gap(12).Radius(theme.RadiusWindow).Background(pal.Flyout).Border(1, pal.Border).
				Shadow(0, 12, 32, 0, ui.RGBA(0, 0, 0, 0.35)).Focusable().FocusRing(false).AutoFocus().Children(func() {
				switch s := e.export.(type) {
				case *exportDialog:
					e.exportDialog(c, s, pal)
				case *exportRunning:
					title := "Exporting…"
					if s.job == jobCopy {
						title = "Rendering for the clipboard…"
					}
					ui.Text(c, title).FontSize(15).FontWeight(theme.Semibold)
					progressBar(c, s.progress, "Export progress", pal)
					ui.Row(c).Children(func() {
						ui.Text(c, fmt.Sprintf("%.0f%%", s.progress*100)).FontSize(13).TextColor(pal.Muted).Grow(1)
						if button(c, "export-cancel", "Cancel", noIcon, false, pal).Clicked() {
							e.cancelExport()
						}
					})
				}
			})
		})
		switch {
		case escape:
			e.cancelExport()
		case enter:
			e.runJob(jobExport)
		}
	})
}

func (e *editor) exportDialog(c *ui.Context, d *exportDialog, pal theme.Palette) {
	gif := d.opts.Format == core.FormatGif
	probe := e.project().Clone()
	probe.Export = d.opts
	w, h := probe.OutputSize(e.session.SourceW, e.session.SourceH)
	hasAudio := e.session.Tracks.Mic != "" || e.session.Tracks.System != ""
	row := func(label string, body func()) {
		ui.Row(c).Gap(8).Children(func() {
			ui.Text(c, label).Width(96).Shrink(0).FontSize(13).TextColor(pal.Muted)
			ui.Row(c).Wrap().Gap(4).Grow(1).Children(body)
		})
	}
	ui.Text(c, "Export recording").FontSize(15).FontWeight(theme.Semibold)
	row("Format", func() {
		for _, f := range []struct {
			f     core.Format
			label string
		}{{core.FormatMp4, "MP4"}, {core.FormatGif, "GIF"}} {
			if chip(c, "fmt-"+f.label, f.label, d.opts.Format == f.f, pal) {
				d.opts.Format = f.f
			}
		}
	})
	if !gif {
		row("Codec", func() {
			if chip(c, "codec-h264", "H.264", d.opts.Codec == core.CodecH264, pal) {
				d.opts.Codec = core.CodecH264
			}
			if e.hevc && chip(c, "codec-hevc", "HEVC", d.opts.Codec == core.CodecHevc, pal) {
				d.opts.Codec = core.CodecHevc
			}
		})
	}
	row("Resolution", func() {
		for _, r := range resolutions {
			if chip(c, "res-"+r.label, r.label, d.opts.Resolution == r.res, pal) {
				d.opts.Resolution = r.res
			}
		}
	})
	row("", func() {
		text := fmt.Sprintf("%d × %d", w, h)
		if gif {
			text = "GIFs are 15 fps, at most 480 px on the short side"
		}
		hint(c, text, pal)
	})
	row("Quality", func() {
		for _, q := range []struct {
			q     core.Quality
			label string
		}{{core.QualityLow, "Low"}, {core.QualityMedium, "Medium"}, {core.QualityHigh, "High"}} {
			if chip(c, "q-"+q.label, q.label, d.opts.Quality == q.q, pal) {
				d.opts.Quality = q.q
			}
		}
	})
	if !gif {
		row("Frame rate", func() {
			if chip(c, "fps-30", "30 fps", d.opts.Fps == core.Fps30, pal) {
				d.opts.Fps = core.Fps30
			}
			if chip(c, "fps-60", "60 fps", d.opts.Fps == core.Fps60, pal) {
				d.opts.Fps = core.Fps60
			}
		})
	}
	if toggle(c, "blur", "Motion blur", d.opts.MotionBlur, pal) {
		d.opts.MotionBlur = !d.opts.MotionBlur
	}
	if !gif && hasAudio && toggle(c, "audio", "Include audio", d.audio, pal) {
		d.audio = !d.audio
	}
	ui.Row(c).Gap(8).Padding(8, 0, 0, 0).Children(func() {
		ui.Spacer(c)
		if button(c, "export-close", "Cancel", noIcon, false, pal).Clicked() {
			e.cancelExport()
		}
		if button(c, "export-go", "Export", theme.IconSave, true, pal).Clicked() {
			e.runJob(jobExport)
		}
	})
	hint(c, "Saves to "+e.saveDir(), pal)
}
