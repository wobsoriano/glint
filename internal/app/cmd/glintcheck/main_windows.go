// Command glintcheck checks a running Glint against the real desktop. It shows a stage window
// of known pixels and text for Glint to capture, takes and compares screenshots, reads the
// clipboard, lists a Library and reports windows. The scenario scripts in this directory
// drive Glint with it, inside the desktop lock.
//
//	glintcheck stage [-x 160 -y 120 -w 1100 -h 640]
//	glintcheck shot -out f.png [-rect x,y,w,h]
//	glintcheck cmp -a a.png -b b.png [-arect x,y,w,h] [-brect x,y,w,h] [-tol n]
//	glintcheck dim -frozen before.png -shown during.png -sel x,y,w,h
//	glintcheck clip [-png out.png]
//	glintcheck library -dir <GLINT_DATA_DIR>
//	glintcheck wins [-pid n] [-title substring]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/store"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: glintcheck stage|shot|cmp|dim|clip|library|wins [flags]")
		os.Exit(2)
	}
	cmds := map[string]func([]string) error{
		"stage": stage, "shot": shot, "cmp": compare, "dim": dim, "clip": clip, "library": library, "wins": wins,
	}
	run, ok := cmds[os.Args[1]]
	if !ok {
		fmt.Fprintln(os.Stderr, "unknown command", os.Args[1])
		os.Exit(2)
	}
	if err := run(os.Args[2:]); err != nil {
		fmt.Fprintln(os.Stderr, "FAIL:", err)
		os.Exit(1)
	}
}

func parseRect(s string) (image.Rectangle, error) {
	var v [4]int
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return image.Rectangle{}, fmt.Errorf("rect %q is not x,y,w,h", s)
	}
	for i, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return image.Rectangle{}, err
		}
		v[i] = n
	}
	return image.Rect(v[0], v[1], v[0]+v[2], v[1]+v[3]), nil
}

// StageText is what the stage's text panel says, line by line.
var StageText = []string{"Glint reads this text", "Second line 12345", "Ship it on Friday"}

// stage shows a window of seeded noise beside black text on white, which stays the same from
// run to run, for captures to be compared against and read.
func stage(args []string) error {
	fs := flag.NewFlagSet("stage", flag.ExitOnError)
	x, y := fs.Int("x", 160, "left, DIPs"), fs.Int("y", 120, "top, DIPs")
	w, h := fs.Int("w", 1100, "width, DIPs"), fs.Int("h", 640, "height, DIPs")
	fs.Parse(args)
	noise := image.NewRGBA(image.Rect(0, 0, 420, 420))
	r := rand.New(rand.NewPCG(7, 11))
	for i := 0; i < len(noise.Pix); i += 4 {
		v := r.Uint32()
		noise.Pix[i], noise.Pix[i+1], noise.Pix[i+2], noise.Pix[i+3] = uint8(v), uint8(v>>8), uint8(v>>16), 255
	}
	bmp := ui.NewBitmap(noise).Pixelated()
	mygo.App.SetName("Glint stage")
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title: "Glint stage", X: *x, Y: *y, Width: *w, Height: *h, AlwaysOnTop: true,
			Content: ui.View(func(c *ui.Context) {
				c.Root().Background(ui.RGB(255, 255, 255))
				ui.Row(c).Fill().Padding(24).Gap(32).Children(func() {
					ui.Image(c, bmp).Size(420, 420)
					ui.Column(c).Gap(18).Padding(20, 0).Children(func() {
						for _, line := range StageText {
							ui.Text(c, line).FontSize(34).TextColor(ui.RGB(0, 0, 0))
						}
					})
				})
			}),
		})
		win.Focus()
	})
	return mygo.App.Run()
}

func shot(args []string) error {
	fs := flag.NewFlagSet("shot", flag.ExitOnError)
	out := fs.String("out", "", "PNG to write")
	rect := fs.String("rect", "", "x,y,w,h in physical pixels (default: every display)")
	fs.Parse(args)
	r := platform.Rect{}
	if *rect != "" {
		rr, err := parseRect(*rect)
		if err != nil {
			return err
		}
		r = platform.Rect{X: rr.Min.X, Y: rr.Min.Y, W: rr.Dx(), H: rr.Dy()}
	} else {
		ds, err := platform.Displays()
		if err != nil {
			return err
		}
		for _, d := range ds {
			if r.Empty() {
				r = d.Bounds
				continue
			}
			x0, y0 := min(r.X, d.Bounds.X), min(r.Y, d.Bounds.Y)
			x1, y1 := max(r.X+r.W, d.Bounds.X+d.Bounds.W), max(r.Y+r.H, d.Bounds.Y+d.Bounds.H)
			r = platform.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
		}
	}
	img, err := platform.CaptureRect(r)
	if err != nil {
		return err
	}
	fmt.Printf("shot %+v -> %s\n", r, *out)
	return writePNG(*out, img)
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func readImage(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	return img, err
}

func rgba(img image.Image, x, y int) color.RGBA {
	return color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
}

// compare counts the pixels of a's arect that differ from b's brect by more than tol in a
// channel, and fails when any does.
func compare(args []string) error {
	fs := flag.NewFlagSet("cmp", flag.ExitOnError)
	pa, pb := fs.String("a", "", "first PNG"), fs.String("b", "", "second PNG")
	ra, rb := fs.String("arect", "", "x,y,w,h of a"), fs.String("brect", "", "x,y,w,h of b")
	tol := fs.Int("tol", 0, "largest channel difference that counts as equal")
	fs.Parse(args)
	a, err := readImage(*pa)
	if err != nil {
		return err
	}
	b, err := readImage(*pb)
	if err != nil {
		return err
	}
	areaA, areaB := a.Bounds(), b.Bounds()
	if *ra != "" {
		if areaA, err = parseRect(*ra); err != nil {
			return err
		}
	}
	if *rb != "" {
		if areaB, err = parseRect(*rb); err != nil {
			return err
		}
	} else if *ra != "" && b.Bounds().Size() == a.Bounds().Size() {
		areaB = areaA
	}
	if areaA.Size() != areaB.Size() {
		return fmt.Errorf("sizes differ: %v against %v", areaA.Size(), areaB.Size())
	}
	n, worst, first := 0, 0, image.Point{-1, -1}
	for y := range areaA.Dy() {
		for x := range areaA.Dx() {
			ca, cb := rgba(a, areaA.Min.X+x, areaA.Min.Y+y), rgba(b, areaB.Min.X+x, areaB.Min.Y+y)
			d := max(absDiff(ca.R, cb.R), absDiff(ca.G, cb.G), absDiff(ca.B, cb.B), absDiff(ca.A, cb.A))
			worst = max(worst, d)
			if d > *tol {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
			}
		}
	}
	total := areaA.Dx() * areaA.Dy()
	fmt.Printf("cmp %s%v vs %s%v: %d of %d pixels differ (tol %d, max delta %d, first at %v)\n", filepath.Base(*pa), areaA, filepath.Base(*pb), areaB, n, total, *tol, worst, first)
	if n > 0 {
		return fmt.Errorf("%d pixels differ", n)
	}
	return nil
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// dim checks the overlay mid-drag: inside the selection the frozen screen shows unchanged,
// and away from the selection, the hint, the loupe and the label it shows dimmed by the
// overlay's 0x73 black.
func dim(args []string) error {
	fs := flag.NewFlagSet("dim", flag.ExitOnError)
	pf, ps := fs.String("frozen", "", "screen before the overlay"), fs.String("shown", "", "screen with the overlay")
	sel := fs.String("sel", "", "selection x,y,w,h")
	fs.Parse(args)
	frozen, err := readImage(*pf)
	if err != nil {
		return err
	}
	shown, err := readImage(*ps)
	if err != nil {
		return err
	}
	s, err := parseRect(*sel)
	if err != nil {
		return err
	}
	inner := s.Inset(3)
	in, inTotal := 0, 0
	for y := inner.Min.Y; y < inner.Max.Y; y++ {
		for x := inner.Min.X; x < inner.Max.X; x++ {
			inTotal++
			if rgba(frozen, x, y) != rgba(shown, x, y) {
				in++
			}
		}
	}
	const keep = 1 - float64(0x73)/255
	b := frozen.Bounds()
	away := s.Inset(-40)
	off, offTotal, worst := 0, 0, 0
	for y := b.Min.Y + 100; y < b.Max.Y; y += 2 {
		for x := b.Min.X; x < b.Max.X; x += 2 {
			p := image.Pt(x, y)
			// The loupe sits below and right of the pointer, the selection's far corner.
			loupe := image.Rect(s.Max.X, s.Max.Y, s.Max.X+200, s.Max.Y+200)
			if p.In(away) || p.In(loupe) {
				continue
			}
			f, sh := rgba(frozen, x, y), rgba(shown, x, y)
			want := color.RGBA{uint8(float64(f.R) * keep), uint8(float64(f.G) * keep), uint8(float64(f.B) * keep), 255}
			d := max(absDiff(want.R, sh.R), absDiff(want.G, sh.G), absDiff(want.B, sh.B))
			offTotal++
			worst = max(worst, d)
			if d > 3 {
				off++
			}
		}
	}
	fmt.Printf("dim: inside the selection %d of %d pixels differ from the frozen screen; outside %d of %d sampled pixels are not the frozen screen dimmed by 0x73 (max delta %d)\n", in, inTotal, off, offTotal, worst)
	if in > 0 || off > offTotal/1000 {
		return fmt.Errorf("the overlay does not show the frozen screen 1:1")
	}
	return nil
}

// library lists a Library's captures with their meta.json and files.
func library(args []string) error {
	fs := flag.NewFlagSet("library", flag.ExitOnError)
	dir := fs.String("dir", os.Getenv("GLINT_DATA_DIR"), "data directory")
	fs.Parse(args)
	items := store.New(filepath.Join(*dir, "Library")).Scan()
	fmt.Printf("library %s: %d item(s)\n", *dir, len(items))
	for _, it := range items {
		m, _ := json.Marshal(it.Meta)
		ents, _ := os.ReadDir(it.Dir)
		var files []string
		for _, e := range ents {
			files = append(files, e.Name())
		}
		fmt.Printf("  %s %s files=%v\n", it.ID(), m, files)
	}
	return nil
}
