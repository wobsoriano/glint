//! Small widget kit on top of GPUI primitives.

use gpui::prelude::*;
use gpui::{
    canvas, div, linear_color_stop, linear_gradient, px, relative, rgb, rgba, svg, AnyView, App,
    Bounds, ClickEvent, DispatchPhase, ElementId, Hsla, MouseButton, MouseMoveEvent,
    MouseUpEvent, Pixels, Point, Rgba, SharedString, Window,
};
use std::cell::Cell;
use std::rc::Rc;

pub const BG: u32 = 0x141416;
pub const PANEL: u32 = 0x1C1C1F;
pub const ELEV: u32 = 0x27272B;
pub const HOVER: u32 = 0x313136;
pub const BORDER: u32 = 0x2E2E33;
pub const TEXT: u32 = 0xECECEF;
pub const MUTED: u32 = 0x8D8D96;
pub const ACCENT: u32 = 0x7C8CFF;

pub const FONT: &str = "Segoe UI Variable Text";

/// Corner radius of windows, cards and panels.
pub const RADIUS_WINDOW: f32 = 8.;
/// Corner radius of buttons, fields and other controls.
pub const RADIUS_CONTROL: f32 = 4.;

/// The Windows accent color (Settings > Personalization > Colors), read once.
fn system_accent() -> u32 {
    use windows::Win32::System::Registry::{RegGetValueW, HKEY_CURRENT_USER, RRF_RT_REG_DWORD};
    static CACHE: std::sync::OnceLock<u32> = std::sync::OnceLock::new();
    *CACHE.get_or_init(|| {
        let mut abgr = 0u32;
        let mut size = 4u32;
        let ok = unsafe {
            RegGetValueW(
                HKEY_CURRENT_USER,
                windows::core::w!(r"Software\Microsoft\Windows\DWM"),
                windows::core::w!("AccentColor"),
                RRF_RT_REG_DWORD,
                None,
                Some(&mut abgr as *mut u32 as *mut _),
                Some(&mut size),
            )
        }
        .is_ok();
        if !ok {
            return ACCENT;
        }
        let (r, g, b) = (abgr & 0xFF, (abgr >> 8) & 0xFF, (abgr >> 16) & 0xFF);
        // Dark surfaces need a light enough accent to read; lift dim accents toward white.
        let lift = |c: u32| if r.max(g).max(b) < 0x90 { c + (0xFF - c) / 3 } else { c };
        (lift(r) << 16) | (lift(g) << 8) | lift(b)
    })
}

pub fn accent() -> Rgba {
    rgb(system_accent())
}

pub fn accent_alpha(alpha: u8) -> Rgba {
    rgba((system_accent() << 8) | alpha as u32)
}

pub fn accent_hover() -> Rgba {
    let c = system_accent();
    let up = |v: u32| v + (0xFF - v) / 5;
    rgb((up(c >> 16) << 16) | (up((c >> 8) & 0xFF) << 8) | up(c & 0xFF))
}

pub type OnValue = Rc<dyn Fn(f32, &mut Window, &mut App)>;

pub fn icon(name: &str, size: f32, color: impl Into<Hsla>) -> impl IntoElement {
    svg()
        .path(SharedString::from(format!("icons/{name}.svg")))
        .size(px(size))
        .flex_none()
        .text_color(color.into())
}

struct Tip(SharedString);

impl Render for Tip {
    fn render(&mut self, _: &mut Window, _: &mut gpui::Context<Self>) -> impl IntoElement {
        div()
            .font_family(FONT)
            .px_2()
            .py_1()
            .bg(rgb(0x0C0C0E))
            .border_1()
            .border_color(rgb(BORDER))
            .rounded(px(RADIUS_CONTROL))
            .text_xs()
            .text_color(rgb(TEXT))
            .child(self.0.clone())
    }
}

pub fn tooltip(text: impl Into<SharedString>) -> impl Fn(&mut Window, &mut App) -> AnyView + 'static {
    let text = text.into();
    move |_, cx| cx.new(|_| Tip(text.clone())).into()
}

pub fn icon_button(
    id: impl Into<ElementId>,
    icon_name: &str,
    active: bool,
    tip: impl Into<SharedString>,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    div()
        .id(id.into())
        .size(px(32.))
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(RADIUS_CONTROL))
        .cursor_pointer()
        .when(active, |d| d.bg(accent()))
        .when(!active, |d| d.hover(|s| s.bg(rgb(HOVER))))
        .child(icon(icon_name, 17., rgb(if active { 0xFFFFFF } else { TEXT })))
        .tooltip(tooltip(tip))
        .on_click(on_click)
}

pub fn button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    icon_name: Option<&str>,
    primary: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    div()
        .id(id.into())
        .h(px(30.))
        .px_3()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .gap_1p5()
        .rounded(px(RADIUS_CONTROL))
        .cursor_pointer()
        .text_sm()
        .when(primary, |d| {
            d.bg(accent())
                .text_color(rgb(0xFFFFFF))
                .hover(|s| s.bg(accent_hover()))
        })
        .when(!primary, |d| {
            d.bg(rgb(ELEV))
                .text_color(rgb(TEXT))
                .border_1()
                .border_color(rgb(BORDER))
                .hover(|s| s.bg(rgb(HOVER)))
        })
        .when_some(icon_name, |d, name| {
            d.child(icon(name, 14., rgb(if primary { 0xFFFFFF } else { TEXT })))
        })
        .child(label.into())
        .on_click(on_click)
}

pub fn section(title: &str) -> gpui::Div {
    div()
        .flex()
        .flex_col()
        .gap_2()
        .pb_4()
        .mb_3()
        .border_b_1()
        .border_color(rgb(BORDER))
        .child(
            div()
                .text_xs()
                .font_weight(gpui::FontWeight::SEMIBOLD)
                .text_color(rgb(MUTED))
                .child(title.to_uppercase()),
        )
}

pub fn chip(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    selected: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    div()
        .id(id.into())
        .px_2()
        .h(px(26.))
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(RADIUS_CONTROL))
        .text_xs()
        .cursor_pointer()
        .border_1()
        .when(selected, |d| {
            d.bg(accent_alpha(0x33))
                .border_color(accent())
                .text_color(rgb(0xFFFFFF))
        })
        .when(!selected, |d| {
            d.bg(rgb(ELEV))
                .border_color(rgb(BORDER))
                .text_color(rgb(TEXT))
                .hover(|s| s.bg(rgb(HOVER)))
        })
        .child(label.into())
        .on_click(on_click)
}

pub fn toggle(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    on: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    div()
        .id(id.into())
        .flex()
        .items_center()
        .justify_between()
        .gap_3()
        .cursor_pointer()
        .text_sm()
        .text_color(rgb(TEXT))
        .child(label.into())
        .child(
            div()
                .w(px(34.))
                .h(px(20.))
                .flex_none()
                .rounded_full()
                .p(px(2.))
                .flex()
                .when(on, |d| d.bg(accent()).justify_end())
                .when(!on, |d| d.bg(rgb(HOVER)).justify_start())
                .child(div().size(px(16.)).rounded_full().bg(rgb(0xFFFFFF))),
        )
        .on_click(on_click)
}

/// A color swatch; `colors` with two entries renders as a gradient.
pub fn swatch(
    id: impl Into<ElementId>,
    colors: &[Rgba],
    selected: bool,
    size: f32,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let inner = div().size_full().rounded(px(size * 0.28));
    let inner = if colors.len() >= 2 {
        inner.bg(linear_gradient(
            135.,
            linear_color_stop(colors[0], 0.),
            linear_color_stop(colors[colors.len() - 1], 1.),
        ))
    } else {
        inner.bg(colors[0])
    };
    div()
        .id(id.into())
        .size(px(size))
        .flex_none()
        .p(px(2.))
        .rounded(px(size * 0.32))
        .cursor_pointer()
        .border_2()
        .when(selected, |d| d.border_color(accent()))
        .when(!selected, |d| d.border_color(rgba(0x00000000)).hover(|s| s.border_color(rgb(HOVER))))
        .child(inner.border_1().border_color(rgba(0xFFFFFF1F)))
        .on_click(on_click)
}

thread_local! {
    static ACTIVE_SLIDER: Cell<Option<u64>> = const { Cell::new(None) };
}

fn hash(s: &str) -> u64 {
    use std::hash::{Hash, Hasher};
    let mut h = std::collections::hash_map::DefaultHasher::new();
    s.hash(&mut h);
    h.finish()
}

/// Labeled horizontal slider. Keeps tracking the pointer even outside its bounds while dragging.
pub fn slider(
    key: &str,
    label: &str,
    value: f32,
    min: f32,
    max: f32,
    display: String,
    on_change: OnValue,
) -> impl IntoElement {
    let id = hash(key);
    let frac = ((value - min) / (max - min)).clamp(0.0, 1.0);
    let bounds: Rc<Cell<Bounds<Pixels>>> = Rc::default();
    let set: Rc<dyn Fn(Point<Pixels>, &mut Window, &mut App)> = {
        let bounds = bounds.clone();
        Rc::new(move |pos, window, cx| {
            let b = bounds.get();
            if b.size.width <= px(0.) {
                return;
            }
            let t = ((pos.x - b.origin.x) / b.size.width).clamp(0.0, 1.0);
            on_change(min + t * (max - min), window, cx);
        })
    };
    div()
        .flex()
        .flex_col()
        .gap_1()
        .child(
            div()
                .flex()
                .justify_between()
                .text_xs()
                .child(div().text_color(rgb(TEXT)).child(label.to_string()))
                .child(div().text_color(rgb(MUTED)).child(display)),
        )
        .child(
            div()
                .id(SharedString::from(key.to_string()))
                .relative()
                .h(px(18.))
                .flex()
                .items_center()
                .cursor_pointer()
                .on_mouse_down(MouseButton::Left, {
                    let set = set.clone();
                    move |e, window, cx| {
                        ACTIVE_SLIDER.set(Some(id));
                        set(e.position, window, cx);
                    }
                })
                .child(
                    div()
                        .relative()
                        .w_full()
                        .h(px(4.))
                        .rounded_full()
                        .bg(rgb(HOVER))
                        .child(
                            div()
                                .absolute()
                                .left_0()
                                .top_0()
                                .h_full()
                                .w(relative(frac))
                                .rounded_full()
                                .bg(accent()),
                        ),
                )
                .child(
                    div()
                        .absolute()
                        .top(px(2.))
                        .left(relative(frac))
                        .ml(px(-7.))
                        .size(px(14.))
                        .rounded_full()
                        .bg(rgb(0xFFFFFF))
                        .shadow_sm(),
                )
                .child(
                    canvas(
                        move |b, _, _| bounds.set(b),
                        move |_, _, window, _| {
                            window.on_mouse_event({
                                let set = set.clone();
                                move |e: &MouseMoveEvent, phase, window, cx| {
                                    if phase == DispatchPhase::Bubble
                                        && ACTIVE_SLIDER.get() == Some(id)
                                        && e.pressed_button == Some(MouseButton::Left)
                                    {
                                        set(e.position, window, cx);
                                    }
                                }
                            });
                            window.on_mouse_event(|_: &MouseUpEvent, _, _, _| {
                                ACTIVE_SLIDER.set(None);
                            });
                        },
                    )
                    .absolute()
                    .top_0()
                    .left_0()
                    .size_full(),
                ),
        )
}

/// Keycap-style label for shortcuts.
pub fn kbd(text: &str) -> impl IntoElement {
    div()
        .flex()
        .gap_1()
        .children(text.split('+').map(|k| {
            div()
                .px_1p5()
                .py(px(1.))
                .rounded(px(RADIUS_CONTROL))
                .bg(rgb(ELEV))
                .border_1()
                .border_color(rgb(BORDER))
                .text_xs()
                .text_color(rgb(TEXT))
                .child(k.to_string())
        }))
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Edit {
    Changed,
    Submit,
    Cancel,
    Ignored,
}

/// Applies one key press to a single-line text value (type, Backspace, Ctrl+Backspace,
/// Ctrl+V, Enter, Esc). The caret is always at the end.
pub fn edit_text(text: &mut String, e: &gpui::KeyDownEvent) -> Edit {
    let k = &e.keystroke;
    let ctrl = k.modifiers.control;
    match k.key.as_str() {
        "enter" => Edit::Submit,
        "escape" => Edit::Cancel,
        "backspace" if ctrl => {
            let trimmed = text.trim_end().len();
            let cut = text[..trimmed].rfind(char::is_whitespace).map(|i| i + 1).unwrap_or(0);
            text.truncate(cut);
            Edit::Changed
        }
        "backspace" => {
            text.pop();
            Edit::Changed
        }
        "v" if ctrl => match arboard::Clipboard::new().and_then(|mut c| c.get_text()) {
            Ok(t) => {
                text.push_str(t.lines().next().unwrap_or_default());
                Edit::Changed
            }
            Err(_) => Edit::Ignored,
        },
        _ if ctrl || k.modifiers.alt => Edit::Ignored,
        "space" => {
            text.push(' ');
            Edit::Changed
        }
        _ => match &k.key_char {
            Some(c) if !c.chars().any(char::is_control) => {
                text.push_str(c);
                Edit::Changed
            }
            _ => Edit::Ignored,
        },
    }
}

/// A single-line text box drawn for a value edited with [`edit_text`].
pub fn text_box(value: &str, placeholder: &str, focused: bool) -> gpui::Div {
    div()
        .h(px(28.))
        .px_2()
        .flex()
        .items_center()
        .gap_1p5()
        .rounded(px(RADIUS_CONTROL))
        .bg(rgb(0x101012))
        .border_1()
        .border_color(if focused { accent() } else { rgb(BORDER) })
        .text_sm()
        .overflow_hidden()
        .child(
            div()
                .flex()
                .items_center()
                .min_w_0()
                .whitespace_nowrap()
                .when(value.is_empty() && !focused, |d| d.text_color(rgb(MUTED)).child(placeholder.to_string()))
                .when(!value.is_empty(), |d| d.text_color(rgb(TEXT)).child(value.to_string()))
                .when(focused, |d| d.child(div().w(px(1.5)).h(px(16.)).bg(accent()))),
        )
}

pub trait ToRgba {
    fn rgba(self) -> gpui::Rgba;
}

impl ToRgba for crate::style::Color {
    fn rgba(self) -> gpui::Rgba {
        gpui::Rgba {
            r: self.r() as f32 / 255.0,
            g: self.g() as f32 / 255.0,
            b: self.b() as f32 / 255.0,
            a: self.a() as f32 / 255.0,
        }
    }
}
