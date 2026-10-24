package media

// Tests named TestDesktop* capture the real desktop: they open small topmost windows that
// never take focus and record them. They skip under -short.

import (
	"errors"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/media/capture"
	"github.com/wobsoriano/glint/internal/media/internal/testwin"
	"github.com/wobsoriano/glint/internal/platform"
)

func desktop(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("needs an interactive desktop")
	}
}

var markerColour = color.RGBA{30, 220, 60, 255}

const markerSize = 24

// markerAt is the marker's top-left in the target window's client area.
var markerAt = image.Pt(100, 80)

func openTarget(t *testing.T) *testwin.Window {
	t.Helper()
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	d := ds[0].Bounds
	r := image.Rect(d.X+d.W/2-200, d.Y+d.H/2-150, d.X+d.W/2+200, d.Y+d.H/2+150)
	w, err := testwin.Open(r, testwin.Options{Title: "Glint pointer map target", Bordered: true, Paint: func(c *testwin.Canvas, _ int64) {
		c.Fill(image.Rect(0, 0, c.W, c.H), color.RGBA{50, 50, 50, 255})
		c.Fill(image.Rectangle{Min: markerAt, Max: markerAt.Add(image.Pt(markerSize, markerSize))}, markerColour)
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(w.Close)
	time.Sleep(300 * time.Millisecond)
	return w
}

// findMarker is the top-left of the bright marker on the dark target within 40 pixels of
// around. It thresholds luma, which H.264 keeps at full resolution; chroma is shared by
// 2×2 blocks and smears the marker's colour a pixel past odd edges.
func findMarker(img *image.RGBA, around image.Point) (image.Point, bool) {
	r := image.Rectangle{Min: around, Max: around.Add(image.Pt(markerSize, markerSize))}.Inset(-40).Intersect(img.Rect)
	top := image.Pt(r.Max.X, r.Max.Y)
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			p := img.RGBAAt(x, y)
			if 2126*int(p.R)+7152*int(p.G)+722*int(p.B) > 109*10000 {
				top = image.Pt(min(top.X, x), min(top.Y, y))
			}
		}
	}
	return top, top.X < r.Max.X
}

func lastFrame(t *testing.T, path string) *image.RGBA {
	t.Helper()
	fr, err := OpenFrameReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()
	var img *image.RGBA
	for {
		f, err := fr.NextFrame()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		img = f.ToRGBA(img)
		f.Release()
	}
	if img == nil {
		t.Fatal("no frames")
	}
	return img
}

// TestDesktopPointerMapsOntoTheVideoPixel records a marker at a known physical desktop
// position through the recorder's own source resolution, capture and pointer map, then
// finds the marker in the decoded video: the pointer map must send the marker's desktop
// position to the pixel where the video shows it, for each kind of source.
func TestDesktopPointerMapsOntoTheVideoPixel(t *testing.T) {
	desktop(t)
	win := openTarget(t)
	marker := win.Client().Min.Add(markerAt)
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	d := ds[0]
	wr := win.Rect()
	area := platform.Rect{X: wr.Min.X - 51, Y: wr.Min.Y - 33, W: wr.Dx() + 101, H: wr.Dy() + 67}
	for _, tc := range []struct {
		name string
		src  Source
	}{
		{"display", DisplaySource{Display: d.ID}},
		{"area", AreaSource{Display: d.ID, Rect: area}},
		{"window", WindowSource{Window: int64(win.HWND)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res, err := resolveSource(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			clock := capture.NewClock()
			path := filepath.Join(t.TempDir(), "screen.mp4")
			screen, geo, err := startScreen(res.screen, FPS30, path, clock)
			if err != nil {
				t.Fatal(err)
			}
			clock.Resume()
			time.Sleep(400 * time.Millisecond)
			clock.Pause()
			if _, err := screen.finish(); err != nil {
				t.Fatal(err)
			}
			m := pointerMap(res, geo)
			runtime.LockOSThread()
			restore := physicalPixels()
			got := m.ToCapture(marker.X, marker.Y)
			restore()
			runtime.UnlockOSThread()

			img := lastFrame(t, path)
			at, ok := findMarker(img, image.Pt(int(got.X), int(got.Y)))
			if !ok {
				t.Fatalf("marker not in the %v video", img.Rect.Size())
			}
			if int(got.X) != at.X || int(got.Y) != at.Y {
				t.Fatalf("pointer at the marker maps to (%v, %v); the video shows the marker at %v (video %v, crop %v)",
					got.X, got.Y, at, img.Rect.Size(), geo.crop)
			}
			t.Logf("marker at desktop %v shows at video %v of %v (crop %v, window bounds %v, window rect %v)",
				marker, at, img.Rect.Size(), geo.crop, geo.bounds, wr)
		})
	}
}

// TestDesktopMissingDevicesBecomeWarnings starts a recording with a camera and a
// microphone that do not exist: both become warnings, the screen still records, and
// neither leaves a file behind.
func TestDesktopMissingDevicesBecomeWarnings(t *testing.T) {
	desktop(t)
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "session")
	cam := CameraID(`\\?\no-such-camera`)
	mic := capture.DeviceID("{0.0.1.00000000}.{00000000-0000-0000-0000-000000000000}")
	rec, err := StartRecorder(RecordOptions{Source: DisplaySource{Display: ds[0].ID}, SessionDir: dir, FPS: FPS30, Camera: &cam, Mic: &mic})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	sum := rec.Stop()
	joined := strings.Join(sum.Warnings, "\n")
	if !strings.Contains(joined, `camera not recorded: no camera with id \\?\no-such-camera`) || !strings.Contains(joined, "microphone not recorded") {
		t.Fatalf("warnings %q", sum.Warnings)
	}
	if len(sum.Files) != 1 || filepath.Base(sum.Files[0]) != "screen.mp4" || sum.Screen.Frames < 5 {
		t.Fatalf("files %v, %d frames", sum.Files, sum.Screen.Frames)
	}
	for _, name := range []string{"camera.mp4", "mic.wav"} {
		if _, err := os.Stat(filepath.Join(dir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s exists after its device failed: %v", name, err)
		}
	}
}

func TestCamerasEnumerate(t *testing.T) {
	list, err := Cameras()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.ID == "" || c.Name == "" {
			t.Errorf("camera %+v", c)
		}
	}
	t.Logf("%d cameras", len(list))
}

// TestDesktopCancelDeletesTheSession checks Cancel leaves nothing, the input track's
// cursors directory included.
func TestDesktopCancelDeletesTheSession(t *testing.T) {
	desktop(t)
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "session")
	rec, err := StartRecorder(RecordOptions{Source: DisplaySource{Display: ds[0].ID}, SessionDir: dir, FPS: FPS60, RecordInput: true, SystemAudio: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	rec.Cancel()
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		entries, _ := os.ReadDir(dir)
		t.Fatalf("session dir survives Cancel: %v %v", err, entries)
	}
}

// TestDesktopFailedScreenKeepsWhatItWrote injects an encoder failure after 10 frames: the
// recording ends there, screen.mp4 keeps those frames, and media time stops at them.
func TestDesktopFailedScreenKeepsWhatItWrote(t *testing.T) {
	desktop(t)
	t.Setenv("GLINT_FAIL_SCREEN_AFTER", "10")
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "session")
	rec, err := StartRecorder(RecordOptions{Source: DisplaySource{Display: ds[0].ID}, SessionDir: dir, FPS: FPS30})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	for rec.IsLive() {
		if time.Since(start) > 3*time.Second {
			t.Fatal("the recording is still live after its screen track failed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Logf("ended on its own %v after starting", time.Since(start))
	sum := rec.Stop()
	if !strings.Contains(strings.Join(sum.Warnings, "\n"), "the screen capture failed; the recording ends there: GLINT_FAIL_SCREEN_AFTER") {
		t.Fatalf("warnings %q", sum.Warnings)
	}
	if sum.Screen.Frames != 10 || sum.DurationMS != 333 {
		t.Fatalf("%d frames, %d ms; want 10 frames ending media time at 333 ms", sum.Screen.Frames, sum.DurationMS)
	}
	fr, err := OpenFrameReader(sum.Files[0])
	if err != nil {
		t.Fatal(err)
	}
	defer fr.Close()
	n := 0
	for {
		f, err := fr.NextFrame()
		if err != nil {
			break
		}
		f.Release()
		n++
	}
	if n != 10 {
		t.Fatalf("screen.mp4 decodes to %d frames, want 10", n)
	}
}

// TestDesktopClosedWindowEndsTheRecording closes the recorded window: the screen track
// ends on its own, media time stops at its last frame, and Stop says why.
func TestDesktopClosedWindowEndsTheRecording(t *testing.T) {
	desktop(t)
	win := openTarget(t)
	dir := filepath.Join(t.TempDir(), "session")
	rec, err := StartRecorder(RecordOptions{Source: WindowSource{Window: int64(win.HWND)}, SessionDir: dir, FPS: FPS30, SystemAudio: true})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	win.Close()
	deadline := time.Now().Add(2 * time.Second)
	for rec.IsLive() {
		if time.Now().After(deadline) {
			t.Fatal("the recording is still live after its window closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	ended := rec.Elapsed()
	time.Sleep(200 * time.Millisecond)
	sum := rec.Stop()
	if !strings.Contains(strings.Join(sum.Warnings, "\n"), "the captured window closed") {
		t.Fatalf("warnings %q", sum.Warnings)
	}
	if time.Duration(sum.DurationMS)*time.Millisecond != ended.Truncate(time.Millisecond) || sum.Screen.Frames == 0 {
		t.Fatalf("media time %d ms after the window closed at %v, %d frames", sum.DurationMS, ended, sum.Screen.Frames)
	}
	wav, err := ReadWAV(filepath.Join(dir, "system.wav"))
	if err != nil {
		t.Fatal(err)
	}
	if d := wav.DurationMS() - int64(sum.DurationMS); d < -1 || d > 1 {
		t.Fatalf("system.wav lasts %d ms, media time %d ms", wav.DurationMS(), sum.DurationMS)
	}
}
