package drive

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"golang.org/x/sys/windows"

	"github.com/wobsoriano/glint/internal/platform"
)

var (
	user32         = windows.NewLazySystemDLL("user32.dll")
	pSendInput     = user32.NewProc("SendInput")
	pSetCursorPos  = user32.NewProc("SetCursorPos")
	pForeground    = user32.NewProc("GetForegroundWindow")
	pSetForeground = user32.NewProc("SetForegroundWindow")
	pWindowRect    = user32.NewProc("GetWindowRect")
	pWindowText    = user32.NewProc("GetWindowTextW")
	pClassName     = user32.NewProc("GetClassNameW")
	pEnumWindows   = user32.NewProc("EnumWindows")
	pPostMessage   = user32.NewProc("PostMessageW")
	pIsWindow      = user32.NewProc("IsWindow")
	pIsVisible     = user32.NewProc("IsWindowVisible")
	pSetWindowPos  = user32.NewProc("SetWindowPos")
	pWindowFromPt  = user32.NewProc("WindowFromPoint")
	pGetAncestor   = user32.NewProc("GetAncestor")
	pGetLayered    = user32.NewProc("GetLayeredWindowAttributes")
)

// Log prints timed lines and counts failed checks.
type Log struct {
	start    time.Time
	Failures int
	// Dir is where Shot writes.
	Dir string
}

func NewLog(dir string) *Log {
	os.MkdirAll(dir, 0o755)
	return &Log{start: time.Now(), Dir: dir}
}

func (l *Log) Printf(format string, args ...any) {
	fmt.Printf("[%7.1fs] %s\n", time.Since(l.start).Seconds(), fmt.Sprintf(format, args...))
}

// Check logs PASS or FAIL and reports ok.
func (l *Log) Check(ok bool, format string, args ...any) bool {
	status := "PASS"
	if !ok {
		status = "FAIL"
		l.Failures++
	}
	l.Printf("%s %s", status, fmt.Sprintf(format, args...))
	return ok
}

// Finish quits the app with exit code 1 when a check failed.
func (l *Log) Finish() {
	l.Printf("done: %d check(s) failed", l.Failures)
	code := 0
	if l.Failures > 0 {
		code = 1
	}
	mygo.RunOnMain(func() { mygo.App.Exit(code) })
}

// Shot saves the screen inside r as a PNG in Dir and returns it.
func (l *Log) Shot(name string, r platform.Rect) *image.RGBA {
	img, err := platform.CaptureRect(r)
	if err != nil {
		l.Printf("shot %s: %v", name, err)
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	path := filepath.Join(l.Dir, name)
	if f, err := os.Create(path); err == nil {
		png.Encode(f, img)
		f.Close()
		l.Printf("saved %s", path)
	}
	return img
}

// Capture reads the screen inside r without saving it.
func Capture(r platform.Rect) *image.RGBA {
	img, err := platform.CaptureRect(r)
	if err != nil {
		return image.NewRGBA(image.Rect(0, 0, max(r.W, 1), max(r.H, 1)))
	}
	return img
}

// Diff counts the pixels of a and b that differ by more than tol in a channel.
func Diff(a, b *image.RGBA, tol int) int {
	if a.Rect.Size() != b.Rect.Size() {
		return -1
	}
	n := 0
	for i := 0; i+3 < len(a.Pix) && i+3 < len(b.Pix); i += 4 {
		for k := range 3 {
			if d := int(a.Pix[i+k]) - int(b.Pix[i+k]); d > tol || d < -tol {
				n++
				break
			}
		}
	}
	return n
}

type mouseInput struct {
	Type      uint32
	_         uint32
	DX, DY    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	Extra     uintptr
}

type keyInput struct {
	Type     uint32
	_        uint32
	VK, Scan uint16
	Flags    uint32
	Time     uint32
	Extra    uintptr
	_        [8]byte
}

func mouse(flags, data uint32) {
	in := mouseInput{Flags: flags, MouseData: data}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
}

// MoveTo puts the pointer at (x, y) in physical pixels and tells the window under it.
func MoveTo(x, y int) {
	pSetCursorPos.Call(uintptr(x), uintptr(y))
	mouse(0x0001, 0)
}

func Down() { mouse(0x0002, 0) }
func Up()   { mouse(0x0004, 0) }

func Click(x, y int) {
	MoveTo(x, y)
	time.Sleep(40 * time.Millisecond)
	Down()
	time.Sleep(30 * time.Millisecond)
	Up()
}

func DoubleClick(x, y int) {
	MoveTo(x, y)
	time.Sleep(40 * time.Millisecond)
	Down()
	Up()
	time.Sleep(60 * time.Millisecond)
	Down()
	Up()
}

// Drag presses at (x0, y0), moves to (x1, y1) in steps and lets go there.
func Drag(x0, y0, x1, y1, steps int, pause time.Duration) {
	MoveTo(x0, y0)
	time.Sleep(pause)
	Down()
	time.Sleep(pause)
	for i := 1; i <= steps; i++ {
		MoveTo(x0+(x1-x0)*i/steps, y0+(y1-y0)*i/steps)
		time.Sleep(pause)
	}
	time.Sleep(pause)
	Up()
}

// Wheel scrolls notches at (x, y), positive away from the user.
func Wheel(x, y, notches int) {
	MoveTo(x, y)
	time.Sleep(30 * time.Millisecond)
	step := 120
	if notches < 0 {
		step, notches = -120, -notches
	}
	for range notches {
		mouse(0x0800, uint32(int32(step)))
		time.Sleep(60 * time.Millisecond)
	}
}

const (
	VKControl = 0x11
	VKEscape  = 0x1B
	VKSpace   = 0x20
	VKC       = 'C'
)

// Keys presses the keys down in order and lets them go in reverse.
func Keys(vks ...uint16) {
	for _, vk := range vks {
		in := keyInput{Type: 1, VK: vk}
		pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
	for i := len(vks) - 1; i >= 0; i-- {
		in := keyInput{Type: 1, VK: vks[i], Flags: 0x0002}
		pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}
}

// WithCtrl runs fn with Ctrl held.
func WithCtrl(fn func()) {
	in := keyInput{Type: 1, VK: VKControl}
	pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	defer func() {
		in := keyInput{Type: 1, VK: VKControl, Flags: 0x0002}
		pSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	}()
	time.Sleep(30 * time.Millisecond)
	fn()
}

func Foreground() uintptr {
	h, _, _ := pForeground.Call()
	return h
}

func SetForeground(h uintptr) { pSetForeground.Call(h) }

func WindowText(h uintptr) string {
	buf := make([]uint16, 256)
	pWindowText.Call(h, uintptr(unsafe.Pointer(&buf[0])), 256)
	return syscall.UTF16ToString(buf)
}

func className(h uintptr) string {
	buf := make([]uint16, 128)
	pClassName.Call(h, uintptr(unsafe.Pointer(&buf[0])), 128)
	return syscall.UTF16ToString(buf)
}

// WindowRect is the window's rectangle in physical pixels.
func WindowRect(h uintptr) platform.Rect {
	var r struct{ L, T, R, B int32 }
	pWindowRect.Call(h, uintptr(unsafe.Pointer(&r)))
	return platform.Rect{X: int(r.L), Y: int(r.T), W: int(r.R - r.L), H: int(r.B - r.T)}
}

// SetWindowRect moves and sizes another window in physical pixels.
func SetWindowRect(h uintptr, r platform.Rect) {
	pSetWindowPos.Call(h, 0, uintptr(r.X), uintptr(r.Y), uintptr(r.W), uintptr(r.H), 0x0004)
}

// Visible reports whether the window exists and shows.
func Visible(h uintptr) bool {
	ok, _, _ := pIsWindow.Call(h)
	v, _, _ := pIsVisible.Call(h)
	return ok != 0 && v != 0
}

// RootAt is the top-level window under a screen point.
func RootAt(x, y int) uintptr {
	h, _, _ := pWindowFromPt.Call(uintptr(uint32(int32(x))) | uintptr(uint32(int32(y)))<<32)
	const gaRoot = 2
	root, _, _ := pGetAncestor.Call(h, gaRoot)
	return root
}

// LayeredAlpha is the window's LWA_ALPHA opacity, 255 for a window that is not layered.
func LayeredAlpha(h uintptr) int {
	var key uint32
	var alpha uint8
	var flags uint32
	ok, _, _ := pGetLayered.Call(h, uintptr(unsafe.Pointer(&key)), uintptr(unsafe.Pointer(&alpha)), uintptr(unsafe.Pointer(&flags)))
	if ok == 0 || flags&0x2 == 0 {
		return 255
	}
	return int(alpha)
}

// FindWindow waits up to d for a visible top-level window whose title starts with title.
func FindWindow(title string, d time.Duration) uintptr {
	for t0 := time.Now(); time.Since(t0) < d; time.Sleep(50 * time.Millisecond) {
		var found uintptr
		cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
			if Visible(h) && strings.HasPrefix(WindowText(h), title) {
				found = h
				return 0
			}
			return 1
		})
		pEnumWindows.Call(cb, 0)
		if found != 0 {
			return found
		}
	}
	return 0
}

// WaitForeground waits up to d for a window titled title to be the foreground window.
func WaitForeground(title string, d time.Duration) uintptr {
	for t0 := time.Now(); time.Since(t0) < d; time.Sleep(50 * time.Millisecond) {
		if h := Foreground(); WindowText(h) == title {
			return h
		}
	}
	return 0
}

// ForegroundTitle is the title of the other app the demos keep in front.
const ForegroundTitle = "Glint demo foreground"

// StartForeground runs this executable again with -foreground, a plain window of another
// process that holds the keyboard, and returns it with its window.
func StartForeground(l *Log) (*exec.Cmd, uintptr) {
	cmd := exec.Command(os.Args[0], "-foreground")
	if err := cmd.Start(); err != nil {
		l.Check(false, "start the foreground app: %v", err)
		return nil, 0
	}
	h := WaitForeground(ForegroundTitle, 5*time.Second)
	return cmd, h
}

// RunForeground is the -foreground mode: a window that takes the keyboard and sits at the
// screen's top middle, away from the bottom corners where the stack shows.
func RunForeground() {
	mygo.App.WhenReady(func() {
		w := mygo.NewWindow(mygo.WindowOptions{Title: ForegroundTitle, X: 640, Y: 120, Width: 640, Height: 400, Content: ui.View(func(c *ui.Context) {
			ui.Column(c).Fill().Center().Background(ui.RGB(0x1F, 0x5F, 0x3F)).Children(func() {
				ui.Text(c, "another app").FontSize(24).TextColor(ui.RGB(255, 255, 255))
			})
		})})
		w.Focus()
	})
}

// OpenExplorer opens a folder window and returns it once it shows.
func OpenExplorer(dir string) uintptr {
	exec.Command("explorer.exe", dir).Start()
	name := filepath.Base(dir)
	for range 80 {
		time.Sleep(100 * time.Millisecond)
		var found uintptr
		cb := syscall.NewCallback(func(h, _ uintptr) uintptr {
			if className(h) == "CabinetWClass" && strings.HasPrefix(WindowText(h), name) {
				found = h
				return 0
			}
			return 1
		})
		pEnumWindows.Call(cb, 0)
		if found != 0 {
			return found
		}
	}
	return 0
}

// CloseWindow asks a window to close and reports whether it went.
func CloseWindow(h uintptr) bool {
	const wmClose = 0x0010
	pPostMessage.Call(h, wmClose, 0, 0)
	for range 60 {
		time.Sleep(50 * time.Millisecond)
		if ok, _, _ := pIsWindow.Call(h); ok == 0 {
			return true
		}
	}
	return false
}

// WaitFile waits up to d for path to exist.
func WaitFile(path string, d time.Duration) bool {
	for t0 := time.Now(); time.Since(t0) < d; time.Sleep(50 * time.Millisecond) {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// Until polls cond every 50 ms for up to d.
func Until(d time.Duration, cond func() bool) bool {
	for t0 := time.Now(); time.Since(t0) < d; time.Sleep(50 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return cond()
}
