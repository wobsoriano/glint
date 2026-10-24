package media

import (
	"image"
	"testing"

	"github.com/wobsoriano/glint/internal/platform"
)

func TestCropForClipsToTheItemAndRoundsToEven(t *testing.T) {
	area := areaInDisplay(platform.Rect{X: -2000, Y: 100, W: 501, H: 301}, platform.Rect{X: -1920, Y: 0, W: 1920, H: 1080})
	got, err := cropFor(1920, 1080, &area)
	if err != nil {
		t.Fatal(err)
	}
	if want := image.Rect(0, 100, 420, 400); got != want {
		t.Fatalf("crop %v, want %v: clipped at the display's left edge, then even", got, want)
	}
	if got, _ := cropFor(1365, 767, nil); got != image.Rect(0, 0, 1364, 766) {
		t.Fatalf("whole item %v, want even sides", got)
	}
	outside := image.Rect(2000, 0, 2100, 100)
	if _, err := cropFor(1920, 1080, &outside); err == nil {
		t.Fatal("an area off the display must not record")
	}
}
