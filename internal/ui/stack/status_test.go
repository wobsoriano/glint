package stack

import (
	"image"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

func TestAFailedActionShowsItsReasonWithoutTheCheckMark(t *testing.T) {
	s := New(stuckHost{make(chan struct{})}, settings.Default().Overlay)
	s.cards = []*card{{id: 1, item: store.Item{Meta: store.Meta{Kind: store.Screenshot}}, thumb: ui.NewBitmap(image.NewRGBA(image.Rect(0, 0, 4, 4)))}}
	w, h := stackSize(1, false)
	tt := ui.NewTester(s.view, int(w), int(h))
	s.now = tt.Now
	near := func(want color.RGBA) int {
		img, n := tt.Image(), 0
		for i := 0; i < len(img.Pix); i += 4 {
			if absDiff(img.Pix[i], want.R) < 24 && absDiff(img.Pix[i+1], want.G) < 24 && absDiff(img.Pix[i+2], want.B) < 24 {
				n++
			}
		}
		return n
	}
	green, red := color.RGBA{0x34, 0xC7, 0x59, 255}, color.RGBA{0xFF, 0x99, 0xA4, 255}

	s.flash(1, "Copied")
	tt.Frame()
	if near(green) < 8 || near(red) > 0 {
		t.Fatalf("success: %d green and %d red pixels", near(green), near(red))
	}

	_, err := os.Open(filepath.Join(t.TempDir(), "Pictures", "shot.png"))
	s.fail(1, "Copy", err)
	tt.Frame()
	if !slices.ContainsFunc(tt.Texts(), func(s string) bool {
		return strings.HasPrefix(s, "Copy failed: ") && strings.HasSuffix(s, " (shot.png)")
	}) {
		t.Errorf("no failure status in %q", tt.Texts())
	}
	if near(green) > 0 || near(red) < 8 {
		t.Errorf("failure: %d green and %d red pixels", near(green), near(red))
	}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
