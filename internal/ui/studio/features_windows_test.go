package studio

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/render"
	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/transcribe"
)

func dragOn(tt *ui.Tester, x0, y0, x1, y1 float32) {
	tt.Press(x0, y0)
	for i := 1; i <= 6; i++ {
		f := float32(i) / 6
		tt.Move(x0+(x1-x0)*f, y0+(y1-y0)*f)
	}
	tt.Release(x1, y1)
}

func click(t *testing.T, tt *ui.Tester, label string) {
	t.Helper()
	if err := tt.Click(label); err != nil {
		t.Fatalf("%v (texts %q)", err, tt.Texts())
	}
}

func settleFor(t *testing.T, e *editor, sh *fakeShell, tt *ui.Tester, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out; player error: " + e.player.errText())
		}
		time.Sleep(10 * time.Millisecond)
		sh.drain()
		tt.Frame()
	}
}

func TestCameraBubbleMovesResizesAndHides(t *testing.T) {
	t.Setenv("GLINT_DATA_DIR", t.TempDir())
	e, _, _, tt, _ := openStudio(t, testSession(t, true))
	if !e.hasCamera() {
		t.Fatal("the session's camera track did not open")
	}
	click(t, tt, "Camera")
	box, _ := tt.Find("Preview")
	at := func() (float32, float32, float32, float32) {
		b := bubbleRect(e.project().Design.Camera, e.frame.w, e.frame.h)
		return box.X + e.frame.x + b.x, box.Y + e.frame.y + b.y, b.w, b.h
	}
	before := e.project().Design.Camera.Center
	x, y, w, h := at()
	dragOn(tt, x+w/2, y+h/2, x+w/2-200, y+h/2-100)
	moved := e.project().Design.Camera.Center
	if !(moved[0] < before[0] && moved[1] < before[1]) {
		t.Errorf("bubble center %v after dragging up and left from %v", moved, before)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := e.project().Design.Camera.Center; got != before {
		t.Errorf("one undo left the center at %v, want %v: the drag was more than one step", got, before)
	}
	tt.Key(ui.Cmd, ui.KeyY)
	size := e.project().Design.Camera.Size
	x, y, w, h = at()
	dragOn(tt, x+w, y+h, x+w+60, y+h+60)
	if got := e.project().Design.Camera.Size; got <= size {
		t.Errorf("size %v after dragging the handle out from %v", got, size)
	}
	click(t, tt, "Show camera")
	if e.project().Design.Camera.Visible || e.hasCamera() {
		t.Error("Show camera did not hide the bubble")
	}
}

func TestPresetSavedAsDefaultShapesTheNextRecording(t *testing.T) {
	t.Setenv("GLINT_DATA_DIR", t.TempDir())
	e, _, _, tt, _ := openStudio(t, testSession(t))
	e.edit("", func(p *core.Project) { p.Design.Padding = 0.15 })
	tt.Frame()
	click(t, tt, "Presets")
	click(t, tt, "Preset name")
	tt.Type("Slides")
	tt.Key(0, ui.KeyEnter)
	if n := len(e.project().Clips); n != 1 {
		t.Errorf("typing the name reached the timeline: %d clips", n)
	}
	if !tt.HasText("Slides") {
		t.Fatalf("no preset listed: %q", tt.Texts())
	}
	click(t, tt, "Use for new recordings")
	if p, ok := e.presets.DefaultPreset(); !ok || p.Name != "Slides" {
		t.Fatalf("default preset %+v %v", p, ok)
	}
	next, _, _, _, _ := openStudio(t, testSession(t))
	if got := next.project().Design.Padding; got != 0.15 {
		t.Errorf("a new recording opened with padding %v, want the default preset's 0.15", got)
	}
}

var testWords = []transcribe.Word{
	{Text: "First", StartMs: 500, EndMs: 900, Confidence: 1},
	{Text: "Um,", StartMs: 1_500, EndMs: 1_800, Confidence: 1},
	{Text: "second", StartMs: 2_500, EndMs: 3_000, Confidence: 1},
	{Text: "third", StartMs: 3_500, EndMs: 4_000, Confidence: 1},
	{Text: "fourth", StartMs: 5_000, EndMs: 5_500, Confidence: 1},
	{Text: "last.", StartMs: 8_000, EndMs: 8_600, Confidence: 1},
}

func transcribedStudio(t *testing.T) (*editor, *fakeShell, *ui.Tester) {
	t.Helper()
	t.Setenv("GLINT_DATA_DIR", t.TempDir())
	dir := testSession(t)
	if err := transcribe.NewTranscript("en", testWords).Save(filepath.Join(dir, transcribe.TranscriptFile)); err != nil {
		t.Fatal(err)
	}
	e, sh, _, tt, _ := openStudio(t, dir)
	click(t, tt, "Captions")
	return e, sh, tt
}

func TestCaptionLinesEditAndStyle(t *testing.T) {
	e, _, tt := transcribedStudio(t)
	if len(e.project().CaptionCues) == 0 {
		t.Fatal("no caption lines from the transcript")
	}
	click(t, tt, "Edit line 1")
	tt.Key(ui.Cmd, ui.KeyA)
	tt.Type("Hello there")
	tt.Key(0, ui.KeyEnter)
	if got := e.project().CaptionCues[0].Text; got != "Hello there" {
		t.Errorf("line 1 reads %q after editing", got)
	}
	click(t, tt, "Top")
	click(t, tt, "Highlight spoken word")
	if c := e.project().Captions; c.Position != 0.1 || !c.Karaoke {
		t.Errorf("captions %+v, want top with karaoke", c)
	}
	click(t, tt, "Line at "+formatTime(e.outputAt(core.Span{Start: e.project().CaptionCues[0].StartMs, End: e.project().CaptionCues[0].EndMs})))
	if got := e.position(); math.Abs(got-500) > 1 {
		t.Errorf("clicking line 1's time went to %v ms, want 500", got)
	}
}

func TestTranscriptSeeksCutsWordsFillersAndSilences(t *testing.T) {
	e, sh, tt := transcribedStudio(t)
	click(t, tt, "Edit video")
	click(t, tt, "second")
	if got := e.position(); got != 2_500 {
		t.Errorf("clicking a word went to %v ms, want 2500", got)
	}
	if err := tt.ClickWith(ui.Shift, "third"); err != nil {
		t.Fatal(err)
	}
	before := e.duration()
	tt.Key(0, ui.KeyDelete)
	if cut := before - e.duration(); cut < 1_500 || cut > 2_500 {
		t.Errorf("cutting 'second third' removed %.0f ms", cut)
	}
	if _, kept := e.project().EventOutputTime(2_700); kept {
		t.Error("the cut word still plays")
	}
	before = e.duration()
	click(t, tt, "Remove filler words")
	if _, kept := e.project().EventOutputTime(1_600); kept || e.duration() >= before {
		t.Errorf("the filler survived: %.0f ms before, %.0f after", before, e.duration())
	}
	before = e.duration()
	click(t, tt, "0.5 s")
	click(t, tt, "Trim silences")
	settle(t, e, sh, tt, func() bool { return !e.cap.trimming })
	if e.duration() >= before {
		t.Errorf("trimming silences left %.0f ms of %.0f", e.duration(), before)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if math.Abs(e.duration()-before) > 1 {
		t.Errorf("undo after trimming gave %.0f ms, want %.0f", e.duration(), before)
	}
}

func TestExportCancelLeavesNoFileThenExportsTheTimeline(t *testing.T) {
	e, sh, h, tt, dir := testStudio(t)
	start := func() {
		e.openExport()
		d := e.export.(*exportDialog)
		d.opts.Resolution, d.opts.Fps, d.opts.MotionBlur = core.Res720, core.Fps30, false
		e.runJob(jobExport)
	}
	start()
	settle(t, e, sh, tt, func() bool { r, ok := e.export.(*exportRunning); return ok && r.progress > 0.02 })
	e.cancelExport()
	settle(t, e, sh, tt, func() bool { return !e.exportRunning() })
	if e.notice == nil || e.notice.text != "Export cancelled" {
		t.Errorf("notice after cancel: %+v", e.notice)
	}
	files, _ := os.ReadDir(h.s.SaveDir)
	renders, _ := filepath.Glob(filepath.Join(dir, "render", "export-*"))
	if len(files) != 0 || len(renders) != 0 {
		t.Fatalf("cancelled export left %v in the export folder and %v in render/", files, renders)
	}

	start()
	settleFor(t, e, sh, tt, 2*time.Minute, func() bool { return !e.exportRunning() })
	if len(h.exported) != 1 {
		t.Fatalf("exported %v; notice %+v", h.exported, e.notice)
	}
	if !strings.HasPrefix(e.notice.text, "Saved ") || e.notice.file != h.exported[0] {
		t.Errorf("notice %+v", e.notice)
	}
	r, err := media.OpenVideoReader(h.exported[0])
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if got, want := float64(r.DurationMS()), e.duration(); math.Abs(got-want) > 40 {
		t.Errorf("export lasts %v ms, the timeline %v ms", got, want)
	}
	if e.project().Export.Resolution != core.Res720 {
		t.Error("the dialog's options were not remembered in the project")
	}
}

// The preset name keeps the focus after Save preset is clicked, and it takes Enter; the
// export dialog opened over it still has to.
func TestExportDialogTakesEnterAndEscapeWhateverHadTheFocus(t *testing.T) {
	e, sh, _, tt, _ := testStudio(t)
	click(t, tt, "Presets")
	click(t, tt, "Preset name")
	tt.Type("Look")
	click(t, tt, "Save preset")
	presets, zooms := len(e.presetList), len(e.project().Zooms)

	tt.Key(ui.Cmd, ui.KeyE)
	tt.Key(0, ui.KeyEscape)
	if _, closed := e.export.(exportClosed); !closed {
		t.Fatalf("Escape left the export dialog as %T", e.export)
	}

	tt.Key(ui.Cmd, ui.KeyE)
	click(t, tt, "720p")
	tt.Key(0, ui.KeyZ)
	tt.Key(0, ui.KeyEnter)
	if !e.exportRunning() {
		t.Fatalf("Enter left the export as %T", e.export)
	}
	if len(e.presetList) != presets || e.presetName != "" || len(e.project().Zooms) != zooms {
		t.Errorf("keys reached behind the dialog: %d presets, want %d; preset name %q; %d zooms, want %d", len(e.presetList), presets, e.presetName, len(e.project().Zooms), zooms)
	}
	tt.Key(0, ui.KeyEscape)
	settle(t, e, sh, tt, func() bool { return !e.exportRunning() })
	if e.notice == nil || e.notice.text != "Export cancelled" {
		t.Errorf("notice after Escape during the export: %+v", e.notice)
	}
}

func TestCursorAudioBackgroundAndExportChoices(t *testing.T) {
	e, _, _, tt, _ := testStudio(t)
	click(t, tt, "Cursor")
	click(t, tt, "Click highlights")
	if e.project().ClickHighlights == core.DefaultProject().ClickHighlights {
		t.Error("Click highlights did not change")
	}
	click(t, tt, "Audio")
	enabled := e.project().Audio.System.Enabled
	click(t, tt, "Include")
	if e.project().Audio.System.Enabled == enabled {
		t.Error("Include did not change system audio")
	}
	click(t, tt, "Background")
	click(t, tt, "Image")
	if e.wallpaper != "" {
		if bg, ok := e.project().Design.Background.(render.ImageBackground); !ok || bg.Path != e.wallpaper {
			t.Errorf("Image chose %#v, want the wallpaper %s", e.project().Design.Background, e.wallpaper)
		}
	}
	e.openExport()
	tt.Frame()
	click(t, tt, "GIF")
	if d := e.export.(*exportDialog); d.opts.Format != core.FormatGif || tt.HasText("Frame rate") || tt.HasText("Codec") {
		t.Errorf("GIF chosen: format %v, frame rate shown %v, codec shown %v", d.opts.Format, tt.HasText("Frame rate"), tt.HasText("Codec"))
	}
	tt.Key(0, ui.KeyEscape)
	if _, closed := e.export.(exportClosed); !closed {
		t.Error("Escape left the export dialog open")
	}
}

func TestEscapeRestoresTheCropAndCloseSavesOrReportsTheError(t *testing.T) {
	e, _, _, tt, dir := testStudio(t)
	e.enterCrop()
	tt.Frame()
	click(t, tt, "1:1")
	if e.project().Crop == core.FullCrop {
		t.Fatal("1:1 did not crop")
	}
	tt.Key(0, ui.KeyEscape)
	if _, ok := e.mode.(normalMode); !ok || e.project().Crop != core.FullCrop {
		t.Errorf("after Escape: mode %T, crop %+v", e.mode, e.project().Crop)
	}

	e.edit("", func(p *core.Project) { p.Design.Shadow = 0.9 })
	if !e.closeRequested() || savedProject(t, dir).Design.Shadow != 0.9 {
		t.Error("closing did not save the last edit")
	}

	path := filepath.Join(dir, core.ProjectFile)
	os.Remove(path)
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	e.edit("", func(p *core.Project) { p.Design.Shadow = 0.1 })
	if e.closeRequested() {
		t.Fatal("the window closed although project.json could not be written")
	}
	tt.Frame()
	if !tt.HasText("Close without saving") {
		t.Fatalf("no save error shown: %q", tt.Texts())
	}
}

func TestZoomKeysAndTimelineZoom(t *testing.T) {
	e, _, _, tt, _ := testStudio(t)
	n := len(e.project().Zooms)
	e.seek(9_000)
	tt.Key(0, ui.KeyZ)
	if len(e.project().Zooms) != n+1 || e.sel.kind != selZoom || e.tab != tabZoom {
		t.Fatalf("Z: %d zooms, selection %+v, tab %v", len(e.project().Zooms), e.sel, e.tab)
	}
	click(t, tt, "Enabled")
	if e.project().Zooms[e.sel.i].Enabled {
		t.Error("Enabled did not turn the zoom off")
	}
	tt.Key(0, ui.KeyDelete)
	if len(e.project().Zooms) != n {
		t.Errorf("Delete left %d zooms, want %d", len(e.project().Zooms), n)
	}
	click(t, tt, "Zoom timeline in")
	if e.tlZoom <= 1 {
		t.Errorf("timeline zoom %v after zooming in", e.tlZoom)
	}
}
