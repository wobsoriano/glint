package main

import (
	"image"
	"image/color"
	"math"
)

// The stage is the animated window the tool records. Each composition it paints its frame
// number as a barcode, moves a box, and shows a white square while a tone plays, so the
// video can be checked frame by frame against the audio and the pointer.
const (
	stageW, stageH = 640, 400
	cell           = 16
	barcodeBits    = 16
)

var (
	barcodeAt   = image.Pt(16, 16)
	flashRect   = image.Rect(16, 144, 136, 264)
	zoneRect    = image.Rect(160, 144, stageW, stageH)
	excludeRect = image.Rect(520, 160, 620, 260)
	// cursorAt is where the stage puts the pointer, inside the plain zone, when it can.
	cursorAt = image.Pt(400, 272)

	stageBG    = color.RGBA{40, 40, 40, 255}
	zoneColour = color.RGBA{0, 128, 128, 255}
	boxColour  = color.RGBA{230, 120, 20, 255}
	magenta    = color.RGBA{255, 0, 255, 255}
	white      = color.RGBA{255, 255, 255, 255}
	black      = color.RGBA{0, 0, 0, 255}
)

// barcodeCell is bit i's square: the frame number's 16 bits on the first row, their
// complement on the second, so a torn or blurred read is rejected rather than misread.
func barcodeCell(i int) image.Rectangle {
	p := barcodeAt.Add(image.Pt((i%barcodeBits)*cell, (i/barcodeBits)*cell))
	return image.Rectangle{Min: p, Max: p.Add(image.Pt(cell, cell))}
}

func boxRect(frame int64) image.Rectangle {
	x := 16 + int(frame*8%int64(stageW-32-48))
	return image.Rect(x, 64, x+48, 112)
}

// mean is the average colour of r in img.
func mean(img *image.RGBA, r image.Rectangle) (float64, float64, float64) {
	r = r.Intersect(img.Rect)
	var sr, sg, sb float64
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			sr, sg, sb = sr+float64(p.R), sg+float64(p.G), sb+float64(p.B)
		}
	}
	n := float64(max(r.Dx()*r.Dy(), 1))
	return sr / n, sg / n, sb / n
}

// readBarcode decodes the frame number of the stage whose top-left sits at origin in img.
func readBarcode(img *image.RGBA, origin image.Point) (int64, bool) {
	var v, inv int64
	for i := range 2 * barcodeBits {
		c := barcodeCell(i).Inset(5).Add(origin)
		_, g, _ := mean(img, c)
		bit := int64(0)
		if g > 128 {
			bit = 1
		}
		if i < barcodeBits {
			v |= bit << i
		} else {
			inv |= bit << (i - barcodeBits)
		}
	}
	return v, v^inv == 1<<barcodeBits-1
}

// flashOn says whether the stage at origin shows its white square.
func flashOn(img *image.RGBA, origin image.Point) bool {
	r, g, b := mean(img, flashRect.Inset(8).Add(origin))
	return r > 200 && g > 200 && b > 200
}

// worstDistance is the largest per-channel distance from want over r.
func worstDistance(img *image.RGBA, r image.Rectangle, want color.RGBA) int {
	worst := 0
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			for _, d := range [3]int{int(p.R) - int(want.R), int(p.G) - int(want.G), int(p.B) - int(want.B)} {
				worst = max(worst, d, -d)
			}
		}
	}
	return worst
}

// toneOnsets finds where each 1 kHz burst starts in channel 0 of interleaved samples, in
// milliseconds. It follows the 1 kHz amplitude in 5 ms windows a millisecond apart, so
// whatever else the desktop plays does not hide the bursts, and reports the first rise
// above 0.1 after 100 ms below 0.02.
func toneOnsets(samples []float32, channels, rate int) []float64 {
	n, hop := rate/200, rate/1000
	coeff := 2 * math.Cos(2*math.Pi*1000/float64(rate))
	var out []float64
	quiet := 0
	for start := 0; (start+n)*channels <= len(samples); start += hop {
		var s1, s2 float64
		for i := start; i < start+n; i++ {
			s1, s2 = float64(samples[i*channels])+coeff*s1-s2, s1
		}
		amp := 2 * math.Sqrt(max(s1*s1+s2*s2-coeff*s1*s2, 0)) / float64(n)
		switch {
		case amp > 0.1:
			if quiet >= 100 {
				// A step onset fills a fifth of the window when its estimate crosses 0.1 of
				// a 0.5 tone, so the onset lies four fifths of a window after its start.
				out = append(out, (float64(start)+0.8*float64(n))*1000/float64(rate))
			}
			quiet = 0
		case amp < 0.02:
			quiet++
		}
	}
	return out
}
