use super::{Mode, Selection, Studio, Tab, TABS};
use crate::style::{Background, Color, Gradient, GRADIENTS, SOLIDS};
use crate::ui::{self, OnValue, ToRgba};
use crate::win;
use glint_studio::{AspectMode, AspectPreset, CropRect, KeystrokePosition, Project, ZoomFocus};
use gpui::prelude::*;
use gpui::{div, px, Context, ElementId, PathPromptOptions, SharedString, WeakEntity};
use std::rc::Rc;

pub(super) fn eid(prefix: &str, i: usize) -> ElementId {
    ElementId::Name(format!("{prefix}-{i}").into())
}

fn value(weak: &WeakEntity<Studio>, group: &'static str, f: impl Fn(&mut Project, f32) + 'static) -> OnValue {
    let weak = weak.clone();
    Rc::new(move |v, _, cx| {
        let _ = weak.update(cx, |s, cx| s.edit(Some(group), cx, |p| f(p, v)));
    })
}

pub(super) fn hint(text: impl Into<SharedString>) -> gpui::Div {
    div().text_xs().text_color(ui::theme().muted).child(text.into())
}

fn percent(v: f32) -> String {
    format!("{:.0}%", v * 100.0)
}

#[derive(Clone, Copy, PartialEq)]
enum BgMode {
    Gradient,
    Solid,
    Blur,
    Image,
    None,
}

impl Studio {
    pub(super) fn sidebar(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let tabs = div().flex().flex_wrap().gap_0p5().children(TABS.iter().map(|(tab, icon, title)| {
            let tab = *tab;
            ui::icon_button(SharedString::from(format!("tab-{title}")), icon, self.tab == tab, *title, cx.listener(move |s, _, _, cx| {
                s.tab = tab;
                cx.notify();
            }))
        }));
        let title = TABS.iter().find(|(t, ..)| *t == self.tab).map_or("", |(_, _, t)| *t);
        let body = match self.tab {
            Tab::Background => self.background_tab(cx),
            Tab::Layout => self.layout_tab(cx),
            Tab::Cursor => self.cursor_tab(cx),
            Tab::Keystrokes => self.keystrokes_tab(cx),
            Tab::Camera => self.camera_tab(cx),
            Tab::Audio => self.audio_tab(cx),
            Tab::Zoom => self.zoom_tab(cx),
            Tab::Captions => self.captions_tab(cx),
            Tab::Presets => self.presets_tab(cx),
        };
        div()
            .id("sidebar")
            .w(px(312.))
            .flex_none()
            .h_full()
            .flex()
            .flex_col()
            .bg(ui::theme().layer)
            .border_l_1()
            .border_color(ui::theme().border)
            .child(div().p_2().border_b_1().border_color(ui::theme().border).child(tabs))
            .child(
                div()
                    .id("sidebar-body")
                    .flex_1()
                    .min_h_0()
                    .overflow_y_scroll()
                    .p_4()
                    .flex()
                    .flex_col()
                    .gap_3()
                    .text_color(ui::theme().text)
                    .child(div().text_base().font_weight(gpui::FontWeight::SEMIBOLD).child(title))
                    .child(body),
            )
    }

    fn background_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let weak = cx.entity().downgrade();
        let d = &self.project().design;
        let mode = match d.background {
            Background::Gradient(_) => BgMode::Gradient,
            Background::Solid(_) => BgMode::Solid,
            Background::Blurred => BgMode::Blur,
            Background::Image(_) => BgMode::Image,
            Background::None => BgMode::None,
        };
        let set_bg = |bg: Background| cx.listener(move |s: &mut Studio, _, _, cx| s.edit(None, cx, |p| p.design.background = bg.clone()));
        let mode_chip = |id: &'static str, label: &'static str, m: BgMode, bg: Background| ui::chip(id, label, mode == m, set_bg(bg));
        let wallpaper = win::desktop_wallpaper();
        let mut section = ui::section("Background").child(
            div()
                .flex()
                .flex_wrap()
                .gap_1()
                .child(mode_chip("bg-g", "Gradient", BgMode::Gradient, Background::Gradient(GRADIENTS[0].1.clone())))
                .child(mode_chip("bg-s", "Solid", BgMode::Solid, Background::Solid(Color::rgb(0x27272A))))
                .child(mode_chip("bg-b", "Blur", BgMode::Blur, Background::Blurred))
                .child(mode_chip(
                    "bg-i",
                    "Image",
                    BgMode::Image,
                    wallpaper.clone().map(Background::Image).unwrap_or(Background::Gradient(GRADIENTS[1].1.clone())),
                ))
                .child(mode_chip("bg-n", "None", BgMode::None, Background::None)),
        );
        match &d.background {
            Background::Gradient(g) => {
                let angle = g.angle;
                let stops = g.stops.clone();
                section = section
                    .child(div().flex().flex_wrap().gap_1().children(GRADIENTS.iter().enumerate().map(|(i, (_, preset))| {
                        let bg = Background::Gradient(Gradient { stops: preset.stops.clone(), angle });
                        ui::swatch(eid("grad", i), &preset.stops.iter().map(|c| c.rgba()).collect::<Vec<_>>(), preset.stops == stops, 34., set_bg(bg))
                    })))
                    .child(ui::slider("angle", "Angle", angle, 0., 360., format!("{angle:.0}\u{00b0}"), value(&weak, "angle", |p, v| {
                        if let Background::Gradient(g) = &mut p.design.background {
                            g.angle = (v / 15.0).round() * 15.0;
                        }
                    })));
            }
            Background::Solid(c) => {
                let current = *c;
                let w2 = weak.clone();
                section = section
                    .child(div().flex().flex_wrap().gap_1().children(SOLIDS.iter().enumerate().map(|(i, hex)| {
                        let col = Color::rgb(*hex);
                        ui::swatch(eid("solid", i), &[col.rgba()], current == col, 30., set_bg(Background::Solid(col)))
                    })))
                    .child(ui::slider("hue", "Custom hue", self.hue, 0., 360., current.hex(), Rc::new(move |v, _, cx| {
                        let _ = w2.update(cx, |s, cx| {
                            s.hue = v;
                            s.edit(Some("hue"), cx, |p| p.design.background = Background::Solid(Color::from_hsl(v, 0.7, 0.62)));
                        });
                    })));
            }
            Background::Blurred => section = section.child(hint("A soft, blurred copy of the recording behind it.")),
            Background::Image(path) => {
                let name = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
                let w1 = weak.clone();
                section = section.child(hint(name).truncate()).child(
                    div()
                        .flex()
                        .gap_2()
                        .child(ui::button("bg-pick", "Choose image\u{2026}", Some("image"), false, move |_, _, cx| {
                            let rx = cx.prompt_for_paths(PathPromptOptions { files: true, directories: false, multiple: false, prompt: Some("Choose background".into()) });
                            let w = w1.clone();
                            cx.spawn(async move |cx| {
                                if let Ok(Ok(Some(paths))) = rx.await {
                                    if let Some(p) = paths.into_iter().next() {
                                        let _ = w.update(cx, |s, cx| s.edit(None, cx, |pr| pr.design.background = Background::Image(p)));
                                    }
                                }
                            })
                            .detach();
                        }))
                        .when_some(wallpaper, |d, w| d.child(ui::button("bg-wall", "Wallpaper", Some("monitor"), false, set_bg(Background::Image(w))))),
                );
            }
            Background::None => section = section.child(hint("No background: the card fills the frame.")),
        }
        let d = &self.project().design;
        div().flex().flex_col().child(section).child(
            ui::section("Frame")
                .child(ui::slider("pad", "Padding", d.padding, 0., 0.2, percent(d.padding), value(&weak, "pad", |p, v| p.design.padding = v)))
                .child(ui::slider("radius", "Corner radius", d.corner_radius, 0., 0.08, percent(d.corner_radius), value(&weak, "radius", |p, v| p.design.corner_radius = v)))
                .child(ui::slider("shadow", "Shadow", d.shadow, 0., 1., percent(d.shadow), value(&weak, "shadow", |p, v| p.design.shadow = v))),
        )
    }

    fn layout_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let p = self.project();
        let (aspect, mode, crop) = (p.aspect, p.aspect_mode, p.crop);
        let out = p.output_size(self.session.source_size);
        let (sw, sh) = self.session.source_size;
        let cropping = matches!(self.mode, Mode::Crop { .. });
        div()
            .flex()
            .flex_col()
            .child(
                ui::section("Aspect ratio")
                    .child(div().flex().flex_wrap().gap_1().children(AspectPreset::ALL.iter().enumerate().map(|(i, a)| {
                        let a = *a;
                        ui::chip(eid("aspect", i), a.label(), aspect == a, cx.listener(move |s, _, _, cx| s.edit(None, cx, |p| p.aspect = a)))
                    })))
                    .when(aspect != AspectPreset::Original, |d| {
                        d.child(
                            div()
                                .flex()
                                .gap_1()
                                .child(ui::chip("fill", "Fill", mode == AspectMode::Fill, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.aspect_mode = AspectMode::Fill))))
                                .child(ui::chip("fit", "Fit", mode == AspectMode::Fit, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.aspect_mode = AspectMode::Fit)))),
                        )
                        .child(hint(if mode == AspectMode::Fill {
                            "Fill crops into the recording and follows the pointer."
                        } else {
                            "Fit keeps the whole recording visible on the background."
                        }))
                    })
                    .child(hint(format!("Export size {} \u{00d7} {}", out.0, out.1))),
            )
            .child(
                ui::section("Crop")
                    .child(hint(format!(
                        "Showing {} \u{00d7} {} of {sw} \u{00d7} {sh}",
                        (crop.w * sw as f32).round(),
                        (crop.h * sh as f32).round()
                    )))
                    .child(
                        div()
                            .flex()
                            .gap_2()
                            .child(ui::button("crop", if cropping { "Cropping\u{2026}" } else { "Crop video" }, Some("crop"), cropping, cx.listener(|s, _, _, cx| s.enter_crop(cx))))
                            .when(crop != CropRect::FULL, |d| {
                                d.child(ui::button("crop-clear", "Reset", None, false, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.crop = CropRect::FULL))))
                            }),
                    ),
            )
    }

    fn cursor_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let weak = cx.entity().downgrade();
        let p = self.project();
        let c = p.design.cursor;
        let clicks = p.click_highlights;
        div().flex().flex_col().child(
            ui::section("Cursor")
                .child(ui::toggle("cursor-show", "Show cursor", c.visible, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.design.cursor.visible = !p.design.cursor.visible))))
                .child(ui::slider("cursor-size", "Size", c.size, 0.5, 4., format!("{:.1}\u{00d7}", c.size), value(&weak, "cursor-size", |p, v| p.design.cursor.size = (v * 10.0).round() / 10.0)))
                .child(ui::slider("cursor-smooth", "Smoothing", c.smoothing, 0., 1., percent(c.smoothing), value(&weak, "cursor-smooth", |p, v| p.design.cursor.smoothing = v)))
                .child(ui::toggle("clicks", "Click highlights", clicks, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.click_highlights = !p.click_highlights)))),
        )
    }

    fn keystrokes_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let k = self.project().keystrokes;
        let label = |p: KeystrokePosition| match p {
            KeystrokePosition::TopLeft => "Top left",
            KeystrokePosition::TopCenter => "Top",
            KeystrokePosition::TopRight => "Top right",
            KeystrokePosition::BottomLeft => "Bottom left",
            KeystrokePosition::BottomCenter => "Bottom",
            KeystrokePosition::BottomRight => "Bottom right",
        };
        div().flex().flex_col().child(
            ui::section("Keystrokes")
                .child(ui::toggle("keys", "Show keystrokes", k.enabled, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.keystrokes.enabled = !p.keystrokes.enabled))))
                .child(hint("Shortcuts pressed while recording appear as captions."))
                .child(div().grid().grid_cols(3).gap_1().children(KeystrokePosition::ALL.iter().enumerate().map(|(i, pos)| {
                    let pos = *pos;
                    ui::chip(eid("keypos", i), label(pos), k.position == pos, cx.listener(move |s, _, _, cx| s.edit(None, cx, |p| p.keystrokes.position = pos)))
                }))),
        )
    }

    fn camera_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        if self.session.tracks.camera.is_none() {
            return div().child(hint("This recording has no camera track."));
        }
        let weak = cx.entity().downgrade();
        let c = self.project().design.camera;
        div().flex().flex_col().child(
            ui::section("Camera")
                .child(ui::toggle("cam-show", "Show camera", c.visible, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.design.camera.visible = !p.design.camera.visible))))
                .child(ui::slider("cam-size", "Size", c.size, 0.08, 0.6, percent(c.size), value(&weak, "cam-size", |p, v| p.design.camera.size = v)))
                .child(ui::slider("cam-round", "Roundness", c.roundness, 0., 0.5, percent(c.roundness * 2.0), value(&weak, "cam-round", |p, v| p.design.camera.roundness = v)))
                .child(hint("Drag the camera on the preview to move it. Scroll over it or drag its handle to resize.")),
        )
    }

    fn audio_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let weak = cx.entity().downgrade();
        let a = self.project().audio;
        let t = &self.session.tracks;
        let track = |key: &'static str, title: &'static str, present: bool, enabled: bool, volume: f32, mic: bool| {
            let mut s = ui::section(title);
            if !present {
                return s.child(hint("Not recorded."));
            }
            s = s.child(ui::toggle(SharedString::from(format!("{key}-on")), "Include", enabled, cx.listener(move |s, _, _, cx| {
                s.edit(None, cx, |p| {
                    let t = if mic { &mut p.audio.mic } else { &mut p.audio.system };
                    t.enabled = !t.enabled;
                })
            })));
            s.child(ui::slider(key, "Volume", volume, 0., 2., percent(volume), value(&weak, key, move |p, v| {
                let t = if mic { &mut p.audio.mic } else { &mut p.audio.system };
                t.volume = v;
            })))
        };
        div()
            .flex()
            .flex_col()
            .child(track("mic-vol", "Microphone", t.mic.is_some(), a.mic.enabled, a.mic.volume, true))
            .child(track("sys-vol", "System audio", t.system.is_some(), a.system.enabled, a.system.volume, false))
    }

    fn zoom_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let weak = cx.entity().downgrade();
        let p = self.project();
        let count = p.zooms.len();
        let selected = match self.selection {
            Some(Selection::Zoom(i)) => p.zooms.get(i).map(|z| (i, z.clone())),
            _ => None,
        };
        let mut root = div().flex().flex_col();
        if let Some((i, z)) = selected {
            let fixed = matches!(z.focus, ZoomFocus::Fixed { .. });
            let editing = self.mode == Mode::Focus { zoom: i };
            root = root.child(
                ui::section("Selected zoom")
                    .child(ui::toggle("zoom-on", "Enabled", z.enabled, cx.listener(move |s, _, _, cx| s.edit(None, cx, |p| p.zooms[i].enabled = !p.zooms[i].enabled))))
                    .child(ui::slider("zoom-scale", "Magnification", z.scale, 1.1, 3., format!("{:.2}\u{00d7}", z.scale), value(&weak, "zoom-scale", move |p, v| {
                        if let Some(z) = p.zooms.get_mut(i) {
                            z.scale = (v * 20.0).round() / 20.0;
                        }
                    })))
                    .child(
                        div()
                            .flex()
                            .gap_1()
                            .child(ui::chip("focus-pointer", "Follow pointer", !fixed, cx.listener(move |s, _, _, cx| {
                                s.set_mode(Mode::Normal, cx);
                                s.edit(None, cx, |p| p.zooms[i].focus = ZoomFocus::Pointer);
                            })))
                            .child(ui::chip("focus-fixed", "Fixed point", fixed, cx.listener(move |s, _, _, cx| s.enter_focus(i, cx)))),
                    )
                    .when(fixed, |d| {
                        d.child(ui::button("focus-edit", if editing { "Editing focus\u{2026}" } else { "Set focus on preview" }, Some("zoom"), editing, cx.listener(move |s, _, _, cx| s.enter_focus(i, cx))))
                    })
                    .when(z.auto, |d| d.child(hint("Made by auto zoom. Editing it keeps it when auto zoom is rebuilt.")))
                    .child(ui::button("zoom-del", "Delete zoom", Some("trash"), false, cx.listener(|s, _, _, cx| s.delete_selection(cx)))),
            );
        }
        root.child(
            ui::section("Zooms")
                .child(hint(format!("{count} zoom{} on the timeline. Select one to edit it, drag it to retime.", if count == 1 { "" } else { "s" })))
                .child(
                    div()
                        .flex()
                        .flex_wrap()
                        .gap_2()
                        .child(ui::button("zoom-add", "Add at playhead", Some("plus"), false, cx.listener(|s, _, _, cx| s.add_zoom(cx))))
                        .child(ui::button("zoom-auto", "Rebuild auto zooms", Some("sparkles"), false, cx.listener(|s, _, _, cx| {
                            let input = s.session.assets.clone();
                            let dur = s.session.source_duration_ms;
                            s.selection = None;
                            s.edit(None, cx, |p| p.apply_auto_zoom(input.input(), dur));
                        }))),
                ),
        )
    }

    fn presets_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let editing = self.preset_name.clone();
        let rows = self.preset_list.iter().enumerate().map(|(i, preset)| {
            let name = preset.name.clone();
            let is_default = self.default_preset.as_deref() == Some(name.as_str());
            let (n1, n2, n3) = (name.clone(), name.clone(), name.clone());
            div()
                .flex()
                .items_center()
                .gap_1()
                .py_1()
                .child(div().flex_1().min_w_0().text_sm().truncate().child(name))
                .when(is_default, |d| d.child(div().text_xs().text_color(ui::accent()).child("Default")))
                .child(ui::button(eid("preset-apply", i), "Apply", None, false, cx.listener(move |s, _, _, cx| {
                    if let Ok(p) = s.presets.load(&n1) {
                        s.edit(None, cx, |pr| p.apply(pr));
                    }
                })))
                .child(ui::icon_button(eid("preset-def", i), "check", is_default, if is_default { "Stop using as default" } else { "Use for new recordings" }, cx.listener(move |s, _, _, cx| {
                    let r = s.presets.set_default(if is_default { None } else { Some(&n2) });
                    s.preset_result(r, cx);
                })))
                .child(ui::icon_button(eid("preset-del", i), "trash", false, "Delete preset", cx.listener(move |s, _, _, cx| {
                    let r = s.presets.delete(&n3);
                    s.preset_result(r, cx);
                })))
        });
        div()
            .flex()
            .flex_col()
            .child(
                ui::section("Save current look")
                    .child(hint("Saves the background, layout, cursor and camera settings."))
                    .child(
                        div()
                            .id("preset-name")
                            .cursor_text()
                            .on_click(cx.listener(|s, _, _, cx| {
                                s.preset_name.get_or_insert_with(String::new);
                                cx.notify();
                            }))
                            .child(ui::text_box(editing.as_deref().unwrap_or(""), "Preset name", editing.is_some())),
                    )
                    .child(ui::button("preset-save", "Save preset", Some("save"), true, cx.listener(|s, _, _, cx| {
                        if s.preset_name.as_deref().is_none_or(|n| n.trim().is_empty()) {
                            s.preset_name = Some(String::new());
                            s.notify("Type a name for the preset", false, cx);
                        } else {
                            s.save_preset(cx);
                        }
                        cx.notify();
                    }))),
            )
            .child(
                ui::section("Presets")
                    .when(self.preset_list.is_empty(), |d| d.child(hint("No presets yet.")))
                    .children(rows)
                    .child(hint("The default preset is applied to recordings the first time they open in Studio.")),
            )
    }
}
