// Package pin floats a screenshot above every other window. Drag it to move it, scroll to
// zoom, Ctrl+scroll to fade it; a double click, Esc or its close button closes it, and Ctrl+C
// copies the image.
//
// A pin is a MyGo panel: a topmost tool window that stays above the taskbar (a plain topmost
// frameless window falls under it as it activates) and takes the keyboard when shown or
// clicked, which Esc and Ctrl+C need. A flyout would never take the keyboard, or would close
// at the first click elsewhere.
package pin

import (
	"bytes"
	"image"
	"image/png"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// Pin is one pinned screenshot. Its methods are safe from any goroutine.
type Pin struct {
	win     *mygo.Window
	img     image.Image
	bmp     *ui.Bitmap
	aspect  float64
	scale   float64 // the display's physical pixels per DIP
	opacity float64

	// The move in progress: where the pointer and the window were as it was pressed.
	moving    bool
	pressAt   [2]int
	pressRect platform.Rect

	// Copied is called after Ctrl+C with the clipboard's error, for a toast or a test.
	Copied func(error)
}

// Open pins img in the middle of the display under the pointer. captureScale is the pixels
// per DIP of the display it was captured on, so it shows at the size it had there, shrunk to
// fit 60% of the work area.
func Open(img image.Image, captureScale float64) *Pin {
	area, scale := workAreaAtCursor()
	b := img.Bounds()
	r := initialRect(area, b.Dx(), b.Dy(), scale/max(captureScale, 0.1))
	p := &Pin{img: img, bmp: ui.NewBitmap(img), aspect: float64(b.Dx()) / float64(max(b.Dy(), 1)), scale: scale, opacity: 1}
	mygo.RunOnMain(func() {
		p.win = mygo.NewWindow(mygo.WindowOptions{
			Title: "Pinned screenshot",
			Width: int(math.Round(float64(r.W) / scale)), Height: int(math.Round(float64(r.H) / scale)),
			Frameless: true, Panel: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true,
			DisableResize: true, DisableMinimize: true, DisableMaximize: true, DisableFullScreen: true,
			Content: ui.View(p.view),
		})
		setBounds(p.win, r)
		p.win.Show()
		p.win.Focus()
	})
	return p
}

// Window is the pin's window.
func (p *Pin) Window() *mygo.Window { return p.win }

// Close unpins it.
func (p *Pin) Close() { p.win.Close() }

// Opacity is the pin's opacity, 0.2 to 1.
func (p *Pin) Opacity() (o float64) {
	mygo.RunOnMain(func() { o = p.opacity })
	return o
}

var closeRed = ui.RGBA(0xFF, 0x3B, 0x30, 0.9)

func (p *Pin) view(c *ui.Context) {
	pal := theme.Apply(c)
	c.Root().Background(ui.RGB(0, 0, 0))
	if c.Shortcut(0, ui.KeyEscape) {
		p.win.Close()
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyC) {
		go p.copy()
	}
	root := ui.Box(c).Fill()
	hovered := root.Hovered()
	root.Children(func() {
		ui.Box(c).Fill().Border(1, pal.Accent.Alpha(0.6)).HandleInput(p.input).Children(func() {
			ui.Image(c, p.bmp).Fill().Fit(ui.FillBox)
		})
		if !hovered {
			return
		}
		b := ui.ButtonBase(c.Key("close")).Absolute().Top(6).Right(6).Size(22, 22).Radius(11).Center().
			Background(ui.RGBA(0, 0, 0, 0.7)).Tooltip("Close")
		if b.Hovered() {
			b.Background(closeRed)
		}
		b.Children(func() { ui.Icon(c, theme.Icon(theme.IconClose)).Size(12, 12).TextColor(ui.RGB(0xFF, 0xFF, 0xFF)) })
		if b.Clicked() {
			p.win.Close()
		}
	})
}

func (p *Pin) input(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if ev.Clicks >= 2 {
			p.moving = false
			p.win.Close()
			return true
		}
		x, y := cursor()
		p.moving, p.pressAt, p.pressRect = true, [2]int{x, y}, bounds(p.win)
		return true
	case ui.InputPointerMove:
		if !p.moving || ev.Button != 0 {
			return false
		}
		x, y := cursor()
		r := p.pressRect
		r.X += x - p.pressAt[0]
		r.Y += y - p.pressAt[1]
		setBounds(p.win, r)
		return true
	case ui.InputPointerUp:
		p.moving = false
		return true
	case ui.InputScroll:
		n := notches(ev.DY, ev.Precise)
		if ev.Mods&ui.Ctrl != 0 {
			p.opacity = nextOpacity(p.opacity, n)
			p.win.SetOpacity(p.opacity)
			return true
		}
		p.zoom(math.Pow(zoomStep, n))
		return true
	case ui.InputZoom:
		p.zoom(float64(ev.Scale))
		return true
	}
	return false
}

func (p *Pin) zoom(k float64) {
	if k == 1 {
		return
	}
	x, y := cursor()
	lo, hi := int(minW*p.scale), int(maxW*p.scale)
	setBounds(p.win, zoomRect(bounds(p.win), x, y, k, p.aspect, lo, hi))
}

func (p *Pin) copy() {
	var buf bytes.Buffer
	err := png.Encode(&buf, p.img)
	if err == nil {
		err = mygo.Clipboard.WriteImage(buf.Bytes())
	}
	if p.Copied != nil {
		p.Copied(err)
	}
}
