// Package record is Glint's screen recording UI: the picker that chooses what to record and
// with which devices, the countdown, the guide around a window or area, the floating control
// bar, the teleprompter and the camera bubble, and the session that drives media.Recorder and
// puts the finished recording into the Library.
package record

import (
	"errors"
	"image"
	"log"
	"os"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// Host is what recording needs from the app. Every method runs on the main thread and must
// return quickly.
type Host interface {
	// Settings is the app's one settings value. The controller changes Record in place and
	// then calls SettingsChanged, which saves it. Hotkeys name Glint's own shortcuts, which
	// recordings leave out of their keystroke captions.
	Settings() *settings.Settings
	SettingsChanged()
	Library() store.Library
	// SelectRegion lets the user click a window or drag an area on the display, as the
	// capture overlay does; kind is what the user asked for. done runs on the main thread,
	// with ok false when the user backs out. Select is one implementation.
	SelectRegion(kind settings.RecordTarget, display platform.Display, done func(sel Selection, ok bool))
	// RecordingFinished hands over a recording that is now in the Library, with the warnings
	// of its tracks: the app runs Settings > After recording on it.
	RecordingFinished(item store.Item, warnings []string)
	Toast(msg string)
}

type stage int

const (
	idle stage = iota
	picking
	selecting
	counting
	starting
	live
	finishing
)

type session struct {
	rec     *media.Recorder
	pending store.PendingRecording
	target  Target
}

// Controller runs recordings one at a time. Its methods are safe from any goroutine; its
// state lives on the main thread.
type Controller struct {
	host Host

	stage stage
	// epoch invalidates timers and background starts that belong to a stage left since.
	epoch   int
	target  *Target
	left    int // seconds of the countdown still to go
	session *session
	monitor *capture.MicMonitor
	onQuit  func()

	owner                                   *mygo.Window
	picker                                  *picker
	guide, countdown, bar, prompter, camera *mygo.Window
	barView                                 *bar
	controls                                map[control]ui.Rect
}

func New(host Host) *Controller { return &Controller{host: host} }

// Toggle is the Record hotkey: it opens the picker, closes it, cancels the countdown, or
// stops the recording.
func (c *Controller) Toggle() { mygo.RunOnMain(c.toggle) }

// Live reports whether a recording is running.
func (c *Controller) Live() (on bool) {
	mygo.RunOnMain(func() { on = c.stage == live })
	return on
}

// Quit ends what is going on, finishing a running recording into the Library, then calls
// done on the main thread.
func (c *Controller) Quit(done func()) {
	mygo.RunOnMain(func() {
		c.onQuit = done
		switch c.stage {
		case live:
			c.stop()
		case starting, finishing:
		default:
			c.end()
			c.quitNow()
		}
	})
}

func (c *Controller) quitNow() {
	if done := c.onQuit; done != nil {
		c.onQuit = nil
		done()
	}
}

func (c *Controller) prefs() *settings.RecordPrefs { return &c.host.Settings().Record }

func (c *Controller) updatePrefs(f func(*settings.RecordPrefs)) {
	f(c.prefs())
	c.host.SettingsChanged()
}

func (c *Controller) toggle() {
	switch c.stage {
	case idle:
		c.openPicker()
	case picking, selecting:
		c.closePicker()
	case counting:
		c.cancelCountdown()
	case live:
		c.stop()
	}
}

func (c *Controller) displays() []platform.Display {
	list, err := platform.Displays()
	if err != nil {
		c.host.Toast("Could not read the displays: " + err.Error())
	}
	return list
}

func (c *Controller) displayAtCursor() (platform.Display, bool) {
	x, y := platform.CursorPos()
	d, err := displayAt(c.displays(), x, y)
	return d, err == nil
}

func (c *Controller) openPicker() {
	if c.target == nil {
		p := c.prefs()
		switch {
		case p.Target == settings.RecordArea && p.Area != nil:
			if t, ok := areaTarget(c.displays(), prefArea(*p.Area)); ok {
				c.target = &t
			}
		case p.Target == settings.RecordWindow:
		default:
			if d, ok := c.displayAtCursor(); ok {
				t := displayTarget(d)
				c.target = &t
			}
		}
	}
	c.stage = picking
	c.restartMonitor()
	c.showGuide()
	c.showPicker()
	c.showCamera()
}

// picked takes what the user chose on screen and goes back to the picker.
func (c *Controller) picked(sel Selection, ok bool) {
	if c.stage != selecting {
		return
	}
	if ok {
		if t, usable := fromSelection(c.displays(), sel); usable {
			c.target = &t
			c.updatePrefs(func(p *settings.RecordPrefs) {
				p.Target = t.Kind
				if t.Kind == settings.RecordArea {
					p.Area = areaPref(t.Area)
				}
			})
		} else {
			c.host.Toast("That area is too small to record")
		}
	}
	c.openPicker()
}

func (c *Controller) choose(kind settings.RecordTarget) {
	if c.stage != picking {
		return
	}
	d, ok := c.displayAtCursor()
	if !ok {
		return
	}
	if kind == settings.RecordDisplay {
		t := displayTarget(d)
		c.target = &t
		c.updatePrefs(func(p *settings.RecordPrefs) { p.Target = settings.RecordDisplay })
		c.showGuide()
		return
	}
	c.hidePickerChrome()
	c.stage = selecting
	c.host.SelectRegion(kind, d, c.picked)
}

func (c *Controller) closeWindow(w **mygo.Window) {
	if *w != nil {
		(*w).Destroy()
		*w = nil
	}
}

func (c *Controller) hidePickerChrome() {
	if c.picker != nil {
		c.picker.close()
		c.picker = nil
	}
	c.closeWindow(&c.guide)
	c.closeWindow(&c.camera)
}

func (c *Controller) closePicker() {
	c.epoch++
	c.hidePickerChrome()
	c.stopMonitor()
	c.stage = idle
	c.closeWindow(&c.countdown)
}

func (c *Controller) stopMonitor() {
	if c.monitor != nil {
		c.monitor.Close()
		c.monitor = nil
	}
}

func (c *Controller) restartMonitor() {
	c.stopMonitor()
	if id := c.prefs().Mic; id != nil {
		if m, err := capture.StartMicMonitor(capture.DeviceID(*id)); err == nil {
			c.monitor = m
		}
	}
}

func (c *Controller) micLevel() (float32, bool) {
	if c.monitor == nil {
		return 0, false
	}
	return c.monitor.Level(), true
}

func (c *Controller) record() {
	if c.stage != picking {
		return
	}
	if c.target == nil {
		c.choose(c.prefs().Target)
		return
	}
	secs := int(c.prefs().Countdown)
	if secs == 0 {
		c.begin()
		return
	}
	c.epoch++
	epoch := c.epoch
	c.stage, c.left = counting, secs
	c.showCountdown()
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for range t.C {
			more := false
			mygo.RunOnMain(func() {
				if c.epoch != epoch || c.stage != counting {
					return
				}
				c.left--
				if c.left == 0 {
					c.begin()
					return
				}
				c.redraw(c.countdown)
				more = true
			})
			if !more {
				return
			}
		}
	}()
	c.redrawPicker()
}

func (c *Controller) cancelCountdown() {
	if c.stage != counting {
		return
	}
	c.epoch++
	c.stage = picking
	c.closeWindow(&c.countdown)
	c.redrawPicker()
}

func (c *Controller) redraw(w *mygo.Window) {
	if w != nil {
		w.Invalidate()
	}
}

func (c *Controller) redrawPicker() {
	if c.picker != nil {
		c.picker.win.Invalidate()
	}
}

func (c *Controller) begin() {
	if c.target == nil {
		return
	}
	target := *c.target
	p := *c.prefs()
	c.epoch++
	epoch := c.epoch
	c.stage = starting
	c.stopMonitor()
	c.closeWindow(&c.countdown)
	if c.picker != nil {
		c.picker.close()
		c.picker = nil
	}
	c.closeWindow(&c.camera)
	if c.guide == nil {
		c.showGuide()
	}

	pending, err := c.host.Library().BeginRecording(time.Now())
	if err != nil {
		c.host.Toast("Could not start recording: " + err.Error())
		c.end()
		return
	}
	if p.Teleprompter && strings.TrimSpace(p.Script) != "" {
		c.showPrompter(target, p.Script, int(min(max(p.PrompterLines, 1), 6)))
	}
	opts := media.RecordOptions{
		Source:      target.Source(),
		SessionDir:  pending.Dir,
		FPS:         media.FPS30,
		SystemAudio: p.SystemAudio,
		RecordInput: true,
		IgnoredKeys: ignoredKeys(c.host.Settings().Hotkeys),
	}
	if p.Mic != nil {
		id := capture.DeviceID(*p.Mic)
		opts.Mic = &id
	}
	if p.Camera != nil {
		id := media.CameraID(*p.Camera)
		opts.Camera = &id
	}
	go func() {
		rec, err := media.StartRecorder(opts)
		mygo.RunOnMain(func() {
			if c.epoch != epoch {
				if err == nil {
					go rec.Cancel()
				}
				return
			}
			if err != nil {
				os.RemoveAll(pending.Dir)
				c.host.Toast("Could not start recording: " + err.Error())
				c.end()
				c.quitNow()
				return
			}
			c.session = &session{rec: rec, pending: pending, target: target}
			c.stage = live
			if c.onQuit != nil {
				c.stop()
				return
			}
			if c.bar == nil {
				c.showBar(target)
			}
			if w := rec.Warnings(); len(w) > 0 {
				log.Printf("recording started with warnings: %v", w)
				c.host.Toast(warningSummary(w))
			}
			c.watchLive(epoch)
		})
	}()
}

// watchLive stops the recording once its screen capture ends on its own, as when the
// recorded window closes.
func (c *Controller) watchLive(epoch int) {
	go func() {
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			more := false
			mygo.RunOnMain(func() {
				if c.epoch != epoch || c.stage != live {
					return
				}
				if !c.session.rec.IsLive() {
					c.stop()
					return
				}
				more = true
			})
			if !more {
				return
			}
		}
	}()
}

// end closes every surface and goes idle.
func (c *Controller) end() {
	c.epoch++
	c.stage = idle
	c.stopMonitor()
	c.hidePickerChrome()
	for _, w := range []**mygo.Window{&c.countdown, &c.bar, &c.prompter} {
		c.closeWindow(w)
	}
	c.barView = nil
}

func (c *Controller) takeSession() *session {
	if c.stage != live {
		return nil
	}
	s := c.session
	c.session = nil
	return s
}

func (c *Controller) pauseToggle() {
	if c.stage != live {
		return
	}
	if r := c.session.rec; r.IsPaused() {
		r.Resume()
	} else {
		r.Pause()
	}
}

func (c *Controller) restart() {
	s := c.takeSession()
	if s == nil {
		return
	}
	c.closeWindow(&c.prompter)
	c.target = &s.target
	c.stage = starting
	c.epoch++
	epoch := c.epoch
	go func() {
		s.rec.Cancel()
		mygo.RunOnMain(func() {
			if c.epoch == epoch {
				c.begin()
			}
		})
	}()
}

func (c *Controller) delete() {
	s := c.takeSession()
	if s == nil {
		return
	}
	c.end()
	go s.rec.Cancel()
	c.host.Toast("Recording deleted")
}

func (c *Controller) stop() {
	s := c.takeSession()
	if s == nil {
		return
	}
	c.end()
	c.stage = finishing
	lib := c.host.Library()
	go func() {
		item, warnings, err := finish(lib, s)
		mygo.RunOnMain(func() {
			c.stage = idle
			if err != nil {
				c.host.Toast("The recording could not be saved: " + err.Error())
			} else if c.onQuit == nil {
				c.host.RecordingFinished(item, warnings)
			}
			c.quitNow()
		})
	}()
}

// finish stops the recorder and moves its files into the Library, with the first frame as
// the thumbnail.
func finish(lib store.Library, s *session) (store.Item, []string, error) {
	sum := s.rec.Stop()
	if len(sum.Files) == 0 {
		return store.Item{}, sum.Warnings, errors.New("nothing was recorded")
	}
	info := store.RecordingInfo{Width: sum.Width, Height: sum.Height, DurationMs: sum.DurationMS, Source: s.target.Label()}
	var thumb image.Image
	if r, err := media.OpenVideoReader(sum.Files[0]); err == nil {
		if f, err := r.NextFrame(); err == nil {
			thumb = f.Image
		}
		r.Close()
	}
	item, err := lib.FinishRecording(s.pending, info, thumb)
	return item, sum.Warnings, err
}

// later runs fn on the main thread after the frame being built, for actions that close the
// window whose view asks for them.
func later(fn func()) { go mygo.RunOnMain(fn) }
