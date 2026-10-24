package editor

import (
	"bufio"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/settings"
)

const thumbW, thumbH = 144, 90

type wallpaper struct {
	path  string
	thumb *ui.Bitmap
}

// gallery is the wallpapers a background can use: the desktop's, the user's own in the data
// folder, and the system's. One scan serves every editor of the process.
type gallery struct {
	mu        sync.Mutex
	walls     []wallpaper
	listeners []func()
}

var (
	galleryOnce sync.Once
	theGallery  *gallery
)

func sharedGallery(changed func()) *gallery {
	galleryOnce.Do(func() {
		theGallery = &gallery{}
		go theGallery.scan()
	})
	theGallery.mu.Lock()
	theGallery.listeners = append(theGallery.listeners, changed)
	theGallery.mu.Unlock()
	return theGallery
}

func (g *gallery) list() []wallpaper {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.walls)
}

func (g *gallery) changed() {
	g.mu.Lock()
	ls := slices.Clone(g.listeners)
	g.mu.Unlock()
	for _, f := range ls {
		f()
	}
}

func userWallpapers() string { return filepath.Join(settings.DataDir(), "Wallpapers") }

func isImage(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".jpg", ".jpeg", ".png", ".webp":
		return true
	}
	return false
}

func walk(dir string, out *[]string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, en := range entries {
		p := filepath.Join(dir, en.Name())
		if en.IsDir() {
			walk(p, out)
		} else if isImage(p) {
			*out = append(*out, p)
		}
	}
}

func wallpaperSources() []string {
	var out []string
	if p, err := platform.Wallpaper(); err == nil && p != "" {
		out = append(out, p)
	}
	walk(userWallpapers(), &out)
	windir := os.Getenv("WINDIR")
	if windir == "" {
		windir = `C:\Windows`
	}
	walk(filepath.Join(windir, "Web", "Wallpaper"), &out)
	seen := map[string]bool{}
	return slices.DeleteFunc(out, func(p string) bool {
		k := strings.ToLower(p)
		dup := seen[k]
		seen[k] = true
		return dup
	})
}

func wallThumb(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(bufio.NewReader(f))
	if err != nil {
		return nil, err
	}
	src := toRGBA(img)
	w, h := src.Rect.Dx(), src.Rect.Dy()
	k := max(float64(thumbW)/float64(w), float64(thumbH)/float64(h))
	sw, sh := max(int(float64(w)*k+0.999), thumbW), max(int(float64(h)*k+0.999), thumbH)
	small := render.Thumbnail(src, sw, sh)
	x, y := (sw-thumbW)/2, (sh-thumbH)/2
	return small.SubImage(image.Rect(x, y, x+thumbW, y+thumbH)).(*image.RGBA), nil
}

func (g *gallery) scan() {
	for _, p := range wallpaperSources() {
		t, err := wallThumb(p)
		if err != nil {
			continue
		}
		g.mu.Lock()
		g.walls = append(g.walls, wallpaper{p, ui.NewBitmap(t)})
		g.mu.Unlock()
		g.changed()
	}
}

// add copies an image into the user's wallpapers and returns the copy's path.
func (g *gallery) add(path string) (string, error) {
	if !isImage(path) {
		return "", errors.New("Choose a PNG, JPEG or WebP image")
	}
	dir := userWallpapers()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(filepath.Base(path), ext)
	dest := filepath.Join(dir, filepath.Base(path))
	for n := 1; ; n++ {
		if _, err := os.Stat(dest); errors.Is(err, os.ErrNotExist) {
			break
		}
		dest = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
	}
	data, err := os.ReadFile(path)
	if err == nil {
		err = os.WriteFile(dest, data, 0o644)
	}
	if err != nil {
		return "", fmt.Errorf("copying %s: %w", path, err)
	}
	t, err := wallThumb(dest)
	if err != nil {
		return "", errors.New("couldn't read that image")
	}
	g.mu.Lock()
	g.walls = slices.Insert(g.walls, 0, wallpaper{dest, ui.NewBitmap(t)})
	g.mu.Unlock()
	g.changed()
	return dest, nil
}
