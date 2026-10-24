// Package app is Glint's process: the tray icon and menu, the global shortcuts, the capture
// flow from the overlay to the Library, what happens after a capture, and routing to the
// windows other packages build.
package app

import (
	"crypto/sha256"
	"encoding/hex"
	"image"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/actions"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/overlay"
	"github.com/wobsoriano/glint/internal/ui/toast"
)

// Surfaces are the windows other packages build. Each runs on the main thread. A nil one is
// not built yet: asking for it says so in a toast.
type Surfaces struct {
	// ShowCard shows the preview card of a new capture.
	ShowCard func(item store.Item)
	// OpenEditor opens a screenshot in the editor, with its saved edits.
	OpenEditor func(item store.Item)
	// Pin floats img above every window; dpi is the scale of the display it came from.
	Pin func(img *image.RGBA, dpi float64)
	// Imported tells the Library window about a capture that just joined the Library.
	Imported     func(item store.Item)
	OpenLibrary  func()
	OpenPrefs    func(pane string)
	OpenRecorder func()
	OpenStudio   func(dir string)
	OpenTrim     func(path string)
}

// App owns Glint's state. Every method runs on the main thread.
type App struct {
	Surfaces Surfaces
	Actions  *actions.Actions

	launch   Launch
	settings settings.Settings
	library  store.Library

	sys    system
	picker picker
	acts   automation
	say    func(string)
	post   func(func())

	// last is the directory of the newest capture of this run.
	last string

	hotkeys   map[settings.HotkeyAction]string // what is registered now
	failures  map[settings.HotkeyAction]error
	tray      *mygo.Tray
	lastClick int64 // unix nanoseconds of the previous tray click
	counting  bool  // a self-timer is running
}

// New loads the settings and the Library from the data directory.
func New(l Launch) *App {
	a := &App{
		launch:   l,
		settings: settings.Load(),
		library:  store.New(store.DefaultRoot()),
		sys:      desktop{},
		picker:   overlay.New(),
		say:      func(msg string) { toast.Show(msg) },
		post:     mygo.RunOnMain,
		hotkeys:  map[settings.HotkeyAction]string{},
		failures: map[settings.HotkeyAction]error{},
	}
	a.Actions = actions.New(func() settings.Settings { return a.settings }, a.toast)
	a.acts = a.Actions
	return a
}

// automation is what the capture flow asks of the actions.
type automation interface {
	CopyImage(img image.Image, done func(error))
	Save(item store.Item, ask bool, done func(string, error))
	ReportSave(path string, err error)
	CopyTextOf(img *image.RGBA)
}

// InstanceName names the app for its single-instance lock: a Glint run with GLINT_DATA_DIR is
// kept apart from the installed one and from runs on other data directories.
func InstanceName() string {
	dir := os.Getenv("GLINT_DATA_DIR")
	if dir == "" {
		return "Glint"
	}
	if a, err := filepath.Abs(dir); err == nil {
		dir = a
	}
	sum := sha256.Sum256([]byte(strings.ToLower(filepath.Clean(dir))))
	return "Glint " + hex.EncodeToString(sum[:4])
}

// Run starts the event loop and returns when Glint quits. Call RequestSingleInstanceLock first.
func (a *App) Run() error {
	mygo.App.OnSecondInstance(func(args []string, wd string) {
		l, err := ParseArgs(args, wd)
		if err != nil {
			log.Print(err)
			return
		}
		a.open(l)
	})
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.WhenReady(a.ready)
	return mygo.App.Run()
}

func (a *App) ready() {
	excludeOwnWindows(a.settings.IncludeOwnWindows)
	a.registerHotkeys()
	a.buildTray()
	a.open(a.launch)
}

// open does what a command line asks: a plain launch opens the Library, --background alone
// does nothing more.
func (a *App) open(l Launch) {
	if len(l.Requests) == 0 && !l.Background {
		a.Do(CmdLibrary)
	}
	var images []string
	for _, r := range l.Requests {
		switch r.Kind {
		case RequestCapture:
			a.Do(captureCommands[r.Capture])
		case RequestLibrary:
			a.Do(CmdLibrary)
		case RequestRecord:
			a.Do(CmdRecord)
		case RequestOpen:
			images = append(images, r.Path)
		case RequestTrim:
			a.openTrim(r.Path)
		case RequestStudio:
			if a.surface("Studio", a.Surfaces.OpenStudio != nil) {
				a.Surfaces.OpenStudio(r.Path)
			}
		}
	}
	if len(images) > 0 {
		a.openImages(images)
	}
}

// Do does what a shortcut, a menu item or a command line asks.
func (a *App) Do(cmd Command) {
	switch cmd {
	case CmdCaptureArea:
		a.startArea(overlay.ModeImage)
	case CmdCaptureText:
		a.startArea(overlay.ModeText)
	case CmdCaptureScreen:
		a.captureScreen()
	case CmdCaptureWindow:
		a.captureWindow()
	case CmdCapturePrevious:
		a.capturePrevious()
	case CmdCaptureTimer:
		a.captureAfterTimer()
	case CmdRecord:
		if a.surface("Recording", a.Surfaces.OpenRecorder != nil) {
			a.Surfaces.OpenRecorder()
		}
	case CmdTrim:
		a.pickTrim()
	case CmdLibrary:
		if a.surface("The Library", a.Surfaces.OpenLibrary != nil) {
			a.Surfaces.OpenLibrary()
		}
	case CmdSettings:
		if a.surface("Settings", a.Surfaces.OpenPrefs != nil) {
			a.Surfaces.OpenPrefs("")
		}
	case CmdOpenLast:
		a.openLast()
	case CmdQuit:
		mygo.App.Quit()
	}
}

// surface reports whether a surface is built, and says so when it is not.
func (a *App) surface(name string, built bool) bool {
	if !built {
		log.Printf("app: %s is not built yet", name)
		a.toast(name + " is not available yet")
	}
	return built
}

func (a *App) toast(msg string) { a.say(msg) }

// Settings is the app's one settings value. Windows change it in place on the main thread,
// then call SettingsChanged.
func (a *App) Settings() *settings.Settings { return &a.settings }

// SettingsChanged saves the settings and applies them: the shortcuts, the tray menu and
// whether Glint's windows show in screenshots.
func (a *App) SettingsChanged() {
	a.saveSettings()
	excludeOwnWindows(a.settings.IncludeOwnWindows)
	if a.tray != nil {
		a.registerHotkeys()
		a.refreshTray()
	}
}

// SetLaunchAtLogin registers Glint to start in the tray at login, or stops it.
func (a *App) SetLaunchAtLogin(on bool) error {
	return platform.SetLaunchAtLogin(on, []string{"--background"})
}

// Capture starts a capture as the action's global shortcut does.
func (a *App) Capture(action settings.HotkeyAction) { a.Do(hotkeyCommands[action]) }

// OpenFiles opens images in the editor and videos in Trim & Compress.
func (a *App) OpenFiles(paths []string) {
	var images []string
	for _, p := range paths {
		if IsVideo(p) {
			a.openTrim(p)
		} else {
			images = append(images, p)
		}
	}
	if len(images) > 0 {
		a.openImages(images)
	}
}

// Library is the Library on disk.
func (a *App) Library() store.Library { return a.library }

func (a *App) saveSettings() {
	if err := a.settings.Save(); err != nil {
		log.Printf("app: saving settings: %v", err)
	}
}
