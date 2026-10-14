//! The editing canvas: renders only the visible part of the composition at the zoom's scale,
//! maps pointer positions to image coordinates and draws selection and crop overlays.

use super::viewport::View;
use super::{Editor, Typing};
use crate::annotate::{pt, Pt, Shape, Tool, R};
use crate::style::Background;
use crate::ui::{self, fluent::{c, theme}};
use glint_render::compose::Frame;
use glint_render::doc::EditDoc;
use gpui::prelude::*;
use gpui::{
    canvas, div, img, px, rgb, rgba, App, Context, CursorStyle, DispatchPhase, ImageSource, MouseButton, MouseMoveEvent,
    MouseUpEvent, ObjectFit, Pixels, Point, RenderImage, ScrollDelta, ScrollWheelEvent, Window,
};
use std::sync::Arc;

pub(super) struct Preview {
    image: Arc<RenderImage>,
    key: String,
    /// Document geometry at `scale`.
    pub frame: Frame,
    /// Render scale: canvas pixels per full-resolution canvas pixel.
    pub scale: f32,
    pub view: View,
    /// Full-resolution canvas size.
    pub canvas: (f32, f32),
    /// Physical offset and size of the rendered image inside the canvas area.
    pos: (f32, f32),
    size: (f32, f32),
    pub sf: f32,
}

/// Straight BGRA for GPUI, optionally enlarged `k` times with nearest-neighbor sampling so
/// zoomed-in pixels stay crisp.
fn to_bgra(pm: &tiny_skia::Pixmap, k: f32) -> image::RgbaImage {
    let (sw, sh) = (pm.width() as usize, pm.height() as usize);
    let (dw, dh) = (((sw as f32 * k).round() as usize).max(1), ((sh as f32 * k).round() as usize).max(1));
    let xs: Vec<usize> = (0..dw).map(|x| (((x as f32 + 0.5) / k) as usize).min(sw - 1)).collect();
    let src = pm.pixels();
    let mut out = Vec::with_capacity(dw * dh * 4);
    let mut row: Vec<u8> = Vec::with_capacity(dw * 4);
    let mut last_sy = usize::MAX;
    for y in 0..dh {
        let sy = (((y as f32 + 0.5) / k) as usize).min(sh - 1);
        if sy != last_sy {
            row.clear();
            for &sx in &xs {
                let c = src[sy * sw + sx].demultiply();
                row.extend_from_slice(&[c.blue(), c.green(), c.red(), c.alpha()]);
            }
            last_sy = sy;
        }
        out.extend_from_slice(&row);
    }
    image::RgbaImage::from_raw(dw as u32, dh as u32, out).unwrap()
}

impl Editor {
    /// The document as the canvas shows it: crop mode shows the whole flat image.
    fn view_doc(&self) -> EditDoc {
        let mut d = self.doc.clone();
        if self.crop.is_some() {
            let (w, h) = self.base_size();
            let c = self.doc.crop_rect(w, h);
            d.crop = None;
            for a in &mut d.annotations {
                a.translate(c.x, c.y);
            }
            d.effects.perspective = Default::default();
            d.effects.blur.enabled = false;
        }
        match self.typing {
            Some(Typing::Text(id)) => {
                if let Some(Shape::Text { text, .. }) = d.annotations.iter_mut().find(|a| a.id == id).map(|a| &mut a.shape) {
                    text.push('|');
                }
            }
            Some(Typing::Watermark) => d.effects.watermark.text.push('|'),
            None => {}
        }
        d
    }

    fn view_size(&self, window: &Window) -> (f32, f32) {
        let area = self.area.get();
        let sf = window.scale_factor();
        if area.size.width > px(10.) {
            (f32::from(area.size.width) * sf, f32::from(area.size.height) * sf)
        } else {
            let vp = window.viewport_size();
            ((f32::from(vp.width) - 340.) * sf, (f32::from(vp.height) - 90.) * sf)
        }
    }

    pub(super) fn ensure_preview(&mut self, window: &mut Window, cx: &mut App) {
        let sf = window.scale_factor();
        let vsize = self.view_size(window);
        let doc = self.view_doc();
        let (bw, bh) = self.base_size();
        let full = Frame::new(&doc, bw, bh, 1.0).layout;
        let canvas = (full.width as f32, full.height as f32);
        let view = self.viewport.resolve(canvas, vsize, sf);
        let scale = view.zoom.min(1.0);
        let frame = Frame::new(&doc, bw, bh, scale);
        let visible = R::new(view.origin.x, view.origin.y, vsize.0 / view.zoom, vsize.1 / view.zoom)
            .intersect(&R::new(0.0, 0.0, canvas.0, canvas.1))
            .unwrap_or(R::new(0.0, 0.0, 1.0, 1.0));
        let region = R::new(visible.x * scale, visible.y * scale, visible.w * scale, visible.h * scale)
            .round_out()
            .intersect(&R::new(0.0, 0.0, frame.layout.width as f32, frame.layout.height as f32))
            .unwrap_or(R::new(0.0, 0.0, 1.0, 1.0));
        let key = format!("{doc:?}|{region:?}|{scale}|{view:?}|{vsize:?}");
        if self.preview.as_ref().is_some_and(|p| p.key == key) {
            return;
        }
        self.ensure_sources();
        let mut compositor = std::mem::take(&mut self.compositor);
        let pm = compositor.render(&doc, &self.sources(), scale, Some(region));
        self.compositor = compositor;
        let k = view.zoom / scale;
        let bgra = to_bgra(&pm, if k > 1.001 { k } else { 1.0 });
        let size = (bgra.width() as f32, bgra.height() as f32);
        let image = Arc::new(RenderImage::new(smallvec::smallvec![image::Frame::new(bgra)]));
        let pos = ((region.x / scale - view.origin.x) * view.zoom, (region.y / scale - view.origin.y) * view.zoom);
        let old = self.preview.replace(Preview { image, key, frame, scale, view, canvas, pos, size, sf });
        if let Some(old) = old {
            cx.drop_image(old.image, Some(window));
        }
    }

    /// Window position -> image coordinates (base pixels while cropping).
    pub(super) fn to_image(&self, pos: Point<Pixels>) -> Option<Pt> {
        let p = self.preview.as_ref()?;
        let a = self.area.get();
        let local = pt(f32::from(pos.x - a.origin.x) * p.sf, f32::from(pos.y - a.origin.y) * p.sf);
        let c = p.view.to_canvas(local);
        p.frame.to_image(pt(c.x * p.scale, c.y * p.scale))
    }

    /// Image coordinates -> logical pixels inside the canvas area.
    fn to_local(&self, p: Pt) -> Pt {
        let Some(pv) = self.preview.as_ref() else { return p };
        let c = pv.frame.to_canvas(p);
        let s = pv.view.to_screen(pt(c.x / pv.scale, c.y / pv.scale));
        pt(s.x / pv.sf, s.y / pv.sf)
    }

    /// Image pixels per logical pixel (ignoring perspective).
    pub(super) fn image_per_logical(&self) -> f32 {
        match &self.preview {
            Some(p) => p.sf * p.scale / (p.frame.layout.scale * p.view.zoom),
            None => 1.0,
        }
    }

    pub(super) fn zoom_percent(&self) -> f32 {
        self.preview.as_ref().map_or(100.0, |p| p.view.zoom * 100.0)
    }

    pub(super) fn zoom_to(&mut self, zoom: Option<f32>, anchor: Option<Pt>) {
        let Some(p) = &self.preview else { return };
        let vsize = (f32::from(self.area.get().size.width) * p.sf, f32::from(self.area.get().size.height) * p.sf);
        let anchor = anchor.unwrap_or(pt(vsize.0 / 2.0, vsize.1 / 2.0));
        match zoom {
            None => self.viewport = super::viewport::Viewport::default(),
            Some(z) => self.viewport.zoom_to(z, anchor, p.canvas, vsize, p.sf),
        }
    }

    /// Zooms to the next standard level, around the middle of the view.
    pub(super) fn zoom_step(&mut self, up: bool) {
        let z = self.preview.as_ref().map_or(1.0, |p| p.view.zoom);
        self.zoom_to(Some(super::viewport::step(z, up)), None);
    }

    pub(super) fn on_scroll(&mut self, e: &ScrollWheelEvent, _: &mut Window, cx: &mut Context<Self>) {
        let Some(p) = &self.preview else { return };
        let sf = p.sf;
        let a = self.area.get();
        let vsize = (f32::from(a.size.width) * sf, f32::from(a.size.height) * sf);
        let (dx, dy) = match e.delta {
            ScrollDelta::Pixels(d) => (f32::from(d.x), f32::from(d.y)),
            ScrollDelta::Lines(d) => (d.x * 48.0, d.y * 48.0),
        };
        if e.modifiers.control || e.modifiers.alt {
            let anchor = pt(f32::from(e.position.x - a.origin.x) * sf, f32::from(e.position.y - a.origin.y) * sf);
            let factor = (dy * 0.0035).exp();
            let z = p.view.zoom * factor;
            self.viewport.zoom_to(z, anchor, p.canvas, vsize, sf);
        } else {
            let (dx, dy) = if e.modifiers.shift && dx == 0.0 { (dy, 0.0) } else { (dx, dy) };
            self.viewport.pan(-dx * sf, -dy * sf, p.canvas, vsize, sf);
        }
        cx.notify();
    }

    fn overlays(&self) -> gpui::Div {
        let mut layer = div().absolute().inset_0();
        let bbox = |r: R| {
            let pts = [pt(r.x, r.y), pt(r.right(), r.y), pt(r.right(), r.bottom()), pt(r.x, r.bottom())].map(|q| self.to_local(q));
            let x0 = pts.iter().map(|q| q.x).fold(f32::MAX, f32::min);
            let y0 = pts.iter().map(|q| q.y).fold(f32::MAX, f32::min);
            let x1 = pts.iter().map(|q| q.x).fold(f32::MIN, f32::max);
            let y1 = pts.iter().map(|q| q.y).fold(f32::MIN, f32::max);
            R::new(x0, y0, x1 - x0, y1 - y0)
        };
        let handle = |p: Pt| {
            div()
                .absolute()
                .left(px(p.x - 5.))
                .top(px(p.y - 5.))
                .size(px(10.))
                .rounded_full()
                .bg(rgb(0xFFFFFF))
                .border_2()
                .border_color(c(theme().accent))
        };
        if let Some(c) = &self.crop {
            let (w, h) = self.base_size();
            let img = bbox(R::new(0.0, 0.0, w as f32, h as f32));
            let r = bbox(c.rect);
            let shade = |x: f32, y: f32, w: f32, h: f32| {
                div().absolute().left(px(x)).top(px(y)).w(px(w.max(0.))).h(px(h.max(0.))).bg(rgba(0x000000A6))
            };
            layer = layer
                .child(shade(img.x, img.y, img.w, r.y - img.y))
                .child(shade(img.x, r.bottom(), img.w, img.bottom() - r.bottom()))
                .child(shade(img.x, r.y, r.x - img.x, r.h))
                .child(shade(r.right(), r.y, img.right() - r.right(), r.h))
                .child(div().absolute().left(px(r.x)).top(px(r.y)).w(px(r.w)).h(px(r.h)).border_2().border_color(rgb(0xFFFFFF)))
                .children(super::input::crop_handles(c.rect, self.crop_ratio().is_none()).into_iter().map(|p| handle(self.to_local(p))))
                .child(
                    div()
                        .absolute()
                        .left(px(r.x))
                        .top(px((r.y - 26.).max(4.)))
                        .px_2()
                        .py_0p5()
                        .rounded_md()
                        .bg(rgba(0x000000CC))
                        .text_xs()
                        .text_color(rgb(0xFFFFFF))
                        .child(format!("{} × {}", c.rect.w as i32, c.rect.h as i32)),
                );
            return layer;
        }
        for a in self.selected_anns() {
            let b = bbox(a.bounds());
            layer = layer.child(
                div()
                    .absolute()
                    .left(px(b.x - 3.))
                    .top(px(b.y - 3.))
                    .w(px(b.w + 6.))
                    .h(px(b.h + 6.))
                    .border_1()
                    .border_color(c(theme().accent))
                    .rounded_sm(),
            );
        }
        if let Some(a) = self.single_selected() {
            layer = layer.children(a.handles().into_iter().map(|h| handle(self.to_local(h))));
        }
        if let Some(super::Drag::Marquee { start, now, .. }) = &self.drag {
            let m = bbox(R::from_pts(*start, *now));
            layer = layer.child(
                div()
                    .absolute()
                    .left(px(m.x))
                    .top(px(m.y))
                    .w(px(m.w))
                    .h(px(m.h))
                    .bg(c((theme().accent & 0xFFFFFF00) | 0x26))
                    .border_1()
                    .border_color(c(theme().accent)),
            );
        }
        layer
    }

    pub(super) fn canvas_area(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let weak = cx.entity().downgrade();
        let area_cell = self.area.clone();
        let cursor = match self.tool {
            Tool::Select => CursorStyle::Arrow,
            Tool::Text => CursorStyle::IBeam,
            _ => CursorStyle::Crosshair,
        };
        let drag_weak = weak.clone();
        let image = self.preview.as_ref().map(|p| {
            div()
                .absolute()
                .left(px(p.pos.0 / p.sf))
                .top(px(p.pos.1 / p.sf))
                .w(px(p.size.0 / p.sf))
                .h(px(p.size.1 / p.sf))
                .when(matches!(self.doc.style.background, Background::None), |d| d.bg(rgb(0x1A1A1D)))
                .child(img(ImageSource::Render(p.image.clone())).size_full().object_fit(ObjectFit::Fill))
        });
        div()
            .id("preview-area")
            .flex_1()
            .h_full()
            .relative()
            .overflow_hidden()
            .bg(c(theme().canvas))
            .cursor(cursor)
            .on_mouse_down(MouseButton::Left, cx.listener(Self::on_mouse_down))
            .on_scroll_wheel(cx.listener(Self::on_scroll))
            .child(
                canvas(
                    move |b, window, _| {
                        let old = area_cell.get();
                        if old != b {
                            area_cell.set(b);
                            window.on_next_frame(|w, _| w.refresh());
                        }
                    },
                    move |_, _, window, _| {
                        let w1 = drag_weak.clone();
                        window.on_mouse_event(move |e: &MouseMoveEvent, phase, _, cx| {
                            if phase == DispatchPhase::Bubble && e.pressed_button == Some(MouseButton::Left) {
                                let _ = w1.update(cx, |this, cx| {
                                    if this.drag.is_some() {
                                        this.on_drag_move(e.position, e.modifiers, cx);
                                    }
                                });
                            }
                        });
                        let w2 = drag_weak.clone();
                        window.on_mouse_event(move |_: &MouseUpEvent, phase, _, cx| {
                            if phase == DispatchPhase::Bubble {
                                let _ = w2.update(cx, |this, cx| this.on_drag_end(cx));
                            }
                        });
                    },
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
            .children(image)
            .child(self.overlays())
            .when_some(self.toast.clone().or(self.busy.clone()), |d, msg| {
                d.child(
                    div().absolute().bottom(px(18.)).left_0().w_full().flex().justify_center().child(
                        div()
                            .flex()
                            .items_center()
                            .gap_2()
                            .px_4()
                            .py_2()
                            .rounded_full()
                            .bg(rgba(0x1C1C1FF2))
                            .border_1()
                            .border_color(rgba(0xFFFFFF1F))
                            .shadow_lg()
                            .text_sm()
                            .text_color(c(theme().text))
                            .child(ui::icon(if self.busy.is_some() { "scan" } else { "check" }, 15., c(theme().accent)))
                            .max_w(px(620.))
                            .child(div().truncate().child(msg)),
                    ),
                )
            })
    }
}
