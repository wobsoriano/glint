// Package quicklook is the large preview of a capture: a screenshot at the size that fits, or
// a recording playing with its sound. Space or Esc closes it. One
// window serves every preview; opening another capture swaps its content.
package quicklook

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// Host does what the preview's buttons ask for. Each call runs on a goroutine of its own.
type Host interface {
	Copy(store.Item) error
	// Edit opens a screenshot in the editor or a recording in Studio.
	Edit(store.Item) error
	// Reveal shows the capture in Explorer or Finder.
	Reveal(store.Item) error
}

// Viewer owns the Quick Look window. Its methods are safe from any goroutine.
type Viewer struct {
	host Host

	// Main thread only.
	win    *mygo.Window
	item   store.Item
	bmp    *ui.Bitmap
	err    error
	note   string // what the last button failed with
	player *player
}

func New(host Host) *Viewer { return &Viewer{host: host} }

// Open previews item, in the open window if there is one.
func (v *Viewer) Open(item store.Item) {
	mygo.RunOnMain(func() {
		v.stopPlayer()
		v.item, v.bmp, v.err, v.note = item, nil, nil, ""
		if v.win == nil {
			w, h := windowSize(item)
			v.win = mygo.NewWindow(mygo.WindowOptions{Title: item.Meta.Title, Width: w, Height: h, MinWidth: 520, MinHeight: 360, Content: ui.View(v.view)})
			v.win.OnClosed(func() { v.stopPlayer(); v.win = nil })
		} else {
			v.win.SetTitle(item.Meta.Title)
		}
		win := v.win
		if item.Kind() == store.Recording {
			v.player = startPlayer(item, func(img *image.RGBA) {
				win.Update(func() {
					if v.win != win {
						return
					}
					if v.bmp == nil {
						v.bmp = ui.NewBitmap(img)
					} else {
						v.bmp.Update(img)
					}
				})
			})
		} else {
			v.load(item)
		}
		win.Show()
		win.Focus()
	})
}

// Refresh shows a capture's saved edit when the window previews it. A recording keeps playing
// as it is.
func (v *Viewer) Refresh(item store.Item) {
	mygo.RunOnMain(func() {
		if v.win == nil || v.item.Dir != item.Dir {
			return
		}
		v.item = item
		v.win.SetTitle(item.Meta.Title)
		if item.Kind() != store.Recording {
			v.load(item)
		}
	})
}

// load decodes a screenshot into the open window.
func (v *Viewer) load(item store.Item) {
	win := v.win
	go func() {
		img, err := decode(item.Deliverable())
		win.Update(func() {
			if v.win == win && v.item.Dir == item.Dir {
				v.err = err
				if err == nil {
					v.bmp = ui.NewBitmap(img)
				}
			}
		})
	}()
}

// Close closes the window, and reports whether it was open.
func (v *Viewer) Close() (open bool) {
	mygo.RunOnMain(func() {
		if open = v.win != nil; open {
			v.win.Close()
		}
	})
	return open
}

// Window is the open Quick Look window, or nil.
func (v *Viewer) Window() (w *mygo.Window) {
	mygo.RunOnMain(func() { w = v.win })
	return w
}

// Frames is how many frames of a recording have shown, and the last one's time.
func (v *Viewer) Frames() (n, ms int64) {
	mygo.RunOnMain(func() {
		if v.player != nil {
			n, ms = v.player.shown.Load(), v.player.posMS.Load()
		}
	})
	return n, ms
}

func (v *Viewer) stopPlayer() {
	if v.player != nil {
		v.player.stop()
		v.player = nil
	}
}

// windowSize fits the capture in 75% of the primary display with room for the bar, as the Rust
// build does, but never smaller than 520 x 360.
func windowSize(item store.Item) (int, int) {
	iw, ih := float64(max(item.Meta.Width, 1)), float64(max(item.Meta.Height, 1))
	mw, mh := 1200.0, 800.0
	if d := mygo.Screen.PrimaryDisplay(); d.WorkArea.Width > 0 {
		mw, mh = float64(d.Bounds.Width)*0.75, float64(d.Bounds.Height)*0.75
	}
	k := min((mw-40)/iw, (mh-100)/ih, 1)
	return int(max(iw*k+40, 520)), int(max(ih*k+100, 360))
}

func decode(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

func fileManager() string {
	if runtime.GOOS == "darwin" {
		return "Finder"
	}
	return "Explorer"
}

func (v *Viewer) view(c *ui.Context) {
	p := theme.Apply(c)
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(0, ui.KeySpace) {
		v.win.Close()
		return
	}
	item := v.item
	recording := item.Kind() == store.Recording
	ui.Column(c).Fill().Background(p.Base).Children(func() {
		stage := ui.Box(c).Grow(1).Padding(16).Center()
		if recording && v.player != nil {
			stage.Cursor(ui.CursorPointer).Tooltip("Click to pause or play")
			if stage.Clicked() {
				v.player.togglePlay()
			}
		}
		stage.Children(func() {
			switch {
			case v.bmp != nil:
				ui.Image(c, v.bmp).Fill().Fit(ui.Contain)
			case v.err != nil:
				ui.Text(c, v.err.Error()).TextColor(p.Muted)
			case v.player != nil && v.player.error() != nil:
				ui.Text(c, v.player.error().Error()).TextColor(p.Muted)
			default:
				ui.Text(c, "Loading…").TextColor(p.Muted)
			}
		})
		ui.Row(c).PaddingX(16).PaddingY(8).Gap(8).AlignItems(ui.Center).Background(p.Layer).Border(1, p.Border).Children(func() {
			text := info(item)
			if v.note != "" {
				text = v.note
			}
			ui.Text(c, text).FontSize(13).TextColor(p.Muted).Grow(1).MaxLines(1)
			v.button(c, "reveal", "Show in "+fileManager(), theme.IconFolder, false, v.host.Reveal)
			v.button(c, "copy", "Copy", theme.IconCopy, false, v.host.Copy)
			edit := "Edit"
			if recording {
				edit = "Open in Studio"
			}
			v.button(c, "edit", edit, theme.IconEdit, true, func(i store.Item) error {
				err := v.host.Edit(i)
				if err == nil {
					v.Close()
				}
				return err
			})
		})
	})
}

func (v *Viewer) button(c *ui.Context, key, label string, icon theme.IconName, primary bool, act func(store.Item) error) {
	p := theme.For(c)
	b := ui.ButtonBase(c.Key(key)).Height(32).PaddingX(12).Radius(theme.RadiusControl).Center().Border(1, p.Border)
	bg, fg := p.Control, p.Text
	if primary {
		bg, fg = p.Accent, p.OnAccent
	}
	if b.Hovered() {
		bg = bg.Mix(p.ControlHover, 0.5)
	}
	b.Background(bg).Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, theme.Icon(icon)).Size(14, 14).TextColor(fg)
			ui.Text(c, label).FontSize(13).TextColor(fg)
		})
	})
	if b.Clicked() {
		item := v.item
		go func() {
			if err := act(item); err != nil {
				mygo.RunOnMain(func() {
					if v.win != nil {
						v.note = label + " failed: " + err.Error()
						v.win.Invalidate()
					}
				})
			}
		}()
	}
}

func info(item store.Item) string {
	s := fmt.Sprintf("%d × %d", item.Meta.Width, item.Meta.Height)
	if d := item.Meta.DurationMs; d != nil {
		s += fmt.Sprintf(" · %d:%02d", *d/60_000, *d/1000%60)
	}
	return s
}
