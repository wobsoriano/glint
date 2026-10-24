package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/capture"
)

// stackRect prints the preview stack's window of a process as x,y,w,h: the visible
// no-activate flyout 244 DIPs wide, the stack's width with one card column.
func stackRect(args []string) error {
	fs := flag.NewFlagSet("stack", flag.ExitOnError)
	pid := fs.Int("pid", 0, "process id")
	fs.Parse(args)
	const wsExNoActivate = 0x08000000
	found := ""
	cb := windows.NewCallback(func(h, _ uintptr) uintptr {
		var p uint32
		windows.GetWindowThreadProcessId(windows.HWND(h), &p)
		if int(p) != *pid || !windows.IsWindowVisible(windows.HWND(h)) {
			return 1
		}
		ex, _, _ := procGetWindowLongPtrW.Call(h, uintptr(^uintptr(19)))
		var r struct{ L, T, R, B int32 }
		procGetWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
		w, hh := r.R-r.L, r.B-r.T
		if ex&wsExNoActivate != 0 && w >= 244 && w <= 244*3 && hh > 200 {
			found = fmt.Sprintf("%d,%d,%d,%d", r.L, r.T, w, hh)
			return 0
		}
		return 1
	})
	windows.EnumWindows(cb, nil)
	if found == "" {
		return fmt.Errorf("no preview stack of pid %d shows", *pid)
	}
	fmt.Println(found)
	return nil
}

// video writes an MP4 of a bar sweeping over a gradient, with a 440 Hz tone, for Trim &
// Compress and Quick Look to open.
func video(args []string) error {
	fs := flag.NewFlagSet("video", flag.ExitOnError)
	out := fs.String("out", "", "MP4 to write")
	w, h := fs.Int("w", 640, "width"), fs.Int("h", 360, "height")
	secs, fps := fs.Int("seconds", 4, "length"), fs.Int("fps", 30, "frames a second")
	fs.Parse(args)
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	const rate = 48_000
	wr, err := media.CreateVideoWriter(*out, media.VideoParams{Width: *w, Height: *h, FPS: *fps, Bitrate: media.RecordingBitrate(*w, *h, *fps)},
		&media.AudioParams{SampleRate: rate, Channels: 2})
	if err != nil {
		return err
	}
	defer wr.Close()
	img := image.NewRGBA(image.Rect(0, 0, *w, *h))
	var written int64
	for i := range *fps * *secs {
		bar := i * *w / (*fps * *secs)
		for y := range *h {
			for x := range *w {
				c := color.RGBA{uint8(x * 255 / *w), uint8(y * 255 / *h), 120, 255}
				if x >= bar && x < bar+24 {
					c = color.RGBA{255, 255, 255, 255}
				}
				img.SetRGBA(x, y, c)
			}
		}
		if err := wr.WriteFrame(img); err != nil {
			return err
		}
		until := int64(i+1) * rate / int64(*fps)
		samples := make([]float32, 0, (until-written)*2)
		for n := written; n < until; n++ {
			v := float32(0.2 * math.Sin(2*math.Pi*440*float64(n)/rate))
			samples = append(samples, v, v)
		}
		if err := wr.WriteAudio(samples); err != nil {
			return err
		}
		written = until
	}
	sum, err := wr.Finish()
	if err != nil {
		return err
	}
	fmt.Printf("video %s: %+v\n", *out, sum)
	return nil
}

// loopback records what the default output device plays for a while and prints its level, so
// a scenario can tell that Glint made a sound.
func loopback(args []string) error {
	fs := flag.NewFlagSet("loopback", flag.ExitOnError)
	secs := fs.Float64("seconds", 3, "how long to listen")
	least := fs.Float64("peak", 0.05, "least peak that counts as sound")
	silent := fs.Bool("silent", false, "pass when no sound plays, for a control")
	fs.Parse(args)
	dir, err := os.MkdirTemp("", "glintcheck-loopback")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	tap := make(chan []float32, 1024)
	clock := capture.NewClock()
	track, err := capture.StartTrack(capture.Loopback, filepath.Join(dir, "loopback.wav"), 2, clock, tap)
	if err != nil {
		return err
	}
	clock.Resume()
	time.Sleep(time.Duration(*secs * float64(time.Second)))
	track.Stop()
	close(tap)
	var sum float64
	var n int
	peak := 0.0
	for buf := range tap {
		for _, v := range buf {
			sum += float64(v) * float64(v)
			peak = math.Max(peak, math.Abs(float64(v)))
			n++
		}
	}
	rms := math.Sqrt(sum / float64(max(n, 1)))
	fmt.Printf("loopback %.1f s: %d samples, rms %.4f, peak %.4f\n", *secs, n, rms, peak)
	switch heard := peak >= *least; {
	case *silent && heard:
		return fmt.Errorf("the output device played sound (peak %.4f) where it should be quiet", peak)
	case !*silent && !heard:
		return fmt.Errorf("the output device played no sound (peak %.4f under %.4f)", peak, *least)
	}
	return nil
}
