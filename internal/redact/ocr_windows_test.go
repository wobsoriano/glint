package redact

import (
	"image"
	"slices"
	"testing"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
)

// The Rust build's redacts_rendered_secrets test: draw three lines of secrets, read them
// with the OS engine and redact what it read.
func TestRedactsWhatOCRReads(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 1400, 360))
	render.Fill(img, render.White)
	lines := []string{"Contact: jane.doe@example.com", "Key: sk_live_51HxYzAbCdEfGhIjKlMnOp", "Card 4242 4242 4242 4242 exp 12/29"}
	for i, text := range lines {
		render.DrawText(img, text, 30, 30+float32(i)*100, 40, render.Black)
	}
	read, err := ocr.Recognize(img)
	if err != nil {
		t.Fatal(err)
	}
	found := Find(read, settings.DefaultRedactRules(), 2)
	for row, want := range []Kind{Email, Secret, Card} {
		top, bottom := 30+float32(row)*100, 30+float32(row)*100+50
		i := slices.IndexFunc(found, func(f Finding) bool {
			mid := f.Rect.Y + f.Rect.H/2
			return f.Kind == want && mid > top && mid < bottom
		})
		if i < 0 {
			t.Errorf("no %s over line %d; OCR read %q, found %+v", want.Label(), row, ocr.Text(read), found)
		}
	}
}
