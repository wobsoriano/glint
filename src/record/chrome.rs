use gpui::prelude::*;
use gpui::{
    point, px, size, AnyWindowHandle, App, Bounds, ClickEvent, Context, Timer, Window, WindowBackgroundAppearance, WindowBounds, WindowHandle, WindowKind,
    WindowOptions,
};
use std::time::Duration;

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
                crate::sys::chrome::style(window, kind);
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
            let _ = any.update(cx, |_, window, _| crate::sys::chrome::style(window, kind));
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

pub fn close(handle: Option<AnyWindowHandle>, cx: &mut App) {
    if let Some(h) = handle {
        let _ = h.update(cx, |_, window, _| window.remove_window());
    }
}

pub fn defer_past_teardown(f: impl Fn(&mut App) + Clone + 'static) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    move |_, _, cx| cx.defer(f.clone())
}

/// Set `GLINT_CAPTURE_CHROME` to let screenshots and recordings see the recording chrome.
pub(crate) fn capturable() -> bool {
    std::env::var_os("GLINT_CAPTURE_CHROME").is_some()
}
