// Command quicklookdemo previews a demo screenshot, or with -recording a demo recording, from a
// Library under GLINT_DATA_DIR. With -auto it drives Quick Look on the real desktop, checks it
// and writes screenshots to -out, then quits with 1 if a check failed. Run it inside the
// desktop lock:
//
//	go build -o %TEMP%\quicklookdemo.exe ./internal/ui/quicklook/cmd/quicklookdemo
//	set GLINT_DATA_DIR=%TEMP%\glint-quicklookdemo
//	go run ./tools/desklock -- %TEMP%\quicklookdemo.exe -auto
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/quicklook"
	"github.com/wobsoriano/glint/internal/ui/stack/drive"
)

var (
	auto       = flag.Bool("auto", false, "drive Quick Look, check it and quit")
	recording  = flag.Bool("recording", false, "preview the recording instead of the screenshot")
	foreground = flag.Bool("foreground", false, "run as the other app that keeps the keyboard")
	outDir     = flag.String("out", filepath.Join(os.TempDir(), "glint-quicklookdemo-evidence"), "where screenshots go")
)

type host struct{}

func (host) Copy(i store.Item) error   { fmt.Println("host: copy", i.Meta.Title); return nil }
func (host) Edit(i store.Item) error   { fmt.Println("host: edit", i.Meta.Title); return nil }
func (host) Reveal(i store.Item) error { fmt.Println("host: reveal", i.Meta.Title); return nil }

var (
	stripeA = color.RGBA{0x8E, 0x44, 0xAD, 255}
	stripeB = color.RGBA{0xF5, 0xEE, 0xF8, 255}
)

func main() {
	flag.Parse()
	if *foreground {
		drive.RunForeground()
		if err := mygo.App.Run(); err != nil {
			log.Fatal(err)
		}
		return
	}
	if os.Getenv("GLINT_DATA_DIR") == "" {
		dir, err := os.MkdirTemp("", "glint-quicklookdemo")
		if err != nil {
			log.Fatal(err)
		}
		os.Setenv("GLINT_DATA_DIR", dir)
	}
	lib := store.New(store.DefaultRoot())
	shot, err := drive.Screenshot(lib, "Quick Look shot", 1600, 900, stripeA, stripeB, time.Now())
	if err != nil {
		log.Fatal("screenshot: ", err)
	}
	rec, err := drive.Recording(lib, 640, 360, 8, time.Now().Add(time.Second))
	if err != nil {
		log.Fatal("recording: ", err)
	}
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		v := quicklook.New(host{})
		if *auto {
			go run(v, shot, rec)
			return
		}
		if *recording {
			v.Open(rec)
		} else {
			v.Open(shot)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// share is how much of img is one of the two colors, within 12 per channel.
func share(img *image.RGBA, a, b color.RGBA) float64 {
	near := func(p, q color.RGBA) bool {
		d := func(x, y uint8) bool { return int(x)-int(y) <= 12 && int(y)-int(x) <= 12 }
		return d(p.R, q.R) && d(p.G, q.G) && d(p.B, q.B)
	}
	n, all := 0, 0
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y += 2 {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x += 2 {
			p := img.RGBAAt(x, y)
			if near(p, a) || near(p, b) {
				n++
			}
			all++
		}
	}
	return float64(n) / float64(max(all, 1))
}

// barX finds the recording's white bar along the middle row of r, -1 when it does not show.
func barX(r platform.Rect) int {
	img := drive.Capture(platform.Rect{X: r.X, Y: r.Y + r.H/2, W: r.W, H: 1})
	for x := range r.W {
		if p := img.RGBAAt(x, 0); p.R > 230 && p.G > 230 && p.B > 230 {
			return r.X + x
		}
	}
	return -1
}

// focus gives the window the keyboard with a click at (x, y) inside it, as a user would.
func focus(l *drive.Log, h uintptr, x, y int) {
	if drive.Foreground() != h {
		drive.Click(x, y)
		time.Sleep(300 * time.Millisecond)
	}
	l.Check(drive.Foreground() == h, "Quick Look has the keyboard (foreground %q)", drive.WindowText(drive.Foreground()))
}

func run(v *quicklook.Viewer, shot, rec store.Item) {
	l := drive.NewLog(*outDir)
	defer l.Finish()
	sx, sy := platform.CursorPos()
	defer drive.MoveTo(sx, sy)
	drive.MoveTo(960, 40)

	fg, other := drive.StartForeground(l)
	if fg != nil {
		defer fg.Process.Kill()
	}
	l.Check(other != 0, "another app has the keyboard")

	v.Open(shot)
	var h uintptr
	l.Check(drive.Until(3*time.Second, func() bool { w := v.Window(); return w != nil && drive.Visible(w.NativeHandle()) }), "Quick Look opens for the screenshot")
	h = v.Window().NativeHandle()
	time.Sleep(700 * time.Millisecond)
	r := drive.WindowRect(h)
	l.Printf("Quick Look window %+v, title %q", r, drive.WindowText(h))
	stage := platform.Rect{X: r.X + 60, Y: r.Y + 80, W: 480, H: 260}
	focus(l, h, stage.X+stage.W/2, stage.Y+stage.H/2)
	l.Shot("quicklook-image.png", r)
	l.Check(r.W >= 1000 && r.H >= 600, "the window is large: %dx%d for a 1600x900 capture", r.W, r.H)
	s := share(drive.Capture(stage), stripeA, stripeB)
	l.Check(s > 0.9, "the stage shows the capture's pixels (%.0f%% stripe colors)", s*100)
	drive.Keys(drive.VKSpace)
	l.Check(drive.Until(2*time.Second, func() bool { return !drive.Visible(h) }), "Space closes Quick Look")

	v.Open(rec)
	l.Check(drive.Until(3*time.Second, func() bool { w := v.Window(); return w != nil && drive.Visible(w.NativeHandle()) }), "Quick Look opens for the recording")
	h = v.Window().NativeHandle()
	time.Sleep(500 * time.Millisecond)
	r = drive.WindowRect(h)
	stage = platform.Rect{X: r.X + 30, Y: r.Y + 60, W: r.W - 60, H: r.H - 160}
	n0, ms0 := v.Frames()
	x0 := barX(stage)
	l.Shot("quicklook-video-1.png", r)
	time.Sleep(time.Second)
	n1, ms1 := v.Frames()
	x1 := barX(stage)
	l.Shot("quicklook-video-2.png", r)
	l.Printf("frames %d at %d ms, then %d at %d ms; bar at x=%d, then x=%d", n0, ms0, n1, ms1, x0, x1)
	l.Check(n1-n0 >= 20 && ms1 > ms0, "the recording plays: %d frames in a second, %d ms to %d ms", n1-n0, ms0, ms1)
	l.Check(x0 >= 0 && x1 > x0, "the frames on screen advance: the bar moved from x=%d to x=%d", x0, x1)

	focus(l, h, stage.X+stage.W/2, stage.Y+stage.H/2)
	n2, _ := v.Frames()
	time.Sleep(600 * time.Millisecond)
	n3, _ := v.Frames()
	if n3 > n2 {
		drive.Click(stage.X+stage.W/2, stage.Y+stage.H/2)
		time.Sleep(200 * time.Millisecond)
		n2, _ = v.Frames()
		time.Sleep(600 * time.Millisecond)
		n3, _ = v.Frames()
	}
	l.Check(n3 == n2, "a click on the video pauses it (%d frames, then %d)", n2, n3)
	drive.Click(stage.X+stage.W/2, stage.Y+stage.H/2)
	time.Sleep(600 * time.Millisecond)
	n4, _ := v.Frames()
	l.Check(n4 > n3, "another click plays it on, or again from the start (%d frames, then %d)", n3, n4)

	drive.Keys(drive.VKEscape)
	l.Check(drive.Until(2*time.Second, func() bool { return !drive.Visible(h) }), "Esc closes Quick Look")
}
