package main

import (
	"os"
	"strings"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/ui/record"
	"github.com/wobsoriano/glint/internal/ui/stack/drive"
)

// runCamera checks the camera bubble. With a camera attached it shows the first one live;
// without, the list offers only No camera, and a remembered camera that is gone shows no
// bubble, says so, and still records.
func runCamera(h *host, ctl *record.Controller) {
	l := drive.NewLog(*outDir)
	defer l.Finish()
	time.AfterFunc(2*time.Minute, func() {
		l.Printf("FAIL the drive took over 2 minutes")
		os.Exit(2)
	})
	d := &driver{l: l, h: h, ctl: ctl}
	displays, err := platform.Displays()
	if !l.Check(err == nil && len(displays) > 0, "read the displays: %v", err) {
		return
	}
	d.display = displays[0]
	os.Setenv("GLINT_CAPTURE_CHROME", "1")
	h.s.Record.Countdown, h.s.Record.Target, h.s.Record.Teleprompter = 0, settings.RecordDisplay, false
	cams, err := media.Cameras()
	l.Printf("cameras: %d (%v)", len(cams), err)

	gone := `\\?\usb#vid_0000&pid_0000#gone`
	h.s.Record.Camera = &gone
	if len(cams) > 0 {
		id := string(cams[0].ID)
		h.s.Record.Camera = &id
	}
	pressHotkey()
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the hotkey opens the picker") {
		return
	}
	time.Sleep(1500 * time.Millisecond)
	l.Shot("camera-01-picker.png", d.display.Bounds)
	if len(cams) > 0 {
		if l.Check(d.waitSurface(record.SurfaceCamera, true, 3*time.Second), "the bubble shows %s", cams[0].Name) {
			bubble := drive.WindowRect(d.window(record.SurfaceCamera))
			a := l.Shot("camera-02-bubble-a.png", bubble)
			time.Sleep(time.Second)
			b := l.Shot("camera-03-bubble-b.png", bubble)
			l.Check(drive.Diff(a, b, 8) > 0, "the bubble is live (%d px changed in 1 s)", drive.Diff(a, b, 8))
		}
	} else {
		l.Check(d.window(record.SurfaceCamera) == 0, "no bubble opens for the camera that is gone")
		l.Check(strings.HasPrefix(h.lastToast(), "Camera preview unavailable: no camera with id"), "the user is told: %q", h.lastToast())
		if d.click(record.SurfacePicker, "camera") && l.Check(d.waitSurface(record.SurfacePanel, true, 2*time.Second), "the camera list opens") {
			time.Sleep(300 * time.Millisecond)
			_, _, none := d.point(record.SurfacePanel, "cam-none")
			l.Check(none, "the camera list offers No camera")
			l.Shot("camera-02-list.png", d.display.Bounds)
			drive.Keys(drive.VKEscape)
			d.waitSurface(record.SurfacePanel, false, 2*time.Second)
		}
	}
	time.Sleep(300 * time.Millisecond)
	if !d.click(record.SurfacePicker, "record") || !l.Check(drive.Until(5*time.Second, ctl.Live), "the recording starts") {
		return
	}
	l.Check(d.window(record.SurfaceCamera) == 0, "the bubble closes while recording")
	time.Sleep(2 * time.Second)
	d.stopInto("the recording")
	h.mu.Lock()
	l.Printf("toasts: %q", h.toasts)
	h.mu.Unlock()
	ctl.Quit(func() {})
}
