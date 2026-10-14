//! Glint's widget kit and Windows 11 look: one theme that follows the system light/dark
//! setting and accent color, 8 px cards, 4 px controls, 1 px borders.

use gpui::prelude::*;
use gpui::{
    canvas, div, linear_color_stop, linear_gradient, px, relative, rgba, svg, AnyView, App, Bounds, ClickEvent,
    DispatchPhase, ElementId, Hsla, MouseButton, MouseMoveEvent, MouseUpEvent, Pixels, Point, Rgba, SharedString,
    Window,
};
use std::cell::Cell;
use std::rc::Rc;
use std::sync::{LazyLock, RwLock};
use windows::core::{w, PCWSTR};
use windows::Win32::System::Registry::{RegGetValueW, HKEY_CURRENT_USER, RRF_RT_REG_DWORD};

pub const FONT: &str = "Segoe UI Variable Text";

/// Corner radius of windows, cards and panels.
pub const RADIUS_WINDOW: f32 = 8.;
/// Corner radius of buttons, fields and other controls.
pub const RADIUS_CONTROL: f32 = 4.;

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Theme {
    pub dark: bool,
    pub accent: Rgba,
    /// Text and icons on accent fills.
    pub on_accent: Rgba,
    /// Window surface over the Mica backdrop.
    pub base: Rgba,
    /// Toolbars and sidebars.
    pub layer: Rgba,
    pub card: Rgba,
    pub control: Rgba,
    pub control_hover: Rgba,
    pub border: Rgba,
    pub text: Rgba,
    pub muted: Rgba,
    /// Behind images: the editor canvas and thumbnail wells.
    pub canvas: Rgba,
    /// Text box fill.
    pub field: Rgba,
    /// Menus, tooltips and other popups.
    pub flyout: Rgba,
}

fn reg_dword(key: PCWSTR, value: PCWSTR) -> Option<u32> {
    let mut v = 0u32;
    let mut size = 4u32;
    let r = unsafe {
        RegGetValueW(HKEY_CURRENT_USER, key, value, RRF_RT_REG_DWORD, None, Some(&mut v as *mut u32 as *mut _), Some(&mut size))
    };
    r.is_ok().then_some(v)
}

/// Reads the system light/dark setting and accent color.
fn read_theme() -> Theme {
    let light = reg_dword(w!(r"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize"), w!("AppsUseLightTheme")) == Some(1);
    // AccentColor is stored as 0xAABBGGRR.
    let (mut r, mut g, mut b) = reg_dword(w!(r"Software\Microsoft\Windows\DWM"), w!("AccentColor"))
        .map(|v| (v & 0xFF, (v >> 8) & 0xFF, (v >> 16) & 0xFF))
        .unwrap_or((0x00, 0x67, 0xC0));
    if !light && r.max(g).max(b) < 0x90 {
        // Dark surfaces need a light enough accent to read; lift dim accents toward white.
        let lift = |c: u32| c + (0xFF - c) / 3;
        (r, g, b) = (lift(r), lift(g), lift(b));
    }
    let accent = (r << 24) | (g << 16) | (b << 8) | 0xFF;
    let luma = (0.299 * r as f32 + 0.587 * g as f32 + 0.114 * b as f32) / 255.0;
    let on_accent = if luma > 0.6 { 0x000000FF } else { 0xFFFFFFFF };
    if light {
        Theme {
            dark: false,
            accent: rgba(accent),
            on_accent: rgba(on_accent),
            base: rgba(0xF3F3F3D8),
            layer: rgba(0xFBFBFBA8),
            card: rgba(0xFFFFFFB8),
            control: rgba(0xFFFFFFB3),
            control_hover: rgba(0xF0F0F0F0),
            border: rgba(0x0000001A),
            text: rgba(0x1B1B1BFF),
            muted: rgba(0x5F5F5FFF),
            canvas: rgba(0xE4E4E4FF),
            field: rgba(0xFFFFFFE6),
            flyout: rgba(0xF9F9F9F8),
        }
    } else {
        Theme {
            dark: true,
            accent: rgba(accent),
            on_accent: rgba(on_accent),
            base: rgba(0x202020D0),
            layer: rgba(0x2C2C2CA0),
            card: rgba(0xFFFFFF0D),
            control: rgba(0xFFFFFF0F),
            control_hover: rgba(0xFFFFFF1C),
            border: rgba(0xFFFFFF17),
            text: rgba(0xFFFFFFFF),
            muted: rgba(0xFFFFFFA3),
            canvas: rgba(0x141414FF),
            field: rgba(0x1C1C1CE6),
            flyout: rgba(0x2C2C2CF8),
        }
    }
}

static THEME: LazyLock<RwLock<Theme>> = LazyLock::new(|| RwLock::new(read_theme()));

/// The current system theme.
pub fn theme() -> Theme {
    *THEME.read().unwrap()
}

/// Re-reads the system theme. Returns true when it changed and windows need a redraw.
pub fn refresh_theme() -> bool {
    let next = read_theme();
    let mut t = THEME.write().unwrap();
    let changed = *t != next;
    *t = next;
    changed
}

pub fn accent() -> Rgba {
    theme().accent
}

pub fn accent_alpha(alpha: u8) -> Rgba {
    Rgba { a: alpha as f32 / 255.0, ..theme().accent }
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
        let t = theme();
        div()
            .font_family(FONT)
            .px_2()
            .py_1()
            .bg(t.flyout)
            .border_1()
            .border_color(t.border)
            .rounded(px(RADIUS_CONTROL))
            .text_xs()
            .text_color(t.text)
            .child(self.0.clone())
    }
}

pub fn tooltip(text: impl Into<SharedString>) -> impl Fn(&mut Window, &mut App) -> AnyView + 'static {
    let text = text.into();
    move |_, cx| cx.new(|_| Tip(text.clone())).into()
}

/// A square toolbar button holding one icon; `active` fills it with the accent.
pub fn icon_button(
    id: impl Into<ElementId>,
    icon_name: &str,
    active: bool,
    tip: impl Into<SharedString>,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let t = theme();
    div()
        .id(id.into())
        .size(px(32.))
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .rounded(px(RADIUS_CONTROL))
        .cursor_pointer()
        .when(active, |d| d.bg(t.accent))
        .when(!active, |d| d.hover(|s| s.bg(t.control_hover)))
        .child(icon(icon_name, 16., if active { t.on_accent } else { t.text }))
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
    let t = theme();
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
        .border_1()
        .when(primary, |d| d.bg(t.accent).border_color(t.accent).text_color(t.on_accent).hover(|s| s.opacity(0.9)))
        .when(!primary, |d| d.bg(t.control).border_color(t.border).text_color(t.text).hover(|s| s.bg(t.control_hover)))
        .when_some(icon_name, |d, name| d.child(icon(name, 14., if primary { t.on_accent } else { t.text })))
        .child(label.into())
        .on_click(on_click)
}

pub fn chip(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    selected: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let t = theme();
    div()
        .id(id.into())
        .px_2()
        .h(px(26.))
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .rounded(px(RADIUS_CONTROL))
        .text_xs()
        .cursor_pointer()
        .border_1()
        .when(selected, |d| d.bg(t.accent).border_color(t.accent).text_color(t.on_accent))
        .when(!selected, |d| d.bg(t.control).border_color(t.border).text_color(t.text).hover(|s| s.bg(t.control_hover)))
        .child(label.into())
        .on_click(on_click)
}

pub fn toggle(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    on: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> impl IntoElement {
    let t = theme();
    div()
        .id(id.into())
        .flex()
        .items_center()
        .justify_between()
        .gap_3()
        .cursor_pointer()
        .text_sm()
        .text_color(t.text)
        .child(label.into())
        .child(
            div()
                .w(px(40.))
                .h(px(20.))
                .flex_none()
                .rounded_full()
                .p(px(3.))
                .flex()
                .items_center()
                .border_1()
                .when(on, |d| d.bg(t.accent).border_color(t.accent).justify_end())
                .when(!on, |d| d.border_color(t.muted).justify_start())
                .child(div().size(px(12.)).rounded_full().bg(if on { t.on_accent } else { t.muted })),
        )
        .on_click(on_click)
}

/// A color swatch; with two or more colors it shows a gradient.
pub fn swatch(
    id: impl Into<ElementId>,
    colors: &[Rgba],
    selected: bool,
    size: f32,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let t = theme();
    let inner = div().size_full().rounded(px(size * 0.22)).border_1().border_color(t.border);
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
        .rounded(px(size * 0.3))
        .cursor_pointer()
        .border_2()
        .border_color(if selected { t.accent } else { rgba(0) })
        .hover(|s| s.border_color(t.muted))
        .child(inner)
        .on_click(on_click)
}

/// A card with a title, used for inspector sections.
pub fn section(title: &str) -> gpui::Div {
    let t = theme();
    div()
        .flex()
        .flex_col()
        .gap_2()
        .p_3()
        .mb_2()
        .rounded(px(RADIUS_WINDOW))
        .bg(t.card)
        .border_1()
        .border_color(t.border)
        .child(div().text_sm().font_weight(gpui::FontWeight::SEMIBOLD).text_color(t.text).child(title.to_string()))
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

/// A horizontal slider track. Keeps tracking the pointer outside its bounds while dragging.
pub fn slider_track(key: &str, value: f32, min: f32, max: f32, on_change: OnValue) -> gpui::Stateful<gpui::Div> {
    let t = theme();
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
            let f = ((pos.x - b.origin.x) / b.size.width).clamp(0.0, 1.0);
            on_change(min + f * (max - min), window, cx);
        })
    };
    div()
        .id(SharedString::from(key.to_string()))
        .relative()
        .h(px(20.))
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
                .bg(t.border)
                .child(div().absolute().left_0().top_0().h_full().w(relative(frac)).rounded_full().bg(t.accent)),
        )
        .child(
            div()
                .absolute()
                .top(px(1.))
                .left(relative(frac))
                .ml(px(-9.))
                .size(px(18.))
                .rounded_full()
                .bg(rgba(if t.dark { 0x454545FF } else { 0xFFFFFFFF }))
                .border_1()
                .border_color(t.border)
                .flex()
                .items_center()
                .justify_center()
                .child(div().size(px(10.)).rounded_full().bg(t.accent)),
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
                    window.on_mouse_event(|_: &MouseUpEvent, _, _, _| ACTIVE_SLIDER.set(None));
                },
            )
            .absolute()
            .top_0()
            .left_0()
            .size_full(),
        )
}

/// Labeled slider with its value on the right.
pub fn slider(key: &str, label: &str, value: f32, min: f32, max: f32, display: String, on_change: OnValue) -> impl IntoElement {
    let t = theme();
    div()
        .flex()
        .flex_col()
        .gap_0p5()
        .child(
            div()
                .flex()
                .justify_between()
                .text_xs()
                .child(div().text_color(t.text).child(label.to_string()))
                .child(div().text_color(t.muted).child(display)),
        )
        .child(slider_track(key, value, min, max, on_change))
}

/// Keycap-style label for shortcuts.
pub fn kbd(text: &str) -> impl IntoElement {
    let t = theme();
    div().flex().gap_1().children(text.split('+').map(move |k| {
        div()
            .px_1p5()
            .py(px(1.))
            .rounded(px(RADIUS_CONTROL))
            .bg(t.control)
            .border_1()
            .border_color(t.border)
            .text_xs()
            .text_color(t.text)
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
    let t = theme();
    div()
        .h(px(28.))
        .px_2()
        .flex()
        .items_center()
        .gap_1p5()
        .rounded(px(RADIUS_CONTROL))
        .bg(t.field)
        .border_1()
        .border_color(if focused { t.accent } else { t.border })
        .text_sm()
        .overflow_hidden()
        .child(
            div()
                .flex()
                .items_center()
                .min_w_0()
                .whitespace_nowrap()
                .when(value.is_empty() && !focused, |d| d.text_color(t.muted).child(placeholder.to_string()))
                .when(!value.is_empty(), |d| d.text_color(t.text).child(value.to_string()))
                .when(focused, |d| d.child(div().w(px(1.5)).h(px(16.)).bg(t.accent))),
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
