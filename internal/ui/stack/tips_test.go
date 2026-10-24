package stack

import (
	"image"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// stuckHost never finishes an action, so a test sees the stack as the click left it.
type stuckHost struct{ never chan struct{} }

func (h stuckHost) wait() error                         { <-h.never; return nil }
func (h stuckHost) Copy(store.Item) error               { return h.wait() }
func (h stuckHost) Save(store.Item, bool) error         { return h.wait() }
func (h stuckHost) Compress(store.Item) error           { return h.wait() }
func (h stuckHost) CopyText(store.Item) error           { return h.wait() }
func (h stuckHost) Edit(store.Item) error               { return h.wait() }
func (h stuckHost) Pin(store.Item) error                { return h.wait() }
func (h stuckHost) QuickLook(store.Item) error          { return h.wait() }
func (h stuckHost) Delete(store.Item) error             { return h.wait() }
func (h stuckHost) DragFile(store.Item) (string, error) { return "", h.wait() }

func TestATipStaysHiddenAfterAClickUntilThePointerLeavesTheCard(t *testing.T) {
	s := New(stuckHost{make(chan struct{})}, settings.Default().Overlay)
	s.cards = []*card{{id: 1, item: store.Item{Meta: store.Meta{Kind: store.Screenshot}}, thumb: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 4, 4)))}}
	w, h := stackSize(1, false)
	tt := ui.NewTester(s.view, int(w), int(h))
	s.now = tt.Now
	rest := func() { tt.Advance(800 * time.Millisecond); tt.Frame() }
	tip := help[settings.CardCopy]

	tt.Move(float32(w)/2, float32(h)-20)
	tt.Frame()
	r, ok := tt.Find("Copy")
	if !ok {
		t.Fatalf("no Copy button in %q", tt.Texts())
	}
	x, y := r.X+r.W/2, r.Y+r.H/2
	tt.Move(x, y)
	rest()
	if !tt.HasText(tip) {
		t.Fatalf("resting on Copy shows no tip: %q", tt.Texts())
	}

	tt.ClickAt(x, y)
	s.flash(1, "Copied")
	rest()
	// The hand resting on the mouse still moves it a pixel now and then.
	tt.Move(x+1, y)
	tt.Advance(flashFor)
	s.tick()
	rest()
	tt.Move(x, y)
	rest()
	if !tt.HasText("Copy") || tt.HasText(tip) {
		t.Fatalf("after the click's status the tip shows again: %q", tt.Texts())
	}

	tt.Move(float32(w)/2, 4)
	tt.Frame()
	tt.Move(x, y)
	rest()
	if !tt.HasText(tip) {
		t.Fatalf("the tip does not come back once the pointer leaves the card and returns: %q", tt.Texts())
	}
}
