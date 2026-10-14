//! Title bar, tool strip and the inspector panels.

use super::{gallery, Editor, Tab, Typing};
use crate::annotate::{RedactStyle, Shape, TextStyle, Tool};
use crate::settings::Settings;
use crate::style::{Background, Chrome, Color, Gradient, Style, ASPECTS, GRADIENTS, INK, SOLIDS};
use crate::ui::{self, OnValue, ToRgba, ACCENT, BG, BORDER, ELEV, FONT, HOVER, MUTED, PANEL, TEXT};
use glint_render::doc::{BlurEdge, BlurMode, EditDoc, Placement};
use glint_render::fonts::{self, Align, Weight};
use gpui::prelude::*;
use gpui::{
    div, img, px, rgb, rgba, App, ClickEvent, Context, ElementId, ImageSource, ObjectFit, PathPromptOptions, SharedString,
    WeakEntity, Window, WindowControlArea,
};
use std::rc::Rc;

type Listener = Box<dyn Fn(&ClickEvent, &mut Window, &mut App) + 'static>;

pub(super) fn on(weak: &WeakEntity<Editor>, f: impl Fn(&mut Editor, &mut Context<Editor>) + 'static) -> Listener {
    let weak = weak.clone();
    Box::new(move |_, _, cx| {
        let _ = weak.update(cx, |this, cx| {
            f(this, cx);
            cx.notify();
        });
    })
}

fn on_value(weak: &WeakEntity<Editor>, f: impl Fn(&mut Editor, f32) + 'static) -> OnValue {
    let weak = weak.clone();
    Rc::new(move |v, _, cx| {
        let _ = weak.update(cx, |this, cx| {
            f(this, v);
            cx.notify();
        });
    })
}

/// A slider bound to one document field; a drag is one undo step.
fn doc_slider(
    weak: &WeakEntity<Editor>,
    key: &'static str,
    label: &str,
    value: f32,
    range: (f32, f32),
    display: String,
    set: impl Fn(&mut EditDoc, f32) + 'static,
) -> impl IntoElement {
    ui::slider(key, label, value, range.0, range.1, display, on_value(weak, move |e, v| e.edit(key, |d| set(d, v))))
}

pub(super) fn eid(prefix: &str, i: usize) -> ElementId {
    ElementId::Name(format!("{prefix}-{i}").into())
}

fn row(label: &str) -> gpui::Div {
    div().flex().flex_wrap().items_center().gap_1().child(div().text_xs().w(px(64.)).text_color(rgb(MUTED)).child(label.to_string()))
}

fn note(text: &str) -> impl IntoElement {
    div().text_xs().text_color(rgb(MUTED)).child(text.to_string())
}

fn grid9(weak: &WeakEntity<Editor>, prefix: &'static str, selected: Option<u8>, set: impl Fn(&mut Editor, u8) + Clone + 'static) -> impl IntoElement {
    div().grid().grid_cols(3).gap(px(3.)).children((0..9u8).map(move |i| {
        let set = set.clone();
        let sel = selected == Some(i);
        div()
            .id(eid(prefix, i as usize))
            .size(px(16.))
            .rounded_sm()
            .cursor_pointer()
            .border_1()
            .border_color(rgb(BORDER))
            .when(sel, |d| d.bg(rgb(ACCENT)).border_color(rgb(ACCENT)))
            .when(!sel, |d| d.bg(rgb(ELEV)).hover(|s| s.bg(rgb(HOVER))))
            .on_click(on(weak, move |e, _| set(e, i)))
    }))
}

fn color_row(weak: &WeakEntity<Editor>, prefix: &'static str, current: Color, set: impl Fn(&mut Editor, Color) + Clone + 'static) -> impl IntoElement {
    let colors = [0xFFFFFF, 0x000000].into_iter().chain(INK.into_iter().take(8));
    div().flex().flex_wrap().gap_1().children(colors.enumerate().map(move |(i, hex)| {
        let col = Color::rgb(hex);
        let set = set.clone();
        ui::swatch(eid(prefix, i), &[col.rgba()], current.with_alpha(255) == col, 24., on(weak, move |e, _| set(e, col)))
    }))
}

#[derive(PartialEq, Clone, Copy)]
enum BgMode {
    Gradient,
    Solid,
    Wallpaper,
    Blur,
    None,
}

impl Editor {
    pub(super) fn titlebar(&self, weak: &WeakEntity<Editor>) -> impl IntoElement {
        let active = if self.crop.is_some() { Tool::Crop } else { self.tool };
        let tools = Tool::ALL.iter().enumerate().map(|(i, t)| {
            let t = *t;
            ui::icon_button(eid("tool", i), t.icon(), active == t, t.name(), on(weak, move |e, _| e.set_tool(t)))
        });
        let caption = |id: &'static str, icon: &'static str, area: WindowControlArea, close: bool| {
            div()
                .id(id)
                .w(px(46.))
                .h_full()
                .flex()
                .items_center()
                .justify_center()
                .window_control_area(area)
                .when(close, |d| d.hover(|s| s.bg(rgb(0xC42B1C))))
                .when(!close, |d| d.hover(|s| s.bg(rgb(HOVER))))
                .child(ui::icon(icon, 14., rgb(TEXT)))
        };
        div()
            .h(px(48.))
            .flex_none()
            .flex()
            .items_center()
            .bg(rgb(PANEL))
            .border_b_1()
            .border_color(rgb(BORDER))
            .child(
                div()
                    .id("drag-l")
                    .h_full()
                    .w(px(200.))
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap_2()
                    .pl_4()
                    .window_control_area(WindowControlArea::Drag)
                    .child(
                        div()
                            .size(px(22.))
                            .rounded_md()
                            .flex()
                            .items_center()
                            .justify_center()
                            .bg(gpui::linear_gradient(
                                135.,
                                gpui::linear_color_stop(rgb(0x6366F1), 0.),
                                gpui::linear_color_stop(rgb(0xEC4899), 1.),
                            ))
                            .child(ui::icon("area", 13., rgb(0xFFFFFF))),
                    )
                    .child(div().text_sm().font_weight(gpui::FontWeight::SEMIBOLD).truncate().child(self.title.clone())),
            )
            .child(div().id("drag-m1").h_full().flex_1().window_control_area(WindowControlArea::Drag))
            .child(div().flex().items_center().gap_0p5().p_1().rounded_lg().bg(rgb(BG)).border_1().border_color(rgb(BORDER)).children(tools))
            .child(div().id("drag-m2").h_full().flex_1().window_control_area(WindowControlArea::Drag))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_1()
                    .pr_2()
                    .child(ui::icon_button("undo", "undo", false, "Undo (Ctrl+Z)", on(weak, |e, _| e.undo())))
                    .child(ui::icon_button("redo", "redo", false, "Redo (Ctrl+Y)", on(weak, |e, _| e.redo())))
                    .child(div().w(px(1.)).h(px(20.)).mx_1().bg(rgb(BORDER)))
                    .child(ui::icon_button("pin", "pin", false, "Pin to screen (Ctrl+P)", on(weak, |e, cx| e.pin(cx))))
                    .child(ui::icon_button("saveas", "folder", false, "Save as… (Ctrl+Shift+S)", on(weak, |e, cx| e.save_as(cx))))
                    .child(ui::icon_button("copy", "copy", false, "Copy (Ctrl+C)", on(weak, |e, cx| e.copy(cx))))
                    .child(div().w(px(6.)))
                    .child(ui::button("save", "Save", Some("save"), false, {
                        let w = weak.clone();
                        move |_, window, cx| {
                            let _ = w.update(cx, |e, cx| e.save(false, window, cx));
                        }
                    }))
                    .child(ui::button("done", "Done", Some("check"), true, {
                        let w = weak.clone();
                        move |_, window, cx| {
                            let _ = w.update(cx, |e, cx| e.save(true, window, cx));
                        }
                    })),
            )
            .child(
                div()
                    .h_full()
                    .flex()
                    .child(caption("min", "min", WindowControlArea::Min, false))
                    .child(caption("max", "max", WindowControlArea::Max, false))
                    .child(caption("close", "close", WindowControlArea::Close, true)),
            )
    }

    pub(super) fn sidebar(&self, weak: &WeakEntity<Editor>) -> impl IntoElement {
        let tab = |id: &'static str, label: &'static str, t: Tab| ui::chip(id, label, self.tab == t, on(weak, move |e, _| e.tab = t));
        let body = match self.tab {
            Tab::Style => self.style_panel(weak).into_any_element(),
            Tab::Effects => self.effects_panel(weak).into_any_element(),
            Tab::Annotate => self.annotate_panel(weak).into_any_element(),
        };
        div()
            .w(px(320.))
            .flex_none()
            .h_full()
            .flex()
            .flex_col()
            .bg(rgb(PANEL))
            .border_l_1()
            .border_color(rgb(BORDER))
            .child(
                div()
                    .flex()
                    .gap_1()
                    .p_3()
                    .border_b_1()
                    .border_color(rgb(BORDER))
                    .child(tab("tab-style", "Style", Tab::Style))
                    .child(tab("tab-fx", "Effects", Tab::Effects))
                    .child(tab("tab-ann", "Annotate", Tab::Annotate)),
            )
            .child(div().id("sidebar").flex_1().min_h_0().overflow_y_scroll().child(div().p_4().flex().flex_col().text_color(rgb(TEXT)).child(body)))
    }

    fn presets_section(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let list = div().flex().flex_wrap().gap_1().children(self.presets.iter().enumerate().map(|(i, p)| {
            div()
                .flex()
                .items_center()
                .rounded_md()
                .bg(rgb(ELEV))
                .border_1()
                .border_color(rgb(BORDER))
                .child(
                    div()
                        .id(eid("preset", i))
                        .px_2()
                        .h(px(24.))
                        .flex()
                        .items_center()
                        .text_xs()
                        .cursor_pointer()
                        .hover(|s| s.bg(rgb(HOVER)))
                        .child(p.name.clone())
                        .on_click(on(weak, move |e, _| e.apply_preset(i))),
                )
                .child(
                    div()
                        .id(eid("preset-del", i))
                        .px_1()
                        .h(px(24.))
                        .flex()
                        .items_center()
                        .cursor_pointer()
                        .text_color(rgb(MUTED))
                        .hover(|s| s.text_color(rgb(0xFF6B6B)))
                        .child(ui::icon("close", 10., rgb(MUTED)))
                        .tooltip(ui::tooltip(format!("Delete “{}”", p.name)))
                        .on_click(on(weak, move |e, cx| e.delete_preset(i, cx))),
                )
        }));
        ui::section("Presets")
            .when(self.presets.is_empty(), |d| d.child(note("Save the current look to reuse it on other screenshots.")))
            .child(list)
            .child(
                div()
                    .flex()
                    .gap_1()
                    .child(ui::button("pr-save", "Save current", Some("check"), false, on(weak, |e, cx| e.add_preset(cx))))
                    .child(ui::button("pr-import", "Import…", None, false, on(weak, |e, cx| e.import_presets(cx))))
                    .child(ui::button("pr-export", "Export…", None, false, on(weak, |e, cx| e.export_presets(cx)))),
            )
    }

    fn style_panel(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let st = &self.doc.style;
        let mode = match st.background {
            Background::Gradient(_) => BgMode::Gradient,
            Background::Solid(_) => BgMode::Solid,
            Background::Blurred => BgMode::Blur,
            Background::Image(_) => BgMode::Wallpaper,
            Background::None => BgMode::None,
        };
        let auto = self.auto_colors.clone();
        let wallpaper0 = self.wallpapers.first().map(|w| w.path.clone());
        let mode_chip = |id: &'static str, label: &'static str, m: BgMode| {
            let auto = auto.clone();
            let wallpaper0 = wallpaper0.clone();
            ui::chip(id, label, mode == m, on(weak, move |e, _| {
                let bg = match m {
                    BgMode::Gradient => Background::Gradient(Gradient { stops: auto.clone(), angle: 135.0 }),
                    BgMode::Solid => Background::Solid(Color::rgb(0xF4F4F5)),
                    BgMode::Blur => Background::Blurred,
                    BgMode::Wallpaper => match &wallpaper0 {
                        Some(p) => Background::Image(p.clone()),
                        None => return,
                    },
                    BgMode::None => Background::None,
                };
                e.edit("bg", |d| d.style.background = bg);
            }))
        };
        let mut bg = ui::section("Background").child(
            div()
                .flex()
                .flex_wrap()
                .gap_1()
                .child(mode_chip("bg-g", "Gradient", BgMode::Gradient))
                .child(mode_chip("bg-s", "Solid", BgMode::Solid))
                .child(mode_chip("bg-i", "Wallpaper", BgMode::Wallpaper))
                .child(mode_chip("bg-b", "Blur", BgMode::Blur))
                .child(mode_chip("bg-n", "None", BgMode::None)),
        );
        match &st.background {
            Background::Gradient(g) => {
                let current = g.clone();
                let auto_g = Gradient { stops: auto.clone(), angle: current.angle };
                let mut grid = div().flex().flex_wrap().gap_1().child(
                    div()
                        .relative()
                        .child(ui::swatch(
                            "g-auto",
                            &auto.iter().map(|c| c.rgba()).collect::<Vec<_>>(),
                            current.stops == auto,
                            34.,
                            on(weak, move |e, _| {
                                let g = auto_g.clone();
                                e.edit("bg", |d| d.style.background = Background::Gradient(g));
                            }),
                        ))
                        .child(div().absolute().top(px(9.)).left(px(9.)).child(ui::icon("sparkles", 16., rgb(0xFFFFFF)))),
                );
                for (i, (_, preset)) in GRADIENTS.iter().enumerate() {
                    let stops = preset.stops.clone();
                    let angle = current.angle;
                    grid = grid.child(ui::swatch(
                        eid("grad", i),
                        &preset.stops.iter().map(|c| c.rgba()).collect::<Vec<_>>(),
                        preset.stops == current.stops,
                        34.,
                        on(weak, move |e, _| {
                            let g = Gradient { stops: stops.clone(), angle };
                            e.edit("bg", |d| d.style.background = Background::Gradient(g));
                        }),
                    ));
                }
                bg = bg.child(grid).child(doc_slider(weak, "angle", "Angle", current.angle, (0., 360.), format!("{:.0}°", current.angle), |d, v| {
                    if let Background::Gradient(g) = &mut d.style.background {
                        g.angle = (v / 15.0).round() * 15.0;
                    }
                }));
            }
            Background::Solid(c) => {
                let current = *c;
                let grid = div().flex().flex_wrap().gap_1().children(SOLIDS.iter().enumerate().map(|(i, hex)| {
                    let col = Color::rgb(*hex);
                    ui::swatch(eid("solid", i), &[col.rgba()], current == col, 30., on(weak, move |e, _| e.edit("bg", |d| d.style.background = Background::Solid(col))))
                }));
                bg = bg.child(grid).child(ui::slider("hue", "Custom hue", self.hue, 0., 360., current.hex(), on_value(weak, |e, v| {
                    e.hue = v;
                    e.edit("hue", |d| d.style.background = Background::Solid(Color::from_hsl(v, 0.7, 0.62)));
                })));
            }
            Background::Image(path) => {
                let current = path.clone();
                let tiles = self.wallpapers.iter().enumerate().map(|(i, w)| {
                    let p = w.path.clone();
                    let selected = w.path == current;
                    div()
                        .id(eid("wall", i))
                        .w(px(88.))
                        .h(px(55.))
                        .rounded_md()
                        .overflow_hidden()
                        .cursor_pointer()
                        .border_2()
                        .border_color(if selected { rgb(ACCENT) } else { rgba(0x00000000) })
                        .hover(|s| s.border_color(rgb(HOVER)))
                        .child(img(ImageSource::Render(w.thumb.clone())).size_full().object_fit(ObjectFit::Cover))
                        .tooltip(ui::tooltip(w.path.file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default()))
                        .on_click(on(weak, move |e, _| {
                            let p = p.clone();
                            e.edit("bg", |d| d.style.background = Background::Image(p));
                        }))
                });
                let w1 = weak.clone();
                bg = bg
                    .when(self.wallpapers.is_empty(), |d| d.child(note("Loading wallpapers…")))
                    .child(div().flex().flex_wrap().gap_1().children(tiles))
                    .child(ui::button("bg-add", "Add image…", Some("image"), false, move |_, _, cx| {
                        let rx = cx.prompt_for_paths(PathPromptOptions { files: true, directories: false, multiple: false, prompt: Some("Add background image".into()) });
                        let w = w1.clone();
                        cx.spawn(async move |cx| {
                            let Ok(Ok(Some(paths))) = rx.await else { return };
                            let Some(p) = paths.into_iter().next() else { return };
                            let added = gallery::add(&p);
                            let _ = w.update(cx, |e, cx| {
                                match added {
                                    Ok(dest) => {
                                        e.wallpapers = gallery::all();
                                        e.edit("bg", |d| d.style.background = Background::Image(dest));
                                    }
                                    Err(err) => e.show_toast(format!("{err:#}"), cx),
                                }
                                cx.notify();
                            });
                        })
                        .detach();
                    }))
                    .child(note("Your desktop wallpaper, Windows wallpapers and images you add. Nothing is downloaded."));
            }
            Background::Blurred => bg = bg.child(note("A soft, blurred copy of your screenshot; it always matches its colors.")),
            Background::None => bg = bg.child(note("Transparent background, great for docs and slides.")),
        }

        let frame = ui::section("Frame")
            .child(doc_slider(weak, "pad", "Padding", st.padding, (0., 240.), format!("{:.0}", st.padding), |d, v| d.style.padding = v.round()))
            .child(doc_slider(weak, "inset", "Inset", st.inset, (0., 120.), format!("{:.0}", st.inset), |d, v| d.style.inset = v.round()))
            .child(doc_slider(weak, "radius", "Corner radius", st.radius, (0., 48.), format!("{:.0}", st.radius), |d, v| d.style.radius = v.round()))
            .child(doc_slider(weak, "shadow", "Shadow", st.shadow, (0., 120.), format!("{:.0}", st.shadow), |d, v| d.style.shadow = v.round()))
            .child(doc_slider(weak, "shadow-o", "Shadow opacity", st.shadow_opacity, (0., 1.), format!("{:.0}%", st.shadow_opacity * 100.), |d, v| d.style.shadow_opacity = v))
            .child(ui::toggle("balance", "Auto-balance inset color", st.auto_balance, on(weak, |e, _| e.edit("balance", |d| d.style.auto_balance = !d.style.auto_balance))));

        let chrome = ui::section("Window frame")
            .child(div().flex().flex_wrap().gap_1().children(Chrome::ALL.iter().enumerate().map(|(i, c)| {
                let c = *c;
                ui::chip(eid("chrome", i), c.label(), st.chrome == c, on(weak, move |e, _| e.edit("chrome", |d| d.style.chrome = c)))
            })))
            .when(st.chrome != Chrome::None, |d| {
                d.child(ui::toggle("ctitle", "Show window title", st.chrome_title, on(weak, |e, _| e.edit("ctitle", |d| d.style.chrome_title = !d.style.chrome_title))))
            });

        let canvas = ui::section("Canvas")
            .child(div().flex().flex_wrap().gap_1().children(ASPECTS.iter().enumerate().map(|(i, (label, a))| {
                let a = *a;
                ui::chip(eid("aspect", i), *label, st.aspect == a, on(weak, move |e, _| e.edit("aspect", |d| d.style.aspect = a)))
            })))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_3()
                    .child(div().text_xs().text_color(rgb(TEXT)).child("Position"))
                    .child(grid9(weak, "align", Some(st.align), |e, i| e.edit("align", |d| d.style.align = i))),
            );

        let footer = div()
            .flex()
            .gap_2()
            .child(ui::button("def", "Save as default", Some("check"), false, on(weak, |e, cx| {
                let style = e.doc.style.clone();
                cx.update_global::<Settings, _>(|s, _| {
                    s.style = style;
                    s.save();
                });
                e.show_toast("Saved as your default style", cx);
            })))
            .child(ui::button("reset", "Reset", None, false, on(weak, |e, _| e.edit("reset", |d| {
                d.style = Style::default();
                d.effects = Default::default();
            }))));

        div().flex().flex_col().child(self.presets_section(weak)).child(bg).child(frame).child(chrome).child(canvas).child(footer)
    }

    fn effects_panel(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let fx = &self.doc.effects;

        let b = &fx.border;
        let border = ui::section("Border")
            .child(ui::toggle("border-on", "Border around screenshot", b.enabled, on(weak, |e, _| e.edit("border-on", |d| d.effects.border.enabled = !d.effects.border.enabled))))
            .when(b.enabled, |d| {
                d.child(doc_slider(weak, "border-w", "Width", b.width, (1., 40.), format!("{:.0}", b.width), |d, v| d.effects.border.width = v.round()))
                    .child(color_row(weak, "border-c", b.color, |e, c| e.edit("border-c", |d| d.effects.border.color = c)))
            });

        let wm = &fx.watermark;
        let typing = self.typing == Some(Typing::Watermark);
        let field = div()
            .id("wm-text")
            .h(px(30.))
            .px_2()
            .flex()
            .items_center()
            .rounded_md()
            .bg(rgb(BG))
            .border_1()
            .border_color(if typing { rgb(ACCENT) } else { rgb(BORDER) })
            .cursor(gpui::CursorStyle::IBeam)
            .text_sm()
            .child(if wm.text.is_empty() && !typing {
                div().text_color(rgb(MUTED)).child("Watermark text")
            } else {
                div().child(format!("{}{}", wm.text, if typing { "|" } else { "" }))
            })
            .on_click(on(weak, |e, _| {
                e.commit_text();
                e.typing = Some(Typing::Watermark);
            }));
        let placement = match wm.placement {
            Placement::Grid(i) => Some(i),
            Placement::Tiled => None,
        };
        let watermark = ui::section("Watermark")
            .child(ui::toggle("wm-on", "Text watermark", wm.enabled, on(weak, |e, _| {
                e.edit("wm-on", |d| {
                    d.effects.watermark.enabled = !d.effects.watermark.enabled;
                    if d.effects.watermark.text.is_empty() {
                        d.effects.watermark.text = format!("© {}", std::env::var("USERNAME").unwrap_or_else(|_| "Glint".into()));
                    }
                })
            })))
            .when(wm.enabled, |d| {
                d.child(field)
                    .child(self.font_picker(weak, "wm", &wm.font, |e, f| e.edit("wm-font", |d| d.effects.watermark.font = f)))
                    .child(doc_slider(weak, "wm-size", "Size", wm.size, (8., 120.), format!("{:.0}", wm.size), |d, v| d.effects.watermark.size = v.round()))
                    .child(doc_slider(weak, "wm-op", "Opacity", wm.opacity, (0.05, 1.), format!("{:.0}%", wm.opacity * 100.), |d, v| d.effects.watermark.opacity = v))
                    .child(color_row(weak, "wm-c", wm.color, |e, c| e.edit("wm-c", |d| d.effects.watermark.color = c)))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_3()
                            .child(div().text_xs().text_color(rgb(TEXT)).child("Placement"))
                            .child(grid9(weak, "wm-pos", placement, |e, i| e.edit("wm-pos", |d| d.effects.watermark.placement = Placement::Grid(i))))
                            .child(ui::chip("wm-tiled", "Tiled", placement.is_none(), on(weak, |e, _| e.edit("wm-pos", |d| d.effects.watermark.placement = Placement::Tiled)))),
                    )
                    .when(placement.is_none(), |d| {
                        d.child(doc_slider(weak, "wm-rot", "Rotation", wm.rotation, (-90., 90.), format!("{:.0}°", wm.rotation), |d, v| d.effects.watermark.rotation = v.round()))
                    })
            });

        let p = &fx.perspective;
        let perspective = ui::section("Perspective")
            .child(
                div()
                    .flex()
                    .flex_wrap()
                    .gap_1()
                    .children([("Flat", 0.0, 0.0, 0.0), ("Left", 0.0, 24.0, 0.0), ("Right", 0.0, -24.0, 0.0), ("Tilt back", 22.0, 0.0, 0.0), ("Hero", 14.0, -20.0, 4.0)].into_iter().enumerate().map(
                        |(i, (label, x, y, z))| {
                            ui::chip(eid("persp", i), label, p.rot_x == x && p.rot_y == y && p.rot_z == z, on(weak, move |e, _| {
                                e.edit("persp", |d| {
                                    d.effects.perspective.rot_x = x;
                                    d.effects.perspective.rot_y = y;
                                    d.effects.perspective.rot_z = z;
                                })
                            }))
                        },
                    )),
            )
            .child(doc_slider(weak, "p-rx", "Rotate X", p.rot_x, (-60., 60.), format!("{:.0}°", p.rot_x), |d, v| d.effects.perspective.rot_x = v.round()))
            .child(doc_slider(weak, "p-ry", "Rotate Y", p.rot_y, (-60., 60.), format!("{:.0}°", p.rot_y), |d, v| d.effects.perspective.rot_y = v.round()))
            .child(doc_slider(weak, "p-rz", "Rotate Z", p.rot_z, (-45., 45.), format!("{:.0}°", p.rot_z), |d, v| d.effects.perspective.rot_z = v.round()))
            .child(doc_slider(weak, "p-zoom", "Zoom", p.zoom, (0.4, 1.6), format!("{:.0}%", p.zoom * 100.), |d, v| d.effects.perspective.zoom = (v * 100.).round() / 100.))
            .child(doc_slider(weak, "p-px", "Pan X", p.pan_x, (-0.5, 0.5), format!("{:.0}%", p.pan_x * 100.), |d, v| d.effects.perspective.pan_x = (v * 100.).round() / 100.))
            .child(doc_slider(weak, "p-py", "Pan Y", p.pan_y, (-0.5, 0.5), format!("{:.0}%", p.pan_y * 100.), |d, v| d.effects.perspective.pan_y = (v * 100.).round() / 100.))
            .child(ui::button("p-reset", "Reset perspective", None, false, on(weak, |e, _| e.edit("p-reset", |d| d.effects.perspective = Default::default()))));

        let bl = &fx.blur;
        let blur = ui::section("Progressive blur")
            .child(ui::toggle("blur-on", "Progressive blur", bl.enabled, on(weak, |e, _| e.edit("blur-on", |d| d.effects.blur.enabled = !d.effects.blur.enabled))))
            .when(bl.enabled, |d| {
                d.child(
                    row("Shape")
                        .child(ui::chip("bm-r", "Radial", bl.mode == BlurMode::Radial, on(weak, |e, _| e.edit("bm", |d| d.effects.blur.mode = BlurMode::Radial))))
                        .child(ui::chip("bm-d", "Directional", bl.mode == BlurMode::Directional, on(weak, |e, _| e.edit("bm", |d| d.effects.blur.mode = BlurMode::Directional)))),
                )
                .child(
                    row("Applies to")
                        .child(ui::chip("be-c", "Screenshot", bl.edge == BlurEdge::Clipped, on(weak, |e, _| e.edit("be", |d| d.effects.blur.edge = BlurEdge::Clipped))))
                        .child(ui::chip("be-b", "Whole scene", bl.edge == BlurEdge::Bleed, on(weak, |e, _| e.edit("be", |d| d.effects.blur.edge = BlurEdge::Bleed)))),
                )
                .child(doc_slider(weak, "bl-s", "Strength", bl.strength, (1., 100.), format!("{:.0}", bl.strength), |d, v| d.effects.blur.strength = v.round()))
                .child(doc_slider(weak, "bl-st", "Start", bl.start, (0., 1.), format!("{:.0}%", bl.start * 100.), |d, v| d.effects.blur.start = (v * 100.).round() / 100.))
                .when(bl.mode == BlurMode::Directional, |d| {
                    d.child(doc_slider(weak, "bl-a", "Angle", bl.angle, (0., 180.), format!("{:.0}°", bl.angle), |d, v| d.effects.blur.angle = v.round()))
                })
            });

        div().flex().flex_col().child(border).child(watermark).child(perspective).child(blur)
    }

    fn font_picker(
        &self,
        weak: &WeakEntity<Editor>,
        prefix: &'static str,
        font: &fonts::TextFont,
        set: impl Fn(&mut Editor, fonts::TextFont) + Clone + 'static,
    ) -> impl IntoElement {
        let families = fonts::families().iter().enumerate().map(|(i, fam)| {
            let set = set.clone();
            let mut f = font.clone();
            f.family = fam.to_string();
            let weights = fonts::weights(fam);
            if !weights.contains(&f.weight) {
                f.weight = *weights.iter().min_by_key(|w| (**w as i32 - font.weight as i32).abs()).unwrap_or(&Weight::Regular);
            }
            div()
                .id(SharedString::from(format!("{prefix}-fam-{i}")))
                .px_2()
                .h(px(26.))
                .flex()
                .items_center()
                .rounded_md()
                .cursor_pointer()
                .text_sm()
                .font_family(SharedString::from(fam.to_string()))
                .border_1()
                .when(font.family == *fam, |d| d.bg(rgba(0x7C8CFF33)).border_color(rgb(ACCENT)))
                .when(font.family != *fam, |d| d.bg(rgb(ELEV)).border_color(rgb(BORDER)).hover(|s| s.bg(rgb(HOVER))))
                .child(fam.to_string())
                .on_click(on(weak, move |e, _| set(e, f.clone())))
        });
        let weights = fonts::weights(&font.family).into_iter().map(|w| {
            let set = set.clone();
            let mut f = font.clone();
            f.weight = w;
            ui::chip(SharedString::from(format!("{prefix}-w-{}", w.label())), w.label(), font.weight == w, on(weak, move |e, _| set(e, f.clone())))
        });
        let aligns = [(Align::Left, "Left"), (Align::Center, "Center"), (Align::Right, "Right")].map(|(a, label)| {
            let set = set.clone();
            let mut f = font.clone();
            f.align = a;
            ui::chip(SharedString::from(format!("{prefix}-a-{label}")), label, font.align == a, on(weak, move |e, _| set(e, f.clone())))
        });
        div()
            .flex()
            .flex_col()
            .gap_2()
            .child(div().id(SharedString::from(format!("{prefix}-fonts"))).max_h(px(150.)).overflow_y_scroll().child(div().flex().flex_wrap().gap_1().children(families)))
            .child(row("Weight").children(weights))
            .child(row("Align").children(aligns))
    }

    fn annotate_panel(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let sel: Vec<_> = self.selected_anns().collect();
        let sel_text = sel.iter().find_map(|a| match &a.shape {
            Shape::Text { font, style, .. } => Some((font.clone(), *style)),
            _ => None,
        });
        let sel_redact = sel.iter().find_map(|a| match &a.shape {
            Shape::Redact { strength, .. } => Some(*strength / self.doc.dpi),
            _ => None,
        });
        let show_text = sel_text.is_some() || self.tool == Tool::Text;
        let show_redact = sel_redact.is_some() || matches!(self.tool, Tool::Pixelate | Tool::Blur);
        let (font, text_style) = sel_text.unwrap_or((self.ink.font.clone(), self.ink.text_style));
        let strength = sel_redact.unwrap_or(self.ink.redact_strength);

        let ink = ui::section("Ink")
            .child(div().flex().flex_wrap().gap_1().children(INK.iter().enumerate().map(|(i, hex)| {
                let col = Color::rgb(*hex);
                ui::swatch(eid("ink", i), &[col.rgba()], self.ink.color == col, 26., on(weak, move |e, _| {
                    e.ink.color = col;
                    e.modify_selected("ink", |a| {
                        if !a.is_redaction() {
                            a.color = col
                        }
                    });
                }))
            })))
            .child(ui::slider("stroke", "Stroke width", self.ink.stroke, 1., 24., format!("{:.0}", self.ink.stroke), on_value(weak, |e, v| {
                e.ink.stroke = v.round();
                let w = e.ink.stroke * e.doc.dpi;
                e.modify_selected("stroke", |a| a.width = w);
            })))
            .child(ui::slider("tsize", "Text & marker size", self.ink.text_size, 10., 160., format!("{:.0}", self.ink.text_size), on_value(weak, |e, v| {
                e.ink.text_size = v.round();
                let s = e.ink.text_size * e.doc.dpi;
                e.modify_selected("tsize", |a| match &mut a.shape {
                    Shape::Text { size, .. } => *size = s,
                    Shape::Counter { size, .. } => *size = s * 0.5,
                    _ => {}
                });
            })));

        let text = ui::section("Text")
            .child(self.font_picker(weak, "tx", &font, |e, f| {
                e.ink.font = f.clone();
                e.modify_selected("tx-font", |a| {
                    if let Shape::Text { font, .. } = &mut a.shape {
                        *font = f.clone();
                    }
                });
            }))
            .child(row("Style").children([(TextStyle::Plain, "Plain"), (TextStyle::Outline, "Outline"), (TextStyle::Pill, "Label")].map(|(ts, label)| {
                ui::chip(SharedString::from(format!("ts-{label}")), label, text_style == ts, on(weak, move |e, _| {
                    e.ink.text_style = ts;
                    e.modify_selected("ts", |a| {
                        if let Shape::Text { style, .. } = &mut a.shape {
                            *style = ts;
                        }
                    });
                }))
            })));

        let redaction = ui::section("Redaction")
            .child(ui::slider("strength", "Strength", strength, 2., 48., format!("{strength:.0}"), on_value(weak, |e, v| {
                e.ink.redact_strength = v.round();
                let s = e.ink.redact_strength * e.doc.dpi;
                e.modify_selected("strength", |a| {
                    if let Shape::Redact { strength, .. } = &mut a.shape {
                        *strength = s;
                    }
                });
            })))
            .when(sel_redact.is_some(), |d| {
                d.child(row("Kind").children(RedactStyle::ALL.map(|k| {
                    ui::chip(SharedString::from(format!("rk-{}", k.label())), k.label(), sel.iter().any(|a| matches!(a.shape, Shape::Redact { kind, .. } if kind == k)), on(weak, move |e, _| {
                        e.modify_selected("rk", |a| {
                            if let Shape::Redact { kind, .. } = &mut a.shape {
                                *kind = k;
                            }
                        });
                    }))
                })))
            });

        let privacy = ui::section("Privacy")
            .child(ui::button("p-redact", "Smart redaction", Some("shield"), false, on(weak, |e, cx| e.auto_redact(cx))).w_full())
            .child(ui::button("p-ocr", "Copy text from image", Some("scan"), false, on(weak, |e, cx| e.copy_text(cx))).w_full())
            .child(note("Finds emails, phone numbers, card numbers, keys and tokens with on-device OCR, then blurs them. Strength stays editable."));

        let selection = (!sel.is_empty()).then(|| {
            ui::section("Selection")
                .child(note(&format!("{} selected. Shift-click or drag a box to select more; drag to move them together.", sel.len())))
                .child(ui::button("del", "Delete selected", Some("trash"), false, on(weak, |e, _| e.delete_selected())))
        });

        div()
            .flex()
            .flex_col()
            .children(selection)
            .child(ink)
            .when(show_text, |d| d.child(text))
            .when(show_redact, |d| d.child(redaction))
            .child(privacy)
    }
}

impl Render for Editor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.ensure_preview(window, cx);
        let weak = cx.entity().downgrade();
        div()
            .id("editor")
            .size_full()
            .flex()
            .flex_col()
            .bg(rgb(BG))
            .font_family(FONT)
            .text_color(rgb(TEXT))
            .child(self.titlebar(&weak))
            // Focus lives below the title bar: a focusable ancestor would swallow the mouse-down
            // on the native min/max/close buttons.
            .child(
                div()
                    .id("editor-body")
                    .track_focus(&self.focus)
                    .on_key_down(cx.listener(Self::on_key))
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .child(self.canvas_area(cx))
                    .child(self.sidebar(&weak)),
            )
            .child(self.status_bar(&weak))
    }
}
