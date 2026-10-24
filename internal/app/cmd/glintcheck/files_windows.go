package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/actions"
	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// edited checks a capture the editor saved: image.png is RenderDoc of base.png and edit.json,
// and thumb.png is the Library's thumbnail of that image.
func edited(args []string) error {
	fs := flag.NewFlagSet("edited", flag.ExitOnError)
	dir := fs.String("dir", "", "capture directory")
	fs.Parse(args)
	item, doc, want, err := flatten(*dir)
	if err != nil {
		return err
	}
	got, err := readImage(item.Deliverable())
	if err != nil {
		return err
	}
	thumb, err := readImage(item.Thumb())
	if err != nil {
		return err
	}
	diff := differing(got, render.Unpremultiply(want))
	thumbDiff := differing(thumb, store.Thumbnail(want))
	kinds := map[string]int{}
	for _, a := range doc.Annotations {
		kinds[fmt.Sprintf("%T", a.Shape)]++
	}
	fmt.Printf("edited %s: image.png %v, %d pixels differ from RenderDoc of base.png + edit.json; thumb.png %v, %d pixels differ; annotations %v\n",
		item.ID(), got.Bounds().Size(), diff, thumb.Bounds().Size(), thumbDiff, kinds)
	if diff != 0 || thumbDiff != 0 {
		return errors.New("the saved files are not the edit")
	}
	return nil
}

// flatten is the capture's edit as the editor flattens it: RenderDoc of base.png and edit.json.
func flatten(dir string) (store.Item, render.EditDoc, *image.RGBA, error) {
	item, err := store.Load(dir)
	if err != nil {
		return item, render.EditDoc{}, nil, err
	}
	base, raw, err := store.LoadEdit(item)
	if err != nil {
		return item, render.EditDoc{}, nil, err
	}
	if raw == nil {
		return item, render.EditDoc{}, nil, fmt.Errorf("%s has no edit.json", item.Dir)
	}
	doc, err := render.ParseEditDoc(raw)
	if err != nil {
		return item, doc, nil, err
	}
	return item, doc, render.RenderDoc(render.Premultiply(base), doc), nil
}

// differing counts the pixels whose 8-bit channels differ, every pixel when the sizes do.
func differing(a, b image.Image) int {
	ra, rb := a.Bounds(), b.Bounds()
	if ra.Size() != rb.Size() {
		return max(ra.Dx()*ra.Dy(), rb.Dx()*rb.Dy())
	}
	n := 0
	for y := range ra.Dy() {
		for x := range ra.Dx() {
			r1, g1, b1, a1 := a.At(ra.Min.X+x, ra.Min.Y+y).RGBA()
			r2, g2, b2, a2 := b.At(rb.Min.X+x, rb.Min.Y+y).RGBA()
			if r1>>8 != r2>>8 || g1>>8 != g2>>8 || b1>>8 != b2>>8 || a1>>8 != a2>>8 {
				n++
			}
		}
	}
	return n
}

// encoded succeeds when file holds exactly what Glint writes for a capture's edit as a JPEG of
// quality q.
func encoded(args []string) error {
	fs := flag.NewFlagSet("encoded", flag.ExitOnError)
	dir, file := fs.String("dir", "", "capture directory the editor exported"), fs.String("file", "", "the export")
	q := fs.Int("quality", 85, "JPEG quality")
	fs.Parse(args)
	_, _, img, err := flatten(*dir)
	if err != nil {
		return err
	}
	want, err := actions.Encode(img, settings.FormatJPEG, uint8(*q))
	if err != nil {
		return err
	}
	got, err := os.ReadFile(*file)
	if err != nil {
		return err
	}
	same := bytes.Equal(got, want)
	fmt.Printf("encoded %s: %d bytes; a quality %d JPEG of the edit is %d bytes; identical: %v\n", filepath.Base(*file), len(got), *q, len(want), same)
	if !same {
		return errors.New("the export is not that JPEG")
	}
	return nil
}

// probe decodes a video: its size, length and frames, and the loudness of its sound.
func probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ExitOnError)
	file := fs.String("file", "", "MP4 to decode")
	fs.Parse(args)
	r, err := media.OpenVideoReader(*file)
	if err != nil {
		return err
	}
	frames, size := 0, image.Point{}
	for {
		f, err := r.NextFrame()
		if err != nil {
			break
		}
		frames++
		size = f.Image.Rect.Size()
	}
	dur := r.DurationMS()
	r.Close()
	audio := "no sound track"
	if w, err := media.ReadMP4Audio(*file); err == nil {
		peak, sum := 0.0, 0.0
		for _, s := range w.Samples {
			v := math.Abs(float64(s))
			peak = max(peak, v)
			sum += v * v
		}
		rms := 0.0
		if len(w.Samples) > 0 {
			rms = math.Sqrt(sum / float64(len(w.Samples)))
		}
		audio = fmt.Sprintf("sound %d Hz x%d, %.2f s, peak %.3f, rms %.4f", w.SampleRate, w.Channels,
			float64(len(w.Samples))/float64(w.SampleRate*max(w.Channels, 1)), peak, rms)
	}
	fmt.Printf("probe %s: %v, %d ms, %d frames; %s\n", *file, size, dur, frames, audio)
	if frames == 0 {
		return errors.New("no frames decode")
	}
	return nil
}
