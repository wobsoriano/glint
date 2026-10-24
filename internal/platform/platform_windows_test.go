package platform_test

// Tests named TestDesktop* call the real APIs against the interactive desktop: they open
// small topmost windows of a known colour (never focused) and read the screen back. They
// skip under -short, for sessions without a desktop.

import (
	"errors"
	"fmt"
	"image"
	"image/color"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/platform/internal/swatch"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func desktop(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("needs an interactive desktop")
	}
}

func show(t *testing.T, r platform.Rect, c color.RGBA, title string, tool bool) *swatch.Swatch {
	t.Helper()
	s, err := swatch.Show(r, c, title, tool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s
}

func primary(t *testing.T) platform.Display {
	t.Helper()
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	return ds[0]
}

// swatchRect is a w×h rect centred on d.
func swatchRect(d platform.Display, w, h int) platform.Rect {
	return platform.Rect{X: d.Bounds.X + (d.Bounds.W-w)/2, Y: d.Bounds.Y + (d.Bounds.H-h)/2, W: w, H: h}
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting until %s", what)
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func near(a, b color.RGBA, tol int) bool {
	d := func(x, y uint8) bool { return max(int(x)-int(y), int(y)-int(x)) <= tol }
	return d(a.R, b.R) && d(a.G, b.G) && d(a.B, b.B)
}

// worst is the largest per-channel distance from want over every pixel of img.
func worst(img *image.RGBA, want color.RGBA) (int, color.RGBA) {
	b := img.Bounds()
	dist, at := 0, img.RGBAAt(b.Min.X, b.Min.Y)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			p := img.RGBAAt(x, y)
			for _, d := range []int{int(p.R) - int(want.R), int(p.G) - int(want.G), int(p.B) - int(want.B)} {
				if d < 0 {
					d = -d
				}
				if d > dist {
					dist, at = d, p
				}
			}
		}
	}
	return dist, at
}

func centre(img *image.RGBA) color.RGBA {
	b := img.Bounds()
	return img.RGBAAt((b.Min.X+b.Max.X)/2, (b.Min.Y+b.Max.Y)/2)
}

func capture(t *testing.T, r platform.Rect) *image.RGBA {
	t.Helper()
	img, err := platform.CaptureRect(r)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func TestTheProcessSeesPhysicalPixels(t *testing.T) {
	if !platform.PerMonitorV2() {
		t.Fatal("process is not per-monitor-v2 DPI aware")
	}
	err := platform.SetPerMonitorV2()
	t.Logf("a second SetProcessDpiAwarenessContext, as MyGo makes: %v", err)
	if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
		t.Errorf("second call: %v, want access denied", err)
	}
	if !platform.PerMonitorV2() {
		t.Fatal("a second call changed the awareness")
	}
}

func TestDesktopDisplays(t *testing.T) {
	desktop(t)
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	if !ds[0].Primary || ds[0].Bounds.X != 0 || ds[0].Bounds.Y != 0 {
		t.Errorf("first display is not the primary at the origin: %+v", ds[0])
	}
	var names []string
	for _, d := range ds {
		t.Logf("%s id=%#x bounds=%+v scale=%.2f primary=%v", d.Name, d.ID, d.Bounds, d.Scale, d.Primary)
		if d.Bounds.Empty() || d.Scale < 1 {
			t.Errorf("bad display %+v", d)
		}
		names = append(names, d.Name)
	}
	slices.Sort(names)
	for i, n := range names {
		if n != fmt.Sprintf("Display %d", i+1) {
			t.Errorf("names %v are not Display 1..%d", names, len(ds))
		}
	}
	x, y := platform.CursorPos()
	if !slices.ContainsFunc(ds, func(d platform.Display) bool { return d.Bounds.Contains(x, y) }) {
		t.Errorf("cursor (%d, %d) is on no display", x, y)
	}
}

// The colours a user sees must come back exactly, including on an HDR desktop where SDR
// content is tone mapped.
func TestDesktopCaptureColours(t *testing.T) {
	desktop(t)
	r := swatchRect(primary(t), 360, 240)
	inner := platform.Rect{X: r.X + 8, Y: r.Y + 8, W: r.W - 16, H: r.H - 16}
	for _, want := range []color.RGBA{
		{18, 140, 230, 255},
		{200, 40, 60, 255},
		{250, 250, 250, 255},
		{6, 6, 6, 255},
		{128, 128, 128, 255},
	} {
		show(t, r, want, "glint colour check", true)
		var img *image.RGBA
		eventually(t, fmt.Sprintf("the swatch %v shows", want), func() bool {
			img = capture(t, inner)
			return near(centre(img), want, 2)
		})
		dist, at := worst(img, want)
		t.Logf("want %v got centre %v worst %v (distance %d)", want, centre(img), at, dist)
		if dist > 2 {
			t.Errorf("colour %v came back as far as %v", want, at)
		}
	}
}

// One blit covers several displays: a swatch straddling each pair of neighbouring displays
// must come back whole. With one display, a rect hanging off the desktop must still return
// the on-screen part.
func TestDesktopCaptureSpansDisplays(t *testing.T) {
	desktop(t)
	ds, err := platform.Displays()
	if err != nil {
		t.Fatal(err)
	}
	var virt platform.Rect
	for i, d := range ds {
		if i == 0 {
			virt = d.Bounds
			continue
		}
		x0, y0 := min(virt.X, d.Bounds.X), min(virt.Y, d.Bounds.Y)
		x1, y1 := max(virt.X+virt.W, d.Bounds.X+d.Bounds.W), max(virt.Y+virt.H, d.Bounds.Y+d.Bounds.H)
		virt = platform.Rect{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}
	}
	full := capture(t, virt)
	if full.Bounds().Dx() != virt.W || full.Bounds().Dy() != virt.H {
		t.Fatalf("virtual desktop capture is %v, want %dx%d", full.Bounds().Size(), virt.W, virt.H)
	}

	want := color.RGBA{30, 180, 90, 255}
	straddled := 0
	for _, a := range ds {
		for _, b := range ds {
			if a.Bounds.X+a.Bounds.W != b.Bounds.X {
				continue
			}
			y := max(a.Bounds.Y, b.Bounds.Y) + 100
			r := platform.Rect{X: b.Bounds.X - 150, Y: y, W: 300, H: 120}
			s := show(t, r, want, "glint span check", true)
			eventually(t, "the straddling swatch shows on both displays", func() bool {
				img := capture(t, platform.Rect{X: r.X + 4, Y: r.Y + 4, W: r.W - 8, H: r.H - 8})
				d, _ := worst(img, want)
				return d <= 2
			})
			s.Close()
			straddled++
		}
	}
	t.Logf("%d display boundaries straddled", straddled)

	d := ds[0]
	r := platform.Rect{X: d.Bounds.X + d.Bounds.W - 100, Y: d.Bounds.Y + 100, W: 100, H: 100}
	show(t, r, want, "glint edge check", true)
	off := platform.Rect{X: r.X, Y: r.Y, W: 200, H: 100}
	eventually(t, "the edge swatch shows", func() bool {
		return near(capture(t, off).RGBAAt(50, 50), want, 2)
	})
	img := capture(t, off)
	if img.Bounds().Dx() != 200 {
		t.Fatalf("off-desktop capture is %v wide", img.Bounds().Dx())
	}
	p := img.RGBAAt(150, 50)
	t.Logf("off the desktop the capture holds %v", p)
	if p.A != 255 {
		t.Errorf("the part off every display is not opaque: %v", p)
	}
}

func TestDesktopWindowsListsOwnWindowFrontToBack(t *testing.T) {
	desktop(t)
	d := primary(t)
	r := swatchRect(d, 300, 200)
	back := show(t, r, color.RGBA{200, 0, 0, 255}, "glint list back", false)
	front := show(t, platform.Rect{X: r.X + 50, Y: r.Y + 50, W: 300, H: 200}, color.RGBA{0, 0, 200, 255}, "glint list front", false)
	tool := show(t, r, color.RGBA{0, 200, 0, 255}, "glint list tool", true)

	ws, err := platform.Windows()
	if err != nil {
		t.Fatal(err)
	}
	at := func(hwnd uintptr) int {
		return slices.IndexFunc(ws, func(w platform.Window) bool { return w.ID == int64(hwnd) })
	}
	bi, fi := at(back.HWND), at(front.HWND)
	if bi < 0 || fi < 0 {
		t.Fatalf("own windows missing: back %d front %d of %d", bi, fi, len(ws))
	}
	if fi > bi {
		t.Errorf("front window listed after the one it covers (%d > %d)", fi, bi)
	}
	if at(tool.HWND) >= 0 {
		t.Error("tool window listed")
	}
	exe, _ := os.Executable()
	w := ws[bi]
	if w.Title != "glint list back" || w.PID != os.Getpid() || w.Bounds != r {
		t.Errorf("listed %+v, want title, pid %d and bounds %+v", w, os.Getpid(), r)
	}
	if want := strings.TrimSuffix(filepath.Base(exe), ".exe"); w.App != want {
		t.Errorf("app %q, want %q", w.App, want)
	}
	for _, w := range ws {
		if w.Bounds.W <= 20 || w.Bounds.H <= 20 || w.Title == "" {
			t.Errorf("listed a window that should be skipped: %+v", w)
		}
	}
}

func TestDesktopCaptureWindowSeesThroughCoveringWindows(t *testing.T) {
	desktop(t)
	d := primary(t)
	r := swatchRect(d, 320, 200)
	want := color.RGBA{220, 120, 20, 255}
	target := show(t, r, want, "glint window target", false)
	show(t, r, color.RGBA{20, 20, 220, 255}, "glint window cover", true)
	eventually(t, "the cover shows", func() bool {
		return near(centre(capture(t, r)), color.RGBA{20, 20, 220, 255}, 2)
	})

	img, err := platform.CaptureWindow(int64(target.HWND), false)
	if err != nil {
		t.Fatal(err)
	}
	if img.Bounds() != image.Rect(0, 0, r.W, r.H) {
		t.Fatalf("window capture is %v, want %dx%d", img.Bounds(), r.W, r.H)
	}
	if dist, at := worst(img, want); dist > 2 {
		t.Errorf("window capture has %v, want %v everywhere", at, want)
	}

	sh, err := platform.CaptureWindow(int64(target.HWND), true)
	if err != nil {
		t.Fatal(err)
	}
	s := platform.WindowScale(target.HWND)
	pad, drop := int(28*s+0.5), int(6*s+0.5)
	if sh.Bounds().Dx() != r.W+2*pad || sh.Bounds().Dy() != r.H+2*pad+drop {
		t.Fatalf("shadowed capture is %v, want %dx%d", sh.Bounds().Size(), r.W+2*pad, r.H+2*pad+drop)
	}
	if p := sh.RGBAAt(pad+r.W/2, pad+r.H/2); !near(p, want, 2) || p.A != 255 {
		t.Errorf("shadowed middle %v", p)
	}
	if p := sh.RGBAAt(0, 0); p.A != 0 {
		t.Errorf("shadow margin corner %v, want transparent", p)
	}
	if p := sh.RGBAAt(pad, pad); p.A == 255 {
		t.Errorf("window corner %v is not rounded", p)
	}
	if p := sh.RGBAAt(pad+r.W/2, pad+r.H+drop/2+1); p.A == 0 || p.R != 0 {
		t.Errorf("no shadow below the window: %v", p)
	}
}

func TestDesktopExcludeFromCapture(t *testing.T) {
	desktop(t)
	r := swatchRect(primary(t), 200, 160)
	want := color.RGBA{240, 0, 240, 255}
	s := show(t, r, want, "glint exclude check", true)
	shows := func() bool { return near(centre(capture(t, r)), want, 2) }
	eventually(t, "the swatch shows", shows)

	if err := platform.ExcludeFromCapture(s.HWND, true); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the excluded swatch leaves the capture", func() bool { return !shows() })
	if err := platform.ExcludeFromCapture(s.HWND, false); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the swatch comes back", shows)
}

func TestDesktopWallpaper(t *testing.T) {
	desktop(t)
	p, err := platform.Wallpaper()
	if errors.Is(err, fs.ErrNotExist) {
		t.Skipf("no wallpaper file: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(p); err != nil || st.IsDir() {
		t.Errorf("wallpaper %q is not a file: %v", p, err)
	}
	t.Logf("wallpaper %s", p)
}

func TestRunCommandQuotesLikeRust(t *testing.T) {
	got := platform.RunCommand(`C:\Program Files\Glint\Glint.exe`, []string{"--background"})
	if want := `"C:\Program Files\Glint\Glint.exe" --background`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
	got = platform.RunCommand(`C:\Glint.exe`, []string{"--open", `C:\My Shots`})
	if want := `"C:\Glint.exe" --open "C:\My Shots"`; got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

// Writes the real Run value and puts back whatever was there before.
func TestLaunchAtLoginWritesTheRustValue(t *testing.T) {
	k, err := registry.OpenKey(registry.CURRENT_USER, platform.RunKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer k.Close()
	before, _, beforeErr := k.GetStringValue(platform.RunValue)
	t.Cleanup(func() {
		if beforeErr == nil {
			k.SetStringValue(platform.RunValue, before)
		} else {
			k.DeleteValue(platform.RunValue)
		}
	})

	if err := platform.SetLaunchAtLogin(true, []string{"--background"}); err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	got, typ, err := k.GetStringValue(platform.RunValue)
	if err != nil || typ != registry.SZ {
		t.Fatalf("Run value: %q type %d: %v", got, typ, err)
	}
	if want := `"` + exe + `" --background`; got != want {
		t.Errorf("Run value %s, want %s", got, want)
	}
	if err := platform.SetLaunchAtLogin(false, nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := k.GetStringValue(platform.RunValue); !errors.Is(err, registry.ErrNotExist) {
		t.Errorf("Run value still there after disabling: %v", err)
	}
	if err := platform.SetLaunchAtLogin(false, nil); err != nil {
		t.Errorf("disabling twice: %v", err)
	}
}
