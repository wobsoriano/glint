//! Annotation shapes drawn on top of a screenshot. Coordinates are pixels of the cropped image.

use crate::fonts::{self, TextFont};
use crate::style::Color;
use serde::{Deserialize, Serialize};

#[derive(Clone, Copy, Debug, PartialEq, Default, Serialize, Deserialize)]
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

#[derive(Clone, Copy, Debug, PartialEq, Default, Serialize, Deserialize)]
pub struct R {
    pub x: f32,
    pub y: f32,
    pub w: f32,
    pub h: f32,
}

impl R {
    pub fn new(x: f32, y: f32, w: f32, h: f32) -> R {
        R { x, y, w, h }
    }
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
    pub fn center(&self) -> Pt {
        pt(self.x + self.w / 2.0, self.y + self.h / 2.0)
    }
    pub fn contains(&self, p: Pt) -> bool {
        p.x >= self.x && p.x <= self.right() && p.y >= self.y && p.y <= self.bottom()
    }
    pub fn contains_rect(&self, o: &R) -> bool {
        o.x >= self.x && o.y >= self.y && o.right() <= self.right() && o.bottom() <= self.bottom()
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
    pub fn translate(&self, dx: f32, dy: f32) -> R {
        R { x: self.x + dx, y: self.y + dy, ..*self }
    }
    /// Smallest integer-aligned rect containing this one.
    pub fn round_out(&self) -> R {
        let x = self.x.floor();
        let y = self.y.floor();
        R { x, y, w: self.right().ceil() - x, h: self.bottom().ceil() - y }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum TextStyle {
    Plain,
    Outline,
    Pill,
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

/// Default redaction strength: pixel block size or blur radius, in image pixels at 1x DPI.
pub const REDACT_STRENGTH: f32 = 12.0;

fn default_strength() -> f32 {
    REDACT_STRENGTH
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(tag = "type", rename_all = "snake_case")]
pub enum Shape {
    Arrow { from: Pt, to: Pt },
    Line { from: Pt, to: Pt },
    Rect { rect: R, filled: bool },
    Ellipse { rect: R, filled: bool },
    Pen { points: Vec<Pt> },
    Text {
        pos: Pt,
        text: String,
        size: f32,
        style: TextStyle,
        #[serde(default)]
        font: TextFont,
    },
    Counter { center: Pt, n: u32, size: f32 },
    /// Blurs, pixelates or blacks out the screenshot under `rect`. `strength` is in image pixels.
    Redact {
        rect: R,
        kind: RedactStyle,
        #[serde(default = "default_strength")]
        strength: f32,
    },
    /// Keeps `rect` bright and dims the rest of the screenshot.
    Highlight { rect: R },
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Annotation {
    pub id: u64,
    pub shape: Shape,
    pub color: Color,
    pub width: f32,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Tool {
    Select,
    Rect,
    FilledRect,
    Ellipse,
    Line,
    Arrow,
    Pen,
    Counter,
    Text,
    Highlight,
    Pixelate,
    Blur,
    Crop,
}

impl Tool {
    pub const ALL: [Tool; 13] = [
        Tool::Select,
        Tool::Rect,
        Tool::FilledRect,
        Tool::Ellipse,
        Tool::Line,
        Tool::Arrow,
        Tool::Pen,
        Tool::Counter,
        Tool::Text,
        Tool::Highlight,
        Tool::Pixelate,
        Tool::Blur,
        Tool::Crop,
    ];
    pub fn icon(self) -> &'static str {
        match self {
            Tool::Select => "select",
            Tool::Rect => "rect",
            Tool::FilledRect => "rect-filled",
            Tool::Ellipse => "ellipse",
            Tool::Line => "line",
            Tool::Arrow => "arrow",
            Tool::Pen => "pen",
            Tool::Counter => "counter",
            Tool::Text => "text",
            Tool::Highlight => "spotlight",
            Tool::Pixelate => "pixelate",
            Tool::Blur => "blur",
            Tool::Crop => "crop",
        }
    }
    pub fn name(self) -> &'static str {
        match self {
            Tool::Select => "Select (V)",
            Tool::Rect => "Rectangle (R)",
            Tool::FilledRect => "Filled rectangle (F)",
            Tool::Ellipse => "Circle (O)",
            Tool::Line => "Line (L)",
            Tool::Arrow => "Arrow (A)",
            Tool::Pen => "Freehand (P)",
            Tool::Counter => "Numbered marker (N)",
            Tool::Text => "Text (T)",
            Tool::Highlight => "Highlight (H)",
            Tool::Pixelate => "Pixelate (X)",
            Tool::Blur => "Blur (B)",
            Tool::Crop => "Crop (C)",
        }
    }
    pub fn from_key(key: &str) -> Option<Tool> {
        Some(match key {
            "v" => Tool::Select,
            "r" => Tool::Rect,
            "f" => Tool::FilledRect,
            "o" => Tool::Ellipse,
            "l" => Tool::Line,
            "a" => Tool::Arrow,
            "p" => Tool::Pen,
            "n" => Tool::Counter,
            "t" => Tool::Text,
            "h" => Tool::Highlight,
            "x" => Tool::Pixelate,
            "b" => Tool::Blur,
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
            Shape::Arrow { from, to } | Shape::Line { from, to } => R::from_pts(*from, *to).inflate(pad * 2.0),
            Shape::Rect { rect, .. }
            | Shape::Ellipse { rect, .. }
            | Shape::Highlight { rect }
            | Shape::Redact { rect, .. } => *rect,
            Shape::Pen { points } => {
                let mut r = R::from_pts(points[0], points[0]);
                for p in points {
                    r = r.union(&R::from_pts(*p, *p));
                }
                r.inflate(pad)
            }
            Shape::Text { .. } => self.text_box().unwrap_or_default().inflate(self.text_size() * 0.3),
            Shape::Counter { center, size, .. } => R {
                x: center.x - size,
                y: center.y - size,
                w: size * 2.0,
                h: size * 2.0,
            },
        }
    }

    fn text_size(&self) -> f32 {
        match &self.shape {
            Shape::Text { size, .. } => *size,
            _ => 0.0,
        }
    }

    /// The laid-out text block (without padding), for text annotations.
    pub fn text_box(&self) -> Option<R> {
        let Shape::Text { pos, text, size, font, .. } = &self.shape else { return None };
        let f = fonts::font(&font.family, font.weight);
        let (w, h) = fonts::measure(f.as_deref(), text, *size);
        Some(R { x: pos.x, y: pos.y, w: w.max(size * 0.5), h })
    }

    pub fn hit(&self, p: Pt, tolerance: f32) -> bool {
        match &self.shape {
            Shape::Arrow { from, to } | Shape::Line { from, to } => seg_dist(p, *from, *to) <= self.width + tolerance,
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
            Shape::Pen { points } => points.windows(2).any(|w| seg_dist(p, w[0], w[1]) <= self.width + tolerance),
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
            | Shape::Redact { rect, .. } => *rect = rect.translate(dx, dy),
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
            | Shape::Redact { rect, .. } => vec![
                pt(rect.x, rect.y),
                pt(rect.right(), rect.y),
                pt(rect.right(), rect.bottom()),
                pt(rect.x, rect.bottom()),
            ],
            Shape::Text { .. } => {
                let b = self.text_box().unwrap_or_default();
                vec![pt(b.right(), b.bottom())]
            }
            Shape::Counter { center, size, .. } => vec![pt(center.x + size, center.y)],
            Shape::Pen { .. } => vec![],
        }
    }

    pub fn set_handle(&mut self, i: usize, p: Pt) {
        let text_box = self.text_box();
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
            | Shape::Redact { rect, .. } => {
                // The opposite corner stays fixed.
                let opposite = match i {
                    0 => pt(rect.right(), rect.bottom()),
                    1 => pt(rect.x, rect.bottom()),
                    2 => pt(rect.x, rect.y),
                    _ => pt(rect.right(), rect.y),
                };
                *rect = R::from_pts(opposite, p);
            }
            Shape::Text { pos, size, .. } => {
                let b = text_box.unwrap_or_default();
                if b.h > 0.0 {
                    *size = (*size * (p.y - pos.y) / b.h).clamp(6.0, 2000.0);
                }
            }
            Shape::Counter { center, size, .. } => *size = center.dist(p).clamp(6.0, 1000.0),
            Shape::Pen { .. } => {}
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
