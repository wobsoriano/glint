package record

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestTheDeleteConfirmationShowsItsWholeQuestion lays the control bar out as it asks before
// deleting, at the width the bar takes for the question, and compares the question's box with
// the width the text needs on its own.
func TestTheDeleteConfirmationShowsItsWholeQuestion(t *testing.T) {
	natural := ui.NewTester(func(ctx *ui.Context) {
		ui.Row(ctx).Children(func() { ui.Text(ctx, deleteQuestion).SingleLine() })
	}, 600, 60)
	want, ok := natural.Find(deleteQuestion)
	if !ok {
		t.Fatalf("no question in %q", natural.Texts())
	}
	c := &Controller{barView: &bar{born: time.Now(), confirmDelete: true, width: barW}}
	ui.NewTester(c.barUI, barW, barH).Frame()
	w := c.barView.width
	tt := ui.NewTester(c.barUI, int(w), barH)
	got, ok := tt.Find(deleteQuestion)
	if !ok {
		t.Fatalf("the bar does not ask: %q", tt.Texts())
	}
	if got.W+0.5 < want.W {
		t.Errorf("the question gets %.1f DIPs of the %.1f it needs in a %v DIP bar", got.W, want.W, w)
	}
	del, ok := tt.Find("Delete")
	if !ok {
		t.Fatalf("no Delete button: %q", tt.Texts())
	}
	if right := del.X + del.W; right > float32(w)-12 {
		t.Errorf("the Delete label ends at %.1f in a %v DIP bar", right, w)
	}
	if _, ok := tt.Find("Keep"); !ok {
		t.Errorf("no Keep button: %q", tt.Texts())
	}

	c.barView.confirmDelete = false
	tt.Frame()
	if c.barView.width != barW {
		t.Errorf("after Keep the bar stays %v DIPs wide", c.barView.width)
	}
}
