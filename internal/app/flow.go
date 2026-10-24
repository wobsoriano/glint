package app

import (
	"fmt"
	"image"
	"log"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/redact"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/overlay"
	"github.com/wobsoriano/glint/internal/ui/toast"
)

// shot is a screenshot on its way into the Library.
type shot struct {
	image   *image.RGBA
	dpi     float64
	title   string // the captured window's, empty for an area or a display
	created time.Time
}

// background runs work on a goroutine and the function it returns on the main thread.
func (a *App) background(work func() func()) {
	go func() {
		then := work()
		a.post(then)
	}()
}

// freeze reads every display's pixels before the overlay covers them.
func (a *App) freeze() ([]overlay.Shot, error) {
	ds, err := a.sys.displays()
	if err != nil {
		return nil, err
	}
	shots := make([]overlay.Shot, 0, len(ds))
	for _, d := range ds {
		img, err := a.sys.captureRect(d.Bounds)
		if err != nil {
			return nil, err
		}
		shots = append(shots, overlay.Shot{Display: d, Image: img})
	}
	return shots, nil
}

func (a *App) startArea(mode overlay.Mode) {
	if a.picker.IsOpen() {
		return
	}
	shots, err := a.freeze()
	if err != nil {
		a.toast("Capture failed: " + err.Error())
		return
	}
	a.picker.Open(shots, overlay.Options{
		Mode:      mode,
		ShowLoupe: a.settings.ShowLoupe,
		Windows:   a.sys.windows(),
		Cursor:    a.sys.cursor(),
		OnPick:    a.picked,
	})
}

func (a *App) picked(p overlay.Pick) {
	a.settings.LastArea = &settings.Area{int32(p.Rect.X), int32(p.Rect.Y), int32(p.Rect.W), int32(p.Rect.H)}
	a.saveSettings()
	if p.Mode == overlay.ModeText {
		a.acts.CopyTextOf(p.Image)
		return
	}
	s := shot{image: p.Image, dpi: p.Scale, created: time.Now()}
	if p.Window != nil {
		s.title = p.Window.Title
		// The window itself, without what covered it and with its shadow when the settings ask.
		if img, err := a.sys.captureWindow(p.Window.ID, a.settings.WindowShadow); err == nil {
			s.image = img
		} else {
			log.Printf("app: capturing window %q: %v; keeping the frozen pixels", p.Window.Title, err)
		}
	}
	a.finish(s)
}

// displayAt is the display holding p, or the first.
func displayAt(ds []platform.Display, p image.Point) platform.Display {
	for _, d := range ds {
		if d.Bounds.Contains(p.X, p.Y) {
			return d
		}
	}
	return ds[0]
}

func (a *App) captureScreen() {
	ds, err := a.sys.displays()
	if err != nil {
		a.toast("Capture failed: " + err.Error())
		return
	}
	d := displayAt(ds, a.sys.cursor())
	img, err := a.sys.captureRect(d.Bounds)
	if err != nil {
		a.toast("Capture failed: " + err.Error())
		return
	}
	a.finish(shot{image: img, dpi: d.Scale, created: time.Now()})
}

func (a *App) captureWindow() {
	w, ok := a.sys.foreground()
	if !ok {
		a.captureScreen()
		return
	}
	ds, err := a.sys.displays()
	if err != nil {
		a.toast("Capture failed: " + err.Error())
		return
	}
	c := image.Pt(w.Bounds.X+w.Bounds.W/2, w.Bounds.Y+w.Bounds.H/2)
	dpi := displayAt(ds, c).Scale
	img, err := a.sys.captureWindow(w.ID, a.settings.WindowShadow)
	if err != nil {
		log.Printf("app: capturing window %q: %v; reading the screen instead", w.Title, err)
		if img, err = a.sys.captureRect(w.Bounds); err != nil {
			a.toast("Capture failed: " + err.Error())
			return
		}
	}
	a.finish(shot{image: img, dpi: dpi, title: w.Title, created: time.Now()})
}

// capturePrevious captures the last area again, on the display holding its centre.
func (a *App) capturePrevious() {
	last := a.settings.LastArea
	if last == nil {
		a.startArea(overlay.ModeImage)
		return
	}
	ds, err := a.sys.displays()
	if err != nil {
		a.toast("Capture failed: " + err.Error())
		return
	}
	r := platform.Rect{X: int(last[0]), Y: int(last[1]), W: int(last[2]), H: int(last[3])}
	d := displayAt(ds, image.Pt(r.X+r.W/2, r.Y+r.H/2))
	r = r.Intersect(d.Bounds)
	if r.W < 2 || r.H < 2 {
		a.startArea(overlay.ModeImage)
		return
	}
	img, err := a.sys.captureRect(r)
	if err != nil {
		a.toast("Capture failed: " + err.Error())
		return
	}
	a.finish(shot{image: img, dpi: d.Scale, created: time.Now()})
}

// captureAfterTimer counts down in a toast, then captures the display with the pointer. The
// toast closes a moment before the capture, so it is not in it even when Glint's windows are.
func (a *App) captureAfterTimer() {
	if a.counting {
		return
	}
	a.counting = true
	secs := timerSeconds(a.settings)
	t := toast.ShowFor(countdown(secs), -1)
	go func() {
		for left := secs - 1; left > 0; left-- {
			time.Sleep(time.Second)
			mygo.RunOnMain(func() { t.SetText(countdown(left), -1) })
		}
		time.Sleep(time.Second - 150*time.Millisecond)
		mygo.RunOnMain(t.Close)
		time.Sleep(150 * time.Millisecond)
		mygo.RunOnMain(func() {
			a.counting = false
			a.captureScreen()
		})
	}()
}

func countdown(secs int) string {
	if secs == 1 {
		return "Capturing in 1 second…"
	}
	return fmt.Sprintf("Capturing in %d seconds…", secs)
}

// finish redacts the shot when the settings ask, then imports it.
func (a *App) finish(s shot) {
	if !a.settings.AutoRedact {
		a.importShot(s, nil)
		return
	}
	rules, pad := a.settings.RedactRules, float32(2*s.dpi)
	a.background(func() func() {
		lines, err := a.sys.recognize(s.image)
		if err != nil {
			log.Printf("app: automatic redaction: %v", err)
		}
		findings := redact.Find(redactLines(lines), rules, pad)
		return func() { a.importShot(s, findings) }
	})
}

func redactLines(lines []ocr.Line) []redact.Line {
	out := make([]redact.Line, len(lines))
	for i, l := range lines {
		words := make([]redact.Word, len(l.Words))
		for j, w := range l.Words {
			words[j] = redact.Word{Text: w.Text, Rect: redact.Rect(w.Bounds)}
		}
		out[i] = redact.Line{Words: words, Rect: redact.Rect(l.Bounds)}
	}
	return out
}

// redactionDoc hides each finding under an editable redaction, with no frame around the
// screenshot, as the Rust build's editor::redaction_doc.
func redactionDoc(findings []redact.Finding, style settings.RedactStyle, dpi float32) render.EditDoc {
	st := render.DefaultStyle()
	st.Background, st.Padding, st.Inset, st.Radius, st.Shadow = render.NoBackground{}, 0, 0, 0, 0
	st.Chrome, st.Aspect = render.ChromeNone, nil
	var kind render.RedactStyle
	if err := kind.UnmarshalText([]byte(style)); err != nil {
		kind = render.RedactBlur
	}
	doc := render.NewEditDoc(st, dpi, nil)
	for i, f := range findings {
		doc.Annotations = append(doc.Annotations, render.Annotation{
			ID:    uint64(i + 1),
			Shape: render.Redact{Rect: render.R{X: f.Rect.X, Y: f.Rect.Y, W: f.Rect.W, H: f.Rect.H}, Kind: kind, Strength: render.RedactStrength},
			Color: render.Black,
		})
	}
	return doc
}

// importShot writes the shot into the Library, then runs what the settings say happens after
// a capture.
func (a *App) importShot(s shot, findings []redact.Finding) {
	var doc *render.EditDoc
	if len(findings) > 0 {
		d := redactionDoc(findings, a.settings.RedactStyle, float32(s.dpi))
		doc = &d
	}
	lib := a.library
	a.background(func() func() {
		deliverable := s.image
		ns := store.NewScreenshot{Base: s.image, Created: s.created, Source: s.title}
		if doc != nil {
			deliverable = render.RenderDoc(s.image, *doc)
			ns.Edit = &store.Edit{Image: deliverable, Doc: doc.JSON()}
		}
		item, err := lib.ImportScreenshot(ns)
		return func() {
			if err != nil {
				a.toast("Could not add the capture to the Library: " + err.Error())
				return
			}
			a.last = item.Dir
			if a.Surfaces.Imported != nil {
				a.Surfaces.Imported(item)
			}
			if len(findings) > 0 {
				a.toast(fmt.Sprintf("Hid %d sensitive item(s)", len(findings)))
			}
			a.automate(item, deliverable, s.dpi)
		}
	})
}

// step is one thing that happens after a capture.
type step int

const (
	stepCopy step = iota
	stepSave
	stepPin
	stepEdit
	stepCard
	// stepCopiedToast confirms a copy when nothing else on screen shows the capture.
	stepCopiedToast
)

// plan is the order of the Rust build's automate: copy, save, pin, edit, then the card, or
// a toast for a copy nothing else shows.
func plan(after settings.AfterShot) []step {
	var steps []step
	for _, s := range []struct {
		on bool
		do step
	}{{after.Copy, stepCopy}, {after.Save, stepSave}, {after.Pin, stepPin}, {after.Edit, stepEdit}, {after.Preview, stepCard}} {
		if s.on {
			steps = append(steps, s.do)
		}
	}
	if !after.Preview && after.Copy && !after.Edit && !after.Pin {
		steps = append(steps, stepCopiedToast)
	}
	return steps
}

func (a *App) automate(item store.Item, img *image.RGBA, dpi float64) {
	steps := plan(a.settings.AfterShot)
	for _, s := range steps {
		switch s {
		case stepCopy:
			confirm := false
			for _, t := range steps {
				confirm = confirm || t == stepCopiedToast
			}
			a.acts.CopyImage(img, func(err error) {
				if err != nil {
					a.toast("Copy failed: " + err.Error())
				} else if confirm {
					a.toast("Copied to clipboard")
				}
			})
		case stepSave:
			a.acts.Save(item, false, a.acts.ReportSave)
		case stepPin:
			if a.surface("Pinning", a.Surfaces.Pin != nil) {
				a.Surfaces.Pin(img, dpi)
			}
		case stepEdit:
			if a.surface("The editor", a.Surfaces.OpenEditor != nil) {
				a.Surfaces.OpenEditor(item)
			}
		case stepCard:
			if a.surface("The preview card", a.Surfaces.ShowCard != nil) {
				a.Surfaces.ShowCard(item)
			}
		}
	}
}

// openLast opens the newest capture of this run in the editor, else the Library's newest
// screenshot.
func (a *App) openLast() {
	lib, last := a.library, a.last
	a.background(func() func() {
		item, err := store.Load(last)
		if last == "" || err != nil {
			err = errNoCaptures
			for _, it := range lib.Scan() {
				if it.Kind() == store.Screenshot {
					item, err = it, nil
					break
				}
			}
		}
		return func() {
			if err != nil {
				a.toast("No captures yet")
				return
			}
			a.edit(item)
		}
	})
}

var errNoCaptures = fmt.Errorf("no captures")

func (a *App) edit(item store.Item) {
	if item.Kind() == store.Recording {
		if a.surface("Studio", a.Surfaces.OpenStudio != nil) {
			a.Surfaces.OpenStudio(item.Dir)
		}
		return
	}
	if a.surface("The editor", a.Surfaces.OpenEditor != nil) {
		a.Surfaces.OpenEditor(item)
	}
}

// openImages opens images in the editor: a capture of the Library as it is, any other image
// as a copy imported into the Library.
func (a *App) openImages(paths []string) {
	lib := a.library
	for _, path := range paths {
		a.background(func() func() {
			var item store.Item
			var err error
			if dir, ok := lib.ItemDir(path); ok {
				item, err = store.Load(dir)
			} else {
				item, err = lib.ImportFile(path)
			}
			return func() {
				if err != nil {
					a.toast(err.Error())
					return
				}
				if a.Surfaces.Imported != nil {
					a.Surfaces.Imported(item)
				}
				a.edit(item)
			}
		})
	}
}

func (a *App) openTrim(path string) {
	if !IsVideo(path) {
		a.toast(filepath.Base(path) + " is not a video Glint can trim (MP4, MOV, M4V, WMV, AVI)")
		return
	}
	if a.surface("Trim & Compress", a.Surfaces.OpenTrim != nil) {
		a.Surfaces.OpenTrim(path)
	}
}

func (a *App) pickTrim() {
	a.background(func() func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Title:   "Trim & Compress Video",
			Filters: []mygo.FileFilter{{Name: "Videos", Extensions: VideoExtensions}},
		})
		return func() {
			if err == nil && len(paths) > 0 {
				a.openTrim(paths[0])
			}
		}
	})
}
