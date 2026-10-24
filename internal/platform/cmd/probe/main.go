// Command probe exercises internal/platform on the real desktop: it lists the displays and
// windows, captures the virtual desktop, each display and one window with and without its
// shadow to PNGs, and prints how long each capture took.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/wobsoriano/glint/internal/platform"
)

func main() {
	out := flag.String("out", "", "directory for the PNGs (default: a new temp dir)")
	hwnd := flag.Int64("window", 0, "window to capture (default: the frontmost window of another app)")
	runs := flag.Int("runs", 10, "captures per timing")
	flag.Parse()
	if *out == "" {
		dir, err := os.MkdirTemp("", "glint-probe-")
		if err != nil {
			log.Fatal(err)
		}
		*out = dir
	} else if err := os.MkdirAll(*out, 0o755); err != nil {
		log.Fatal(err)
	}

	ds, err := platform.Displays()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("displays:")
	var virt platform.Rect
	for i, d := range ds {
		fmt.Printf("  %s id=%#x bounds=%+v scale=%.2f primary=%v\n", d.Name, d.ID, d.Bounds, d.Scale, d.Primary)
		if i == 0 {
			virt = d.Bounds
		} else {
			virt = union(virt, d.Bounds)
		}
	}
	for _, line := range advancedColour() {
		fmt.Println("  " + line)
	}
	x, y := platform.CursorPos()
	fmt.Printf("cursor: (%d, %d)\n", x, y)
	if p, err := platform.Wallpaper(); err != nil {
		fmt.Printf("wallpaper: %v\n", err)
	} else {
		fmt.Printf("wallpaper: %s\n", p)
	}

	start := time.Now()
	ws, err := platform.Windows()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("windows (%d, listed in %v), front to back:\n", len(ws), time.Since(start).Round(time.Microsecond))
	for _, w := range ws {
		fmt.Printf("  %#x pid=%d app=%q title=%q bounds=%+v\n", w.ID, w.PID, w.App, w.Title, w.Bounds)
	}

	timed(fmt.Sprintf("virtual desktop %dx%d", virt.W, virt.H), *runs, filepath.Join(*out, "desktop.png"), func() (*image.RGBA, error) {
		return platform.CaptureRect(virt)
	})
	small := platform.Rect{X: ds[0].Bounds.X, Y: ds[0].Bounds.Y, W: 64, H: 64}
	timed("64x64 corner of Display 1", *runs, filepath.Join(*out, "small.png"), func() (*image.RGBA, error) {
		return platform.CaptureRect(small)
	})
	for i, d := range ds {
		timed(fmt.Sprintf("%s %dx%d", d.Name, d.Bounds.W, d.Bounds.H), *runs, filepath.Join(*out, fmt.Sprintf("display%d.png", i+1)), func() (*image.RGBA, error) {
			return platform.CaptureRect(d.Bounds)
		})
	}

	if *hwnd == 0 {
		i := slices.IndexFunc(ws, func(w platform.Window) bool { return w.PID != os.Getpid() })
		if i < 0 {
			log.Fatal("no window to capture")
		}
		*hwnd = ws[i].ID
	}
	fmt.Printf("window %#x:\n", *hwnd)
	timed("  without shadow", *runs, filepath.Join(*out, "window.png"), func() (*image.RGBA, error) {
		return platform.CaptureWindow(*hwnd, false)
	})
	timed("  with shadow", *runs, filepath.Join(*out, "window-shadow.png"), func() (*image.RGBA, error) {
		return platform.CaptureWindow(*hwnd, true)
	})
	fmt.Printf("PNGs in %s\n", *out)
}

func timed(label string, runs int, path string, capture func() (*image.RGBA, error)) {
	var took []time.Duration
	var img *image.RGBA
	for range max(runs, 1) {
		start := time.Now()
		var err error
		img, err = capture()
		took = append(took, time.Since(start))
		if err != nil {
			fmt.Printf("%s: %v\n", label, err)
			return
		}
	}
	slices.Sort(took)
	fmt.Printf("%s: %v, min %v median %v max %v over %d runs\n", label, img.Bounds().Size(),
		took[0].Round(time.Microsecond), took[len(took)/2].Round(time.Microsecond), took[len(took)-1].Round(time.Microsecond), len(took))
	f, err := os.Create(path)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		log.Fatal(err)
	}
}

func union(a, b platform.Rect) platform.Rect {
	x0, y0 := min(a.X, b.X), min(a.Y, b.Y)
	x1, y1 := max(a.X+a.W, b.X+b.W), max(a.Y+a.H, b.Y+b.H)
	return platform.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
}
