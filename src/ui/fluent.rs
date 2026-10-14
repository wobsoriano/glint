//! Windows 11 look: system light/dark theme and accent color, Mica backdrop, 4 px controls,
//! 8 px cards, 1 px borders.

use super::{hash, icon, tooltip, OnValue};
use gpui::prelude::*;
use gpui::{
    canvas, div, px, relative, rgba, Bounds, ClickEvent, DispatchPhase, ElementId, MouseButton, MouseMoveEvent, MouseUpEvent,
    Pixels, Point, Rgba, SharedString, Window,
};
use std::cell::Cell;
use std::rc::Rc;
use std::sync::OnceLock;
use windows::core::{w, PCWSTR};
use windows::Win32::System::Registry::{RegGetValueW, HKEY_CURRENT_USER, RRF_RT_REG_DWORD};

pub const FONT: &str = "Segoe UI Variable Text";

/// Colors as 0xRRGGBBAA.
#[derive(Clone, Copy, Debug)]
pub struct Theme {
    pub dark: bool,
    pub accent: u32,
    /// Text and icons on accent fills.
    pub on_accent: u32,
    /// Window surface over the Mica backdrop.
    pub base: u32,
    /// Toolbars and sidebars.
    pub layer: u32,
    pub card: u32,
    pub control: u32,
    pub control_hover: u32,
    pub border: u32,
    pub text: u32,
    pub muted: u32,
    pub canvas: u32,
}

fn reg_dword(key: PCWSTR, value: PCWSTR) -> Option<u32> {
    let mut v = 0u32;
    let mut size = 4u32;
    let r = unsafe {
        RegGetValueW(HKEY_CURRENT_USER, key, value, RRF_RT_REG_DWORD, None, Some(&mut v as *mut u32 as *mut _), Some(&mut size))
    };
    r.is_ok().then_some(v)
}

/// The system theme, read once per process.
pub fn theme() -> &'static Theme {
    static T: OnceLock<Theme> = OnceLock::new();
    T.get_or_init(|| {
        let light = reg_dword(w!("Software\\Microsoft\\Windows\\CurrentVersion\\Themes\\Personalize"), w!("AppsUseLightTheme")) == Some(1);
        // AccentColor is stored as 0xAABBGGRR.
        let accent = reg_dword(w!("Software\\Microsoft\\Windows\\DWM"), w!("AccentColor"))
            .map(|v| ((v & 0xFF) << 24) | (((v >> 8) & 0xFF) << 16) | (((v >> 16) & 0xFF) << 8) | 0xFF)
            .unwrap_or(0x0067C0FF);
        let (r, g, b) = ((accent >> 24) as f32, ((accent >> 16) & 0xFF) as f32, ((accent >> 8) & 0xFF) as f32);
        let on_accent = if (0.299 * r + 0.587 * g + 0.114 * b) / 255.0 > 0.6 { 0x000000FF } else { 0xFFFFFFFF };
        if light {
            Theme {
                dark: false,
                accent,
                on_accent,
                base: 0xF3F3F3D8,
                layer: 0xFBFBFBA8,
                card: 0xFFFFFFB8,
                control: 0xFFFFFFB3,
                control_hover: 0xF0F0F0F0,
                border: 0x0000001A,
                text: 0x1B1B1BFF,
                muted: 0x5F5F5FFF,
                canvas: 0xE4E4E4FF,
            }
        } else {
            Theme {
                dark: true,
                accent,
                on_accent,
                base: 0x202020D0,
                layer: 0x2C2C2CA0,
                card: 0xFFFFFF0D,
                control: 0xFFFFFF0F,
                control_hover: 0xFFFFFF1C,
                border: 0xFFFFFF17,
                text: 0xFFFFFFFF,
                muted: 0xFFFFFFA3,
                canvas: 0x141414FF,
            }
        }
    })
}

pub fn c(hex: u32) -> Rgba {
    rgba(hex)
}

/// Mica backdrop behind the window's translucent surfaces, with matching dark caption.
pub fn apply_mica(window: &Window) {
    use windows::Win32::Graphics::Dwm::{DwmSetWindowAttribute, DWMWA_SYSTEMBACKDROP_TYPE, DWMWA_USE_IMMERSIVE_DARK_MODE};
    let Some(hwnd) = crate::win::hwnd(window) else { return };
    let dark = theme().dark as i32;
    let main_window_backdrop: i32 = 2;
    unsafe {
        let _ = DwmSetWindowAttribute(hwnd, DWMWA_USE_IMMERSIVE_DARK_MODE, &dark as *const i32 as *const _, 4);
        let _ = DwmSetWindowAttribute(hwnd, DWMWA_SYSTEMBACKDROP_TYPE, &main_window_backdrop as *const i32 as *const _, 4);
    }
}

type Click = dyn Fn(&ClickEvent, &mut Window, &mut gpui::App) + 'static;

pub fn tool_button(
    id: impl Into<ElementId>,
    icon_name: &str,
    active: bool,
    tip: impl Into<SharedString>,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut gpui::App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let t = theme();
    div()
        .id(id.into())
        .size(px(32.))
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .rounded(px(4.))
        .cursor_pointer()
        .when(active, |d| d.bg(c(t.accent)))
        .when(!active, |d| d.hover(|s| s.bg(c(t.control_hover))))
        .child(icon(icon_name, 16., c(if active { t.on_accent } else { t.text })))
        .tooltip(tooltip(tip))
        .on_click(on_click)
}

pub fn button(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    icon_name: Option<&str>,
    primary: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut gpui::App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let t = theme();
    let on_click: Box<Click> = Box::new(on_click);
    div()
        .id(id.into())
        .h(px(30.))
        .px_3()
        .flex()
        .flex_none()
        .items_center()
        .justify_center()
        .gap_1p5()
        .rounded(px(4.))
        .cursor_pointer()
        .text_sm()
        .border_1()
        .when(primary, |d| d.bg(c(t.accent)).border_color(c(t.accent)).text_color(c(t.on_accent)).hover(|s| s.opacity(0.9)))
        .when(!primary, |d| d.bg(c(t.control)).border_color(c(t.border)).text_color(c(t.text)).hover(|s| s.bg(c(t.control_hover))))
        .when_some(icon_name, |d, name| d.child(icon(name, 14., c(if primary { t.on_accent } else { t.text }))))
        .child(label.into())
        .on_click(on_click)
}

pub fn chip(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    selected: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut gpui::App) + 'static,
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
        .rounded(px(4.))
        .text_xs()
        .cursor_pointer()
        .border_1()
        .when(selected, |d| d.bg(c(t.accent)).border_color(c(t.accent)).text_color(c(t.on_accent)))
        .when(!selected, |d| d.bg(c(t.control)).border_color(c(t.border)).text_color(c(t.text)).hover(|s| s.bg(c(t.control_hover))))
        .child(label.into())
        .on_click(on_click)
}

pub fn toggle(
    id: impl Into<ElementId>,
    label: impl Into<SharedString>,
    on: bool,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut gpui::App) + 'static,
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
        .text_color(c(t.text))
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
                .when(on, |d| d.bg(c(t.accent)).border_color(c(t.accent)).justify_end())
                .when(!on, |d| d.border_color(c(t.muted)).justify_start())
                .child(div().size(px(12.)).rounded_full().bg(c(if on { t.on_accent } else { t.muted }))),
        )
        .on_click(on_click)
}

/// A round color swatch; with two or more colors it shows a gradient.
pub fn swatch(
    id: impl Into<ElementId>,
    colors: &[Rgba],
    selected: bool,
    size: f32,
    on_click: impl Fn(&ClickEvent, &mut Window, &mut gpui::App) + 'static,
) -> gpui::Stateful<gpui::Div> {
    let t = theme();
    let inner = div().size_full().rounded(px(size * 0.22)).border_1().border_color(c(t.border));
    let inner = if colors.len() >= 2 {
        inner.bg(gpui::linear_gradient(
            135.,
            gpui::linear_color_stop(colors[0], 0.),
            gpui::linear_color_stop(colors[colors.len() - 1], 1.),
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
        .border_color(if selected { c(t.accent) } else { rgba(0) })
        .hover(|s| s.border_color(c(t.muted)))
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
        .rounded(px(8.))
        .bg(c(t.card))
        .border_1()
        .border_color(c(t.border))
        .child(div().text_sm().font_weight(gpui::FontWeight::SEMIBOLD).text_color(c(t.text)).child(title.to_string()))
}

thread_local! {
    static ACTIVE: Cell<Option<u64>> = const { Cell::new(None) };
}

/// A horizontal slider track. Keeps tracking the pointer outside its bounds while dragging.
pub fn slider_track(key: &str, value: f32, min: f32, max: f32, on_change: OnValue) -> gpui::Stateful<gpui::Div> {
    let t = theme();
    let id = hash(key);
    let frac = ((value - min) / (max - min)).clamp(0.0, 1.0);
    let bounds: Rc<Cell<Bounds<Pixels>>> = Rc::default();
    let set: Rc<dyn Fn(Point<Pixels>, &mut Window, &mut gpui::App)> = {
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
                ACTIVE.set(Some(id));
                set(e.position, window, cx);
            }
        })
        .child(
            div()
                .relative()
                .w_full()
                .h(px(4.))
                .rounded_full()
                .bg(c(t.border))
                .child(div().absolute().left_0().top_0().h_full().w(relative(frac)).rounded_full().bg(c(t.accent))),
        )
        .child(
            div()
                .absolute()
                .top(px(1.))
                .left(relative(frac))
                .ml(px(-9.))
                .size(px(18.))
                .rounded_full()
                .bg(c(if t.dark { 0x454545FF } else { 0xFFFFFFFF }))
                .border_1()
                .border_color(c(t.border))
                .flex()
                .items_center()
                .justify_center()
                .child(div().size(px(10.)).rounded_full().bg(c(t.accent))),
        )
        .child(
            canvas(
                move |b, _, _| bounds.set(b),
                move |_, _, window, _| {
                    window.on_mouse_event({
                        let set = set.clone();
                        move |e: &MouseMoveEvent, phase, window, cx| {
                            if phase == DispatchPhase::Bubble && ACTIVE.get() == Some(id) && e.pressed_button == Some(MouseButton::Left) {
                                set(e.position, window, cx);
                            }
                        }
                    });
                    window.on_mouse_event(|_: &MouseUpEvent, _, _, _| ACTIVE.set(None));
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
                .child(div().text_color(c(t.text)).child(label.to_string()))
                .child(div().text_color(c(t.muted)).child(display)),
        )
        .child(slider_track(key, value, min, max, on_change))
}
