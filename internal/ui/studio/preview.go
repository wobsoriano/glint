package studio

import (
	"fmt"
	"math"
	"runtime"

	"github.com/egoist/mygo/ui"

	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

// handle is how near, in DIPs, a press must be to a corner or the bubble's handle to take it.
const handle = 12

var fileManager = map[string]string{"windows": "Explorer", "darwin": "Finder"}[runtime.GOOS]

func (e *editor) hasCamera() bool {
	return e.session.Tracks.Camera != "" && e.project().Design.Camera.Visible
}

// card is the screen card inside the preview frame, in frame DIPs.
func (e *editor) card() rect {
	p := e.previewProject()
	return cardRect(&p, e.session.SourceW, e.session.SourceH, e.frame.w, e.frame.h)
}

// inCard maps a point in the frame to the card, normalized and clamped.
func (e *editor) inCard(x, y float32) (float32, float32) {
	c := e.card()
	return clampf((x-c.x)/c.w, 0, 1), clampf((y-c.y)/c.h, 0, 1)
}

func (e *editor) cropRatio() (float32, bool) {
	if m, ok := e.mode.(cropMode); ok {
		return m.ratio.value(e.session.SourceW, e.session.SourceH)
	}
	return 0, false
}

func (e *editor) enterCrop() {
	e.pause()
	e.setMode(cropMode{ratio: cropFree, before: e.project().Crop})
}

func (e *editor) setCropRatio(r cropRatio) {
	if m, ok := e.mode.(cropMode); ok {
		e.mode = cropMode{ratio: r, before: m.before}
	}
	ratio, fixed := r.value(e.session.SourceW, e.session.SourceH)
	w, h := e.session.SourceW, e.session.SourceH
	e.edit("crop", func(p *core.Project) { p.Crop = cropForRatio(p.Crop, ratio, fixed, w, h) })
}

func (e *editor) enterFocus(zoom int) {
	e.pause()
	e.edit("", func(p *core.Project) {
		if p.Zooms[zoom].Focus.Kind == core.FocusPointer {
			p.Zooms[zoom].Focus = core.FixedFocus(0.5, 0.5)
		}
	})
	e.setMode(focusMode{zoom})
}

// previewInput takes presses on the preview: on the crop or the focus while editing them, on
// the camera bubble to move or resize it, and anywhere else to play or pause.
func (e *editor) previewInput(ev ui.InputEvent) bool {
	x, y := ev.X-e.frame.x, ev.Y-e.frame.y
	switch {
	case ev.Kind == ui.InputPointerDown && ev.Button == 0:
		e.lastGroup = ""
		switch m := e.mode.(type) {
		case cropMode:
			card, c := e.card(), e.project().Crop
			nx, ny := e.inCard(x, y)
			corners := [4][2]float32{{c.X, c.Y}, {c.X + c.W, c.Y}, {c.X, c.Y + c.H}, {c.X + c.W, c.Y + c.H}}
			e.drag = cropFromDragging{nx, ny}
			for i, k := range corners {
				if absf(card.x+k[0]*card.w-x) < handle && absf(card.y+k[1]*card.h-y) < handle {
					e.drag = cropFromDragging{corners[3-i][0], corners[3-i][1]}
					return true
				}
			}
			if nx > c.X && nx < c.X+c.W && ny > c.Y && ny < c.Y+c.H {
				e.drag = cropMoveDragging{nx - c.X, ny - c.Y, c}
			}
		case focusMode:
			e.drag = focusDrag{m.zoom}
			e.previewDrag(x, y)
		default:
			b := bubbleRect(e.project().Design.Camera, e.frame.w, e.frame.h)
			onHandle := absf(x-(b.x+b.w)) < handle && absf(y-(b.y+b.h)) < handle
			switch {
			case e.hasCamera() && onHandle:
				e.drag = cameraSizeDrag{}
			case e.hasCamera() && b.contains(x, y):
				e.drag = cameraDrag{x - (b.x + b.w/2), y - (b.y + b.h/2)}
			case e.frame.contains(ev.X, ev.Y):
				e.togglePlay()
			}
		}
		return true
	case ev.Kind == ui.InputPointerMove && e.drag != nil:
		e.previewDrag(x, y)
		return true
	case ev.Kind == ui.InputPointerUp && e.drag != nil:
		e.endDrag()
		return true
	case ev.Kind == ui.InputScroll:
		if _, ok := e.mode.(normalMode); !ok || !e.hasCamera() {
			return false
		}
		if !bubbleRect(e.project().Design.Camera, e.frame.w, e.frame.h).contains(x, y) {
			return false
		}
		dy := ev.DY
		e.edit("camera", func(p *core.Project) {
			p.Design.Camera.Size = clampf(p.Design.Camera.Size*(1-dy/600), 0.08, 0.6)
		})
		return true
	}
	return false
}

// previewDrag moves what the press took to (x, y) in frame DIPs.
func (e *editor) previewDrag(x, y float32) {
	w, h := e.frame.w, e.frame.h
	if w <= 0 || h <= 0 {
		return
	}
	sw, sh := e.session.SourceW, e.session.SourceH
	switch d := e.drag.(type) {
	case cameraDrag:
		cx, cy := clampf((x-d.gx)/w, 0, 1), clampf((y-d.gy)/h, 0, 1)
		e.edit("camera", func(p *core.Project) { p.Design.Camera.Center = [2]float32{cx, cy} })
	case cameraSizeDrag:
		b := bubbleRect(e.project().Design.Camera, w, h)
		half := max(absf(x-(b.x+b.w/2)), absf(y-(b.y+b.h/2)))
		size := clampf(2*half/min(w, h), 0.08, 0.6)
		e.edit("camera", func(p *core.Project) { p.Design.Camera.Size = size })
	case focusDrag:
		fx, fy := e.inCard(x, y)
		e.edit("focus", func(p *core.Project) {
			if d.zoom < len(p.Zooms) {
				p.Zooms[d.zoom].Focus = core.FixedFocus(fx, fy)
			}
		})
	case cropFromDragging:
		tx, ty := e.inCard(x, y)
		ratio, fixed := e.cropRatio()
		e.edit("crop", func(p *core.Project) { p.Crop = cropFromDrag(d.ax, d.ay, tx, ty, ratio, fixed, sw, sh) })
	case cropMoveDragging:
		nx, ny := e.inCard(x, y)
		c := d.base
		c.X, c.Y = clampf(nx-d.gx, 0, 1-c.W), clampf(ny-d.gy, 0, 1-c.H)
		e.edit("crop", func(p *core.Project) { p.Crop = c })
	}
}

// layoutPreview fits the frame in the preview area and asks the player for frames of its
// size in pixels.
func (e *editor) layoutPreview(w, h, scale float32) {
	p := e.previewProject()
	ow, oh := p.OutputSize(e.session.SourceW, e.session.SourceH)
	aw, ah := max(w-48, 64), max(h-48, 64)
	k := min(aw/float32(ow), ah/float32(oh))
	lw, lh := float32(ow)*k, float32(oh)*k
	e.previewW, e.previewH = w, h
	e.frame = rect{(w - lw) / 2, (h - lh) / 2, lw, lh}
	e.player.setPreviewSize(uint32(math.Round(float64(lw*scale))), uint32(math.Round(float64(lh*scale))))
}

func (e *editor) preview(c *ui.Context, pal theme.Palette) {
	box := ui.Box(c.Key("preview")).Grow(1).MinHeight(120).Clip().Label("Preview")
	cursor := ui.CursorDefault
	if x, y, over := box.PointerPosition(); over {
		lx, ly := x-e.frame.x, y-e.frame.y
		switch e.mode.(type) {
		case cropMode, focusMode:
			cursor = ui.CursorCrosshair
		default:
			b := bubbleRect(e.project().Design.Camera, e.frame.w, e.frame.h)
			switch {
			case e.hasCamera() && absf(lx-(b.x+b.w)) < handle && absf(ly-(b.y+b.h)) < handle:
				cursor = ui.CursorResizeNWSE
			case e.hasCamera() && b.contains(lx, ly):
				cursor = ui.CursorMove
			case e.frame.contains(x, y):
				cursor = ui.CursorPointer
			}
		}
	}
	box.Cursor(cursor).HandleInput(e.previewInput)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		e.layoutPreview(r.W, r.H, p.Scale())
		e.paintPreview(p, r, pal)
	})
	box.Children(func() {
		e.previewToolbar(c, pal)
		e.noticeView(c, pal)
		if err := e.player.errText(); err != "" {
			ui.Text(c, err).FontSize(12).TextColor(pal.Danger).MaxWidth(360).Absolute().Top(10).Right(12)
		}
	})
}

func (e *editor) paintPreview(p *ui.Painter, r ui.Rect, pal theme.Palette) {
	f := ui.Rect{X: r.X + e.frame.x, Y: r.Y + e.frame.y, W: e.frame.w, H: e.frame.h}
	p.Shadow(f, 4, 0, 8, 28, 0, ui.RGBA(0, 0, 0, 0.35))
	p.Fill(f, pal.Canvas, 4)
	if e.video != nil {
		p.Clip(f, 4, func() { p.Image(e.video, f, ui.FillBox) })
	}
	at := func(x, y, w, h float32) ui.Rect { return ui.Rect{X: f.X + x, Y: f.Y + y, W: max(w, 0), H: max(h, 0)} }
	shade := ui.RGBA(0, 0, 0, 0.62)
	white := ui.RGB(255, 255, 255)
	dot := func(x, y float32) {
		p.Fill(at(x-6, y-6, 12, 12), white, 6)
		p.Stroke(at(x-6, y-6, 12, 12), pal.Accent, 6, 2)
	}
	mask := func(card, in rect) {
		p.Fill(at(card.x, card.y, card.w, in.y-card.y), shade, 0)
		p.Fill(at(card.x, in.y+in.h, card.w, card.y+card.h-in.y-in.h), shade, 0)
		p.Fill(at(card.x, in.y, in.x-card.x, in.h), shade, 0)
		p.Fill(at(in.x+in.w, in.y, card.x+card.w-in.x-in.w, in.h), shade, 0)
		p.Stroke(at(in.x, in.y, in.w, in.h), white, 0, 2)
	}
	switch m := e.mode.(type) {
	case cropMode:
		card, c := e.card(), e.project().Crop
		in := rect{card.x + c.X*card.w, card.y + c.Y*card.h, c.W * card.w, c.H * card.h}
		mask(card, in)
		for _, k := range [4][2]float32{{in.x, in.y}, {in.x + in.w, in.y}, {in.x, in.y + in.h}, {in.x + in.w, in.y + in.h}} {
			dot(k[0], k[1])
		}
		label := ui.Span{Text: fmt.Sprintf("%.0f × %.0f", math.Round(float64(c.W*float32(e.session.SourceW))), math.Round(float64(c.H*float32(e.session.SourceH)))), Size: 12, Color: white}
		lw, lh := p.MeasureText(0, label)
		p.Fill(at(in.x+6, in.y+6, lw+12, lh+4), ui.RGBA(0, 0, 0, 0.7), theme.RadiusControl)
		p.RichText(f.X+in.x+12, f.Y+in.y+8, 0, label)
	case focusMode:
		if m.zoom >= len(e.project().Zooms) {
			return
		}
		z := e.project().Zooms[m.zoom]
		fx, fy := float32(0.5), float32(0.5)
		if z.Focus.Kind == core.FocusFixed {
			fx, fy = z.Focus.X, z.Focus.Y
		}
		card := e.card()
		s := max(z.Scale, 1)
		w, h := card.w/s, card.h/s
		x := clampf(card.x+fx*card.w-w/2, card.x, card.x+card.w-w)
		y := clampf(card.y+fy*card.h-h/2, card.y, card.y+card.h-h)
		mask(card, rect{x, y, w, h})
		dot(card.x+fx*card.w, card.y+fy*card.h)
	default:
		_, dragging := e.drag.(cameraDrag)
		_, sizing := e.drag.(cameraSizeDrag)
		if !e.hasCamera() || !(e.tab == tabCamera || dragging || sizing) {
			return
		}
		cam := e.project().Design.Camera
		b := bubbleRect(cam, e.frame.w, e.frame.h)
		p.Stroke(at(b.x, b.y, b.w, b.h), pal.Accent, clampf(cam.Roundness, 0, 0.5)*b.w, 2)
		dot(b.x+b.w, b.y+b.h)
	}
}

func (e *editor) previewToolbar(c *ui.Context, pal theme.Palette) {
	switch m := e.mode.(type) {
	case cropMode:
		toolbarPanel(c, pal, func() {
			for _, r := range cropRatios {
				if chip(c, fmt.Sprintf("crop-ratio-%d", r), r.label(), m.ratio == r, pal) {
					e.setCropRatio(r)
				}
			}
			ui.Box(c).Width(8)
			if button(c, "crop-reset", "Reset", noIcon, false, pal).Clicked() {
				e.edit("crop", func(p *core.Project) { p.Crop = core.FullCrop })
			}
			if button(c, "crop-cancel", "Cancel", noIcon, false, pal).Clicked() {
				e.edit("crop", func(p *core.Project) { p.Crop = m.before })
				e.setMode(normalMode{})
			}
			if button(c, "crop-done", "Done", theme.IconCheck, true, pal).Clicked() {
				e.setMode(normalMode{})
			}
		})
	case focusMode:
		toolbarPanel(c, pal, func() {
			ui.Text(c, "Drag the frame to where the zoom should look").FontSize(12).TextColor(pal.Muted)
			if button(c, "focus-done", "Done", theme.IconCheck, true, pal).Clicked() {
				e.setMode(normalMode{})
			}
		})
	}
}

func toolbarPanel(c *ui.Context, pal theme.Palette, body func()) {
	ui.Row(c).Attach(ui.AnchorTop, ui.AnchorTop).Top(10).Gap(4).Padding(6, 8).Radius(theme.RadiusWindow).
		Background(pal.Flyout).Border(1, pal.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3)).Children(body)
}

func (e *editor) noticeView(c *ui.Context, pal theme.Palette) {
	n := e.notice
	if n == nil {
		return
	}
	pad := float32(16)
	if n.file != "" {
		pad = 8
	}
	ui.Row(c).Attach(ui.AnchorBottom, ui.AnchorBottom).Bottom(16).Gap(8).Padding(6, pad, 6, 16).Radius(theme.RadiusPill).
		Background(pal.Flyout).Border(1, pal.Border).Shadow(0, 8, 24, 0, ui.RGBA(0, 0, 0, 0.3)).Label("Notice").Children(func() {
		icon, col := theme.IconCheck, pal.Accent
		if n.err {
			icon, col = theme.IconClose, pal.Danger
		}
		ui.Icon(c, theme.Icon(icon)).Size(15, 15).TextColor(col)
		ui.Text(c, n.text).FontSize(13).MaxWidth(640)
		if n.file == "" {
			return
		}
		file := n.file
		if button(c, "reveal", "Show in "+fileManager, theme.IconFolder, false, pal).Clicked() {
			e.host.Reveal(file)
		}
		if button(c, "copy-file", "Copy", theme.IconCopy, false, pal).Clicked() {
			if err := e.host.CopyFile(file); err != nil {
				e.notify("Copy failed: "+err.Error(), true)
			} else {
				e.notify("Copied the video to the clipboard", false)
			}
		}
	})
}
