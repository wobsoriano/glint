package stack

import (
	"fmt"
	"math"
	"slices"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// Sizes in DIPs, from src/stack.rs.
const (
	cardW   = 224
	cardH   = 148
	gap     = 12
	edge    = 12
	pad     = 10
	headerH = 28
	tabW    = 30
	tabH    = 84
	// tabLift raises the peek tab off the bottom of the work area by this share of its height.
	tabLift = 0.18
)

// stackSize is the window's size in DIPs: the column of n cards under the header, or the peek tab.
func stackSize(n int, collapsed bool) (w, h float64) {
	if collapsed {
		return tabW, tabH
	}
	return pad*2 + cardW, pad*2 + headerH + gap + float64(n)*(cardH+gap) - gap
}

// stackRect places the window in physical pixels on the work area: against the bottom
// corner of side, edge DIPs in, or flush with the edge and lifted as a peek tab.
func stackRect(area platform.Rect, scale float64, side settings.Side, n int, collapsed bool) platform.Rect {
	w, h := stackSize(n, collapsed)
	pw, ph := int(math.Round(w*scale)), int(math.Round(h*scale))
	inset := int(math.Round(edge * scale))
	if collapsed {
		inset = 0
	}
	x := area.X + inset
	if side == settings.SideRight {
		x = area.X + area.W - pw - inset
	}
	y := area.Y + area.H - ph - inset
	if collapsed {
		y = area.Y + area.H - ph - int(float64(area.H)*tabLift)
	}
	return platform.Rect{X: x, Y: y, W: pw, H: ph}
}

// capacity is how many cards fit in a work area areaH DIPs tall; older cards beyond it go.
func capacity(areaH float64) int {
	return max(int((areaH-headerH-pad*2-edge*2)/(cardH+gap)), 1)
}

// pastEdge reports whether a pointer at screen x has left the work area on the stack's side,
// which turns a drag let go over nothing into a dismissal.
func pastEdge(x int, area platform.Rect, side settings.Side) bool {
	if side == settings.SideRight {
		return x >= area.X+area.W-3
	}
	return x <= area.X+2
}

var help = map[settings.CardAction]string{
	settings.CardCopy:      "Copy to the clipboard",
	settings.CardSave:      "Save to the export folder",
	settings.CardSaveAs:    "Save to a location you choose",
	settings.CardCompress:  "Save a smaller JPEG and copy it as a file",
	settings.CardPin:       "Pin above other windows",
	settings.CardQuickLook: "Open a large preview",
	settings.CardEdit:      "Open in the editor",
	settings.CardCopyText:  "Copy recognized text (on-device OCR)",
	settings.CardDelete:    "Move the capture to the trash",
	settings.CardDismiss:   "Close the card and keep the capture",
	settings.CardMore:      "Show the hidden actions",
}

var icons = map[settings.CardAction]theme.IconName{
	settings.CardCopy:      theme.IconCopy,
	settings.CardSave:      theme.IconSave,
	settings.CardSaveAs:    theme.IconSaveAs,
	settings.CardCompress:  theme.IconCompress,
	settings.CardPin:       theme.IconPin,
	settings.CardQuickLook: theme.IconEye,
	settings.CardEdit:      theme.IconEdit,
	settings.CardCopyText:  theme.IconScan,
	settings.CardDelete:    theme.IconTrash,
	settings.CardDismiss:   theme.IconClose,
	settings.CardMore:      theme.IconMore,
}

// imageOnly actions need the pixels of a screenshot. Quick Look is not among them: it plays
// recordings.
func imageOnly(a settings.CardAction) bool {
	switch a {
	case settings.CardCompress, settings.CardCopyText, settings.CardPin:
		return true
	}
	return false
}

// Slots is what a card shows on hover: an action or "" in each corner (indexed by
// settings.Corner), the pills in the middle, and the overflow menu behind More.
type Slots struct {
	Corners [4]settings.CardAction
	Center  []settings.CardAction
	Menu    []settings.CardAction
}

// SlotsFor fills the slots of a card from the layout in Settings > Overlay, leaving out what a
// recording cannot do.
func SlotsFor(l settings.CardLayout, recording bool) Slots {
	shown := func(a settings.CardAction) bool { return !recording || !imageOnly(a) }
	var s Slots
	for _, c := range settings.Corners {
		if a, ok := l.CornerAction(c); ok && shown(a) {
			s.Corners[c] = a
		}
	}
	for _, a := range l.Center {
		if shown(a) {
			s.Center = append(s.Center, a)
		}
	}
	for _, a := range l.Hidden {
		if a != settings.CardMore && shown(a) {
			s.Menu = append(s.Menu, a)
		}
	}
	return s
}

// Has reports whether the action shows anywhere on the card.
func (s Slots) Has(a settings.CardAction) bool {
	return slices.Contains(s.Corners[:], a) || slices.Contains(s.Center, a) || slices.Contains(s.Menu, a)
}

// countLabel names the stack in its header: "2 Screenshots", "1 Recording", "3 Captures".
func countLabel(recordings, n int) string {
	var noun string
	switch {
	case recordings == 0 && n == 1:
		noun = "Screenshot"
	case recordings == 0:
		noun = "Screenshots"
	case recordings == n && n == 1:
		noun = "Recording"
	case recordings == n:
		noun = "Recordings"
	default:
		noun = "Captures"
	}
	return fmt.Sprintf("%d %s", n, noun)
}

// clock formats a duration as the recording bar does: mm:ss, or h:mm:ss from an hour.
func clock(ms uint64) string {
	s := ms / 1000
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%02d:%02d", s/60, s%60)
}
