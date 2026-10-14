//! Quick Look: a large preview of one capture. Space or Esc closes it.

use crate::library::{CaptureKind, Item};
use crate::ui::{self, BG, BORDER, FONT, MUTED, PANEL, TEXT};
use crate::{actions, shell, win};
use gpui::prelude::*;
use gpui::{
    div, img, px, rgb, size, App, Bounds, Context, FocusHandle, Global, ImageSource, KeyDownEvent, ObjectFit,
    RenderImage, TitlebarOptions, Window, WindowBounds, WindowHandle, WindowOptions,
};
use std::sync::Arc;

#[derive(Default)]
struct QuickLookState(Option<WindowHandle<QuickLook>>);

impl Global for QuickLookState {}

pub struct QuickLook {
    item: Item,
    image: Option<Arc<RenderImage>>,
    error: Option<String>,
    focus: FocusHandle,
}

/// Shows `item`, reusing the open Quick Look window if there is one.
pub fn open(item: Item, cx: &mut App) {
    if let Some(h) = cx.default_global::<QuickLookState>().0 {
        let reused = h.update(cx, |this, window, cx| {
            this.show(item.clone(), cx);
            window.activate_window();
        });
        if reused.is_ok() {
            return;
        }
    }
    let (iw, ih) = (item.meta.width.max(1) as f32, item.meta.height.max(1) as f32);
    let display = cx.primary_display().map(|d| d.bounds().size);
    let (mw, mh) = display.map(|s| (f32::from(s.width) * 0.75, f32::from(s.height) * 0.75)).unwrap_or((1200., 800.));
    let k = ((mw - 40.) / iw).min((mh - 100.) / ih).min(1.0);
    let (w, h) = ((iw * k + 40.).max(520.), (ih * k + 100.).max(360.));
    let bounds = Bounds::centered(None, size(px(w), px(h)), cx);
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions { title: Some(item.meta.title.clone().into()), appears_transparent: false, traffic_light_position: None }),
            focus: true,
            ..Default::default()
        },
        move |window, cx| {
            let view = cx.new(|cx| {
                let mut q = QuickLook { item: item.clone(), image: None, error: None, focus: cx.focus_handle() };
                q.show(item, cx);
                q
            });
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            win::bring_to_front(window);
            view
        },
    );
    cx.default_global::<QuickLookState>().0 = handle.ok();
}

pub fn close(cx: &mut App) -> bool {
    let Some(h) = cx.default_global::<QuickLookState>().0.take() else { return false };
    h.update(cx, |_, window, _| window.remove_window()).is_ok()
}

impl QuickLook {
    fn show(&mut self, item: Item, cx: &mut Context<Self>) {
        self.item = item.clone();
        self.image = None;
        self.error = None;
        let path = match item.kind() {
            CaptureKind::Screenshot => item.deliverable(),
            CaptureKind::Recording => item.thumb(),
        };
        let task = cx.background_spawn(async move { image::open(&path).map(|i| i.to_rgba8()) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                match result {
                    Ok(img) => this.image = Some(win::render_image(&img)),
                    Err(e) => this.error = Some(e.to_string()),
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }
}

impl Render for QuickLook {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        window.set_window_title(&self.item.meta.title);
        let item = self.item.clone();
        let recording = item.kind() == CaptureKind::Recording;
        let info = match item.meta.duration_ms {
            Some(ms) => format!("{} × {} · {}:{:02}", item.meta.width, item.meta.height, ms / 60_000, (ms / 1000) % 60),
            None => format!("{} × {}", item.meta.width, item.meta.height),
        };
        let body = match (&self.image, &self.error) {
            (Some(image), _) => img(ImageSource::Render(image.clone())).size_full().object_fit(ObjectFit::Contain).into_any_element(),
            (None, Some(e)) => div().text_color(rgb(MUTED)).child(e.clone()).into_any_element(),
            (None, None) => div().text_color(rgb(MUTED)).child("Loading…").into_any_element(),
        };
        let (edit_item, copy_item, reveal_item) = (item.clone(), item.clone(), item.clone());
        div()
            .id("quicklook")
            .track_focus(&self.focus)
            .size_full()
            .flex()
            .flex_col()
            .bg(rgb(BG))
            .font_family(FONT)
            .text_color(rgb(TEXT))
            .on_key_down(cx.listener(|_, e: &KeyDownEvent, window, _| {
                if matches!(e.keystroke.key.as_str(), "escape" | "space") {
                    window.remove_window();
                }
            }))
            .child(div().flex_1().min_h_0().p_4().flex().items_center().justify_center().child(body))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .px_4()
                    .py_2()
                    .bg(rgb(PANEL))
                    .border_t_1()
                    .border_color(rgb(BORDER))
                    .child(div().flex_1().text_sm().text_color(rgb(MUTED)).child(info))
                    .when(recording, |d| d.child(div().text_xs().text_color(rgb(MUTED)).child("Playback arrives with Studio")))
                    .child(ui::button("ql-reveal", "Show in Explorer", Some("folder"), false, move |_, _, _| {
                        shell::reveal(std::slice::from_ref(&reveal_item.dir))
                    }))
                    .child(ui::button("ql-copy", "Copy", Some("copy"), false, move |_, _, cx| actions::copy(&copy_item, cx)))
                    .child(ui::button("ql-edit", "Edit", Some("edit"), true, move |_, window, cx| {
                        actions::edit(&edit_item, cx);
                        if edit_item.kind() == CaptureKind::Screenshot {
                            window.remove_window();
                        }
                    })),
            )
    }
}
