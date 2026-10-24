package studio

import (
	"math"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/wobsoriano/glint/internal/render"
)

// monoMeasure stands in for the font: every character is half the size wide.
func monoMeasure(text string, size float32) (float32, float32) {
	return float32(utf8.RuneCountInString(text)) * size * 0.5, size * 1.25
}

func TestCursorLandsOnItsSourcePixelAndScalesWithTheCard(t *testing.T) {
	p := NewProject(5_000)
	p.Design.Padding = 0
	p.Export.Resolution = Res1080
	w, h := p.OutputSize(3840, 2160)
	eq(t, [2]uint32{w, h}, [2]uint32{1920, 1080})
	l := FrameLayout(&p, 3840, 2160, w, h)
	xf := NewScreenXf(&p, &l, 3840, 2160, IdentityViewport)
	x, y := xf.Point(1000, 1000)
	eq(t, [2]float32{x, y}, [2]float32{500, 500})
	drawn := 48 * CursorScale(l.FillH, 1.5)
	if math.Abs(drawn-69.12) > 1e-9 {
		t.Fatalf("a 48 px sprite at 1.5x draws %v px on a 1080p card", drawn)
	}
}

func TestFillOverflowsTheCardOnOneAxis(t *testing.T) {
	p := NewProject(1_000)
	p.Aspect, p.AspectMode = AspectVertical9x16, AspectFill
	p.Design.Padding = 0
	l := FrameLayout(&p, 2560, 1440, 1080, 1920)
	eq(t, l.Card, render.R{X: 0, Y: 0, W: 1080, H: 1920})
	eq(t, l.FillH, 1920.0)
	if l.FillW <= 1080 {
		t.Fatalf("fill width %v should overflow the card", l.FillW)
	}
	p.AspectMode = AspectFit
	l = FrameLayout(&p, 2560, 1440, 1080, 1920)
	eq(t, l.Card.W, float32(1080))
	eq(t, l.Card.H, float32(1080*1440.0/2560.0))
}

func TestCameraBubbleStaysOnCanvas(t *testing.T) {
	p := NewProject(1_000)
	p.Design.Camera.Center = [2]float32{1, 1}
	l := FrameLayout(&p, 1920, 1080, 1920, 1080)
	b := l.Bubble.Rect
	margin := float32(math.Round(1080 * 0.03))
	eq(t, [2]float32{b.Right(), b.Bottom()}, [2]float32{1920 - margin, 1080 - margin})
	p.Design.Camera.Visible = false
	if FrameLayout(&p, 1920, 1080, 1920, 1080).Bubble != nil {
		t.Fatal("hidden camera has no bubble")
	}
}

func TestKeystrokeMovesClearOfTheCaptionBar(t *testing.T) {
	card := render.R{X: 0, Y: 0, W: 1920, H: 1080}
	key := KeyFrame{Modifiers: "Ctrl+", Key: "S", Opacity: 1, Scale: 1}
	margin := min(card.W, card.H) * 0.045
	free := LayoutKeystroke(card, KeysBottomCenter, key, nil, monoMeasure)
	if free.Rect.Bottom() != card.Bottom()-margin {
		t.Fatalf("bottom margin: %+v", free.Rect)
	}
	bar := render.R{X: 600, Y: free.Rect.Y + 5, W: 720, H: 80}
	moved := LayoutKeystroke(card, KeysBottomCenter, key, &bar, monoMeasure)
	if moved.Rect.Bottom() > bar.Y || overlaps(moved.Rect, bar) {
		t.Fatalf("keystroke %+v overlaps caption bar %+v", moved.Rect, bar)
	}
	top := LayoutKeystroke(card, KeysTopLeft, key, &bar, monoMeasure)
	eq(t, top.Rect.Y, card.Y+margin)
	if top.KeyX <= top.ModifiersX {
		t.Fatal("key draws after its modifiers")
	}
}

func TestCaptionsWrapToThreeLinesAndShrink(t *testing.T) {
	s := DefaultCaptionSettings()
	s.Karaoke = true
	short := CaptionFrame{Words: []string{"Open", "the", "settings"}, Active: 1, Spoken: 2}
	bar := LayoutCaptions(1920, 1080, s, short, monoMeasure)
	eq(t, len(bar.Words), 3)
	eq(t, []WordTone{bar.Words[0].Tone, bar.Words[1].Tone, bar.Words[2].Tone}, []WordTone{TonePlain, ToneActive, TonePending})
	if math.Abs(float64(bar.Rect.Y+bar.Rect.H/2)-1080*0.92) > 1e-3 {
		t.Fatalf("bar centered at the caption position: %+v", bar.Rect)
	}

	long := CaptionFrame{Words: strings.Fields(strings.Repeat("caption words wrap ", 40)), Active: -1}
	wrapped := LayoutCaptions(1920, 1080, s, long, monoMeasure)
	rows := map[float32]bool{}
	for _, w := range wrapped.Words {
		rows[w.Y] = true
		if w.X < 0 || w.X > 1920 {
			t.Fatalf("word off canvas: %+v", w)
		}
	}
	if len(rows) > captionMaxLines || wrapped.Font >= bar.Font {
		t.Fatalf("%d lines at font %v", len(rows), wrapped.Font)
	}
}
