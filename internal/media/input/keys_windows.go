package input

import (
	"fmt"
	"strings"
	"unicode"
)

const (
	vkBack     = 0x08
	vkTab      = 0x09
	vkReturn   = 0x0D
	vkShift    = 0x10
	vkControl  = 0x11
	vkMenu     = 0x12
	vkPause    = 0x13
	vkCapital  = 0x14
	vkEscape   = 0x1B
	vkSpace    = 0x20
	vkPrior    = 0x21
	vkNext     = 0x22
	vkEnd      = 0x23
	vkHome     = 0x24
	vkLeft     = 0x25
	vkUp       = 0x26
	vkRight    = 0x27
	vkDown     = 0x28
	vkSnapshot = 0x2C
	vkInsert   = 0x2D
	vkDelete   = 0x2E
	vkLWin     = 0x5B
	vkRWin     = 0x5C
	vkApps     = 0x5D
	vkNumpad0  = 0x60
	vkNumpad9  = 0x69
	vkF1       = 0x70
	vkF24      = 0x87
	vkNumLock  = 0x90
	vkScroll   = 0x91
	vkLShift   = 0xA0
	vkRShift   = 0xA1
	vkLControl = 0xA2
	vkRControl = 0xA3
	vkLMenu    = 0xA4
	vkRMenu    = 0xA5
)

// scSynthetic marks the Left Ctrl that Windows synthesizes ahead of Right Alt when AltGr is
// pressed; both reach a low-level hook with one timestamp. A physical Left Ctrl lacks it.
const scSynthetic = 0x200

type mods struct{ ctrl, shift, alt, win, altgr bool }

// apply updates the modifier state and reports whether vk is a modifier.
func (m *mods) apply(vk, scan uint32, down bool) bool {
	var slot *bool
	switch {
	case vk == vkLControl && scan&scSynthetic != 0:
		slot = &m.altgr
	case vk == vkRMenu && m.altgr:
		return true
	case vk == vkControl || vk == vkLControl || vk == vkRControl:
		slot = &m.ctrl
	case vk == vkShift || vk == vkLShift || vk == vkRShift:
		slot = &m.shift
	case vk == vkMenu || vk == vkLMenu || vk == vkRMenu:
		slot = &m.alt
	case vk == vkLWin || vk == vkRWin:
		slot = &m.win
	default:
		return false
	}
	*slot = down
	return true
}

// shortcutLabel names a press worth showing: a modifier combo or a special key. Plain
// typing, Shift alone and AltGr characters return "".
func shortcutLabel(vk uint32, m mods) string {
	key := keyName(vk)
	if key == "" || !(m.ctrl || m.alt || m.win || isSpecial(vk)) {
		return ""
	}
	parts := make([]string, 0, 5)
	for _, p := range []struct {
		on   bool
		name string
	}{{m.win, "Win"}, {m.ctrl, "Ctrl"}, {m.alt, "Alt"}, {m.shift, "Shift"}} {
		if p.on {
			parts = append(parts, p.name)
		}
	}
	return strings.Join(append(parts, key), "+")
}

func isSpecial(vk uint32) bool {
	switch vk {
	case vkReturn, vkEscape, vkTab, vkBack, vkDelete, vkInsert, vkHome, vkEnd, vkPrior, vkNext,
		vkLeft, vkUp, vkRight, vkDown, vkSnapshot:
		return true
	}
	return vk >= vkF1 && vk <= vkF24
}

var keyNames = map[uint32]string{
	vkReturn: "Enter", vkEscape: "Esc", vkTab: "Tab", vkBack: "Backspace", vkDelete: "Delete",
	vkInsert: "Insert", vkHome: "Home", vkEnd: "End", vkPrior: "PageUp", vkNext: "PageDown",
	vkLeft: "Left", vkUp: "Up", vkRight: "Right", vkDown: "Down", vkSpace: "Space",
	vkSnapshot: "PrintScreen",
	// Modifiers and lock keys never label a press on their own.
	vkControl: "", vkLControl: "", vkRControl: "", vkShift: "", vkLShift: "", vkRShift: "",
	vkMenu: "", vkLMenu: "", vkRMenu: "", vkLWin: "", vkRWin: "",
	vkCapital: "", vkNumLock: "", vkScroll: "", vkPause: "", vkApps: "",
}

func keyName(vk uint32) string {
	if name, ok := keyNames[vk]; ok {
		return name
	}
	switch {
	case vk >= vkF1 && vk <= vkF24:
		return fmt.Sprintf("F%d", vk-vkF1+1)
	case vk >= '0' && vk <= '9', vk >= 'A' && vk <= 'Z':
		return string(rune(vk))
	case vk >= vkNumpad0 && vk <= vkNumpad9:
		return fmt.Sprintf("Num%d", vk-vkNumpad0)
	}
	c := rune(vkToChar(vk) & 0xFFFF)
	if c == 0 || unicode.IsControl(c) || unicode.IsSpace(c) {
		return ""
	}
	return strings.ToUpper(string(c))
}

func vkToChar(vk uint32) uint32 {
	const mapvkVKToChar = 2
	r, _, _ := procMapVirtualKeyW.Call(uintptr(vk), mapvkVKToChar)
	return uint32(r)
}
