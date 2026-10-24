// Package synth writes Studio sessions with no user data in them: an animated screen.mp4,
// narration spoken by Windows SAPI as system.wav, and scripted input with clicks, keystrokes
// and a cursor sprite. Tests and cmd/studiodemo open them.
package synth

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/input"
)

// Options sizes a session.
type Options struct {
	Width, Height, FPS int
	// Speech narrates the session through SAPI; without it system.wav holds tone bursts.
	Speech bool
}

// Phrases are the narration, each followed by its pause in milliseconds. The fillers and the
// two-second pause give the transcript tools something to remove.
var Phrases = []struct {
	Text    string
	PauseMs int
}{
	{"Welcome to the Glint studio demo.", 700},
	{"Um, this recording shows the timeline and the captions.", 2_200},
	{"Uh, click a word to jump to it.", 600},
	{"Then cut the words you do not need.", 1_500},
	{"Zoom in on the clicks, and export when you are done.", 600},
}

const leadMs = 600

// Write makes a session in dir and returns its length in milliseconds.
func Write(dir string, o Options) (int, error) {
	if err := os.MkdirAll(filepath.Join(dir, "cursors"), 0o755); err != nil {
		return 0, err
	}
	var pcm []float32
	var err error
	if o.Speech {
		pcm, err = narration(dir)
	} else {
		pcm = tones(20_000)
	}
	if err != nil {
		return 0, err
	}
	ms := len(pcm) / 2 * 1000 / 48_000
	if err := writeWAV(filepath.Join(dir, "system.wav"), pcm); err != nil {
		return 0, err
	}
	if err := writeScreen(filepath.Join(dir, "screen.mp4"), o, ms); err != nil {
		return 0, err
	}
	if err := writeCursor(filepath.Join(dir, "cursors", "0.png")); err != nil {
		return 0, err
	}
	return ms, writeInput(dir, o, ms)
}

// narration speaks each phrase with SAPI and lays them end to end with their pauses, as
// 48 kHz stereo.
func narration(dir string) ([]float32, error) {
	tmp, err := os.MkdirTemp("", "glint-synth-speech")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	pcm := make([]float32, leadMs*48*2)
	for i, p := range Phrases {
		wav := filepath.Join(tmp, fmt.Sprintf("%d.wav", i))
		if err := speak(wav, p.Text); err != nil {
			return nil, err
		}
		w, err := media.ReadWAV(wav)
		if err != nil {
			return nil, err
		}
		if w.SampleRate != 48_000 || w.Channels != 1 {
			return nil, fmt.Errorf("SAPI wrote %d Hz %d ch, want 48000 Hz mono", w.SampleRate, w.Channels)
		}
		for _, s := range w.Samples {
			pcm = append(pcm, s, s)
		}
		pcm = append(pcm, make([]float32, p.PauseMs*48*2)...)
	}
	return pcm, nil
}

func speak(path, text string) error {
	script := `Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
$f = New-Object System.Speech.AudioFormat.SpeechAudioFormatInfo(48000, [System.Speech.AudioFormat.AudioBitsPerSample]::Sixteen, [System.Speech.AudioFormat.AudioChannel]::Mono)
$s.SetOutputToWaveFile($env:GLINT_SYNTH_WAV, $f)
$s.Speak($env:GLINT_SYNTH_TEXT)
$s.Dispose()`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", script)
	cmd.Env = append(os.Environ(), "GLINT_SYNTH_WAV="+path, "GLINT_SYNTH_TEXT="+text)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("SAPI synthesis: %v: %s", err, out)
	}
	return nil
}

// tones are 200 ms bursts of 440 Hz at every whole second but the first.
func tones(ms int) []float32 {
	pcm := make([]float32, ms*48*2)
	for i := range ms * 48 {
		t := float64(i) / 48
		if s := math.Mod(t, 1000); t >= 1000 && s < 200 {
			v := float32(0.4 * math.Sin(2*math.Pi*440*s/1000))
			pcm[2*i], pcm[2*i+1] = v, v
		}
	}
	return pcm
}

func writeWAV(path string, pcm []float32) error {
	w, err := media.CreateWAV(path, 48_000, 2)
	if err != nil {
		return err
	}
	if err := w.Write(pcm); err != nil {
		return err
	}
	_, err = w.Finish()
	return err
}

// writeScreen draws a grid with a box sweeping across it and a bar along the top that grows
// with time, so a frame on screen shows roughly when it is from.
func writeScreen(path string, o Options, ms int) error {
	w, err := media.CreateVideoWriter(path, media.VideoParams{Width: o.Width, Height: o.Height, FPS: o.FPS, Bitrate: media.RecordingBitrate(o.Width, o.Height, o.FPS)}, nil)
	if err != nil {
		return err
	}
	defer w.Close()
	bg := image.NewRGBA(image.Rect(0, 0, o.Width, o.Height))
	for y := range o.Height {
		for x := range o.Width {
			c := color.RGBA{uint8(30 + 40*x/o.Width), uint8(40 + 60*y/o.Height), 90, 255}
			if x%120 == 0 || y%120 == 0 {
				c = color.RGBA{80, 90, 110, 255}
			}
			bg.SetRGBA(x, y, c)
		}
	}
	img := image.NewRGBA(bg.Rect)
	frames := ms * o.FPS / 1000
	for i := range frames {
		t := float64(i) * 1000 / float64(o.FPS)
		copy(img.Pix, bg.Pix)
		fill(img, image.Rect(0, 0, int(float64(o.Width)*t/float64(ms)), o.Height/40), color.RGBA{255, 214, 10, 255})
		bx := int(float64(o.Width-o.Width/6) * (0.5 + 0.5*math.Sin(t/1500)))
		by := o.Height / 3
		fill(img, image.Rect(bx, by, bx+o.Width/6, by+o.Height/5), color.RGBA{236, 120, 40, 255})
		fill(img, image.Rect(o.Width*2/3, o.Height*2/3, o.Width*2/3+o.Width/8, o.Height*2/3+o.Height/9), color.RGBA{52, 120, 246, 255})
		if err := w.WriteFrame(img); err != nil {
			return err
		}
	}
	_, err = w.Finish()
	return err
}

func fill(img *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(img.Rect)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := img.Pix[y*img.Stride+r.Min.X*4 : y*img.Stride+r.Max.X*4]
		for x := 0; x < len(row); x += 4 {
			row[x], row[x+1], row[x+2], row[x+3] = c.R, c.G, c.B, 255
		}
	}
}

func writeCursor(path string) error {
	img := image.NewNRGBA(image.Rect(0, 0, 24, 36))
	for y := range 36 {
		for x := range 24 {
			inside := x <= y*2/3 && y < 30
			switch {
			case inside && (x == 0 || x == y*2/3 || y == 29):
				img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 255})
			case inside:
				img.SetNRGBA(x, y, color.NRGBA{255, 255, 255, 255})
			}
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// writeInput circles the pointer around the frame, clicks twice and presses two shortcuts.
func writeInput(dir string, o Options, ms int) error {
	w, h := float64(o.Width), float64(o.Height)
	in := input.Timeline{
		Version: 1,
		Size:    [2]uint32{uint32(o.Width), uint32(o.Height)},
		Clicks: []input.Click{
			{T: uint64(ms) * 2 / 10, Up: uint64(ms)*2/10 + 120, X: float32(w * 0.3), Y: float32(h * 0.4), Button: input.ButtonLeft},
			{T: uint64(ms) * 7 / 10, Up: uint64(ms)*7/10 + 120, X: float32(w * 0.7), Y: float32(h * 0.7), Button: input.ButtonLeft},
		},
		Keys:    []input.KeyPress{{T: uint64(ms) * 35 / 100, Label: "Ctrl+S"}, {T: uint64(ms) * 85 / 100, Label: "Ctrl+Shift+E"}},
		Cursors: []input.CursorSprite{{ID: 0, File: "cursors/0.png", Hotspot: [2]uint32{2, 2}, Scale: 1}},
	}
	for t := 0; t <= ms; t += 16 {
		a := float64(t) / 5_000 * 2 * math.Pi
		in.Pointer = append(in.Pointer, input.PointerSample{T: uint64(t), X: float32(w/2 + w*0.3*math.Cos(a)), Y: float32(h/2 + h*0.3*math.Sin(a))})
	}
	return in.Save(dir)
}
