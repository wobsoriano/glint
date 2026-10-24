// Package actions is what a capture's buttons and menus do: copy it, save or export it,
// compress it, copy its text, delete it. The preview cards, the editor, the Library and the
// capture flow all call these, so a capture behaves the same wherever the user acts on it.
//
// Every method runs on the main thread, does slow work on a goroutine, and reports back on the
// main thread, through a done callback or a toast.
package actions

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// SaveChoice is the user's answer to a failed save.
type SaveChoice int

const (
	SaveCancel SaveChoice = iota
	SaveRetry
	SaveChooseFolder
)

type Actions struct {
	settings func() settings.Settings
	toast    func(string)

	// RecordingFile resolves the video a recording hands out, with notices to show the user,
	// such as Studio's export of its edits. Nil hands out the raw screen.mp4.
	RecordingFile func(item store.Item) (path string, notices []string, err error)

	// The seams below talk to the desktop; tests replace them.
	post          func(func())
	writeClip     func(transfer.Data) error
	writeText     func(string)
	writeFiles    func(...string) error
	recognize     func(*image.RGBA) ([]ocr.Line, error)
	trash         func([]string) error
	askSavePath   func(dir, name string, format settings.ExportFormat) string
	askSaveFailed func(detail string) SaveChoice
	askFolder     func(button string) string
}

// New makes the actions read the current settings through settings and report through toast.
func New(settings func() settings.Settings, toast func(string)) *Actions {
	return &Actions{
		settings:      settings,
		toast:         toast,
		post:          mygo.RunOnMain,
		writeClip:     func(d transfer.Data) error { return mygo.Clipboard.Write(d) },
		writeText:     mygo.Clipboard.WriteText,
		writeFiles:    mygo.Clipboard.WriteFiles,
		recognize:     ocr.Recognize,
		trash:         store.Trash,
		askSavePath:   askSavePath,
		askSaveFailed: askSaveFailed,
		askFolder:     askFolder,
	}
}

// background runs work on a goroutine and then on the main thread.
func (a *Actions) background(work func() func()) {
	go func() {
		then := work()
		a.post(then)
	}()
}

// CopyImage puts img on the clipboard as PNG, which other apps also read as a bitmap.
func (a *Actions) CopyImage(img image.Image, done func(error)) {
	a.background(func() func() {
		var b bytes.Buffer
		err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(&b, img)
		return func() {
			if err == nil {
				err = a.writeClip(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, b.Bytes()))))
			}
			done(err)
		}
	})
}

// report hands err to done, which may be nil.
func report(done func(error), err error) {
	if done != nil {
		done(err)
	}
}

var errNotAScreenshot = errors.New("This works on screenshots; a recording has no single image to use")

// withPixels runs f with the screenshot's pixels. When they cannot be read, done gets the
// error, or with no done a toast says why.
func (a *Actions) withPixels(item store.Item, done func(error), f func(image.Image)) {
	fail := func(err error) {
		if done == nil {
			a.toast(err.Error())
			return
		}
		done(err)
	}
	if item.Kind() != store.Screenshot {
		fail(errNotAScreenshot)
		return
	}
	a.background(func() func() {
		img, err := LoadPixels(item)
		return func() {
			if err != nil {
				fail(err)
				return
			}
			f(img)
		}
	})
}

func (a *Actions) recordingFile(item store.Item) (string, []string, error) {
	if a.RecordingFile != nil {
		return a.RecordingFile(item)
	}
	path := item.Deliverable()
	if _, err := os.Stat(path); err != nil {
		return "", nil, err
	}
	return path, nil, nil
}

func (a *Actions) withRecordingFile(item store.Item, f func(string, error)) {
	a.background(func() func() {
		file, notices, err := a.recordingFile(item)
		return func() {
			f(file, err)
			if err == nil && len(notices) > 0 {
				a.toast(strings.Join(notices, " "))
			}
		}
	})
}

// Copy puts a screenshot's image, or a recording's video file, on the clipboard, and says how
// it went.
func (a *Actions) Copy(item store.Item) {
	a.CopyItem(item, func(err error) {
		switch {
		case err != nil:
			a.toast("Copy failed: " + err.Error())
		case item.Kind() == store.Recording:
			a.toast("Copied the video file")
		default:
			a.toast("Copied to clipboard")
		}
	})
}

// CopyItem is Copy that reports to done alone, for a surface that shows the outcome itself,
// such as a preview card.
func (a *Actions) CopyItem(item store.Item, done func(error)) {
	if item.Kind() == store.Recording {
		a.withRecordingFile(item, func(file string, err error) {
			if err == nil {
				file, err = HardLinkOutsideCache(file, item.ID(), item.Meta.Title)
			}
			if err == nil {
				err = a.writeFiles(file)
			}
			done(err)
		})
		return
	}
	a.withPixels(item, done, func(img image.Image) { a.CopyImage(img, done) })
}

// CopyFiles puts the captures' files on the clipboard, for pasting into a folder or a chat.
func (a *Actions) CopyFiles(items []store.Item) {
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.Deliverable()
	}
	if err := a.writeFiles(paths...); err != nil {
		a.toast("Copy failed: " + err.Error())
		return
	}
	a.toast(fmt.Sprintf("Copied %d file(s)", len(paths)))
}

// ErrNoText is what copying the text of an image with none reports.
var ErrNoText = errors.New("no text found")

// CopyText recognizes the text in a screenshot and copies it, as CopyTextOf. done, which may
// be nil, gets the outcome too.
func (a *Actions) CopyText(item store.Item, done func(error)) {
	a.withPixels(item, done, func(img image.Image) { a.copyText(toRGBA(img), done) })
}

// CopyTextOf recognizes the text in img, copies it in reading order and says what it copied.
func (a *Actions) CopyTextOf(img *image.RGBA) { a.copyText(img, nil) }

func (a *Actions) copyText(img *image.RGBA, done func(error)) {
	a.background(func() func() {
		lines, err := a.recognize(img)
		return func() {
			switch text := ocr.Text(lines); {
			case err != nil:
				a.toast("OCR failed: " + err.Error())
			case strings.TrimSpace(text) == "":
				a.toast("No text found")
				err = ErrNoText
			default:
				a.writeText(text)
				a.toast("Copied text: " + strings.ReplaceAll(firstRunes(text, 60), "\n", " "))
			}
			report(done, err)
		}
	})
}

func firstRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}

// Compress copies a JPEG of the screenshot, at most quality 80, as a file, and says how big it
// is. done, which may be nil, gets the outcome too.
func (a *Actions) Compress(item store.Item, done func(error)) {
	quality := min(a.settings().JPEGQuality, 80)
	a.withPixels(item, done, func(img image.Image) {
		a.background(func() func() {
			dir := filepath.Join(os.TempDir(), "Glint", "Compressed")
			path, err := Export(img, dir, item.Meta.Title, settings.FormatJPEG, quality)
			var size int64
			if err == nil {
				var st os.FileInfo
				if st, err = os.Stat(path); err == nil {
					size = st.Size()
				}
			}
			return func() {
				if err == nil {
					err = a.writeFiles(path)
				}
				if err != nil {
					a.toast("Compress failed: " + err.Error())
				} else {
					a.toast(fmt.Sprintf("Copied a %s JPEG", HumanBytes(uint64(size))))
				}
				report(done, err)
			}
		})
	})
}

// Save writes the capture to the export folder, or asks where when ask is set or the settings
// say to always ask. done gets the written path, or ErrCancelled, or the error the user gave up on.
func (a *Actions) Save(item store.Item, ask bool, done func(string, error)) {
	if item.Kind() == store.Recording {
		a.saveVideo(item, ask, done)
		return
	}
	a.withPixels(item, func(err error) { done("", err) }, func(img image.Image) { a.SaveImage(item.Meta.Title, img, ask, done) })
}

// SaveImage saves img, named after title, as Save does.
func (a *Actions) SaveImage(title string, img image.Image, ask bool, done func(string, error)) {
	s := a.settings()
	t := target{dir: s.SaveDir, ask: ask || !s.SaveWithoutDialog}
	a.saveTo(title, img, t, done)
}

type target struct {
	dir string
	ask bool
}

func (a *Actions) saveTo(stem string, img image.Image, t target, done func(string, error)) {
	s := a.settings()
	a.background(func() func() {
		var path string
		var err error
		if t.ask {
			os.MkdirAll(t.dir, 0o755)
			path = a.askSavePath(t.dir, Sanitize(stem)+"."+s.Format.Extension(), s.Format)
			if path == "" {
				return func() { done("", ErrCancelled) }
			}
			var data []byte
			if data, err = Encode(img, formatOf(path, s.Format), s.JPEGQuality); err == nil {
				if err = os.WriteFile(path, data, 0o644); err != nil {
					err = fmt.Errorf("writing %s: %w", path, err)
				}
			}
			t = target{dir: filepath.Dir(path)}
		} else {
			path, err = Export(img, t.dir, stem, s.Format, s.JPEGQuality)
		}
		return func() { a.finishSave(stem, img, path, err, t, done) }
	})
}

// finishSave offers Retry or another folder when a save failed, as the Rust build did.
func (a *Actions) finishSave(stem string, img image.Image, path string, err error, retry target, done func(string, error)) {
	if err == nil {
		done(path, nil)
		return
	}
	a.background(func() func() {
		switch a.askSaveFailed(err.Error()) {
		case SaveRetry:
			return func() { a.saveTo(stem, img, retry, done) }
		case SaveChooseFolder:
			if dir := a.askFolder("Save Here"); dir != "" {
				return func() { a.saveTo(stem, img, target{dir: dir}, done) }
			}
		}
		return func() { done("", err) }
	})
}

func (a *Actions) saveVideo(item store.Item, ask bool, done func(string, error)) {
	s := a.settings()
	stem := Sanitize(item.Meta.Title)
	ask = ask || !s.SaveWithoutDialog
	a.withRecordingFile(item, func(src string, err error) {
		if err != nil {
			done("", err)
			return
		}
		ext := strings.TrimPrefix(filepath.Ext(src), ".")
		if ext == "" {
			ext = "mp4"
		}
		a.background(func() func() {
			dest := UniquePath(s.SaveDir, stem, ext)
			if ask {
				os.MkdirAll(s.SaveDir, 0o755)
				if dest = a.askSavePath(s.SaveDir, stem+"."+ext, ""); dest == "" {
					return func() { done("", ErrCancelled) }
				}
			}
			err := copyFile(src, dest)
			return func() {
				if err != nil {
					done("", err)
					return
				}
				done(dest, nil)
			}
		})
	})
}

func copyFile(src, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(dest), err)
	}
	data, err := os.ReadFile(src)
	if err == nil {
		err = os.WriteFile(dest, data, 0o644)
	}
	if err != nil {
		return fmt.Errorf("writing %s: %w", dest, err)
	}
	return nil
}

// ReportSave tells the user how a save ended. A cancelled save says nothing.
func (a *Actions) ReportSave(path string, err error) {
	switch {
	case err == nil:
		a.toast("Saved " + filepath.Base(path))
	case errors.Is(err, ErrCancelled):
	default:
		a.toast("Not saved: " + err.Error())
	}
}

// ExportToFolder asks for a folder and writes every screenshot among items into it.
func (a *Actions) ExportToFolder(items []store.Item) {
	a.background(func() func() {
		dir := a.askFolder("Export Here")
		if dir == "" {
			return func() {}
		}
		return func() { a.ExportTo(items, dir) }
	})
}

// ExportTo writes every screenshot among items into dir in the export format of the
// settings, and says how it went.
func (a *Actions) ExportTo(items []store.Item, dir string) {
	s := a.settings()
	a.background(func() func() {
		n := 0
		var err error
		for _, it := range items {
			if it.Kind() != store.Screenshot {
				continue
			}
			var img image.Image
			if img, err = LoadPixels(it); err == nil {
				_, err = Export(img, dir, it.Meta.Title, s.Format, s.JPEGQuality)
			}
			if err != nil {
				break
			}
			n++
		}
		return func() {
			if err != nil {
				a.toast("Export failed: " + err.Error())
				return
			}
			a.toast(fmt.Sprintf("Exported %d capture(s)", n))
		}
	})
}

// DragFile is the file to hand other apps when the capture is dragged out.
func (a *Actions) DragFile(item store.Item) (string, error) {
	return HardLinkOutsideCache(item.Deliverable(), item.ID(), item.Meta.Title)
}

// Delete moves capture directories to the trash; done says whether all of them went.
func (a *Actions) Delete(dirs []string, done func(ok bool)) {
	a.background(func() func() {
		err := a.trash(dirs)
		return func() {
			if err != nil {
				a.toast("Delete failed: " + err.Error())
				done(false)
				return
			}
			if len(dirs) == 1 {
				a.toast("Moved to the " + Trash)
			} else {
				a.toast(fmt.Sprintf("Moved %d captures to the %s", len(dirs), Trash))
			}
			done(true)
		}
	})
}

func toRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Rect.Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	out := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out
}

func askSavePath(dir, name string, format settings.ExportFormat) string {
	opts := mygo.SaveDialogOptions{Title: "Save", DefaultPath: filepath.Join(dir, name)}
	png, jpg := mygo.FileFilter{Name: "PNG image", Extensions: []string{"png"}}, mygo.FileFilter{Name: "JPEG image", Extensions: []string{"jpg", "jpeg"}}
	switch format {
	case settings.FormatPNG:
		opts.Filters = []mygo.FileFilter{png, jpg}
	case settings.FormatJPEG:
		opts.Filters = []mygo.FileFilter{jpg, png}
	}
	path, err := mygo.Dialog.Save(opts)
	if err != nil {
		return ""
	}
	return path
}

func askSaveFailed(detail string) SaveChoice {
	res, err := mygo.Dialog.Message(mygo.MessageOptions{
		Type:    mygo.MessageWarning,
		Title:   "Glint",
		Message: "The screenshot could not be saved",
		Detail:  detail,
		Buttons: []string{"Retry", "Choose Another Folder…", "Cancel"},
	})
	if err != nil {
		return SaveCancel
	}
	return [...]SaveChoice{SaveRetry, SaveChooseFolder, SaveCancel}[min(max(res.Button, 0), 2)]
}

func askFolder(button string) string {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Directory: true, ButtonLabel: button, CreateDirectories: true})
	if err != nil || len(paths) == 0 {
		return ""
	}
	return paths[0]
}
