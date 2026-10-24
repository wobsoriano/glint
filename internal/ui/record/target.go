package record

import (
	"errors"
	"fmt"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
)

// Target is what a recording captures: a display, a window, or an area of one display.
// Rects are physical pixels of the virtual desktop.
type Target struct {
	Kind    settings.RecordTarget
	Display platform.Display
	// Window is set for RecordWindow.
	Window platform.Window
	// Area is set for RecordArea: inside Display, with even sides.
	Area platform.Rect
}

// Selection is what the user picked on screen: a window, or a rectangle that is an area or
// a whole display.
type Selection struct {
	Rect   platform.Rect
	Window *platform.Window
}

// minArea is the smallest side of an area worth recording.
const minArea = 16

var errNoDisplays = errors.New("no displays")

func displayTarget(d platform.Display) Target {
	return Target{Kind: settings.RecordDisplay, Display: d}
}

func displayContaining(list []platform.Display, x, y int) (platform.Display, bool) {
	for _, d := range list {
		if d.Bounds.Contains(x, y) {
			return d, true
		}
	}
	return platform.Display{}, false
}

// displayAt is the display holding the point, or the first one.
func displayAt(list []platform.Display, x, y int) (platform.Display, error) {
	if len(list) == 0 {
		return platform.Display{}, errNoDisplays
	}
	if d, ok := displayContaining(list, x, y); ok {
		return d, nil
	}
	return list[0], nil
}

// clipArea clips r to the display, with its sides rounded down to even pixels for the H.264
// encoder. ok is false when less than minArea is left on a side.
func clipArea(r, display platform.Rect) (platform.Rect, bool) {
	c := r.Intersect(display)
	c.W, c.H = c.W&^1, c.H&^1
	return c, c.W >= minArea && c.H >= minArea
}

// areaTarget is r clipped to the display holding its centre; ok is false when nothing usable
// is left.
func areaTarget(list []platform.Display, r platform.Rect) (Target, bool) {
	d, found := displayContaining(list, r.X+r.W/2, r.Y+r.H/2)
	if !found {
		return Target{}, false
	}
	a, ok := clipArea(r, d.Bounds)
	return Target{Kind: settings.RecordArea, Display: d, Area: a}, ok
}

// fromSelection reads what the user picked: a window, a whole display, or an area.
func fromSelection(list []platform.Display, sel Selection) (Target, bool) {
	if w := sel.Window; w != nil {
		d, err := displayAt(list, w.Bounds.X+w.Bounds.W/2, w.Bounds.Y+w.Bounds.H/2)
		if err != nil {
			return Target{}, false
		}
		return Target{Kind: settings.RecordWindow, Display: d, Window: *w}, true
	}
	for _, d := range list {
		if d.Bounds == sel.Rect {
			return displayTarget(d), true
		}
	}
	return areaTarget(list, sel.Rect)
}

// Source is what the recorder captures for t.
func (t Target) Source() media.Source {
	switch t.Kind {
	case settings.RecordWindow:
		return media.WindowSource{Window: t.Window.ID}
	case settings.RecordArea:
		return media.AreaSource{Display: t.Display.ID, Rect: t.Area}
	}
	return media.DisplaySource{Display: t.Display.ID}
}

// Label is the recording's source as the Library shows it.
func (t Target) Label() string {
	switch t.Kind {
	case settings.RecordWindow:
		return t.Window.Title
	case settings.RecordArea:
		return fmt.Sprintf("%d × %d area of %s", t.Area.W, t.Area.H, t.Display.Name)
	}
	return t.Display.Name
}

// frame is where the target is on screen now: a window follows its moves.
func (t Target) frame() platform.Rect {
	switch t.Kind {
	case settings.RecordWindow:
		if r, ok := windowRect(t.Window.ID); ok {
			return r
		}
		return t.Display.Bounds
	case settings.RecordArea:
		return t.Area
	}
	return t.Display.Bounds
}

func areaPref(r platform.Rect) *settings.Area {
	return &settings.Area{int32(r.X), int32(r.Y), int32(r.W), int32(r.H)}
}

func prefArea(a settings.Area) platform.Rect {
	return platform.Rect{X: int(a[0]), Y: int(a[1]), W: int(a[2]), H: int(a[3])}
}
