package app

import (
	"bytes"
	"fmt"
	"image/png"
	"log"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// markPNG renders the Glint mark for the tray: in color on Windows, as a template that macOS
// tints for the menu bar.
func markPNG(size int) []byte {
	template := runtime.GOOS == "darwin"
	img := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Image(c, theme.Mark(template)).Size(float32(size), float32(size))
	}, size, size, 1)
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func (a *App) menu() *mygo.Menu {
	var items []*mygo.MenuItem
	for _, e := range trayMenu(a.settings) {
		if e.label == "" {
			items = append(items, mygo.Separator())
			continue
		}
		cmd := e.cmd
		items = append(items, &mygo.MenuItem{
			Label:       e.menuLabel(),
			Accelerator: e.hotkey,
			Click:       func(*mygo.MenuItem, *mygo.Window) { a.Do(cmd) },
		})
	}
	return mygo.NewMenu(items)
}

func (a *App) buildTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		ID:             "glint",
		Icon:           markPNG(32),
		IconIsTemplate: runtime.GOOS == "darwin",
		ToolTip:        "Glint",
		Menu:           a.menu(),
	})
	if err != nil {
		log.Printf("app: tray: %v", err)
		return
	}
	a.tray = tray
	// MyGo reports each click's release and no double click, so two clicks within the
	// system's double-click time are one (docs/go-port/mygo-findings.md, section 4).
	tray.OnClick(func() {
		now := time.Now().UnixNano()
		if a.lastClick != 0 && time.Duration(now-a.lastClick) <= doubleClickTime() {
			a.lastClick = 0
			a.Do(CmdCaptureArea)
			return
		}
		a.lastClick = now
	})
}

func (a *App) refreshTray() {
	if a.tray != nil {
		a.tray.SetMenu(a.menu())
	}
}

// registerHotkeys binds every shortcut the settings name, replacing what was bound.
// Failures, such as a shortcut another app holds, are kept for the Settings window.
func (a *App) registerHotkeys() {
	a.SuspendHotkeys()
	clear(a.failures)
	for _, action := range settings.HotkeyActions {
		accel := *a.settings.Hotkeys.Binding(action)
		if accel == "" {
			continue
		}
		if err := a.bind(action, accel); err != nil {
			a.failures[action] = err.Error()
			log.Printf("app: shortcut %s for %s: %v", accel, action.Label(), err)
		}
	}
}

func (a *App) bind(action settings.HotkeyAction, accel string) error {
	cmd := hotkeyCommands[action]
	if err := mygo.GlobalShortcut.Register(accel, func() { a.Do(cmd) }); err != nil {
		return err
	}
	a.hotkeys[action] = accel
	return nil
}

// SuspendHotkeys releases every shortcut, while the Settings window records a new one.
func (a *App) SuspendHotkeys() {
	for action, accel := range a.hotkeys {
		mygo.GlobalShortcut.Unregister(accel)
		delete(a.hotkeys, action)
	}
}

// ResumeHotkeys binds the shortcuts again after SuspendHotkeys.
func (a *App) ResumeHotkeys() { a.registerHotkeys() }

// HotkeyFailures says which shortcuts could not be registered, and why.
func (a *App) HotkeyFailures() map[settings.HotkeyAction]string {
	out := make(map[settings.HotkeyAction]string, len(a.failures))
	for k, v := range a.failures {
		out[k] = v
	}
	return out
}

// Rebind gives action the shortcut accel, keeping the old one when accel cannot be
// registered, and saves the settings.
func (a *App) Rebind(action settings.HotkeyAction, accel string) error {
	for other, bound := range a.hotkeys {
		if other != action && strings.EqualFold(bound, accel) {
			return fmt.Errorf("%s is already used for “%s”", accel, other.Label())
		}
	}
	old, had := a.hotkeys[action]
	if had && strings.EqualFold(old, accel) {
		return nil
	}
	if had {
		mygo.GlobalShortcut.Unregister(old)
		delete(a.hotkeys, action)
	}
	if err := a.bind(action, accel); err != nil {
		if had {
			a.bind(action, old)
		}
		return fmt.Errorf("%s could not be registered, another app probably uses it (%v)", accel, err)
	}
	delete(a.failures, action)
	*a.settings.Hotkeys.Binding(action) = accel
	a.saveSettings()
	a.refreshTray()
	return nil
}
