package settings

import (
	"encoding/json"
	"slices"
)

// CardAction is one button of the capture card that floats after a shot.
type CardAction string

const (
	CardCopy      CardAction = "copy"
	CardSave      CardAction = "save"
	CardSaveAs    CardAction = "save_as"
	CardCompress  CardAction = "compress"
	CardPin       CardAction = "pin"
	CardQuickLook CardAction = "quick_look"
	CardEdit      CardAction = "edit"
	CardCopyText  CardAction = "copy_text"
	CardDelete    CardAction = "delete"
	CardDismiss   CardAction = "dismiss"
	CardMore      CardAction = "more"
)

// CardActions lists every action in the order Normalized appends missing ones.
var CardActions = []CardAction{
	CardCopy, CardSave, CardSaveAs, CardCompress, CardPin, CardQuickLook,
	CardEdit, CardCopyText, CardDelete, CardDismiss, CardMore,
}

func (a *CardAction) UnmarshalJSON(b []byte) (err error) {
	*a, err = parseEnum(b, CardActions)
	return err
}

func (a CardAction) Label() string {
	return map[CardAction]string{
		CardCopy: "Copy", CardSave: "Save", CardSaveAs: "Save As", CardCompress: "Compress",
		CardPin: "Pin", CardQuickLook: "Quick Look", CardEdit: "Edit", CardCopyText: "Copy Text",
		CardDelete: "Delete", CardDismiss: "Dismiss", CardMore: "More",
	}[a]
}

// Corner indexes CardLayout.Corners.
type Corner int

const (
	TopLeft Corner = iota
	TopRight
	BottomLeft
	BottomRight
)

var Corners = []Corner{TopLeft, TopRight, BottomLeft, BottomRight}

// Zone is where an action sits on the card: one of the corners, the center row, or the
// tray behind More.
type Zone struct {
	kind   zoneKind
	corner Corner
}

type zoneKind int

const (
	zoneTray zoneKind = iota
	zoneCenter
	zoneCorner
)

func CornerZone(c Corner) Zone { return Zone{kind: zoneCorner, corner: c} }

var (
	CenterZone = Zone{kind: zoneCenter}
	TrayZone   = Zone{}
)

// Corner reports the corner of a corner zone.
func (z Zone) Corner() (Corner, bool) { return z.corner, z.kind == zoneCorner }

// CornerSlots holds the action in each corner, nil for an empty corner. It is a JSON array
// of exactly four strings or nulls.
type CornerSlots [4]*CardAction

func (s *CornerSlots) UnmarshalJSON(b []byte) error {
	xs, err := fixedArray[*CardAction](b, 4)
	if err != nil {
		return err
	}
	*s = CornerSlots(xs)
	return nil
}

// CardLayout says which action goes where on the capture card.
type CardLayout struct {
	Corners CornerSlots  `json:"corners"`
	Center  []CardAction `json:"center"`
	Hidden  []CardAction `json:"hidden"`
}

// CenterCapacity is how many actions fit in the center row.
const CenterCapacity = 3

func DefaultCardLayout() CardLayout {
	return CardLayout{
		Corners: CornerSlots{ptr(CardDismiss), ptr(CardEdit), ptr(CardPin), ptr(CardMore)},
		Center:  []CardAction{CardCopy, CardSave},
		Hidden:  []CardAction{CardSaveAs, CardCompress, CardQuickLook, CardCopyText, CardDelete},
	}
}

func ptr[T any](v T) *T { return &v }

func (l CardLayout) MarshalJSON() ([]byte, error) {
	type plain CardLayout
	p := plain(l)
	if p.Center == nil {
		p.Center = []CardAction{}
	}
	if p.Hidden == nil {
		p.Hidden = []CardAction{}
	}
	return json.Marshal(p)
}

func (l CardLayout) CornerAction(c Corner) (CardAction, bool) {
	if a := l.Corners[c]; a != nil {
		return *a, true
	}
	return "", false
}

// ZoneOf finds where a sits, false when the layout does not hold it.
func (l CardLayout) ZoneOf(a CardAction) (Zone, bool) {
	for _, c := range Corners {
		if got, ok := l.CornerAction(c); ok && got == a {
			return CornerZone(c), true
		}
	}
	if slices.Contains(l.Center, a) {
		return CenterZone, true
	}
	if slices.Contains(l.Hidden, a) {
		return TrayZone, true
	}
	return TrayZone, false
}

func (l *CardLayout) remove(a CardAction) {
	for i, slot := range l.Corners {
		if slot != nil && *slot == a {
			l.Corners[i] = nil
		}
	}
	l.Center = slices.DeleteFunc(l.Center, func(x CardAction) bool { return x == a })
	l.Hidden = slices.DeleteFunc(l.Hidden, func(x CardAction) bool { return x == a })
}

// Place moves a into zone. Dropping on an occupied corner sends the occupant to where a came
// from. In the center, centerIndex picks the slot (negative appends), and an overflowing center
// row pushes one action into the tray.
func (l *CardLayout) Place(a CardAction, zone Zone, centerIndex int) {
	origin, _ := l.ZoneOf(a)
	if origin == zone && zone != CenterZone {
		return
	}
	originIndex := slices.Index(l.Center, a)
	l.Center = slices.Clone(l.Center)
	l.Hidden = slices.Clone(l.Hidden)
	l.remove(a)
	clampIndex := func(i int) int {
		if i < 0 || i > len(l.Center) {
			return len(l.Center)
		}
		return i
	}
	switch zone.kind {
	case zoneCorner:
		if occupant := l.Corners[zone.corner]; occupant != nil {
			switch origin.kind {
			case zoneCorner:
				l.Corners[origin.corner] = occupant
			case zoneCenter:
				l.Center = slices.Insert(l.Center, clampIndex(originIndex), *occupant)
			case zoneTray:
				l.Hidden = append(l.Hidden, *occupant)
			}
		}
		l.Corners[zone.corner] = ptr(a)
	case zoneCenter:
		i := clampIndex(centerIndex)
		l.Center = slices.Insert(l.Center, i, a)
		if len(l.Center) > CenterCapacity {
			last := len(l.Center) - 1
			drop := last
			if i == last {
				drop = last - 1
			}
			overflow := l.Center[drop]
			l.Center = slices.Delete(l.Center, drop, drop+1)
			l.Hidden = append(l.Hidden, overflow)
		}
	case zoneTray:
		l.Hidden = append(l.Hidden, a)
	}
}

// Normalized repairs a hand-edited or older layout: each action appears once, the center row
// fits, and actions the file never mentioned land in the tray.
func (l CardLayout) Normalized() CardLayout {
	seen := map[CardAction]bool{}
	claim := func(a CardAction) bool {
		if seen[a] {
			return false
		}
		seen[a] = true
		return true
	}
	var out CardLayout
	for i, slot := range l.Corners {
		if slot != nil && claim(*slot) {
			out.Corners[i] = ptr(*slot)
		}
	}
	out.Center = []CardAction{}
	for _, a := range l.Center {
		if claim(a) {
			out.Center = append(out.Center, a)
		}
	}
	out.Hidden = []CardAction{}
	for _, a := range l.Hidden {
		if claim(a) {
			out.Hidden = append(out.Hidden, a)
		}
	}
	if len(out.Center) > CenterCapacity {
		out.Hidden = append(out.Hidden, out.Center[CenterCapacity:]...)
		out.Center = out.Center[:CenterCapacity]
	}
	for _, a := range CardActions {
		if claim(a) {
			out.Hidden = append(out.Hidden, a)
		}
	}
	return out
}
