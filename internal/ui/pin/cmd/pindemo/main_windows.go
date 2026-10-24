// Command pindemo pins a demo screenshot. With -auto it drives the pin on the real desktop,
// checks each gesture and writes screenshots to -out, then quits with 1 if a check failed. It
// puts an image on the clipboard. Run it inside the desktop lock:
//
//	go build -o %TEMP%\pindemo.exe ./internal/ui/pin/cmd/pindemo
//	go run ./tools/desklock -- %TEMP%\pindemo.exe -auto
package main

import (
	"flag"
	"image"
	"image/color"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/pin"
	"github.com/wobsoriano/glint/internal/ui/stack/drive"
)

var (
	auto       = flag.Bool("auto", false, "drive the pin, check it and quit")
	foreground = flag.Bool("foreground", false, "run as the other app that keeps the keyboard")
	outDir     = flag.String("out", filepath.Join(os.TempDir(), "glint-pindemo-evidence"), "where screenshots go")
)

func stripes(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			c := color.RGBA{0x3A, 0x5B, 0xD9, 255}
			if (x+y)/30%2 == 1 {
				c = color.RGBA{0xF2, 0xF4, 0xFF, 255}
			}
			img.SetRGBA(x, y, c)
		}
	}
	return img
}

func main() {
	flag.Parse()
	if *foreground {
		drive.RunForeground()
	} else {
		mygo.App.OnWindowAllClosed(func() {})
		mygo.App.WhenReady(func() {
			if *auto {
				go run()
				return
			}
			pin.Open(stripes(800, 450), 1)
		})
	}
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func center(r platform.Rect) (int, int) { return r.X + r.W/2, r.Y + r.H/2 }

// osClipboardImage asks another process the size of the clipboard's device-independent
// bitmap, the format Paint and Office paste, as "WxH".
func osClipboardImage() string {
	out, err := exec.Command("powershell", "-NoProfile", "-STA", "-Command",
		"Add-Type -AssemblyName System.Windows.Forms; $s = [System.Windows.Forms.Clipboard]::GetData('DeviceIndependentBitmap'); "+
			"if ($s) { $b = New-Object byte[] 12; [void]$s.Read($b, 0, 12); '{0}x{1}' -f [BitConverter]::ToInt32($b, 4), [Math]::Abs([BitConverter]::ToInt32($b, 8)) } "+
			"else { 'formats: ' + ([System.Windows.Forms.Clipboard]::GetDataObject().GetFormats() -join ',') }").Output()
	if err != nil {
		return "error: " + err.Error()
	}
	return strings.TrimSpace(string(out))
}

func run() {
	l := drive.NewLog(*outDir)
	defer l.Finish()
	sx, sy := platform.CursorPos()
	defer drive.MoveTo(sx, sy)
	drive.MoveTo(960, 40)

	fg, other := drive.StartForeground(l)
	if fg != nil {
		defer fg.Process.Kill()
	}
	l.Check(other != 0, "another app has the keyboard")

	area := workArea()
	p := pin.Open(stripes(800, 450), 1)
	copied := make(chan error, 1)
	p.Copied = func(err error) { copied <- err }
	time.Sleep(500 * time.Millisecond)
	hwnd := p.Window().NativeHandle()
	r := drive.WindowRect(hwnd)
	want := platform.Rect{X: area.X + (area.W-800)/2, Y: area.Y + (area.H-450)/2, W: 800, H: 450}
	l.Check(r == want, "the pin opens centered on the work area at the image's size (%+v, want %+v)", r, want)
	l.Check(drive.Foreground() == hwnd, "the new pin has the keyboard (foreground %q)", drive.WindowText(drive.Foreground()))
	l.Shot("pin-open.png", platform.Rect{X: r.X - 40, Y: r.Y - 40, W: r.W + 80, H: r.H + 80})

	drive.SetWindowRect(other, platform.Rect{X: r.X - 200, Y: r.Y - 100, W: r.W + 400, H: r.H + 200})
	or := drive.WindowRect(other)
	drive.Click(or.X+60, or.Y+or.H-60)
	time.Sleep(400 * time.Millisecond)
	cx, cy := center(r)
	l.Check(drive.Foreground() == other && drive.RootAt(cx, cy) == hwnd, "with the other app focused and under it, the pin stays on top (foreground %q, at the pin's center %q)", drive.WindowText(drive.Foreground()), drive.WindowText(drive.RootAt(cx, cy)))
	l.Shot("pin-over-focused-app.png", or)

	drive.Drag(cx, cy, cx+150, cy+80, 20, 15*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	moved := drive.WindowRect(hwnd)
	l.Check(moved.X == r.X+150 && moved.Y == r.Y+80 && moved.W == r.W, "dragging the pin by (150, 80) moves it by exactly that (%+v from %+v)", moved, r)
	l.Check(drive.Foreground() == hwnd, "pressing the pin gives it the keyboard")

	mx, my := center(moved)
	drive.Wheel(mx, my, 2)
	time.Sleep(300 * time.Millisecond)
	zoomed := drive.WindowRect(hwnd)
	wantW := int(math.Round(800 * 1.08 * 1.08))
	l.Check(abs(zoomed.W-wantW) <= 2 && abs(zoomed.H*16-zoomed.W*9) <= 32, "two notches up zoom the pin to %dx%d (want about %d wide, 16:9)", zoomed.W, zoomed.H, wantW)
	zx, zy := center(zoomed)
	l.Check(abs(zx-mx) <= 2 && abs(zy-my) <= 2, "zooming keeps the point under the pointer (center %d,%d, pointer %d,%d)", zx, zy, mx, my)
	l.Shot("pin-zoomed.png", platform.Rect{X: zoomed.X - 20, Y: zoomed.Y - 20, W: zoomed.W + 40, H: zoomed.H + 40})
	drive.Wheel(mx, my, -2)
	time.Sleep(300 * time.Millisecond)
	back := drive.WindowRect(hwnd)
	l.Check(abs(back.W-800) <= 2, "two notches down zoom it back (%dx%d)", back.W, back.H)

	behind := drive.Capture(platform.Rect{X: back.X + 100, Y: back.Y + 100, W: 1, H: 1}).RGBAAt(0, 0)
	drive.WithCtrl(func() { drive.Wheel(mx, my, -3) })
	time.Sleep(300 * time.Millisecond)
	alpha := drive.LayeredAlpha(hwnd)
	l.Check(math.Abs(p.Opacity()-0.76) < 0.001 && abs(alpha-194) <= 1, "Ctrl and three notches down fade the pin to 76%% (window alpha %d of 255, opacity %.2f)", alpha, p.Opacity())
	faded := drive.Capture(platform.Rect{X: back.X + 100, Y: back.Y + 100, W: 1, H: 1}).RGBAAt(0, 0)
	l.Check(faded != behind, "the faded pin lets the app behind show through (%v, opaque %v)", faded, behind)
	l.Shot("pin-faded.png", platform.Rect{X: back.X - 20, Y: back.Y - 20, W: back.W + 40, H: back.H + 40})
	drive.WithCtrl(func() { drive.Wheel(mx, my, 3) })
	time.Sleep(200 * time.Millisecond)
	l.Check(p.Opacity() == 1, "Ctrl and three notches up make it opaque again (%.2f)", p.Opacity())

	if text := mygo.Clipboard.ReadText(); text != "" {
		defer mygo.Clipboard.WriteText(text)
	}
	drive.Keys(drive.VKControl, drive.VKC)
	var err error
	select {
	case err = <-copied:
	case <-time.After(3 * time.Second):
		err = os.ErrDeadlineExceeded
	}
	l.Check(err == nil, "Ctrl+C copies (%v)", err)
	got := osClipboardImage()
	l.Check(got == "800x450", "another process reads an 800x450 bitmap from the clipboard (%q)", got)

	drive.Keys(drive.VKEscape)
	l.Check(drive.Until(2*time.Second, func() bool { return !drive.Visible(hwnd) }), "Esc closes the pin")

	p2 := pin.Open(stripes(640, 360), 1)
	time.Sleep(500 * time.Millisecond)
	h2 := p2.Window().NativeHandle()
	r2 := drive.WindowRect(h2)
	drive.DoubleClick(center(r2))
	l.Check(drive.Until(2*time.Second, func() bool { return !drive.Visible(h2) }), "a double click closes a pin")

	p3 := pin.Open(stripes(640, 360), 1)
	time.Sleep(500 * time.Millisecond)
	h3 := p3.Window().NativeHandle()
	r3 := drive.WindowRect(h3)
	drive.MoveTo(r3.X+r3.W/2, r3.Y+r3.H/2)
	time.Sleep(300 * time.Millisecond)
	drive.MoveTo(r3.X+r3.W-17, r3.Y+17)
	time.Sleep(300 * time.Millisecond)
	l.Shot("pin-close-button.png", platform.Rect{X: r3.X + r3.W - 120, Y: r3.Y, W: 120, H: 60})
	drive.Click(r3.X+r3.W-17, r3.Y+17)
	l.Check(drive.Until(2*time.Second, func() bool { return !drive.Visible(h3) }), "the close button closes a pin")
}

func workArea() platform.Rect {
	d := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint())
	k, wa := d.ScaleFactor, d.WorkArea
	return platform.Rect{X: int(float64(wa.X) * k), Y: int(float64(wa.Y) * k), W: int(float64(wa.Width) * k), H: int(float64(wa.Height) * k)}
}

func abs(x int) int { return max(x, -x) }
