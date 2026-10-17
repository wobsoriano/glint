use gpui::prelude::*;
use gpui::{
    point, px, size, AnyWindowHandle, App, Bounds, ClickEvent, Context, Timer, Window, WindowBackgroundAppearance, WindowBounds, WindowHandle, WindowKind,
    WindowOptions,
};
use std::time::Duration;
use windows::Win32::Graphics::Dwm::{
    DwmSetWindowAttribute, DWMWA_BORDER_COLOR, DWMWA_COLOR_NONE, DWMWA_WINDOW_CORNER_PREFERENCE, DWMWCP_DONOTROUND,
};
use windows::Win32::UI::WindowsAndMessaging::{
    GetWindowLongPtrW, SetLayeredWindowAttributes, SetWindowDisplayAffinity, SetWindowLongPtrW, GWL_EXSTYLE, LWA_ALPHA,
    WDA_EXCLUDEFROMCAPTURE, WS_EX_LAYERED, WS_EX_NOACTIVATE, WS_EX_TRANSPARENT,
};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Kind {
    Focused,
    Passive,
    DuringRecording,
    ClickThrough,
}

pub struct Placement {
    pub rect: [i32; 4],
    pub scale: f32,
    pub kind: Kind,
}

pub fn open<V: Render + 'static>(
    placement: Placement,
    cx: &mut App,
    build: impl FnOnce(&mut Window, &mut Context<V>) -> V + 'static,
) -> Option<WindowHandle<V>> {
    let Placement { rect, scale, kind } = placement;
    let focus = kind == Kind::Focused;
    let logical = size(px(rect[2] as f32 / scale), px(rect[3] as f32 / scale));
    let handle = cx
        .open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(Bounds { origin: point(px(0.), px(0.)), size: logical })),
                titlebar: None,
                kind: WindowKind::PopUp,
                focus,
                // Showing through GPUI activates the window; `place` shows the others without that.
                show: focus,
                is_movable: false,
                is_resizable: false,
                window_background: WindowBackgroundAppearance::Transparent,
                ..Default::default()
            },
            move |window, cx| {
                style(window, kind);
                crate::shell::place(window, rect, cx);
                if focus {
                    window.activate_window();
                }
                cx.new(|cx| build(window, cx))
            },
        )
        .ok()?;
    if matches!(kind, Kind::DuringRecording | Kind::ClickThrough) {
        let any: AnyWindowHandle = handle.into();
        cx.spawn(async move |cx| {
            Timer::after(Duration::from_millis(150)).await;
            let _ = any.update(cx, |_, window, _| style(window, kind));
        })
        .detach();
    }
    Some(handle)
}

pub fn tick(handle: AnyWindowHandle, ms: u64, cx: &mut App) {
    cx.spawn(async move |cx| loop {
        Timer::after(Duration::from_millis(ms)).await;
        if handle.update(cx, |_, window, _| window.refresh()).is_err() {
            break;
        }
    })
    .detach();
}

pub fn place(window: &Window, rect: [i32; 4], cx: &App) {
    crate::shell::place(window, rect, cx);
}

pub fn close(handle: Option<AnyWindowHandle>, cx: &mut App) {
    if let Some(h) = handle {
        let _ = h.update(cx, |_, window, _| window.remove_window());
    }
}

pub fn defer_past_teardown(f: impl Fn(&mut App) + Clone + 'static) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    move |_, _, cx| cx.defer(f.clone())
}

fn capturable() -> bool {
    std::env::var_os("GLINT_CAPTURE_CHROME").is_some()
}

fn style(window: &Window, kind: Kind) {
    let Some(h) = crate::win::hwnd(window) else { return };
    unsafe {
        // Windows 11 frames every popup with a rounded 1 px border; the chrome draws its own.
        let none = DWMWA_COLOR_NONE;
        let square = DWMWCP_DONOTROUND;
        let _ = DwmSetWindowAttribute(h, DWMWA_BORDER_COLOR, &none as *const _ as *const _, 4);
        let _ = DwmSetWindowAttribute(h, DWMWA_WINDOW_CORNER_PREFERENCE, &square as *const _ as *const _, 4);
        if matches!(kind, Kind::DuringRecording | Kind::ClickThrough) && !capturable() {
            let _ = SetWindowDisplayAffinity(h, WDA_EXCLUDEFROMCAPTURE);
        }
        // Layered, or the transparent parts of the window show a pale tint. Only the picker
        // takes focus: the rest must not pull it from the picker (Esc) or from the app being
        // recorded.
        let ex = GetWindowLongPtrW(h, GWL_EXSTYLE);
        let want = ex
            | WS_EX_LAYERED.0 as isize
            | match kind {
                Kind::Focused => 0,
                Kind::Passive | Kind::DuringRecording => WS_EX_NOACTIVATE.0 as isize,
                Kind::ClickThrough => (WS_EX_NOACTIVATE.0 | WS_EX_TRANSPARENT.0) as isize,
            };
        if want != ex {
            SetWindowLongPtrW(h, GWL_EXSTYLE, want);
            let _ = SetLayeredWindowAttributes(h, windows::Win32::Foundation::COLORREF(0), 255, LWA_ALPHA);
        }
    }
}
