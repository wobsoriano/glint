// Command editordemo opens a Library capture in the editor, with a Host built on internal/store,
// and checks what the editor saved.
//
//	editordemo -data DIR -capture            grab the primary display into a new capture, open it
//	MYGO_FRAME_STATS=all editordemo -data DIR -capture -size 2560x1440 -seed 30 -frames F
//	                                         a capture of that size with 30 annotations, each frame logged
//	editordemo -analyze F -from MS -to MS    frame timings between two Unix-ms marks
//	editordemo -data DIR                     open the newest capture again
//	editordemo -data DIR -verify             check the newest capture's files against RenderDoc
//
// Every run uses DIR as GLINT_DATA_DIR and refuses the real data folder.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/editor"
)

var (
	dataDir = flag.String("data", "", "GLINT_DATA_DIR for this run (required)")
	capture = flag.Bool("capture", false, "grab the primary display into a new Library capture")
	sample  = flag.Bool("sample", false, "import an image of text with an email, a card and a phone number")
	sizeArg = flag.String("size", "", "WxH: crop the grab to this size (scaled up first when the display is smaller)")
	seed    = flag.Int("seed", 0, "add this many annotations before opening")
	stats   = flag.String("frames", "", "write each frame's timings to this file (needs MYGO_FRAME_STATS=all)")
	report  = flag.String("analyze", "", "report the frames this -frames file logged between -from and -to")
	from    = flag.Float64("from", 0, "start of the span -analyze measures, Unix ms")
	to      = flag.Float64("to", 0, "end of the span, Unix ms")
	verify  = flag.Bool("verify", false, "check the newest capture's image.png against RenderDoc of its edit.json")
	winX    = flag.Int("x", -1, "place the window's left edge here (DIPs)")
	winY    = flag.Int("y", -1, "and its top edge here")
)

func main() {
	flag.Parse()
	if *report != "" {
		if err := analyze(*report, *from, *to); err != nil {
			log.Fatal(err)
		}
		return
	}
	if *dataDir == "" {
		log.Fatal("-data is required")
	}
	abs, _ := filepath.Abs(*dataDir)
	if appdata := os.Getenv("APPDATA"); appdata != "" && strings.EqualFold(filepath.Clean(abs), filepath.Join(appdata, "Glint")) {
		log.Fatal("refusing the real data folder")
	}
	os.Setenv("GLINT_DATA_DIR", abs)
	lib := store.New(store.DefaultRoot())
	if *verify {
		if err := verifyNewest(lib); err != nil {
			fmt.Println("FAIL:", err)
			os.Exit(1)
		}
		return
	}
	if *stats != "" {
		if os.Getenv("MYGO_FRAME_STATS") != "all" {
			// MyGo reads it as its package initializes, before main.
			log.Fatal("-frames needs MYGO_FRAME_STATS=all in the environment")
		}
		f, err := os.Create(*stats)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		log.SetOutput(&frameLog{f: f})
		log.SetFlags(0)
	}
	mygo.App.WhenReady(func() {
		item, err := pick(lib)
		if err != nil {
			fmt.Fprintln(os.Stderr, "editordemo:", err)
			mygo.App.Quit()
			return
		}
		open(item)
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// sampleImage is text with an email, a card number and a phone number, for smart redaction.
func sampleImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 1600, 900))
	render.Fill(img, render.White)
	for i, line := range []string{
		"Invoice 2026-118 for jane.doe@example.com",
		"Card on file: 4242 4242 4242 4242",
		"Questions? Call +1 415 555 0134",
		"Thanks for your order.",
	} {
		render.DrawText(img, line, 120, 160+float32(i)*140, 64, render.RGB(0x1C1C1E))
	}
	return img
}

func pick(lib store.Library) (store.Item, error) {
	if *sample {
		return lib.ImportScreenshot(store.NewScreenshot{Base: sampleImage(), Created: time.Now(), Title: "OCR sample"})
	}
	if *capture {
		img, err := grab()
		if err != nil {
			return store.Item{}, err
		}
		return lib.ImportScreenshot(store.NewScreenshot{Base: img, Created: time.Now(), Source: "Display 1"})
	}
	items := lib.Scan()
	if len(items) == 0 {
		return store.Item{}, errors.New("the Library is empty; run with -capture")
	}
	return items[0], nil
}

func grab() (*image.RGBA, error) {
	ds, err := platform.Displays()
	if err != nil {
		return nil, err
	}
	img, err := platform.CaptureRect(ds[0].Bounds)
	if err != nil {
		return nil, err
	}
	if *sizeArg == "" {
		return img, nil
	}
	var w, h int
	if _, err := fmt.Sscanf(*sizeArg, "%dx%d", &w, &h); err != nil {
		return nil, fmt.Errorf("-size %q: %w", *sizeArg, err)
	}
	if img.Rect.Dx() < w || img.Rect.Dy() < h {
		k := max(float64(w)/float64(img.Rect.Dx()), float64(h)/float64(img.Rect.Dy()))
		img = render.Resize(img, int(float64(img.Rect.Dx())*k+0.999), int(float64(img.Rect.Dy())*k+0.999))
	}
	return img.SubImage(image.Rect(0, 0, w, h)).(*image.RGBA), nil
}

func open(item store.Item) {
	base, raw, err := store.LoadEdit(item)
	if err != nil {
		log.Fatal(err)
	}
	doc := render.NewEditDoc(render.DefaultStyle(), 1, item.Meta.Source)
	if raw != nil {
		if doc, err = render.ParseEditDoc(raw); err != nil {
			log.Fatal(err)
		}
	}
	if *seed > 0 {
		doc.Annotations = append(doc.Annotations, seeded(*seed, base.Rect.Dx(), base.Rect.Dy())...)
	}
	h := &host{item: item}
	e := editor.Open(editor.Options{Title: item.Meta.Title, Base: render.Premultiply(base), Doc: doc, Host: h})
	h.win = e.Window()
	if *winX >= 0 && *winY >= 0 {
		e.Window().SetPosition(*winX, *winY)
	}
	b := e.Window().ContentBounds()
	fmt.Printf("opened %s (%dx%d) in a window at %d,%d %dx%d\n", item.Dir, base.Rect.Dx(), base.Rect.Dy(), b.X, b.Y, b.Width, b.Height)
	printAnchors(doc, base.Rect.Dx(), base.Rect.Dy(), b)
}

// printAnchors prints where image points land on the screen at fit zoom, for driver scripts:
// the canvas box is the content minus the 48 + 44 px bars above, the 52 px bar below and the
// 312 px sidebar, at 100% display scale.
func printAnchors(doc render.EditDoc, w, h int, b mygo.Rectangle) {
	l := render.NewFrame(doc, uint32(w), uint32(h), 1).Layout
	box := struct{ x, y, w, h float64 }{float64(b.X), float64(b.Y + 92), float64(b.Width - 312), float64(b.Height - 92 - 52)}
	cw, ch := float64(l.Width), float64(l.Height)
	zoom := min((box.w-56)/cw, (box.h-56)/ch, 1)
	ox, oy := cw/2-box.w/zoom/2, ch/2-box.h/zoom/2
	at := func(name string, p render.Pt) {
		c := l.ToCanvas(p)
		fmt.Printf("anchor %s %.0f %.0f\n", name, box.x+(float64(c.X)-ox)*zoom, box.y+(float64(c.Y)-oy)*zoom)
	}
	fmt.Printf("anchor canvas %.0f %.0f\n", box.x+box.w/2, box.y+box.h/2)
	fmt.Printf("zoom %.4f\n", zoom)
	at("image-tl", render.P(0, 0))
	at("image-br", render.P(float32(w), float32(h)))
	for i, a := range doc.Annotations {
		r := a.Bounds()
		if t, ok := a.Shape.(render.RectShape); ok {
			at(fmt.Sprintf("ann%d-edge", i), render.P(t.Rect.X, t.Rect.Y+t.Rect.H/2))
		}
		at(fmt.Sprintf("ann%d", i), r.Center())
	}
}

func seeded(n, w, h int) []render.Annotation {
	r := rand.New(rand.NewPCG(7, 11))
	inks := render.Ink
	var out []render.Annotation
	for i := range n {
		x := float32(r.IntN(w - 420))
		y := float32(r.IntN(h - 260))
		var s render.Shape
		switch i % 7 {
		case 0:
			s = render.Arrow{From: render.P(x, y+150), To: render.P(x+260, y)}
		case 1:
			s = render.RectShape{Rect: render.Rect(x, y, 300, 160)}
		case 2:
			s = render.Ellipse{Rect: render.Rect(x, y, 240, 140)}
		case 3:
			s = render.Text{Pos: render.P(x, y), Text: "Annotation " + strconv.Itoa(i), Size: 32, Style: render.TextOutline, Font: render.DefaultTextFont()}
		case 4:
			s = render.Counter{Center: render.P(x+20, y+20), N: uint32(i), Size: 16}
		case 5:
			s = render.Line{From: render.P(x, y), To: render.P(x+360, y+80)}
		case 6:
			pts := make([]render.Pt, 40)
			for k := range pts {
				pts[k] = render.P(x+float32(k)*8, y+float32(30*((k/5)%2)))
			}
			s = render.Pen{Points: pts}
		}
		out = append(out, render.Annotation{ID: uint64(1000 + i), Shape: s, Color: render.RGB(inks[i%len(inks)]), Width: 5})
	}
	return out
}

type host struct {
	mu   sync.Mutex
	item store.Item
	win  *mygo.Window
}

func (h *host) SaveEdit(img *image.RGBA, doc render.EditDoc) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	saved, err := store.SaveEdit(h.item, img, doc.JSON())
	if err != nil {
		return err
	}
	h.item = saved
	fmt.Printf("saved %s: %d annotations, image %dx%d\n", saved.Dir, len(doc.Annotations), img.Rect.Dx(), img.Rect.Dy())
	return nil
}

func (h *host) Export(title string, img *image.RGBA, ask bool) (string, error) {
	dir := filepath.Join(settings.DataDir(), "Exports")
	path := filepath.Join(dir, title+".png")
	if ask {
		p, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: h.win, Title: "Save as", DefaultPath: path,
			Filters: []mygo.FileFilter{{Name: "PNG image", Extensions: []string{"png"}}}})
		if err != nil {
			return "", err
		}
		if p == "" {
			return "", editor.ErrCanceled
		}
		path = p
	} else {
		for n := 2; exists(path); n++ {
			path = filepath.Join(dir, fmt.Sprintf("%s %d.png", title, n))
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, err := encode(img)
	if err != nil {
		return "", err
	}
	fmt.Println("exported", path)
	return path, os.WriteFile(path, data, 0o644)
}

func exists(p string) bool { _, err := os.Stat(p); return err == nil }

func encode(img image.Image) ([]byte, error) {
	var buf bytes.Buffer
	err := png.Encode(&buf, img)
	return buf.Bytes(), err
}

func (h *host) Copy(img *image.RGBA) error {
	data, err := encode(img)
	if err != nil {
		return err
	}
	fmt.Printf("copied %dx%d\n", img.Rect.Dx(), img.Rect.Dy())
	return mygo.Clipboard.Write(transfer.New(transfer.NewItem(transfer.Bytes(transfer.PNG, data))))
}

func (h *host) Pin(img *image.RGBA, dpi float32) error {
	bmp := ui.NewBitmap(img)
	w, hh := float32(img.Rect.Dx())/dpi, float32(img.Rect.Dy())/dpi
	if k := min(1, 900/max(w, hh)); k < 1 {
		w, hh = w*k, hh*k
	}
	mygo.RunOnMain(func() {
		mygo.NewWindow(mygo.WindowOptions{Title: "editordemo pin", Width: int(w), Height: int(hh), Frameless: true, AlwaysOnTop: true,
			Content: ui.View(func(c *ui.Context) {
				ui.Image(c, bmp).Fill().DragWindow()
				if c.Shortcut(0, ui.KeyEscape) {
					for _, win := range mygo.Windows() {
						if win.Title() == "editordemo pin" {
							win.Close()
						}
					}
				}
			})})
	})
	fmt.Println("pinned")
	return nil
}

func (h *host) SaveStyle(style render.Style) error {
	s := settings.Load()
	s.Style = style
	return s.Save()
}

func (h *host) Closed() {
	fmt.Println("editor closed")
	mygo.App.Quit()
}

// verifyNewest decodes the newest capture's files and compares image.png with RenderDoc of
// edit.json over base.png.
func verifyNewest(lib store.Library) error {
	items := lib.Scan()
	if len(items) == 0 {
		return errors.New("the Library is empty")
	}
	item := items[0]
	base, raw, err := store.LoadEdit(item)
	if err != nil {
		return err
	}
	if raw == nil {
		return fmt.Errorf("%s has no edit.json", item.Dir)
	}
	doc, err := render.ParseEditDoc(raw)
	if err != nil {
		return err
	}
	want := render.RenderDoc(render.Premultiply(base), doc)
	f, err := os.Open(filepath.Join(item.Dir, "image.png"))
	if err != nil {
		return err
	}
	defer f.Close()
	got, err := png.Decode(f)
	if err != nil {
		return err
	}
	wantN := render.Unpremultiply(want)
	if got.Bounds() != wantN.Bounds() {
		return fmt.Errorf("image.png is %v, RenderDoc gives %v", got.Bounds(), wantN.Bounds())
	}
	diff := 0
	for y := 0; y < wantN.Rect.Dy(); y++ {
		for x := 0; x < wantN.Rect.Dx(); x++ {
			r1, g1, b1, a1 := got.At(x, y).RGBA()
			r2, g2, b2, a2 := wantN.At(x, y).RGBA()
			if r1>>8 != r2>>8 || g1>>8 != g2>>8 || b1>>8 != b2>>8 || a1>>8 != a2>>8 {
				diff++
			}
		}
	}
	if _, err := os.Stat(item.Thumb()); err != nil {
		return err
	}
	var kinds []string
	for _, a := range doc.Annotations {
		j, _ := json.Marshal(a)
		var t struct {
			Shape struct{ Type string } `json:"shape"`
		}
		json.Unmarshal(j, &t)
		kinds = append(kinds, t.Shape.Type)
	}
	slices.Sort(kinds)
	fmt.Printf("%s: image.png %v, %d of %d pixels differ from RenderDoc; edit.json has %d annotations %v, crop %v, background %T, padding %v; thumb.png present\n",
		item.Dir, got.Bounds().Size(), diff, wantN.Rect.Dx()*wantN.Rect.Dy(), len(doc.Annotations), kinds, doc.Crop, doc.Style.Background, doc.Style.Padding)
	if diff != 0 {
		return fmt.Errorf("%d pixels differ", diff)
	}
	return nil
}

// frameLog writes each MYGO_FRAME_STATS line to a file with the time it came, in Unix
// milliseconds, so -analyze can measure the frames of a span the driver marked.
type frameLog struct {
	mu sync.Mutex
	f  *os.File
}

var statLine = regexp.MustCompile(`took ([\d.]+) ms: build ([\d.]+).*layout ([\d.]+), paint ([\d.]+), present ([\d.]+) \(([^)]*)\)`)

func (l *frameLog) Write(b []byte) (int, error) {
	at := float64(time.Now().UnixMicro()) / 1000
	l.mu.Lock()
	defer l.mu.Unlock()
	fmt.Fprintf(l.f, "%.3f\t%s", at, b)
	if len(b) == 0 || b[len(b)-1] != '\n' {
		l.f.WriteString("\n")
	}
	return len(b), nil
}

// analyze reports the frames logged between from and to (Unix milliseconds).
func analyze(path string, from, to float64) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var at, total, build, layout, paint, present []float64
	paths := map[string]int{}
	num := func(s string) float64 { v, _ := strconv.ParseFloat(s, 64); return v }
	for _, line := range strings.Split(string(data), "\n") {
		ts, rest, ok := strings.Cut(line, "\t")
		m := statLine.FindStringSubmatch(rest)
		if !ok || m == nil || num(ts) < from || num(ts) > to {
			continue
		}
		at = append(at, num(ts))
		total, build, layout = append(total, num(m[1])), append(build, num(m[2])), append(layout, num(m[3]))
		paint, present = append(paint, num(m[4])), append(present, num(m[5]))
		kind, _, _ := strings.Cut(m[6], ":")
		if strings.Contains(m[6], "redrew") {
			kind += ", damaged part"
		}
		paths[kind]++
	}
	if len(at) < 2 {
		return fmt.Errorf("%d frames between %.0f and %.0f", len(at), from, to)
	}
	var interval []float64
	for i := 1; i < len(at); i++ {
		interval = append(interval, at[i]-at[i-1])
	}
	secs := (to - from) / 1000
	fmt.Printf("%d frames in %.2f s (%.1f/s)\n  interval %s\n  frame CPU %s\n  build %s\n  layout %s\n  paint %s\n  present %s\n  paths %v\n",
		len(at), secs, float64(len(at))/secs, q(interval), q(total), q(build), q(layout), q(paint), q(present), paths)
	return nil
}

func q(v []float64) string {
	s := slices.Clone(v)
	slices.Sort(s)
	at := func(p float64) float64 { return s[min(len(s)-1, int(p*float64(len(s))))] }
	return fmt.Sprintf("p50 %.2f, p90 %.2f, p99 %.2f, max %.2f ms", at(0.5), at(0.9), at(0.99), s[len(s)-1])
}
