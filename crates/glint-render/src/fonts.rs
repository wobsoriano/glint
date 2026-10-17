use crate::style::Color;
use ab_glyph::{Font, FontVec, PxScale, ScaleFont};
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
        Self { family: "Segoe UI".into(), weight: Weight::Semibold, align: Align::Left }
    }
}

const CATALOG: &[(&str, [&str; 5])] = &[
    ("Segoe UI", ["segoeuil.ttf", "segoeui.ttf", "seguisb.ttf", "segoeuib.ttf", "seguibl.ttf"]),
    ("Arial", ["", "arial.ttf", "", "arialbd.ttf", "ariblk.ttf"]),
    ("Calibri", ["calibril.ttf", "calibri.ttf", "", "calibrib.ttf", ""]),
    ("Cambria", ["", "cambria.ttc", "", "cambriab.ttf", ""]),
    ("Candara", ["Candaral.ttf", "Candara.ttf", "", "Candarab.ttf", ""]),
    ("Corbel", ["corbell.ttf", "corbel.ttf", "", "corbelb.ttf", ""]),
    ("Constantia", ["", "constan.ttf", "", "constanb.ttf", ""]),
    ("Georgia", ["", "georgia.ttf", "", "georgiab.ttf", ""]),
    ("Times New Roman", ["", "times.ttf", "", "timesbd.ttf", ""]),
    ("Palatino Linotype", ["", "pala.ttf", "", "palab.ttf", ""]),
    ("Verdana", ["", "verdana.ttf", "", "verdanab.ttf", ""]),
    ("Tahoma", ["", "tahoma.ttf", "", "tahomabd.ttf", ""]),
    ("Trebuchet MS", ["", "trebuc.ttf", "", "trebucbd.ttf", ""]),
    ("Bahnschrift", ["", "bahnschrift.ttf", "", "", ""]),
    ("Consolas", ["", "consola.ttf", "", "consolab.ttf", ""]),
    ("Cascadia Code", ["", "CascadiaCode.ttf", "", "", ""]),
    ("Comic Sans MS", ["", "comic.ttf", "", "comicbd.ttf", ""]),
    ("Impact", ["", "impact.ttf", "", "", ""]),
    ("Segoe Print", ["", "segoepr.ttf", "", "segoeprb.ttf", ""]),
    ("Segoe Script", ["", "segoesc.ttf", "", "segoescb.ttf", ""]),
];

fn fonts_dir() -> String {
    let dir = std::env::var("WINDIR").unwrap_or_else(|_| "C:\\Windows".into());
    format!("{dir}\\Fonts")
}

/// Families from the catalog whose regular face is installed.
pub fn families() -> &'static [&'static str] {
    static INSTALLED: LazyLock<Vec<&'static str>> = LazyLock::new(|| {
        let dir = fonts_dir();
        CATALOG
            .iter()
            .filter(|(_, files)| std::path::Path::new(&format!("{dir}\\{}", files[1])).exists())
            .map(|(name, _)| *name)
            .collect()
    });
    &INSTALLED
}

/// Weights with a real face for `family`.
pub fn weights(family: &str) -> Vec<Weight> {
    let Some((_, files)) = CATALOG.iter().find(|(n, _)| *n == family) else {
        return vec![Weight::Regular];
    };
    Weight::ALL.into_iter().filter(|w| !files[w.index()].is_empty()).collect()
}

type FontCache = Mutex<HashMap<(String, Weight), Option<Arc<FontVec>>>>;
static CACHE: LazyLock<FontCache> = LazyLock::new(Default::default);

fn load(file: &str) -> Option<FontVec> {
    let bytes = std::fs::read(format!("{}\\{file}", fonts_dir())).ok()?;
    FontVec::try_from_vec_and_index(bytes, 0).ok()
}

/// The face for `family` at the nearest available weight, falling back to Segoe UI, then Arial.
pub fn font(family: &str, weight: Weight) -> Option<Arc<FontVec>> {
    let key = (family.to_string(), weight);
    if let Some(f) = CACHE.lock().unwrap().get(&key) {
        return f.clone();
    }
    let loaded = [family, "Segoe UI", "Arial"].iter().find_map(|fam| {
        let (_, files) = CATALOG.iter().find(|(n, _)| n == fam)?;
        let want = weight.index() as i32;
        let mut order: Vec<usize> = (0..5).filter(|i| !files[*i].is_empty()).collect();
        order.sort_by_key(|i| ((*i as i32 - want).abs(), -(*i as i32)));
        order.into_iter().find_map(|i| load(files[i])).map(Arc::new)
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
        assert!(families().contains(&"Segoe UI"));
        assert_eq!(weights("Georgia"), vec![Weight::Regular, Weight::Bold]);
        assert!(font("Georgia", Weight::Black).is_some(), "nearest weight is used");
        assert!(font("No Such Font", Weight::Regular).is_some(), "falls back to Segoe UI");
        let (w1, _) = measure(font("Segoe UI", Weight::Regular).as_deref(), "Hello", 40.0);
        let (w2, _) = measure(font("Segoe UI", Weight::Black).as_deref(), "Hello", 40.0);
        assert!(w2 > w1, "black is wider than regular: {w2} vs {w1}");
    }
}
