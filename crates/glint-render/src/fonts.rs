use crate::style::Color;
use ab_glyph::{Font, FontVec, PxScale, ScaleFont, VariableFont};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::{Arc, LazyLock, Mutex};

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Weight {
    Light,
    Regular,
    Semibold,
    Bold,
    Black,
}

impl Weight {
    pub const ALL: [Weight; 5] = [Weight::Light, Weight::Regular, Weight::Semibold, Weight::Bold, Weight::Black];
    pub fn label(self) -> &'static str {
        match self {
            Weight::Light => "Light",
            Weight::Regular => "Regular",
            Weight::Semibold => "Semibold",
            Weight::Bold => "Bold",
            Weight::Black => "Black",
        }
    }
    fn index(self) -> usize {
        self as usize
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "snake_case")]
pub enum Align {
    Left,
    Center,
    Right,
}

#[derive(Clone, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(default)]
pub struct TextFont {
    pub family: String,
    pub weight: Weight,
    pub align: Align,
}

impl Default for TextFont {
    fn default() -> Self {
        Self { family: DEFAULT_FAMILY.into(), weight: Weight::Semibold, align: Align::Left }
    }
}

/// One face of a family: a font file, the face's index in a collection, and for variable
/// fonts the `wght` axis value.
#[derive(Clone, Copy, Debug)]
struct Face {
    file: &'static str,
    index: u32,
    wght: Option<f32>,
}

/// A static face, the only one in its file.
const fn f(file: &'static str) -> Option<Face> {
    Some(Face { file, index: 0, wght: None })
}

/// A face at `index` of a collection.
const fn c(file: &'static str, index: u32) -> Option<Face> {
    Some(Face { file, index, wght: None })
}

/// An instance of a variable font.
#[cfg(target_os = "macos")]
const fn v(file: &'static str, wght: f32) -> Option<Face> {
    Some(Face { file, index: 0, wght: Some(wght) })
}

/// Families with their Light, Regular, Semibold, Bold and Black faces. Regular is required.
#[cfg(not(target_os = "macos"))]
const CATALOG: &[(&str, [Option<Face>; 5])] = &[
    ("Segoe UI", [f("segoeuil.ttf"), f("segoeui.ttf"), f("seguisb.ttf"), f("segoeuib.ttf"), f("seguibl.ttf")]),
    ("Arial", [None, f("arial.ttf"), None, f("arialbd.ttf"), f("ariblk.ttf")]),
    ("Calibri", [f("calibril.ttf"), f("calibri.ttf"), None, f("calibrib.ttf"), None]),
    ("Cambria", [None, c("cambria.ttc", 0), None, f("cambriab.ttf"), None]),
    ("Candara", [f("Candaral.ttf"), f("Candara.ttf"), None, f("Candarab.ttf"), None]),
    ("Corbel", [f("corbell.ttf"), f("corbel.ttf"), None, f("corbelb.ttf"), None]),
    ("Constantia", [None, f("constan.ttf"), None, f("constanb.ttf"), None]),
    ("Georgia", [None, f("georgia.ttf"), None, f("georgiab.ttf"), None]),
    ("Times New Roman", [None, f("times.ttf"), None, f("timesbd.ttf"), None]),
    ("Palatino Linotype", [None, f("pala.ttf"), None, f("palab.ttf"), None]),
    ("Verdana", [None, f("verdana.ttf"), None, f("verdanab.ttf"), None]),
    ("Tahoma", [None, f("tahoma.ttf"), None, f("tahomabd.ttf"), None]),
    ("Trebuchet MS", [None, f("trebuc.ttf"), None, f("trebucbd.ttf"), None]),
    ("Bahnschrift", [None, f("bahnschrift.ttf"), None, None, None]),
    ("Consolas", [None, f("consola.ttf"), None, f("consolab.ttf"), None]),
    ("Cascadia Code", [None, f("CascadiaCode.ttf"), None, None, None]),
    ("Comic Sans MS", [None, f("comic.ttf"), None, f("comicbd.ttf"), None]),
    ("Impact", [None, f("impact.ttf"), None, None, None]),
    ("Segoe Print", [None, f("segoepr.ttf"), None, f("segoeprb.ttf"), None]),
    ("Segoe Script", [None, f("segoesc.ttf"), None, f("segoescb.ttf"), None]),
];

#[cfg(not(target_os = "macos"))]
const DEFAULT_FAMILY: &str = "Segoe UI";

#[cfg(not(target_os = "macos"))]
fn fonts_dir() -> std::path::PathBuf {
    let dir = std::env::var("WINDIR").unwrap_or_else(|_| "C:\\Windows".into());
    std::path::Path::new(&dir).join("Fonts")
}

/// Paths are relative to /System/Library/Fonts. Collection indices and variable instances come
/// from the faces CoreText lists for each file.
#[cfg(target_os = "macos")]
const CATALOG: &[(&str, [Option<Face>; 5])] = &[
    ("SF Pro", [v("SFNS.ttf", 300.), v("SFNS.ttf", 400.), v("SFNS.ttf", 600.), v("SFNS.ttf", 700.), v("SFNS.ttf", 900.)]),
    ("New York", [None, v("NewYork.ttf", 400.), v("NewYork.ttf", 600.), v("NewYork.ttf", 700.), v("NewYork.ttf", 900.)]),
    ("SF Mono", [v("SFNSMono.ttf", 300.), v("SFNSMono.ttf", 400.), v("SFNSMono.ttf", 600.), v("SFNSMono.ttf", 700.), v("SFNSMono.ttf", 800.)]),
    ("Helvetica Neue", [c("HelveticaNeue.ttc", 7), c("HelveticaNeue.ttc", 0), c("HelveticaNeue.ttc", 10), c("HelveticaNeue.ttc", 1), None]),
    ("Avenir Next", [c("Avenir Next.ttc", 10), c("Avenir Next.ttc", 7), c("Avenir Next.ttc", 2), c("Avenir Next.ttc", 0), c("Avenir Next.ttc", 8)]),
    ("Arial", [None, f("Supplemental/Arial.ttf"), None, f("Supplemental/Arial Bold.ttf"), f("Supplemental/Arial Black.ttf")]),
    ("Georgia", [None, f("Supplemental/Georgia.ttf"), None, f("Supplemental/Georgia Bold.ttf"), None]),
    ("Times New Roman", [None, f("Supplemental/Times New Roman.ttf"), None, f("Supplemental/Times New Roman Bold.ttf"), None]),
    ("Palatino", [None, c("Palatino.ttc", 0), None, c("Palatino.ttc", 2), None]),
    ("Optima", [None, c("Optima.ttc", 0), None, c("Optima.ttc", 1), c("Optima.ttc", 4)]),
    ("Verdana", [None, f("Supplemental/Verdana.ttf"), None, f("Supplemental/Verdana Bold.ttf"), None]),
    ("Tahoma", [None, f("Supplemental/Tahoma.ttf"), None, f("Supplemental/Tahoma Bold.ttf"), None]),
    ("Trebuchet MS", [None, f("Supplemental/Trebuchet MS.ttf"), None, f("Supplemental/Trebuchet MS Bold.ttf"), None]),
    ("Menlo", [None, c("Menlo.ttc", 0), None, c("Menlo.ttc", 1), None]),
    ("Courier New", [None, f("Supplemental/Courier New.ttf"), None, f("Supplemental/Courier New Bold.ttf"), None]),
    ("Comic Sans MS", [None, f("Supplemental/Comic Sans MS.ttf"), None, f("Supplemental/Comic Sans MS Bold.ttf"), None]),
    ("Impact", [None, f("Supplemental/Impact.ttf"), None, None, None]),
];

#[cfg(target_os = "macos")]
const DEFAULT_FAMILY: &str = "SF Pro";

#[cfg(target_os = "macos")]
fn fonts_dir() -> std::path::PathBuf {
    "/System/Library/Fonts".into()
}

/// Families from the catalog whose regular face is installed.
pub fn families() -> &'static [&'static str] {
    static INSTALLED: LazyLock<Vec<&'static str>> = LazyLock::new(|| {
        let dir = fonts_dir();
        CATALOG
            .iter()
            .filter(|(_, faces)| faces[1].is_some_and(|f| dir.join(f.file).exists()))
            .map(|(name, _)| *name)
            .collect()
    });
    &INSTALLED
}

/// Weights with a real face for `family`.
pub fn weights(family: &str) -> Vec<Weight> {
    let Some((_, faces)) = CATALOG.iter().find(|(n, _)| *n == family) else {
        return vec![Weight::Regular];
    };
    Weight::ALL.into_iter().filter(|w| faces[w.index()].is_some()).collect()
}

type FontCache = Mutex<HashMap<(String, Weight), Option<Arc<FontVec>>>>;
static CACHE: LazyLock<FontCache> = LazyLock::new(Default::default);

fn load(face: Face) -> Option<FontVec> {
    let bytes = std::fs::read(fonts_dir().join(face.file)).ok()?;
    let mut font = FontVec::try_from_vec_and_index(bytes, face.index).ok()?;
    if let Some(wght) = face.wght {
        font.set_variation(b"wght", wght);
    }
    Some(font)
}

/// The face for `family` at the nearest available weight, falling back to the platform's
/// default family, then Arial.
pub fn font(family: &str, weight: Weight) -> Option<Arc<FontVec>> {
    let key = (family.to_string(), weight);
    if let Some(f) = CACHE.lock().unwrap().get(&key) {
        return f.clone();
    }
    let loaded = [family, DEFAULT_FAMILY, "Arial"].iter().find_map(|fam| {
        let (_, faces) = CATALOG.iter().find(|(n, _)| n == fam)?;
        let want = weight.index() as i32;
        let mut order: Vec<usize> = (0..5).filter(|i| faces[*i].is_some()).collect();
        order.sort_by_key(|i| ((*i as i32 - want).abs(), -(*i as i32)));
        order.into_iter().find_map(|i| load(faces[i]?)).map(Arc::new)
    });
    CACHE.lock().unwrap().insert(key, loaded.clone());
    loaded
}

pub fn default_font() -> Option<Arc<FontVec>> {
    let f = TextFont::default();
    font(&f.family, f.weight)
}

pub const LINE_HEIGHT: f32 = 1.25;

fn line_width(font: &FontVec, sf: &ab_glyph::PxScaleFont<&FontVec>, line: &str) -> f32 {
    let mut caret = 0.0;
    let mut prev = None;
    for c in line.chars() {
        let id = font.glyph_id(c);
        if let Some(p) = prev {
            caret += sf.kern(p, id);
        }
        caret += sf.h_advance(id);
        prev = Some(id);
    }
    caret
}

/// Width and height of a (possibly multi-line) block of text.
pub fn measure(font: Option<&FontVec>, text: &str, size: f32) -> (f32, f32) {
    let lines = text.split('\n').count() as f32;
    let Some(font) = font else {
        let w = text.split('\n').map(|l| l.chars().count()).max().unwrap_or(0);
        return (w as f32 * size * 0.55, lines * size * LINE_HEIGHT);
    };
    let sf = font.as_scaled(PxScale::from(size));
    let w = text.split('\n').map(|l| line_width(font, &sf, l)).fold(0.0, f32::max);
    (w, lines * size * LINE_HEIGHT)
}

/// Draws text with the block's top-left corner at (x, y). Lines are aligned within the block.
#[allow(clippy::too_many_arguments)]
pub fn draw(
    pm: &mut tiny_skia::Pixmap,
    font: Option<&FontVec>,
    text: &str,
    x: f32,
    y: f32,
    size: f32,
    align: Align,
    color: Color,
) {
    let Some(font) = font else { return };
    let sf = font.as_scaled(PxScale::from(size));
    let line_h = size * LINE_HEIGHT;
    let ascent = sf.ascent() + (line_h - (sf.ascent() - sf.descent())) / 2.0;
    let block_w = text.split('\n').map(|l| line_width(font, &sf, l)).fold(0.0, f32::max);
    let (pw, ph) = (pm.width() as i32, pm.height() as i32);
    let pixels = pm.pixels_mut();
    for (li, line) in text.split('\n').enumerate() {
        let baseline = y + ascent + li as f32 * line_h;
        let lw = line_width(font, &sf, line);
        let mut caret = x + match align {
            Align::Left => 0.0,
            Align::Center => (block_w - lw) / 2.0,
            Align::Right => block_w - lw,
        };
        let mut prev = None;
        for c in line.chars() {
            let id = font.glyph_id(c);
            if let Some(p) = prev {
                caret += sf.kern(p, id);
            }
            let glyph = id.with_scale_and_position(sf.scale(), ab_glyph::point(caret, baseline));
            caret += sf.h_advance(id);
            prev = Some(id);
            if let Some(og) = font.outline_glyph(glyph) {
                let b = og.px_bounds();
                if b.max.x < 0.0 || b.max.y < 0.0 || b.min.x > pw as f32 || b.min.y > ph as f32 {
                    continue;
                }
                og.draw(|gx, gy, cov| {
                    let px = b.min.x as i32 + gx as i32;
                    let py = b.min.y as i32 + gy as i32;
                    if px >= 0 && py >= 0 && px < pw && py < ph {
                        blend_px(&mut pixels[(py * pw + px) as usize], color, cov);
                    }
                });
            }
        }
    }
}

pub(crate) fn blend_px(dst: &mut tiny_skia::PremultipliedColorU8, c: Color, cov: f32) {
    let a = (c.a() as f32 / 255.0) * cov.clamp(0.0, 1.0);
    if a <= 0.0 {
        return;
    }
    let inv = 1.0 - a;
    let mix = |s: u8, d: u8| ((s as f32 * a) + d as f32 * inv).round().min(255.0) as u8;
    let na = (255.0 * a + dst.alpha() as f32 * inv).round().min(255.0) as u8;
    let r = mix(c.r(), dst.red()).min(na);
    let g = mix(c.g(), dst.green()).min(na);
    let b = mix(c.b(), dst.blue()).min(na);
    if let Some(p) = tiny_skia::PremultipliedColorU8::from_rgba(r, g, b, na) {
        *dst = p;
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn catalog_lists_installed_families_and_falls_back() {
        assert!(families().contains(&DEFAULT_FAMILY));
        assert_eq!(weights("Georgia"), vec![Weight::Regular, Weight::Bold]);
        assert!(font("Georgia", Weight::Black).is_some(), "nearest weight is used");
        assert!(font("No Such Font", Weight::Regular).is_some(), "falls back to the default family");
        let (w1, _) = measure(font(DEFAULT_FAMILY, Weight::Regular).as_deref(), "Hello", 40.0);
        let (w2, _) = measure(font(DEFAULT_FAMILY, Weight::Black).as_deref(), "Hello", 40.0);
        assert!(w2 > w1, "black is wider than regular: {w2} vs {w1}");
    }

    #[test]
    fn every_face_of_an_installed_family_loads() {
        for family in families() {
            let (_, faces) = CATALOG.iter().find(|(n, _)| n == family).unwrap();
            for face in faces.iter().flatten() {
                assert!(load(*face).is_some(), "{family}: {face:?}");
            }
        }
    }
}
