package settings

import (
	"reflect"
	"testing"
)

func corners(a, b, c, d CardAction) CornerSlots {
	slot := func(x CardAction) *CardAction {
		if x == "" {
			return nil
		}
		return ptr(x)
	}
	return CornerSlots{slot(a), slot(b), slot(c), slot(d)}
}

func eq[T any](t *testing.T, what string, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", what, got, want)
	}
}

func TestDroppingOnAnOccupiedCornerSwapsWithTheOrigin(t *testing.T) {
	l := DefaultCardLayout()
	l.Place(CardSave, CornerZone(TopLeft), -1)
	eq(t, "corners", l.Corners, corners(CardSave, CardEdit, CardPin, CardMore))
	eq(t, "center", l.Center, []CardAction{CardCopy, CardDismiss})

	l.Place(CardSave, CornerZone(BottomRight), -1)
	eq(t, "corners", l.Corners, corners(CardMore, CardEdit, CardPin, CardSave))
}

func TestCenterKeepsOrderAndCapacity(t *testing.T) {
	l := DefaultCardLayout()
	l.Place(CardCopyText, CenterZone, 0)
	l.Place(CardQuickLook, CenterZone, 1)
	eq(t, "center", l.Center, []CardAction{CardCopyText, CardQuickLook, CardCopy})
	eq(t, "hidden", l.Hidden, []CardAction{CardSaveAs, CardCompress, CardDelete, CardSave})

	l.Place(CardCopy, CenterZone, 0)
	eq(t, "center", l.Center, []CardAction{CardCopy, CardCopyText, CardQuickLook})

	l.Place(CardCopy, TrayZone, -1)
	eq(t, "center", l.Center, []CardAction{CardCopyText, CardQuickLook})
	eq(t, "last hidden", l.Hidden[len(l.Hidden)-1], CardCopy)
}

func TestNormalizedRepairsDuplicatesAndMissingActions(t *testing.T) {
	broken := CardLayout{
		Corners: corners(CardCopy, CardCopy, "", ""),
		Center:  []CardAction{CardSave, CardSave, CardPin, CardEdit, CardDelete},
	}
	n := broken.Normalized()
	eq(t, "corners", n.Corners, corners(CardCopy, "", "", ""))
	eq(t, "center", n.Center, []CardAction{CardSave, CardPin, CardEdit})
	eq(t, "hidden", n.Hidden, []CardAction{CardDelete, CardSaveAs, CardCompress, CardQuickLook, CardCopyText, CardDismiss, CardMore})
}

func TestZoneOf(t *testing.T) {
	l := DefaultCardLayout()
	if z, ok := l.ZoneOf(CardPin); !ok || z != CornerZone(BottomLeft) {
		t.Errorf("pin is in %v", z)
	}
	if z, ok := l.ZoneOf(CardSave); !ok || z != CenterZone {
		t.Errorf("save is in %v", z)
	}
	if z, ok := l.ZoneOf(CardDelete); !ok || z != TrayZone {
		t.Errorf("delete is in %v", z)
	}
	l.Hidden = nil
	if _, ok := l.ZoneOf(CardDelete); ok {
		t.Error("an action the layout lacks has a zone")
	}
}

func TestHotkeyBindingsCoverEveryAction(t *testing.T) {
	h := Default().Hotkeys
	seen := map[*string]bool{}
	for _, a := range HotkeyActions {
		b := h.Binding(a)
		if seen[b] || *b == "" || a.Label() == "" {
			t.Errorf("%v: binding %q label %q", a, *b, a.Label())
		}
		seen[b] = true
	}
	if *h.Binding(HotkeyLibrary) != "Ctrl+Shift+L" || HotkeyText.Label() != "Capture text" {
		t.Error("bindings do not match their actions")
	}
}
