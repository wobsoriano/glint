package media

import (
	"strings"
	"testing"
	"time"
)

func TestACameraPreviewOfAMissingCameraFailsBeforeAnyFrame(t *testing.T) {
	p, err := StartCameraPreview(`\\?\no-such-camera`)
	if err == nil {
		p.Stop()
		t.Fatal("a missing camera must not start a preview")
	}
	if !strings.Contains(err.Error(), "no camera with id") {
		t.Fatalf("error %q does not say the camera is missing", err)
	}
}

func TestCameraPixelsArriveAsOpaqueRGBA(t *testing.T) {
	bgra := []byte{0x10, 0x20, 0x30, 0x00, 0xA0, 0xB0, 0xC0, 0x7F}
	img := bgraToRGBA(bgra, 2, 1)
	want := []byte{0x30, 0x20, 0x10, 0xFF, 0xC0, 0xB0, 0xA0, 0xFF}
	if string(img.Pix) != string(want) {
		t.Fatalf("got % x, want % x", img.Pix, want)
	}
}

// TestTheFirstCameraPreviews runs only on a machine with a camera.
func TestTheFirstCameraPreviews(t *testing.T) {
	cams, err := Cameras()
	if err != nil || len(cams) == 0 {
		t.Skipf("no camera to preview (%v)", err)
	}
	p, err := StartCameraPreview(cams[0].ID)
	if err != nil {
		t.Fatalf("preview %s: %v", cams[0].Name, err)
	}
	w, h := p.Size()
	select {
	case img := <-p.Frames():
		if img.Bounds().Dx() != w || img.Bounds().Dy() != h {
			t.Errorf("frame %v, preview says %dx%d", img.Bounds(), w, h)
		}
	case <-time.After(5 * time.Second):
		t.Error("no frame within 5 s")
	}
	if err := p.Stop(); err != nil {
		t.Errorf("stop: %v", err)
	}
	if _, open := <-p.Frames(); open {
		t.Error("Frames stays open after Stop")
	}
}
