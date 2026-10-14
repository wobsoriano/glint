//! The timeline: transport, a thumbnail strip cut into clips, the zoom lane and the playhead.
//! Clip and zoom drags re-apply their edit to the project as it was when the drag began, so
//! a drag never accumulates rounding and stays one undo step.

use super::geometry::{format_time, zoom_around, TimeScale};
use super::{Drag, Selection, Studio, ZoomPart};
use crate::ui;
use crate::win;
use glint_studio::timeline::MIN_ZOOM_MS;
use gpui::prelude::*;
use gpui::{
    canvas, div, img, px, rgb, rgba, Context, CursorStyle, ElementId, ImageSource, MouseButton, MouseDownEvent,
    ObjectFit, Pixels, Point, ScrollWheelEvent, SharedString, Window,
};
use std::sync::Arc;

const SPEEDS: [f64; 8] = [0.5, 0.75, 1.0, 1.25, 1.5, 2.0, 3.0, 4.0];
const THUMBS: u32 = 48;
const THUMB_HEIGHT: u32 = 96;
const RULER_H: f32 = 22.;
const CLIP_TOP: f32 = 26.;
const CLIP_H: f32 = 54.;
const ZOOM_TOP: f32 = 88.;
const ZOOM_H: f32 = 26.;
const TRACKS_H: f32 = 120.;
const EDGE: f32 = 8.;
const MAX_ZOOM: f64 = 40.0;

fn speed_label(speed: f64) -> String {
    format!("{}\u{00d7}", (speed * 100.0).round() / 100.0)
}

impl Studio {
    pub(super) fn load_thumbnails(&mut self, cx: &mut Context<Self>) {
        let snap = self.snapshot();
        let task = cx.background_spawn(async move { glint_studio::timeline_thumbnails(&snap, THUMBS, THUMB_HEIGHT) });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |s: &mut Studio, cx| match result {
                Ok(t) => {
                    s.thumbs = t.iter().map(win::render_image).collect();
                    cx.notify();
                }
                Err(e) => s.notify(format!("Couldn't load timeline thumbnails: {e:#}"), true, cx),
            });
        })
        .detach();
    }

    fn tracks_width(&self) -> f32 {
        f32::from(self.timeline_bounds.get().size.width)
    }

    /// The live scale, or the one a clip or zoom drag froze so blocks stay under the pointer.
    fn scale(&self) -> TimeScale {
        match &self.drag {
            Some(Drag::Trim { scale, .. } | Drag::Zoom { scale, .. }) => *scale,
            _ => TimeScale::new(self.duration(), self.tracks_width(), self.tl_zoom, self.tl_start),
        }
    }

    fn timeline_x(&self, pos: Point<Pixels>) -> f32 {
        f32::from(pos.x - self.timeline_bounds.get().origin.x)
    }

    pub(super) fn follow_playhead(&mut self) {
        if self.tl_zoom <= 1.0 {
            return;
        }
        let s = self.scale();
        let x = s.x(self.playhead);
        if x < 0.0 || x > self.tracks_width() {
            self.tl_start = self.playhead - self.tracks_width() as f64 * s.ms_per_px * 0.1;
        }
    }

    fn set_timeline_zoom(&mut self, zoom: f64, anchor_x: f32, cx: &mut Context<Self>) {
        let anchor_ms = self.scale().ms(anchor_x);
        self.tl_zoom = zoom.clamp(1.0, MAX_ZOOM);
        self.tl_start = zoom_around(self.duration(), self.tracks_width(), self.tl_zoom, anchor_ms, anchor_x);
        cx.notify();
    }

    fn tracks_scroll(&mut self, e: &ScrollWheelEvent, _: &mut Window, cx: &mut Context<Self>) {
        let d = e.delta.pixel_delta(px(20.));
        let x = self.timeline_x(e.position);
        if e.modifiers.control {
            self.set_timeline_zoom(self.tl_zoom * (1.0 + f32::from(d.y) as f64 / 300.0), x, cx);
        } else if self.tl_zoom > 1.0 {
            let delta = if d.x != px(0.) { d.x } else { d.y };
            self.tl_start = self.scale().start_ms - f32::from(delta) as f64 * self.scale().ms_per_px;
            self.tl_start = TimeScale::new(self.duration(), self.tracks_width(), self.tl_zoom, self.tl_start).start_ms;
            cx.notify();
        }
    }

    fn tracks_mouse_down(&mut self, e: &MouseDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        self.speed_menu = false;
        self.drag = Some(Drag::Scrub);
        if self.player.is_playing() {
            self.toggle_play(cx);
        }
        self.seek(self.scale().ms(self.timeline_x(e.position)), cx);
    }

    fn clip_mouse_down(&mut self, clip: usize, edge: Option<bool>, e: &MouseDownEvent, cx: &mut Context<Self>) {
        cx.stop_propagation();
        self.select(Some(Selection::Clip(clip)), cx);
        if self.player.is_playing() {
            self.toggle_play(cx);
        }
        let scale = self.scale();
        let at = scale.ms(self.timeline_x(e.position));
        match edge {
            Some(end) => {
                self.last_group = None;
                self.drag = Some(Drag::Trim { clip, end, base: self.project().clone(), from_ms: at, scale });
            }
            None => {
                self.drag = Some(Drag::Scrub);
                self.seek(at, cx);
            }
        }
    }

    fn zoom_mouse_down(&mut self, zoom: usize, part: ZoomPart, e: &MouseDownEvent, cx: &mut Context<Self>) {
        cx.stop_propagation();
        self.select(Some(Selection::Zoom(zoom)), cx);
        let base = self.project().clone();
        let z = &base.zooms[zoom];
        let ranges = base.output_ranges(z.start_ms..z.end_ms);
        let (Some(first), Some(last)) = (ranges.first(), ranges.last()) else { return };
        let range = first.start..last.end;
        let scale = self.scale();
        self.last_group = None;
        self.drag = Some(Drag::Zoom {
            zoom,
            part,
            base,
            range,
            from_ms: scale.ms(self.timeline_x(e.position)),
            scale,
        });
    }

    pub(super) fn timeline_drag(&mut self, drag: Drag, pos: Point<Pixels>, cx: &mut Context<Self>) {
        let x = self.timeline_x(pos);
        match drag {
            Drag::Scrub => self.seek(self.scale().ms(x), cx),
            Drag::Trim { clip, end, base, from_ms, scale } => {
                let dx = scale.ms(x) - from_ms;
                let c = base.clips[clip];
                let mut next = base;
                if end {
                    next.trim_clip(clip, c.start_ms, c.end_ms + dx * c.speed);
                } else {
                    next.trim_clip(clip, c.start_ms + dx * c.speed, c.end_ms);
                }
                let edge = next.clip_output_start(clip) + if end { next.clips[clip].output_len() - 1.0 } else { 0.0 };
                self.edit(Some("trim"), cx, |p| *p = next);
                self.seek(edge, cx);
            }
            Drag::Zoom { zoom, part, base, range, from_ms, scale } => {
                let dx = scale.ms(x) - from_ms;
                let dur = base.duration_ms();
                let len = range.end - range.start;
                let r = match part {
                    ZoomPart::Body => {
                        let start = (range.start + dx).clamp(0.0, (dur - len).max(0.0));
                        start..start + len
                    }
                    ZoomPart::Start => (range.start + dx).clamp(0.0, range.end - MIN_ZOOM_MS)..range.end,
                    ZoomPart::End => range.start..(range.end + dx).clamp(range.start + MIN_ZOOM_MS, dur),
                };
                let mut next = base;
                let i = next.retime_zoom(zoom, r);
                self.edit(Some("zoom"), cx, |p| *p = next);
                self.selection = Some(Selection::Zoom(i));
            }
            _ => {}
        }
    }

    pub(super) fn timeline(&mut self, cx: &mut Context<Self>) -> impl IntoElement {
        let playing = self.player.is_playing();
        let pos = self.position();
        let dur = self.duration();
        let clip_sel = match self.selection {
            Some(Selection::Clip(i)) => Some(i),
            _ => None,
        };
        let sep = || div().w(px(1.)).h(px(20.)).mx_1().bg(ui::theme().border);

        let speed_menu = clip_sel.map(|i| {
            let current = self.project().clips[i].speed;
            div()
                .relative()
                .child(ui::button("speed", format!("Speed {}", speed_label(current)), Some("timer"), false, cx.listener(|s, _, _, cx| {
                    s.speed_menu = !s.speed_menu;
                    cx.notify();
                })))
                .when(self.speed_menu, |d| {
                    d.child(
                        div()
                            .id("speed-menu")
                            .occlude()
                            .absolute()
                            .bottom(px(36.))
                            .left_0()
                            .w(px(120.))
                            .p_1()
                            .flex()
                            .flex_col()
                            .rounded(px(ui::RADIUS_WINDOW))
                            .bg(ui::theme().flyout)
                            .border_1()
                            .border_color(ui::theme().border)
                            .shadow_lg()
                            .children(SPEEDS.iter().enumerate().map(|(k, sp)| {
                                let sp = *sp;
                                div()
                                    .id(ElementId::Name(format!("speed-{k}").into()))
                                    .px_2()
                                    .py_1()
                                    .rounded(px(ui::RADIUS_CONTROL))
                                    .text_sm()
                                    .cursor_pointer()
                                    .hover(|s| s.bg(ui::theme().control_hover))
                                    .when((sp - current).abs() < 1e-6, |d| d.text_color(ui::accent()))
                                    .child(speed_label(sp))
                                    .on_click(cx.listener(move |s, _, _, cx| s.set_speed(sp, cx)))
                            })),
                    )
                })
        });

        let toolbar = div()
            .h(px(44.))
            .flex_none()
            .flex()
            .items_center()
            .gap_1()
            .px_3()
            .child(ui::icon_button("play", if playing { "pause" } else { "play" }, false, if playing { "Pause (Space)" } else { "Play (Space)" }, cx.listener(|s, _, _, cx| s.toggle_play(cx))))
            .child(
                div()
                    .w(px(130.))
                    .text_sm()
                    .child(format_time(pos))
                    .child(div().text_color(ui::theme().muted).text_xs().child(format!("of {}", format_time(dur)))),
            )
            .child(sep())
            .child(ui::icon_button("split", "scissors", false, "Split at playhead (S)", cx.listener(|s, _, _, cx| s.split(cx))))
            .child(div().child(ui::icon_button("delete", "trash", false, "Delete selection (Del)", cx.listener(|s, _, _, cx| s.delete_selection(cx)))).when(self.selection.is_none(), |d| d.opacity(0.4)))
            .children(speed_menu)
            .child(sep())
            .child(ui::button("add-zoom", "Add zoom", Some("zoom"), false, cx.listener(|s, _, _, cx| s.add_zoom(cx))))
            .child(div().flex_1())
            .child(div().text_xs().text_color(ui::theme().muted).child("Ctrl+scroll to zoom"))
            .child(ui::icon_button("tl-out", "minus", false, "Zoom timeline out", cx.listener(|s, _, _, cx| {
                let mid = s.tracks_width() / 2.0;
                s.set_timeline_zoom(s.tl_zoom / 1.5, mid, cx)
            })))
            .child(ui::icon_button("tl-in", "plus", false, "Zoom timeline in", cx.listener(|s, _, _, cx| {
                let x = s.scale().x(s.position()).clamp(0.0, s.tracks_width());
                s.set_timeline_zoom(s.tl_zoom * 1.5, x, cx)
            })));

        let bounds_cell = self.timeline_bounds.clone();
        let weak = cx.entity().downgrade();
        let width = self.tracks_width();
        let measured = width > 10.0;
        let scale = self.scale();

        let mut tracks = div()
            .id("tracks")
            .relative()
            .h(px(TRACKS_H))
            .mx_3()
            .mb_2()
            .overflow_hidden()
            .cursor(CursorStyle::Arrow)
            .on_mouse_down(MouseButton::Left, cx.listener(Self::tracks_mouse_down))
            .on_scroll_wheel(cx.listener(Self::tracks_scroll))
            .child(
                canvas(
                    move |b, window, _| {
                        let old = bounds_cell.get();
                        if (old.size.width - b.size.width).abs() > px(0.5) || old.origin != b.origin {
                            bounds_cell.set(b);
                            let weak = weak.clone();
                            window.on_next_frame(move |w, cx| {
                                let _ = weak.update(cx, |_, cx| cx.notify());
                                w.refresh();
                            });
                        }
                    },
                    |_, _, _, _| {},
                )
                .absolute()
                .size_full(),
            )
            .child(div().absolute().left_0().top(px(CLIP_TOP)).w_full().h(px(CLIP_H)).rounded(px(6.)).bg(ui::theme().canvas))
            .child(div().absolute().left_0().top(px(ZOOM_TOP)).w_full().h(px(ZOOM_H)).rounded(px(6.)).bg(ui::theme().canvas));
        if measured {
            tracks = tracks.children(self.ruler(scale, width)).children(self.clip_blocks(scale, width, cx)).children(self.zoom_blocks(scale, width, cx));
            let x = scale.x(pos);
            tracks = tracks.child(
                div()
                    .absolute()
                    .left(px(x - 1.))
                    .top_0()
                    .w(px(2.))
                    .h_full()
                    .bg(ui::theme().text)
                    .child(div().absolute().top_0().left(px(-5.)).size(px(12.)).rounded_full().bg(ui::theme().text)),
            );
        }

        div()
            .flex_none()
            .flex()
            .flex_col()
            .bg(ui::theme().layer)
            .border_t_1()
            .border_color(ui::theme().border)
            .child(toolbar)
            .child(tracks)
    }

    fn ruler(&self, scale: TimeScale, width: f32) -> Vec<gpui::Div> {
        let steps = [100.0, 250.0, 500.0, 1_000.0, 2_000.0, 5_000.0, 10_000.0, 15_000.0, 30_000.0, 60_000.0, 120_000.0, 300_000.0];
        let step = steps.into_iter().find(|s| s / scale.ms_per_px >= 80.0).unwrap_or(600_000.0);
        let first = (scale.start_ms / step).floor() as i64;
        let last = (scale.ms(width) / step).ceil() as i64;
        (first..=last)
            .map(|k| {
                let t = k as f64 * step;
                div()
                    .absolute()
                    .left(px(scale.x(t)))
                    .top_0()
                    .h(px(RULER_H))
                    .border_l_1()
                    .border_color(ui::theme().border)
                    .pl_1()
                    .text_xs()
                    .text_color(ui::theme().muted)
                    .child(format_time(t))
            })
            .collect()
    }

    fn clip_blocks(&self, scale: TimeScale, width: f32, cx: &mut Context<Self>) -> Vec<impl IntoElement> {
        let p = self.project();
        let (sw, sh) = self.session.source_size;
        let tile_w = CLIP_H * sw as f32 / sh.max(1) as f32;
        let src_dur = self.session.source_duration_ms.max(1.0);
        let mut out_start = 0.0;
        let mut blocks = Vec::new();
        for (i, c) in p.clips.iter().enumerate() {
            let start = out_start;
            out_start += c.output_len();
            let (x0, x1) = (scale.x(start), scale.x(out_start));
            if x1 < 0.0 || x0 > width {
                continue;
            }
            let w = x1 - x0;
            let selected = self.selection == Some(Selection::Clip(i));
            let first = ((-x0).max(0.0) / tile_w).floor() as i32;
            let last = ((width - x0).min(w) / tile_w).ceil() as i32;
            let tiles = (first..last.max(first)).filter_map(|k| {
                let tx = k as f32 * tile_w;
                let t = start + (tx + tile_w / 2.0) as f64 * scale.ms_per_px;
                let src = c.start_ms + (t - start) * c.speed;
                let idx = ((src / src_dur * THUMBS as f64) as usize).min(self.thumbs.len().checked_sub(1)?);
                let image: Arc<gpui::RenderImage> = self.thumbs[idx].clone();
                Some(
                    div()
                        .absolute()
                        .left(px(tx))
                        .top_0()
                        .w(px(tile_w))
                        .h_full()
                        .child(img(ImageSource::Render(image)).size_full().object_fit(ObjectFit::Cover)),
                )
            });
            let edge = |end: bool| {
                div()
                    .id(ElementId::Name(format!("clip-{i}-{}", if end { "end" } else { "start" }).into()))
                    .absolute()
                    .top_0()
                    .h_full()
                    .w(px(EDGE))
                    .when(end, |d| d.right_0())
                    .when(!end, |d| d.left_0())
                    .cursor(CursorStyle::ResizeLeftRight)
                    .when(selected, |d| d.bg(ui::accent()))
                    .hover(|s| s.bg(ui::accent()))
                    .on_mouse_down(MouseButton::Left, cx.listener(move |s, e, _, cx| s.clip_mouse_down(i, Some(end), e, cx)))
            };
            blocks.push(
                div()
                    .id(ElementId::Name(format!("clip-{i}").into()))
                    .absolute()
                    .left(px(x0))
                    .top(px(CLIP_TOP))
                    .w(px(w))
                    .h(px(CLIP_H))
                    .rounded(px(6.))
                    .overflow_hidden()
                    .bg(ui::theme().control)
                    .border_2()
                    .border_color(if selected { ui::accent() } else { ui::theme().border })
                    .cursor(CursorStyle::PointingHand)
                    .on_mouse_down(MouseButton::Left, cx.listener(move |s, e, _, cx| s.clip_mouse_down(i, None, e, cx)))
                    .children(tiles)
                    .when((c.speed - 1.0).abs() > 1e-6, |d| {
                        d.child(
                            div()
                                .absolute()
                                .top(px(4.))
                                .left(px(EDGE + 4.))
                                .px_1p5()
                                .rounded(px(ui::RADIUS_CONTROL))
                                .bg(rgba(0x000000C0))
                                .text_xs()
                                .text_color(rgb(0xFFFFFF))
                                .child(speed_label(c.speed)),
                        )
                    })
                    .child(edge(false))
                    .child(edge(true)),
            );
        }
        blocks
    }

    fn zoom_blocks(&self, scale: TimeScale, width: f32, cx: &mut Context<Self>) -> Vec<impl IntoElement> {
        let p = self.project();
        let mut blocks = Vec::new();
        for (i, z) in p.zooms.iter().enumerate() {
            let ranges = p.output_ranges(z.start_ms..z.end_ms);
            let selected = self.selection == Some(Selection::Zoom(i));
            let n = ranges.len();
            for (k, r) in ranges.into_iter().enumerate() {
                let (x0, x1) = (scale.x(r.start), scale.x(r.end));
                if x1 < 0.0 || x0 > width {
                    continue;
                }
                let label: SharedString = format!(
                    "{}{}{}",
                    if z.auto { "Auto \u{00b7} " } else { "" },
                    speed_label(z.scale as f64),
                    if matches!(z.focus, glint_studio::ZoomFocus::Fixed { .. }) { " \u{00b7} Fixed" } else { "" }
                )
                .into();
                let handle = |part: ZoomPart| {
                    div()
                        .id(ElementId::Name(format!("zoom-{i}-{k}-{part:?}").into()))
                        .absolute()
                        .top_0()
                        .h_full()
                        .w(px(EDGE))
                        .when(part == ZoomPart::End, |d| d.right_0())
                        .when(part == ZoomPart::Start, |d| d.left_0())
                        .cursor(CursorStyle::ResizeLeftRight)
                        .hover(|s| s.bg(ui::theme().control_hover))
                        .on_mouse_down(MouseButton::Left, cx.listener(move |s, e, _, cx| s.zoom_mouse_down(i, part, e, cx)))
                };
                blocks.push(
                    div()
                        .id(ElementId::Name(format!("zoom-{i}-{k}").into()))
                        .absolute()
                        .left(px(x0))
                        .top(px(ZOOM_TOP))
                        .w(px((x1 - x0).max(4.)))
                        .h(px(ZOOM_H))
                        .rounded(px(6.))
                        .overflow_hidden()
                        .flex()
                        .items_center()
                        .px(px(EDGE + 2.))
                        .gap_1()
                        .when(z.enabled, |d| d.bg(ui::accent_alpha(0x55)))
                        .when(!z.enabled, |d| d.bg(ui::theme().control).text_color(ui::theme().muted))
                        .border_2()
                        .border_color(if selected { ui::theme().text } else if z.enabled { ui::accent() } else { ui::theme().border })
                        .cursor(CursorStyle::OpenHand)
                        .text_xs()
                        .text_color(ui::theme().text)
                        .on_mouse_down(MouseButton::Left, cx.listener(move |s, e, _, cx| s.zoom_mouse_down(i, ZoomPart::Body, e, cx)))
                        .child(ui::icon("zoom", 12., ui::theme().text))
                        .child(div().whitespace_nowrap().child(label))
                        .when(k == 0, |d| d.child(handle(ZoomPart::Start)))
                        .when(k + 1 == n, |d| d.child(handle(ZoomPart::End))),
                );
            }
        }
        blocks
    }
}
