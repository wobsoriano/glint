package studio

import (
	"fmt"
	"math"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"github.com/wobsoriano/glint/internal/render"
	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

func percent(v float32) string { return fmt.Sprintf("%.0f%%", v*100) }

func (e *editor) sidebar(c *ui.Context, pal theme.Palette) {
	title := ""
	ui.Column(c.Key("sidebar")).Width(312).Shrink(0).Background(pal.Layer).BorderWidth(0, 0, 0, 1).BorderColor(pal.Border).Children(func() {
		ui.Row(c).Wrap().Gap(2).Padding(8).BorderWidth(0, 0, 1, 0).BorderColor(pal.Border).Children(func() {
			for _, t := range tabs {
				if t.tab == e.tab {
					title = t.title
				}
				if iconButtonSized(c, "tab-"+t.title, t.icon, e.tab == t.tab, true, t.title, 30, pal) {
					e.tab = t.tab
				}
			}
		})
		ui.Scroll(c.Key("sidebar-body")).Grow(1).Padding(16).Gap(12).Children(func() {
			ui.Text(c, title).FontSize(15).FontWeight(theme.Semibold)
			switch e.tab {
			case tabBackground:
				e.backgroundTab(c, pal)
			case tabLayout:
				e.layoutTab(c, pal)
			case tabCursor:
				e.cursorTab(c, pal)
			case tabKeystrokes:
				e.keystrokesTab(c, pal)
			case tabCamera:
				e.cameraTab(c, pal)
			case tabAudio:
				e.audioTab(c, pal)
			case tabZoom:
				e.zoomTab(c, pal)
			case tabCaptions:
				e.captionsTab(c, pal)
			case tabPresets:
				e.presetsTab(c, pal)
			}
		})
	})
}

// sliderEdit is a slider that edits the project as one undo step per drag.
func (e *editor) sliderEdit(c *ui.Context, key, title string, value, lo, hi float32, display string, pal theme.Palette, set func(p *core.Project, v float32)) {
	if v, changed := slider(c, key, title, value, lo, hi, display, pal); changed {
		e.edit(key, func(p *core.Project) { set(p, v) })
	}
}

func (e *editor) toggleEdit(c *ui.Context, key, title string, on bool, pal theme.Palette, flip func(p *core.Project)) {
	if toggle(c, key, title, on, pal) {
		e.edit("", flip)
	}
}

func (e *editor) setBackground(bg render.Background) {
	e.edit("", func(p *core.Project) { p.Design.Background = bg })
}

func (e *editor) backgroundTab(c *ui.Context, pal theme.Palette) {
	d := e.project().Design
	section(c, "Background", pal, func() {
		ui.Row(c).Wrap().Gap(4).Children(func() {
			_, isGrad := d.Background.(render.GradientBackground)
			_, isSolid := d.Background.(render.SolidBackground)
			_, isBlur := d.Background.(render.BlurredBackground)
			_, isImage := d.Background.(render.ImageBackground)
			_, isNone := d.Background.(render.NoBackground)
			if chip(c, "bg-g", "Gradient", isGrad, pal) {
				e.setBackground(render.GradientBackground{Gradient: cloneGradient(render.Gradients[0].Gradient)})
			}
			if chip(c, "bg-s", "Solid", isSolid, pal) {
				e.setBackground(render.SolidBackground{Color: render.RGB(0x27272A)})
			}
			if chip(c, "bg-b", "Blur", isBlur, pal) {
				e.setBackground(render.BlurredBackground{})
			}
			if chip(c, "bg-i", "Image", isImage, pal) {
				if e.wallpaper != "" {
					e.setBackground(render.ImageBackground{Path: e.wallpaper})
				} else {
					e.setBackground(render.GradientBackground{Gradient: cloneGradient(render.Gradients[1].Gradient)})
				}
			}
			if chip(c, "bg-n", "None", isNone, pal) {
				e.setBackground(render.NoBackground{})
			}
		})
		switch bg := d.Background.(type) {
		case render.GradientBackground:
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for i, g := range render.Gradients {
					if swatch(c, "grad-"+g.Name, g.Gradient.Stops, slices.Equal(g.Gradient.Stops, bg.Gradient.Stops), 34, g.Name, pal) {
						next := cloneGradient(g.Gradient)
						next.Angle = bg.Gradient.Angle
						e.setBackground(render.GradientBackground{Gradient: next})
					}
					_ = i
				}
			})
			e.sliderEdit(c, "angle", "Angle", bg.Gradient.Angle, 0, 360, fmt.Sprintf("%.0f°", bg.Gradient.Angle), pal, func(p *core.Project, v float32) {
				if g, ok := p.Design.Background.(render.GradientBackground); ok {
					g.Gradient = cloneGradient(g.Gradient)
					g.Gradient.Angle = float32(math.Round(float64(v)/15) * 15)
					p.Design.Background = g
				}
			})
		case render.SolidBackground:
			ui.Row(c).Wrap().Gap(4).Children(func() {
				for _, hex := range render.Solids {
					col := render.RGB(hex)
					if swatch(c, "solid-"+col.Hex(), []render.Color{col}, bg.Color == col, 30, col.Hex(), pal) {
						e.setBackground(render.SolidBackground{Color: col})
					}
				}
			})
			if v, changed := slider(c, "hue", "Custom hue", e.hue, 0, 360, bg.Color.Hex(), pal); changed {
				e.hue = v
				e.edit("hue", func(p *core.Project) {
					p.Design.Background = render.SolidBackground{Color: render.FromHSL(v, 0.7, 0.62)}
				})
			}
		case render.BlurredBackground:
			hint(c, "A soft, blurred copy of the recording behind it.", pal)
		case render.ImageBackground:
			ui.Text(c, filepath.Base(bg.Path)).FontSize(12).TextColor(pal.Muted).SingleLine()
			ui.Row(c).Gap(8).Children(func() {
				if button(c, "bg-pick", "Choose image…", theme.IconImage, false, pal).Clicked() {
					go func() {
						if path := e.sh.pickImage(); path != "" {
							e.sh.update(func() { e.setBackground(render.ImageBackground{Path: path}) })
						}
					}()
				}
				if e.wallpaper != "" && button(c, "bg-wall", "Wallpaper", theme.IconMonitor, false, pal).Clicked() {
					e.setBackground(render.ImageBackground{Path: e.wallpaper})
				}
			})
		case render.NoBackground:
			hint(c, "No background: the card fills the frame.", pal)
		}
	})
	section(c, "Frame", pal, func() {
		e.sliderEdit(c, "pad", "Padding", d.Padding, 0, 0.2, percent(d.Padding), pal, func(p *core.Project, v float32) { p.Design.Padding = v })
		e.sliderEdit(c, "radius", "Corner radius", d.CornerRadius, 0, 0.08, percent(d.CornerRadius), pal, func(p *core.Project, v float32) { p.Design.CornerRadius = v })
		e.sliderEdit(c, "shadow", "Shadow", d.Shadow, 0, 1, percent(d.Shadow), pal, func(p *core.Project, v float32) { p.Design.Shadow = v })
	})
}

func cloneGradient(g render.Gradient) render.Gradient {
	g.Stops = slices.Clone(g.Stops)
	return g
}

func (e *editor) layoutTab(c *ui.Context, pal theme.Palette) {
	p := e.project()
	ow, oh := p.OutputSize(e.session.SourceW, e.session.SourceH)
	sw, sh := e.session.SourceW, e.session.SourceH
	_, cropping := e.mode.(cropMode)
	section(c, "Aspect ratio", pal, func() {
		ui.Row(c).Wrap().Gap(4).Children(func() {
			for _, a := range core.AllAspectPresets {
				if chip(c, "aspect-"+string(a), a.Label(), p.Aspect == a, pal) {
					e.edit("", func(p *core.Project) { p.Aspect = a })
				}
			}
		})
		if p.Aspect != core.AspectOriginal {
			ui.Row(c).Gap(4).Children(func() {
				if chip(c, "fill", "Fill", p.AspectMode == core.AspectFill, pal) {
					e.edit("", func(p *core.Project) { p.AspectMode = core.AspectFill })
				}
				if chip(c, "fit", "Fit", p.AspectMode == core.AspectFit, pal) {
					e.edit("", func(p *core.Project) { p.AspectMode = core.AspectFit })
				}
			})
			if p.AspectMode == core.AspectFill {
				hint(c, "Fill crops into the recording and follows the pointer.", pal)
			} else {
				hint(c, "Fit keeps the whole recording visible on the background.", pal)
			}
		}
		hint(c, fmt.Sprintf("Export size %d × %d", ow, oh), pal)
	})
	section(c, "Crop", pal, func() {
		cr := p.Crop
		hint(c, fmt.Sprintf("Showing %.0f × %.0f of %d × %d", math.Round(float64(cr.W*float32(sw))), math.Round(float64(cr.H*float32(sh))), sw, sh), pal)
		ui.Row(c).Gap(8).Children(func() {
			label := "Crop video"
			if cropping {
				label = "Cropping…"
			}
			if button(c, "crop", label, theme.IconCrop, cropping, pal).Clicked() && !cropping {
				e.enterCrop()
			}
			if cr != core.FullCrop && button(c, "crop-clear", "Reset", noIcon, false, pal).Clicked() {
				e.edit("", func(p *core.Project) { p.Crop = core.FullCrop })
			}
		})
	})
}

func (e *editor) cursorTab(c *ui.Context, pal theme.Palette) {
	p := e.project()
	cur := p.Design.Cursor
	section(c, "Cursor", pal, func() {
		e.toggleEdit(c, "cursor-show", "Show cursor", cur.Visible, pal, func(p *core.Project) { p.Design.Cursor.Visible = !p.Design.Cursor.Visible })
		e.sliderEdit(c, "cursor-size", "Size", cur.Size, 0.5, 4, fmt.Sprintf("%.1f×", cur.Size), pal, func(p *core.Project, v float32) {
			p.Design.Cursor.Size = float32(math.Round(float64(v)*10) / 10)
		})
		e.sliderEdit(c, "cursor-smooth", "Smoothing", cur.Smoothing, 0, 1, percent(cur.Smoothing), pal, func(p *core.Project, v float32) { p.Design.Cursor.Smoothing = v })
		e.toggleEdit(c, "clicks", "Click highlights", p.ClickHighlights, pal, func(p *core.Project) { p.ClickHighlights = !p.ClickHighlights })
	})
}

var keyPositionLabels = map[core.KeystrokePosition]string{
	core.KeysTopLeft: "Top left", core.KeysTopCenter: "Top", core.KeysTopRight: "Top right",
	core.KeysBottomLeft: "Bottom left", core.KeysBottomCenter: "Bottom", core.KeysBottomRight: "Bottom right",
}

func (e *editor) keystrokesTab(c *ui.Context, pal theme.Palette) {
	k := e.project().Keystrokes
	section(c, "Keystrokes", pal, func() {
		e.toggleEdit(c, "keys", "Show keystrokes", k.Enabled, pal, func(p *core.Project) { p.Keystrokes.Enabled = !p.Keystrokes.Enabled })
		hint(c, "Shortcuts pressed while recording appear as captions.", pal)
		for row := range 2 {
			ui.Row(c).Gap(4).Children(func() {
				for _, pos := range core.AllKeystrokePositions[row*3 : row*3+3] {
					if chip(c, "keypos-"+string(pos), keyPositionLabels[pos], k.Position == pos, pal) {
						e.edit("", func(p *core.Project) { p.Keystrokes.Position = pos })
					}
				}
			})
		}
	})
}

func (e *editor) cameraTab(c *ui.Context, pal theme.Palette) {
	if e.session.Tracks.Camera == "" {
		hint(c, "This recording has no camera track.", pal)
		return
	}
	cam := e.project().Design.Camera
	section(c, "Camera", pal, func() {
		e.toggleEdit(c, "cam-show", "Show camera", cam.Visible, pal, func(p *core.Project) { p.Design.Camera.Visible = !p.Design.Camera.Visible })
		e.sliderEdit(c, "cam-size", "Size", cam.Size, 0.08, 0.6, percent(cam.Size), pal, func(p *core.Project, v float32) { p.Design.Camera.Size = v })
		e.sliderEdit(c, "cam-round", "Roundness", cam.Roundness, 0, 0.5, percent(cam.Roundness*2), pal, func(p *core.Project, v float32) { p.Design.Camera.Roundness = v })
		hint(c, "Drag the camera on the preview to move it. Scroll over it or drag its handle to resize.", pal)
	})
}

func (e *editor) audioTab(c *ui.Context, pal theme.Palette) {
	a := e.project().Audio
	track := func(key, title string, present bool, t core.AudioTrack, pick func(p *core.Project) *core.AudioTrack) {
		section(c, title, pal, func() {
			if !present {
				hint(c, "Not recorded.", pal)
				return
			}
			e.toggleEdit(c, key+"-on", "Include", t.Enabled, pal, func(p *core.Project) { pick(p).Enabled = !pick(p).Enabled })
			e.sliderEdit(c, key, "Volume", t.Volume, 0, 2, percent(t.Volume), pal, func(p *core.Project, v float32) { pick(p).Volume = v })
		})
	}
	track("mic-vol", "Microphone", e.session.Tracks.Mic != "", a.Mic, func(p *core.Project) *core.AudioTrack { return &p.Audio.Mic })
	track("sys-vol", "System audio", e.session.Tracks.System != "", a.System, func(p *core.Project) *core.AudioTrack { return &p.Audio.System })
}

func (e *editor) zoomTab(c *ui.Context, pal theme.Palette) {
	p := e.project()
	if i, ok := e.sel.zoom(); ok && i < len(p.Zooms) {
		z := p.Zooms[i]
		fixed := z.Focus.Kind == core.FocusFixed
		editing := e.mode == focusMode{i}
		section(c, "Selected zoom", pal, func() {
			e.toggleEdit(c, "zoom-on", "Enabled", z.Enabled, pal, func(p *core.Project) { p.Zooms[i].Enabled = !p.Zooms[i].Enabled })
			e.sliderEdit(c, "zoom-scale", "Magnification", z.Scale, 1.1, 3, fmt.Sprintf("%.2f×", z.Scale), pal, func(p *core.Project, v float32) {
				if i < len(p.Zooms) {
					p.Zooms[i].Scale = float32(math.Round(float64(v)*20) / 20)
				}
			})
			ui.Row(c).Gap(4).Children(func() {
				if chip(c, "focus-pointer", "Follow pointer", !fixed, pal) {
					e.setMode(normalMode{})
					e.edit("", func(p *core.Project) { p.Zooms[i].Focus = core.ZoomFocus{} })
				}
				if chip(c, "focus-fixed", "Fixed point", fixed, pal) {
					e.enterFocus(i)
				}
			})
			if fixed {
				label := "Set focus on preview"
				if editing {
					label = "Editing focus…"
				}
				if button(c, "focus-edit", label, theme.IconZoom, editing, pal).Clicked() {
					e.enterFocus(i)
				}
			}
			if z.Auto {
				hint(c, "Made by auto zoom. Editing it keeps it when auto zoom is rebuilt.", pal)
			}
			if button(c, "zoom-del", "Delete zoom", theme.IconTrash, false, pal).Clicked() {
				e.deleteSelection()
			}
		})
	}
	section(c, "Zooms", pal, func() {
		n := len(p.Zooms)
		plural := "s"
		if n == 1 {
			plural = ""
		}
		hint(c, fmt.Sprintf("%d zoom%s on the timeline. Select one to edit it, drag it to retime.", n, plural), pal)
		ui.Row(c).Wrap().Gap(8).Children(func() {
			if button(c, "zoom-add", "Add at playhead", theme.IconPlus, false, pal).Clicked() {
				e.addZoom()
			}
			if button(c, "zoom-auto", "Rebuild auto zooms", theme.IconSparkles, false, pal).Clicked() {
				in, dur := e.session.Assets.Input(), e.session.SourceDurationMs
				e.sel = selection{}
				e.edit("", func(p *core.Project) { p.ApplyAutoZoom(in, dur) })
			}
		})
	})
}

func (e *editor) reloadPresets() {
	e.presetList = e.presets.List()
	e.defaultPreset = ""
	if p, ok := e.presets.DefaultPreset(); ok {
		e.defaultPreset = p.Name
	}
}

func (e *editor) savePreset() {
	name := strings.TrimSpace(e.presetName)
	if name == "" {
		e.notify("Type a name for the preset", false)
		e.nameInput.Focus()
		return
	}
	preset := core.NewPreset(name, e.project().Design)
	if err := e.presets.Save(preset); err != nil {
		e.notify("Couldn't save the preset: "+err.Error(), true)
	} else {
		e.notify("Saved preset “"+preset.Name+"”", false)
		e.presetName = ""
	}
	e.reloadPresets()
}

func (e *editor) presetResult(err error) {
	if err != nil {
		e.notify(err.Error(), true)
	}
	e.reloadPresets()
}

func (e *editor) presetsTab(c *ui.Context, pal theme.Palette) {
	section(c, "Save current look", pal, func() {
		hint(c, "Saves the background, layout, cursor and camera settings.", pal)
		if ui.TextInput(c.Key("preset-name"), &e.presetName).Placeholder("Preset name").Label("Preset name").Bind(&e.nameInput).Submitted() {
			e.savePreset()
		}
		if button(c, "preset-save", "Save preset", theme.IconSave, true, pal).Clicked() {
			e.savePreset()
		}
	})
	section(c, "Presets", pal, func() {
		if len(e.presetList) == 0 {
			hint(c, "No presets yet.", pal)
		}
		for _, preset := range e.presetList {
			name := preset.Name
			isDefault := e.defaultPreset == name
			ui.Row(c.Key("preset-"+name)).Gap(4).Padding(4, 0).Children(func() {
				ui.Text(c, name).FontSize(13).Grow(1).MinWidth(0).SingleLine()
				if isDefault {
					ui.Text(c, "Default").FontSize(12).TextColor(pal.Accent)
				}
				if button(c, "apply", "Apply", noIcon, false, pal).Clicked() {
					if p, err := e.presets.Load(name); err == nil {
						e.edit("", func(pr *core.Project) { p.Apply(pr) })
					}
				}
				tip := "Use for new recordings"
				if isDefault {
					tip = "Stop using as default"
				}
				if iconButton(c, "default", theme.IconCheck, isDefault, true, tip, pal) {
					next := name
					if isDefault {
						next = ""
					}
					e.presetResult(e.presets.SetDefault(next))
				}
				if iconButton(c, "delete", theme.IconTrash, false, true, "Delete preset", pal) {
					e.presetResult(e.presets.Delete(name))
				}
			})
		}
		hint(c, "The default preset is applied to recordings the first time they open in Studio.", pal)
	})
}
