// Package testclip writes the video Trim & Compress is checked against. Each frame carries its
// number as a bar code across the top, a bar sweeps across it, and the frame at every whole
// second is lighter. The audio is a steady tone with a click at every whole second.
package testclip

import (
	"image"
	"math"

	"github.com/wobsoriano/glint/internal/media"
)

const (
	SampleRate = 48_000
	ToneHz     = 440
	ClickHz    = 2_000
	// ClickMS is how long each click lasts.
	ClickMS = 5
	bits    = 12
)

// Write makes a seconds-long clip of w x h at fps with stereo audio.
func Write(path string, w, h, fps, seconds int) error {
	wr, err := media.CreateVideoWriter(path, media.VideoParams{Width: w, Height: h, FPS: fps, Bitrate: media.RecordingBitrate(w, h, fps)},
		&media.AudioParams{SampleRate: SampleRate, Channels: 2})
	if err != nil {
		return err
	}
	defer wr.Close()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	var written int64
	for i := range fps * seconds {
		paint(img, i, fps)
		if err := wr.WriteFrame(img); err != nil {
			return err
		}
		until := int64(i+1) * SampleRate / int64(fps)
		if err := wr.WriteAudio(Audio(written, until)); err != nil {
			return err
		}
		written = until
	}
	_, err = wr.Finish()
	return err
}

// Audio is the clip's stereo samples from frame a to frame b at SampleRate.
func Audio(a, b int64) []float32 {
	out := make([]float32, 0, (b-a)*2)
	for n := a; n < b; n++ {
		t := float64(n) / SampleRate
		v := 0.15 * math.Sin(2*math.Pi*ToneHz*t)
		if n%SampleRate < SampleRate*ClickMS/1000 {
			v = 0.8 * math.Sin(2*math.Pi*ClickHz*t)
		}
		out = append(out, float32(v), float32(v))
	}
	return out
}

func paint(img *image.RGBA, i, fps int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	base := uint8(40)
	if i%fps == 0 {
		base = 140
	}
	bar := (i * w / (fps * 2)) % w
	code := codeHeight(h)
	for y := range h {
		row := img.Pix[y*img.Stride:]
		for x := range w {
			p := row[x*4 : x*4+4]
			v := base + uint8(60*y/h)
			r, g, b := v, v/2+uint8(80*x/w), v
			if x >= bar && x < bar+w/40 && y >= code {
				r, g, b = 250, 250, 250
			}
			if y < code {
				r, g, b = 0, 0, 0
				if k := x / blockWidth(w); k < bits+2 && bit(i, k) {
					r, g, b = 255, 255, 255
				}
			}
			p[0], p[1], p[2], p[3] = r, g, b, 255
		}
	}
}

func blockWidth(w int) int { return w / (bits + 4) }
func codeHeight(h int) int { return h / 12 }

// bit is block k of frame i's code: the frame number's bits from the top, then a white and a
// black guard block that tell a code from anything else.
func bit(i, k int) bool {
	switch {
	case k < bits:
		return i>>(bits-1-k)&1 == 1
	case k == bits:
		return true
	}
	return false
}

// FrameNumber reads the code of a frame shown in r of img, scaled to any size. ok is false
// when the guard blocks are not there.
func FrameNumber(img *image.RGBA, r image.Rectangle) (n int, ok bool) {
	bw := float64(r.Dx()) / (bits + 4)
	y := r.Min.Y + int(float64(r.Dy())/24)
	lit := func(k int) bool {
		c := img.RGBAAt(r.Min.X+int(bw*(float64(k)+0.5)), y)
		return int(c.R)+int(c.G)+int(c.B) > 3*128
	}
	if !lit(bits) || lit(bits+1) {
		return 0, false
	}
	for k := range bits {
		n <<= 1
		if lit(k) {
			n |= 1
		}
	}
	return n, true
}
