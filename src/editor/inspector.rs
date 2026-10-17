use super::{gallery, AfterSave, Editor, Typing};
use crate::annotate::{RedactStyle, Shape, TextStyle, Tool};
use crate::settings::Settings;
use crate::style::{Background, Chrome, Color, Gradient, Style, ASPECTS, GRADIENTS, INK, SOLIDS};
use crate::ui::{self, theme, OnValue, ToRgba};
use glint_render::doc::{BlurEdge, BlurMode, EditDoc, Placement};
use glint_render::fonts::{self, Align, Weight};
use gpui::prelude::*;
use gpui::{
    deferred, div, img, px, rgba, App, ClickEvent, Context, ElementId, ImageSource, ObjectFit, PathPromptOptions, SharedString,
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

fn on_window(weak: &WeakEntity<Editor>, f: impl Fn(&mut Editor, &mut Window, &mut Context<Editor>) + 'static) -> Listener {
    let weak = weak.clone();
    Box::new(move |_, window, cx| {
        let _ = weak.update(cx, |this, cx| {
            f(this, window, cx);
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
    div().flex().flex_wrap().items_center().gap_1().child(div().text_xs().w(px(64.)).text_color(theme().muted).child(label.to_string()))
}

fn note(text: &str) -> impl IntoElement {
    div().text_xs().text_color(theme().muted).child(text.to_string())
}

fn divider() -> impl IntoElement {
    div().w(px(1.)).h(px(20.)).mx_1().flex_none().bg(theme().border)
}

fn label(text: &str) -> impl IntoElement {
    div().text_xs().flex_none().text_color(theme().muted).child(text.to_string())
}

fn grid9(weak: &WeakEntity<Editor>, prefix: &'static str, selected: Option<u8>, set: impl Fn(&mut Editor, u8) + Clone + 'static) -> impl IntoElement {
    let t = theme();
    div().grid().grid_cols(3).gap(px(3.)).children((0..9u8).map(move |i| {
        let set = set.clone();
        let sel = selected == Some(i);
        div()
            .id(eid(prefix, i as usize))
            .size(px(16.))
            .rounded(px(3.))
            .cursor_pointer()
            .border_1()
            .border_color(t.border)
            .when(sel, |d| d.bg(t.accent).border_color(t.accent))
            .when(!sel, |d| d.bg(t.control).hover(|s| s.bg(t.control_hover)))
            .on_click(on(weak, move |e, _| set(e, i)))
    }))
}

fn color_row(weak: &WeakEntity<Editor>, prefix: &'static str, current: Color, size: f32, set: impl Fn(&mut Editor, Color) + Clone + 'static) -> gpui::Div {
    let colors = INK.into_iter();
    div().flex().flex_wrap().gap(px(2.)).children(colors.enumerate().map(move |(i, hex)| {
        let col = Color::rgb(hex);
        let set = set.clone();
        ui::swatch(eid(prefix, i), &[col.rgba()], current.with_alpha(255) == col, size, on(weak, move |e, _| set(e, col)))
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

#[derive(Default)]
struct Context_ {
    color: bool,
    stroke: bool,
    fill: Option<bool>,
    text: bool,
    size: bool,
    redact: bool,
}

impl Editor {
    fn style_context(&self) -> Context_ {
        let mut cx = Context_::default();
        let mut add = |shape: &Shape| match shape {
            Shape::Arrow { .. } | Shape::Line { .. } | Shape::Pen { .. } => {
                cx.color = true;
                cx.stroke = true;
            }
            Shape::Rect { filled, .. } | Shape::Ellipse { filled, .. } => {
                cx.color = true;
                cx.stroke |= !filled;
                cx.fill = Some(*filled);
            }
            Shape::Text { .. } => {
                cx.color = true;
                cx.text = true;
                cx.size = true;
            }
            Shape::Counter { .. } => {
                cx.color = true;
                cx.size = true;
            }
            Shape::Redact { .. } => cx.redact = true,
            Shape::Highlight { .. } => {}
        };
        if self.selected.is_empty() {
            let probe = match self.tool {
                Tool::Arrow | Tool::Line | Tool::Pen => Some(Shape::Pen { points: vec![] }),
                Tool::Rect | Tool::Ellipse => Some(Shape::Rect { rect: Default::default(), filled: false }),
                Tool::FilledRect => Some(Shape::Rect { rect: Default::default(), filled: true }),
                Tool::Text => Some(Shape::Text { pos: Default::default(), text: String::new(), size: 0.0, style: TextStyle::Plain, font: Default::default() }),
                Tool::Counter => Some(Shape::Counter { center: Default::default(), n: 0, size: 0.0 }),
                Tool::Pixelate | Tool::Blur => Some(Shape::Redact { rect: Default::default(), kind: RedactStyle::Blur, strength: 0.0 }),
                _ => None,
            };
            if let Some(s) = probe {
                add(&s);
            }
        } else {
            for a in self.selected_anns() {
                add(&a.shape);
            }
        }
        cx
    }

    pub(super) fn toolbar(&self, weak: &WeakEntity<Editor>) -> impl IntoElement {
        let t = theme();
        let active = if self.crop.is_some() { Tool::Crop } else { self.tool };
        let tools = Tool::ALL.iter().enumerate().map(|(i, tool)| {
            let tool = *tool;
            let gap = matches!(tool, Tool::Rect | Tool::Counter | Tool::Highlight | Tool::Crop);
            div()
                .flex()
                .items_center()
                .when(gap, |d| d.child(divider()))
                .child(ui::icon_button(eid("tool", i), tool.icon(), active == tool, tool.name(), on(weak, move |e, _| e.set_tool(tool))))
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
                .when(close, |d| d.hover(|s| s.bg(gpui::rgb(0xC42B1C))))
                .when(!close, |d| d.hover(|s| s.bg(t.control_hover)))
                .child(ui::icon(icon, 13., t.text))
        };
        div()
            .h(px(48.))
            .flex_none()
            .flex()
            .items_center()
            .child(
                div()
                    .id("drag-l")
                    .h_full()
                    .w(px(220.))
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap_2()
                    .pl_4()
                    .window_control_area(WindowControlArea::Drag)
                    .child(
                        div()
                            .size(px(20.))
                            .rounded(px(5.))
                            .flex()
                            .items_center()
                            .justify_center()
                            .bg(t.accent)
                            .child(ui::icon("area", 12., t.on_accent)),
                    )
                    .child(div().text_sm().truncate().text_color(t.text).child(self.title.clone())),
            )
            .child(div().id("drag-m1").h_full().flex_1().window_control_area(WindowControlArea::Drag))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_0p5()
                    .p_1()
                    .rounded(px(8.))
                    .bg(t.card)
                    .border_1()
                    .border_color(t.border)
                    .children(tools),
            )
            .child(div().id("drag-m2").h_full().flex_1().window_control_area(WindowControlArea::Drag))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_0p5()
                    .pr_2()
                    .child(ui::icon_button("undo", "undo", false, "Undo (Ctrl+Z)", on(weak, |e, _| e.undo())))
                    .child(ui::icon_button("redo", "redo", false, "Redo (Ctrl+Y)", on(weak, |e, _| e.redo())))
                    .child(divider())
                    .child(ui::icon_button("redact", "shield", false, "Smart redaction (Ctrl+Shift+R)", on(weak, |e, cx| e.auto_redact(cx))))
                    .child(ui::icon_button("ocr", "scan", false, "Copy text from image (Ctrl+Shift+T)", on(weak, |e, cx| e.copy_text(cx)))),
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

    pub(super) fn style_bar(&self, weak: &WeakEntity<Editor>) -> impl IntoElement {
        let t = theme();
        let mut bar = div()
            .h(px(44.))
            .flex_none()
            .flex()
            .items_center()
            .gap_2()
            .px_4()
            .bg(t.layer)
            .border_t_1()
            .border_b_1()
            .border_color(t.border)
            .text_color(t.text);
        if let Some(crop) = self.crop {
            return bar
                .child(label(&format!("Crop {} × {}", crop.rect.w as i32, crop.rect.h as i32)))
                .child(divider())
                .children(super::CROP_PRESETS.iter().enumerate().map(|(i, (name, preset))| {
                    let preset = *preset;
                    ui::chip(eid("crop-p", i), *name, crop.preset == preset, on(weak, move |e, _| e.set_crop_preset(preset)))
                }))
                .child(div().flex_1())
                .child(ui::button("crop-cancel", "Cancel", None, false, on(weak, |e, _| e.set_tool(Tool::Select))))
                .child(ui::button("crop-apply", "Apply crop", Some("check"), true, on(weak, |e, _| e.apply_crop())));
        }
        let ctx = self.style_context();
        let sel_text = self.selected_anns().find_map(|a| match &a.shape {
            Shape::Text { font, style, .. } => Some((font.clone(), *style)),
            _ => None,
        });
        let sel_redact = self.selected_anns().find_map(|a| match &a.shape {
            Shape::Redact { strength, kind, .. } => Some((*strength / self.doc.dpi, *kind)),
            _ => None,
        });
        let (font, text_style) = sel_text.unwrap_or((self.ink.font.clone(), self.ink.text_style));
        let strength = sel_redact.map_or(self.ink.redact_strength, |r| r.0);
        let empty = !(ctx.color || ctx.redact);
        if empty {
            bar = bar.child(label(match self.tool {
                Tool::Highlight => "Drag over what matters; everything else dims",
                _ if self.selected.is_empty() => "Click an annotation to select it. Shift-click or drag a box to select several",
                _ => "Highlight keeps the selected area bright",
            }));
        }
        if ctx.color {
            bar = bar.child(color_row(weak, "ink", self.ink.color, 22., |e, col| {
                e.ink.color = col;
                e.modify_selected("ink", |a| {
                    if !a.is_redaction() {
                        a.color = col
                    }
                });
            }));
        }
        if ctx.stroke {
            bar = bar.child(divider()).child(label("Width")).child(
                div().w(px(110.)).child(ui::slider_track("stroke", self.ink.stroke, 1., 24., on_value(weak, |e, v| {
                    e.ink.stroke = v.round();
                    let w = e.ink.stroke * e.doc.dpi;
                    e.modify_selected("stroke", |a| a.width = w);
                }))),
            ).child(label(&format!("{:.0}", self.ink.stroke)));
        }
        if let Some(filled) = ctx.fill {
            bar = bar.child(divider()).child(ui::chip("fill", "Fill", filled, on(weak, move |e, _| {
                let f = !filled;
                if e.selected.is_empty() {
                    e.tool = if f { Tool::FilledRect } else { Tool::Rect };
                }
                e.modify_selected("fill", |a| match &mut a.shape {
                    Shape::Rect { filled, .. } | Shape::Ellipse { filled, .. } => *filled = f,
                    _ => {}
                });
            })));
        }
        if ctx.text {
            let set_font = |f: fonts::TextFont| {
                move |e: &mut Editor, _: &mut Context<Editor>| {
                    e.ink.font = f.clone();
                    e.font_menu = false;
                    let f = f.clone();
                    e.modify_selected("tx-font", move |a| {
                        if let Shape::Text { font, .. } = &mut a.shape {
                            *font = f.clone();
                        }
                    });
                }
            };
            let menu = self.font_menu.then(|| {
                deferred(
                    div()
                        .id("font-menu")
                        .absolute()
                        .top(px(34.))
                        .left_0()
                        .w(px(220.))
                        .max_h(px(360.))
                        .overflow_y_scroll()
                        .p_1()
                        .rounded(px(8.))
                        .bg(gpui::rgba(if t.dark { 0x2C2C2CFF } else { 0xF9F9F9FF }))
                        .border_1()
                        .border_color(t.border)
                        .shadow_lg()
                        .occlude()
                        .children(fonts::families().iter().enumerate().map(|(i, fam)| {
                            let mut f = font.clone();
                            f.family = fam.to_string();
                            let weights = fonts::weights(fam);
                            if !weights.contains(&f.weight) {
                                f.weight = *weights.iter().min_by_key(|w| (**w as i32 - font.weight as i32).abs()).unwrap_or(&Weight::Regular);
                            }
                            let selected = font.family == *fam;
                            div()
                                .id(eid("fam", i))
                                .px_2()
                                .h(px(30.))
                                .flex()
                                .items_center()
                                .rounded(px(4.))
                                .cursor_pointer()
                                .text_sm()
                                .font_family(SharedString::from(fam.to_string()))
                                .when(selected, |d| d.bg(t.control_hover))
                                .hover(|s| s.bg(t.control_hover))
                                .child(fam.to_string())
                                .on_click(on(weak, set_font(f)))
                        })),
                )
            });
            let font_button = div()
                .relative()
                .child(
                    div()
                        .id("font-btn")
                        .h(px(28.))
                        .w(px(150.))
                        .px_2()
                        .flex()
                        .items_center()
                        .justify_between()
                        .rounded(px(4.))
                        .bg(t.control)
                        .border_1()
                        .border_color(t.border)
                        .cursor_pointer()
                        .hover(|s| s.bg(t.control_hover))
                        .text_sm()
                        .child(div().truncate().font_family(SharedString::from(font.family.clone())).child(font.family.clone()))
                        .child(div().text_xs().text_color(t.muted).child("▾"))
                        .on_click(on(weak, |e, _| e.font_menu = !e.font_menu)),
                )
                .children(menu);
            let weights = fonts::weights(&font.family).into_iter().map(|w| {
                let mut f = font.clone();
                f.weight = w;
                ui::chip(SharedString::from(format!("w-{}", w.label())), w.label(), font.weight == w, on(weak, set_font(f)))
            });
            let aligns = [(Align::Left, "align-left"), (Align::Center, "align-center"), (Align::Right, "align-right")].map(|(a, icon)| {
                let mut f = font.clone();
                f.align = a;
                ui::icon_button(SharedString::from(icon), icon, font.align == a, format!("Align {:?}", a).to_lowercase(), on(weak, set_font(f)))
            });
            let styles = [(TextStyle::Plain, "Plain"), (TextStyle::Outline, "Outline"), (TextStyle::Pill, "Label")].map(|(ts, name)| {
                ui::chip(SharedString::from(format!("ts-{name}")), name, text_style == ts, on(weak, move |e, _| {
                    e.ink.text_style = ts;
                    e.modify_selected("ts", |a| {
                        if let Shape::Text { style, .. } = &mut a.shape {
                            *style = ts;
                        }
                    });
                }))
            });
            bar = bar.child(divider()).child(font_button).children(weights).child(divider()).children(aligns).child(divider()).children(styles);
        }
        if ctx.size {
            bar = bar.child(divider()).child(label("Size")).child(
                div().w(px(100.)).child(ui::slider_track("tsize", self.ink.text_size, 10., 160., on_value(weak, |e, v| {
                    e.ink.text_size = v.round();
                    let s = e.ink.text_size * e.doc.dpi;
                    e.modify_selected("tsize", |a| match &mut a.shape {
                        Shape::Text { size, .. } => *size = s,
                        Shape::Counter { size, .. } => *size = s * 0.5,
                        _ => {}
                    });
                }))),
            ).child(label(&format!("{:.0}", self.ink.text_size)));
        }
        if ctx.redact {
            if let Some((_, kind)) = sel_redact {
                bar = bar.children(RedactStyle::ALL.map(|k| {
                    ui::chip(SharedString::from(format!("rk-{}", k.label())), k.label(), kind == k, on(weak, move |e, _| {
                        e.modify_selected("rk", |a| {
                            if let Shape::Redact { kind, .. } = &mut a.shape {
                                *kind = k;
                            }
                        });
                    }))
                })).child(divider());
            }
            bar = bar.child(label("Strength")).child(
                div().w(px(140.)).child(ui::slider_track("strength", strength, 2., 48., on_value(weak, |e, v| {
                    e.ink.redact_strength = v.round();
                    let s = e.ink.redact_strength * e.doc.dpi;
                    e.modify_selected("strength", |a| {
                        if let Shape::Redact { strength, .. } = &mut a.shape {
                            *strength = s;
                        }
                    });
                }))),
            ).child(label(&format!("{strength:.0}")));
        }
        bar.child(div().flex_1()).when(!self.selected.is_empty(), |d| {
            let n = self.selected.len();
            d.child(label(&format!("{n} selected"))).child(ui::icon_button("del", "trash", false, "Delete (Del)", on(weak, |e, _| e.delete_selected())))
        })
    }

    pub(super) fn sidebar(&self, weak: &WeakEntity<Editor>) -> impl IntoElement {
        let t = theme();
        div()
            .id("sidebar")
            .w(px(312.))
            .flex_none()
            .h_full()
            .overflow_y_scroll()
            .bg(t.layer)
            .border_l_1()
            .border_color(t.border)
            .child(
                div()
                    .p_3()
                    .flex()
                    .flex_col()
                    .text_color(t.text)
                    .child(self.presets_section(weak))
                    .child(self.background_section(weak))
                    .child(self.frame_sections(weak))
                    .child(self.effects_sections(weak))
                    .child(
                        div()
                            .flex()
                            .gap_2()
                            .pb_2()
                            .child(ui::button("def", "Save as default", None, false, on(weak, |e, cx| {
                                let style = e.doc.style.clone();
                                cx.update_global::<Settings, _>(|s, _| {
                                    s.style = style;
                                    s.save();
                                });
                                e.show_toast("Saved as your default style", cx);
                            })))
                            .child(ui::button("reset", "Reset look", None, false, on(weak, |e, _| e.edit("reset", |d| {
                                d.style = Style::default();
                                d.effects = Default::default();
                            })))),
                    ),
            )
    }

    fn presets_section(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let t = theme();
        let list = div().flex().flex_wrap().gap_1().children(self.presets.iter().enumerate().map(|(i, p)| {
            div()
                .flex()
                .items_center()
                .rounded(px(4.))
                .bg(t.control)
                .border_1()
                .border_color(t.border)
                .child(
                    div()
                        .id(eid("preset", i))
                        .pl_2()
                        .pr_1()
                        .h(px(26.))
                        .flex()
                        .items_center()
                        .text_xs()
                        .cursor_pointer()
                        .hover(|s| s.bg(t.control_hover))
                        .child(p.name.clone())
                        .on_click(on(weak, move |e, _| e.apply_preset(i))),
                )
                .child(
                    div()
                        .id(eid("preset-del", i))
                        .px_1()
                        .h(px(26.))
                        .flex()
                        .items_center()
                        .cursor_pointer()
                        .hover(|s| s.bg(t.control_hover))
                        .child(ui::icon("close", 10., t.muted))
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
                    .child(ui::button("pr-save", "Save look", Some("check"), false, on(weak, |e, cx| e.add_preset(cx))))
                    .child(ui::button("pr-import", "Import…", None, false, on(weak, |e, cx| e.import_presets(cx))))
                    .child(ui::button("pr-export", "Export…", None, false, on(weak, |e, cx| e.export_presets(cx)))),
            )
    }

    fn background_section(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let t = theme();
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
        let mode_chip = |id: &'static str, name: &'static str, m: BgMode| {
            let auto = auto.clone();
            let wallpaper0 = wallpaper0.clone();
            ui::chip(id, name, mode == m, on(weak, move |e, _| {
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
                let current = g;
                let auto_g = Gradient { stops: auto.clone(), angle: current.angle };
                let mut grid = div().flex().flex_wrap().gap_1().child(
                    div()
                        .relative()
                        .child(ui::swatch("g-auto", &auto.iter().map(|c| c.rgba()).collect::<Vec<_>>(), current.stops == auto, 32., on(weak, move |e, _| {
                            let g = auto_g.clone();
                            e.edit("bg", |d| d.style.background = Background::Gradient(g));
                        })))
                        .child(div().absolute().top(px(8.)).left(px(8.)).child(ui::icon("sparkles", 16., gpui::rgb(0xFFFFFF)))),
                );
                for (i, (_, preset)) in GRADIENTS.iter().enumerate() {
                    let stops = preset.stops.clone();
                    let angle = current.angle;
                    grid = grid.child(ui::swatch(eid("grad", i), &preset.stops.iter().map(|c| c.rgba()).collect::<Vec<_>>(), preset.stops == current.stops, 32., on(weak, move |e, _| {
                        let g = Gradient { stops: stops.clone(), angle };
                        e.edit("bg", |d| d.style.background = Background::Gradient(g));
                    })));
                }
                bg = bg.child(grid).child(doc_slider(weak, "angle", "Angle", current.angle, (0., 360.), format!("{:.0}°", current.angle), |d, v| {
                    if let Background::Gradient(g) = &mut d.style.background {
                        g.angle = (v / 15.0).round() * 15.0;
                    }
                }));
            }
            Background::Solid(col) => {
                let current = *col;
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
                        .w(px(WALL_W))
                        .h(px(WALL_W * 0.625))
                        .rounded(px(6.))
                        .overflow_hidden()
                        .cursor_pointer()
                        .border_2()
                        .border_color(if selected { t.accent } else { rgba(0) })
                        .hover(|s| s.border_color(t.muted))
                        .child(img(ImageSource::Render(w.thumb.clone())).size_full().rounded(px(4.)).object_fit(ObjectFit::Cover))
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
                    .child(note("Your desktop wallpaper, the Windows wallpapers and images you add. Nothing is downloaded."));
            }
            Background::Blurred => bg = bg.child(note("A soft, blurred copy of your screenshot, so it always matches its colors.")),
            Background::None => bg = bg.child(note("Transparent background, for docs and slides.")),
        }
        bg
    }

    fn frame_sections(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let st = &self.doc.style;
        let frame = ui::section("Layout")
            .child(doc_slider(weak, "pad", "Padding", st.padding, (0., 240.), format!("{:.0}", st.padding), |d, v| d.style.padding = v.round()))
            .child(doc_slider(weak, "inset", "Inset", st.inset, (0., 120.), format!("{:.0}", st.inset), |d, v| d.style.inset = v.round()))
            .child(doc_slider(weak, "radius", "Corner radius", st.radius, (0., 48.), format!("{:.0}", st.radius), |d, v| d.style.radius = v.round()))
            .child(doc_slider(weak, "shadow", "Shadow", st.shadow, (0., 120.), format!("{:.0}", st.shadow), |d, v| d.style.shadow = v.round()))
            .child(doc_slider(weak, "shadow-o", "Shadow opacity", st.shadow_opacity, (0., 1.), format!("{:.0}%", st.shadow_opacity * 100.), |d, v| d.style.shadow_opacity = v))
            .child(ui::toggle("balance", "Auto-balance inset color", st.auto_balance, on(weak, |e, _| e.edit("balance", |d| d.style.auto_balance = !d.style.auto_balance))))
            .child(div().text_xs().text_color(theme().muted).child("Aspect ratio"))
            .child(div().flex().flex_wrap().gap_1().children(ASPECTS.iter().enumerate().map(|(i, (name, a))| {
                let a = *a;
                ui::chip(eid("aspect", i), *name, st.aspect == a, on(weak, move |e, _| e.edit("aspect", |d| d.style.aspect = a)))
            })))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_3()
                    .child(div().text_xs().text_color(theme().muted).child("Position"))
                    .child(grid9(weak, "align", Some(st.align), |e, i| e.edit("align", |d| d.style.align = i))),
            );
        let chrome = ui::section("Window frame")
            .child(div().flex().flex_wrap().gap_1().children(Chrome::ALL.iter().enumerate().map(|(i, ch)| {
                let ch = *ch;
                ui::chip(eid("chrome", i), ch.label(), st.chrome == ch, on(weak, move |e, _| e.edit("chrome", |d| d.style.chrome = ch)))
            })))
            .when(st.chrome != Chrome::None, |d| {
                d.child(ui::toggle("ctitle", "Show window title", st.chrome_title, on(weak, |e, _| e.edit("ctitle", |d| d.style.chrome_title = !d.style.chrome_title))))
            });
        div().flex().flex_col().child(frame).child(chrome)
    }

    fn effects_sections(&self, weak: &WeakEntity<Editor>) -> gpui::Div {
        let t = theme();
        let fx = &self.doc.effects;

        let b = &fx.border;
        let border = ui::section("Border")
            .child(ui::toggle("border-on", "Border around screenshot", b.enabled, on(weak, |e, _| e.edit("border-on", |d| d.effects.border.enabled = !d.effects.border.enabled))))
            .when(b.enabled, |d| {
                d.child(doc_slider(weak, "border-w", "Width", b.width, (1., 40.), format!("{:.0}", b.width), |d, v| d.effects.border.width = v.round()))
                    .child(color_row(weak, "border-c", b.color, 22., |e, col| e.edit("border-c", |d| d.effects.border.color = col)))
            });

        let wm = &fx.watermark;
        let typing = self.typing == Some(Typing::Watermark);
        let field = div()
            .id("wm-text")
            .h(px(32.))
            .px_2()
            .flex()
            .items_center()
            .rounded(px(4.))
            .bg(t.control)
            .border_1()
            .border_color(t.border)
            .when(typing, |d| d.border_b_2().border_color(t.accent))
            .cursor(gpui::CursorStyle::IBeam)
            .text_sm()
            .child(if wm.text.is_empty() && !typing {
                div().text_color(t.muted).child("Watermark text")
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
        let wm_font = wm.font.clone();
        let fonts_row = div().flex().flex_wrap().gap_1().children(fonts::families().iter().take(10).enumerate().map(|(i, fam)| {
            let mut f = wm_font.clone();
            f.family = fam.to_string();
            if !fonts::weights(fam).contains(&f.weight) {
                f.weight = Weight::Regular;
            }
            ui::chip(eid("wm-fam", i), fam.to_string(), wm_font.family == *fam, on(weak, move |e, _| {
                let f = f.clone();
                e.edit("wm-font", |d| d.effects.watermark.font = f)
            }))
            .font_family(SharedString::from(fam.to_string()))
        }));
        let weights = fonts::weights(&wm_font.family).into_iter().map(|w| {
            let mut f = wm_font.clone();
            f.weight = w;
            ui::chip(SharedString::from(format!("wm-w-{}", w.label())), w.label(), wm_font.weight == w, on(weak, move |e, _| {
                let f = f.clone();
                e.edit("wm-font", |d| d.effects.watermark.font = f)
            }))
        });
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
                    .child(fonts_row)
                    .child(div().flex().flex_wrap().gap_1().children(weights))
                    .child(doc_slider(weak, "wm-size", "Size", wm.size, (8., 120.), format!("{:.0}", wm.size), |d, v| d.effects.watermark.size = v.round()))
                    .child(doc_slider(weak, "wm-op", "Opacity", wm.opacity, (0.05, 1.), format!("{:.0}%", wm.opacity * 100.), |d, v| d.effects.watermark.opacity = v))
                    .child(color_row(weak, "wm-c", wm.color, 22., |e, col| e.edit("wm-c", |d| d.effects.watermark.color = col)))
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_3()
                            .child(div().text_xs().text_color(t.muted).child("Placement"))
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
                div().flex().flex_wrap().gap_1().children(
                    [("Flat", 0.0, 0.0, 0.0), ("Left", 0.0, 24.0, 0.0), ("Right", 0.0, -24.0, 0.0), ("Tilt back", 22.0, 0.0, 0.0), ("Hero", 14.0, -20.0, 4.0)]
                        .into_iter()
                        .enumerate()
                        .map(|(i, (name, x, y, z))| {
                            ui::chip(eid("persp", i), name, p.rot_x == x && p.rot_y == y && p.rot_z == z, on(weak, move |e, _| {
                                e.edit("persp", |d| {
                                    d.effects.perspective.rot_x = x;
                                    d.effects.perspective.rot_y = y;
                                    d.effects.perspective.rot_z = z;
                                })
                            }))
                        }),
                ),
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

    pub(super) fn bottom_bar(&self, weak: &WeakEntity<Editor>) -> impl IntoElement {
        let t = theme();
        let (ew, eh) = self.export_size();
        let zoom = self.zoom_percent();
        let zb = |id: &'static str, text: &'static str, tip: &'static str, f: fn(&mut Editor)| {
            div()
                .id(id)
                .px_2()
                .h(px(28.))
                .flex()
                .items_center()
                .rounded(px(4.))
                .cursor_pointer()
                .text_xs()
                .text_color(t.text)
                .hover(|s| s.bg(t.control_hover))
                .child(text)
                .tooltip(ui::tooltip(tip))
                .on_click(on(weak, move |e, _| f(e)))
        };
        div()
            .h(px(52.))
            .flex_none()
            .flex()
            .items_center()
            .gap_2()
            .px_3()
            .bg(t.layer)
            .border_t_1()
            .border_color(t.border)
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_0p5()
                    .p_0p5()
                    .rounded(px(4.))
                    .border_1()
                    .border_color(t.border)
                    .child(zb("z-fit", "Fit", "Fit (Ctrl+1)", |e| e.zoom_to(None, None)))
                    .child(zb("z-100", "100%", "Actual size (Ctrl+0)", |e| e.zoom_to(Some(1.0), None)))
                    .child(zb("z-out", "−", "Zoom out (Ctrl+-)", |e| e.zoom_step(false)))
                    .child(div().w(px(48.)).flex().justify_center().text_xs().text_color(t.text).child(format!("{zoom:.0}%")))
                    .child(zb("z-in", "+", "Zoom in (Ctrl+=)", |e| e.zoom_step(true))),
            )
            .child(div().text_xs().text_color(t.muted).child(format!("{ew} × {eh} px")))
            .child(div().flex_1())
            .child(ui::icon_button("pin", "pin", false, "Pin to screen (Ctrl+P)", on(weak, |e, cx| e.pin(cx))))
            .child(ui::icon_button("saveas", "folder", false, "Save as… (Ctrl+Shift+S)", on_window(weak, |e, window, cx| e.commit(AfterSave::Export { ask: true }, window, cx))))
            .child(ui::button("copy", "Copy", Some("copy"), false, on(weak, |e, cx| e.copy(cx))))
            .child(ui::button("save", "Save", Some("save"), false, on_window(weak, |e, window, cx| e.commit(AfterSave::Export { ask: false }, window, cx))))
            .child(ui::button("done", "Done", Some("check"), true, on_window(weak, |e, window, cx| e.commit(AfterSave::Close, window, cx))))
    }
}

const WALL_W: f32 = 126.;

impl Render for Editor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.ensure_preview(window, cx);
        let weak = cx.entity().downgrade();
        let t = theme();
        div()
            .id("editor")
            .size_full()
            .flex()
            .flex_col()
            .bg(t.base)
            .font_family(ui::FONT)
            .text_color(t.text)
            .child(self.toolbar(&weak))
            .child(
                div()
                    .id("editor-body")
                    .track_focus(&self.focus)
                    .on_key_down(cx.listener(Self::on_key))
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .flex_col()
                    .child(self.style_bar(&weak))
                    .child(div().flex_1().min_h_0().flex().child(self.canvas_area(cx)).child(self.sidebar(&weak)))
                    .child(self.bottom_bar(&weak)),
            )
    }
}
