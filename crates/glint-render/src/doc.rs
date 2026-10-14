//! `EditDoc`: everything the screenshot editor changes, stored next to the untouched base image
//! as `edit.json`. Rendering `(base, EditDoc)` reproduces the deliverable exactly.

use crate::annotate::{Annotation, R};
use crate::fonts::TextFont;
use crate::style::{Color, Style};
use serde::{Deserialize, Serialize};

pub const DOC_VERSION: u32 = 1;

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct EditDoc {
    pub version: u32,
    /// Scale factor of the monitor the capture came from; point sizes are multiplied by it.
    pub dpi: f32,
    /// Captured window title, shown by window chrome.
    pub window_title: Option<String>,
    pub style: Style,
    pub effects: Effects,
    /// Kept region of the base image in base pixels; `None` keeps the whole image.
    pub crop: Option<R>,
    /// In pixels of the cropped image.
    pub annotations: Vec<Annotation>,
}

impl Default for EditDoc {
    fn default() -> Self {
        Self {
            version: DOC_VERSION,
            dpi: 1.0,
            window_title: None,
            style: Style::default(),
            effects: Effects::default(),
            crop: None,
            annotations: Vec::new(),
        }
    }
}

impl EditDoc {
    pub fn new(style: Style, dpi: f32, window_title: Option<String>) -> Self {
        Self { style, dpi, window_title, ..Default::default() }
    }

    pub fn from_json(json: &str) -> anyhow::Result<Self> {
        let doc: EditDoc = serde_json::from_str(json)?;
        anyhow::ensure!(doc.version <= DOC_VERSION, "edit.json version {} is newer than this app supports", doc.version);
        Ok(doc)
    }

    pub fn to_json(&self) -> String {
        serde_json::to_string_pretty(self).expect("EditDoc serializes")
    }

    /// The kept region, clamped to a `w`×`h` base image.
    pub fn crop_rect(&self, w: u32, h: u32) -> R {
        let full = R::new(0.0, 0.0, w as f32, h as f32);
        self.crop.and_then(|c| c.round_out().intersect(&full)).unwrap_or(full)
    }

    /// Replaces the crop and moves annotations so they stay on the same base pixels.
    pub fn set_crop(&mut self, crop: R, w: u32, h: u32) {
        let old = self.crop_rect(w, h);
        let full = R::new(0.0, 0.0, w as f32, h as f32);
        let new = crop.round_out().intersect(&full).unwrap_or(full);
        let (dx, dy) = (old.x - new.x, old.y - new.y);
        for a in &mut self.annotations {
            a.translate(dx, dy);
        }
        self.crop = (new != full).then_some(new);
    }
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Effects {
    pub border: Border,
    pub watermark: Watermark,
    pub perspective: Perspective,
    pub blur: ProgressiveBlur,
}

/// Solid border around the screenshot card.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Border {
    pub enabled: bool,
    /// Points.
    pub width: f32,
    pub color: Color,
}

impl Default for Border {
    fn default() -> Self {
        Self { enabled: false, width: 6.0, color: Color::WHITE }
    }
}

impl Border {
    /// Width in points, zero when off.
    pub fn points(&self) -> f32 {
        if self.enabled { self.width.max(0.0) } else { 0.0 }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Placement {
    /// 0..=8, row-major 3x3 grid over the screenshot.
    Grid(u8),
    /// Repeated across the screenshot, rotated by `Watermark::rotation`.
    Tiled,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Watermark {
    pub enabled: bool,
    pub text: String,
    pub font: TextFont,
    /// Points.
    pub size: f32,
    pub opacity: f32,
    pub color: Color,
    pub placement: Placement,
    /// Degrees, tiled placement only.
    pub rotation: f32,
}

impl Default for Watermark {
    fn default() -> Self {
        Self {
            enabled: false,
            text: String::new(),
            font: TextFont::default(),
            size: 22.0,
            opacity: 0.6,
            color: Color::WHITE,
            placement: Placement::Grid(8),
            rotation: -30.0,
        }
    }
}

impl Watermark {
    pub fn is_visible(&self) -> bool {
        self.enabled && !self.text.trim().is_empty() && self.opacity > 0.0
    }
}

/// Camera-style mockup: the card is a plane rotated in 3D and seen through a pinhole camera.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Perspective {
    /// Degrees around the card's horizontal axis (top edge leans away when positive).
    pub rot_x: f32,
    /// Degrees around the card's vertical axis.
    pub rot_y: f32,
    /// Degrees around the viewing axis.
    pub rot_z: f32,
    pub zoom: f32,
    /// Fractions of the canvas size.
    pub pan_x: f32,
    pub pan_y: f32,
    /// Lens angle in degrees; smaller is flatter.
    pub fov: f32,
}

impl Default for Perspective {
    fn default() -> Self {
        Self { rot_x: 0.0, rot_y: 0.0, rot_z: 0.0, zoom: 1.0, pan_x: 0.0, pan_y: 0.0, fov: 24.0 }
    }
}

impl Perspective {
    pub fn is_active(&self) -> bool {
        let d = Perspective::default();
        [self.rot_x, self.rot_y, self.rot_z, self.pan_x, self.pan_y].iter().any(|v| v.abs() > 1e-4)
            || (self.zoom - d.zoom).abs() > 1e-4
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BlurMode {
    Radial,
    Directional,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum BlurEdge {
    /// Only the screenshot blurs.
    Clipped,
    /// The whole scene blurs, background included.
    Bleed,
}

/// Sharp focus in the middle that falls off into blur, radially or across a band.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct ProgressiveBlur {
    pub enabled: bool,
    pub mode: BlurMode,
    pub edge: BlurEdge,
    /// 0..100; the blur radius is `strength / 1000` of the blurred area's shortest edge.
    pub strength: f32,
    /// 0..1: size of the sharp area, from a small spot to the farthest edge.
    pub start: f32,
    /// Degrees; direction of the sharp band in directional mode (0 = horizontal).
    pub angle: f32,
}

impl Default for ProgressiveBlur {
    fn default() -> Self {
        Self { enabled: false, mode: BlurMode::Radial, edge: BlurEdge::Bleed, strength: 30.0, start: 0.35, angle: 0.0 }
    }
}

impl ProgressiveBlur {
    pub fn is_active(&self) -> bool {
        self.enabled && self.strength > 0.01
    }
}

/// The largest rect of `ratio` (width / height) centered in `within`, in whole pixels.
pub fn ratio_crop(within: R, ratio: f32) -> R {
    let (mut w, mut h) = (within.w, within.w / ratio);
    if h > within.h {
        h = within.h;
        w = h * ratio;
    }
    let (w, h) = (w.round().max(1.0), h.round().max(1.0));
    R::new((within.x + (within.w - w) / 2.0).round(), (within.y + (within.h - h) / 2.0).round(), w, h)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::annotate::{pt, RedactStyle, Shape, TextStyle};
    use crate::fonts::{Align, Weight};
    use crate::style::{Background, Gradient};

    fn sample() -> EditDoc {
        let mut doc = EditDoc::new(Style::default(), 1.5, Some("Notepad".into()));
        doc.style.background = Background::Image("C:\\Users\\me\\Pictures\\beach.jpg".into());
        doc.style.padding = 40.0;
        doc.effects.border = Border { enabled: true, width: 4.0, color: Color::rgb(0x112233) };
        doc.effects.watermark = Watermark { enabled: true, text: "Draft".into(), placement: Placement::Tiled, ..Default::default() };
        doc.effects.perspective.rot_y = 20.0;
        doc.effects.blur = ProgressiveBlur { enabled: true, mode: BlurMode::Directional, edge: BlurEdge::Clipped, ..Default::default() };
        doc.crop = Some(R::new(10.0, 20.0, 300.0, 200.0));
        doc.annotations = vec![
            Annotation { id: 1, shape: Shape::Arrow { from: pt(1.0, 2.0), to: pt(30.0, 40.0) }, color: Color::rgb(0xFF0000), width: 5.0 },
            Annotation {
                id: 2,
                shape: Shape::Text {
                    pos: pt(5.0, 6.0),
                    text: "Hi\nthere".into(),
                    size: 28.0,
                    style: TextStyle::Pill,
                    font: TextFont { family: "Georgia".into(), weight: Weight::Bold, align: Align::Center },
                },
                color: Color::WHITE,
                width: 2.0,
            },
            Annotation { id: 3, shape: Shape::Redact { rect: R::new(0.0, 0.0, 50.0, 10.0), kind: RedactStyle::Pixelate, strength: 7.0 }, color: Color::BLACK, width: 0.0 },
            Annotation { id: 4, shape: Shape::Highlight { rect: R::new(1.0, 1.0, 2.0, 2.0) }, color: Color::BLACK, width: 0.0 },
        ];
        doc
    }

    #[test]
    fn edit_doc_round_trips_through_json() {
        let doc = sample();
        let json = doc.to_json();
        assert!(json.contains("\"version\": 1"));
        let back = EditDoc::from_json(&json).unwrap();
        assert_eq!(back, doc);
    }

    #[test]
    fn background_only_doc_round_trips() {
        let mut doc = EditDoc::default();
        doc.style.background = Background::Gradient(Gradient { stops: vec![Color::rgb(0x123456), Color::WHITE], angle: 30.0 });
        assert_eq!(EditDoc::from_json(&doc.to_json()).unwrap(), doc);
    }

    #[test]
    fn missing_fields_take_defaults_and_newer_versions_are_rejected() {
        let doc = EditDoc::from_json(r#"{"version":1,"dpi":2.0}"#).unwrap();
        assert_eq!(doc.dpi, 2.0);
        assert_eq!(doc.effects, Effects::default());
        assert!(EditDoc::from_json(r#"{"version":99}"#).is_err());
    }

    #[test]
    fn crop_remaps_annotations_onto_the_same_base_pixels() {
        let mut doc = EditDoc::default();
        doc.annotations = vec![Annotation { id: 1, shape: Shape::Rect { rect: R::new(100.0, 50.0, 20.0, 10.0), filled: false }, color: Color::BLACK, width: 2.0 }];
        doc.set_crop(R::new(40.0, 30.0, 200.0, 100.0), 400, 300);
        assert_eq!(doc.crop, Some(R::new(40.0, 30.0, 200.0, 100.0)));
        assert_eq!(doc.annotations[0].shape, Shape::Rect { rect: R::new(60.0, 20.0, 20.0, 10.0), filled: false });
        doc.set_crop(R::new(50.0, 35.0, 100.0, 100.0), 400, 300);
        assert_eq!(doc.annotations[0].shape, Shape::Rect { rect: R::new(50.0, 15.0, 20.0, 10.0), filled: false });
        doc.set_crop(R::new(0.0, 0.0, 400.0, 300.0), 400, 300);
        assert_eq!(doc.crop, None, "the full image is stored as no crop");
        assert_eq!(doc.annotations[0].shape, Shape::Rect { rect: R::new(100.0, 50.0, 20.0, 10.0), filled: false });
    }

    #[test]
    fn ratio_crop_is_centered_and_maximal() {
        assert_eq!(ratio_crop(R::new(0.0, 0.0, 1600.0, 900.0), 1.0), R::new(350.0, 0.0, 900.0, 900.0));
        assert_eq!(ratio_crop(R::new(0.0, 0.0, 1600.0, 900.0), 9.0 / 16.0), R::new(547.0, 0.0, 506.0, 900.0));
        assert_eq!(ratio_crop(R::new(0.0, 0.0, 1000.0, 1000.0), 16.0 / 9.0), R::new(0.0, 219.0, 1000.0, 563.0));
    }
}
