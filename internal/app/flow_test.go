package app

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/overlay"
)

// screen is a fake virtual desktop: two displays side by side, the right one at 150%.
type screen struct {
	t         *testing.T
	pixels    *image.RGBA
	ds        []platform.Display
	wins      []platform.Window
	fg        *platform.Window
	pointer   image.Point
	lines     []ocr.Line
	winErr    error
	rects     []platform.Rect
	windowsAt []int64
	shadows   []bool
}

func newScreen(t *testing.T) *screen {
	img := image.NewRGBA(image.Rect(0, 0, 500, 200))
	for y := range 200 {
		for x := range 500 {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x ^ y), 255})
		}
	}
	return &screen{t: t, pixels: img, ds: []platform.Display{
		{ID: 1, Name: "Display 1", Bounds: platform.Rect{W: 200, H: 200}, Scale: 1, Primary: true},
		{ID: 2, Name: "Display 2", Bounds: platform.Rect{X: 200, W: 300, H: 200}, Scale: 1.5},
	}}
}

func (s *screen) displays() ([]platform.Display, error) { return s.ds, nil }
func (s *screen) captureRect(r platform.Rect) (*image.RGBA, error) {
	s.rects = append(s.rects, r)
	return crop(s.pixels, image.Rect(r.X, r.Y, r.X+r.W, r.Y+r.H)), nil
}
func (s *screen) captureWindow(id int64, shadow bool) (*image.RGBA, error) {
	s.windowsAt, s.shadows = append(s.windowsAt, id), append(s.shadows, shadow)
	if s.winErr != nil {
		return nil, s.winErr
	}
	return solid(40, 30, color.RGBA{9, 9, 9, 255}), nil
}
func (s *screen) windows() []platform.Window { return s.wins }
func (s *screen) cursor() image.Point        { return s.pointer }
func (s *screen) foreground() (platform.Window, bool) {
	if s.fg == nil {
		return platform.Window{}, false
	}
	return *s.fg, true
}
func (s *screen) recognize(*image.RGBA) ([]ocr.Line, error) { return s.lines, nil }

type fakePicker struct {
	shots []overlay.Shot
	opts  overlay.Options
	open  bool
}

func (p *fakePicker) Open(shots []overlay.Shot, opts overlay.Options) {
	p.shots, p.opts, p.open = shots, opts, true
}
func (p *fakePicker) IsOpen() bool { return p.open }

// pick is the user choosing r on the display the fake overlay showed first.
func (p *fakePicker) pick(r image.Rectangle, w *platform.Window) {
	sh := p.shots[0]
	p.open = false
	p.opts.OnPick(overlay.Pick{
		Mode:   p.opts.Mode,
		Rect:   platform.Rect{X: sh.Display.Bounds.X + r.Min.X, Y: sh.Display.Bounds.Y + r.Min.Y, W: r.Dx(), H: r.Dy()},
		Image:  crop(sh.Image, r),
		Window: w,
		Scale:  sh.Display.Scale,
	})
}

type fakeActs struct {
	copied []image.Image
	texts  []*image.RGBA
	saved  []store.Item
}

func (f *fakeActs) CopyImage(img image.Image, done func(error)) {
	f.copied = append(f.copied, img)
	done(nil)
}
func (f *fakeActs) Save(item store.Item, ask bool, done func(string, error)) {
	f.saved = append(f.saved, item)
	done(filepath.Join("exports", item.Meta.Title+".png"), nil)
}
func (f *fakeActs) ReportSave(path string, err error) {}
func (f *fakeActs) CopyTextOf(img *image.RGBA)        { f.texts = append(f.texts, img) }

type harness struct {
	*App
	screen *screen
	picker *fakePicker
	acts   *fakeActs
	mu     sync.Mutex
	toasts []string
	idle   sync.WaitGroup
	cards  []store.Item
}

func newHarness(t *testing.T, change func(*settings.Settings)) *harness {
	t.Setenv("GLINT_DATA_DIR", t.TempDir())
	h := &harness{screen: newScreen(t), picker: &fakePicker{}, acts: &fakeActs{}}
	s := settings.Default()
	s.SaveDir = filepath.Join(t.TempDir(), "Exports")
	if change != nil {
		change(&s)
	}
	h.App = &App{
		settings: s,
		library:  store.New(store.DefaultRoot()),
		sys:      h.screen,
		picker:   h.picker,
		acts:     h.acts,
		hotkeys:  map[settings.HotkeyAction]string{},
		failures: map[settings.HotkeyAction]error{},
	}
	h.say = func(m string) { h.mu.Lock(); h.toasts = append(h.toasts, m); h.mu.Unlock() }
	var main sync.Mutex
	h.post = func(fn func()) { main.Lock(); defer main.Unlock(); fn(); h.idle.Done() }
	h.Surfaces.ShowCard = func(it store.Item) { h.cards = append(h.cards, it) }
	return h
}

// do runs fn as the main thread would, and waits for the goroutines it starts.
func (h *harness) do(posts int, fn func()) {
	h.idle.Add(posts)
	fn()
	done := make(chan struct{})
	go func() { h.idle.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		panic("the flow did not finish")
	}
}

func (h *harness) items(t *testing.T) []store.Item {
	t.Helper()
	return h.library.Scan()
}

func TestPlanFollowsTheRustOrder(t *testing.T) {
	for _, c := range []struct {
		after settings.AfterShot
		want  []step
	}{
		{settings.AfterShot{Preview: true, Copy: true}, []step{stepCopy, stepCard}},
		{settings.AfterShot{Copy: true}, []step{stepCopy, stepCopiedToast}},
		{settings.AfterShot{Copy: true, Pin: true}, []step{stepCopy, stepPin}},
		{settings.AfterShot{Copy: true, Save: true, Edit: true, Pin: true, Preview: true}, []step{stepCopy, stepSave, stepPin, stepEdit, stepCard}},
		{settings.AfterShot{Save: true}, []step{stepSave}},
		{settings.AfterShot{}, nil},
	} {
		if got := plan(c.after); !slices.Equal(got, c.want) {
			t.Errorf("%+v: got %v, want %v", c.after, got, c.want)
		}
	}
}

func TestAnAreaPickImportsTheFrozenPixelsThenCopiesAndShowsTheCard(t *testing.T) {
	h := newHarness(t, nil)
	h.screen.pointer = image.Pt(50, 50)
	h.Do(CmdCaptureArea)
	if !h.picker.open || len(h.picker.shots) != 2 || h.picker.opts.Mode != overlay.ModeImage || !h.picker.opts.ShowLoupe {
		t.Fatalf("overlay opened with %d shots, %+v", len(h.picker.shots), h.picker.opts)
	}
	if got := h.picker.shots[1].Image.Rect; got.Dx() != 300 || got.Dy() != 200 {
		t.Fatalf("second display frozen at %v", got)
	}
	r := image.Rect(30, 40, 130, 90)
	h.do(1, func() { h.picker.pick(r, nil) })

	items := h.items(t)
	if len(items) != 1 {
		t.Fatalf("Library holds %d items", len(items))
	}
	got := readPNG(t, items[0].Deliverable())
	if !samePixels(got, crop(h.screen.pixels, r)) {
		t.Fatal("image.png is not the frozen pixels of the picked area")
	}
	if items[0].Meta.Source != nil {
		t.Fatalf("an area has no source, got %q", *items[0].Meta.Source)
	}
	if len(h.acts.copied) != 1 || !samePixels(h.acts.copied[0], crop(h.screen.pixels, r)) {
		t.Fatal("the capture was not copied")
	}
	if len(h.cards) != 1 || h.cards[0].Dir != items[0].Dir {
		t.Fatal("no preview card for the capture")
	}
	saved := settings.Load()
	if saved.LastArea == nil || *saved.LastArea != (settings.Area{30, 40, 100, 50}) {
		t.Fatalf("last area saved as %v", saved.LastArea)
	}
	if h.last != items[0].Dir {
		t.Fatal("the capture is not the last one")
	}
}

func TestCaptureTextCopiesTheTextAndAddsNothing(t *testing.T) {
	h := newHarness(t, nil)
	h.Do(CmdCaptureText)
	if h.picker.opts.Mode != overlay.ModeText {
		t.Fatal("the overlay is not in text mode")
	}
	r := image.Rect(10, 10, 90, 40)
	h.picker.pick(r, nil)
	if len(h.acts.texts) != 1 || !samePixels(h.acts.texts[0], crop(h.screen.pixels, r)) {
		t.Fatal("the picked pixels were not read for text")
	}
	if n := len(h.items(t)); n != 0 {
		t.Fatalf("Capture Text added %d items", n)
	}
}

func TestFullScreenTakesTheDisplayWithThePointer(t *testing.T) {
	h := newHarness(t, func(s *settings.Settings) { s.AfterShot = settings.AfterShot{} })
	h.screen.pointer = image.Pt(350, 20)
	h.do(1, func() { h.Do(CmdCaptureScreen) })
	if want := (platform.Rect{X: 200, W: 300, H: 200}); !slices.Equal(h.screen.rects, []platform.Rect{want}) {
		t.Fatalf("captured %v", h.screen.rects)
	}
	items := h.items(t)
	if len(items) != 1 || items[0].Meta.Width != 300 {
		t.Fatalf("imported %+v", items)
	}
	if len(h.acts.copied)+len(h.cards) != 0 {
		t.Fatal("automation ran with every step off")
	}
}

func TestWindowCaptureTakesTheForegroundWindowWithItsShadow(t *testing.T) {
	h := newHarness(t, func(s *settings.Settings) { s.WindowShadow = true })
	h.screen.fg = &platform.Window{ID: 77, Title: "Notes", Bounds: platform.Rect{X: 220, Y: 10, W: 40, H: 30}}
	h.do(2, func() { h.Do(CmdCaptureWindow) })
	if !slices.Equal(h.screen.windowsAt, []int64{77}) || !slices.Equal(h.screen.shadows, []bool{true}) {
		t.Fatalf("captured windows %v with shadows %v", h.screen.windowsAt, h.screen.shadows)
	}
	items := h.items(t)
	if len(items) != 1 || items[0].Meta.Source == nil || *items[0].Meta.Source != "Notes" {
		t.Fatalf("imported %+v", items)
	}

	h.screen.winErr = errors.New("PrintWindow failed")
	h.do(2, func() { h.Do(CmdCaptureWindow) })
	if last := h.screen.rects[len(h.screen.rects)-1]; last != h.screen.fg.Bounds {
		t.Fatalf("a failed window capture does not read the screen at the window: %v", last)
	}
	h.screen.winErr = nil

	h.screen.fg = nil
	h.do(1, func() { h.Do(CmdCaptureWindow) })
	if last := h.screen.rects[len(h.screen.rects)-1]; last != (platform.Rect{W: 200, H: 200}) {
		t.Fatalf("with no window in front the display was not captured: %v", last)
	}
}

func TestClickingAWindowInTheOverlayCapturesItLiveOrFallsBack(t *testing.T) {
	h := newHarness(t, func(s *settings.Settings) { s.WindowShadow = false })
	w := platform.Window{ID: 5, Title: "Editor", Bounds: platform.Rect{X: 10, Y: 10, W: 60, H: 50}}
	h.Do(CmdCaptureArea)
	h.do(2, func() { h.picker.pick(image.Rect(10, 10, 70, 60), &w) })
	if !slices.Equal(h.screen.windowsAt, []int64{5}) || h.screen.shadows[0] {
		t.Fatalf("window capture %v shadow %v", h.screen.windowsAt, h.screen.shadows)
	}
	if got := readPNG(t, h.items(t)[0].Deliverable()); got.Bounds().Dx() != 40 {
		t.Fatalf("the live window capture was not kept: %v", got.Bounds())
	}

	h.screen.winErr = errors.New("minimized")
	h.Do(CmdCaptureArea)
	h.do(2, func() { h.picker.pick(image.Rect(10, 10, 70, 60), &w) })
	newest := h.items(t)[0]
	if got := readPNG(t, newest.Deliverable()); !samePixels(got, crop(h.screen.pixels, image.Rect(10, 10, 70, 60))) {
		t.Fatal("a failed window capture does not keep the frozen pixels")
	}
}

func TestPreviousAreaCapturesTheSavedRectOrOpensTheOverlay(t *testing.T) {
	h := newHarness(t, nil)
	h.Do(CmdCapturePrevious)
	if !h.picker.open {
		t.Fatal("with no previous area the overlay opens")
	}
	h.picker.open = false
	h.settings.LastArea = &settings.Area{180, 20, 100, 40}
	h.screen.rects = nil
	h.do(1, func() { h.Do(CmdCapturePrevious) })
	if want := (platform.Rect{X: 200, Y: 20, W: 80, H: 40}); !slices.Equal(h.screen.rects, []platform.Rect{want}) {
		t.Fatalf("captured %v, want the part on the display holding the centre", h.screen.rects)
	}
}

func TestAutomaticRedactionStoresAnEditableRedaction(t *testing.T) {
	h := newHarness(t, func(s *settings.Settings) { s.AutoRedact = true; s.AfterShot = settings.AfterShot{} })
	h.screen.lines = []ocr.Line{{
		Words:  []ocr.Word{{Text: "jane.doe@example.com", Bounds: ocr.Rect{X: 20, Y: 20, W: 120, H: 14}}},
		Bounds: ocr.Rect{X: 20, Y: 20, W: 120, H: 14},
	}}
	h.screen.pointer = image.Pt(10, 10)
	h.do(2, func() { h.Do(CmdCaptureScreen) })
	items := h.items(t)
	if len(items) != 1 || !items[0].IsEdited() {
		t.Fatalf("imported %+v", items)
	}
	data, err := os.ReadFile(filepath.Join(items[0].Dir, "edit.json"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := render.ParseEditDoc(data)
	if err != nil || len(doc.Annotations) != 1 || !doc.Annotations[0].IsRedaction() {
		t.Fatalf("edit.json %s: %v", data, err)
	}
	if _, ok := doc.Style.Background.(render.NoBackground); !ok || doc.Style.Padding != 0 {
		t.Fatalf("a redaction doc frames nothing: %+v", doc.Style)
	}
	base := readPNG(t, filepath.Join(items[0].Dir, "base.png"))
	shown := readPNG(t, items[0].Deliverable())
	if !samePixels(base, crop(h.screen.pixels, image.Rect(0, 0, 200, 200))) || samePixels(base, shown) {
		t.Fatal("base.png keeps the pixels and image.png hides the email")
	}
	if !slices.Contains(h.toasts, "Hid 1 sensitive item(s)") {
		t.Fatalf("toasts %q", h.toasts)
	}
}

func TestSaveRunsWhenTheSettingsAsk(t *testing.T) {
	h := newHarness(t, func(s *settings.Settings) { s.AfterShot = settings.AfterShot{Save: true} })
	h.do(1, func() { h.Do(CmdCaptureScreen) })
	if len(h.acts.saved) != 1 || h.acts.saved[0].Dir != h.items(t)[0].Dir {
		t.Fatal("the capture was not saved")
	}
}

func TestSurfacesNotBuiltYetSaySo(t *testing.T) {
	h := newHarness(t, func(s *settings.Settings) { s.AfterShot = settings.AfterShot{Preview: true, Pin: true, Edit: true} })
	h.Surfaces = Surfaces{}
	h.Do(CmdLibrary)
	h.Do(CmdSettings)
	h.Do(CmdRecord)
	h.do(1, func() { h.Do(CmdCaptureScreen) })
	for _, want := range []string{"The Library is not available yet", "Settings is not available yet", "Recording is not available yet",
		"Pinning is not available yet", "The editor is not available yet", "The preview card is not available yet"} {
		if !slices.Contains(h.toasts, want) {
			t.Errorf("no toast %q in %q", want, h.toasts)
		}
	}
}

func TestLaunchesRouteToCommands(t *testing.T) {
	h := newHarness(t, nil)
	var opened int
	h.Surfaces.OpenLibrary = func() { opened++ }
	h.open(Launch{})
	h.open(Launch{Background: true})
	h.open(Launch{Requests: []Request{{Kind: RequestLibrary}}})
	if opened != 2 {
		t.Fatalf("Library opened %d times, want 2: a plain launch and --library", opened)
	}
	h.open(Launch{Background: true, Requests: []Request{{Kind: RequestCapture, Capture: CaptureArea}}})
	if !h.picker.open {
		t.Fatal("--capture area opens the overlay")
	}
	var trimmed []string
	h.Surfaces.OpenTrim = func(p string) { trimmed = append(trimmed, p) }
	h.open(Launch{Requests: []Request{{Kind: RequestTrim, Path: `C:\v\talk.mov`}, {Kind: RequestTrim, Path: `C:\v\notes.txt`}}})
	if !slices.Equal(trimmed, []string{`C:\v\talk.mov`}) || !slices.ContainsFunc(h.toasts, func(m string) bool { return strings.HasPrefix(m, "notes.txt is not a video") }) {
		t.Fatalf("trim %q, toasts %q", trimmed, h.toasts)
	}
}

func TestOpeningAnImageImportsItAndOpensTheEditor(t *testing.T) {
	h := newHarness(t, nil)
	var edited []store.Item
	h.Surfaces.OpenEditor = func(it store.Item) { edited = append(edited, it) }
	path := filepath.Join(t.TempDir(), "photo.png")
	writePNG(t, path, solid(12, 8, color.RGBA{1, 2, 3, 255}))
	h.do(1, func() { h.open(Launch{Requests: []Request{{Kind: RequestOpen, Path: path}}}) })
	if len(edited) != 1 || edited[0].Meta.ImportedFrom == nil {
		t.Fatalf("edited %+v", edited)
	}
	h.do(1, func() { h.open(Launch{Requests: []Request{{Kind: RequestOpen, Path: edited[0].Deliverable()}}}) })
	if len(edited) != 2 || edited[1].Dir != edited[0].Dir || len(h.items(t)) != 1 {
		t.Fatal("a Library capture opens as it is, without a second import")
	}
}

func TestEditLastCaptureFallsBackToTheNewestScreenshot(t *testing.T) {
	h := newHarness(t, nil)
	var edited []store.Item
	h.Surfaces.OpenEditor = func(it store.Item) { edited = append(edited, it) }
	h.do(1, h.openLast)
	if !slices.Contains(h.toasts, "No captures yet") {
		t.Fatalf("toasts %q", h.toasts)
	}
	if _, err := h.library.ImportScreenshot(store.NewScreenshot{Base: solid(4, 4, color.RGBA{A: 255}), Created: time.Now()}); err != nil {
		t.Fatal(err)
	}
	h.do(1, h.openLast)
	if len(edited) != 1 {
		t.Fatal("Edit Last Capture did not open the Library's newest screenshot")
	}
}

func TestTrayMenuShowsShortcutsAndTheTimer(t *testing.T) {
	s := settings.Default()
	s.TimerSeconds = 10
	var labels []string
	for _, e := range trayMenu(s) {
		labels = append(labels, e.menuLabel()+"|"+e.hotkey)
	}
	want := []string{"Capture Area|Ctrl+Shift+4", "Capture Window|Ctrl+Shift+5", "Capture Full Screen|Ctrl+Shift+3",
		"Capture Previous Area|Ctrl+Shift+6", "Capture Text|Ctrl+Shift+2", "Self-Timer (10 s)|", "Record Screen|Ctrl+Shift+7",
		"Trim && Compress Video…|", "|", "Open Library|Ctrl+Shift+L", "Edit Last Capture|", "Settings…|", "|", "Quit Glint|"}
	if !slices.Equal(labels, want) {
		t.Fatalf("menu\n%q\nwant\n%q", labels, want)
	}
}

func TestInstanceNameKeepsDataDirectoriesApart(t *testing.T) {
	t.Setenv("GLINT_DATA_DIR", "")
	if InstanceName() != "Glint" {
		t.Fatal("the installed Glint keeps its name")
	}
	t.Setenv("GLINT_DATA_DIR", `C:\Temp\a`)
	a := InstanceName()
	t.Setenv("GLINT_DATA_DIR", `c:\temp\A\`)
	if InstanceName() != a {
		t.Fatal("the same folder spelled differently is the same instance")
	}
	t.Setenv("GLINT_DATA_DIR", `C:\Temp\b`)
	if InstanceName() == a || !strings.HasPrefix(a, "Glint ") {
		t.Fatalf("names %q and %q", a, InstanceName())
	}
}

func crop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := range r.Dy() {
		for x := range r.Dx() {
			out.SetRGBA(x, y, img.RGBAAt(r.Min.X+x, r.Min.Y+y))
		}
	}
	return out
}

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{c.R, c.G, c.B, c.A})
	}
	return img
}

func samePixels(a, b image.Image) bool {
	if a.Bounds().Size() != b.Bounds().Size() {
		return false
	}
	ab, bb := a.Bounds(), b.Bounds()
	for y := range ab.Dy() {
		for x := range ab.Dx() {
			if color.RGBAModel.Convert(a.At(ab.Min.X+x, ab.Min.Y+y)) != color.RGBAModel.Convert(b.At(bb.Min.X+x, bb.Min.Y+y)) {
				return false
			}
		}
	}
	return true
}

func readPNG(t *testing.T, path string) image.Image {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func writePNG(t *testing.T, path string, img image.Image) {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}
