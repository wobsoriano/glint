use super::{next_id, AfterSave, Drag, Editor, Typing};
use crate::annotate::{pt, Annotation, Pt, Shape, Tool, R};
use gpui::{Context, KeyDownEvent, Modifiers, MouseDownEvent, Pixels, Point, Window};

pub(super) fn crop_handles(r: R, edges: bool) -> Vec<Pt> {
    let mut v = vec![pt(r.x, r.y), pt(r.right(), r.y), pt(r.right(), r.bottom()), pt(r.x, r.bottom())];
    if edges {
        let c = r.center();
        v.extend([pt(c.x, r.y), pt(r.right(), c.y), pt(c.x, r.bottom()), pt(r.x, c.y)]);
    }
    v
}

fn ratio_rect(anchor: Pt, p: Pt, ratio: f32, bounds: R) -> R {
    let (sx, sy) = ((p.x - anchor.x).signum(), (p.y - anchor.y).signum());
    let max_w = if sx >= 0.0 { bounds.right() - anchor.x } else { anchor.x - bounds.x };
    let max_h = if sy >= 0.0 { bounds.bottom() - anchor.y } else { anchor.y - bounds.y };
    let mut w = (p.x - anchor.x).abs().max((p.y - anchor.y).abs() * ratio);
    w = w.min(max_w).min(max_h * ratio);
    let h = w / ratio;
    R::from_pts(anchor, pt(anchor.x + sx * w, anchor.y + sy * h))
}

fn snap_angle(a: Pt, b: Pt) -> Pt {
    let (dx, dy) = (b.x - a.x, b.y - a.y);
    let len = (dx * dx + dy * dy).sqrt();
    let step = std::f32::consts::FRAC_PI_4;
    let snapped = (dy.atan2(dx) / step).round() * step;
    pt(a.x + len * snapped.cos(), a.y + len * snapped.sin())
}

fn square(a: Pt, b: Pt) -> Pt {
    let s = (b.x - a.x).abs().max((b.y - a.y).abs());
    pt(a.x + s * (b.x - a.x).signum(), a.y + s * (b.y - a.y).signum())
}

impl Editor {
    fn full_rect(&self) -> R {
        let (w, h) = self.base_size();
        R::new(0.0, 0.0, w as f32, h as f32)
    }

    pub(super) fn on_mouse_down(&mut self, e: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        window.focus(&self.focus);
        let Some(p) = self.to_image(e.position) else { return };
        let tol = 6.0 * self.image_per_logical();

        if let Some(c) = self.crop {
            let full = self.full_rect();
            let handles = crop_handles(c.rect, self.crop_ratio().is_none());
            self.drag = Some(if let Some(idx) = handles.iter().position(|h| h.dist(p) <= tol * 1.8) {
                Drag::CropHandle { idx }
            } else if c.rect.contains(p) {
                Drag::CropMove { last: p }
            } else {
                Drag::CropNew { start: pt(p.x.clamp(0.0, full.w), p.y.clamp(0.0, full.h)) }
            });
            cx.notify();
            return;
        }

        if let Some(sel) = self.single_selected() {
            let id = sel.id;
            if let Some(idx) = sel.handles().iter().position(|h| h.dist(p) <= tol * 1.6) {
                self.commit_text();
                self.checkpoint();
                self.drag = Some(Drag::Handle { id, idx });
                cx.notify();
                return;
            }
        }

        let hit = self
            .doc
            .annotations
            .iter()
            .rev()
            .find(|a| a.hit(p, tol))
            .map(|a| (a.id, matches!(a.shape, Shape::Text { .. })));

        if let (Some((id, _)), Some(Typing::Text(editing))) = (hit, self.typing) {
            if id == editing {
                return;
            }
        }
        self.commit_text();

        let shift = e.modifiers.shift;
        let on_selected = hit.is_some_and(|(id, _)| self.selected.contains(&id));
        if self.tool == Tool::Select || on_selected {
            match hit {
                Some((id, _)) if shift => {
                    if let Some(i) = self.selected.iter().position(|s| *s == id) {
                        self.selected.remove(i);
                    } else {
                        self.selected.push(id);
                    }
                }
                Some((id, is_text)) => {
                    if !self.selected.contains(&id) {
                        self.selected = vec![id];
                    }
                    self.checkpoint();
                    if is_text && e.click_count >= 2 {
                        self.selected = vec![id];
                        self.typing = Some(Typing::Text(id));
                    } else {
                        self.drag = Some(Drag::Move { last: p });
                    }
                }
                None => {
                    let base = if shift { self.selected.clone() } else { Vec::new() };
                    self.selected = base.clone();
                    self.drag = Some(Drag::Marquee { start: p, now: p, base });
                }
            }
            cx.notify();
            return;
        }

        if self.tool == Tool::Text {
            if let Some((id, true)) = hit {
                self.selected = vec![id];
                self.checkpoint();
                self.typing = Some(Typing::Text(id));
                cx.notify();
                return;
            }
        }

        let dpi = self.doc.dpi;
        let ink = self.ink.clone();
        let rect0 = R::from_pts(p, p);
        let shape = match self.tool {
            Tool::Arrow => Shape::Arrow { from: p, to: p },
            Tool::Line => Shape::Line { from: p, to: p },
            Tool::Rect => Shape::Rect { rect: rect0, filled: false },
            Tool::FilledRect => Shape::Rect { rect: rect0, filled: true },
            Tool::Ellipse => Shape::Ellipse { rect: rect0, filled: false },
            Tool::Highlight => Shape::Highlight { rect: rect0 },
            Tool::Pixelate => Shape::Redact { rect: rect0, kind: crate::annotate::RedactStyle::Pixelate, strength: ink.redact_strength * dpi },
            Tool::Blur => Shape::Redact { rect: rect0, kind: crate::annotate::RedactStyle::Blur, strength: ink.redact_strength * dpi },
            Tool::Pen => Shape::Pen { points: vec![p] },
            Tool::Text => {
                let size = ink.text_size * dpi;
                Shape::Text { pos: pt(p.x, p.y - size * 0.62), text: String::new(), size, style: ink.text_style, font: ink.font.clone() }
            }
            Tool::Counter => {
                let n = self
                    .doc
                    .annotations
                    .iter()
                    .filter_map(|a| match a.shape {
                        Shape::Counter { n, .. } => Some(n),
                        _ => None,
                    })
                    .max()
                    .unwrap_or(0)
                    + 1;
                Shape::Counter { center: p, n, size: ink.text_size * 0.5 * dpi }
            }
            Tool::Select | Tool::Crop => return,
        };
        self.checkpoint();
        let id = next_id();
        self.doc.annotations.push(Annotation { id, shape, color: ink.color, width: ink.stroke * dpi });
        self.selected = vec![id];
        match self.tool {
            Tool::Text => self.typing = Some(Typing::Text(id)),
            Tool::Counter => {}
            _ => self.drag = Some(Drag::Create { id, start: p }),
        }
        cx.notify();
    }

    pub(super) fn on_drag_move(&mut self, pos: Point<Pixels>, mods: Modifiers, cx: &mut Context<Self>) {
        let Some(p) = self.to_image(pos) else { return };
        let full = self.full_rect();
        let clamp = |q: Pt| pt(q.x.clamp(0.0, full.w), q.y.clamp(0.0, full.h));
        match self.drag.take() {
            Some(Drag::Create { id, start }) => {
                if let Some(a) = self.ann_mut(id) {
                    match &mut a.shape {
                        Shape::Arrow { to, .. } | Shape::Line { to, .. } => *to = if mods.shift { snap_angle(start, p) } else { p },
                        Shape::Rect { rect, .. } | Shape::Ellipse { rect, .. } | Shape::Highlight { rect } | Shape::Redact { rect, .. } => {
                            *rect = R::from_pts(start, if mods.shift { square(start, p) } else { p });
                        }
                        Shape::Pen { points } => {
                            if points.last().is_none_or(|l| l.dist(p) > 1.5) {
                                points.push(p);
                            }
                        }
                        _ => {}
                    }
                }
                self.drag = Some(Drag::Create { id, start });
            }
            Some(Drag::Move { last }) => {
                let (dx, dy) = (p.x - last.x, p.y - last.y);
                let sel = self.selected.clone();
                for a in self.doc.annotations.iter_mut().filter(|a| sel.contains(&a.id)) {
                    a.translate(dx, dy);
                }
                self.drag = Some(Drag::Move { last: p });
            }
            Some(Drag::Handle { id, idx }) => {
                if let Some(a) = self.ann_mut(id) {
                    a.set_handle(idx, p);
                }
                self.drag = Some(Drag::Handle { id, idx });
            }
            Some(Drag::Marquee { start, base, .. }) => {
                let m = R::from_pts(start, p);
                let mut sel = base.clone();
                for a in &self.doc.annotations {
                    if a.bounds().intersect(&m).is_some() && !sel.contains(&a.id) {
                        sel.push(a.id);
                    }
                }
                self.selected = sel;
                self.drag = Some(Drag::Marquee { start, now: p, base });
            }
            Some(Drag::CropNew { start }) => {
                let end = clamp(p);
                let rect = match self.crop_ratio() {
                    Some(r) => ratio_rect(start, end, r, full),
                    None => R::from_pts(start, end),
                };
                if let Some(c) = &mut self.crop {
                    c.rect = rect.round_out().intersect(&full).unwrap_or(c.rect);
                }
                self.drag = Some(Drag::CropNew { start });
            }
            Some(Drag::CropMove { last }) => {
                if let Some(c) = &mut self.crop {
                    let r = c.rect;
                    let x = (r.x + p.x - last.x).clamp(0.0, full.w - r.w);
                    let y = (r.y + p.y - last.y).clamp(0.0, full.h - r.h);
                    let moved = R::new(x, y, r.w, r.h);
                    let applied = pt(last.x + (moved.x - r.x), last.y + (moved.y - r.y));
                    c.rect = moved;
                    self.drag = Some(Drag::CropMove { last: applied });
                }
            }
            Some(Drag::CropHandle { idx }) => {
                let ratio = self.crop_ratio();
                if let Some(c) = &mut self.crop {
                    let r = c.rect;
                    let q = clamp(p);
                    let opposite = [pt(r.right(), r.bottom()), pt(r.x, r.bottom()), pt(r.x, r.y), pt(r.right(), r.y)];
                    let next = match (idx, ratio) {
                        (0..=3, Some(k)) => ratio_rect(opposite[idx], q, k, full),
                        (0..=3, None) => R::from_pts(opposite[idx], q),
                        (4, _) => R::from_pts(pt(r.x, q.y), pt(r.right(), r.bottom())),
                        (5, _) => R::from_pts(pt(r.x, r.y), pt(q.x, r.bottom())),
                        (6, _) => R::from_pts(pt(r.x, r.y), pt(r.right(), q.y)),
                        _ => R::from_pts(pt(q.x, r.y), pt(r.right(), r.bottom())),
                    };
                    if next.w >= 8.0 && next.h >= 8.0 {
                        c.rect = next.round_out().intersect(&full).unwrap_or(r);
                    }
                }
                self.drag = Some(Drag::CropHandle { idx });
            }
            None => return,
        }
        cx.notify();
    }

    pub(super) fn on_drag_end(&mut self, cx: &mut Context<Self>) {
        match self.drag.take() {
            Some(Drag::Create { id, .. }) => {
                let tiny = self.doc.annotations.iter().find(|a| a.id == id).is_some_and(|a| {
                    let b = a.bounds();
                    match a.shape {
                        Shape::Arrow { .. } | Shape::Line { .. } => b.w.max(b.h) < a.width * 4.0 + 4.0,
                        Shape::Pen { ref points } => points.len() < 2,
                        _ => b.w < 4.0 || b.h < 4.0,
                    }
                });
                if tiny {
                    self.doc.annotations.retain(|a| a.id != id);
                    self.selected.retain(|s| *s != id);
                    self.history.undo.pop();
                }
            }
            Some(Drag::CropNew { .. }) => {
                if let Some(c) = &mut self.crop {
                    if c.rect.w < 8.0 || c.rect.h < 8.0 {
                        let (w, h) = (self.base.width(), self.base.height());
                        c.rect = R::new(0.0, 0.0, w as f32, h as f32);
                    }
                }
            }
            Some(Drag::Marquee { .. } | Drag::CropMove { .. } | Drag::CropHandle { .. }) => {}
            Some(Drag::Move { .. } | Drag::Handle { .. }) => self.drop_noop_checkpoint(),
            None => return,
        }
        cx.notify();
    }

    fn type_into(&mut self, typing: Typing, key: &str, key_char: Option<&str>, shift: bool) -> bool {
        let text = match typing {
            Typing::Text(id) => match self.ann_mut(id) {
                Some(Annotation { shape: Shape::Text { text, .. }, .. }) => text,
                _ => return true,
            },
            Typing::Watermark => &mut self.doc.effects.watermark.text,
        };
        match key {
            "escape" => return true,
            "enter" if !shift || typing == Typing::Watermark => return true,
            "enter" => text.push('\n'),
            "space" => text.push(' '),
            "backspace" => {
                text.pop();
            }
            _ => {
                if let Some(ch) = key_char {
                    text.push_str(ch);
                }
            }
        }
        false
    }

    pub(super) fn on_key(&mut self, e: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let k = &e.keystroke;
        let (ctrl, shift) = (k.modifiers.control, k.modifiers.shift);
        let key = k.key.as_str();

        if let Some(typing) = self.typing {
            if !ctrl {
                if typing == Typing::Watermark && self.history.last.map(|l| l.0) != Some("wm-text") {
                    self.checkpoint_for("wm-text");
                }
                if self.type_into(typing, key, k.key_char.as_deref(), shift) {
                    self.commit_text();
                }
                cx.stop_propagation();
                cx.notify();
                return;
            }
        }

        match (ctrl, shift, key) {
            (true, false, "c") => self.copy(cx),
            (true, false, "s") => self.commit(AfterSave::Export { ask: false }, window, cx),
            (true, true, "s") => self.commit(AfterSave::Export { ask: true }, window, cx),
            (true, false, "enter") => self.commit(AfterSave::Close, window, cx),
            (true, false, "z") => self.undo(),
            (true, true, "z") | (true, _, "y") => self.redo(),
            (true, false, "p") => self.pin(cx),
            (true, true, "r") => self.auto_redact(cx),
            (true, true, "t") => self.copy_text(cx),
            (true, false, "a") => {
                self.set_tool(Tool::Select);
                self.selected = self.doc.annotations.iter().map(|a| a.id).collect();
            }
            (true, _, "=" | "+") => self.zoom_step(true),
            (true, _, "-") => self.zoom_step(false),
            (true, _, "0") => self.zoom_to(Some(1.0), None),
            (true, _, "1") => self.zoom_to(None, None),
            (true, false, "w") => self.request_close(window, cx),
            (false, _, "delete" | "backspace") => self.delete_selected(),
            (false, _, "escape") => {
                if self.crop.is_some() {
                    self.set_tool(Tool::Select);
                } else {
                    self.selected.clear();
                }
            }
            (false, _, "enter") if self.crop.is_some() => self.apply_crop(),
            (false, _, "left" | "right" | "up" | "down") => {
                let d = if shift { 10.0 } else { 1.0 };
                let (dx, dy) = match key {
                    "left" => (-d, 0.0),
                    "right" => (d, 0.0),
                    "up" => (0.0, -d),
                    _ => (0.0, d),
                };
                self.modify_selected("nudge", |a| a.translate(dx, dy));
            }
            (false, false, _) => {
                if let Some(t) = Tool::from_key(key) {
                    self.set_tool(t);
                }
            }
            _ => return,
        }
        cx.notify();
    }
}
