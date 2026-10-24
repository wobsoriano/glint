package record

import (
	"math"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
)

// chromeKind is how a floating surface treats the pointer and screen capture.
type chromeKind int

const (
	// passive takes clicks without taking the keyboard.
	passive chromeKind = iota
	// duringRecording is passive and left out of the recording.
	duringRecording
	// clickThrough lets every click through to what is behind, and is left out of the recording.
	clickThrough
)

// capturable lets screenshots and recordings see the recording chrome, for evidence of how
// it looks: set GLINT_CAPTURE_CHROME.
func capturable() bool { return os.Getenv("GLINT_CAPTURE_CHROME") != "" }

// dips is a physical length in whole DIPs, rounded up so MyGo never makes the window smaller
// than place then sizes it.
func dips(px int, scale float64) int { return int(math.Ceil(float64(px) / scale)) }

// openChrome shows a flyout of the hidden owner on r. A flyout has no redirection bitmap, so
// what the view leaves transparent shows what is behind it; a plain transparent window
// would show black there. Main thread only.
func (c *Controller) openChrome(kind chromeKind, r platform.Rect, scale float64, view func(*ui.Context)) *mygo.Window {
	if c.owner == nil {
		c.owner = mygo.NewWindow(mygo.WindowOptions{Title: "Glint recording", Width: 1, Height: 1, Frameless: true, SkipTaskbar: true, Hidden: true})
	}
	w := mygo.NewFlyout(mygo.FlyoutOptions{Parent: c.owner, Width: dips(r.W, scale), Height: dips(r.H, scale), Hidden: true, Content: ui.View(view)})
	if kind == clickThrough {
		w.SetIgnoreMouseEvents(true)
	}
	if kind != passive && !capturable() {
		w.SetContentProtection(true)
	}
	place(w, r)
	w.ShowInactive()
	return w
}

// scaled is a length in DIPs as physical pixels of a display.
func scaled(v float64, scale float64) int { return int(math.Round(v * scale)) }

// bottomCentre is a w×h DIP box centred near the bottom of the display's work area, margin
// DIPs above its edge.
func bottomCentre(d platform.Display, w, h, margin float64) platform.Rect {
	a := workArea(d)
	pw, ph := scaled(w, d.Scale), scaled(h, d.Scale)
	return platform.Rect{X: a.X + (a.W-pw)/2, Y: a.Y + a.H - ph - scaled(margin, d.Scale), W: pw, H: ph}
}
