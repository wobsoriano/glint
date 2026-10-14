//! The floating thumbnail shown after a capture, and transient toast notifications.

use crate::annotate::Annotation;
use crate::capture::Capture;
use crate::settings::Settings;
use crate::ui::{self, BORDER, FONT, TEXT};
use crate::{editor, pin, win};
use gpui::prelude::*;
use gpui::{
    div, img, point, px, rgb, rgba, size, AnyWindowHandle, App, Bounds, Context, Global,
    ImageSource, ObjectFit, RenderImage, SharedString, Timer, Window, WindowBounds, WindowHandle,
    WindowKind, WindowOptions,
};
use std::sync::Arc;
use std::time::{Duration, Instant};

#[derive(Default)]
struct QuickState {
    window: Option<WindowHandle<QuickAccess>>,
    toast: Option<AnyWindowHandle>,
}

impl Global for QuickState {}

pub struct QuickAccess {
    cap: Capture,
    redactions: Vec<Annotation>,
    thumb: Arc<RenderImage>,
    hovered: bool,
    deadline: Instant,
    status: Option<SharedString>,
}

fn bottom_left(w: f32, h: f32, cx: &App) -> Bounds<gpui::Pixels> {
    let display = cx.primary_display();
    let b = display.map(|d| d.bounds()).unwrap_or(Bounds {
        origin: point(px(0.), px(0.)),
        size: size(px(1280.), px(800.)),
    });
    Bounds {
        origin: point(b.origin.x + px(20.), b.origin.y + b.size.height - px(h + 64.)),
        size: size(px(w), px(h)),
    }
}

pub fn close(cx: &mut App) {
    if let Some(h) = cx.default_global::<QuickState>().window.take() {
        let _ = h.update(cx, |_, window, _| window.remove_window());
    }
}

pub fn show(cap: Capture, redactions: Vec<Annotation>, cx: &mut App) {
    close(cx);
    let (iw, ih) = (cap.image.width() as f32, cap.image.height() as f32);
    let w = 260.;
    let th = (w - 16.) * (ih / iw).clamp(0.3, 1.2);
    let h = th + 16.;
    let thumb = {
        let small = image::imageops::thumbnail(cap.image.as_ref(), 520, (520. * ih / iw) as u32);
        let flat = editor::flatten_redactions(
            &Capture::new(small.clone(), cap.dpi, None),
            &scale_annotations(&redactions, small.width() as f32 / iw),
        );
        win::render_image(&flat)
    };
    let seconds = cx.global::<Settings>().quick_access_seconds.max(2) as u64;
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bottom_left(w, h, cx))),
            titlebar: None,
            kind: WindowKind::PopUp,
            focus: false,
            is_movable: false,
            is_resizable: false,
            window_background: gpui::WindowBackgroundAppearance::Transparent,
            ..Default::default()
        },
        move |window, cx| {
            win::make_topmost(window, None, false);
            cx.new(|_| QuickAccess {
                cap,
                redactions,
                thumb,
                hovered: false,
                deadline: Instant::now() + Duration::from_secs(seconds),
                status: None,
            })
        },
    );
    if let Ok(h) = handle {
        cx.default_global::<QuickState>().window = Some(h);
        // Auto-dismiss unless the pointer is over it.
        cx.spawn(async move |cx| loop {
            Timer::after(Duration::from_millis(250)).await;
            let done = h.update(cx, |this, window, _| {
                if !this.hovered && Instant::now() > this.deadline {
                    window.remove_window();
                    true
                } else {
                    false
                }
            });
            if done.unwrap_or(true) {
                break;
            }
        })
        .detach();
    }
}

fn scale_annotations(a: &[Annotation], k: f32) -> Vec<Annotation> {
    a.iter()
        .cloned()
        .map(|mut a| {
            if let crate::annotate::Shape::Redact { rect, .. } = &mut a.shape {
                rect.x *= k;
                rect.y *= k;
                rect.w *= k;
                rect.h *= k;
            }
            a
        })
        .collect()
}

impl QuickAccess {
    fn flat(&self) -> image::RgbaImage {
        editor::flatten_redactions(&self.cap, &self.redactions)
    }

    fn flash(&mut self, msg: &str, cx: &mut Context<Self>) {
        self.status = Some(msg.to_string().into());
        self.deadline = Instant::now() + Duration::from_millis(1400);
        cx.notify();
    }
}

fn action(id: &'static str, icon: &'static str, label: &'static str) -> gpui::Stateful<gpui::Div> {
    div()
        .id(id)
        .flex()
        .flex_col()
        .items_center()
        .gap_1()
        .px_2()
        .py_1p5()
        .rounded_md()
        .cursor_pointer()
        .bg(rgba(0x1C1C1FE6))
        .border_1()
        .border_color(rgba(0xFFFFFF26))
        .hover(|s| s.bg(rgba(0x7C8CFFE6)))
        .text_xs()
        .text_color(rgb(0xFFFFFF))
        .child(ui::icon(icon, 15., rgb(0xFFFFFF)))
        .child(label)
}

impl Render for QuickAccess {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let hovered = self.hovered;
        div()
            .id("quick")
            .size_full()
            .p(px(8.))
            .font_family(FONT)
            .on_hover(cx.listener(|this, h: &bool, _, cx| {
                this.hovered = *h;
                if !*h {
                    this.deadline = this.deadline.max(Instant::now() + Duration::from_secs(2));
                }
                cx.notify();
            }))
            .child(
                div()
                    .relative()
                    .size_full()
                    .rounded_xl()
                    .overflow_hidden()
                    .bg(rgb(0x111113))
                    .border_1()
                    .border_color(rgb(BORDER))
                    .shadow_lg()
                    .child(
                        img(ImageSource::Render(self.thumb.clone()))
                            .size_full()
                            .object_fit(ObjectFit::Cover),
                    )
                    .when(hovered && self.status.is_none(), |d| {
                        d.child(
                            div()
                                .absolute()
                                .inset_0()
                                .bg(rgba(0x0000008C))
                                .flex()
                                .flex_col()
                                .justify_center()
                                .items_center()
                                .gap_2()
                                .child(
                                    div()
                                        .flex()
                                        .gap_1p5()
                                        .child(action("q-copy", "copy", "Copy").on_click(cx.listener(
                                            |this, _, _, cx| {
                                                let _ = win::copy_image(&this.flat());
                                                this.flash("Copied", cx);
                                            },
                                        )))
                                        .child(action("q-save", "save", "Save").on_click(cx.listener(
                                            |this, _, _, cx| {
                                                let path = cx
                                                    .global::<Settings>()
                                                    .save_dir
                                                    .join(win::file_name(&this.cap.created));
                                                match win::save_png(&this.flat(), &path) {
                                                    Ok(()) => this.flash("Saved", cx),
                                                    Err(_) => this.flash("Save failed", cx),
                                                }
                                            },
                                        )))
                                        .child(action("q-edit", "edit", "Edit").on_click(cx.listener(
                                            |this, _, window, cx| {
                                                editor::open_with(this.cap.clone(), this.redactions.clone(), cx);
                                                window.remove_window();
                                            },
                                        )))
                                        .child(action("q-pin", "pin", "Pin").on_click(cx.listener(
                                            |this, _, window, cx| {
                                                pin::open(Arc::new(this.flat()), this.cap.dpi, cx);
                                                window.remove_window();
                                            },
                                        ))),
                                ),
                        )
                        .child(
                            div()
                                .id("q-close")
                                .absolute()
                                .top(px(6.))
                                .left(px(6.))
                                .size(px(22.))
                                .rounded_full()
                                .flex()
                                .items_center()
                                .justify_center()
                                .cursor_pointer()
                                .bg(rgba(0x000000B3))
                                .hover(|s| s.bg(rgba(0xFF3B30E6)))
                                .child(ui::icon("close", 12., rgb(0xFFFFFF)))
                                .on_click(|_, window, _| window.remove_window()),
                        )
                    })
                    .when_some(self.status.clone(), |d, msg| {
                        d.child(
                            div()
                                .absolute()
                                .inset_0()
                                .bg(rgba(0x000000A6))
                                .flex()
                                .items_center()
                                .justify_center()
                                .gap_2()
                                .text_color(rgb(TEXT))
                                .child(ui::icon("check", 18., rgb(0x34C759)))
                                .child(msg),
                        )
                    }),
            )
    }
}

// ---------------------------------------------------------------------------------------------

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

pub fn show_toast(msg: impl Into<SharedString>, cx: &mut App) {
    if let Some(h) = cx.default_global::<QuickState>().toast.take() {
        let _ = h.update(cx, |_, window, _| window.remove_window());
    }
    let msg = msg.into();
    let w = (msg.len() as f32 * 7.2 + 72.).clamp(200., 560.);
    let h = 50.;
    let display = cx.primary_display();
    let b = display.map(|d| d.bounds()).unwrap_or(Bounds {
        origin: point(px(0.), px(0.)),
        size: size(px(1280.), px(800.)),
    });
    let bounds = Bounds {
        origin: point(
            b.origin.x + (b.size.width - px(w)) / 2.,
            b.origin.y + b.size.height - px(h + 72.),
        ),
        size: size(px(w), px(h)),
    };
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: None,
            kind: WindowKind::PopUp,
            focus: false,
            is_movable: false,
            is_resizable: false,
            window_background: gpui::WindowBackgroundAppearance::Transparent,
            ..Default::default()
        },
        move |window, cx| {
            win::make_topmost(window, None, false);
            cx.new(|_| Toast(msg))
        },
    );
    if let Ok(h) = handle {
        let any: AnyWindowHandle = h.into();
        cx.default_global::<QuickState>().toast = Some(any);
        cx.spawn(async move |cx| {
            Timer::after(Duration::from_millis(2600)).await;
            let _ = any.update(cx, |_, window, _| window.remove_window());
        })
        .detach();
    }
}
