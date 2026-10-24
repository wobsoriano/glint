package editor

import (
	"fmt"
	"math"
	"os"
	"slices"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

func (e *Editor) view(c *ui.Context) {
	pal := theme.Apply(c)
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	for _, t := range e.toasts {
		c.Toast(t)
	}
	e.toasts = nil
	e.shortcuts(c)
	ui.Column(c).Fill().Background(pal.Base).Children(func() {
		e.toolbar(c, pal)
		e.styleBar(c, pal)
		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			e.canvasBox(c, pal)
			e.sidebar(c, pal)
		})
		e.bottomBar(c, pal)
	})
}

type shortcut struct {
	mods ui.Modifiers
	key  ui.Key
	do   func()
}

var toolKeys = map[ui.Key]string{
	ui.KeyV: "v", ui.KeyR: "r", ui.KeyF: "f", ui.KeyO: "o", ui.KeyL: "l", ui.KeyA: "a", ui.KeyP: "p",
	ui.KeyN: "n", ui.KeyT: "t", ui.KeyH: "h", ui.KeyX: "x", ui.KeyB: "b", ui.KeyC: "c",
}

// shortcuts registers every key the editor takes, then runs those pressed: all are asked each
// frame, so none goes missing from the next frame's routing.
func (e *Editor) shortcuts(c *ui.Context) {
	m := e.m
	cmd, shift := ui.Cmd, ui.Shift
	list := []shortcut{
		{cmd, ui.KeyC, e.copyImage},
		{cmd, ui.KeyS, func() { e.commit(thenExport) }},
		{cmd | shift, ui.KeyS, func() { e.commit(thenExportAsk) }},
		{cmd, ui.KeyEnter, func() { e.commit(thenClose) }},
		{cmd, ui.KeyZ, m.undo},
		{cmd | shift, ui.KeyZ, m.redo},
		{cmd, ui.KeyY, m.redo},
		{cmd | shift, ui.KeyY, m.redo},
		{cmd, ui.KeyP, e.pin},
		{cmd, ui.KeyA, m.selectAll},
		{cmd, ui.KeyEqual, func() { e.zoomStep(true) }},
		{cmd | shift, ui.KeyEqual, func() { e.zoomStep(true) }},
		{cmd, ui.KeyMinus, func() { e.zoomStep(false) }},
		{cmd, ui.Key0, func() { e.zoomTo(1, nil) }},
		{cmd, ui.Key1, func() { e.vp = viewport{} }},
		{cmd, ui.KeyW, e.requestClose},
		{0, ui.KeyEscape, m.escape},
	}
	if m.typing == 0 {
		list = append(list,
			shortcut{0, ui.KeyDelete, m.deleteSelected},
			shortcut{0, ui.KeyBackspace, m.deleteSelected},
		)
		for _, d := range []struct {
			key    ui.Key
			dx, dy float32
		}{{ui.KeyLeft, -1, 0}, {ui.KeyRight, 1, 0}, {ui.KeyUp, 0, -1}, {ui.KeyDown, 0, 1}} {
			list = append(list,
				shortcut{0, d.key, func() { m.nudge(d.dx, d.dy) }},
				shortcut{shift, d.key, func() { m.nudge(d.dx*10, d.dy*10) }},
			)
		}
		if m.crop != nil {
			list = append(list, shortcut{0, ui.KeyEnter, m.applyCrop})
		}
		for key, letter := range toolKeys {
			if t, ok := render.ToolFromKey(letter); ok {
				list = append(list, shortcut{0, key, func() { m.setTool(t) }})
			}
		}
	}
	var fire []func()
	for _, s := range list {
		if c.Shortcut(s.mods, s.key) {
			fire = append(fire, s.do)
		}
	}
	for _, f := range fire {
		f()
	}
}

func (e *Editor) zoomTo(z float32, anchor *render.Pt) {
	g := e.cv.geom
	if !e.cv.ready {
		return
	}
	vs := g.vsize()
	a := render.Pt{X: vs.w / 2, Y: vs.h / 2}
	if anchor != nil {
		a = *anchor
	}
	e.vp.zoomTo(z, a, g.full, vs, g.sf)
}

func (e *Editor) zoomStep(up bool) {
	if !e.cv.ready {
		return
	}
	e.zoomTo(zoomStep(e.cv.geom.view.zoom, up), nil)
}

func (e *Editor) zoomPercent() float32 {
	if !e.cv.ready {
		return 100
	}
	return e.cv.geom.view.zoom * 100
}

func (e *Editor) canvasInput(ev ui.InputEvent) bool {
	m := e.m
	g := e.cv.geom
	if !e.cv.ready {
		return false
	}
	shift := ev.Mods&ui.Shift != 0
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		if p, ok := g.toImage(ev.X, ev.Y); ok {
			before := m.typing
			m.pointerDown(p, 6*g.imagePerDIP(), shift, ev.Clicks)
			if m.typing != 0 && m.typing != before {
				e.canvasFocus.Focus()
			}
		}
		return true
	case ui.InputPointerMove:
		if m.drag == nil {
			return false
		}
		if p, ok := g.toImage(ev.X, ev.Y); ok {
			m.dragTo(p, shift)
		}
		return true
	case ui.InputPointerUp:
		if m.drag == nil {
			return false
		}
		m.dragEnd()
		return true
	case ui.InputScroll:
		vs := g.vsize()
		if ev.Mods&(ui.Ctrl|ui.Alt) != 0 {
			a := render.Pt{X: ev.X * g.sf, Y: ev.Y * g.sf}
			e.vp.zoomTo(g.view.zoom*float32(math.Exp(float64(-ev.DY)*0.0035)), a, g.full, vs, g.sf)
			return true
		}
		dx, dy := ev.DX, ev.DY
		if shift && dx == 0 {
			dx, dy = dy, 0
		}
		e.vp.pan(dx*g.sf, dy*g.sf, g.full, vs, g.sf)
		return true
	case ui.InputZoom:
		a := render.Pt{X: ev.X * g.sf, Y: ev.Y * g.sf}
		e.vp.zoomTo(g.view.zoom*ev.Scale, a, g.full, g.vsize(), g.sf)
		return true
	case ui.InputText:
		if m.typing == 0 {
			return false
		}
		m.typeText(ev.Text)
		return true
	case ui.InputKeyDown:
		if m.typing == 0 || ev.Mods&^ui.Shift != 0 {
			return false
		}
		switch ev.Key {
		case ui.KeyEnter:
			m.typeKey("enter", shift)
		case ui.KeyBackspace:
			m.typeKey("backspace", false)
		default:
			return false
		}
		return true
	}
	return false
}

func (e *Editor) canvasBox(c *ui.Context, pal palette) {
	m := e.m
	cursor := ui.CursorCrosshair
	switch {
	case m.crop != nil:
		cursor = ui.CursorCrosshair
	case m.tool == render.ToolSelect:
		cursor = ui.CursorDefault
	case m.tool == render.ToolText:
		cursor = ui.CursorText
	}
	box := ui.Box(c.Key("canvas")).Grow(1).Background(pal.Canvas).Clip().Focusable().Bind(&e.canvasFocus).Cursor(cursor).Label("Canvas")
	box.HandleInput(e.canvasInput)
	if a := m.ann(m.typing); a != nil && e.cv.ready {
		if b, ok := a.TextBox(); ok {
			q := e.cv.geom.toLocal(render.Pt{X: b.Right(), Y: b.Y})
			box.TextCaret(ui.Rect{X: q.X, Y: q.Y, W: 1, H: b.H / e.cv.geom.imagePerDIP()})
		}
	}
	box.Draw(func(p *ui.Painter, r ui.Rect) { e.paintCanvas(p, r, pal) })
}

func (e *Editor) toolbar(c *ui.Context, pal palette) {
	m := e.m
	bar := c.TitleBar()
	ui.Row(c).Height(max(bar.Height, 48)).Padding(0, bar.Right+8, 0, bar.Left+16).Gap(8).DragWindow().Children(func() {
		ui.Row(c).Width(220).Gap(8).Shrink(0).Children(func() {
			ui.Image(c, theme.Mark(false)).Size(20, 20)
			ui.Text(c, e.title).FontSize(13).TextColor(pal.Text).SingleLine()
		})
		ui.Spacer(c)
		ui.Row(c.Key("tools")).Gap(2).Padding(4).Radius(8).Background(pal.Card).Border(1, pal.Border).Children(func() {
			for _, t := range render.AllTools {
				switch t {
				case render.ToolRect, render.ToolCounter, render.ToolHighlight, render.ToolCrop:
					divider(c, pal)
				}
				icon, _ := theme.IconByName(t.Icon())
				if iconButton(c, t, icon, m.activeTool() == t, t.Name(), pal) {
					m.setTool(t)
				}
			}
		})
		ui.Spacer(c)
		if iconButton(c, "undo", theme.IconUndo, false, "Undo (Ctrl+Z)", pal) {
			m.undo()
		}
		if iconButton(c, "redo", theme.IconRedo, false, "Redo (Ctrl+Y)", pal) {
			m.redo()
		}
	})
}

func (e *Editor) styleBar(c *ui.Context, pal palette) {
	m := e.m
	ui.Row(c.Key("style")).Height(44).Shrink(0).Gap(8).PaddingX(16).Background(pal.Layer).BorderWidth(1, 0, 1, 0).BorderColor(pal.Border).Children(func() {
		if cr := m.crop; cr != nil {
			label(c, fmt.Sprintf("Crop %d × %d", int(cr.rect.W), int(cr.rect.H)), pal)
			divider(c, pal)
			for i, p := range cropPresets {
				if chip(c, "crop-"+p.name, p.name, cr.preset == i, pal) {
					m.setCropPreset(i)
				}
			}
			ui.Spacer(c)
			if button(c, "crop-cancel", "Cancel", nil, false, pal) {
				m.setTool(render.ToolSelect)
			}
			if button(c, "crop-apply", "Apply crop", iconRef(theme.IconCheck), true, pal) {
				m.applyCrop()
			}
			return
		}
		cx := m.styleContext()
		if !(cx.color || cx.redact) {
			hint := "Highlight keeps the selected area bright"
			switch {
			case m.tool == render.ToolHighlight:
				hint = "Drag over what matters; everything else dims"
			case len(m.sel) == 0:
				hint = "Click an annotation to select it. Shift-click or drag a box to select several"
			}
			label(c, hint, pal)
		}
		if cx.color {
			if col, ok := colorRow(c, "ink", m.ink.color, 22, pal); ok {
				m.setColor(col)
			}
		}
		if cx.stroke {
			divider(c, pal)
			label(c, "Width", pal)
			if v, ok := track(c, "stroke", "Width", m.ink.stroke, 1, 24, 110); ok {
				m.setStroke(v)
			}
			label(c, fmt.Sprintf("%.0f", m.ink.stroke), pal)
		}
		if cx.fill != nil {
			divider(c, pal)
			if chip(c, "fill", "Fill", *cx.fill, pal) {
				m.setFill(!*cx.fill)
			}
		}
		if cx.text {
			e.textControls(c, pal)
		}
		if cx.size {
			divider(c, pal)
			label(c, "Size", pal)
			if v, ok := track(c, "tsize", "Size", m.ink.textSize, 10, 160, 100); ok {
				m.setTextSize(v)
			}
			label(c, fmt.Sprintf("%.0f", m.ink.textSize), pal)
		}
		if cx.redact {
			strength := m.ink.redactStrength
			if s, kind, ok := m.selectedRedaction(); ok {
				strength = s
				for _, k := range render.AllRedactStyles {
					if chip(c, "rk-"+k.Label(), k.Label(), kind == k, pal) {
						m.setRedactKind(k)
					}
				}
				divider(c, pal)
			}
			label(c, "Strength", pal)
			if v, ok := track(c, "strength", "Strength", strength, 2, 48, 140); ok {
				m.setRedactStrength(v)
			}
			label(c, fmt.Sprintf("%.0f", strength), pal)
		}
		ui.Spacer(c)
		if n := len(m.sel); n > 0 {
			label(c, fmt.Sprintf("%d selected", n), pal)
			if iconButton(c, "del", theme.IconTrash, false, "Delete (Del)", pal) {
				m.deleteSelected()
			}
		}
	})
}

func nearestWeight(family string, w render.Weight) render.Weight {
	ws := render.Weights(family)
	if slices.Contains(ws, w) || len(ws) == 0 {
		return w
	}
	best := ws[0]
	for _, x := range ws {
		if absInt(int(x)-int(w)) < absInt(int(best)-int(w)) {
			best = x
		}
	}
	return best
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func (e *Editor) textControls(c *ui.Context, pal palette) {
	m := e.m
	font, style := m.textFont()
	divider(c, pal)
	family := font.Family
	families := render.Families()
	if !slices.Contains(families, family) {
		families = append([]string{family}, families...)
	}
	if ui.Select(c.Key("font"), &family, families).Label("Font").Width(160).Changed() {
		f := font
		f.Family = family
		f.Weight = nearestWeight(family, f.Weight)
		m.setFont(f)
	}
	for _, w := range render.Weights(font.Family) {
		if chip(c, "w-"+w.Label(), w.Label(), font.Weight == w, pal) {
			f := font
			f.Weight = w
			m.setFont(f)
		}
	}
	divider(c, pal)
	for _, a := range []struct {
		align render.Align
		icon  theme.IconName
		tip   string
	}{{render.AlignLeft, theme.IconAlignLeft, "Align left"}, {render.AlignCenter, theme.IconAlignCenter, "Align center"}, {render.AlignRight, theme.IconAlignRight, "Align right"}} {
		if iconButton(c, a.tip, a.icon, font.Align == a.align, a.tip, pal) {
			f := font
			f.Align = a.align
			m.setFont(f)
		}
	}
	divider(c, pal)
	for _, s := range []struct {
		style render.TextStyle
		name  string
	}{{render.TextPlain, "Plain"}, {render.TextOutline, "Outline"}, {render.TextPill, "Label"}} {
		if chip(c, "ts-"+s.name, s.name, style == s.style, pal) {
			m.setTextStyle(s.style)
		}
	}
}

func (e *Editor) bottomBar(c *ui.Context, pal palette) {
	m := e.m
	bw, bh := m.baseSize()
	l := render.NewFrame(m.doc, bw, bh, 1).Layout
	zb := func(key, text, tip string) bool {
		b := ui.ButtonBase(c.Key(key)).Height(28).PaddingX(8).Radius(theme.RadiusControl).Center().Label(tip).Tooltip(tip)
		if b.Hovered() {
			b.Background(pal.ControlHover)
		}
		clicked := b.Clicked()
		b.Children(func() { ui.Text(c, text).FontSize(12).TextColor(pal.Text) })
		return clicked
	}
	ui.Row(c.Key("bottom")).Height(52).Shrink(0).Gap(8).PaddingX(12).Background(pal.Layer).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Children(func() {
		ui.Row(c.Key("zoom")).Gap(2).Padding(2).Radius(theme.RadiusControl).Border(1, pal.Border).Children(func() {
			if zb("z-fit", "Fit", "Fit (Ctrl+1)") {
				e.vp = viewport{}
			}
			if zb("z-100", "100%", "Actual size (Ctrl+0)") {
				e.zoomTo(1, nil)
			}
			if zb("z-out", "−", "Zoom out (Ctrl+-)") {
				e.zoomStep(false)
			}
			ui.Text(c, fmt.Sprintf("%.0f%%", e.zoomPercent())).FontSize(12).TextColor(pal.Text).Width(48).TextAlign(ui.Center)
			if zb("z-in", "+", "Zoom in (Ctrl+=)") {
				e.zoomStep(true)
			}
		})
		ui.Text(c, fmt.Sprintf("%d × %d px", l.Width, l.Height)).FontSize(12).TextColor(pal.Muted)
		ui.Spacer(c)
		if iconButton(c, "pin", theme.IconPin, false, "Pin to screen (Ctrl+P)", pal) {
			e.pin()
		}
		if iconButton(c, "saveas", theme.IconFolder, false, "Save as… (Ctrl+Shift+S)", pal) {
			e.commit(thenExportAsk)
		}
		if button(c, "copy", "Copy", iconRef(theme.IconCopy), false, pal) {
			e.copyImage()
		}
		if button(c, "save", "Save", iconRef(theme.IconSave), false, pal) {
			e.commit(thenExport)
		}
		if button(c, "done", "Done", iconRef(theme.IconCheck), true, pal) {
			e.commit(thenClose)
		}
	})
}

func (e *Editor) sidebar(c *ui.Context, pal palette) {
	m := e.m
	ui.Scroll(c.Key("sidebar")).Width(312).Shrink(0).Background(pal.Layer).BorderWidth(0, 0, 0, 1).BorderColor(pal.Border).Children(func() {
		ui.Column(c).Padding(12).Children(func() {
			e.presetsSection(c, pal)
			e.backgroundSection(c, pal)
			e.layoutSection(c, pal)
			e.effectsSections(c, pal)
			ui.Row(c).Gap(8).Padding(0, 0, 8, 0).Children(func() {
				if button(c, "def", "Save as default", nil, false, pal) {
					e.saveDefaultStyle()
				}
				if button(c, "reset", "Reset look", nil, false, pal) {
					m.resetLook()
				}
			})
		})
	})
}

func (e *Editor) presetsSection(c *ui.Context, pal palette) {
	m := e.m
	section(c, "Presets", pal, func() {
		if len(m.presets) == 0 {
			note(c, "Save the current look to reuse it on other screenshots.", pal)
		}
		apply, del := -1, -1
		ui.Row(c.Key("list")).Gap(4).Wrap().Children(func() {
			for i, p := range m.presets {
				ui.Row(c.Key(i)).Radius(theme.RadiusControl).Background(pal.Control).Border(1, pal.Border).Children(func() {
					b := ui.ButtonBase(c.Key("apply")).Height(26).Padding(0, 4, 0, 8).Center()
					if b.Hovered() {
						b.Background(pal.ControlHover)
					}
					if b.Clicked() {
						apply = i
					}
					b.Children(func() { ui.Text(c, p.Name).FontSize(12).TextColor(pal.Text).SingleLine() })
					tip := "Delete “" + p.Name + "”"
					x := ui.ButtonBase(c.Key("del")).Height(26).PaddingX(4).Center().Label(tip).Tooltip(tip)
					if x.Hovered() {
						x.Background(pal.ControlHover)
					}
					if x.Clicked() {
						del = i
					}
					x.Children(func() { ui.Icon(c, theme.Icon(theme.IconClose)).Size(10, 10).TextColor(pal.Muted) })
				})
			}
		})
		if apply >= 0 {
			m.applyPreset(apply)
		}
		if del >= 0 {
			if name, ok := m.deletePreset(del); ok {
				e.savePresets()
				e.toast("Deleted “" + name + "”")
			}
		}
		ui.Row(c).Gap(4).Children(func() {
			if button(c, "pr-save", "Save look", iconRef(theme.IconCheck), false, pal) {
				name := m.addPreset()
				e.savePresets()
				e.toast("Saved “" + name + "”")
			}
			if button(c, "pr-import", "Import…", nil, false, pal) {
				e.importPresets()
			}
			if button(c, "pr-export", "Export…", nil, false, pal) {
				e.exportPresets()
			}
		})
	})
}

func (e *Editor) backgroundSection(c *ui.Context, pal palette) {
	m := e.m
	st := m.doc.Style
	walls := e.gallery.list()
	section(c, "Background", pal, func() {
		ui.Row(c.Key("modes")).Gap(4).Wrap().Children(func() {
			_, isGrad := st.Background.(render.GradientBackground)
			_, isSolid := st.Background.(render.SolidBackground)
			_, isImage := st.Background.(render.ImageBackground)
			_, isBlur := st.Background.(render.BlurredBackground)
			_, isNone := st.Background.(render.NoBackground)
			isNone = isNone || st.Background == nil
			if chip(c, "bg-g", "Gradient", isGrad, pal) {
				m.edit("bg", func(d *render.EditDoc) {
					d.Style.Background = render.GradientBackground{Gradient: render.Gradient{Stops: slices.Clone(e.autoColors), Angle: 135}}
				})
			}
			if chip(c, "bg-s", "Solid", isSolid, pal) {
				m.edit("bg", func(d *render.EditDoc) { d.Style.Background = render.SolidBackground{Color: render.RGB(0xF4F4F5)} })
			}
			if chip(c, "bg-i", "Wallpaper", isImage, pal) && len(walls) > 0 {
				m.edit("bg", func(d *render.EditDoc) { d.Style.Background = render.ImageBackground{Path: walls[0].path} })
			}
			if chip(c, "bg-b", "Blur", isBlur, pal) {
				m.edit("bg", func(d *render.EditDoc) { d.Style.Background = render.BlurredBackground{} })
			}
			if chip(c, "bg-n", "None", isNone, pal) {
				m.edit("bg", func(d *render.EditDoc) { d.Style.Background = render.NoBackground{} })
			}
		})
		switch bg := st.Background.(type) {
		case render.GradientBackground:
			g := bg.Gradient
			ui.Row(c.Key("grads")).Gap(4).Wrap().Children(func() {
				ui.Box(c.Key("auto")).Children(func() {
					if swatch(c, "g-auto", e.autoColors, slices.Equal(g.Stops, e.autoColors), 32, "Auto", pal) {
						m.edit("bg", func(d *render.EditDoc) {
							d.Style.Background = render.GradientBackground{Gradient: render.Gradient{Stops: slices.Clone(e.autoColors), Angle: g.Angle}}
						})
					}
					ui.Icon(c, theme.Icon(theme.IconSparkles)).Size(16, 16).TextColor(ui.RGB(255, 255, 255)).Absolute().Top(8).Left(8).PassThrough()
				})
				for i, ng := range render.Gradients {
					if swatch(c, i, ng.Gradient.Stops, slices.Equal(ng.Gradient.Stops, g.Stops), 32, ng.Name, pal) {
						m.edit("bg", func(d *render.EditDoc) {
							d.Style.Background = render.GradientBackground{Gradient: render.Gradient{Stops: slices.Clone(ng.Gradient.Stops), Angle: g.Angle}}
						})
					}
				}
			})
			if v, ok := slider(c, "angle", "Angle", g.Angle, 0, 360, fmt.Sprintf("%.0f°", g.Angle), pal); ok {
				m.edit("angle", func(d *render.EditDoc) {
					if gb, ok := d.Style.Background.(render.GradientBackground); ok {
						gb.Gradient.Angle = float32(math.Round(float64(v)/15) * 15)
						d.Style.Background = gb
					}
				})
			}
		case render.SolidBackground:
			ui.Row(c.Key("solids")).Gap(4).Wrap().Children(func() {
				for i, hex := range render.Solids {
					col := render.RGB(hex)
					if swatch(c, i, []render.Color{col}, bg.Color == col, 30, col.Hex(), pal) {
						m.edit("bg", func(d *render.EditDoc) { d.Style.Background = render.SolidBackground{Color: col} })
					}
				}
			})
			if v, ok := slider(c, "hue", "Custom hue", float32(e.hue), 0, 360, bg.Color.Hex(), pal); ok {
				e.hue = float64(v)
				m.edit("hue", func(d *render.EditDoc) { d.Style.Background = render.SolidBackground{Color: render.FromHSL(v, 0.7, 0.62)} })
			}
		case render.ImageBackground:
			if len(walls) == 0 {
				note(c, "Loading wallpapers…", pal)
			}
			ui.Row(c.Key("walls")).Gap(4).Wrap().Children(func() {
				for i, w := range walls {
					t := ui.ButtonBase(c.Key(i)).Size(126, 126*0.625).Radius(6).Clip().Border(2, ui.Transparent).Label(w.path).Tooltip(baseName(w.path))
					switch {
					case w.path == bg.Path:
						t.BorderColor(pal.Accent)
					case t.Hovered():
						t.BorderColor(pal.Muted)
					}
					if t.Clicked() {
						m.edit("bg", func(d *render.EditDoc) { d.Style.Background = render.ImageBackground{Path: w.path} })
					}
					t.Children(func() { ui.Image(c, w.thumb).Fill().Fit(ui.Cover).Radius(4) })
				}
			})
			if button(c, "bg-add", "Add image…", iconRef(theme.IconImage), false, pal) {
				e.addBackgroundImage()
			}
			note(c, "Your desktop wallpaper, the Windows wallpapers and images you add. Nothing is downloaded.", pal)
		case render.BlurredBackground:
			note(c, "A soft, blurred copy of your screenshot, so it always matches its colors.", pal)
		default:
			note(c, "Transparent background, for docs and slides.", pal)
		}
	})
}

func baseName(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '\\' || p[i] == '/' {
			return p[i+1:]
		}
	}
	return p
}

func (e *Editor) addBackgroundImage() {
	e.async(func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
			Parent: e.win, Title: "Add background image",
			Filters: []mygo.FileFilter{{Name: "Images", Extensions: []string{"png", "jpg", "jpeg", "webp"}}},
		})
		if err != nil || len(paths) == 0 {
			return
		}
		dest, err := e.gallery.add(paths[0])
		e.update(func() {
			if err != nil {
				e.toast(err.Error())
				return
			}
			e.m.edit("bg", func(d *render.EditDoc) { d.Style.Background = render.ImageBackground{Path: dest} })
		})
	})
}

func sameAspect(a, b *render.Aspect) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (e *Editor) layoutSection(c *ui.Context, pal palette) {
	m := e.m
	st := m.doc.Style
	section(c, "Layout", pal, func() {
		docSlider := func(key, title string, v, lo, hi float32, display string, set func(d *render.EditDoc, v float32)) {
			if nv, ok := slider(c, key, title, v, lo, hi, display, pal); ok {
				m.edit(key, func(d *render.EditDoc) { set(d, nv) })
			}
		}
		round := func(v float32) float32 { return float32(math.Round(float64(v))) }
		docSlider("pad", "Padding", st.Padding, 0, 240, fmt.Sprintf("%.0f", st.Padding), func(d *render.EditDoc, v float32) { d.Style.Padding = round(v) })
		docSlider("inset", "Inset", st.Inset, 0, 120, fmt.Sprintf("%.0f", st.Inset), func(d *render.EditDoc, v float32) { d.Style.Inset = round(v) })
		docSlider("radius", "Corner radius", st.Radius, 0, 48, fmt.Sprintf("%.0f", st.Radius), func(d *render.EditDoc, v float32) { d.Style.Radius = round(v) })
		docSlider("shadow", "Shadow", st.Shadow, 0, 120, fmt.Sprintf("%.0f", st.Shadow), func(d *render.EditDoc, v float32) { d.Style.Shadow = round(v) })
		docSlider("shadow-o", "Shadow opacity", st.ShadowOpacity, 0, 1, fmt.Sprintf("%.0f%%", st.ShadowOpacity*100), func(d *render.EditDoc, v float32) { d.Style.ShadowOpacity = v })
		if toggle(c, "balance", "Auto-balance inset color", st.AutoBalance, pal) {
			m.edit("balance", func(d *render.EditDoc) { d.Style.AutoBalance = !d.Style.AutoBalance })
		}
		note(c, "Aspect ratio", pal)
		ui.Row(c.Key("aspects")).Gap(4).Wrap().Children(func() {
			for _, a := range render.Aspects {
				if chip(c, "aspect-"+a.Name, a.Name, sameAspect(st.Aspect, a.Aspect), pal) {
					m.edit("aspect", func(d *render.EditDoc) {
						d.Style.Aspect = nil
						if a.Aspect != nil {
							v := *a.Aspect
							d.Style.Aspect = &v
						}
					})
				}
			}
		})
		ui.Row(c.Key("align")).Gap(12).Children(func() {
			note(c, "Position", pal)
			if i := grid9(c, "Position", int(st.Align), pal); i >= 0 {
				m.edit("align", func(d *render.EditDoc) { d.Style.Align = uint8(i) })
			}
		})
	})
	section(c, "Window frame", pal, func() {
		ui.Row(c.Key("chromes")).Gap(4).Wrap().Children(func() {
			for _, ch := range render.AllChromes {
				if chip(c, "chrome-"+ch.Label(), ch.Label(), st.Chrome == ch, pal) {
					m.edit("chrome", func(d *render.EditDoc) { d.Style.Chrome = ch })
				}
			}
		})
		if st.Chrome != render.ChromeNone && toggle(c, "ctitle", "Show window title", st.ChromeTitle, pal) {
			m.edit("ctitle", func(d *render.EditDoc) { d.Style.ChromeTitle = !d.Style.ChromeTitle })
		}
	})
}

type perspectivePreset struct {
	name       string
	rx, ry, rz float32
}

var perspectivePresets = []perspectivePreset{{"Flat", 0, 0, 0}, {"Left", 0, 24, 0}, {"Right", 0, -24, 0}, {"Tilt back", 22, 0, 0}, {"Hero", 14, -20, 4}}

func (e *Editor) effectsSections(c *ui.Context, pal palette) {
	m := e.m
	fx := m.doc.Effects
	round := func(v float32) float32 { return float32(math.Round(float64(v))) }
	hundredths := func(v float32) float32 { return float32(math.Round(float64(v)*100) / 100) }
	docSlider := func(key, title string, v, lo, hi float32, display string, set func(d *render.EditDoc, v float32)) {
		if nv, ok := slider(c, key, title, v, lo, hi, display, pal); ok {
			m.edit(key, func(d *render.EditDoc) { set(d, nv) })
		}
	}

	b := fx.Border
	section(c, "Border", pal, func() {
		if toggle(c, "border-on", "Border around screenshot", b.Enabled, pal) {
			m.edit("border-on", func(d *render.EditDoc) { d.Effects.Border.Enabled = !d.Effects.Border.Enabled })
		}
		if b.Enabled {
			docSlider("border-w", "Width", b.Width, 1, 40, fmt.Sprintf("%.0f", b.Width), func(d *render.EditDoc, v float32) { d.Effects.Border.Width = round(v) })
			if col, ok := colorRow(c, "border-c", b.Color, 22, pal); ok {
				m.edit("border-c", func(d *render.EditDoc) { d.Effects.Border.Color = col })
			}
		}
	})

	wm := fx.Watermark
	section(c, "Watermark", pal, func() {
		if toggle(c, "wm-on", "Text watermark", wm.Enabled, pal) {
			m.edit("wm-on", func(d *render.EditDoc) {
				d.Effects.Watermark.Enabled = !d.Effects.Watermark.Enabled
				if d.Effects.Watermark.Text == "" {
					user := os.Getenv("USERNAME")
					if user == "" {
						user = os.Getenv("USER")
					}
					if user == "" {
						user = "Glint"
					}
					d.Effects.Watermark.Text = "© " + user
				}
			})
		}
		if !wm.Enabled {
			return
		}
		text := wm.Text
		if ui.TextInput(c.Key("wm-text"), &text).Placeholder("Watermark text").Label("Watermark text").Changed() {
			m.edit("wm-text", func(d *render.EditDoc) { d.Effects.Watermark.Text = text })
		}
		fams := render.Families()
		ui.Row(c.Key("wm-fams")).Gap(4).Wrap().Children(func() {
			for _, fam := range fams[:min(10, len(fams))] {
				if chip(c, "wm-fam-"+fam, fam, wm.Font.Family == fam, pal) {
					f := wm.Font
					f.Family = fam
					if !slices.Contains(render.Weights(fam), f.Weight) {
						f.Weight = render.WeightRegular
					}
					m.edit("wm-font", func(d *render.EditDoc) { d.Effects.Watermark.Font = f })
				}
			}
		})
		ui.Row(c.Key("wm-weights")).Gap(4).Wrap().Children(func() {
			for _, w := range render.Weights(wm.Font.Family) {
				if chip(c, "wm-w-"+w.Label(), w.Label(), wm.Font.Weight == w, pal) {
					f := wm.Font
					f.Weight = w
					m.edit("wm-font", func(d *render.EditDoc) { d.Effects.Watermark.Font = f })
				}
			}
		})
		docSlider("wm-size", "Size", wm.Size, 8, 120, fmt.Sprintf("%.0f", wm.Size), func(d *render.EditDoc, v float32) { d.Effects.Watermark.Size = round(v) })
		docSlider("wm-op", "Opacity", wm.Opacity, 0.05, 1, fmt.Sprintf("%.0f%%", wm.Opacity*100), func(d *render.EditDoc, v float32) { d.Effects.Watermark.Opacity = v })
		if col, ok := colorRow(c, "wm-c", wm.Color, 22, pal); ok {
			m.edit("wm-c", func(d *render.EditDoc) { d.Effects.Watermark.Color = col })
		}
		cell := -1
		if !wm.Placement.IsTiled() {
			cell = int(wm.Placement.Cell())
		}
		ui.Row(c.Key("wm-place")).Gap(12).Children(func() {
			note(c, "Placement", pal)
			if i := grid9(c, "Placement", cell, pal); i >= 0 {
				m.edit("wm-pos", func(d *render.EditDoc) { d.Effects.Watermark.Placement = render.Grid(uint8(i)) })
			}
			if chip(c, "wm-tiled", "Tiled", wm.Placement.IsTiled(), pal) {
				m.edit("wm-pos", func(d *render.EditDoc) { d.Effects.Watermark.Placement = render.Tiled() })
			}
		})
		if wm.Placement.IsTiled() {
			docSlider("wm-rot", "Rotation", wm.Rotation, -90, 90, fmt.Sprintf("%.0f°", wm.Rotation), func(d *render.EditDoc, v float32) { d.Effects.Watermark.Rotation = round(v) })
		}
	})

	p := fx.Perspective
	section(c, "Perspective", pal, func() {
		ui.Row(c.Key("persp")).Gap(4).Wrap().Children(func() {
			for _, pp := range perspectivePresets {
				if chip(c, "persp-"+pp.name, pp.name, p.RotX == pp.rx && p.RotY == pp.ry && p.RotZ == pp.rz, pal) {
					m.edit("persp", func(d *render.EditDoc) {
						d.Effects.Perspective.RotX, d.Effects.Perspective.RotY, d.Effects.Perspective.RotZ = pp.rx, pp.ry, pp.rz
					})
				}
			}
		})
		docSlider("p-rx", "Rotate X", p.RotX, -60, 60, fmt.Sprintf("%.0f°", p.RotX), func(d *render.EditDoc, v float32) { d.Effects.Perspective.RotX = round(v) })
		docSlider("p-ry", "Rotate Y", p.RotY, -60, 60, fmt.Sprintf("%.0f°", p.RotY), func(d *render.EditDoc, v float32) { d.Effects.Perspective.RotY = round(v) })
		docSlider("p-rz", "Rotate Z", p.RotZ, -45, 45, fmt.Sprintf("%.0f°", p.RotZ), func(d *render.EditDoc, v float32) { d.Effects.Perspective.RotZ = round(v) })
		docSlider("p-zoom", "Zoom", p.Zoom, 0.4, 1.6, fmt.Sprintf("%.0f%%", p.Zoom*100), func(d *render.EditDoc, v float32) { d.Effects.Perspective.Zoom = hundredths(v) })
		docSlider("p-px", "Pan X", p.PanX, -0.5, 0.5, fmt.Sprintf("%.0f%%", p.PanX*100), func(d *render.EditDoc, v float32) { d.Effects.Perspective.PanX = hundredths(v) })
		docSlider("p-py", "Pan Y", p.PanY, -0.5, 0.5, fmt.Sprintf("%.0f%%", p.PanY*100), func(d *render.EditDoc, v float32) { d.Effects.Perspective.PanY = hundredths(v) })
		if button(c, "p-reset", "Reset perspective", nil, false, pal) {
			m.edit("p-reset", func(d *render.EditDoc) { d.Effects.Perspective = render.DefaultPerspective() })
		}
	})

	bl := fx.Blur
	section(c, "Progressive blur", pal, func() {
		if toggle(c, "blur-on", "Progressive blur", bl.Enabled, pal) {
			m.edit("blur-on", func(d *render.EditDoc) { d.Effects.Blur.Enabled = !d.Effects.Blur.Enabled })
		}
		if !bl.Enabled {
			return
		}
		ui.Row(c.Key("bm")).Gap(4).Wrap().Children(func() {
			label(c, "Shape", pal)
			if chip(c, "bm-r", "Radial", bl.Mode == render.BlurRadial, pal) {
				m.edit("bm", func(d *render.EditDoc) { d.Effects.Blur.Mode = render.BlurRadial })
			}
			if chip(c, "bm-d", "Directional", bl.Mode == render.BlurDirectional, pal) {
				m.edit("bm", func(d *render.EditDoc) { d.Effects.Blur.Mode = render.BlurDirectional })
			}
		})
		ui.Row(c.Key("be")).Gap(4).Wrap().Children(func() {
			label(c, "Applies to", pal)
			if chip(c, "be-c", "Screenshot", bl.Edge == render.BlurClipped, pal) {
				m.edit("be", func(d *render.EditDoc) { d.Effects.Blur.Edge = render.BlurClipped })
			}
			if chip(c, "be-b", "Whole scene", bl.Edge == render.BlurBleed, pal) {
				m.edit("be", func(d *render.EditDoc) { d.Effects.Blur.Edge = render.BlurBleed })
			}
		})
		docSlider("bl-s", "Strength", bl.Strength, 1, 100, fmt.Sprintf("%.0f", bl.Strength), func(d *render.EditDoc, v float32) { d.Effects.Blur.Strength = round(v) })
		docSlider("bl-st", "Start", bl.Start, 0, 1, fmt.Sprintf("%.0f%%", bl.Start*100), func(d *render.EditDoc, v float32) { d.Effects.Blur.Start = hundredths(v) })
		if bl.Mode == render.BlurDirectional {
			docSlider("bl-a", "Angle", bl.Angle, 0, 180, fmt.Sprintf("%.0f°", bl.Angle), func(d *render.EditDoc, v float32) { d.Effects.Blur.Angle = round(v) })
		}
	})
}
