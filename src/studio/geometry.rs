//! Pure geometry for the Studio window: where the screen card and camera bubble sit on the
//! preview, crop rectangles, and the timeline's time-to-pixel mapping. All preview values
//! are logical pixels relative to the displayed frame's top-left corner.

use glint_studio::{CameraSettings, CropRect, Project};

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Rect {
    pub x: f32,
    pub y: f32,
    pub w: f32,
    pub h: f32,
}

impl Rect {
    pub fn contains(&self, x: f32, y: f32) -> bool {
        x >= self.x && x <= self.x + self.w && y >= self.y && y <= self.y + self.h
    }
}

/// The screen card in a Fit-mode frame of `size`, matching the compositor's layout.
pub fn card_rect(project: &Project, source: (u32, u32), size: (f32, f32)) -> Rect {
    let (w, h) = size;
    let inset = project.design.padding.clamp(0.0, 0.475) * w.min(h);
    let (aw, ah) = (w - 2.0 * inset, h - 2.0 * inset);
    let content = (source.0 as f32 * project.crop.w) / (source.1 as f32 * project.crop.h).max(1.0);
    let (cw, ch) = if aw / ah > content { (ah * content, ah) } else { (aw, aw / content) };
    Rect { x: (w - cw) / 2.0, y: (h - ch) / 2.0, w: cw, h: ch }
}

/// The camera bubble's square in a frame of `size`, matching the compositor's clamping.
pub fn bubble_rect(cam: &CameraSettings, size: (f32, f32)) -> Rect {
    let (w, h) = size;
    let short = w.min(h);
    let dia = cam.size * short;
    let margin = short * 0.03;
    let hx = (dia / 2.0 + margin).min(w / 2.0);
    let hy = (dia / 2.0 + margin).min(h / 2.0);
    let cx = (cam.center[0] * w).clamp(hx, w - hx);
    let cy = (cam.center[1] * h).clamp(hy, h - hy);
    Rect { x: cx - dia / 2.0, y: cy - dia / 2.0, w: dia, h: dia }
}

/// Crop aspect presets for the screen card.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CropRatio {
    Freeform,
    Original,
    Square,
    Wide,
    Tall,
    Classic,
    Photo,
}

impl CropRatio {
    pub const ALL: [CropRatio; 7] = [
        CropRatio::Freeform,
        CropRatio::Original,
        CropRatio::Square,
        CropRatio::Wide,
        CropRatio::Tall,
        CropRatio::Classic,
        CropRatio::Photo,
    ];

    pub fn label(self) -> &'static str {
        match self {
            CropRatio::Freeform => "Freeform",
            CropRatio::Original => "Original",
            CropRatio::Square => "1:1",
            CropRatio::Wide => "16:9",
            CropRatio::Tall => "9:16",
            CropRatio::Classic => "4:3",
            CropRatio::Photo => "3:2",
        }
    }

    /// Width over height in source pixels.
    pub fn value(self, source: (u32, u32)) -> Option<f32> {
        match self {
            CropRatio::Freeform => None,
            CropRatio::Original => Some(source.0 as f32 / source.1.max(1) as f32),
            CropRatio::Square => Some(1.0),
            CropRatio::Wide => Some(16.0 / 9.0),
            CropRatio::Tall => Some(9.0 / 16.0),
            CropRatio::Classic => Some(4.0 / 3.0),
            CropRatio::Photo => Some(3.0 / 2.0),
        }
    }
}

pub const MIN_CROP: f32 = 0.05;

/// Width over height of a normalized crop for a pixel ratio.
fn normalized_ratio(ratio: f32, source: (u32, u32)) -> f32 {
    ratio * source.1 as f32 / source.0.max(1) as f32
}

/// The crop spanned from `anchor` toward `to` (normalized source points), held to `ratio`
/// and kept inside the source.
pub fn crop_from_drag(anchor: (f32, f32), to: (f32, f32), ratio: Option<f32>, source: (u32, u32)) -> CropRect {
    let (sx, sy) = (if to.0 >= anchor.0 { 1.0 } else { -1.0 }, if to.1 >= anchor.1 { 1.0 } else { -1.0 });
    let room_w = if sx > 0.0 { 1.0 - anchor.0 } else { anchor.0 };
    let room_h = if sy > 0.0 { 1.0 - anchor.1 } else { anchor.1 };
    let mut w = (to.0 - anchor.0).abs().clamp(MIN_CROP, room_w.max(MIN_CROP));
    let mut h = (to.1 - anchor.1).abs().clamp(MIN_CROP, room_h.max(MIN_CROP));
    if let Some(r) = ratio.map(|r| normalized_ratio(r, source)) {
        if w / h > r {
            w = h * r;
        } else {
            h = w / r;
        }
        let k = (room_w / w).min(room_h / h).min(1.0);
        w *= k;
        h *= k;
    }
    let x = if sx > 0.0 { anchor.0 } else { anchor.0 - w };
    let y = if sy > 0.0 { anchor.1 } else { anchor.1 - h };
    CropRect { x: x.clamp(0.0, 1.0 - w), y: y.clamp(0.0, 1.0 - h), w, h }
}

/// The largest crop of `ratio` that fits the source, centered on `around`'s center.
pub fn crop_for_ratio(around: CropRect, ratio: Option<f32>, source: (u32, u32)) -> CropRect {
    let Some(r) = ratio.map(|r| normalized_ratio(r, source)) else { return around };
    let (w, h) = if r >= 1.0 { (1.0, 1.0 / r) } else { (r, 1.0) };
    let (cx, cy) = (around.x + around.w / 2.0, around.y + around.h / 2.0);
    CropRect { x: (cx - w / 2.0).clamp(0.0, 1.0 - w), y: (cy - h / 2.0).clamp(0.0, 1.0 - h), w, h }
}

/// Maps output time to timeline pixels: `start_ms` sits at x = 0.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct TimeScale {
    pub start_ms: f64,
    pub ms_per_px: f64,
}

impl TimeScale {
    /// `zoom` 1 shows the whole `duration_ms` across `width`.
    pub fn new(duration_ms: f64, width: f32, zoom: f64, start_ms: f64) -> Self {
        let visible = duration_ms.max(1.0) / zoom.max(1.0);
        let start = start_ms.clamp(0.0, (duration_ms - visible).max(0.0));
        Self { start_ms: start, ms_per_px: visible / width.max(1.0) as f64 }
    }

    pub fn x(&self, ms: f64) -> f32 {
        ((ms - self.start_ms) / self.ms_per_px) as f32
    }

    pub fn ms(&self, x: f32) -> f64 {
        self.start_ms + x as f64 * self.ms_per_px
    }
}

/// The timeline start that keeps `anchor_ms` under pixel `anchor_x` after zooming.
pub fn zoom_around(duration_ms: f64, width: f32, zoom: f64, anchor_ms: f64, anchor_x: f32) -> f64 {
    let visible = duration_ms / zoom.max(1.0);
    (anchor_ms - anchor_x as f64 * visible / width.max(1.0) as f64).clamp(0.0, (duration_ms - visible).max(0.0))
}

/// `m:ss.t` for the transport and the ruler.
pub fn format_time(ms: f64) -> String {
    let tenths = (ms.max(0.0) / 100.0).floor() as u64;
    format!("{}:{:02}.{}", tenths / 600, tenths / 10 % 60, tenths % 10)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn card_fits_inside_padding_with_the_crop_aspect() {
        let mut p = Project::new(1000.0);
        p.design.padding = 0.1;
        assert_eq!(card_rect(&p, (1920, 1080), (1000.0, 1000.0)), Rect { x: 100.0, y: 275.0, w: 800.0, h: 450.0 });
        p.crop = CropRect { x: 0.0, y: 0.0, w: 0.5, h: 1.0 };
        let r = card_rect(&p, (1920, 1080), (1000.0, 1000.0));
        assert!((r.w - 711.111).abs() < 0.01 && r.h == 800.0, "{r:?}");
    }

    #[test]
    fn bubble_is_clamped_inside_the_frame() {
        let cam = CameraSettings { visible: true, center: [1.0, 1.0], size: 0.2, roundness: 0.5 };
        assert_eq!(bubble_rect(&cam, (1000.0, 500.0)), Rect { x: 885.0, y: 385.0, w: 100.0, h: 100.0 });
    }

    #[test]
    fn crop_drag_keeps_ratio_and_bounds() {
        let free = crop_from_drag((0.5, 0.5), (0.2, 0.9), None, (1600, 900));
        assert!((free.x - 0.2).abs() < 1e-6 && (free.w - 0.3).abs() < 1e-6 && (free.h - 0.4).abs() < 1e-6, "{free:?}");
        let square = crop_from_drag((0.0, 0.0), (1.0, 1.0), Some(1.0), (1600, 900));
        assert!((square.w * 1600.0 - square.h * 900.0).abs() < 0.01, "{square:?}");
        assert_eq!((square.h, square.x, square.y), (1.0, 0.0, 0.0));
        let full = crop_for_ratio(CropRect::FULL, Some(9.0 / 16.0), (1920, 1080));
        assert!((full.h - 1.0).abs() < 1e-6 && (full.w * 1920.0 / 1080.0 - 9.0 / 16.0).abs() < 1e-4, "{full:?}");
        assert!((full.x + full.w / 2.0 - 0.5).abs() < 1e-6);
    }

    #[test]
    fn time_scale_round_trips_and_zooms_around_the_pointer() {
        let s = TimeScale::new(10_000.0, 1000.0, 1.0, 0.0);
        assert_eq!((s.x(5_000.0), s.ms(250.0)), (500.0, 2_500.0));
        let start = zoom_around(10_000.0, 1000.0, 4.0, 5_000.0, 500.0);
        let z = TimeScale::new(10_000.0, 1000.0, 4.0, start);
        assert_eq!(z.x(5_000.0), 500.0);
        assert_eq!(TimeScale::new(10_000.0, 1000.0, 2.0, 9_000.0).start_ms, 5_000.0);
        assert_eq!(format_time(83_456.0), "1:23.4");
    }
}
