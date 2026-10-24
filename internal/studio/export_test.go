package studio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"os"
	"path/filepath"
	"testing"
)

func TestGIFCanvasShrinksTo480ShortSide(t *testing.T) {
	size := func(w, h uint32) [2]uint32 { w, h = gifSize(w, h); return [2]uint32{w, h} }
	eq(t, size(1920, 1080), [2]uint32{852, 480})
	eq(t, size(1080, 1920), [2]uint32{480, 852})
	eq(t, size(640, 360), [2]uint32{640, 360})
}

func TestFrameCountsAndBitrates(t *testing.T) {
	eq(t, frameCount(7_000, 60), 420)
	eq(t, frameCount(5_975, 30), 179)
	eq(t, Bitrate(1920, 1080, 30, QualityHigh), 6_220_800)
	eq(t, Bitrate(1920, 1080, 60, QualityLow), 4_976_640)
	eq(t, Bitrate(320, 240, 30, QualityHigh), 1_500_000)
	eq(t, Bitrate(320, 240, 30, QualityMedium), 975_000)
	eq(t, Bitrate(320, 240, 30, QualityLow), 600_000)
}

func twoColours() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	for y := range 8 {
		for x := range 8 {
			c := color.NRGBA{250, 20, 20, 255}
			if x >= 4 {
				c = color.NRGBA{20, 20, 250, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func TestPaletteMapsFlatColoursToTheirNearestEntry(t *testing.T) {
	img := twoColours()
	pal := trainPalette([]*image.NRGBA{img})
	idx := pal.mapFrame(img, nil)
	colour := func(i uint8) []byte { return pal.rgb[int(i)*3 : int(i)*3+3] }
	if c := colour(idx[0]); !(c[0] > 200 && c[2] < 60) {
		t.Errorf("red maps to %v", c)
	}
	if c := colour(idx[7]); !(c[2] > 200 && c[0] < 60) {
		t.Errorf("blue maps to %v", c)
	}
}

// testdata/rust/palette.json is the palette the Rust exporter trains on two compositor
// frames, written by
//
//	(in a checkout of f1d58d2, the last Rust commit) cargo run --release -p glint-studio --example go_export -- palette internal/studio/testdata/compose/wide_zoom_click.png internal/studio/testdata/compose/portrait_plain.png > internal/studio/testdata/rust/palette.json
func TestPaletteMatchesRust(t *testing.T) {
	var want struct {
		RGB    []int  `json:"rgb"`
		LutFNV string `json:"lut_fnv"`
	}
	data, err := os.ReadFile(filepath.Join("testdata", "rust", "palette.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &want); err != nil {
		t.Fatal(err)
	}
	var frames []*image.NRGBA
	for _, name := range []string{"wide_zoom_click.png", "portrait_plain.png"} {
		img := readPNG(t, filepath.Join("testdata", "compose", name))
		n := image.NewNRGBA(img.Bounds())
		draw.Draw(n, n.Rect, img, img.Bounds().Min, draw.Src)
		frames = append(frames, n)
	}
	pal := trainPalette(frames)
	got := make([]int, len(pal.rgb))
	for i, v := range pal.rgb {
		got[i] = int(v)
	}
	eq(t, got, want.RGB)
	h := fnv.New64a()
	h.Write(pal.lut)
	eq(t, fmt.Sprintf("%016x", h.Sum64()), want.LutFNV)
}

func TestGIFWriterOutputDecodesWithItsDelaysAndLoop(t *testing.T) {
	img := twoColours()
	pal := trainPalette([]*image.NRGBA{img})
	var buf bytes.Buffer
	g, err := newGIFWriter(&buf, 8, 8, pal.rgb)
	if err != nil {
		t.Fatal(err)
	}
	idx := pal.mapFrame(img, nil)
	for _, d := range []uint16{7, 6, 7} {
		if err := g.writeFrame(idx, 8, 8, d); err != nil {
			t.Fatal(err)
		}
	}
	if err := g.finish(); err != nil {
		t.Fatal(err)
	}
	decoded, err := gif.DecodeAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	eq(t, decoded.Delay, []int{7, 6, 7})
	eq(t, decoded.LoopCount, 0)
	eq(t, len(decoded.Config.ColorModel.(color.Palette)), 256)
	eq(t, decoded.Image[2].Pix, idx)
}
