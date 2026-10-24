package actions

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"

	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
)

// ErrCancelled is what a save the user cancelled in its dialog reports.
var ErrCancelled = errors.New("cancelled")

// Trash names where deleted captures go, as the platform calls it.
var Trash = map[bool]string{true: "Trash", false: "Recycle Bin"}[runtime.GOOS == "darwin"]

// Encode writes img as PNG or as JPEG of quality 1 to 100.
func Encode(img image.Image, format settings.ExportFormat, quality uint8) ([]byte, error) {
	var b bytes.Buffer
	switch format {
	case settings.FormatJPEG:
		q := min(max(int(quality), 1), 100)
		if err := jpeg.Encode(&b, opaque(img), &jpeg.Options{Quality: q}); err != nil {
			return nil, err
		}
	default:
		if err := png.Encode(&b, img); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// opaque drops alpha the way the Rust build's to_rgb8 did: the color channels stay as stored
// and transparency is ignored, so a JPEG never blends a shadow against an arbitrary matte.
func opaque(img image.Image) image.Image {
	n, ok := img.(*image.NRGBA)
	if !ok {
		return img
	}
	out := image.NewRGBA(n.Rect)
	for i := 0; i < len(n.Pix); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = n.Pix[i], n.Pix[i+1], n.Pix[i+2], 255
	}
	return out
}

// Export encodes img into dir as stem.png or stem.jpg, numbering the name when it is taken.
func Export(img image.Image, dir, stem string, format settings.ExportFormat, quality uint8) (string, error) {
	data, err := Encode(img, format, quality)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := UniquePath(dir, Sanitize(stem), format.Extension())
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// UniquePath is dir/stem.ext, or dir/stem (2).ext and up when that exists.
func UniquePath(dir, stem, ext string) string {
	p := filepath.Join(dir, stem+"."+ext)
	for n := 2; exists(p); n++ {
		p = filepath.Join(dir, fmt.Sprintf("%s (%d).%s", stem, n, ext))
	}
	return p
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// Sanitize makes a capture title usable as a file name on every platform.
func Sanitize(name string) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	s = strings.TrimRight(strings.TrimSpace(s), ".")
	if s == "" {
		return "Capture"
	}
	return s
}

func HumanBytes(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%d KB", n>>10)
	}
	return fmt.Sprintf("%d B", n)
}

// LoadPixels decodes the image a screenshot hands out.
func LoadPixels(item store.Item) (image.Image, error) {
	path := item.Deliverable()
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	return img, nil
}

// HardLinkOutsideCache puts file under the data directory's Drag folder, named by the
// capture's title, so drags and file copies hand other apps a readable name. A hard link
// costs nothing; a copy is the fallback across volumes.
func HardLinkOutsideCache(file, id, title string) (string, error) {
	dir := filepath.Join(settings.DataDir(), "Drag", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	path := filepath.Join(dir, Sanitize(title)+filepath.Ext(file))
	os.Remove(path)
	if os.Link(file, path) == nil {
		return path, nil
	}
	data, err := os.ReadFile(file)
	if err == nil {
		err = os.WriteFile(path, data, 0o644)
	}
	if err != nil {
		return "", fmt.Errorf("writing %s: %w", path, err)
	}
	return path, nil
}

// formatOf picks the format a Save dialog's chosen name asks for, else fallback.
func formatOf(path string, fallback settings.ExportFormat) settings.ExportFormat {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return settings.FormatJPEG
	case ".png":
		return settings.FormatPNG
	}
	return fallback
}
