//! Pins a screenshot as an always-on-top floating window.
//! Drag to move, scroll to zoom, Ctrl+scroll for opacity, double-click or Esc to close.

use crate::ui::{self, FONT};
use crate::win;
use gpui::prelude::*;
use gpui::{
    div, img, point, px, rgb, rgba, size, App, Bounds, Context, FocusHandle, ImageSource,
    KeyDownEvent, MouseButton, ObjectFit, RenderImage, ScrollWheelEvent, Window, WindowBounds,
    WindowKind, WindowOptions,
};
use std::sync::Arc;

pub struct Pin {
    image: Arc<RenderImage>,
    raw: Arc<image::RgbaImage>,
    aspect: f32,
    opacity: f32,
    focus: FocusHandle,
}

pub fn open(raw: Arc<image::RgbaImage>, dpi: f32, cx: &mut App) {
    let (iw, ih) = (raw.width() as f32 / dpi, raw.height() as f32 / dpi);
    let display = cx.primary_display().map(|d| d.bounds());
    let (mw, mh) = display
        .map(|b| (f32::from(b.size.width) * 0.6, f32::from(b.size.height) * 0.6))
        .unwrap_or((900., 600.));
    let k = (mw / iw).min(mh / ih).min(1.0);
    let (w, h) = (iw * k, ih * k);
    let origin = display
        .map(|b| b.center() - point(px(w / 2.), px(h / 2.)))
        .unwrap_or(point(px(200.), px(200.)));
    let image = win::render_image(&raw);
    let _ = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(Bounds {
                origin,
                size: size(px(w), px(h)),
            })),
            titlebar: None,
            kind: WindowKind::PopUp,
            focus: true,
            is_movable: true,
            is_resizable: false,
            ..Default::default()
        },
        move |window, cx| {
            win::make_topmost(window, None, true);
            let view = cx.new(|cx| Pin {
                image,
                aspect: iw / ih,
                raw,
                opacity: 1.0,
                focus: cx.focus_handle(),
            });
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            view
        },
    );
}

impl Pin {
    fn on_scroll(&mut self, e: &ScrollWheelEvent, window: &mut Window, cx: &mut Context<Self>) {
        let dy = f32::from(e.delta.pixel_delta(px(20.)).y);
        if dy == 0.0 {
            return;
        }
        if e.modifiers.control {
            self.opacity = (self.opacity + dy.signum() * 0.08).clamp(0.2, 1.0);
            cx.notify();
            return;
        }
        let cur = window.bounds().size;
        let k = if dy > 0.0 { 1.08 } else { 1.0 / 1.08 };
        let w = (f32::from(cur.width) * k).clamp(80., 4000.);
        window.resize(size(px(w), px(w / self.aspect)));
    }
}

impl Render for Pin {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        div()
            .id("pin")
            .group("pin")
            .track_focus(&self.focus)
            .size_full()
            .relative()
            .font_family(FONT)
            .bg(rgb(0x000000))
            .border_1()
            .border_color(rgba(0x7C8CFF99))
            .opacity(self.opacity)
            .on_mouse_down(MouseButton::Left, |e, window, _| {
                if e.click_count >= 2 {
                    window.remove_window();
                } else {
                    window.start_window_move();
                }
            })
            .on_scroll_wheel(cx.listener(Self::on_scroll))
            .on_key_down(cx.listener(|this, e: &KeyDownEvent, window, _| {
                let k = &e.keystroke;
                if k.key == "escape" {
                    window.remove_window();
                } else if k.key == "c" && k.modifiers.control {
                    let _ = win::copy_image(&this.raw);
                }
            }))
            .child(
                img(ImageSource::Render(self.image.clone()))
                    .size_full()
                    .object_fit(ObjectFit::Fill),
            )
            .child(
                div()
                    .id("pin-close")
                    .absolute()
                    .top(px(6.))
                    .right(px(6.))
                    .size(px(22.))
                    .rounded_full()
                    .flex()
                    .items_center()
                    .justify_center()
                    .cursor_pointer()
                    .bg(rgba(0x000000B3))
                    .invisible()
                    .group_hover("pin", |s| s.visible())
                    .hover(|s| s.bg(rgba(0xFF3B30E6)))
                    .child(ui::icon("close", 12., rgb(0xFFFFFF)))
                    .on_mouse_down(MouseButton::Left, |_, window, cx| {
                        cx.stop_propagation();
                        window.remove_window();
                    }),
            )
    }
}
