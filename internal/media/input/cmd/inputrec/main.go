// Command inputrec runs the live input hooks for a few seconds, writes input.json and the
// cursor sprites into a session directory, then reloads and checks what it wrote. It never
// moves the pointer or types; whatever the person at the desktop does is recorded.
//
//	go run ./internal/media/input/cmd/inputrec -seconds 5
package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/media/input"
)

func main() {
	seconds := flag.Float64("seconds", 5, "how long the hooks run")
	out := flag.String("out", "", "session directory (default: a new temp dir)")
	width := flag.Uint("width", 1920, "screen master width written to input.json")
	height := flag.Uint("height", 1080, "screen master height written to input.json")
	scale := flag.Float64("scale", 1, "display scale recorded with each cursor sprite")
	flag.Parse()

	dir := *out
	if dir == "" {
		var err error
		if dir, err = os.MkdirTemp("", "glint-input-"); err != nil {
			log.Fatal(err)
		}
	}
	clock := capture.NewClock()
	track, err := input.Start(input.Options{
		Map:         input.FixedOrigin(0, 0),
		Size:        [2]uint32{uint32(*width), uint32(*height)},
		Scale:       float32(*scale),
		SessionDir:  dir,
		Clock:       clock,
		IgnoredKeys: []string{"Ctrl+Shift+7"},
	})
	if err != nil {
		log.Fatal(err)
	}
	clock.Resume()
	time.Sleep(time.Duration(*seconds * float64(time.Second)))
	clock.Pause()
	tl, err := track.Stop()
	if err != nil {
		log.Fatal(err)
	}
	if err := tl.Save(dir); err != nil {
		log.Fatal(err)
	}

	back, err := input.Load(dir)
	if err != nil {
		log.Fatalf("input.json does not load: %v", err)
	}
	fmt.Printf("input.json: %d pointer samples, %d clicks, %d keys, %d cursors, media %d ms\n",
		len(back.Pointer), len(back.Clicks), len(back.Keys), len(back.Cursors), clock.NowMS())
	ok := len(back.Cursors) > 0
	for _, c := range back.Cursors {
		f, err := os.Open(filepath.Join(dir, c.File))
		if err != nil {
			log.Fatal(err)
		}
		img, err := png.Decode(f)
		f.Close()
		if err != nil {
			fmt.Printf("%s: %v\n", c.File, err)
			ok = false
			continue
		}
		opaque := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
					opaque++
				}
			}
		}
		fmt.Printf("%s: %dx%d, hotspot %v, scale %g, %d visible pixels\n", c.File, b.Dx(), b.Dy(), c.Hotspot, c.Scale, opaque)
		ok = ok && opaque > 0
	}
	for _, k := range back.Keys {
		fmt.Printf("key %d ms %s\n", k.T, k.Label)
	}
	fmt.Println("session:", dir)
	if !ok {
		fmt.Println("FAILED: no visible cursor sprite was captured")
		os.Exit(1)
	}
}
