use gpui::prelude::*;
use gpui::{
    canvas, div, linear_color_stop, linear_gradient, px, relative, rgba, svg, AnyView, App, Bounds, ClickEvent,
    DispatchPhase, ElementId, Hsla, MouseButton, MouseMoveEvent, MouseUpEvent, Pixels, Point, Rgba, SharedString,
    Window,
};
use std::cell::Cell;
use std::rc::Rc;
use std::sync::{LazyLock, RwLock};

#[cfg(windows)]
pub const FONT: &str = "Segoe UI Variable Text";
/// The system font (San Francisco), as CoreText names it.
#[cfg(target_os = "macos")]
pub const FONT: &str = ".SystemUIFont";

/// The system draws the window controls (the macOS traffic lights), so custom title bars leave
/// them out and keep this much room for them at the left.
pub const SYSTEM_CAPTION_INSET: Option<f32> = if cfg!(target_os = "macos") { Some(86.) } else { None };

pub const RADIUS_WINDOW: f32 = 8.;
pub const RADIUS_CONTROL: f32 = 4.;

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Theme {
    pub dark: bool,
    pub accent: Rgba,
    pub on_accent: Rgba,
    pub base: Rgba,
    pub layer: Rgba,
    pub card: Rgba,
    pub control: Rgba,
    pub control_hover: Rgba,
    pub border: Rgba,
    pub text: Rgba,
    pub muted: Rgba,
    pub canvas: Rgba,
    pub field: Rgba,
    pub flyout: Rgba,
}

pub(crate) struct Appearance {
    pub light: bool,
    /// The user's accent color as RGB, when the system has one.
    pub accent: Option<[u8; 3]>,
    /// The system draws a backdrop behind the window (Mica on Windows), so the window's own
    /// surfaces stay translucent.
    pub backdrop: bool,
}

fn read_theme() -> Theme {
    let Appearance { light, accent, backdrop } = crate::sys::ui::appearance();
    let (mut r, mut g, mut b) = accent.map(|[r, g, b]| (r as u32, g as u32, b as u32)).unwrap_or((0x00, 0x67, 0xC0));
    if !light && r.max(g).max(b) < 0x90 {
        let lift = |c: u32| c + (0xFF - c) / 3;
        (r, g, b) = (lift(r), lift(g), lift(b));
    }
    let accent = (r << 24) | (g << 16) | (b << 8) | 0xFF;
    let luma = (0.299 * r as f32 + 0.587 * g as f32 + 0.114 * b as f32) / 255.0;
    let on_accent = if luma > 0.6 { 0x000000FF } else { 0xFFFFFFFF };
    let theme = if light {
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
    };
    if backdrop {
        return theme;
    }
    let base = Rgba { a: 1., ..theme.base };
    Theme { base, layer: over(theme.layer, base), ..theme }
}

/// `top` composited over the opaque `bottom`.
fn over(top: Rgba, bottom: Rgba) -> Rgba {
    let mix = |t: f32, b: f32| t * top.a + b * (1. - top.a);
    Rgba { r: mix(top.r, bottom.r), g: mix(top.g, bottom.g), b: mix(top.b, bottom.b), a: 1. }
}

static THEME: LazyLock<RwLock<Theme>> = LazyLock::new(|| RwLock::new(read_theme()));

pub fn theme() -> Theme {
    *THEME.read().unwrap()
}

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

/// Shortcut text as the platform writes it. In-app shortcuts are written with `Ctrl`, which
/// stands for the secondary modifier: Ctrl on Windows, ⌘ on macOS (`Undo (Ctrl+Z)` reads
/// `Undo (⌘Z)` there).
pub fn keys(text: &str) -> String {
    if cfg!(target_os = "macos") {
        mac_chords(text, "⌘")
    } else {
        text.to_string()
    }
}

/// Rewrites each `Mod+Mod+Key` chord in `text` with Mac symbols in the Mac order (⌃⌥⇧⌘), with
/// `ctrl` as the symbol for `Ctrl`. A word key keeps a space: `Ctrl+scroll` becomes `⌘ scroll`.
pub fn mac_chords(text: &str, ctrl: &str) -> String {
    static CHORD: LazyLock<regex::Regex> =
        LazyLock::new(|| regex::Regex::new(r"\b((?:(?:Ctrl|Alt|Shift|Super|Win|Cmd)\+)+)([^\s)]+)").unwrap());
    CHORD
        .replace_all(text, |c: &regex::Captures| {
            let mods: Vec<&str> = c[1].split('+').collect();
            let has = |m: &[&str]| mods.iter().any(|x| m.contains(x));
            let mut out = String::new();
            for (present, symbol) in [
                (has(&["Ctrl"]) && ctrl == "⌃", "⌃"),
                (has(&["Alt"]), "⌥"),
                (has(&["Shift"]), "⇧"),
                (has(&["Super", "Win", "Cmd"]) || (has(&["Ctrl"]) && ctrl == "⌘"), "⌘"),
            ] {
                if present {
                    out.push_str(symbol);
                }
            }
            let key = &c[2];
            if key.len() > 1 && key.chars().all(|ch| ch.is_ascii_lowercase()) {
                out.push(' ');
            }
            out + key
        })
        .into_owned()
}

pub fn tooltip(text: impl Into<SharedString>) -> impl Fn(&mut Window, &mut App) -> AnyView + 'static {
    let text: SharedString = keys(&text.into()).into();
    move |_, cx| cx.new(|_| Tip(text.clone())).into()
}

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

pub fn edit_text(text: &mut String, e: &gpui::KeyDownEvent) -> Edit {
    let k = &e.keystroke;
    let ctrl = k.modifiers.secondary();
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

#[cfg(test)]
mod tests {
    use super::mac_chords;

    #[test]
    fn chords_become_mac_symbols_in_mac_order() {
        assert_eq!(mac_chords("Undo (Ctrl+Z)", "⌘"), "Undo (⌘Z)");
        assert_eq!(mac_chords("Smart redaction (Ctrl+Shift+R)", "⌘"), "Smart redaction (⇧⌘R)");
        assert_eq!(mac_chords("Inspector (Ctrl+Alt+I)", "⌘"), "Inspector (⌥⌘I)");
        assert_eq!(mac_chords("Zoom out (Ctrl+-)", "⌘"), "Zoom out (⌘-)");
        assert_eq!(mac_chords("Ctrl+scroll to zoom", "⌘"), "⌘ scroll to zoom");
        assert_eq!(mac_chords("Ctrl+Shift+4", "⌃"), "⌃⇧4");
        assert_eq!(mac_chords("Ctrl+Alt+Shift+Super+F1", "⌃"), "⌃⌥⇧⌘F1");
        assert_eq!(mac_chords("No chords here, Ctrl alone.", "⌘"), "No chords here, Ctrl alone.");
    }
}
