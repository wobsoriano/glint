// Command librarydemo opens the Library window over the Library of GLINT_DATA_DIR with a host
// that does each action plainly and logs it to demo.log in the data directory, so a driver
// script can check what the window asked for.
package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/egoist/mygo"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/library"
)

type host struct {
	s         settings.Settings
	quickLook bool
}

func (h *host) Settings() *settings.Settings { return &h.s }

func (h *host) SettingsChanged() {
	if err := h.s.Save(); err != nil {
		log.Printf("save settings: %v", err)
		return
	}
	log.Printf("settings saved: library %+v", h.s.Library)
}

func (h *host) QuickLook(it store.Item) {
	h.quickLook = true
	log.Printf("quicklook %s", it.ID())
}

func (h *host) CloseQuickLook() bool {
	was := h.quickLook
	h.quickLook = false
	if was {
		log.Printf("quicklook closed")
	}
	return was
}

func (h *host) Edit(it store.Item) { log.Printf("edit %s", it.ID()) }

func (h *host) Copy(items []store.Item) {
	paths := make([]string, len(items))
	for i, it := range items {
		paths[i] = it.Deliverable()
	}
	if err := mygo.Clipboard.WriteFiles(paths...); err != nil {
		log.Printf("copy: %v", err)
		return
	}
	log.Printf("copy %d file(s): %q", len(paths), paths)
}

func (h *host) Export(items []store.Item, dir string) {
	n := 0
	for _, it := range items {
		if it.Kind() != store.Screenshot {
			continue
		}
		if err := copyFile(it.Deliverable(), filepath.Join(dir, it.Meta.Title+filepath.Ext(it.Deliverable()))); err != nil {
			log.Printf("export: %v", err)
			return
		}
		n++
	}
	log.Printf("export %d to %s", n, dir)
}

func (h *host) Reveal(items []store.Item) {
	log.Printf("reveal %s", items[0].ID())
	mygo.Shell.ShowItemInFolder(items[0].Dir)
}

func (h *host) Trash(items []store.Item) error {
	dirs := make([]string, len(items))
	for i, it := range items {
		dirs[i] = it.Dir
	}
	err := store.Trash(dirs)
	log.Printf("trash %q: %v", dirs, err)
	return err
}

func (h *host) Capture(a settings.HotkeyAction) { log.Printf("capture %s", a.Label()) }
func (h *host) OpenSettings()                   { log.Printf("open settings") }
func (h *host) OpenFiles(paths []string)        { log.Printf("open files %q", paths) }

func copyFile(from, to string) error {
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func main() {
	if os.Getenv("GLINT_DATA_DIR") == "" {
		fmt.Fprintln(os.Stderr, "set GLINT_DATA_DIR to a copy of a Library's data directory")
		os.Exit(2)
	}
	f, err := os.OpenFile(filepath.Join(settings.DataDir(), "demo.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		log.Fatal(err)
	}
	log.SetOutput(io.MultiWriter(os.Stderr, f))
	h := &host{s: settings.Load()}
	mygo.App.WhenReady(func() {
		w := library.Open(h, store.New(store.DefaultRoot()))
		w.Native().OnClosed(func() { mygo.App.Quit() })
		log.Printf("library open")
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
