package main

import (
	"encoding/json"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/transcribe"
)

const (
	srcW, srcH   = 1280, 720
	srcFPS       = 30
	srcMs        = 12_000
	flashMs      = 200
	toneHz       = 440.0
	toneAmp      = 0.5
	toneAttackMs = 2
)

// flashes are the source times where the screen turns white and system audio plays a tone,
// both for flashMs, so output frames and output audio can be checked for the same onsets.
var flashes = []float64{1_000, 3_500, 5_000, 7_000, 9_000, 11_000}

func flashing(ms float64) bool {
	for _, f := range flashes {
		if ms >= f && ms < f+flashMs {
			return true
		}
	}
	return false
}

// synthesize writes a session with no user data: a moving pattern with white flashes as
// screen.mp4, tone bursts at the flashes as system.wav, scripted input with a cursor sprite,
// and a transcript.
func synthesize(dir string) error {
	if err := os.MkdirAll(filepath.Join(dir, "cursors"), 0o755); err != nil {
		return err
	}
	if err := writeScreen(filepath.Join(dir, "screen.mp4")); err != nil {
		return err
	}
	if err := writeTones(filepath.Join(dir, "system.wav")); err != nil {
		return err
	}
	if err := writeCursor(filepath.Join(dir, "cursors", "0.png")); err != nil {
		return err
	}
	in := input.Timeline{
		Version: 1,
		Size:    [2]uint32{srcW, srcH},
		Clicks: []input.Click{
			{T: 2_000, Up: 2_120, X: 400, Y: 300, Button: input.ButtonLeft},
			{T: 9_500, Up: 9_600, X: 900, Y: 500, Button: input.ButtonLeft},
		},
		Keys:    []input.KeyPress{{T: 4_000, Label: "Ctrl+S"}, {T: 10_000, Label: "Enter"}},
		Cursors: []input.CursorSprite{{ID: 0, File: "cursors/0.png", Hotspot: [2]uint32{2, 2}, Scale: 1}},
	}
	for t := uint64(0); t <= srcMs; t += 16 {
		a := float64(t) / 4_000 * 2 * math.Pi
		in.Pointer = append(in.Pointer, input.PointerSample{T: t, X: float32(640 + 260*math.Cos(a)), Y: float32(360 + 180*math.Sin(a))})
	}
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, input.File), data, 0o644); err != nil {
		return err
	}
	words := []transcribe.Word{
		{Text: "Hello", StartMs: 400, EndMs: 800, Confidence: 1},
		{Text: "from", StartMs: 850, EndMs: 1_100, Confidence: 1},
		{Text: "Glint.", StartMs: 1_150, EndMs: 1_700, Confidence: 1},
		{Text: "Twice", StartMs: 3_200, EndMs: 3_700, Confidence: 1},
		{Text: "as", StartMs: 3_750, EndMs: 3_900, Confidence: 1},
		{Text: "fast.", StartMs: 3_950, EndMs: 4_600, Confidence: 1},
		{Text: "Zoomed", StartMs: 8_700, EndMs: 9_300, Confidence: 1},
		{Text: "in", StartMs: 9_350, EndMs: 9_600, Confidence: 1},
		{Text: "now.", StartMs: 9_650, EndMs: 10_300, Confidence: 1},
	}
	return transcribe.NewTranscript("en", words).Save(filepath.Join(dir, transcribe.TranscriptFile))
}

func screenFrame(img *image.RGBA, ms float64) {
	if flashing(ms) {
		for i := range img.Pix {
			img.Pix[i] = 255
		}
		return
	}
	shift := ms / 40
	boxX := int(80 + ms*0.08)
	for y := range srcH {
		for x := range srcW {
			p := img.Pix[y*img.Stride+x*4:]
			v := uint8(40 + 60*(1+math.Sin((float64(x)+shift)/90))/2)
			c := color.RGBA{v, uint8(50 + y*80/srcH), uint8(110 - y*50/srcH), 255}
			switch {
			case x%80 == 0 || y%80 == 0:
				c = color.RGBA{90, 96, 110, 255}
			case x >= boxX && x < boxX+220 && y >= 250 && y < 390:
				c = color.RGBA{236, 120, 40, 255}
			case x >= 860 && x < 1100 && y >= 520 && y < 620:
				c = color.RGBA{52, 120, 246, 255}
			}
			p[0], p[1], p[2], p[3] = c.R, c.G, c.B, 255
		}
	}
}

func writeScreen(path string) error {
	w, err := media.CreateVideoWriter(path, media.VideoParams{Width: srcW, Height: srcH, FPS: srcFPS, Bitrate: media.RecordingBitrate(srcW, srcH, srcFPS)}, nil)
	if err != nil {
		return err
	}
	defer w.Close()
	img := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
	for i := range srcMs * srcFPS / 1000 {
		screenFrame(img, float64(i)*1000/srcFPS)
		if err := w.WriteFrame(img); err != nil {
			return err
		}
	}
	_, err = w.Finish()
	return err
}

// writeCamera is a fake camera: a teal frame with a white bar
// sweeping across it, at 30 fps for the whole recording.
func writeCamera(path string) error {
	w, err := media.CreateVideoWriter(path, media.VideoParams{Width: 640, Height: 480, FPS: 30, Bitrate: 2_000_000}, nil)
	if err != nil {
		return err
	}
	defer w.Close()
	img := image.NewRGBA(image.Rect(0, 0, 640, 480))
	for i := range srcMs * 30 / 1000 {
		t := float64(i) / 30
		x0 := int(t * 60)
		for y := range 480 {
			for x := range 640 {
				c := color.RGBA{30, uint8(60 + t*20), 90, 255}
				if x >= x0 && x < x0+24 {
					c = color.RGBA{255, 255, 255, 255}
				}
				img.SetRGBA(x, y, c)
			}
		}
		if err := w.WriteFrame(img); err != nil {
			return err
		}
	}
	_, err = w.Finish()
	return err
}

func writeTones(path string) error {
	w, err := media.CreateWAV(path, studio.SampleRate, 2)
	if err != nil {
		return err
	}
	frames := srcMs * studio.SampleRate / 1000
	pcm := make([]float32, 0, frames*2)
	for i := range frames {
		ms := float64(i) * 1000 / studio.SampleRate
		var v float32
		for _, f := range flashes {
			if ms >= f && ms < f+flashMs {
				attack := min((ms-f)/toneAttackMs, 1)
				v = float32(toneAmp * attack * math.Sin(2*math.Pi*toneHz*(ms-f)/1000))
			}
		}
		pcm = append(pcm, v, v)
	}
	if err := w.Write(pcm); err != nil {
		return err
	}
	_, err = w.Finish()
	return err
}

func writeCursor(path string) error {
	img := image.NewNRGBA(image.Rect(0, 0, 24, 36))
	for y := range 36 {
		for x := range 24 {
			inside := x <= y*2/3 && y < 30
			edge := inside && (x == 0 || x == y*2/3 || y == 29)
			switch {
			case edge:
				img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
			case inside:
				img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			}
		}
	}
	return savePNG(path, img)
}

// baseProject is the edit every variant shares: split into four clips, the second at 2x,
// the third deleted, and a fixed zoom over the last.
func baseProject(cues []studio.CaptionCue) studio.Project {
	p := studio.NewProject(srcMs)
	for _, at := range []float64{3_000, 6_000, 8_000} {
		p.SplitAt(at)
	}
	p.SetSpeed(1, 2)
	p.DeleteClip(2)
	p.Zooms = []studio.Zoom{{StartMs: 8_600, EndMs: 11_200, Scale: 2, Focus: studio.FixedFocus(0.7, 0.45), Enabled: true}}
	p.CaptionCues = cues
	return p
}
