// Package editor is Glint's annotation editor: one window with the tools, a style bar, the
// canvas, a sidebar for the look and a bar of actions. It edits a capture non-destructively:
// the base image never changes, and the result is an EditDoc that internal/render flattens.
package editor

import (
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// ErrCanceled is what Host.Export returns when the user closed its save dialog.
var ErrCanceled = errors.New("canceled")

// Host is what the editor needs from the app. The editor calls each method on a goroutine of its
// own, never on the main thread, and shows the error a method returns.
type Host interface {
	// SaveEdit keeps the edit in the capture: edit.json, the flattened image.png and the
	// thumbnail. Copy keeps nothing; Save, Save As and Done call it first, and when it fails
	// the editor stays open with the changes.
	SaveEdit(img *image.RGBA, doc render.EditDoc) error
	// Export writes a copy of the flattened image for the user, named after title: into the
	// export folder, or where a save dialog says when ask is true (Save As). It returns the
	// path written, or ErrCanceled.
	Export(title string, img *image.RGBA, ask bool) (string, error)
	// Copy puts the flattened image on the clipboard.
	Copy(img *image.RGBA) error
	// Pin shows the flattened image in a window above the others.
	Pin(img *image.RGBA, dpi float32) error
	// Closed tells the app the editor's window closed, by Done or by the user.
	Closed()
}

// Options open an editor.
type Options struct {
	// Title names the capture in the title bar and the files Export writes.
	Title string
	// Base is the capture's original pixels, base.png.
	Base image.Image
	// Doc is the capture's edit.json, or a new document.
	Doc  render.EditDoc
	Host Host
	// PresetsPath is the file of saved looks; the default is presets.json in the data folder.
	PresetsPath string
}

type palette = theme.Palette

// Editor is one open editor window.
type Editor struct {
	m           *model
	title       string
	host        Host
	win         *mygo.Window
	vp          viewport
	cv          canvas
	presetsPath string
	autoColors  []render.Color
	hue         float64
	toasts      []string
	saving      bool
	closed      bool
	gallery     *gallery
	canvasFocus ui.Handle
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) && r.Stride == 4*r.Rect.Dx() {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(out, out.Rect, img, b.Min, draw.Src)
	return out
}

func newEditor(o Options, post func(func())) *Editor {
	base := toRGBA(o.Base)
	path := o.PresetsPath
	if path == "" {
		path = settings.PresetsPath()
	}
	e := &Editor{m: newModel(base, o.Doc), title: o.Title, host: o.Host, presetsPath: path, hue: 220}
	if presets, err := render.LoadPresetStore(path); err == nil {
		e.m.presets = presets
	} else {
		e.toast(fmt.Sprintf("Couldn't read presets: %v", err))
	}
	e.autoColors = render.AutoGradient(base)
	e.cv.renderer = newRenderer(base, post, e.cv.deliver)
	return e
}

// Open shows an editor in a new window. Call it on the main thread.
func Open(o Options) *Editor {
	var e *Editor
	win := mygo.NewWindow(mygo.WindowOptions{
		Title: "Glint — " + o.Title, Width: 1360, Height: 880, MinWidth: 1040, MinHeight: 660,
		TitleBarStyle: mygo.TitleBarHidden, Vibrancy: mygo.VibrancyMica, Hidden: true,
		Content: ui.View(func(c *ui.Context) { e.view(c) }),
	})
	e = newEditor(o, func(f func()) { win.Update(f) })
	e.win = win
	e.gallery = sharedGallery(func() { win.Invalidate() })
	win.OnClose(func(ev *mygo.CloseEvent) {
		e.m.commitText()
		if e.m.dirty() {
			ev.PreventDefault()
			go e.askToSave()
		}
	})
	win.OnClosed(func() {
		e.cv.renderer.stop()
		if e.host != nil {
			go e.host.Closed()
		}
	})
	win.Show()
	win.Focus()
	return e
}

func (e *Editor) toast(msg string) { e.toasts = append(e.toasts, msg) }

// async runs slow work off the main thread; without a window, as in tests, it runs at once.
func (e *Editor) async(f func()) {
	if e.win != nil {
		go f()
	} else {
		f()
	}
}

func (e *Editor) close() {
	e.closed = true
	if e.win != nil {
		e.win.Destroy()
	}
}

// update runs f on the main thread with a new frame after, as results come back.
func (e *Editor) update(f func()) {
	if e.win != nil {
		e.win.Update(f)
	} else {
		f()
	}
}

// flatten is the document as internal/render exports it.
func (e *Editor) flatten(doc render.EditDoc) *image.RGBA {
	return render.RenderDoc(e.m.base, doc)
}

func (e *Editor) snapshot() render.EditDoc {
	e.m.commitText()
	return e.m.doc.Clone()
}

func (e *Editor) copyImage() {
	doc := e.snapshot()
	e.async(func() {
		img := e.flatten(doc)
		err := e.host.Copy(img)
		e.update(func() {
			if err != nil {
				e.toast("Copy failed: " + err.Error())
				return
			}
			e.toast(fmt.Sprintf("Copied %d × %d image", img.Rect.Dx(), img.Rect.Dy()))
		})
	})
}

func (e *Editor) pin() {
	doc := e.snapshot()
	e.async(func() {
		if err := e.host.Pin(e.flatten(doc), doc.DPI); err != nil {
			e.update(func() { e.toast("Couldn't pin: " + err.Error()) })
		}
	})
}

type afterSave int

const (
	thenClose afterSave = iota
	thenExport
	thenExportAsk
)

// commit saves the edit into the capture, then closes the window or exports a copy.
func (e *Editor) commit(then afterSave) {
	if e.saving {
		return
	}
	if e.m.crop != nil {
		e.m.applyCrop()
	}
	doc := e.snapshot()
	e.saving = true
	e.async(func() {
		img := e.flatten(doc)
		err := e.host.SaveEdit(img, doc)
		e.update(func() {
			e.saving = false
			if err != nil {
				e.toast("Couldn't save the edit: " + err.Error())
				return
			}
			e.m.saved = doc
			switch then {
			case thenClose:
				e.close()
			default:
				e.async(func() { e.export(img, then == thenExportAsk) })
			}
		})
	})
}

func (e *Editor) export(img *image.RGBA, ask bool) {
	path, err := e.host.Export(e.title, img, ask)
	e.update(func() {
		switch {
		case errors.Is(err, ErrCanceled):
		case err != nil:
			e.toast("Not saved: " + err.Error())
		default:
			e.toast("Saved to " + path)
		}
	})
}

func (e *Editor) requestClose() {
	e.m.commitText()
	if !e.m.dirty() {
		e.close()
		return
	}
	go e.askToSave()
}

func (e *Editor) askToSave() {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: e.win, Type: mygo.MessageWarning,
		Message: "Save your changes to this screenshot?", Detail: "Your edits are lost if you don't save them.",
		Buttons: []string{"Save", "Discard", "Cancel"},
	})
	if err != nil {
		return
	}
	e.update(func() {
		switch res.Button {
		case 0:
			e.commit(thenClose)
		case 1:
			e.close()
		}
	})
}

func (e *Editor) savePresets() {
	if err := render.SavePresetStore(e.presetsPath, e.m.presets); err != nil {
		e.toast("Couldn't save presets: " + err.Error())
	}
}

func (e *Editor) exportPresets() {
	if len(e.m.presets) == 0 {
		e.toast("Save a preset first")
		return
	}
	data := render.ExportPresets(e.m.presets)
	e.async(func() {
		dir, _ := os.UserHomeDir()
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{
			Parent: e.win, Title: "Export presets", DefaultPath: dir + string(os.PathSeparator) + "Glint presets." + render.PresetExtension,
			Filters: []mygo.FileFilter{{Name: "Glint presets", Extensions: []string{render.PresetExtension}}},
		})
		if err != nil || path == "" {
			return
		}
		if ext := "." + render.PresetExtension; len(path) < len(ext) || path[len(path)-len(ext):] != ext {
			path += ext
		}
		err = os.WriteFile(path, data, 0o644)
		e.update(func() {
			if err != nil {
				e.toast("Export failed: " + err.Error())
				return
			}
			e.toast("Exported to " + path)
		})
	})
}

func (e *Editor) importPresets() {
	e.async(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: e.win, Title: "Import presets",
			Filters: []mygo.FileFilter{{Name: "Glint presets", Extensions: []string{render.PresetExtension, "json"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		data, err := os.ReadFile(paths[0])
		var list []render.Preset
		if err == nil {
			list, err = render.ImportPresets(data)
		}
		e.update(func() {
			if err != nil {
				e.toast("Import failed: " + err.Error())
				return
			}
			e.m.importPresets(list)
			e.savePresets()
			s := "s"
			if len(list) == 1 {
				s = ""
			}
			e.toast(fmt.Sprintf("Imported %d preset%s", len(list), s))
		})
	})
}

func (e *Editor) saveDefaultStyle() {
	raw, err := json.Marshal(e.m.doc.Style)
	if err == nil {
		s := settings.Load()
		s.Style = raw
		err = s.Save()
	}
	if err != nil {
		e.toast("Couldn't save the default style: " + err.Error())
		return
	}
	e.toast("Saved as your default style")
}
