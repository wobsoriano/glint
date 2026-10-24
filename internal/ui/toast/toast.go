// Package toast shows Glint's transient messages: a pill near the bottom centre of the work
// area of the display with the pointer. It shows without taking the keyboard, and fades out.
// One toast shows at a time; a new one replaces it.
//
// It is a flyout of a hidden owner window: a plain transparent window keeps a redirection
// bitmap and shows black around the pill (docs/go-port/mygo-findings.md, section 8).
package toast

import (
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/ui/theme"
)

const (
	// Life is how long a toast shows before it fades.
	Life   = 2600 * time.Millisecond
	fade   = 300 * time.Millisecond
	height = 50
	// bottom is the usual gap between the pill and the bottom of the work area, in DIPs.
	bottom = 24
)

// Toast is a message showing. Its methods run on the main thread.
type Toast struct {
	win *mygo.Window
	msg string
	// bottom is the gap below the pill, in DIPs.
	bottom float64
	gen    int // bumped by every SetText and Close, so a superseded fade stops
	gone   bool
}

var (
	owner   *mygo.Window
	current *Toast
)

// Show replaces the toast showing, if any, with msg for Life.
func Show(msg string) *Toast { return ShowFor(msg, Life) }

// ShowFor shows msg for d before it fades; a negative d keeps it until Close.
func ShowFor(msg string, d time.Duration) *Toast { return show(msg, d, bottom) }

// ShowAbove shows msg for Life with gap DIPs below it, to clear a bar at the bottom of the
// work area.
func ShowAbove(msg string, gap float64) *Toast { return show(msg, Life, gap) }

func show(msg string, d time.Duration, gap float64) *Toast {
	if current != nil {
		current.Close()
	}
	if owner == nil || owner.IsDestroyed() {
		owner = mygo.NewWindow(mygo.WindowOptions{Title: "Glint toasts", Width: 1, Height: 1, Frameless: true, SkipTaskbar: true, Hidden: true})
	}
	t := &Toast{msg: msg, bottom: gap}
	w := width(msg)
	t.win = mygo.NewFlyout(mygo.FlyoutOptions{Parent: owner, Width: w, Height: height, Hidden: true, Content: ui.View(t.view)})
	t.place(w)
	t.win.ShowInactive()
	current = t
	t.expire(d)
	return t
}

// width follows the Rust build: about 7.2 DIPs a character plus the icon and padding.
func width(msg string) int {
	return int(min(max(float32(utf8.RuneCountInString(msg))*7.2+72, 200), 620))
}

func (t *Toast) view(c *ui.Context) {
	p := theme.Apply(c)
	// The inset is the root's padding: a filling row with a margin overflows the window.
	c.Root().Background(ui.Transparent).Padding(6)
	ui.Row(c).Fill().Padding(0, 16).Gap(8).AlignItems(ui.Center).Radius(theme.RadiusPill).
		Background(p.Flyout).Border(1, p.Border).Shadow(0, 2, 6, 0, ui.RGBA(0, 0, 0, 0.25)).Children(func() {
		ui.Icon(c, theme.Icon(theme.IconSparkles)).Size(15, 15).TextColor(p.Accent)
		ui.Text(c, t.msg).SingleLine().Shrink(1).TextColor(p.Text)
	})
}

// SetText changes the message and starts its time again, for a toast that counts down.
func (t *Toast) SetText(msg string, d time.Duration) {
	if t.gone {
		return
	}
	t.msg = msg
	t.win.SetOpacity(1)
	t.place(width(msg))
	t.win.Invalidate()
	t.expire(d)
}

func (t *Toast) expire(d time.Duration) {
	t.gen++
	if d < 0 {
		return
	}
	gen := t.gen
	go func() {
		time.Sleep(d)
		start := time.Now()
		for {
			var stop bool
			mygo.RunOnMain(func() {
				stop = t.gen != gen || t.gone
				if stop {
					return
				}
				left := 1 - float64(time.Since(start))/float64(fade)
				if left <= 0 {
					t.Close()
					stop = true
					return
				}
				t.win.SetOpacity(left)
			})
			if stop {
				return
			}
			time.Sleep(8 * time.Millisecond)
		}
	}()
}

func (t *Toast) Close() {
	if t.gone {
		return
	}
	t.gone = true
	t.gen++
	t.win.Destroy()
	if current == t {
		current = nil
	}
}
