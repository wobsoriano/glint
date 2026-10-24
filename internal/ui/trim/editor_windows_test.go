package trim

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/ui/trim/internal/testclip"
)

// fakeHost queues the editor's updates for the test goroutine, which owns the Tester.
type fakeHost struct {
	mu       sync.Mutex
	queue    []func()
	revealed []string
}

func (h *fakeHost) update(fn func()) {
	h.mu.Lock()
	h.queue = append(h.queue, fn)
	h.mu.Unlock()
}

func (h *fakeHost) saveAs(dir, name string) (string, error) { return "", nil }
func (h *fakeHost) reveal(path string)                      { h.revealed = append(h.revealed, path) }

func (h *fakeHost) pump() {
	h.mu.Lock()
	q := h.queue
	h.queue = nil
	h.mu.Unlock()
	for _, fn := range q {
		fn()
	}
}

type harness struct {
	t  *testing.T
	h  *fakeHost
	e  *editor
	tt *ui.Tester
}

func newHarness(t *testing.T, path string, cfg Config) *harness {
	h := &fakeHost{}
	e := newEditor(h, path, cfg)
	t.Cleanup(func() {
		e.close()
		if e.player != nil {
			e.player.Close()
		}
	})
	go e.load()
	return &harness{t: t, h: h, e: e, tt: ui.NewTester(e.view, 980, 760)}
}

func (x *harness) until(what string, cond func() bool) {
	x.t.Helper()
	for deadline := time.Now().Add(60 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		x.h.pump()
		x.tt.Frame()
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			x.t.Fatalf("timed out waiting for %s; the window shows %q", what, x.tt.Texts())
		}
	}
}

func (x *harness) click(label string) {
	x.t.Helper()
	if err := x.tt.Click(label); err != nil {
		x.t.Fatalf("click %q: %v; the window shows %q", label, err, x.tt.Texts())
	}
	x.h.pump()
	x.tt.Frame()
}

// drag presses at x0, moves to x1 and lets go, all on the timeline's row.
func (x *harness) drag(y, x0, x1 float32) {
	x.tt.Press(x0, y)
	x.tt.Move((x0+x1)/2, y)
	x.tt.Move(x1, y)
	x.tt.Release(x1, y)
	x.h.pump()
	x.tt.Frame()
}

func near(a, b, tol int64) bool { return a-b <= tol && b-a <= tol }

func TestTrimToTheMiddleAt720pAndExport(t *testing.T) {
	dir := t.TempDir()
	x := newHarness(t, testClip(t), Config{SaveDir: dir})
	if !x.tt.HasText("Opening…") {
		t.Fatalf("before the probe the window shows %q", x.tt.Texts())
	}
	x.until("the first frame and the thumbnails", func() bool { return x.e.video != nil && len(x.e.thumbs) == thumbCount })
	if !x.tt.HasText("0:00.0 / 0:04.0") || !x.tt.HasText("Source 1920 × 1080 · 30 fps · "+HumanBytes(x.e.src.Bytes)) {
		t.Fatalf("the transport and source lines read %q", x.tt.Texts())
	}

	tl, ok := x.tt.Find("Timeline")
	if !ok {
		t.Fatal("no timeline")
	}
	y, x0, w := tl.Y+tl.H/2, tl.X+stripPad, tl.W-2*stripPad
	x.drag(y, x0-4, x0+w/4)
	x.drag(y, x0+w+4, x0+w*3/4)
	if !near(x.e.trim.StartMS, 1_000, 15) || !near(x.e.trim.EndMS, 3_000, 15) {
		t.Fatalf("dragging the handles to a quarter and three quarters trimmed to %v", x.e.trim)
	}
	x.until("the frame at the end handle", func() bool { return near(x.e.shown.TMS, 2_966, 40) })
	x.drag(y, x0+w/2, x0+w/2)
	x.until("the frame under the playhead", func() bool { return near(x.e.shown.TMS, 2_000, 40) })
	if n, ok := testclip.FrameNumber(x.e.shown.Image, x.e.shown.Image.Rect); !ok || n != 60 {
		t.Fatalf("a click mid-timeline shows frame %d (%v), want 60", n, ok)
	}
	x.e.trim = Trim{1_000, 3_000}

	x.click("720p")
	small := x.e.opts
	small.Quality = Small
	x.click(fmt.Sprintf("Small · ≈%s", HumanBytes(EstimateBytes(*x.e.src, x.e.trim, small, true))))
	if x.e.opts.Resolution != P720 || x.e.opts.Quality != Small {
		t.Fatalf("options after the clicks: %+v", x.e.opts)
	}

	x.click("Export")
	var progress []float64
	x.until("the export", func() bool {
		if r, ok := x.e.job.(*jobRunning); ok {
			progress = append(progress, r.progress)
		}
		_, done := x.e.job.(jobDone)
		return done
	})
	done := x.e.job.(jobDone)
	if want := filepath.Join(dir, "clip (trimmed).mp4"); done.path != want {
		t.Fatalf("exported to %s, want %s", done.path, want)
	}
	if !x.tt.HasText("Saved clip (trimmed).mp4 · " + HumanBytes(done.bytes) + fmt.Sprintf(" (%+.0f%%)", -(1-float64(done.bytes)/float64(x.e.src.Bytes))*100)) {
		t.Fatalf("after the export the window shows %q", x.tt.Texts())
	}
	moved := 0
	for i := 1; i < len(progress); i++ {
		if progress[i] > progress[i-1] {
			moved++
		}
	}
	if moved < 3 {
		t.Fatalf("the progress bar moved %d times: %v", moved, progress)
	}
	checkExport(t, done.path, 1280, 720, 2_000, 30)

	x.click("Show in " + fileManager)
	if len(x.h.revealed) != 1 || x.h.revealed[0] != done.path {
		t.Fatalf("revealed %v", x.h.revealed)
	}
	x.click("Copy")
	if items := x.tt.ClipboardData(); len(items.Items()) == 0 {
		t.Fatal("Copy put nothing on the clipboard")
	}
}

// checkExport decodes the export: its size, its length and the frame number its first frame
// shows, which proves where the trim began.
func checkExport(t *testing.T, path string, w, h int, durationMS int64, firstFrame int) {
	t.Helper()
	r, err := media.OpenVideoReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if gw, gh := r.Size(); gw != w || gh != h || !near(r.DurationMS(), durationMS, 70) {
		t.Fatalf("%s is %dx%d and %d ms, want %dx%d and %d ms", path, gw, gh, r.DurationMS(), w, h, durationMS)
	}
	f, err := r.NextFrame()
	if err != nil {
		t.Fatal(err)
	}
	if n, ok := testclip.FrameNumber(f.Image, f.Image.Rect); !ok || n != firstFrame {
		t.Fatalf("the export starts on frame %d (%v), want %d", n, ok, firstFrame)
	}
	if _, err := media.ReadMP4Audio(path); err != nil {
		t.Fatalf("the export's audio: %v", err)
	}
}

func TestCancelledExportLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	x := newHarness(t, testClip(t), Config{SaveDir: dir})
	x.until("the source", func() bool { return x.e.src != nil })
	x.click("Export")
	x.until("some progress", func() bool {
		r, ok := x.e.job.(*jobRunning)
		return ok && r.progress > 0.1
	})
	x.click("Cancel")
	x.until("the export to stop", func() bool { _, idle := x.e.job.(jobIdle); return idle })
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("a cancelled export left %v", entries)
	}
}

func TestExportNeverOverwritesTheSource(t *testing.T) {
	src := testClip(t)
	_, err := writeClip(t.Context(), src, src, media.TranscodeOptions{}, nil)
	if err == nil {
		t.Fatal("exporting onto the source succeeded")
	}
}

func TestOpenRefusesFilesThatAreNotVideos(t *testing.T) {
	if _, err := Open(`C:\x\shot.png`, Config{}); err == nil || err.Error() != "shot.png is not a video Glint can trim (MP4, MOV, M4V, WMV, AVI)" {
		t.Fatalf("Open of a PNG: %v", err)
	}
}
