package main

import (
	"flag"
	"image"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

var fps = flag.Int("fps", 60, "video mode: frames a second to push")

// runVideo pushes generated 1920×1080 frames into one Bitmap with Bitmap.Update from a decoder
// goroutine, as a video preview would, and counts what reaches the screen.
func runVideo() {
	mygo.App.WhenReady(func() {
		const w, h = 1920, 1080
		ring := make([]*image.RGBA, 4)
		for i := range ring {
			ring[i] = image.NewRGBA(image.Rect(0, 0, w, h))
		}
		paint := func(img *image.RGBA, n int) {
			for y := 0; y < h; y++ {
				row := img.Pix[y*img.Stride : y*img.Stride+w*4]
				for x := 0; x < w; x++ {
					v := uint8((x + n*8) >> 2)
					row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = v, uint8(y>>2), uint8(n*4), 255
				}
			}
		}
		paint(ring[0], 0)
		bmp := ui.NewBitmap(ring[0])
		var draws, updates atomic.Int64
		var lastDraw atomic.Int64
		view := func(c *ui.Context) {
			c.Root().Background(ui.RGB(0, 0, 0))
			ui.Box(c).Fill().Draw(func(p *ui.Painter, r ui.Rect) {
				draws.Add(1)
				lastDraw.Store(time.Now().UnixNano())
				p.Image(bmp, r, ui.Contain)
			})
		}
		win := mygo.NewWindow(mygo.WindowOptions{Title: "Glint video spike", Width: 960, Height: 540, X: 200, Y: 120, Content: ui.View(view)})
		frames := &frameLog{}
		if *auto {
			go func() {
				failures := 0
				defer finish(&failures)
				time.Sleep(700 * time.Millisecond)
				var gen []float64
				period := time.Second / time.Duration(*fps)
				d0, t0 := draws.Load(), time.Now()
				next := t0
				for n := 1; time.Since(t0) < 3*time.Second; n++ {
					img := ring[n%len(ring)]
					g := time.Now()
					paint(img, n)
					gen = append(gen, ms(time.Since(g)))
					win.Update(func() { bmp.Update(img); updates.Add(1) })
					next = next.Add(period)
					time.Sleep(time.Until(next))
				}
				elapsed := time.Since(t0).Seconds()
				shown := float64(draws.Load()-d0) / elapsed
				logf("video %d fps target: %d updates, %.1f Draw calls a second over %.1f s; generating a frame (the decoder stand-in) %s ms", *fps, updates.Load(), shown, elapsed, stats(gen))
				check(&failures, shown >= float64(*fps)*0.95, "a 1920x1080 Bitmap updated %d times a second draws %.1f frames a second", *fps, shown)
				_ = frames
			}()
		}
	})
}
