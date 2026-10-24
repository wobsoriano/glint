// Package store keeps the Library on disk: one directory per capture under the Library root,
// named by the capture's local creation time, holding meta.json and the capture's files.
// See docs/architecture.md, "Library on disk".
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/settings/serde"
)

// ThumbMax is the longest side of thumb.png.
const ThumbMax = 512

const recordingExt = ".glintrec"

// stampLayout is the local creation time that starts every capture id.
const stampLayout = "2006-01-02_15-04-05"

// Item is one capture as listed: its directory, its meta.json and its size on disk.
type Item struct {
	Dir   string
	Meta  Meta
	Bytes uint64
}

// ID is the directory name, such as 2026-10-14_14-03-22_ab12 or 2026-10-14_14-03-22_ab12.glintrec.
func (i Item) ID() string { return filepath.Base(i.Dir) }

func (i Item) Kind() Kind { return i.Meta.Kind }

// Deliverable is the file a copy, save or drag hands out.
func (i Item) Deliverable() string {
	if i.Meta.Kind == Recording {
		return filepath.Join(i.Dir, "screen.mp4")
	}
	return filepath.Join(i.Dir, "image.png")
}

func (i Item) Thumb() string { return filepath.Join(i.Dir, "thumb.png") }

// IsEdited reports whether the capture has an editor sidecar: edit.json for a screenshot,
// project.json for a recording.
func (i Item) IsEdited() bool {
	sidecar := "edit.json"
	if i.Meta.Kind == Recording {
		sidecar = "project.json"
	}
	_, err := os.Stat(filepath.Join(i.Dir, sidecar))
	return err == nil
}

// DefaultTitle is "Screenshot 2026-10-14 at 14.03.22" or "Recording ...", in local time.
func DefaultTitle(kind Kind, at time.Time) string {
	noun := "Screenshot"
	if kind == Recording {
		noun = "Recording"
	}
	return noun + " " + at.In(time.Local).Format("2006-01-02 at 15.04.05")
}

// Library is a Library root directory.
type Library struct{ root string }

func New(root string) Library { return Library{root: root} }

func DefaultRoot() string { return filepath.Join(settings.DataDir(), "Library") }

func (l Library) Root() string { return l.root }

// Edit is an editor result: the flattened image and its EditDoc as edit.json bytes, which
// internal/render reads and writes; the store keeps them as given.
type Edit struct {
	Image image.Image
	Doc   []byte
}

// NewScreenshot describes a screenshot to import. Empty strings mean absent: no source,
// not imported from a file, and the default title.
type NewScreenshot struct {
	Base         image.Image
	Edit         *Edit
	Created      time.Time
	Source       string
	ImportedFrom string
	Title        string
}

// ImportScreenshot writes the capture into a hidden staging directory and renames it into
// place, so the Library never lists a half-written capture.
func (l Library) ImportScreenshot(s NewScreenshot) (Item, error) {
	b := s.Base.Bounds()
	title := s.Title
	if title == "" {
		title = DefaultTitle(Screenshot, s.Created)
	}
	meta := Meta{
		Version: 1, Kind: Screenshot, Title: title, Created: s.Created, Modified: s.Created,
		Width: uint32(b.Dx()), Height: uint32(b.Dy()), Source: optional(s.Source), ImportedFrom: optional(s.ImportedFrom),
	}
	if err := os.MkdirAll(l.root, 0o755); err != nil {
		return Item{}, err
	}
	id, staging, err := l.reserve(s.Created)
	if err != nil {
		return Item{}, err
	}
	if err := writeScreenshot(staging, s, meta); err != nil {
		os.RemoveAll(staging)
		return Item{}, err
	}
	dir := filepath.Join(l.root, id)
	if err := os.Rename(staging, dir); err != nil {
		return Item{}, fmt.Errorf("finalizing %s: %w", dir, err)
	}
	return Load(dir)
}

func writeScreenshot(dir string, s NewScreenshot, meta Meta) error {
	if err := writePNG(s.Base, filepath.Join(dir, "base.png")); err != nil {
		return err
	}
	deliverable := s.Base
	if s.Edit != nil {
		deliverable = s.Edit.Image
		if err := writePNG(deliverable, filepath.Join(dir, "image.png")); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "edit.json"), s.Edit.Doc, 0o644); err != nil {
			return err
		}
	} else if err := copyFile(filepath.Join(dir, "base.png"), filepath.Join(dir, "image.png")); err != nil {
		return err
	}
	if err := writePNG(Thumbnail(deliverable), filepath.Join(dir, "thumb.png")); err != nil {
		return err
	}
	return writeMeta(dir, meta)
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// PendingRecording is a recording being written: Dir is a hidden staging directory the
// recorder fills until FinishRecording moves it into the Library.
type PendingRecording struct {
	ID      string
	Dir     string
	Created time.Time
}

type RecordingInfo struct {
	Width, Height uint32
	DurationMs    uint64
	Source        string
}

func (l Library) BeginRecording(created time.Time) (PendingRecording, error) {
	if err := os.MkdirAll(l.root, 0o755); err != nil {
		return PendingRecording{}, err
	}
	id, dir, err := l.reserve(created)
	if err != nil {
		return PendingRecording{}, err
	}
	return PendingRecording{ID: id, Dir: dir, Created: created}, nil
}

// FinishRecording writes meta.json, and thumb.png from firstFrame when it is not nil, then
// renames the package to <id>.glintrec.
func (l Library) FinishRecording(p PendingRecording, info RecordingInfo, firstFrame image.Image) (Item, error) {
	meta := Meta{
		Version: 1, Kind: Recording, Title: DefaultTitle(Recording, p.Created), Created: p.Created, Modified: p.Created,
		Width: info.Width, Height: info.Height, DurationMs: &info.DurationMs, Source: optional(info.Source),
	}
	if firstFrame != nil {
		if err := writePNG(Thumbnail(firstFrame), filepath.Join(p.Dir, "thumb.png")); err != nil {
			return Item{}, err
		}
	}
	if err := writeMeta(p.Dir, meta); err != nil {
		return Item{}, err
	}
	dir := filepath.Join(l.root, p.ID+recordingExt)
	if err := os.Rename(p.Dir, dir); err != nil {
		return Item{}, fmt.Errorf("finalizing %s: %w", dir, err)
	}
	return Load(dir)
}

// ImportFile copies a PNG, JPEG or WebP into the Library, titled by its file name. The
// original is only read.
func (l Library) ImportFile(path string) (Item, error) {
	img, err := decodeImage(path)
	if err != nil {
		return Item{}, fmt.Errorf("%s is not a readable image: %w", path, err)
	}
	if img.Bounds().Empty() {
		return Item{}, fmt.Errorf("%s has no pixels", path)
	}
	name := filepath.Base(path)
	return l.ImportScreenshot(NewScreenshot{
		Base:         img,
		Created:      time.Now(),
		ImportedFrom: path,
		Title:        strings.TrimSuffix(name, filepath.Ext(name)),
	})
}

func decodeImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(bufio.NewReader(f))
	return img, err
}

// ItemDir finds the capture directory a path inside the Library belongs to.
func (l Library) ItemDir(path string) (string, bool) {
	rel, err := filepath.Rel(l.root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	first, _, _ := strings.Cut(rel, string(filepath.Separator))
	return filepath.Join(l.root, first), true
}

// Scan lists every capture, newest first. Hidden entries (staging directories) and
// directories without a readable meta.json are skipped.
func (l Library) Scan() []Item {
	entries, err := os.ReadDir(l.root)
	if err != nil {
		return nil
	}
	var items []Item
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if item, err := Load(filepath.Join(l.root, e.Name())); err == nil {
			items = append(items, item)
		}
	}
	slices.SortFunc(items, func(a, b Item) int {
		if c := b.Meta.Created.Compare(a.Meta.Created); c != 0 {
			return c
		}
		return strings.Compare(b.ID(), a.ID())
	})
	return items
}

func (l Library) reserve(created time.Time) (id, staging string, err error) {
	stamp := created.In(time.Local).Format(stampLayout)
	for range 16 {
		id = fmt.Sprintf("%s_%04x", stamp, rand.N(1<<16))
		if exists(filepath.Join(l.root, id)) || exists(filepath.Join(l.root, id+recordingExt)) {
			continue
		}
		staging = filepath.Join(l.root, "."+id+stagingExt)
		switch err := os.Mkdir(staging, 0o755); {
		case err == nil:
			return id, staging, nil
		case errors.Is(err, fs.ErrExist):
			continue
		default:
			return "", "", err
		}
	}
	return "", "", errors.New("could not allocate a capture id")
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// Load reads one capture directory. A meta.json whose kind does not match the directory
// form is an error.
func Load(dir string) (Item, error) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return Item{}, err
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		return Item{}, fmt.Errorf("parsing %s: %w", dir, err)
	}
	if isRec := filepath.Ext(dir) == recordingExt; isRec != (meta.Kind == Recording) {
		return Item{}, fmt.Errorf("%s has kind %s in the wrong directory type", dir, meta.Kind)
	}
	return Item{Dir: dir, Meta: meta, Bytes: dirSize(dir)}, nil
}

// LoadEdit returns a screenshot's original pixels and its edit.json, nil when never edited.
func LoadEdit(item Item) (*image.NRGBA, []byte, error) {
	base, err := readPNG(filepath.Join(item.Dir, "base.png"))
	if err != nil {
		return nil, nil, err
	}
	doc, err := os.ReadFile(filepath.Join(item.Dir, "edit.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return base, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return base, doc, nil
}

// SaveEdit replaces the deliverable, edit.json and thumbnail. All three are staged first so
// a failed write leaves the previous edit intact.
func SaveEdit(item Item, img image.Image, doc []byte) (Item, error) {
	dir := item.Dir
	staged := []struct{ name, tmp string }{
		{"image.png", filepath.Join(dir, "image.png.tmp")},
		{"edit.json", filepath.Join(dir, "edit.json.tmp")},
		{"thumb.png", filepath.Join(dir, "thumb.png.tmp")},
	}
	err := writePNG(img, staged[0].tmp)
	if err == nil {
		err = os.WriteFile(staged[1].tmp, doc, 0o644)
	}
	if err == nil {
		err = writePNG(Thumbnail(img), staged[2].tmp)
	}
	if err != nil {
		for _, s := range staged {
			os.Remove(s.tmp)
		}
		return Item{}, err
	}
	for _, s := range staged {
		if err := os.Rename(s.tmp, filepath.Join(dir, s.name)); err != nil {
			return Item{}, fmt.Errorf("saving %s: %w", filepath.Join(dir, s.name), err)
		}
	}
	meta := item.Meta
	meta.Modified = time.Now()
	if err := writeMeta(dir, meta); err != nil {
		return Item{}, err
	}
	return Load(dir)
}

// Rename changes the title in meta.json only; the directory keeps its name.
func Rename(item Item, title string) (Item, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return Item{}, ErrEmptyTitle
	}
	meta := item.Meta
	meta.Title = title
	meta.Modified = time.Now()
	if err := writeMeta(item.Dir, meta); err != nil {
		return Item{}, err
	}
	return Load(item.Dir)
}

func writeMeta(dir string, meta Meta) error {
	data, err := serde.MarshalPretty(meta)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "meta.json.tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "meta.json"))
}

// boxFilter averages the source pixels under each thumbnail pixel, as the Rust build's
// image::imageops::thumbnail does.
var boxFilter = &draw.Kernel{Support: 0.5, At: func(float64) float64 { return 1 }}

// Thumbnail scales img so its longest side is at most ThumbMax, keeping the aspect ratio.
func Thumbnail(img image.Image) *image.NRGBA {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	k := min(float32(ThumbMax)/float32(max(w, h)), 1)
	tw := max(int(math.Round(float64(float32(w)*k))), 1)
	th := max(int(math.Round(float64(float32(h)*k))), 1)
	dst := image.NewNRGBA(image.Rect(0, 0, tw, th))
	if tw == w && th == h {
		draw.Draw(dst, dst.Rect, img, b.Min, draw.Src)
	} else {
		boxFilter.Scale(dst, dst.Rect, img, b, draw.Src, nil)
	}
	return dst
}

func writePNG(img image.Image, path string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}()
	w := bufio.NewWriter(f)
	if err := (&png.Encoder{CompressionLevel: png.BestSpeed}).Encode(w, img); err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	return w.Flush()
}

func readPNG(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, err := png.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	if n, ok := img.(*image.NRGBA); ok {
		return n, nil
	}
	n := image.NewNRGBA(img.Bounds())
	draw.Draw(n, n.Rect, img, img.Bounds().Min, draw.Src)
	return n, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func dirSize(dir string) uint64 {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	var total uint64
	for _, e := range entries {
		if e.IsDir() {
			total += dirSize(filepath.Join(dir, e.Name()))
		} else if info, err := os.Stat(filepath.Join(dir, e.Name())); err == nil {
			total += uint64(info.Size())
		}
	}
	return total
}
