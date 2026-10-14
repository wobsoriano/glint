//! The preview: the player's latest frame fit to the available space, with the camera
//! bubble handle, the crop editor and the zoom focus pad drawn over it.

use super::geometry::{bubble_rect, card_rect, crop_for_ratio, crop_from_drag, CropRatio, Rect};
use super::{Drag, Mode, Notice, Studio, Tab};
use crate::shell;
use crate::ui::{self, BORDER, MUTED, TEXT};
use glint_studio::{CropRect, ZoomFocus};
use gpui::prelude::*;
use gpui::{
    canvas, div, img, px, rgb, rgba, Context, ImageSource, MouseButton, MouseDownEvent, ObjectFit, Pixels, Point,
    ScrollWheelEvent, Window,
};

const HANDLE: f32 = 12.0;

impl Studio {
    /// The displayed frame's size in logical pixels.
    fn frame_size(&self) -> (f32, f32) {
        let b = self.frame_bounds.get();
        (f32::from(b.size.width), f32::from(b.size.height))
    }

    fn local(&self, pos: Point<Pixels>) -> (f32, f32) {
        let b = self.frame_bounds.get();
        (f32::from(pos.x - b.origin.x), f32::from(pos.y - b.origin.y))
    }

    fn has_camera(&self) -> bool {
        self.session.tracks.camera.is_some() && self.project().design.camera.visible
    }

    /// The screen card on the displayed frame while cropping or setting a focus point.
    fn card(&self) -> Rect {
        card_rect(&self.preview_project(), self.session.source_size, self.frame_size())
    }

    /// A frame point as a normalized point inside the card.
    fn in_card(&self, local: (f32, f32)) -> (f32, f32) {
        let c = self.card();
        (((local.0 - c.x) / c.w).clamp(0.0, 1.0), ((local.1 - c.y) / c.h).clamp(0.0, 1.0))
    }

    fn crop_ratio(&self) -> Option<f32> {
        match self.mode {
            Mode::Crop { ratio, .. } => ratio.value(self.session.source_size),
            _ => None,
        }
    }

    pub(super) fn enter_crop(&mut self, cx: &mut Context<Self>) {
        let before = self.project().crop;
        if self.player.is_playing() {
            self.toggle_play(cx);
        }
        self.set_mode(Mode::Crop { ratio: CropRatio::Freeform, before }, cx);
    }

    fn set_crop_ratio(&mut self, ratio: CropRatio, cx: &mut Context<Self>) {
        if let Mode::Crop { before, .. } = self.mode {
            self.mode = Mode::Crop { ratio, before };
        }
        let src = self.session.source_size;
        self.edit(Some("crop"), cx, |p| p.crop = crop_for_ratio(p.crop, ratio.value(src), src));
    }

    pub(super) fn enter_focus(&mut self, zoom: usize, cx: &mut Context<Self>) {
        if self.player.is_playing() {
            self.toggle_play(cx);
        }
        self.edit(None, cx, |p| {
            if p.zooms[zoom].focus == ZoomFocus::Pointer {
                p.zooms[zoom].focus = ZoomFocus::Fixed { x: 0.5, y: 0.5 };
            }
        });
        self.set_mode(Mode::Focus { zoom }, cx);
    }

    fn preview_mouse_down(&mut self, e: &MouseDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        self.focus_self(cx);
        self.last_group = None;
        let local = self.local(e.position);
        match self.mode {
            Mode::Crop { .. } => {
                let card = self.card();
                let c = self.project().crop;
                let n = self.in_card(local);
                let corners = [(c.x, c.y), (c.x + c.w, c.y), (c.x, c.y + c.h), (c.x + c.w, c.y + c.h)];
                let near = corners.iter().position(|(x, y)| {
                    (card.x + x * card.w - local.0).abs() < HANDLE && (card.y + y * card.h - local.1).abs() < HANDLE
                });
                self.drag = Some(match near {
                    Some(i) => Drag::CropFrom { anchor: corners[3 - i] },
                    None if n.0 > c.x && n.0 < c.x + c.w && n.1 > c.y && n.1 < c.y + c.h => {
                        Drag::CropMove { grab: (n.0 - c.x, n.1 - c.y), base: c }
                    }
                    None => Drag::CropFrom { anchor: n },
                });
            }
            Mode::Focus { zoom } => {
                self.drag = Some(Drag::Focus { zoom });
                self.preview_drag(Drag::Focus { zoom }, e.position, cx);
            }
            Mode::Normal => {
                let bubble = bubble_rect(&self.project().design.camera, self.frame_size());
                let (cx_, cy_) = (bubble.x + bubble.w / 2.0, bubble.y + bubble.h / 2.0);
                let on_handle = (local.0 - (bubble.x + bubble.w)).abs() < HANDLE && (local.1 - (bubble.y + bubble.h)).abs() < HANDLE;
                if self.has_camera() && on_handle {
                    self.drag = Some(Drag::CameraSize);
                } else if self.has_camera() && bubble.contains(local.0, local.1) {
                    self.drag = Some(Drag::Camera { grab: (local.0 - cx_, local.1 - cy_) });
                } else {
                    self.toggle_play(cx);
                }
            }
        }
        cx.notify();
    }

    pub(super) fn preview_drag(&mut self, drag: Drag, pos: Point<Pixels>, cx: &mut Context<Self>) {
        let local = self.local(pos);
        let (w, h) = self.frame_size();
        if w <= 0.0 || h <= 0.0 {
            return;
        }
        let src = self.session.source_size;
        match drag {
            Drag::Camera { grab } => {
                let c = [((local.0 - grab.0) / w).clamp(0.0, 1.0), ((local.1 - grab.1) / h).clamp(0.0, 1.0)];
                self.edit(Some("camera"), cx, |p| p.design.camera.center = c);
            }
            Drag::CameraSize => {
                let b = bubble_rect(&self.project().design.camera, (w, h));
                let (cx_, cy_) = (b.x + b.w / 2.0, b.y + b.h / 2.0);
                let half = (local.0 - cx_).abs().max((local.1 - cy_).abs());
                let size = (2.0 * half / w.min(h)).clamp(0.08, 0.6);
                self.edit(Some("camera"), cx, |p| p.design.camera.size = size);
            }
            Drag::Focus { zoom } => {
                let (x, y) = self.in_card(local);
                self.edit(Some("focus"), cx, |p| {
                    if let Some(z) = p.zooms.get_mut(zoom) {
                        z.focus = ZoomFocus::Fixed { x, y };
                    }
                });
            }
            Drag::CropFrom { anchor } => {
                let to = self.in_card(local);
                let ratio = self.crop_ratio();
                self.edit(Some("crop"), cx, |p| p.crop = crop_from_drag(anchor, to, ratio, src));
            }
            Drag::CropMove { grab, base } => {
                let n = self.in_card(local);
                let c = CropRect { x: (n.0 - grab.0).clamp(0.0, 1.0 - base.w), y: (n.1 - grab.1).clamp(0.0, 1.0 - base.h), ..base };
                self.edit(Some("crop"), cx, |p| p.crop = c);
            }
            _ => {}
        }
    }

    fn preview_scroll(&mut self, e: &ScrollWheelEvent, _: &mut Window, cx: &mut Context<Self>) {
        if self.mode != Mode::Normal || !self.has_camera() {
            return;
        }
        let local = self.local(e.position);
        if !bubble_rect(&self.project().design.camera, self.frame_size()).contains(local.0, local.1) {
            return;
        }
        let dy = f32::from(e.delta.pixel_delta(px(20.)).y);
        self.edit(Some("camera"), cx, |p| {
            p.design.camera.size = (p.design.camera.size * (1.0 + dy / 600.0)).clamp(0.08, 0.6);
        });
        cx.stop_propagation();
    }

    fn focus_self(&mut self, cx: &mut Context<Self>) {
        self.preset_name = None;
        self.speed_menu = false;
        cx.notify();
    }

    pub(super) fn preview(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let sf = window.scale_factor();
        let area = self.preview_area.get();
        let out = self.preview_project().output_size(self.session.source_size);
        let (aw, ah) = ((f32::from(area.size.width) - 48.).max(64.), (f32::from(area.size.height) - 48.).max(64.));
        let k = (aw / out.0 as f32).min(ah / out.1 as f32);
        let (lw, lh) = (out.0 as f32 * k, out.1 as f32 * k);
        let measured = area.size.width > px(10.);
        if measured {
            self.player.set_preview_size((lw * sf).round() as u32, (lh * sf).round() as u32);
        }

        let area_cell = self.preview_area.clone();
        let frame_cell = self.frame_bounds.clone();
        let image = self.frame.as_ref().map(|(_, r)| r.clone());
        let weak = cx.entity().downgrade();

        let frame = div()
            .id("frame")
            .relative()
            .w(px(lw))
            .h(px(lh))
            .flex_none()
            .bg(rgb(0x0A0A0C))
            .rounded(px(4.))
            .overflow_hidden()
            .shadow_lg()
            .on_mouse_down(MouseButton::Left, cx.listener(Self::preview_mouse_down))
            .on_scroll_wheel(cx.listener(Self::preview_scroll))
            .when_some(image, |d, image| d.child(img(ImageSource::Render(image)).size_full().object_fit(ObjectFit::Contain)))
            .child(canvas(move |b, _, _| frame_cell.set(b), |_, _, _, _| {}).absolute().top_0().left_0().size_full())
            .children(self.overlay((lw, lh)));

        let toolbar = match self.mode {
            Mode::Crop { ratio, before } => Some(
                div()
                    .flex()
                    .items_center()
                    .gap_1()
                    .children(CropRatio::ALL.iter().enumerate().map(|(i, r)| {
                        let r = *r;
                        ui::chip(super::sidebar::eid("crop-ratio", i), r.label(), ratio == r, cx.listener(move |s, _, _, cx| s.set_crop_ratio(r, cx)))
                    }))
                    .child(div().w(px(8.)))
                    .child(ui::button("crop-reset", "Reset", None, false, cx.listener(|s, _, _, cx| {
                        s.edit(Some("crop"), cx, |p| p.crop = CropRect::FULL);
                    })))
                    .child(ui::button("crop-cancel", "Cancel", None, false, cx.listener(move |s, _, _, cx| {
                        s.edit(Some("crop"), cx, |p| p.crop = before);
                        s.set_mode(Mode::Normal, cx);
                    })))
                    .child(ui::button("crop-done", "Done", Some("check"), true, cx.listener(|s, _, _, cx| s.set_mode(Mode::Normal, cx)))),
            ),
            Mode::Focus { .. } => Some(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .child(div().text_xs().text_color(rgb(MUTED)).child("Drag the frame to where the zoom should look"))
                    .child(ui::button("focus-done", "Done", Some("check"), true, cx.listener(|s, _, _, cx| s.set_mode(Mode::Normal, cx)))),
            ),
            Mode::Normal => None,
        };

        div()
            .id("preview-area")
            .flex_1()
            .min_h_0()
            .relative()
            .flex()
            .items_center()
            .justify_center()
            .overflow_hidden()
            .child(
                canvas(
                    move |b, window, _| {
                        let old = area_cell.get();
                        if (old.size.width - b.size.width).abs() > px(0.5) || (old.size.height - b.size.height).abs() > px(0.5) {
                            area_cell.set(b);
                            let weak = weak.clone();
                            window.on_next_frame(move |w, cx| {
                                let _ = weak.update(cx, |s, cx| s.kick(cx));
                                w.refresh();
                            });
                        }
                    },
                    |_, _, _, _| {},
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
            .when(measured, |d| d.child(frame))
            .when_some(toolbar, |d, t| {
                d.child(
                    div().absolute().top(px(10.)).left_0().w_full().flex().justify_center().child(
                        t.px_2().py_1p5().rounded(px(ui::RADIUS_WINDOW)).bg(rgba(0x1C1C1FF0)).border_1().border_color(rgb(BORDER)).shadow_lg(),
                    ),
                )
            })
            .children(self.notice_view(cx))
            .children(self.player.error().map(|e| {
                div().absolute().top(px(10.)).right(px(12.)).max_w(px(360.)).text_xs().text_color(rgb(0xF0A0A0)).child(e)
            }))
    }

    fn overlay(&self, size: (f32, f32)) -> Option<gpui::Div> {
        let pos = |r: Rect| div().absolute().left(px(r.x)).top(px(r.y)).w(px(r.w.max(0.))).h(px(r.h.max(0.)));
        let shade = |r: Rect| pos(r).bg(rgba(0x0000009E));
        let dot = |x: f32, y: f32| {
            div()
                .absolute()
                .left(px(x - 6.))
                .top(px(y - 6.))
                .size(px(12.))
                .rounded_full()
                .bg(rgb(0xFFFFFF))
                .border_2()
                .border_color(ui::accent())
        };
        // Shades the card outside `inner`.
        let mask = |card: Rect, inner: Rect| {
            div()
                .absolute()
                .inset_0()
                .child(shade(Rect { x: card.x, y: card.y, w: card.w, h: inner.y - card.y }))
                .child(shade(Rect { x: card.x, y: inner.y + inner.h, w: card.w, h: card.y + card.h - inner.y - inner.h }))
                .child(shade(Rect { x: card.x, y: inner.y, w: inner.x - card.x, h: inner.h }))
                .child(shade(Rect { x: inner.x + inner.w, y: inner.y, w: card.x + card.w - inner.x - inner.w, h: inner.h }))
                .child(pos(inner).border_2().border_color(rgb(0xFFFFFF)))
        };
        match self.mode {
            Mode::Crop { .. } => {
                let card = self.card();
                let c = self.project().crop;
                let inner = Rect { x: card.x + c.x * card.w, y: card.y + c.y * card.h, w: c.w * card.w, h: c.h * card.h };
                let (sw, sh) = self.session.source_size;
                let label = format!("{} \u{00d7} {}", (c.w * sw as f32).round(), (c.h * sh as f32).round());
                Some(
                    mask(card, inner)
                        .children([(inner.x, inner.y), (inner.x + inner.w, inner.y), (inner.x, inner.y + inner.h), (inner.x + inner.w, inner.y + inner.h)].map(|(x, y)| dot(x, y)))
                        .child(
                            div()
                                .absolute()
                                .left(px(inner.x + 6.))
                                .top(px(inner.y + 6.))
                                .px_1p5()
                                .rounded(px(ui::RADIUS_CONTROL))
                                .bg(rgba(0x000000B0))
                                .text_xs()
                                .text_color(rgb(0xFFFFFF))
                                .child(label),
                        ),
                )
            }
            Mode::Focus { zoom } => {
                let z = self.project().zooms.get(zoom)?;
                let (fx, fy) = match z.focus {
                    ZoomFocus::Fixed { x, y } => (x, y),
                    ZoomFocus::Pointer => (0.5, 0.5),
                };
                let card = self.card();
                let s = z.scale.max(1.0);
                let (w, h) = (card.w / s, card.h / s);
                let x = (card.x + fx * card.w - w / 2.0).clamp(card.x, card.x + card.w - w);
                let y = (card.y + fy * card.h - h / 2.0).clamp(card.y, card.y + card.h - h);
                Some(mask(card, Rect { x, y, w, h }).child(dot(card.x + fx * card.w, card.y + fy * card.h)))
            }
            Mode::Normal => {
                let show = self.has_camera() && (self.tab == Tab::Camera || matches!(self.drag, Some(Drag::Camera { .. } | Drag::CameraSize)));
                if !show {
                    return None;
                }
                let cam = &self.project().design.camera;
                let b = bubble_rect(cam, size);
                Some(
                    div()
                        .absolute()
                        .inset_0()
                        .child(pos(b).border_2().border_color(ui::accent()).rounded(px(cam.roundness.clamp(0.0, 0.5) * b.w)))
                        .child(dot(b.x + b.w, b.y + b.h)),
                )
            }
        }
    }

    fn notice_view(&self, cx: &mut Context<Self>) -> Option<impl IntoElement> {
        let Notice { text, file, error } = self.notice.as_ref()?;
        let file = file.clone();
        let has_file = file.is_some();
        Some(
            div().absolute().bottom(px(16.)).left_0().w_full().flex().justify_center().child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .pl_4()
                    .pr_2()
                    .py_1p5()
                    .rounded_full()
                    .bg(rgba(0x1C1C1FF2))
                    .border_1()
                    .border_color(rgba(0xFFFFFF1F))
                    .shadow_lg()
                    .text_sm()
                    .text_color(rgb(TEXT))
                    .child(ui::icon(if *error { "close" } else { "check" }, 15., if *error { rgb(0xF07070) } else { ui::accent() }))
                    .child(text.clone())
                    .when_some(file, |d, path| {
                        let p2 = path.clone();
                        d.child(ui::button("reveal", "Show in Explorer", Some("folder"), false, move |_, _, _| shell::reveal(std::slice::from_ref(&path))))
                            .child(ui::button("copy-file", "Copy", Some("copy"), false, cx.listener(move |s, _, _, cx| {
                                match shell::copy_files(std::slice::from_ref(&p2), None) {
                                    Ok(()) => s.notify("Copied the video to the clipboard", false, cx),
                                    Err(e) => s.notify(format!("Copy failed: {e:#}"), true, cx),
                                }
                            })))
                    })
                    .when(!has_file, |d| d.pr_4()),
            ),
        )
    }
}
