//! Annotation shapes drawn on top of a screenshot. All coordinates are in source-image pixels.

use serde::{Deserialize, Serialize};
use crate::style::Color;

#[derive(Clone, Copy, Debug, PartialEq, Default)]
pub struct Pt {
    pub x: f32,
    pub y: f32,
}

pub fn pt(x: f32, y: f32) -> Pt {
    Pt { x, y }
}

impl Pt {
    pub fn dist(self, o: Pt) -> f32 {
        ((self.x - o.x).powi(2) + (self.y - o.y).powi(2)).sqrt()
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Default)]
pub struct R {
    pub x: f32,
    pub y: f32,
    pub w: f32,
    pub h: f32,
}

impl R {
    pub fn from_pts(a: Pt, b: Pt) -> R {
        R {
            x: a.x.min(b.x),
            y: a.y.min(b.y),
            w: (a.x - b.x).abs(),
            h: (a.y - b.y).abs(),
        }
    }
    pub fn right(&self) -> f32 {
        self.x + self.w
    }
    pub fn bottom(&self) -> f32 {
        self.y + self.h
    }
    pub fn contains(&self, p: Pt) -> bool {
        p.x >= self.x && p.x <= self.right() && p.y >= self.y && p.y <= self.bottom()
    }
    pub fn inflate(&self, d: f32) -> R {
        R {
            x: self.x - d,
            y: self.y - d,
            w: self.w + 2.0 * d,
            h: self.h + 2.0 * d,
        }
    }
    pub fn union(&self, o: &R) -> R {
        let x = self.x.min(o.x);
        let y = self.y.min(o.y);
        R {
            x,
            y,
            w: self.right().max(o.right()) - x,
            h: self.bottom().max(o.bottom()) - y,
        }
    }
    pub fn intersect(&self, o: &R) -> Option<R> {
        let x = self.x.max(o.x);
        let y = self.y.max(o.y);
        let r = self.right().min(o.right());
        let b = self.bottom().min(o.bottom());
        (r > x && b > y).then(|| R {
            x,
            y,
            w: r - x,
            h: b - y,
        })
    }
    pub fn area(&self) -> f32 {
        self.w * self.h
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TextStyle {
    Plain,
    Outline,
    Pill,
}

#[derive(Clone, Debug, PartialEq)]
pub enum Shape {
    Arrow { from: Pt, to: Pt },
    Line { from: Pt, to: Pt },
    Rect { rect: R, filled: bool },
    Ellipse { rect: R, filled: bool },
    Highlight { rect: R },
    Pen { points: Vec<Pt> },
    Text { pos: Pt, text: String, size: f32, style: TextStyle },
    Counter { center: Pt, n: u32, size: f32 },
    Redact { rect: R, kind: RedactStyle },
    Spotlight { rect: R },
}

#[derive(Clone, Debug, PartialEq)]
pub struct Annotation {
    pub id: u64,
    pub shape: Shape,
    pub color: Color,
    pub width: f32,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tool {
    Select,
    Arrow,
    Rect,
    Ellipse,
    Line,
    Pen,
    Text,
    Highlight,
    Counter,
    Redact,
    Spotlight,
    Crop,
}

impl Tool {
    pub const ALL: [Tool; 12] = [
        Tool::Select,
        Tool::Arrow,
        Tool::Rect,
        Tool::Ellipse,
        Tool::Line,
        Tool::Pen,
        Tool::Text,
        Tool::Highlight,
        Tool::Counter,
        Tool::Redact,
        Tool::Spotlight,
        Tool::Crop,
    ];
    pub fn icon(self) -> &'static str {
        match self {
            Tool::Select => "select",
            Tool::Arrow => "arrow",
            Tool::Rect => "rect",
            Tool::Ellipse => "ellipse",
            Tool::Line => "line",
            Tool::Pen => "pen",
            Tool::Text => "text",
            Tool::Highlight => "highlight",
            Tool::Counter => "counter",
            Tool::Redact => "redact",
            Tool::Spotlight => "spotlight",
            Tool::Crop => "crop",
        }
    }
    pub fn name(self) -> &'static str {
        match self {
            Tool::Select => "Select (V)",
            Tool::Arrow => "Arrow (A)",
            Tool::Rect => "Rectangle (R)",
            Tool::Ellipse => "Ellipse (O)",
            Tool::Line => "Line (L)",
            Tool::Pen => "Pen (P)",
            Tool::Text => "Text (T)",
            Tool::Highlight => "Highlighter (H)",
            Tool::Counter => "Step counter (N)",
            Tool::Redact => "Redact (B)",
            Tool::Spotlight => "Spotlight (S)",
            Tool::Crop => "Crop (C)",
        }
    }
    pub fn from_key(key: &str) -> Option<Tool> {
        Some(match key {
            "v" => Tool::Select,
            "a" => Tool::Arrow,
            "r" => Tool::Rect,
            "o" => Tool::Ellipse,
            "l" => Tool::Line,
            "p" => Tool::Pen,
            "t" => Tool::Text,
            "h" => Tool::Highlight,
            "n" => Tool::Counter,
            "b" => Tool::Redact,
            "s" => Tool::Spotlight,
            "c" => Tool::Crop,
            _ => return None,
        })
    }
}

impl Annotation {
    pub fn is_redaction(&self) -> bool {
        matches!(self.shape, Shape::Redact { .. })
    }

    /// Approximate bounds used for selection and hit testing.
    pub fn bounds(&self) -> R {
        let pad = self.width;
        match &self.shape {
            Shape::Arrow { from, to } | Shape::Line { from, to } => {
                R::from_pts(*from, *to).inflate(pad * 2.0)
            }
            Shape::Rect { rect, .. }
            | Shape::Ellipse { rect, .. }
            | Shape::Highlight { rect }
            | Shape::Redact { rect, .. }
            | Shape::Spotlight { rect } => *rect,
            Shape::Pen { points } => {
                let mut r = R::from_pts(points[0], points[0]);
                for p in points {
                    r = r.union(&R::from_pts(*p, *p));
                }
                r.inflate(pad)
            }
            Shape::Text {
                pos, text, size, ..
            } => {
                let (w, h) = crate::render::measure_text(text, *size);
                R {
                    x: pos.x,
                    y: pos.y,
                    w: w.max(*size * 0.5),
                    h,
                }
                .inflate(size * 0.3)
            }
            Shape::Counter { center, size, .. } => R {
                x: center.x - size,
                y: center.y - size,
                w: size * 2.0,
                h: size * 2.0,
            },
        }
    }

    pub fn hit(&self, p: Pt, tolerance: f32) -> bool {
        match &self.shape {
            Shape::Arrow { from, to } | Shape::Line { from, to } => {
                seg_dist(p, *from, *to) <= self.width + tolerance
            }
            Shape::Rect { rect, filled: false } => {
                let outer = rect.inflate(self.width + tolerance);
                let inner = rect.inflate(-(self.width + tolerance));
                outer.contains(p) && !(inner.w > 0.0 && inner.h > 0.0 && inner.contains(p))
            }
            Shape::Ellipse { rect, filled: false } => {
                let (cx, cy) = (rect.x + rect.w / 2.0, rect.y + rect.h / 2.0);
                let (rx, ry) = (rect.w / 2.0, rect.h / 2.0);
                if rx < 1.0 || ry < 1.0 {
                    return rect.inflate(tolerance).contains(p);
                }
                let d = (((p.x - cx) / rx).powi(2) + ((p.y - cy) / ry).powi(2)).sqrt();
                (d - 1.0).abs() * rx.min(ry) <= self.width + tolerance
            }
            Shape::Pen { points } => points
                .windows(2)
                .any(|w| seg_dist(p, w[0], w[1]) <= self.width + tolerance),
            _ => self.bounds().inflate(tolerance).contains(p),
        }
    }

    pub fn translate(&mut self, dx: f32, dy: f32) {
        let mv = |p: &mut Pt| {
            p.x += dx;
            p.y += dy;
        };
        match &mut self.shape {
            Shape::Arrow { from, to } | Shape::Line { from, to } => {
                mv(from);
                mv(to);
            }
            Shape::Rect { rect, .. }
            | Shape::Ellipse { rect, .. }
            | Shape::Highlight { rect }
            | Shape::Redact { rect, .. }
            | Shape::Spotlight { rect } => {
                rect.x += dx;
                rect.y += dy;
            }
            Shape::Pen { points } => points.iter_mut().for_each(mv),
            Shape::Text { pos, .. } => mv(pos),
            Shape::Counter { center, .. } => mv(center),
        }
    }

    /// Draggable control points.
    pub fn handles(&self) -> Vec<Pt> {
        match &self.shape {
            Shape::Arrow { from, to } | Shape::Line { from, to } => vec![*from, *to],
            Shape::Rect { rect, .. }
            | Shape::Ellipse { rect, .. }
            | Shape::Highlight { rect }
            | Shape::Redact { rect, .. }
            | Shape::Spotlight { rect } => vec![
                pt(rect.x, rect.y),
                pt(rect.right(), rect.y),
                pt(rect.right(), rect.bottom()),
                pt(rect.x, rect.bottom()),
            ],
            _ => vec![],
        }
    }

    pub fn set_handle(&mut self, i: usize, p: Pt) {
        match &mut self.shape {
            Shape::Arrow { from, to } | Shape::Line { from, to } => {
                if i == 0 {
                    *from = p
                } else {
                    *to = p
                }
            }
            Shape::Rect { rect, .. }
            | Shape::Ellipse { rect, .. }
            | Shape::Highlight { rect }
            | Shape::Redact { rect, .. }
            | Shape::Spotlight { rect } => {
                // The opposite corner stays fixed.
                let opposite = match i {
                    0 => pt(rect.right(), rect.bottom()),
                    1 => pt(rect.x, rect.bottom()),
                    2 => pt(rect.x, rect.y),
                    _ => pt(rect.right(), rect.y),
                };
                *rect = R::from_pts(opposite, p);
            }
            _ => {}
        }
    }
}

fn seg_dist(p: Pt, a: Pt, b: Pt) -> f32 {
    let (dx, dy) = (b.x - a.x, b.y - a.y);
    let len2 = dx * dx + dy * dy;
    if len2 < 1e-6 {
        return p.dist(a);
    }
    let t = (((p.x - a.x) * dx + (p.y - a.y) * dy) / len2).clamp(0.0, 1.0);
    p.dist(pt(a.x + t * dx, a.y + t * dy))
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum RedactStyle {
    Blur,
    Pixelate,
    Solid,
}

impl RedactStyle {
    pub const ALL: [RedactStyle; 3] = [RedactStyle::Blur, RedactStyle::Pixelate, RedactStyle::Solid];
    pub fn label(self) -> &'static str {
        match self {
            RedactStyle::Blur => "Blur",
            RedactStyle::Pixelate => "Pixelate",
            RedactStyle::Solid => "Solid",
        }
    }
}
