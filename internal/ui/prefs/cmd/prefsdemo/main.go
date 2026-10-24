// Command prefsdemo opens the Settings window over the settings.json of GLINT_DATA_DIR with a
// host that registers the global shortcuts with MyGo and logs every call to demo.log in the
// data directory. Launch at login is only logged, so the demo never touches the login items.
//
//	prefsdemo [-pane shortcuts] [-hold Ctrl+Alt+K]
//
// -hold registers one shortcut and waits, as another app holding it would, so a second
// prefsdemo sees Windows refuse it.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/prefs"
)

type host struct {
	s    settings.Settings
	errs map[settings.HotkeyAction]error
}

func (h *host) Settings() *settings.Settings { return &h.s }

func (h *host) SettingsChanged() {
	if err := h.s.Save(); err != nil {
		log.Printf("save settings: %v", err)
		return
	}
	log.Printf("settings saved")
}

func (h *host) SetLaunchAtLogin(on bool) error {
	log.Printf("launch at login %v", on)
	if os.Getenv("PREFSDEMO_LOGIN_FAILS") != "" {
		return errors.New("Glint could not change its login item (demo)")
	}
	return nil
}

func (h *host) register(a settings.HotkeyAction, accel string) error {
	return mygo.GlobalShortcut.Register(accel, func() { log.Printf("hotkey %s pressed", a.Label()) })
}

func (h *host) ResumeHotkeys() {
	h.errs = map[settings.HotkeyAction]error{}
	for _, a := range settings.HotkeyActions {
		if accel := *h.s.Hotkeys.Binding(a); accel != "" {
			if err := h.register(a, accel); err != nil {
				h.errs[a] = err
			}
		}
	}
	log.Printf("hotkeys registered, %d failed", len(h.errs))
}

func (h *host) SuspendHotkeys() {
	mygo.GlobalShortcut.UnregisterAll()
	log.Printf("hotkeys suspended")
}

func (h *host) HotkeyError(a settings.HotkeyAction) error { return h.errs[a] }

func (h *host) RebindHotkey(a settings.HotkeyAction, accel string) error {
	log.Printf("rebind %s to %s", a.Label(), accel)
	for _, other := range settings.HotkeyActions {
		if other != a && strings.EqualFold(*h.s.Hotkeys.Binding(other), accel) {
			return fmt.Errorf("%s is already used for “%s”", accel, other.Label())
		}
	}
	old := *h.s.Hotkeys.Binding(a)
	if strings.EqualFold(old, accel) {
		return nil
	}
	if old != "" {
		mygo.GlobalShortcut.Unregister(old)
	}
	if err := h.register(a, accel); err != nil {
		if old != "" && h.register(a, old) != nil {
			log.Printf("could not restore %s", old)
		}
		log.Printf("rebind refused: %v", err)
		return fmt.Errorf("%s could not be registered, another app probably uses it (%v)", accel, err)
	}
	delete(h.errs, a)
	return nil
}

func main() {
	pane := flag.String("pane", "general", "the pane to show first")
	hold := flag.String("hold", "", "only hold this global shortcut")
	flag.Parse()
	if os.Getenv("GLINT_DATA_DIR") == "" {
		fmt.Fprintln(os.Stderr, "set GLINT_DATA_DIR to a temporary data directory")
		os.Exit(2)
	}
	f, err := os.OpenFile(filepath.Join(settings.DataDir(), "demo.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(func() {
		if *hold != "" {
			log.Printf("hold %s: %v", *hold, mygo.GlobalShortcut.Register(*hold, func() {}))
			return
		}
		h := &host{s: settings.Load()}
		h.ResumeHotkeys()
		first := prefs.General
		for i, name := range []string{"general", "screenshots", "recordings", "overlay", "shortcuts", "privacy", "about"} {
			if name == *pane {
				first = prefs.Pane(i)
			}
		}
		w := prefs.Open(h, first)
		w.Native().OnClosed(func() { mygo.App.Quit() })
		log.Printf("settings open")
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
