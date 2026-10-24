package main

import (
	"image"
	"math"
	"testing"
)

func TestToneOnsetsFindBurstsOverOtherAudio(t *testing.T) {
	const rate = 48_000
	samples := make([]float32, 3*rate*2)
	for i := range len(samples) / 2 {
		// Background: a loud 300 Hz hum, as when the desktop plays something else.
		v := 0.1 * math.Sin(2*math.Pi*300*float64(i)/rate)
		for _, at := range []float64{0.75, 2.0} {
			if s := float64(i)/rate - at; s >= 0 && s < 0.2 {
				v += 0.5 * math.Sin(2*math.Pi*1000*s)
			}
		}
		samples[2*i], samples[2*i+1] = float32(v), float32(v)
	}
	got := toneOnsets(samples, 2, rate)
	if len(got) != 2 || math.Abs(got[0]-750) > 2 || math.Abs(got[1]-2000) > 2 {
		t.Fatalf("onsets %v ms, want [750 2000] within 2 ms", got)
	}
}

func TestBarcodeRoundTripsAndRejectsTornReads(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, stageW+20, stageH+20))
	origin := image.Pt(10, 10)
	draw := func(frame int64) {
		for i := range 2 * barcodeBits {
			bit := frame >> (i % barcodeBits) & 1
			if i >= barcodeBits {
				bit ^= 1
			}
			c := black
			if bit == 1 {
				c = white
			}
			r := barcodeCell(i).Add(origin)
			for y := r.Min.Y; y < r.Max.Y; y++ {
				for x := r.Min.X; x < r.Max.X; x++ {
					img.SetRGBA(x, y, c)
				}
			}
		}
	}
	draw(4321)
	if n, ok := readBarcode(img, origin); !ok || n != 4321 {
		t.Fatalf("read %d (valid %v), want 4321", n, ok)
	}
	// A torn read: bit 1 (0 in 4321) flips on the first row only.
	r := barcodeCell(1).Add(origin)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, white)
		}
	}
	if _, ok := readBarcode(img, origin); ok {
		t.Fatal("a frame whose bits disagree with their complement must not read")
	}
}
