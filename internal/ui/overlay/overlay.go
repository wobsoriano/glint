// Package overlay is the capture overlay: the frozen screen of every display, drawn 1:1 in a
// Panel window that covers the display and its taskbar, with a crosshair, a magnifier, the
// selection's size and the window under the pointer highlighted. Space switches between
// picking an area and picking a window, Enter takes the whole display, Esc cancels.
//
// The windows are made once per display and hidden between captures, which shows them again
// in 12 to 24 ms (docs/go-port/mygo-findings.md, section 1).
package overlay

import (
	"fmt"
	"image"
	"math"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// Mode is what the picked area is for.
type Mode int

const (
	ModeImage Mode = iota
	ModeText
)

// Shot is one display and its pixels, frozen when the capture started.
type Shot struct {
	Display platform.Display
	Image   *image.RGBA
}

// Pick is what the user chose: Rect in physical pixels of the virtual desktop, Image the
// frozen pixels of Rect, and Window the window clicked, if any.
type Pick struct {
	Mode   Mode
	Rect   platform.Rect
	Image  *image.RGBA
	Window *platform.Window
	Scale  float64
}

type Options struct {
	Mode      Mode
	ShowLoupe bool
	// Windows lists other apps' windows front to back, in physical pixels.
	Windows []platform.Window
	// Cursor is where the pointer is, in physical pixels: its display takes the keyboard.
	Cursor   image.Point
	OnPick   func(Pick)
	OnCancel func()
}

// Overlay covers every display while a capture is picked. Its methods run on the main thread.
type Overlay struct {
	surfaces []*surface
	opts     Options
	open     bool
	quitting bool
}

func New() *Overlay {
	o := &Overlay{}
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { o.quitting = true })
	return o
}

func (o *Overlay) IsOpen() bool { return o.open }

// Open shows shots, one per display, and calls OnPick or OnCancel once.
func (o *Overlay) Open(shots []Shot, opts Options) {
	if o.open {
		return
	}
	o.open, o.opts = true, opts
	o.fit(shots)
	for _, s := range o.surfaces {
		s.show(s.shot.Display.Bounds.Contains(opts.Cursor.X, opts.Cursor.Y) || len(o.surfaces) == 1)
	}
}

// fit keeps a window for each display, reusing those whose display is unchanged.
func (o *Overlay) fit(shots []Shot) {
	keep := make([]*surface, 0, len(shots))
	for _, sh := range shots {
		var found *surface
		for i, s := range o.surfaces {
			if s != nil && s.shot.Display.ID == sh.Display.ID && s.shot.Display.Bounds == sh.Display.Bounds {
				found, o.surfaces[i] = s, nil
				break
			}
		}
		if found == nil {
			found = newSurface(o, sh.Display)
		}
		found.load(sh, o.opts.Windows)
		keep = append(keep, found)
	}
	for _, s := range o.surfaces {
		if s != nil {
			s.win.Destroy()
		}
	}
	o.surfaces = keep
}

// Close hides the overlay without calling OnPick or OnCancel.
func (o *Overlay) Close() {
	if !o.open {
		return
	}
	o.open = false
	for _, s := range o.surfaces {
		s.hide()
	}
}

// Handles are the overlay windows, for checks that read the screen.
func (o *Overlay) Handles() []uintptr {
	var out []uintptr
	for _, s := range o.surfaces {
		out = append(out, s.win.NativeHandle())
	}
	return out
}

func (o *Overlay) decide(s *surface, r Result) {
	if !o.open {
		return
	}
	switch r.Verdict {
	case Cancel:
		o.Close()
		if o.opts.OnCancel != nil {
			o.opts.OnCancel()
		}
	case PickArea, PickWindow:
		b := s.shot.Display.Bounds
		pick := Pick{
			Mode:  o.opts.Mode,
			Rect:  platform.Rect{X: b.X + r.Rect.Min.X, Y: b.Y + r.Rect.Min.Y, W: r.Rect.Dx(), H: r.Rect.Dy()},
			Image: crop(s.shot.Image, r.Rect),
			Scale: s.shot.Display.Scale,
		}
		if r.Verdict == PickWindow {
			w := s.windows[r.Window]
			pick.Window = &w
		}
		o.Close()
		if o.opts.OnPick != nil {
			o.opts.OnPick(pick)
		}
	}
}

// crop copies r out of img into an image of its own.
func crop(img *image.RGBA, r image.Rectangle) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	for y := 0; y < r.Dy(); y++ {
		src := img.PixOffset(r.Min.X, r.Min.Y+y)
		copy(out.Pix[y*out.Stride:(y+1)*out.Stride], img.Pix[src:src+r.Dx()*4])
	}
	return out
}

// surface is the overlay of one display.
type surface struct {
	o       *Overlay
	shot    Shot
	win     *mygo.Window
	bmp     *ui.Bitmap
	sel     *selector
	windows []platform.Window // the windows sel indexes, in desktop pixels
	scale   float32
}

func newSurface(o *Overlay, d platform.Display) *surface {
	s := &surface{o: o, scale: float32(d.Scale)}
	dip := mygo.Screen.PrimaryDisplay().Bounds
	for _, md := range mygo.Screen.Displays() {
		if md.ID == d.ID {
			dip = md.Bounds
		}
	}
	s.win = mygo.NewWindow(mygo.WindowOptions{
		Title: "Glint capture", X: dip.X, Y: dip.Y, Width: dip.Width, Height: dip.Height,
		Frameless: true, AlwaysOnTop: true, SkipTaskbar: true, Hidden: true,
		DisableResize: true, DisableShadow: true, Panel: true,
		Content: ui.View(s.view),
	})
	// A panel closes on an Escape nothing takes and on Alt+F4: both cancel the capture. The
	// windows stay for the next capture, unless Glint is quitting.
	s.win.OnClose(func(e *mygo.CloseEvent) {
		if o.quitting {
			return
		}
		e.PreventDefault()
		s.o.decide(s, Result{Verdict: Cancel})
	})
	return s
}

func (s *surface) load(sh Shot, all []platform.Window) {
	s.shot = sh
	if s.bmp == nil {
		s.bmp = ui.NewBitmap(sh.Image).Pixelated()
	} else {
		s.bmp.Update(sh.Image)
	}
	b := sh.Display.Bounds
	s.windows = s.windows[:0]
	var rects []image.Rectangle
	for _, w := range all {
		r := image.Rect(w.Bounds.X-b.X, w.Bounds.Y-b.Y, w.Bounds.X-b.X+w.Bounds.W, w.Bounds.Y-b.Y+w.Bounds.H)
		if r.Overlaps(image.Rectangle{Max: image.Pt(b.W, b.H)}) {
			s.windows = append(s.windows, w)
			rects = append(rects, r)
		}
	}
	s.sel = newSelector(image.Pt(b.W, b.H), rects)
}

func (s *surface) show(focus bool) {
	prepare(s.win.NativeHandle(), s.shot.Display.Bounds)
	if focus {
		s.win.Show()
		s.win.Focus()
	} else {
		s.win.ShowInactive()
	}
	s.win.Invalidate()
}

func (s *surface) hide() { s.win.Hide() }

// px turns a pointer position in DIPs into this display's physical pixels.
func (s *surface) px(x, y float32) image.Point {
	return image.Pt(int(math.Round(float64(x*s.scale))), int(math.Round(float64(y*s.scale))))
}

func (s *surface) dip(v int) float32 { return float32(v) / s.scale }

func (s *surface) dipRect(r image.Rectangle) ui.Rect {
	return ui.Rect{X: s.dip(r.Min.X), Y: s.dip(r.Min.Y), W: s.dip(r.Dx()), H: s.dip(r.Dy())}
}

var keys = map[ui.Key]Key{ui.KeyEscape: KeyEscape, ui.KeySpace: KeySpace, ui.KeyEnter: KeyEnter, ui.KeyF: KeyF}

func (s *surface) view(c *ui.Context) {
	p := theme.Apply(c)
	c.Root().Background(ui.RGB(0, 0, 0))
	if c.Shortcut(0, ui.KeyEscape) {
		s.o.decide(s, Result{Verdict: Cancel})
	}
	box := ui.Box(c).Fill().Cursor(ui.CursorCrosshair).Focusable().AutoFocus()
	if !box.Hovered() {
		s.sel.leave()
	}
	box.HandleInput(func(ev ui.InputEvent) bool {
		switch ev.Kind {
		case ui.InputPointerDown:
			if ev.Button != 0 {
				return false
			}
			s.sel.down(s.px(ev.X, ev.Y))
		case ui.InputPointerMove:
			s.sel.move(s.px(ev.X, ev.Y))
		case ui.InputPointerUp:
			if ev.Button != 0 {
				return false
			}
			s.o.decide(s, s.sel.up(s.px(ev.X, ev.Y)))
		case ui.InputKeyDown:
			k, ok := keys[ev.Key]
			if !ok {
				return false
			}
			s.o.decide(s, s.sel.key(k))
		default:
			return false
		}
		s.win.Invalidate()
		return true
	})
	box.Draw(func(pt *ui.Painter, r ui.Rect) {
		s.scale = pt.Scale()
		pt.Image(s.bmp, r, ui.FillBox)
		s.paint(pt, r, p)
	})
}

var (
	dimColor   = ui.RGBA(0, 0, 0, float32(0x73)/255)
	idleDim    = ui.RGBA(0, 0, 0, float32(0x40)/255)
	hoverTint  = ui.RGBA(0x7C, 0x8C, 0xFF, float32(0x1A)/255)
	labelBack  = ui.RGBA(0, 0, 0, 0.8)
	white      = ui.RGB(255, 255, 255)
	hintBack   = ui.RGBA(0x18, 0x18, 0x1B, 0.9)
	hintBorder = ui.RGBA(255, 255, 255, float32(0x1F)/255)
	hintText   = ui.RGB(0xED, 0xED, 0xED)
)

func (s *surface) paint(pt *ui.Painter, r ui.Rect, p theme.Palette) {
	focus, isSel, ok := s.sel.focus()
	if ok {
		f := s.dipRect(focus)
		f.X, f.Y = f.X+r.X, f.Y+r.Y
		pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: f.Y - r.Y}, dimColor, 0)
		pt.Fill(ui.Rect{X: r.X, Y: f.Y + f.H, W: r.W, H: r.Y + r.H - f.Y - f.H}, dimColor, 0)
		pt.Fill(ui.Rect{X: r.X, Y: f.Y, W: f.X - r.X, H: f.H}, dimColor, 0)
		pt.Fill(ui.Rect{X: f.X + f.W, Y: f.Y, W: r.X + r.W - f.X - f.W, H: f.H}, dimColor, 0)
		border := white
		if isSel || s.sel.windowMode {
			border = p.Accent
		}
		if !isSel {
			pt.Fill(f, hoverTint, 0)
		}
		pt.Stroke(ui.Rect{X: f.X - 1, Y: f.Y - 1, W: f.W + 2, H: f.H + 2}, border, 0, 2)
		label := fmt.Sprintf("%d × %d", focus.Dx(), focus.Dy())
		if !isSel {
			label = fmt.Sprintf("%s  ·  %s", firstRunes(s.windows[s.sel.hover].Title, 48), label)
		}
		s.label(pt, r, f, label)
	} else {
		pt.Fill(r, idleDim, 0)
	}
	s.hint(pt, r, p)
	if s.o.opts.ShowLoupe && !s.sel.windowMode && s.sel.inside {
		s.loupe(pt, r)
	}
}

// label draws the size, and the title of a hovered window, under the focus rectangle, or
// above it where the display ends.
func (s *surface) label(pt *ui.Painter, r, f ui.Rect, text string) {
	span := ui.Span{Text: text, Size: theme.Caption, Color: white}
	w, h := pt.MeasureText(0, span)
	box := ui.Rect{X: f.X, Y: f.Y + f.H + 8, W: w + 16, H: h + 8}
	if box.Y+box.H+4 > r.Y+r.H {
		box.Y = max(f.Y-box.H-8, r.Y+4)
	}
	box.X = min(max(box.X, r.X+4), r.X+r.W-box.W-4)
	pt.Fill(box, labelBack, theme.RadiusControl+2)
	pt.RichText(box.X+8, box.Y+4, 0, span)
}

func (s *surface) hint(pt *ui.Painter, r ui.Rect, p theme.Palette) {
	text := "Drag to capture an area  ·  Click a window  ·  Space for windows  ·  Enter for the display  ·  Esc to cancel"
	icon := theme.IconArea
	switch {
	case s.o.opts.Mode == ModeText:
		text, icon = "Select text to copy  ·  Esc to cancel", theme.IconScan
	case s.sel.windowMode:
		text, icon = "Click a window to capture it  ·  Space for an area  ·  Esc to cancel", theme.IconWindow
	}
	span := ui.Span{Text: text, Size: theme.Body, Color: hintText}
	w, h := pt.MeasureText(0, span)
	const iconSize, gap, padX, padY = 15, 8, 16, 8
	box := ui.Rect{W: padX + iconSize + gap + w + padX, H: h + padY*2}
	box.X, box.Y = r.X+(r.W-box.W)/2, r.Y+24
	pt.Fill(box, hintBack, box.H/2)
	pt.Stroke(box, hintBorder, box.H/2, 1)
	pt.Icon(theme.Icon(icon), ui.Rect{X: box.X + padX, Y: box.Y + (box.H-iconSize)/2, W: iconSize, H: iconSize}, p.Accent)
	pt.RichText(box.X+padX+iconSize+gap, box.Y+padY, 0, span)
}

// loupe magnifies the 15 by 15 physical pixels around the pointer 8 times, with a grid, the
// centre pixel outlined, and that pixel's color and position below.
func (s *surface) loupe(pt *ui.Painter, r ui.Rect) {
	const n, side = 15, 120
	at := s.sel.cursor
	bounds := s.shot.Image.Rect
	at.X, at.Y = min(max(at.X, 0), bounds.Dx()-1), min(max(at.Y, 0), bounds.Dy()-1)
	cx, cy := r.X+s.dip(at.X), r.Y+s.dip(at.Y)
	box := ui.Rect{X: cx + 24, Y: cy + 24, W: side, H: side}
	const bar = 22
	if box.X+side > r.X+r.W-8 {
		box.X = cx - side - 24
	}
	if box.Y+side+bar > r.Y+r.H-8 {
		box.Y = cy - side - bar - 24
	}
	whole := ui.Rect{X: box.X, Y: box.Y, W: side, H: side + bar}
	pt.Shadow(whole, 8, 0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.45))
	pt.Clip(whole, 8, func() {
		pt.Fill(whole, ui.RGB(0, 0, 0), 0)
		src := ui.Rect{X: float32(at.X - n/2), Y: float32(at.Y - n/2), W: n, H: n}
		pt.ImageRegion(s.bmp, src, box, ui.ImageOptions{})
		cell := float32(side) / n
		grid := ui.RGBA(0, 0, 0, 0.14)
		for i := 1; i < n; i++ {
			o := float32(i) * cell
			pt.Line(box.X+o, box.Y, box.X+o, box.Y+side, 1, grid)
			pt.Line(box.X, box.Y+o, box.X+side, box.Y+o, 1, grid)
		}
		pt.Stroke(ui.Rect{X: box.X + cell*(n/2), Y: box.Y + cell*(n/2), W: cell, H: cell}, white, 0, 1.5)
		barRect := ui.Rect{X: box.X, Y: box.Y + side, W: side, H: bar}
		pt.Fill(barRect, ui.RGBA(0, 0, 0, 0.85), 0)
		c := s.shot.Image.RGBAAt(at.X, at.Y)
		hex := ui.Span{Text: fmt.Sprintf("#%02X%02X%02X", c.R, c.G, c.B), Size: 11, Color: white}
		pos := ui.Span{Text: fmt.Sprintf("%d, %d", s.shot.Display.Bounds.X+at.X, s.shot.Display.Bounds.Y+at.Y), Size: 11, Color: white}
		_, th := pt.MeasureText(0, hex)
		pw, _ := pt.MeasureText(0, pos)
		ty := barRect.Y + (bar-th)/2
		pt.RichText(barRect.X+8, ty, 0, hex)
		pt.RichText(barRect.X+side-8-pw, ty, 0, pos)
	})
	pt.Stroke(whole, ui.RGBA(255, 255, 255, 0.9), 8, 2)
}

func firstRunes(s string, n int) string {
	for i := range s {
		if n == 0 {
			return s[:i]
		}
		n--
	}
	return s
}
