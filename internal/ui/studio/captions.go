package studio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/settings"
	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/transcribe"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

var models = []struct {
	model transcribe.Model
	label string
}{{transcribe.ModelTinyEn, "Fast"}, {transcribe.ModelBaseEn, "Accurate"}, {transcribe.ModelBase, "Multilingual"}}

var silenceChoices = []struct {
	ms    uint64
	label string
}{{500, "0.5 s"}, {1_000, "1 s"}, {2_000, "2 s"}}

const (
	silenceKeepMs   = 150
	silenceMinCutMs = 200
)

var captionPositions = []struct {
	at    float32
	label string
}{{0.1, "Top"}, {0.5, "Middle"}, {0.92, "Bottom"}}

type capView int

const (
	viewCaptions capView = iota
	viewEditVideo
)

type stageKind int

const (
	stageDownloading stageKind = iota
	stageLoading
	stageTranscribing
)

// captionJob is a transcription on its way: downloading the model, loading the audio, or
// recognizing it, with its progress.
type captionJob struct {
	stage    stageKind
	progress float64
	cancel   context.CancelFunc
}

type captionsState struct {
	view      capView
	model     transcribe.Model
	useSystem bool
	job       *captionJob
	redoSetup bool
	// editing is the caption line being edited, or -1.
	editing  int
	editText string
	cueInput ui.Handle
	// words is the transcript selection, anchor and caret, or nil.
	words        *[2]int
	silenceMinMs uint64
	trimming     bool
}

func newCaptionsState(hasMic bool) captionsState {
	data := settings.DataDir()
	model := transcribe.ModelBaseEn
	for _, m := range []transcribe.Model{transcribe.ModelBaseEn, transcribe.ModelTinyEn, transcribe.ModelBase} {
		if transcribe.IsInstalled(data, m) {
			model = m
			break
		}
	}
	return captionsState{model: model, useSystem: !hasMic, editing: -1, silenceMinMs: 1_000}
}

func (s *captionsState) busy() bool { return s.job != nil || s.trimming }

func (s *captionsState) cancel() {
	if s.job != nil {
		s.job.cancel()
	}
}

// selectedWords is the selected run of words, first to last inclusive.
func (s *captionsState) selectedWords() (int, int, bool) {
	if s.words == nil {
		return 0, 0, false
	}
	return min(s.words[0], s.words[1]), max(s.words[0], s.words[1]), true
}

// audioPath is the track to transcribe: the one chosen, else the other.
func (e *editor) audioPath() string {
	t := e.session.Tracks
	if e.cap.useSystem {
		if t.System != "" {
			return t.System
		}
		return t.Mic
	}
	if t.Mic != "" {
		return t.Mic
	}
	return t.System
}

func readPcm(path string) (transcribe.Pcm, error) {
	w, err := media.ReadWAV(path)
	if err != nil {
		return transcribe.Pcm{}, err
	}
	return transcribe.Pcm{Samples: w.Samples, SampleRate: uint32(w.SampleRate), Channels: uint16(w.Channels)}, nil
}

func (e *editor) startTranscription() {
	if e.cap.job != nil {
		return
	}
	audio := e.audioPath()
	if audio == "" {
		e.notify("This recording has no audio to transcribe", true)
		return
	}
	model, data := e.cap.model, settings.DataDir()
	ctx, cancel := context.WithCancel(context.Background())
	job := &captionJob{stage: stageDownloading, cancel: cancel}
	e.cap.job = job
	var mu sync.Mutex
	var last time.Time
	lastStage := stageDownloading
	report := func(stage stageKind, p float64) {
		mu.Lock()
		defer mu.Unlock()
		if time.Since(last) < 50*time.Millisecond && stage == lastStage {
			return
		}
		last, lastStage = time.Now(), stage
		e.sh.update(func() { job.stage, job.progress = stage, p })
	}
	go func() {
		defer cancel()
		t, err := func() (transcribe.Transcript, error) {
			path, err := transcribe.DownloadModel(ctx, data, model, func(done, total uint64) {
				report(stageDownloading, float64(done)/float64(max(total, 1)))
			})
			if err != nil {
				return transcribe.Transcript{}, err
			}
			report(stageLoading, 0)
			pcm, err := readPcm(audio)
			if err != nil {
				return transcribe.Transcript{}, err
			}
			return transcribe.Transcribe(ctx, path, pcm, func(p float32) { report(stageTranscribing, float64(p)) })
		}()
		e.sh.update(func() {
			if e.cap.job != job {
				return
			}
			e.cap.job = nil
			switch {
			case err == nil:
				e.transcribed(t)
			case ctx.Err() != nil:
				e.notify("Transcription cancelled", false)
			case errors.Is(err, errors.ErrUnsupported):
				e.notify("Transcription isn't available on this system", true)
			default:
				e.notify("Transcription failed: "+err.Error(), true)
			}
		})
	}()
}

func (e *editor) transcribed(t transcribe.Transcript) {
	if len(t.Words) == 0 {
		e.notify("No speech found in this recording", false)
		return
	}
	cues := core.CaptionCues(t)
	if err := e.session.SetTranscript(t); err != nil {
		e.notify("Couldn't save the transcript: "+err.Error(), true)
		return
	}
	e.player.setAssets(e.session.Assets)
	e.cap.redoSetup, e.cap.words, e.cap.editing = false, nil, -1
	e.edit("", func(p *core.Project) {
		p.CaptionCues = cues
		p.Captions.Enabled = true
	})
	e.notify(fmt.Sprintf("Transcribed %d words", len(t.Words)), false)
}

func (e *editor) editCue(i int) {
	e.commitCue()
	if i < len(e.project().CaptionCues) {
		e.cap.editing, e.cap.editText = i, e.project().CaptionCues[i].Text
		e.cap.cueInput.Focus()
	}
}

func (e *editor) commitCue() {
	i := e.cap.editing
	if i < 0 {
		return
	}
	text := strings.Join(strings.Fields(e.cap.editText), " ")
	e.cap.editing = -1
	e.edit("", func(p *core.Project) {
		if i < len(p.CaptionCues) {
			p.CaptionCues[i].Text = text
		}
	})
	e.focus.Focus()
}

// outputAt is where a source span first shows on the output timeline, or where the footage
// after it resumes when the span was cut.
func (e *editor) outputAt(src core.Span) float64 {
	p := e.project()
	if r := p.OutputRanges(src); len(r) > 0 {
		return r[0].Start
	}
	if r := p.OutputRanges(core.Span{Start: src.Start, End: e.session.SourceDurationMs}); len(r) > 0 {
		return r[0].Start
	}
	return e.duration()
}

func (e *editor) clickWord(i int, shift bool) {
	if shift && e.cap.words != nil {
		e.cap.words = &[2]int{e.cap.words[0], i}
	} else {
		e.cap.words = &[2]int{i, i}
	}
	if t := e.session.Transcript; t != nil && i < len(t.Words) {
		w := t.Words[i]
		e.pause()
		e.seek(e.outputAt(core.Span{Start: float64(w.StartMs), End: float64(w.EndMs)}))
	}
}

func (e *editor) hasWordSelection() bool {
	return e.tab == tabCaptions && e.cap.view == viewEditVideo && e.cap.words != nil
}

func (e *editor) cutSelectedWords() {
	t := e.session.Transcript
	a, b, ok := e.cap.selectedWords()
	if t == nil || !ok {
		return
	}
	ranges := transcribe.WordCutRanges(t.Words, []transcribe.IndexRange{{Start: a, End: b + 1}}, uint64(e.session.SourceDurationMs))
	e.cap.words = nil
	e.cut(ranges, func(ms float64) string { return "Cut " + formatSeconds(ms) })
}

func (e *editor) removeFillers() {
	t := e.session.Transcript
	if t == nil {
		return
	}
	p := e.project()
	var fillers []transcribe.IndexRange
	n := 0
	for _, r := range transcribe.FillerRanges(t.Words) {
		for _, w := range t.Words[r.Start:r.End] {
			if _, ok := p.EventOutputTime(float64(w.StartMs)); ok {
				fillers = append(fillers, r)
				n += r.End - r.Start
				break
			}
		}
	}
	if len(fillers) == 0 {
		e.notify("No filler words left to remove", false)
		return
	}
	ranges := transcribe.WordCutRanges(t.Words, fillers, uint64(e.session.SourceDurationMs))
	e.cut(ranges, func(ms float64) string {
		plural := "s"
		if n == 1 {
			plural = ""
		}
		return fmt.Sprintf("Removed %d filler word%s (%s)", n, plural, formatSeconds(ms))
	})
}

func (e *editor) trimSilences() {
	if e.cap.trimming {
		return
	}
	audio := e.audioPath()
	if audio == "" {
		e.notify("This recording has no audio to trim", true)
		return
	}
	opts := transcribe.DefaultSilenceOptions()
	opts.MinDurationMs = e.cap.silenceMinMs
	e.cap.trimming = true
	go func() {
		pcm, err := readPcm(audio)
		var ranges []transcribe.TimeRange
		if err == nil {
			ranges = transcribe.SilenceCutRanges(transcribe.SilenceSpans(pcm, opts), silenceKeepMs, silenceMinCutMs)
		}
		e.sh.update(func() {
			e.cap.trimming = false
			switch {
			case err != nil:
				e.notify("Couldn't read the audio: "+err.Error(), true)
			case len(ranges) == 0:
				e.notify("No silences that long", false)
			default:
				e.cut(ranges, func(ms float64) string { return "Trimmed " + formatSeconds(ms) + " of silence" })
			}
		})
	}()
}

func (e *editor) cut(ranges []transcribe.TimeRange, message func(savedMs float64) string) {
	spans := make([]core.Span, len(ranges))
	for i, r := range ranges {
		spans[i] = core.Span{Start: float64(r.StartMs), End: float64(r.EndMs)}
	}
	before := e.duration()
	ok := false
	e.edit("", func(p *core.Project) { ok = p.CutSourceRanges(spans) })
	if !ok {
		e.notify("That would remove the whole recording", true)
		return
	}
	saved := before - e.duration()
	if saved <= 0 {
		e.notify("Already cut", false)
		return
	}
	e.notify(message(saved), false)
}

func (e *editor) captionsTab(c *ui.Context, pal theme.Palette) {
	t := e.session.Tracks
	switch {
	case t.Mic == "" && t.System == "":
		hint(c, "This recording has no audio, so there is nothing to transcribe.", pal)
	case e.cap.job != nil:
		e.jobSection(c, pal)
	case e.session.Transcript == nil || e.cap.redoSetup:
		e.setupSection(c, pal)
	default:
		ui.Row(c).Gap(4).Children(func() {
			if chip(c, "cap-view-captions", "Captions", e.cap.view == viewCaptions, pal) {
				e.cap.view = viewCaptions
			}
			if chip(c, "cap-view-edit", "Edit video", e.cap.view == viewEditVideo, pal) {
				e.commitCue()
				e.cap.view = viewEditVideo
			}
			ui.Spacer(c)
			if iconButton(c, "cap-redo", theme.IconRestart, false, true, "Transcribe again", pal) {
				e.cap.redoSetup = true
			}
		})
		if e.cap.view == viewCaptions {
			e.captionsView(c, pal)
		} else {
			e.editVideoView(c, pal)
		}
	}
}

func (e *editor) setupSection(c *ui.Context, pal theme.Palette) {
	data := settings.DataDir()
	m := e.cap.model
	t := e.session.Tracks
	again := e.session.Transcript != nil
	section(c, "Transcribe", pal, func() {
		hint(c, "Turns the narration into text on this PC. Nothing is uploaded.", pal)
		hint(c, "Model", pal)
		ui.Row(c).Gap(4).Children(func() {
			for _, mm := range models {
				if chip(c, "cap-model-"+string(mm.model), mm.label, m == mm.model, pal) {
					e.cap.model = mm.model
				}
			}
		})
		if transcribe.IsInstalled(data, m) {
			hint(c, m.Label()+" is installed.", pal)
		} else {
			hint(c, fmt.Sprintf("%s downloads once (%.0f MB).", m.Label(), float64(m.SizeBytes())/1e6), pal)
		}
		if t.Mic != "" && t.System != "" {
			hint(c, "Audio", pal)
			ui.Row(c).Gap(4).Children(func() {
				if chip(c, "cap-src-mic", "Microphone", !e.cap.useSystem, pal) {
					e.cap.useSystem = false
				}
				if chip(c, "cap-src-sys", "System audio", e.cap.useSystem, pal) {
					e.cap.useSystem = true
				}
			})
		}
		ui.Row(c).Gap(8).Children(func() {
			label := "Transcribe"
			if again {
				label = "Transcribe again"
			}
			if button(c, "cap-transcribe", label, theme.IconSparkles, true, pal).Clicked() {
				e.startTranscription()
			}
			if again && button(c, "cap-setup-back", "Back", noIcon, false, pal).Clicked() {
				e.cap.redoSetup = false
			}
		})
		if again {
			hint(c, "A new transcript replaces the caption lines and their edits.", pal)
		}
	})
}

func (e *editor) jobSection(c *ui.Context, pal theme.Palette) {
	j := e.cap.job
	title, known := "Preparing…", false
	switch j.stage {
	case stageDownloading:
		title, known = "Downloading "+e.cap.model.Label()+"…", true
	case stageTranscribing:
		title, known = "Transcribing…", true
	}
	section(c, "Transcribe", pal, func() {
		ui.Text(c, title).FontSize(13)
		p := 0.0
		if known {
			p = j.progress
		}
		progressBar(c, p, "Transcription progress", pal)
		ui.Row(c).Children(func() {
			text := ""
			if known {
				text = fmt.Sprintf("%.0f%%", j.progress*100)
			}
			ui.Text(c, text).FontSize(13).TextColor(pal.Muted).Grow(1)
			if button(c, "cap-cancel", "Cancel", noIcon, false, pal).Clicked() {
				e.cap.cancel()
			}
		})
	})
}

func (e *editor) captionsView(c *ui.Context, pal theme.Palette) {
	cs := e.project().Captions
	section(c, "Captions", pal, func() {
		e.toggleEdit(c, "cap-on", "Show captions", cs.Enabled, pal, func(p *core.Project) { p.Captions.Enabled = !p.Captions.Enabled })
		e.toggleEdit(c, "cap-karaoke", "Highlight spoken word", cs.Karaoke, pal, func(p *core.Project) { p.Captions.Karaoke = !p.Captions.Karaoke })
		near := captionPositions[0].at
		for _, cp := range captionPositions {
			if math.Abs(float64(cp.at-cs.Position)) < math.Abs(float64(near-cs.Position)) {
				near = cp.at
			}
		}
		ui.Row(c).Gap(4).Children(func() {
			for _, cp := range captionPositions {
				if chip(c, "cap-pos-"+cp.label, cp.label, near == cp.at, pal) {
					e.edit("", func(p *core.Project) { p.Captions.Position = cp.at })
				}
			}
		})
		e.sliderEdit(c, "cap-size", "Size", cs.Size, 0.6, 1.8, fmt.Sprintf("%.0f%%", cs.Size*100), pal, func(p *core.Project, v float32) {
			p.Captions.Size = float32(math.Round(float64(v)*20) / 20)
		})
	})
	p := e.project()
	src := p.SourceTime(e.position())
	section(c, "Lines", pal, func() {
		hint(c, "Click a time to jump there. Click a line to fix its text, Enter to save.", pal)
		for i, cue := range p.CaptionCues {
			shown := len(p.OutputRanges(core.Span{Start: cue.StartMs, End: cue.EndMs})) > 0
			active := shown && cue.StartMs <= src && src < cue.EndMs
			at := e.outputAt(core.Span{Start: cue.StartMs, End: cue.EndMs})
			row := ui.Row(c.Key(i)).Gap(8).Padding(4).Radius(4).AlignItems(ui.Start)
			if active {
				row.Background(pal.Accent.Alpha(0.15))
			}
			row.Children(func() {
				tb := ui.ButtonBase(c.Key("time")).Width(48).Label("Line at " + formatTime(at)).KeepFocus()
				if tb.Clicked() {
					e.commitCue()
					e.seek(at)
				}
				tb.Children(func() {
					t := ui.Text(c, formatTime(at)).FontSize(12).TextColor(pal.Accent)
					if tb.Hovered() {
						t.Underline()
					}
				})
				if e.cap.editing == i {
					in := ui.TextInput(c.Key("cue-edit"), &e.cap.editText).Label("Caption line").Grow(1).Bind(&e.cap.cueInput).AutoFocus()
					in.OnTextKey(func(k ui.TextKey) bool {
						if k.Key == ui.KeyEscape {
							e.cap.editing = -1
							e.focus.Focus()
							return true
						}
						return false
					})
					if in.Submitted() {
						e.commitCue()
					}
					return
				}
				lb := ui.ButtonBase(c.Key("text")).Grow(1).MinWidth(0).Justify(ui.Start).Padding(0, 4).Radius(4).Label("Edit line " + fmt.Sprint(i+1)).Cursor(ui.CursorText).KeepFocus()
				if lb.Hovered() {
					lb.Background(pal.ControlHover)
				}
				if lb.Clicked() {
					e.editCue(i)
				}
				lb.Children(func() {
					text := cue.Text
					col := pal.Text
					if text == "" {
						text, col = "(empty)", pal.Muted
					}
					t := ui.Text(c, text).FontSize(13)
					if !shown {
						t.Strikethrough()
						col = pal.Muted
					}
					t.TextColor(col)
				})
			})
		}
	})
}

func (e *editor) editVideoView(c *ui.Context, pal theme.Palette) {
	p := e.project()
	src := p.SourceTime(e.position())
	var words []transcribe.Word
	if e.session.Transcript != nil {
		words = e.session.Transcript.Words
	}
	active := sort.Search(len(words), func(i int) bool { return float64(words[i].StartMs) > src }) - 1
	if active >= 0 && src >= float64(words[active].EndMs) {
		active = -1
	}
	a, b, hasSel := e.cap.selectedWords()
	section(c, "Transcript", pal, func() {
		hint(c, "Click a word to jump to it. Shift-click to select a passage, then press Delete to cut it.", pal)
		ui.Row(c.Key("words")).Wrap().GapX(3).GapY(2).Children(func() {
			for i, w := range words {
				_, kept := p.EventOutputTime(float64(w.StartMs))
				selected := hasSel && i >= a && i <= b
				wb := ui.ButtonBase(c.Key(i)).Padding(0, 2).Radius(3).Label(w.Text).KeepFocus()
				col := pal.Text
				switch {
				case selected:
					wb.Background(pal.Accent)
					col = pal.OnAccent
				case i == active:
					wb.Background(pal.Accent.Alpha(0.25))
				case wb.Hovered():
					wb.Background(pal.ControlHover)
				}
				if wb.Clicked() {
					e.clickWord(i, wb.ClickModifiers()&ui.Shift != 0)
				}
				wb.Children(func() {
					t := ui.Text(c, w.Text).FontSize(13)
					if !kept {
						t.Strikethrough()
						if !selected {
							col = pal.Muted
						}
					}
					t.TextColor(col)
				})
			}
		})
		ui.Row(c).Gap(8).Children(func() {
			cut := button(c, "cap-cut", "Cut selection", theme.IconScissors, false, pal)
			if !hasSel {
				cut.Opacity(0.4)
			}
			if cut.Clicked() {
				e.cutSelectedWords()
			}
			if hasSel && button(c, "cap-clear", "Clear", noIcon, false, pal).Clicked() {
				e.cap.words = nil
			}
		})
	})
	section(c, "Clean up", pal, func() {
		if button(c, "cap-fillers", "Remove filler words", theme.IconSparkles, false, pal).Clicked() {
			e.removeFillers()
		}
		hint(c, "Cuts um, uh, er and similar sounds.", pal)
		hint(c, "Trim silences longer than", pal)
		ui.Row(c).Gap(4).Children(func() {
			for _, s := range silenceChoices {
				if chip(c, "cap-silence-"+s.label, s.label, e.cap.silenceMinMs == s.ms, pal) {
					e.cap.silenceMinMs = s.ms
				}
			}
		})
		label := "Trim silences"
		if e.cap.trimming {
			label = "Trimming…"
		}
		if button(c, "cap-silences", label, theme.IconScissors, false, pal).Clicked() {
			e.trimSilences()
		}
	})
}
