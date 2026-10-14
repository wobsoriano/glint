//! Transient toast notifications at the bottom of the active display.

use crate::ui::{self, FONT, TEXT};
use crate::win;
use gpui::prelude::*;
use gpui::{
    div, point, px, rgb, rgba, size, AnyWindowHandle, App, Bounds, Context, Global, SharedString, Timer, Window,
    WindowBounds, WindowKind, WindowOptions,
};
use std::time::Duration;

#[derive(Default)]
struct ToastState(Option<AnyWindowHandle>);

impl Global for ToastState {}

struct Toast(SharedString);

impl Render for Toast {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        div().size_full().p(px(6.)).font_family(FONT).child(
            div()
                .size_full()
                .flex()
                .items_center()
                .gap_2()
                .px_4()
                .rounded_full()
                .bg(rgba(0x1C1C1FF2))
                .border_1()
                .border_color(rgba(0xFFFFFF1F))
                .shadow_lg()
                .text_sm()
                .text_color(rgb(TEXT))
                .child(ui::icon("sparkles", 15., rgb(ui::ACCENT)))
                .child(div().overflow_hidden().whitespace_nowrap().text_ellipsis().child(self.0.clone())),
        )
    }
}

pub fn show(msg: impl Into<SharedString>, cx: &mut App) {
    if let Some(h) = cx.default_global::<ToastState>().0.take() {
        let _ = h.update(cx, |_, window, _| window.remove_window());
    }
    let msg = msg.into();
    let w = (msg.len() as f32 * 7.2 + 72.).clamp(200., 620.);
    let h = 50.;
    let (area, scale) = crate::shell::work_area_at_cursor();
    let (pw, ph) = ((w * scale) as i32, (h * scale) as i32);
    let rect = [area[0] + (area[2] - pw) / 2, area[1] + area[3] - ph - (24. * scale) as i32, pw, ph];
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(Bounds { origin: point(px(0.), px(0.)), size: size(px(w), px(h)) })),
            titlebar: None,
            kind: WindowKind::PopUp,
            focus: false,
            is_movable: false,
            is_resizable: false,
            window_background: gpui::WindowBackgroundAppearance::Transparent,
            ..Default::default()
        },
        move |window, cx| {
            win::make_topmost(window, Some(rect), false);
            cx.new(|_| Toast(msg))
        },
    );
    if let Ok(h) = handle {
        let any: AnyWindowHandle = h.into();
        cx.default_global::<ToastState>().0 = Some(any);
        cx.spawn(async move |cx| {
            Timer::after(Duration::from_millis(2600)).await;
            let _ = any.update(cx, |_, window, _| window.remove_window());
        })
        .detach();
    }
}
