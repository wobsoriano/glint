package studio

import (
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
	core "github.com/wobsoriano/glint/internal/studio"
)

type fakeShell struct {
	mu    sync.Mutex
	queue []func()
}

func (s *fakeShell) update(fn func()) {
	s.mu.Lock()
	s.queue = append(s.queue, fn)
	s.mu.Unlock()
}

func (s *fakeShell) pickImage() string { return "" }
func (s *fakeShell) destroy()          {}

// drain runs what other goroutines posted, as the window's Update would.
func (s *fakeShell) drain() {
	s.mu.Lock()
	q := s.queue
	s.queue = nil
	s.mu.Unlock()
	for _, fn := range q {
		fn()
	}
}

type fakeHost struct {
	s        settings.Settings
	copied   []string
	exported []string
}

func (h *fakeHost) Settings() *settings.Settings  { return &h.s }
func (h *fakeHost) Toast(string)                  {}
func (h *fakeHost) Reveal(string)                 {}
func (h *fakeHost) CopyFile(p string) error       { h.copied = append(h.copied, p); return nil }
func (h *fakeHost) RecordingExported(path string) { h.exported = append(h.exported, path) }

func testStudio(t *testing.T) (*editor, *fakeShell, *fakeHost, *ui.Tester, string) {
	t.Helper()
	t.Setenv("GLINT_DATA_DIR", t.TempDir())
	return openStudio(t, testSession(t))
}

// openStudio opens Studio on dir with the data dir the test has set, sound off.
func openStudio(t *testing.T, dir string) (*editor, *fakeShell, *fakeHost, *ui.Tester, string) {
	t.Helper()
	sh := &fakeShell{}
	h := &fakeHost{s: settings.Default()}
	h.s.SaveDir = filepath.Join(t.TempDir(), "out")
	e, err := newEditor(h, sh, dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.session.Project.Audio.System.Enabled = false
	e.player.setProject(e.previewProject())
	t.Cleanup(func() {
		e.closed()
		time.Sleep(100 * time.Millisecond)
	})
	e.start()
	tt := ui.NewTester(e.view, 1440, 900)
	settle(t, e, sh, tt, func() bool { return e.video != nil && len(e.thumbs) == thumbCount })
	return e, sh, h, tt, dir
}

// settle runs posted work and frames until ok holds.
func settle(t *testing.T, e *editor, sh *fakeShell, tt *ui.Tester, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("timed out; player error: " + e.player.errText())
		}
		time.Sleep(10 * time.Millisecond)
		sh.drain()
		tt.Frame()
	}
}

func savedProject(t *testing.T, dir string) core.Project {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, core.ProjectFile))
	if err != nil {
		t.Fatal(err)
	}
	p, err := core.ParseProject(data, 20_000)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSplitSpeedUndoRedoAndSaveReachProjectJSON(t *testing.T) {
	e, sh, _, tt, dir := testStudio(t)
	e.seek(5_000)
	tt.Key(0, ui.KeyS)
	if n := len(e.project().Clips); n != 2 {
		t.Fatalf("%d clips after S, want 2", n)
	}
	if e.sel != (selection{selClip, 1}) {
		t.Errorf("selection %+v after split, want the new clip", e.sel)
	}
	e.setSpeed(2)
	if got := e.project().Clips[1].Speed; got != 2 {
		t.Errorf("speed %v", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if got := e.project().Clips[1].Speed; got != 1 {
		t.Errorf("speed %v after undo, want 1", got)
	}
	tt.Key(ui.Cmd, ui.KeyZ)
	if n := len(e.project().Clips); n != 1 {
		t.Errorf("%d clips after two undos, want 1", n)
	}
	tt.Key(ui.Cmd, ui.KeyY)
	tt.Key(ui.Cmd, ui.KeyY)
	if n, sp := len(e.project().Clips), e.project().Clips[len(e.project().Clips)-1].Speed; n != 2 || sp != 2 {
		t.Errorf("after redo: %d clips, last at %v×", n, sp)
	}
	settle(t, e, sh, tt, func() bool { return !e.dirty })
	if p := savedProject(t, dir); len(p.Clips) != 2 || p.Clips[1].Speed != 2 {
		t.Errorf("project.json clips %+v", p.Clips)
	}
}

func TestSidebarControlsEditTheProject(t *testing.T) {
	e, sh, _, tt, dir := testStudio(t)
	for _, step := range []struct {
		click []string
		check func() bool
		what  string
	}{
		{[]string{"Keystrokes", "Top left"}, func() bool { return e.project().Keystrokes.Position == core.KeysTopLeft }, "keystroke position"},
		{[]string{"Layout", "9:16"}, func() bool { return e.project().Aspect == core.AspectVertical9x16 }, "aspect"},
		{[]string{"Fit"}, func() bool { return e.project().AspectMode == core.AspectFit }, "fit"},
		{[]string{"Background", "Solid"}, func() bool { _, ok := e.project().Design.Background.(render.SolidBackground); return ok }, "solid background"},
	} {
		for _, label := range step.click {
			if err := tt.Click(label); err != nil {
				t.Fatalf("%s: %v (texts %q)", step.what, err, tt.Texts())
			}
		}
		if !step.check() {
			t.Errorf("%s did not change", step.what)
		}
	}
	settle(t, e, sh, tt, func() bool { return !e.dirty })
	if p := savedProject(t, dir); p.Aspect != core.AspectVertical9x16 || p.Keystrokes.Position != core.KeysTopLeft {
		t.Errorf("project.json aspect %v, keys %v", p.Aspect, p.Keystrokes.Position)
	}
}

func TestCropToolbarTakesClicksOverThePreview(t *testing.T) {
	e, _, _, tt, _ := testStudio(t)
	e.enterCrop()
	tt.Frame()
	if err := tt.Click("3:2"); err != nil {
		t.Fatal(err)
	}
	c := e.project().Crop
	if got := c.W * float32(e.session.SourceW) / (c.H * float32(e.session.SourceH)); !near(got, 1.5) {
		t.Errorf("crop %+v is %.3f wide per tall, want 3:2", c, got)
	}
	if err := tt.Click("Done"); err != nil {
		t.Fatal(err)
	}
	if _, ok := e.mode.(normalMode); !ok {
		t.Errorf("mode %T after Done", e.mode)
	}
}

// TestSnapshot writes what the window shows to $GLINT_STUDIO_SNAPSHOT, for looking at the
// layout without a desktop.
func TestSnapshot(t *testing.T) {
	out := os.Getenv("GLINT_STUDIO_SNAPSHOT")
	if out == "" {
		t.Skip("GLINT_STUDIO_SNAPSHOT is not set")
	}
	e, sh, _, tt, _ := testStudio(t)
	tt.SetDark(true)
	e.seek(6_000)
	settle(t, e, sh, tt, func() bool { return e.shown.outputMs == 6_000 })
	tt.Frame()
	f, err := os.Create(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}
