package app

import (
	"fmt"
	"strings"

	"github.com/wobsoriano/glint/internal/settings"
)

// Command is something the user asks Glint to do, from a shortcut, the tray menu or a command
// line.
type Command int

const (
	CmdCaptureArea Command = iota
	CmdCaptureWindow
	CmdCaptureScreen
	CmdCapturePrevious
	CmdCaptureText
	CmdCaptureTimer
	CmdRecord
	CmdTrim
	CmdLibrary
	CmdSettings
	CmdOpenLast
	CmdQuit
)

var captureCommands = map[Capture]Command{
	CaptureArea: CmdCaptureArea, CaptureWindow: CmdCaptureWindow, CaptureScreen: CmdCaptureScreen,
	CaptureText: CmdCaptureText, CapturePrevious: CmdCapturePrevious,
}

var hotkeyCommands = map[settings.HotkeyAction]Command{
	settings.HotkeyArea: CmdCaptureArea, settings.HotkeyFullscreen: CmdCaptureScreen, settings.HotkeyWindow: CmdCaptureWindow,
	settings.HotkeyPreviousArea: CmdCapturePrevious, settings.HotkeyText: CmdCaptureText,
	settings.HotkeyRecording: CmdRecord, settings.HotkeyLibrary: CmdLibrary,
}

// menuEntry is one line of the tray menu. A zero label is a separator. Hotkey names the
// shortcut shown beside the label, and hint is other text to show.
type menuEntry struct {
	label  string
	hotkey string
	hint   string
	cmd    Command
}

// trayMenu is the Rust build's tray menu for the current settings.
func trayMenu(s settings.Settings) []menuEntry {
	h := s.Hotkeys
	return []menuEntry{
		{label: "Capture Area", hotkey: h.Area, cmd: CmdCaptureArea},
		{label: "Capture Window", hotkey: h.Window, cmd: CmdCaptureWindow},
		{label: "Capture Full Screen", hotkey: h.Fullscreen, cmd: CmdCaptureScreen},
		{label: "Capture Previous Area", hotkey: h.PreviousArea, cmd: CmdCapturePrevious},
		{label: "Capture Text", hotkey: h.Text, cmd: CmdCaptureText},
		{label: "Self-Timer", hint: fmt.Sprintf("%d s", timerSeconds(s)), cmd: CmdCaptureTimer},
		{label: "Record Screen", hotkey: h.Recording, cmd: CmdRecord},
		{label: "Trim & Compress Video…", cmd: CmdTrim},
		{},
		{label: "Open Library", hotkey: h.Library, cmd: CmdLibrary},
		{label: "Edit Last Capture", cmd: CmdOpenLast},
		{label: "Settings…", cmd: CmdSettings},
		{},
		{label: "Quit Glint", cmd: CmdQuit},
	}
}

// menuLabel is what the menu shows: Win32 and AppKit menus read a single & as a mnemonic
// marker, and a hint that is not a shortcut follows the label in parentheses.
func (e menuEntry) menuLabel() string {
	label := strings.ReplaceAll(e.label, "&", "&&")
	if e.hint != "" {
		label += " (" + e.hint + ")"
	}
	return label
}

func timerSeconds(s settings.Settings) int { return max(int(s.TimerSeconds), 1) }
