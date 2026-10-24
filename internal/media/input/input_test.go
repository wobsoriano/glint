package input

import (
	"encoding/json"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/wobsoriano/glint/internal/media/capture"
)

const documented = `{
	"version": 1,
	"size": [2560, 1440],
	"pointer": [[0, 100.5, 200.0, 0], [16, 104.0, 203.5, 0]],
	"clicks": [{ "t": 1200, "up": 1310, "x": 640.0, "y": 360.0, "button": "left" }],
	"keys": [{ "t": 3000, "label": "Ctrl+S" }],
	"cursors": [{ "id": 0, "file": "cursors/0.png", "hotspot": [1, 1], "scale": 1.5 }]
}`

func TestReadsTheDocumentedShape(t *testing.T) {
	var tl Timeline
	if err := json.Unmarshal([]byte(documented), &tl); err != nil {
		t.Fatal(err)
	}
	want := Timeline{
		Version: 1, Size: [2]uint32{2560, 1440},
		Pointer: []PointerSample{{0, 100.5, 200, 0}, {16, 104, 203.5, 0}},
		Clicks:  []Click{{T: 1200, Up: 1310, X: 640, Y: 360, Button: ButtonLeft}},
		Keys:    []KeyPress{{3000, "Ctrl+S"}},
		Cursors: []CursorSprite{{ID: 0, File: "cursors/0.png", Hotspot: [2]uint32{1, 1}, Scale: 1.5}},
	}
	if !reflect.DeepEqual(tl, want) {
		t.Fatalf("%+v", tl)
	}
	again, err := json.Marshal(tl)
	if err != nil {
		t.Fatal(err)
	}
	var back Timeline
	if err := json.Unmarshal(again, &back); err != nil || !reflect.DeepEqual(back, want) {
		t.Fatalf("round trip %s: %v", again, err)
	}
	if !strings.Contains(string(again), `"pointer":[[0,100.5,200,0],[16,104,203.5,0]]`) || !strings.Contains(string(again), `"button":"left"`) {
		t.Fatalf("wire shape %s", again)
	}
}

func TestEmptyListsAreWrittenAsArrays(t *testing.T) {
	b, err := json.Marshal(Timeline{Version: 1, Size: [2]uint32{2, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `{"version":1,"size":[2,2],"pointer":[],"clicks":[],"keys":[],"cursors":[]}` {
		t.Fatalf("serde rejects null lists: %s", b)
	}
}

func TestMalformedRowsAreRejected(t *testing.T) {
	for _, bad := range []string{
		`{"pointer":[[0,1,2]]}`,
		`{"pointer":[[1.5,1,2,0]]}`,
		`{"clicks":[{"t":0,"up":0,"x":0,"y":0,"button":"back"}]}`,
	} {
		var tl Timeline
		if json.Unmarshal([]byte(bad), &tl) == nil {
			t.Errorf("accepted %s", bad)
		}
	}
}

func runningClock() *capture.Clock {
	c := capture.NewClock()
	c.Resume()
	return c
}

func newTestRecorder(t *testing.T, ignored ...string) *recorder {
	return newRecorder([2]uint32{1920, 1080}, 1.5, t.TempDir(), runningClock(), ignored)
}

func TestPointerMovesAreRateLimited(t *testing.T) {
	r := newTestRecorder(t)
	r.moved(Point{10, 20})
	r.moved(Point{11, 21})
	if p := r.tl.Pointer; len(p) != 1 || p[0].X != 10 || p[0].Y != 20 || p[0].Cursor != 0 {
		t.Fatalf("a second move within 8 ms is dropped: %v", p)
	}
}

func TestClicksPairDownWithUpAndCloseAtFinish(t *testing.T) {
	r := newTestRecorder(t)
	r.pressed(ButtonLeft, Point{5, 6})
	r.released(ButtonLeft, Point{5, 6})
	r.pressed(ButtonRight, Point{7, 8})
	tl := r.finish()
	if len(tl.Clicks) != 2 {
		t.Fatal(tl.Clicks)
	}
	c0, c1 := tl.Clicks[0], tl.Clicks[1]
	if c0.Button != ButtonLeft || c0.X != 5 || c0.Y != 6 || c0.Up < c0.T {
		t.Fatalf("%+v", c0)
	}
	if c1.Button != ButtonRight || c1.Up < c1.T {
		t.Fatalf("an unreleased button closes at the end: %+v", c1)
	}
}

func TestAPolledPositionIsRecordedOnlyWhenItMoved(t *testing.T) {
	r := newTestRecorder(t)
	r.moved(Point{1, 1})
	r.polled(Point{1, 1})
	r.polled(Point{40, 2})
	var got []Point
	for _, p := range r.tl.Pointer {
		got = append(got, Point{p.X, p.Y})
	}
	if !reflect.DeepEqual(got, []Point{{1, 1}, {40, 2}}) {
		t.Fatal(got)
	}
}

func TestEachCursorShapeIsSavedOnce(t *testing.T) {
	dir := t.TempDir()
	r := newRecorder([2]uint32{100, 100}, 2, dir, runningClock(), nil)
	white := make([]byte, 16)
	for i := range white {
		white[i] = 255
	}
	arrow := cursorImage{width: 2, height: 2, rgba: white}
	beam := cursorImage{width: 2, height: 2, hotspot: [2]uint32{1, 1}, rgba: make([]byte, 16)}
	r.cursorChanged(arrow, Point{})
	r.cursorChanged(beam, Point{})
	r.cursorChanged(arrow, Point{})
	if len(r.tl.Cursors) != 2 || r.tl.Cursors[0].ID != 0 || r.tl.Cursors[1].ID != 1 {
		t.Fatalf("%+v", r.tl.Cursors)
	}
	if r.tl.Cursors[1].Scale != 2 || r.tl.Cursors[1].Hotspot != [2]uint32{1, 1} || r.tl.Cursors[1].File != "cursors/1.png" {
		t.Fatalf("%+v", r.tl.Cursors[1])
	}
	if last := r.tl.Pointer[len(r.tl.Pointer)-1]; last.Cursor != 0 {
		t.Fatal("back on the first shape")
	}
	f, err := os.Open(filepath.Join(dir, "0.png"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := img.At(1, 1).RGBA(); img.Bounds().Dx() != 2 || r != 0xFFFF || g != 0xFFFF || b != 0xFFFF || a != 0xFFFF {
		t.Fatalf("sprite %v %v", img.Bounds(), img.At(1, 1))
	}
}

func TestIgnoredShortcutsAreLeftOut(t *testing.T) {
	r := newTestRecorder(t, "Ctrl+Shift+7")
	r.key("Ctrl+Shift+7")
	r.key("Ctrl+S")
	if len(r.tl.Keys) != 1 || r.tl.Keys[0].Label != "Ctrl+S" {
		t.Fatal(r.tl.Keys)
	}
}

func TestNothingIsRecordedWhilePaused(t *testing.T) {
	r := newTestRecorder(t)
	r.clock.Pause()
	r.moved(Point{1, 1})
	r.pressed(ButtonLeft, Point{1, 1})
	r.key("Enter")
	if !reflect.DeepEqual(r.tl, Timeline{Version: 1, Size: [2]uint32{1920, 1080}}) {
		t.Fatalf("%+v", r.tl)
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()
	r := newTestRecorder(t)
	r.pressed(ButtonMiddle, Point{3, 4})
	tl := r.finish()
	if err := tl.Save(dir); err != nil {
		t.Fatal(err)
	}
	back, err := Load(dir)
	if err != nil || back.Clicks[0] != tl.Clicks[0] || back.Pointer[0] != tl.Pointer[0] || len(back.Keys) != 0 {
		t.Fatalf("%+v %v", back, err)
	}
}
