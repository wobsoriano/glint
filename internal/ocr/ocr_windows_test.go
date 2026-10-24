package ocr

import (
	"fmt"
	"image"
	"image/draw"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unsafe"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
	"golang.org/x/sys/windows"
)

// placed is a line of text drawn at x with its baseline at y.
type placed struct {
	text string
	x, y int
}

func segoe(t testing.TB, size float64) font.Face {
	t.Helper()
	data, err := os.ReadFile(`C:\Windows\Fonts\segoeui.ttf`)
	if err != nil {
		t.Skipf("Segoe UI not available: %v", err)
	}
	f, err := opentype.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	face, err := opentype.NewFace(f, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull})
	if err != nil {
		t.Fatal(err)
	}
	return face
}

func render(face font.Face, w, h int, lines []placed) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Rect, image.White, image.Point{}, draw.Src)
	for _, l := range lines {
		d := font.Drawer{Dst: img, Src: image.Black, Face: face, Dot: fixed.P(l.x, l.y)}
		d.DrawString(l.text)
	}
	return img
}

// inkOf is the box the glyphs of word cover in a line drawn at l, where prefix is the text
// before the word.
func inkOf(face font.Face, l placed, prefix, word string) Rect {
	b, _ := font.BoundString(face, word)
	x := float32(l.x) + float32(font.MeasureString(face, prefix))/64
	return Rect{
		X: x + float32(b.Min.X)/64,
		Y: float32(l.y) + float32(b.Min.Y)/64,
		W: float32(b.Max.X-b.Min.X) / 64,
		H: float32(b.Max.Y-b.Min.Y) / 64,
	}
}

func iou(a, b Rect) float32 {
	i := a.overlap(b)
	return i / (a.area() + b.area() - i)
}

var twoColumns = []placed{
	{"The quick brown fox jumps", 40, 60},
	{"over the lazy dog near", 40, 110},
	{"a small river bank today", 40, 160},
	{"Contact: jane.doe@example.com", 40, 210},
	{"Capture Text copies words", 700, 60},
	{"from any screenshot you take", 700, 110},
	{"Glint keeps them in order", 700, 160},
	{"Call +1 415-555-0132 now", 700, 210},
}

const twoColumnsText = "The quick brown fox jumps  Capture Text copies words\n" +
	"over the lazy dog near  from any screenshot you take\n" +
	"a small river bank today  Glint keeps them in order\n" +
	"Contact: jane.doe@example.com  Call +1 415-555-0132 now"

func TestRecognizeTwoColumns(t *testing.T) {
	face := segoe(t, 28)
	img := render(face, 1280, 250, twoColumns)
	lines, err := Recognize(img)
	if err != nil {
		t.Fatal(err)
	}
	if got := Text(lines); got != twoColumnsText {
		t.Fatalf("text:\n%s\nwant:\n%s", got, twoColumnsText)
	}

	// Every word's box must sit on the glyphs drawn for it, in the image's pixels, which
	// proves the boxes are mapped back from the 2x upscale the engine read.
	for _, want := range twoColumns {
		i := slices.IndexFunc(lines, func(l Line) bool { return l.Text() == want.text })
		if i < 0 {
			t.Fatalf("no line %q", want.text)
		}
		prefix := ""
		for j, w := range lines[i].Words {
			if j > 0 {
				prefix += " "
			}
			box := inkOf(face, want, prefix, w.Text)
			if q := iou(w.Bounds, box); q < 0.6 {
				t.Errorf("%q: box %+v, glyphs at %+v, IoU %.2f", w.Text, w.Bounds, box, q)
			}
			prefix += w.Text
		}
	}
}

func TestRecognizeSubImageKeepsItsCoordinates(t *testing.T) {
	face := segoe(t, 28)
	full := render(face, 1280, 250, twoColumns)
	right := full.SubImage(image.Rect(640, 0, 1280, 250)).(*image.RGBA)
	lines, err := Recognize(right)
	if err != nil {
		t.Fatal(err)
	}
	want := "Capture Text copies words\nfrom any screenshot you take\nGlint keeps them in order\nCall +1 415-555-0132 now"
	if got := Text(lines); got != want {
		t.Fatalf("text:\n%s\nwant:\n%s", got, want)
	}
	first := lines[0].Words[0]
	box := inkOf(face, twoColumns[4], "", first.Text)
	if q := iou(first.Bounds, box); q < 0.6 {
		t.Errorf("%q: box %+v, glyphs at %+v, IoU %.2f", first.Text, first.Bounds, box, q)
	}
}

// columnPage fills a w by h page with cols columns of lines of common words, and returns it
// with the text Recognize must read from it. The words leave out digits next to letters:
// at 20px the engine reads Segoe UI's "B1" as "Bl" and "A11" as "A1 1".
func columnPage(t testing.TB, w, h int, size float64, cols int) (*image.RGBA, string) {
	words := strings.Fields("time year people way day man thing woman life child world school " +
		"state family student group country problem hand part place case week company system " +
		"program question work number night point home water room mother area money story " +
		"fact month lot right study book eye job word business issue side kind head house")
	face := segoe(t, size)
	pitch := int(size * 1.8)
	colW := w / cols
	rows := (h - pitch) / pitch
	var lines []placed
	grid := make([][]string, rows)
	seed := uint32(7)
	for r := range rows {
		for c := range cols {
			var parts []string
			for range 5 {
				seed = seed*1664525 + 1013904223
				parts = append(parts, words[seed>>8%uint32(len(words))])
			}
			text := strings.Join(parts, " ")
			grid[r] = append(grid[r], text)
			lines = append(lines, placed{text, c*colW + int(size), pitch * (r + 1)})
		}
	}
	rowText := make([]string, rows)
	for r, cells := range grid {
		rowText[r] = strings.Join(cells, "  ")
	}
	return render(face, w, h, lines), strings.Join(rowText, "\n")
}

func TestRecognizeScreenSizedPages(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	for _, c := range []struct {
		w, h int
		size float64
	}{
		{1920, 1080, 20},
		{3840, 2160, 40},
	} {
		t.Run(fmt.Sprintf("%dx%d", c.w, c.h), func(t *testing.T) {
			img, want := columnPage(t, c.w, c.h, c.size, 3)
			lines, err := Recognize(img)
			if err != nil {
				t.Fatal(err)
			}
			got := Text(lines)
			if got != want {
				t.Fatalf("text differs at line %d\ngot:\n%s\nwant:\n%s", firstDiffLine(got, want), got, want)
			}
			t.Logf("%d lines, text matches; %s", len(lines), timeRecognize(t, img))
		})
	}
}

// An image taller than the engine's 10000 pixel limit is read in overlapping tiles. Lines
// inside the overlap must come out once, and lines across a tile's edge must come out whole.
func TestRecognizeTallImageInTiles(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	img, want := columnPage(t, 1000, 10500, 28, 1)
	lines, err := Recognize(img)
	if err != nil {
		t.Fatal(err)
	}
	if got := Text(lines); got != want {
		t.Fatalf("text differs at line %d\ngot:\n%s\nwant:\n%s", firstDiffLine(got, want), got, want)
	}
}

func firstDiffLine(a, b string) int {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := range min(len(al), len(bl)) {
		if al[i] != bl[i] {
			return i + 1
		}
	}
	return min(len(al), len(bl)) + 1
}

func TestRecognizeScreen(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	shot := captureScreen(t)
	big := image.NewRGBA(image.Rect(0, 0, 3840, 2160))
	xdraw.CatmullRom.Scale(big, big.Rect, shot, shot.Rect, xdraw.Src, nil)
	for _, img := range []*image.RGBA{shot, big} {
		lines, err := Recognize(img)
		if err != nil {
			t.Fatal(err)
		}
		text := []rune(Text(lines))
		t.Logf("screen %dx%d: %d lines; %s; starts %q", img.Rect.Dx(), img.Rect.Dy(), len(lines),
			timeRecognize(t, img), string(text[:min(len(text), 200)]))
	}
}

// timeRecognize runs Recognize five times and reports the wall time and how many cores the
// process kept busy meanwhile.
func timeRecognize(t testing.TB, img *image.RGBA) string {
	var walls []time.Duration
	var cores []float64
	for range 5 {
		cpu0 := processCPU()
		t0 := time.Now()
		if _, err := Recognize(img); err != nil {
			t.Fatal(err)
		}
		wall := time.Since(t0)
		walls = append(walls, wall)
		cores = append(cores, float64(processCPU()-cpu0)/float64(wall))
	}
	slices.Sort(walls)
	slices.Sort(cores)
	return fmt.Sprintf("5 runs: median %v, range %v to %v, median %.1f cores busy",
		walls[2].Round(time.Millisecond), walls[0].Round(time.Millisecond), walls[4].Round(time.Millisecond), cores[2])
}

func processCPU() time.Duration {
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(windows.CurrentProcess(), &c, &e, &k, &u); err != nil {
		panic(err)
	}
	ft := func(f windows.Filetime) time.Duration {
		return time.Duration(uint64(f.HighDateTime)<<32|uint64(f.LowDateTime)) * 100
	}
	return ft(k) + ft(u)
}

func TestRecognizeConcurrently(t *testing.T) {
	img := render(segoe(t, 28), 1280, 250, twoColumns)
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for g := range 4 {
		wg.Go(func() {
			for range 3 {
				lines, err := Recognize(img)
				if err == nil && Text(lines) != twoColumnsText {
					err = fmt.Errorf("text:\n%s", Text(lines))
				}
				if err != nil {
					errs[g] = err
					return
				}
			}
		})
	}
	wg.Wait()
	for g, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", g, err)
		}
	}
}

func TestRecognizeDoesNotLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("slow")
	}
	img := render(segoe(t, 28), 1280, 250, twoColumns)
	run := func(n int) {
		for range n {
			lines, err := Recognize(img)
			if err != nil {
				t.Fatal(err)
			}
			if Text(lines) != twoColumnsText {
				t.Fatalf("text:\n%s", Text(lines))
			}
		}
	}
	run(20)
	before := usage()
	run(200)
	after := usage()
	t.Logf("before 200 calls: %s", before)
	t.Logf("after 200 calls:  %s", after)
	// Each call makes about 40 HSTRINGs and 50 COM objects for the words and lines it reads,
	// and a 5 MB bitmap. Leaking any one of them every call grows the heaps by over 300 KB.
	if grew := after.heap - before.heap; grew > 256<<10 {
		t.Errorf("native heaps grew by %d KB", grew>>10)
	}
	if grew := after.private - before.private; grew > 8<<20 {
		t.Errorf("private memory grew by %d KB", grew>>10)
	}
	if grew := after.handles - before.handles; grew > 20 {
		t.Errorf("handle count grew by %d", grew)
	}
}

type memUsage struct {
	private, workingSet int // bytes the OS committed to the process, and of them resident
	heap                int // bytes allocated in the process's native heaps
	handles             int
}

func (m memUsage) String() string {
	return fmt.Sprintf("native heaps %d KB, private %d KB, working set %d KB, %d handles",
		m.heap>>10, m.private>>10, m.workingSet>>10, m.handles)
}

var (
	kernel32                    = windows.NewLazySystemDLL("kernel32.dll")
	procK32GetProcessMemoryInfo = kernel32.NewProc("K32GetProcessMemoryInfo")
	procGetProcessHandleCount   = kernel32.NewProc("GetProcessHandleCount")
	procGetProcessHeaps         = kernel32.NewProc("GetProcessHeaps")
	procHeapSummary             = kernel32.NewProc("HeapSummary")
)

func usage() memUsage {
	runtime.GC()
	debug.FreeOSMemory()
	var m memUsage
	n, _, _ := procGetProcessHeaps.Call(0, 0)
	heaps := make([]windows.Handle, n)
	n, _, _ = procGetProcessHeaps.Call(n, uintptr(unsafe.Pointer(&heaps[0])))
	for _, h := range heaps[:n] {
		var s struct {
			cb                                         uint32
			allocated, committed, reserved, maxReserve uintptr
		}
		s.cb = uint32(unsafe.Sizeof(s))
		if r, _, _ := procHeapSummary.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&s))); r != 0 {
			m.heap += int(s.allocated)
		}
	}
	var c struct {
		cb                                                   uint32
		pageFaultCount                                       uint32
		peakWorkingSet, workingSet                           uintptr
		quotaPeakPaged, quotaPaged, quotaPeakNonPaged, quota uintptr
		pagefile, peakPagefile, private                      uintptr
	}
	c.cb = uint32(unsafe.Sizeof(c))
	p := windows.CurrentProcess()
	if r, _, err := procK32GetProcessMemoryInfo.Call(uintptr(p), uintptr(unsafe.Pointer(&c)), uintptr(c.cb)); r == 0 {
		panic(err)
	}
	var handles uint32
	if r, _, err := procGetProcessHandleCount.Call(uintptr(p), uintptr(unsafe.Pointer(&handles))); r == 0 {
		panic(err)
	}
	m.private, m.workingSet, m.handles = int(c.private), int(c.workingSet), int(handles)
	return m
}

var (
	user32                           = windows.NewLazySystemDLL("user32.dll")
	gdi32                            = windows.NewLazySystemDLL("gdi32.dll")
	procGetDC                        = user32.NewProc("GetDC")
	procReleaseDC                    = user32.NewProc("ReleaseDC")
	procGetSystemMetrics             = user32.NewProc("GetSystemMetrics")
	procSetThreadDpiAwarenessContext = user32.NewProc("SetThreadDpiAwarenessContext")
	procCreateCompatibleDC           = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap       = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject                 = gdi32.NewProc("SelectObject")
	procBitBlt                       = gdi32.NewProc("BitBlt")
	procGetDIBits                    = gdi32.NewProc("GetDIBits")
	procDeleteObject                 = gdi32.NewProc("DeleteObject")
	procDeleteDC                     = gdi32.NewProc("DeleteDC")
)

// captureScreen copies the primary display's pixels with GDI.
func captureScreen(t testing.TB) *image.RGBA {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const perMonitorAwareV2 = ^uintptr(3) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2, -4
	old, _, _ := procSetThreadDpiAwarenessContext.Call(perMonitorAwareV2)
	defer procSetThreadDpiAwarenessContext.Call(old)

	w, _, _ := procGetSystemMetrics.Call(0)
	h, _, _ := procGetSystemMetrics.Call(1)
	screen, _, _ := procGetDC.Call(0)
	defer procReleaseDC.Call(0, screen)
	mem, _, _ := procCreateCompatibleDC.Call(screen)
	defer procDeleteDC.Call(mem)
	bmp, _, _ := procCreateCompatibleBitmap.Call(screen, w, h)
	defer procDeleteObject.Call(bmp)
	prev, _, _ := procSelectObject.Call(mem, bmp)
	const srcCopy, captureBlt = 0x00CC0020, 0x40000000
	if r, _, err := procBitBlt.Call(mem, 0, 0, w, h, screen, 0, 0, srcCopy|captureBlt); r == 0 {
		t.Fatalf("BitBlt: %v", err)
	}
	procSelectObject.Call(mem, prev)

	type bitmapInfoHeader struct {
		size                          uint32
		width, height                 int32
		planes, bitCount              uint16
		compression, sizeImage        uint32
		xPels, yPels, used, important uint32
	}
	info := struct {
		hdr    bitmapInfoHeader
		colors [4]uint32
	}{hdr: bitmapInfoHeader{size: 40, width: int32(w), height: -int32(h), planes: 1, bitCount: 32}}
	img := image.NewRGBA(image.Rect(0, 0, int(w), int(h)))
	if r, _, err := procGetDIBits.Call(mem, bmp, 0, h, uintptr(unsafe.Pointer(&img.Pix[0])), uintptr(unsafe.Pointer(&info)), 0); r == 0 {
		t.Fatalf("GetDIBits: %v", err)
	}
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+2], img.Pix[i+3] = img.Pix[i+2], img.Pix[i], 255
	}
	return img
}
