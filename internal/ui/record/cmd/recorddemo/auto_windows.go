package main

import (
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"golang.org/x/sys/windows"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/record"
	"github.com/wobsoriano/glint/internal/ui/stack/drive"
)

const (
	vkShift, vkEnter = 0x10, 0x0D
	script           = "welcome to glint this is the teleprompter it shows your script while you record and you can scroll it with the mouse wheel to move through the lines one at a time until the very end of the text which keeps going for a few more lines so scrolling has somewhere to go"
)

type driver struct {
	l       *drive.Log
	h       *host
	ctl     *record.Controller
	display platform.Display
	anim    uintptr
}

func run(h *host, ctl *record.Controller) {
	l := drive.NewLog(*outDir)
	defer l.Finish()
	time.AfterFunc(3*time.Minute, func() {
		l.Printf("FAIL the drive took over 3 minutes")
		os.Exit(2)
	})
	d := &driver{l: l, h: h, ctl: ctl}
	displays, err := platform.Displays()
	if !l.Check(err == nil, "read the displays: %v", err) {
		return
	}
	d.display = displays[0]
	l.Printf("display %+v, Library %s", d.display, store.DefaultRoot())

	cmd := exec.Command(os.Args[0], "-animate")
	if !l.Check(cmd.Start() == nil, "start the animated app") {
		return
	}
	defer cmd.Process.Kill()
	d.anim = drive.FindWindow(animTitle, 5*time.Second)
	if !l.Check(d.anim != 0, "the animated app shows") {
		return
	}
	demote(d.anim)
	time.Sleep(500 * time.Millisecond)

	steps := []func() bool{d.picker, d.countdownCancels, d.recordDisplay, d.recordArea, d.deleteFromBar, d.recordWindow}
	for _, step := range steps {
		if !step() {
			break
		}
	}
	d.ctl.Quit(func() {})
}

// pressHotkey presses the Record hotkey, Ctrl+Shift+7, as the user would.
func pressHotkey() { drive.Keys(drive.VKControl, vkShift, '7') }

func (d *driver) window(s record.Surface) uintptr {
	if w := d.ctl.Window(s); w != nil {
		return w.NativeHandle()
	}
	return 0
}

func (d *driver) waitSurface(s record.Surface, shown bool, within time.Duration) bool {
	return drive.Until(within, func() bool { return (d.window(s) != 0) == shown })
}

// point is the centre of a named control on screen, once it has been laid out.
func (d *driver) point(s record.Surface, name string) (int, int, bool) {
	var x, y int
	ok := drive.Until(3*time.Second, func() bool {
		h := d.window(s)
		if h == 0 {
			return false
		}
		var found bool
		x, y, found = d.ctl.Point(s, name, drive.WindowRect(h), d.display.Scale)
		return found
	})
	return x, y, ok
}

func (d *driver) click(s record.Surface, name string) bool {
	x, y, ok := d.point(s, name)
	if !d.l.Check(ok, "%s %q is on screen at (%d,%d)", s, name, x, y) {
		return false
	}
	drive.Click(x, y)
	time.Sleep(250 * time.Millisecond)
	return true
}

func (d *driver) savedPrefs() settings.RecordPrefs { return settings.LoadFrom(settings.Path()).Record }

func (d *driver) libraryEntries() []string {
	entries, _ := os.ReadDir(store.DefaultRoot())
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func typeText(s string) {
	for _, r := range strings.ToUpper(s) {
		drive.Keys(uint16(r))
		time.Sleep(8 * time.Millisecond)
	}
}

// picker opens the picker with the hotkey, shows its device lists and changes choices, which
// settings.json then holds.
func (d *driver) picker() bool {
	l := d.l
	pressHotkey()
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the Record hotkey opens the picker") {
		return false
	}
	time.Sleep(600 * time.Millisecond)
	pr := drive.WindowRect(d.window(record.SurfacePicker))
	work := d.workArea()
	l.Check(pr.Y+pr.H < work.Y+work.H && abs(pr.X+pr.W/2-(work.X+work.W/2)) <= 1, "the picker sits at the bottom centre of the work area: %v in %v", pr, work)
	l.Check(drive.Foreground() == d.window(record.SurfacePicker), "the picker has the keyboard (foreground %q)", drive.WindowText(drive.Foreground()))
	l.Shot("01-picker.png", d.display.Bounds)

	mics, err := capture.Microphones()
	l.Check(err == nil, "list microphones: %d, %v", len(mics), err)
	for _, m := range mics {
		l.Printf("microphone %q", m.Name)
	}
	cams, err := media.Cameras()
	l.Printf("cameras: %d (%v)", len(cams), err)

	if !d.click(record.SurfacePicker, "camera") || !l.Check(d.waitSurface(record.SurfacePanel, true, 2*time.Second), "the camera list opens") {
		return false
	}
	time.Sleep(300 * time.Millisecond)
	_, _, none := d.point(record.SurfacePanel, "cam-none")
	l.Check(none, "the camera list offers No camera")
	l.Shot("02-camera-list.png", d.display.Bounds)
	drive.Keys(drive.VKEscape)
	l.Check(d.waitSurface(record.SurfacePanel, false, 2*time.Second), "Esc closes the camera list")
	l.Check(d.window(record.SurfacePicker) != 0, "and the picker stays")

	if !d.click(record.SurfacePicker, "mic") || !l.Check(d.waitSurface(record.SurfacePanel, true, 2*time.Second), "the microphone list opens") {
		return false
	}
	time.Sleep(300 * time.Millisecond)
	l.Shot("03-mic-list.png", d.display.Bounds)
	if len(mics) > 0 {
		id := string(mics[0].ID)
		if !d.click(record.SurfacePanel, "mic-"+id) {
			return false
		}
		l.Check(d.waitSurface(record.SurfacePanel, false, 2*time.Second), "choosing a microphone closes the list")
		p := d.savedPrefs()
		l.Check(p.Mic != nil && *p.Mic == id, "settings.json holds the microphone %q", mics[0].Name)
		if m, err := capture.StartMicMonitor(mics[0].ID); err == nil {
			var peak float32
			for range 20 {
				time.Sleep(100 * time.Millisecond)
				peak = max(peak, m.Level())
			}
			m.Close()
			l.Printf("microphone peak RMS over 2 s: %.5f (the meter fills %.0f%%)", peak, 100*meter(peak))
		}
		time.Sleep(300 * time.Millisecond)
		l.Shot("04-picker-mic-meter.png", d.display.Bounds)
	}

	before := d.savedPrefs().SystemAudio
	d.click(record.SurfacePicker, "system-audio")
	l.Check(d.savedPrefs().SystemAudio == !before, "the system audio toggle is saved (%v to %v)", before, !before)
	d.click(record.SurfacePicker, "system-audio")
	l.Check(d.savedPrefs().SystemAudio == before, "and back (%v)", before)
	if !before {
		d.click(record.SurfacePicker, "system-audio")
	}

	if !d.click(record.SurfacePicker, "timer") || !d.click(record.SurfacePanel, "timer-3") {
		return false
	}
	l.Check(d.savedPrefs().Countdown == 3, "the 3 s timer is saved (%d)", d.savedPrefs().Countdown)
	return d.click(record.SurfacePicker, "src-Display")
}

func meter(rms float32) float64 {
	if rms <= 1e-6 {
		return 0
	}
	db := 20 * math.Log10(float64(rms))
	return min(max((db+60)/60, 0), 1)
}

func (d *driver) workArea() platform.Rect {
	var r platform.Rect
	mygo.RunOnMain(func() {
		a := mygo.Screen.PrimaryDisplay().WorkArea
		r = platform.Rect{X: a.X, Y: a.Y, W: a.Width, H: a.Height}
	})
	k := d.display.Scale
	return platform.Rect{X: int(float64(r.X) * k), Y: int(float64(r.Y) * k), W: int(float64(r.W) * k), H: int(float64(r.H) * k)}
}

func abs(v int) int { return max(v, -v) }

// countdownCancels starts a recording and presses Esc during the countdown: nothing is
// recorded.
func (d *driver) countdownCancels() bool {
	l := d.l
	before := len(d.libraryEntries())
	if !d.click(record.SurfacePicker, "record") {
		return false
	}
	if !l.Check(d.waitSurface(record.SurfaceCountdown, true, 2*time.Second), "Record shows the countdown") {
		return false
	}
	time.Sleep(700 * time.Millisecond)
	l.Shot("05-countdown.png", d.display.Bounds)
	_, _, cancel := d.point(record.SurfacePicker, "cancel")
	l.Check(cancel, "the picker offers Cancel during the countdown")
	drive.Keys(drive.VKEscape)
	l.Check(d.waitSurface(record.SurfaceCountdown, false, 2*time.Second), "Esc cancels the countdown")
	time.Sleep(3 * time.Second)
	l.Check(!d.ctl.Live() && d.window(record.SurfaceBar) == 0, "no recording started after the countdown's time")
	l.Check(len(d.libraryEntries()) == before, "nothing was written to the Library (%v)", d.libraryEntries())
	l.Check(d.window(record.SurfacePicker) != 0, "the picker stays open")
	return true
}

// recordDisplay records the display for about 4 s with a pause, stops with the hotkey and
// checks the package it wrote.
func (d *driver) recordDisplay() bool {
	l := d.l
	if !d.click(record.SurfacePicker, "timer") || !d.click(record.SurfacePanel, "timer-1") {
		return false
	}
	if !d.click(record.SurfacePicker, "record") {
		return false
	}
	if !l.Check(drive.Until(5*time.Second, d.ctl.Live), "the recording starts after the 1 s countdown") {
		return false
	}
	start := time.Now()
	l.Check(d.waitSurface(record.SurfaceBar, true, 2*time.Second), "the control bar shows")
	l.Check(d.window(record.SurfacePicker) == 0, "the picker closed")
	bar := drive.WindowRect(d.window(record.SurfaceBar))
	work := d.workArea()
	l.Check(abs(bar.X+bar.W/2-(work.X+work.W/2)) <= 1 && bar.Y+bar.H < work.Y+work.H, "the bar sits at the bottom centre of the work area: %v", bar)
	shot := drive.Capture(bar)
	l.Check(greenShare(shot, shot.Bounds()) > 0.9, "a screen capture leaves the bar out: %.0f%% of its rect shows the app behind", 100*greenShare(shot, shot.Bounds()))
	time.Sleep(1200 * time.Millisecond)
	if !d.click(record.SurfaceBar, "pause") {
		return false
	}
	time.Sleep(1000 * time.Millisecond)
	if !d.click(record.SurfaceBar, "pause") {
		return false
	}
	time.Sleep(1800 * time.Millisecond)
	before := d.h.finishedCount()
	pressHotkey()
	wall := time.Since(start)
	if !l.Check(drive.Until(10*time.Second, func() bool { return d.h.finishedCount() > before }), "the Record hotkey stops the recording into the Library") {
		return false
	}
	l.Check(d.window(record.SurfaceBar) == 0, "the bar closed")
	item := d.h.lastFinished()
	l.Check(strings.HasSuffix(item.Dir, ".glintrec"), "the package is %s", filepath.Base(item.Dir))
	for _, f := range []string{"screen.mp4", "system.wav", "input.json", "meta.json", "thumb.png"} {
		_, err := os.Stat(filepath.Join(item.Dir, f))
		l.Check(err == nil, "it holds %s", f)
	}
	dur := time.Duration(*item.Meta.DurationMs) * time.Millisecond
	l.Check(dur > 3*time.Second && dur < wall-700*time.Millisecond, "the pause is left out: %v recorded of %v", dur, wall.Round(time.Millisecond))
	l.Check(item.Meta.Width == uint32(d.display.Bounds.W) && item.Meta.Height == uint32(d.display.Bounds.H), "meta.json says %dx%d", item.Meta.Width, item.Meta.Height)
	d.probe(filepath.Join(item.Dir, "screen.mp4"))
	input, _ := os.ReadFile(filepath.Join(item.Dir, "input.json"))
	l.Check(len(input) > 0 && !strings.Contains(string(input), "Ctrl+Shift+7"), "input.json leaves the Record hotkey out of its keystrokes (%d bytes)", len(input))

	frames := d.frames(filepath.Join(item.Dir, "screen.mp4"), 3)
	if len(frames) == 3 {
		inBar := image.Rect(bar.X-d.display.Bounds.X, bar.Y-d.display.Bounds.Y, bar.X-d.display.Bounds.X+bar.W, bar.Y-d.display.Bounds.Y+bar.H)
		for i, f := range frames {
			l.Check(greenShare(f, inBar) > 0.9, "frame %d shows the app where the bar was, not the bar: %.0f%% green", i, 100*greenShare(f, inBar))
		}
		savePNG(filepath.Join(*outDir, "06-display-frame.png"), frames[1])
		l.Check(drive.Diff(frames[0], frames[2], 8) > 1000, "the animation moves between frames (%d px differ)", drive.Diff(frames[0], frames[2], 8))
	}
	return l.Check(drive.Until(3*time.Second, func() bool { return !d.ctl.Live() }), "the session is idle again")
}

// recordArea draws an area, records it with the teleprompter on, and restarts once.
func (d *driver) recordArea() bool {
	l := d.l
	os.Setenv("GLINT_CAPTURE_CHROME", "1")
	defer os.Unsetenv("GLINT_CAPTURE_CHROME")
	pressHotkey()
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the hotkey opens the picker again") {
		return false
	}
	if !d.click(record.SurfacePicker, "src-Area") {
		return false
	}
	if !l.Check(d.waitSurface(record.SurfacePicker, false, 2*time.Second), "Area hides the picker for the selection") {
		return false
	}
	time.Sleep(600 * time.Millisecond)
	l.Shot("07-select.png", d.display.Bounds)
	x0, y0 := d.display.Bounds.X+400, d.display.Bounds.Y+300
	drive.Drag(x0, y0, x0+801, y0+401, 20, 20*time.Millisecond)
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the picker comes back after the drag") {
		return false
	}
	p := d.savedPrefs()
	want := settings.Area{int32(x0), int32(y0), 800, 400}
	l.Check(p.Target == settings.RecordArea && p.Area != nil && *p.Area == want, "settings.json holds the area %v (%v %v)", want, p.Target, p.Area)
	l.Check(d.waitSurface(record.SurfaceGuide, true, 2*time.Second), "the guide shows around the area")

	if !d.click(record.SurfacePicker, "prompter") || !l.Check(d.waitSurface(record.SurfacePanel, true, 2*time.Second), "the teleprompter panel opens") {
		return false
	}
	time.Sleep(300 * time.Millisecond)
	typeText(script)
	if !d.click(record.SurfacePanel, "prompter-on") || !d.click(record.SurfacePanel, "lines-2") {
		return false
	}
	time.Sleep(300 * time.Millisecond)
	l.Shot("08-script-panel.png", d.display.Bounds)
	p = d.savedPrefs()
	l.Check(p.Script == script && p.Teleprompter && p.PrompterLines == 2, "settings.json holds the script, the teleprompter on and 2 lines")
	drive.Keys(drive.VKEscape)
	d.waitSurface(record.SurfacePanel, false, 2*time.Second)
	time.Sleep(300 * time.Millisecond)
	l.Shot("09-area-picking.png", d.display.Bounds)

	if !d.click(record.SurfacePicker, "record") || !l.Check(drive.Until(5*time.Second, d.ctl.Live), "the area recording starts") {
		return false
	}
	d.waitSurface(record.SurfaceBar, true, 2*time.Second)
	if !l.Check(d.waitSurface(record.SurfacePrompter, true, 2*time.Second), "the teleprompter shows") {
		return false
	}
	time.Sleep(700 * time.Millisecond)
	bar := drive.WindowRect(d.window(record.SurfaceBar))
	pill := drive.WindowRect(d.window(record.SurfacePrompter))
	l.Shot("10-area-live.png", d.display.Bounds)
	a := l.Shot("11-bar-a.png", bar)
	time.Sleep(1500 * time.Millisecond)
	b := l.Shot("12-bar-b.png", bar)
	l.Check(drive.Diff(a, b, 30) > 20, "the bar's clock ticks (%d px changed)", drive.Diff(a, b, 30))
	if !d.click(record.SurfaceBar, "pause") {
		return false
	}
	time.Sleep(400 * time.Millisecond)
	l.Shot("12-bar-paused.png", bar)
	if !d.click(record.SurfaceBar, "pause") {
		return false
	}

	cx, cy := x0+400, y0+200
	l.Check(drive.RootAt(cx, cy) == d.anim, "the window under the area is the app, not the guide (%q)", drive.WindowText(drive.RootAt(cx, cy)))
	title := drive.WindowText(d.anim)
	drive.Click(cx, cy)
	l.Check(drive.Until(2*time.Second, func() bool { return drive.WindowText(d.anim) != title }), "a click in the area reaches the app behind the guide (%q to %q)", title, drive.WindowText(d.anim))

	p0 := l.Shot("13-prompter-a.png", pill)
	drive.Wheel(pill.X+pill.W/2, pill.Y+pill.H/3, -2)
	time.Sleep(400 * time.Millisecond)
	p1 := l.Shot("14-prompter-b.png", pill)
	l.Check(drive.Diff(p0, p1, 30) > 200, "the wheel scrolls the teleprompter (%d px changed)", drive.Diff(p0, p1, 30))

	if !d.click(record.SurfaceBar, "restart") {
		return false
	}
	l.Check(drive.Until(5*time.Second, d.ctl.Live), "Restart starts recording again")
	time.Sleep(2 * time.Second)
	staging := 0
	for _, n := range d.libraryEntries() {
		if strings.HasPrefix(n, ".") {
			staging++
		}
	}
	l.Check(staging == 1, "Restart discarded the first take: %d staging dir(s) %v", staging, d.libraryEntries())
	before := d.h.finishedCount()
	if !d.click(record.SurfaceBar, "stop") {
		return false
	}
	if !l.Check(drive.Until(10*time.Second, func() bool { return d.h.finishedCount() > before }), "Stop finishes the area recording") {
		return false
	}
	item := d.h.lastFinished()
	l.Check(item.Meta.Width == 800 && item.Meta.Height == 400, "the area recording is %dx%d", item.Meta.Width, item.Meta.Height)
	l.Check(*item.Meta.DurationMs < 3500, "it holds the take after the restart only (%d ms)", *item.Meta.DurationMs)
	l.Check(item.Meta.Source != nil && *item.Meta.Source == "800 × 400 area of "+d.display.Name, "its source is %q", deref(item.Meta.Source))
	w, h := d.probe(filepath.Join(item.Dir, "screen.mp4"))
	l.Check(w == 800 && h == 400, "ffprobe reads %dx%d", w, h)
	if f := d.frames(filepath.Join(item.Dir, "screen.mp4"), 1); len(f) == 1 {
		savePNG(filepath.Join(*outDir, "15-area-frame.png"), f[0])
		l.Check(greenShare(f[0], f[0].Bounds()) > 0.6, "the area video shows the app undimmed (%.0f%% green)", 100*greenShare(f[0], f[0].Bounds()))
	}
	return true
}

// deleteFromBar records the area again with the chrome left out of captures, checks the
// guide is out of a screen capture, and deletes the take from the bar.
func (d *driver) deleteFromBar() bool {
	l := d.l
	pressHotkey()
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the hotkey opens the picker") {
		return false
	}
	if !d.click(record.SurfacePicker, "record") || !l.Check(drive.Until(5*time.Second, d.ctl.Live), "the remembered area records again") {
		return false
	}
	d.waitSurface(record.SurfaceBar, true, 2*time.Second)
	time.Sleep(800 * time.Millisecond)
	guide := d.ctl.Window(record.SurfaceGuide)
	if l.Check(guide != nil, "the guide shows") {
		r := drive.WindowRect(guide.NativeHandle())
		shot := l.Shot("16-protected.png", r)
		l.Check(greenShare(shot, image.Rect(300, 200, 1300, 800)) > 0.95, "a screen capture leaves the guide and its dim out")
	}
	entries := len(d.libraryEntries())
	before := d.h.finishedCount()
	if !d.click(record.SurfaceBar, "delete") || !d.click(record.SurfaceBar, "confirm-delete") {
		return false
	}
	l.Check(drive.Until(3*time.Second, func() bool { return d.h.lastToast() == "Recording deleted" }), "Delete says %q", d.h.lastToast())
	time.Sleep(500 * time.Millisecond)
	l.Check(d.window(record.SurfaceBar) == 0 && d.window(record.SurfaceGuide) == 0, "the bar and the guide closed")
	l.Check(len(d.libraryEntries()) == entries-1 && d.h.finishedCount() == before, "the take is gone from the Library: %v", d.libraryEntries())
	return true
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// greenShare is the share of pixels in r that are the animated app's green.
func greenShare(img *image.RGBA, r image.Rectangle) float64 {
	r = r.Intersect(img.Bounds())
	if r.Empty() {
		return 0
	}
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			c := img.RGBAAt(x, y)
			if c.G > 150 && c.R < 80 && c.B < 110 {
				n++
			}
		}
	}
	return float64(n) / float64(r.Dx()*r.Dy())
}

// probe logs what ffprobe reads of the video and returns its size.
func (d *driver) probe(path string) (int, int) {
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_name,width,height,nb_frames,avg_frame_rate:format=duration", "-of", "json", path).Output()
	if !d.l.Check(err == nil, "ffprobe reads %s: %v", filepath.Base(path), err) {
		return 0, 0
	}
	var v struct {
		Streams []struct {
			Codec  string `json:"codec_name"`
			Width  int    `json:"width"`
			Height int    `json:"height"`
			Frames string `json:"nb_frames"`
			Rate   string `json:"avg_frame_rate"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	json.Unmarshal(out, &v)
	if !d.l.Check(len(v.Streams) == 1, "it has a video stream") {
		return 0, 0
	}
	s := v.Streams[0]
	d.l.Printf("ffprobe: %s %dx%d, %s frames at %s, %s s", s.Codec, s.Width, s.Height, s.Frames, s.Rate, v.Format.Duration)
	return s.Width, s.Height
}

// frames decodes n frames spread over the first two seconds of the video.
func (d *driver) frames(path string, n int) []*image.RGBA {
	r, err := media.OpenVideoReader(path)
	if !d.l.Check(err == nil, "decode %s: %v", filepath.Base(path), err) {
		return nil
	}
	defer r.Close()
	var out []*image.RGBA
	for i := 0; len(out) < n; i++ {
		f, err := r.NextFrame()
		if errors.Is(err, io.EOF) || err != nil {
			break
		}
		if i%20 == 5 {
			out = append(out, f.Image)
		}
	}
	return out
}

func savePNG(path string, img image.Image) {
	if f, err := os.Create(path); err == nil {
		png.Encode(f, img)
		f.Close()
	}
}

var pSetWindowPos = windows.NewLazySystemDLL("user32.dll").NewProc("SetWindowPos")

// demote takes the animated app out of the topmost band while keeping it above other apps,
// so Glint's floating bar stays above it after it is clicked, as above any ordinary app.
func demote(h uintptr) {
	const notTopmost = ^uintptr(1)
	const noSize, noMove, noActivate = 0x1, 0x2, 0x10
	pSetWindowPos.Call(h, notTopmost, 0, 0, 0, 0, noSize|noMove|noActivate)
}

// recordWindow picks the animated app's window, records it for a moment and stops with the
// hotkey: the video is the window's size.
func (d *driver) recordWindow() bool {
	l := d.l
	pressHotkey()
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the hotkey opens the picker") {
		return false
	}
	if !d.click(record.SurfacePicker, "src-Window") || !l.Check(d.waitSurface(record.SurfacePicker, false, 2*time.Second), "Window hides the picker for the selection") {
		return false
	}
	time.Sleep(600 * time.Millisecond)
	cx, cy := d.display.Bounds.X+d.display.Bounds.W/2, d.display.Bounds.Y+d.display.Bounds.H/2
	drive.MoveTo(cx-20, cy)
	time.Sleep(100 * time.Millisecond)
	drive.Click(cx, cy)
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the picker comes back after the click") {
		return false
	}
	l.Check(d.savedPrefs().Target == settings.RecordWindow, "settings.json holds the Window target")
	l.Check(d.waitSurface(record.SurfaceGuide, true, 2*time.Second), "the guide outlines the window")
	before := d.h.finishedCount()
	if !d.click(record.SurfacePicker, "record") || !l.Check(drive.Until(5*time.Second, d.ctl.Live), "the window recording starts") {
		return false
	}
	time.Sleep(1500 * time.Millisecond)
	pressHotkey()
	if !l.Check(drive.Until(10*time.Second, func() bool { return d.h.finishedCount() > before }), "the hotkey stops it into the Library") {
		return false
	}
	item := d.h.lastFinished()
	frame := drive.WindowRect(d.anim)
	l.Printf("window rect %v, recording %dx%d from %q", frame, item.Meta.Width, item.Meta.Height, deref(item.Meta.Source))
	l.Check(abs(int(item.Meta.Width)-frame.W) <= 16 && abs(int(item.Meta.Height)-frame.H) <= 16, "the video is the window's size")
	l.Check(strings.HasPrefix(deref(item.Meta.Source), animTitle), "its source is the window's title")
	return true
}
