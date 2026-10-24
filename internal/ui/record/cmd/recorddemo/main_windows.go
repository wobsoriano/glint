// Command recorddemo runs Glint's recording UI on its own, with the Record hotkey
// (Ctrl+Shift+7) toggling it and recordings going to a Library under GLINT_DATA_DIR (a new
// temp dir when unset). With -auto it drives the UI on the real desktop, checks each step,
// decodes what it recorded and writes screenshots to -out, then quits with 1 if a check
// failed. Run it inside the desktop lock:
//
//	go build -o %TEMP%\recorddemo.exe ./internal/ui/record/cmd/recorddemo
//	go run ./tools/desklock -- %TEMP%\recorddemo.exe -auto
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/record"
)

var (
	auto    = flag.Bool("auto", false, "drive the recording UI, check it and quit")
	animate = flag.Bool("animate", false, "run as the animated app that gets recorded")
	outDir  = flag.String("out", filepath.Join(os.TempDir(), "glint-recorddemo-evidence"), "where screenshots go")
)

// host is the app around the recording UI: settings in GLINT_DATA_DIR, the Library there,
// Glint's own area selector, and a log of what the app would show.
type host struct {
	s   settings.Settings
	lib store.Library

	mu       sync.Mutex
	toasts   []string
	finished []store.Item
}

func (h *host) Settings() *settings.Settings { return &h.s }
func (h *host) SettingsChanged() {
	if err := h.s.Save(); err != nil {
		log.Printf("save settings: %v", err)
	}
}
func (h *host) Library() store.Library { return h.lib }
func (h *host) SelectRegion(kind settings.RecordTarget, d platform.Display, done func(record.Selection, bool)) {
	record.Select(kind, d, done)
}
func (h *host) RecordingFinished(item store.Item, warnings []string) {
	log.Printf("recording finished: %s %dx%d %d ms, warnings %v", item.Dir, item.Meta.Width, item.Meta.Height, *item.Meta.DurationMs, warnings)
	h.mu.Lock()
	h.finished = append(h.finished, item)
	h.mu.Unlock()
}
func (h *host) Toast(msg string) {
	log.Printf("toast: %s", msg)
	h.mu.Lock()
	h.toasts = append(h.toasts, msg)
	h.mu.Unlock()
}

func (h *host) lastToast() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.toasts) == 0 {
		return ""
	}
	return h.toasts[len(h.toasts)-1]
}

func (h *host) finishedCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.finished)
}

func (h *host) lastFinished() store.Item {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.finished[len(h.finished)-1]
}

func main() {
	flag.Parse()
	if *animate {
		runAnimate()
	} else {
		if os.Getenv("GLINT_DATA_DIR") == "" {
			dir, err := os.MkdirTemp("", "glint-recorddemo-")
			if err != nil {
				log.Fatal(err)
			}
			os.Setenv("GLINT_DATA_DIR", dir)
		}
		h := &host{s: settings.Load(), lib: store.New(store.DefaultRoot())}
		log.Printf("data dir %s", settings.DataDir())
		ctl := record.New(h)
		mygo.App.OnWindowAllClosed(func() {})
		mygo.App.WhenReady(func() {
			if err := mygo.GlobalShortcut.Register(h.s.Hotkeys.Recording, ctl.Toggle); err != nil {
				log.Printf("register %s: %v", h.s.Hotkeys.Recording, err)
			}
			if *auto {
				go run(h, ctl)
				return
			}
			ctl.Toggle()
		})
	}
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// animTitle is the animated app's title; it ends with the clicks it took.
const animTitle = "Glint demo animation"

var animGreen = ui.RGB(0x20, 0xC0, 0x40)

// runAnimate is a green window with a magenta block sliding across it, which counts the clicks
// it takes in its title.
func runAnimate() {
	mygo.App.WhenReady(func() {
		clicks := 0
		start := time.Now()
		var win *mygo.Window
		win = mygo.NewWindow(mygo.WindowOptions{Title: animTitle + " 0", X: 100, Y: 60, Width: 1720, Height: 960, Content: ui.View(func(c *ui.Context) {
			box := ui.Box(c).Fill().Background(animGreen)
			if box.Clicked() {
				clicks++
				win.SetTitle(fmt.Sprintf("%s %d", animTitle, clicks))
			}
			box.Draw(func(p *ui.Painter, r ui.Rect) {
				t := float32(time.Since(start).Seconds())
				x := r.X + float32(int(t*400)%int(r.W-200))
				p.Fill(ui.Rect{X: x, Y: r.Y + 120, W: 200, H: 120}, ui.RGB(0xE0, 0x20, 0xC0), 0)
				p.Text(r.X+20, r.Y+20, fmt.Sprintf("%.2f s", t), 28, ui.RGB(0, 0, 0))
				p.AnimationFrame()
			})
		})})
		win.Focus()
	})
}
