// Package prefs is the Settings window: every preference of settings.json in panes, the
// global shortcuts recorded by pressing them, and the preview card's layout arranged by
// dragging its actions.
package prefs

import "github.com/wobsoriano/glint/internal/settings"

// Host is what the Settings window needs from the app. Every method runs on the main thread.
type Host interface {
	// Settings is the app's one settings value, which the window changes in place before
	// it calls SettingsChanged. SettingsChanged saves settings.json and applies it: the
	// tray, the preview stack, keeping Glint's windows out of captures.
	Settings() *settings.Settings
	SettingsChanged()

	// SetLaunchAtLogin registers Glint to start at login, or stops it. On an error the
	// window leaves the setting as it was and shows the error.
	SetLaunchAtLogin(on bool) error

	// RebindHotkey registers accel for a in place of a's current shortcut. On success the
	// window stores accel in Settings().Hotkeys and calls SettingsChanged. On an error the
	// previous shortcut must stay registered; the window shows the error as it is, such as
	// "Ctrl+Alt+K could not be registered, another app probably uses it".
	RebindHotkey(a settings.HotkeyAction, accel string) error
	// HotkeyError is why a's shortcut from the settings did not register, nil when it did.
	HotkeyError(a settings.HotkeyAction) error
	// SuspendHotkeys unregisters every global shortcut while the window records a new one,
	// so pressing a taken combination records it instead of capturing; ResumeHotkeys
	// registers them again from the settings.
	SuspendHotkeys()
	ResumeHotkeys()
}
