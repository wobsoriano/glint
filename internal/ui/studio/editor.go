package studio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	// coalesce joins edits of one control made within this long into one undo step.
	coalesce  = time.Second
	saveDelay = 700 * time.Millisecond
	frameMs   = 1000.0 / 60
)

type selKind int

const (
	selNone selKind = iota
	selClip
	selZoom
)

// selection is the clip or zoom the timeline has selected.
type selection struct {
	kind selKind
	i    int
}

func (s selection) clip() (int, bool) { return s.i, s.kind == selClip }
func (s selection) zoom() (int, bool) { return s.i, s.kind == selZoom }

// mode is what a press on the preview does: normalMode, cropMode or focusMode.
type mode interface{ isMode() }

type normalMode struct{}

// cropMode shows the whole recording to crop it; before is the crop to go back to.
type cropMode struct {
	ratio  cropRatio
	before core.CropRect
}

// focusMode places a fixed zoom's focus on the preview.
type focusMode struct{ zoom int }

func (normalMode) isMode() {}
func (cropMode) isMode()   {}
func (focusMode) isMode()  {}

// drag is what the pointer moves while pressed: one of the *Drag types.
type drag interface{ isDrag() }

type scrubDrag struct{}

type trimDrag struct {
	clip   int
	end    bool
	base   core.Project
	fromMs float64
	scale  timeScale
}

type zoomPart int

const (
	zoomStart zoomPart = iota
	zoomEnd
	zoomBody
)

type zoomDrag struct {
	zoom   int
	part   zoomPart
	base   core.Project
	span   core.Span
	fromMs float64
	scale  timeScale
}

type cameraDrag struct{ gx, gy float32 }

type cameraSizeDrag struct{}

type focusDrag struct{ zoom int }

type cropFromDragging struct{ ax, ay float32 }

type cropMoveDragging struct {
	gx, gy float32
	base   core.CropRect
}

func (scrubDrag) isDrag()        {}
func (trimDrag) isDrag()         {}
func (zoomDrag) isDrag()         {}
func (cameraDrag) isDrag()       {}
func (cameraSizeDrag) isDrag()   {}
func (focusDrag) isDrag()        {}
func (cropFromDragging) isDrag() {}
func (cropMoveDragging) isDrag() {}

type tab int

const (
	tabBackground tab = iota
	tabLayout
	tabCursor
	tabKeystrokes
	tabCamera
	tabAudio
	tabZoom
	tabCaptions
	tabPresets
)

var tabs = []struct {
	tab   tab
	icon  theme.IconName
	title string
}{
	{tabBackground, theme.IconImage, "Background"},
	{tabLayout, theme.IconArea, "Layout"},
	{tabCursor, theme.IconSelect, "Cursor"},
	{tabKeystrokes, theme.IconKeyboard, "Keystrokes"},
	{tabCamera, theme.IconCamera, "Camera"},
	{tabAudio, theme.IconSpeaker, "Audio"},
	{tabZoom, theme.IconZoom, "Zoom"},
	{tabCaptions, theme.IconCaptions, "Captions"},
	{tabPresets, theme.IconStack, "Presets"},
}

type notice struct {
	text string
	// file is a saved video the notice offers to show and copy.
	file string
	err  bool
}

// editor is the Studio window's state. Its fields change on the UI thread only.
type editor struct {
	host    Host
	sh      shell
	opts    Options
	session *core.Session
	player  *player
	title   string

	history     core.History
	lastGroup   string
	lastGroupAt time.Time
	sel         selection
	mode        mode
	drag        drag
	tab         tab
	focus       ui.Handle

	video *ui.Bitmap
	shown shownFrame
	// preview is the preview area's size and frame the frame's box inside it, in DIPs, from the
	// last paint.
	previewW, previewH float32
	frame              rect

	thumbs  []*ui.Bitmap
	tracksW float32
	tlZoom  float64
	tlStart float64

	presets       core.PresetStore
	presetList    []core.Preset
	defaultPreset string
	presetName    string
	nameInput     ui.Handle
	hue           float32
	wallpaper     string

	export exportState
	hevc   bool
	cap    captionsState

	saveGen   int
	dirty     bool
	saveErr   string
	notice    *notice
	noticeGen int
	gone      bool
}

func newEditor(host Host, sh shell, dir string, o Options) (*editor, error) {
	s, err := core.LoadSession(dir)
	if err != nil {
		return nil, err
	}
	presets := core.NewPresetStore(filepath.Join(settings.DataDir(), "StudioPresets"))
	if _, err := os.Stat(filepath.Join(dir, core.ProjectFile)); errors.Is(err, os.ErrNotExist) {
		if p, ok := presets.DefaultPreset(); ok {
			p.Apply(&s.Project)
		}
		if err := s.SaveProject(); err != nil {
			return nil, err
		}
	}
	title := fileStem(dir)
	if item, err := store.Load(dir); err == nil && item.Meta.Title != "" {
		title = item.Meta.Title
	}
	e := &editor{
		host: host, sh: sh, opts: o, session: s, title: title,
		mode: normalMode{}, tlZoom: 1, presets: presets, hue: 210,
		export: exportClosed{}, hevc: slices.Contains(media.AvailableCodecs(), media.HEVC),
	}
	e.cap = newCaptionsState(s.Tracks.Mic != "")
	e.wallpaper, _ = platform.Wallpaper()
	e.reloadPresets()
	if e.player, err = newPlayer(s, func() { sh.update(e.showFrame) }); err != nil {
		return nil, err
	}
	return e, nil
}

// start loads what the window shows after its first frame: the thumbnails and the notices of
// sidecar files that could not be read.
func (e *editor) start() {
	snap := *e.session
	go func() {
		thumbs, err := core.TimelineThumbnails(&snap, thumbCount, thumbHeight)
		e.sh.update(func() {
			if err != nil {
				e.notify(fmt.Sprintf("Couldn't load timeline thumbnails: %v", err), true)
				return
			}
			e.thumbs = make([]*ui.Bitmap, len(thumbs))
			for i, t := range thumbs {
				e.thumbs[i] = ui.NewBitmap(t)
			}
		})
	}()
	if len(e.session.Notices) > 0 {
		text := ""
		for i, n := range e.session.Notices {
			if i > 0 {
				text += " "
			}
			text += n
		}
		e.notify(text, true)
	}
}

func (e *editor) project() *core.Project { return &e.session.Project }

func (e *editor) duration() float64 { return e.project().DurationMs() }

// snapshot is a copy of the session for work off the UI thread, which edits never reach.
func (e *editor) snapshot() *core.Session {
	s := *e.session
	s.Project = e.session.Project.Clone()
	s.Notices = nil
	return &s
}

// edit changes the project through f. Edits of one group (a slider, a drag) within a second
// of each other, or within one drag, make a single undo step.
func (e *editor) edit(group string, f func(p *core.Project)) {
	next := e.session.Project.Clone()
	f(&next)
	if reflect.DeepEqual(next, e.session.Project) {
		return
	}
	joined := group != "" && group == e.lastGroup && (e.drag != nil || time.Since(e.lastGroupAt) < coalesce)
	if !joined {
		e.history.Record(&e.session.Project)
	}
	e.lastGroup, e.lastGroupAt = group, time.Now()
	e.session.Project = next
	e.projectChanged()
}

func (e *editor) undo() {
	if e.history.Undo(&e.session.Project) {
		e.lastGroup = ""
		e.projectChanged()
	}
}

func (e *editor) redo() {
	if e.history.Redo(&e.session.Project) {
		e.lastGroup = ""
		e.projectChanged()
	}
}

func (e *editor) projectChanged() {
	p := e.project()
	switch {
	case e.sel.kind == selClip && e.sel.i < len(p.Clips):
	case e.sel.kind == selZoom && e.sel.i < len(p.Zooms):
	default:
		e.sel = selection{}
	}
	if m, ok := e.mode.(focusMode); ok && m.zoom >= len(p.Zooms) {
		e.mode = normalMode{}
	}
	if e.position() > e.duration() {
		e.player.seek(e.duration())
	}
	e.player.setProject(e.previewProject())
	e.scheduleSave()
}

// previewProject is what the preview shows: the project, but the whole recording while
// cropping and no zooms while placing a zoom's focus.
func (e *editor) previewProject() core.Project {
	p := e.session.Project.Clone()
	switch e.mode.(type) {
	case cropMode:
		p.Crop = core.FullCrop
		p.Zooms = nil
		p.Aspect = core.AspectOriginal
		p.AspectMode = core.AspectFit
	case focusMode:
		p.Zooms = nil
		p.AspectMode = core.AspectFit
	}
	return p
}

func (e *editor) setMode(m mode) {
	e.mode = m
	e.player.setProject(e.previewProject())
}

func (e *editor) scheduleSave() {
	e.dirty = true
	e.saveGen++
	gen := e.saveGen
	time.AfterFunc(saveDelay, func() {
		e.sh.update(func() {
			if e.saveGen == gen && !e.gone {
				e.saveNow()
			}
		})
	})
}

func (e *editor) saveNow() bool {
	err := e.session.SaveProject()
	e.saveErr = ""
	if err != nil {
		e.saveErr = err.Error()
	}
	e.dirty = err != nil
	return err == nil
}

// closeRequested stops the work in flight and saves; a failed save keeps the window open
// with the error and a way to close without saving.
func (e *editor) closeRequested() bool {
	e.cancelExport()
	e.cap.cancel()
	return !e.dirty || e.saveNow()
}

func (e *editor) closed() {
	e.gone = true
	e.cancelExport()
	e.cap.cancel()
	go e.player.close()
}

func (e *editor) discardAndClose() {
	e.dirty = false
	e.sh.destroy()
}

func (e *editor) notify(text string, isErr bool) { e.showNotice(notice{text: text, err: isErr}) }

func (e *editor) showNotice(n notice) {
	linger := 3 * time.Second
	if n.file != "" {
		linger = 8 * time.Second
	}
	e.notice = &n
	e.noticeGen++
	gen := e.noticeGen
	time.AfterFunc(linger, func() {
		e.sh.update(func() {
			if e.noticeGen == gen {
				e.notice = nil
			}
		})
	})
}

// showFrame puts the player's newest frame in the preview.
func (e *editor) showFrame() {
	f, ok := e.player.takeFrame()
	if !ok {
		return
	}
	img := asRGBA(f.img)
	if e.video == nil {
		e.video = ui.NewBitmap(img)
	} else {
		e.video.Update(img)
	}
	if e.shown.img != nil {
		core.RecycleFrame(e.shown.img)
	}
	e.shown = f
	if e.opts.OnFrame != nil {
		s := Shown{OutputMs: f.outputMs}
		s.ClockMs, s.Audio, s.Playing = e.player.clockMs()
		e.opts.OnFrame(s)
	}
}

func (e *editor) position() float64 { return e.player.positionMs() }

func (e *editor) togglePlay() {
	if e.player.isPlaying() {
		e.player.pause()
		return
	}
	if _, ok := e.mode.(normalMode); !ok {
		e.setMode(normalMode{})
	}
	e.player.play()
}

func (e *editor) pause() {
	if e.player.isPlaying() {
		e.player.pause()
	}
}

func (e *editor) seek(ms float64) { e.player.seek(max(0, min(ms, e.duration()))) }

func (e *editor) step(deltaMs float64) {
	e.pause()
	e.seek(e.position() + deltaMs)
}

func (e *editor) split() {
	t := e.position()
	var i int
	var ok bool
	e.edit("", func(p *core.Project) { i, ok = p.SplitAt(t) })
	if ok {
		e.sel = selection{selClip, i}
	} else {
		e.notify("Too close to a cut to split here", false)
	}
}

func (e *editor) deleteSelection() {
	switch e.sel.kind {
	case selClip:
		ok := false
		e.edit("", func(p *core.Project) { ok = p.DeleteClip(e.sel.i) })
		if !ok {
			e.notify("The last clip can't be deleted", false)
		}
	case selZoom:
		if _, ok := e.mode.(focusMode); ok {
			e.mode = normalMode{}
		}
		i := e.sel.i
		e.edit("", func(p *core.Project) { p.RemoveZoom(i) })
	default:
		return
	}
	e.sel = selection{}
}

func (e *editor) addZoom() {
	t := e.position()
	end := min(t+2_000, e.duration())
	start := min(max(end-2_000, 0), t)
	var i int
	e.edit("", func(p *core.Project) { i = p.AddZoom(core.Span{Start: start, End: end}) })
	e.sel = selection{selZoom, i}
	e.tab = tabZoom
}

func (e *editor) setSpeed(speed float64) {
	if i, ok := e.sel.clip(); ok {
		e.edit("", func(p *core.Project) { p.SetSpeed(i, speed) })
	}
}

func (e *editor) selectItem(s selection) {
	e.sel = s
	if s.kind == selZoom {
		e.tab = tabZoom
	}
	if m, ok := e.mode.(focusMode); ok && s != (selection{selZoom, m.zoom}) {
		e.setMode(normalMode{})
	}
}

func (e *editor) escape() {
	if _, ok := e.mode.(normalMode); ok && e.cap.words != nil {
		e.cap.words = nil
		return
	}
	switch m := e.mode.(type) {
	case cropMode:
		e.edit("crop", func(p *core.Project) { p.Crop = m.before })
		e.setMode(normalMode{})
	case focusMode:
		e.setMode(normalMode{})
	default:
		e.selectItem(selection{})
	}
}

func (e *editor) endDrag() {
	if e.drag != nil {
		e.drag = nil
		e.lastGroup = ""
	}
}

type shortcut struct {
	mods ui.Modifiers
	key  ui.Key
	do   func()
}

// shortcuts asks for every key Studio takes each frame, then runs those pressed. Plain keys
// stand down while a text field has the focus.
func (e *editor) shortcuts(c *ui.Context) {
	cmd, shift := ui.Cmd, ui.Shift
	if _, closed := e.export.(exportClosed); !closed {
		var fire []func()
		for _, s := range []shortcut{{0, ui.KeyEscape, e.cancelExport}, {0, ui.KeyEnter, func() { e.runJob(jobExport) }}} {
			if c.Shortcut(s.mods, s.key) {
				fire = append(fire, s.do)
			}
		}
		for _, f := range fire {
			f()
		}
		return
	}
	list := []shortcut{
		{cmd, ui.KeyZ, e.undo},
		{cmd | shift, ui.KeyZ, e.redo},
		{cmd, ui.KeyY, e.redo},
		{cmd, ui.KeyE, e.openExport},
		{cmd, ui.KeyC, func() { e.runJob(jobCopy) }},
		{cmd, ui.KeyS, func() { e.runJob(jobSave) }},
	}
	if !e.nameInput.Focused(c) && !e.cap.cueInput.Focused(c) {
		list = append(list,
			shortcut{0, ui.KeySpace, e.togglePlay},
			shortcut{0, ui.KeyK, e.pause},
			shortcut{0, ui.KeyLeft, func() { e.step(-frameMs) }},
			shortcut{0, ui.KeyJ, func() { e.step(-frameMs) }},
			shortcut{shift, ui.KeyLeft, func() { e.step(-1000) }},
			shortcut{shift, ui.KeyJ, func() { e.step(-1000) }},
			shortcut{0, ui.KeyRight, func() { e.step(frameMs) }},
			shortcut{0, ui.KeyL, func() { e.step(frameMs) }},
			shortcut{shift, ui.KeyRight, func() { e.step(1000) }},
			shortcut{shift, ui.KeyL, func() { e.step(1000) }},
			shortcut{0, ui.KeyHome, func() { e.seek(0) }},
			shortcut{0, ui.KeyEnd, func() { e.seek(e.duration()) }},
			shortcut{0, ui.KeyS, e.split},
			shortcut{0, ui.KeyZ, e.addZoom},
			shortcut{0, ui.KeyDelete, e.deleteKey},
			shortcut{0, ui.KeyBackspace, e.deleteKey},
			shortcut{0, ui.KeyEnter, func() {
				if _, ok := e.mode.(normalMode); !ok {
					e.setMode(normalMode{})
				}
			}},
			shortcut{0, ui.KeyEscape, e.escape},
		)
	}
	var fire []func()
	for _, s := range list {
		if c.Shortcut(s.mods, s.key) {
			fire = append(fire, s.do)
		}
	}
	for _, f := range fire {
		f()
	}
}

func (e *editor) deleteKey() {
	if e.hasWordSelection() {
		e.cutSelectedWords()
		return
	}
	e.deleteSelection()
}

func (e *editor) view(c *ui.Context) {
	pal := theme.Apply(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	e.shortcuts(c)
	ui.Column(c.Key("studio")).Fill().Background(pal.Base).Focusable().FocusRing(false).Bind(&e.focus).AutoFocus().Children(func() {
		e.header(c, pal)
		if e.saveErr != "" {
			e.saveErrorBar(c, pal)
		}
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Children(func() {
				e.preview(c, pal)
				e.timeline(c, pal)
			})
			e.sidebar(c, pal)
		})
		e.exportOverlay(c, pal)
	})
}

func (e *editor) header(c *ui.Context, pal theme.Palette) {
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, 48)).Padding(0, bar.Right+8, 0, bar.Left+14).Gap(4).Background(pal.Layer).
		BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).DragWindow().Children(func() {
		ui.Icon(c, theme.Icon(theme.IconFilm)).Size(16, 16).TextColor(pal.Muted)
		ui.Text(c, e.title).FontSize(13).FontWeight(theme.Semibold).SingleLine().Margin(0, 0, 0, 6)
		if e.dirty && e.saveErr == "" {
			ui.Text(c, "Saving…").FontSize(12).TextColor(pal.Muted).Margin(0, 0, 0, 6)
		}
		ui.Spacer(c)
		if iconButton(c, "undo", theme.IconUndo, false, e.history.CanUndo(), "Undo (Ctrl+Z)", pal) {
			e.undo()
		}
		if iconButton(c, "redo", theme.IconRedo, false, e.history.CanRedo(), "Redo (Ctrl+Y)", pal) {
			e.redo()
		}
		separator(c, pal)
		if button(c, "copy", "Copy", theme.IconCopy, false, pal).Clicked() {
			e.runJob(jobCopy)
		}
		if button(c, "save", "Save", theme.IconSave, false, pal).Clicked() {
			e.runJob(jobSave)
		}
		if button(c, "export", "Export…", theme.IconFilm, true, pal).Clicked() {
			e.openExport()
		}
	})
}

func (e *editor) saveErrorBar(c *ui.Context, pal theme.Palette) {
	ui.Row(c).Gap(8).Padding(8, 12).Background(pal.Danger.Alpha(0.2)).BorderWidth(0, 0, 1, 0).BorderColor(pal.Danger).Children(func() {
		ui.Text(c, "Couldn't save the project: "+e.saveErr).FontSize(13).Grow(1).SingleLine()
		if button(c, "retry-save", "Retry", noIcon, false, pal).Clicked() {
			e.saveNow()
		}
		if button(c, "discard", "Close without saving", noIcon, false, pal).Clicked() {
			e.discardAndClose()
		}
	})
}
