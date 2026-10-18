use anyhow::{Context as _, Result};
use image::RgbaImage;
use std::sync::Arc;

pub use crate::sys::capture::{cursor_pos, foreground_window, list_windows, window_rect};

#[derive(Clone)]
pub struct Capture {
    pub image: Arc<RgbaImage>,
    pub dpi: f32,
    pub title: Option<String>,
    pub created: chrono::DateTime<chrono::Local>,
}

impl Capture {
    pub fn new(image: RgbaImage, dpi: f32, title: Option<String>) -> Self {
        Self {
            image: Arc::new(image),
            dpi,
            title,
            created: chrono::Local::now(),
        }
    }
}

pub struct MonitorShot {
    pub x: i32,
    pub y: i32,
    pub width: u32,
    pub height: u32,
    pub scale: f32,
    pub image: Arc<RgbaImage>,
}

#[derive(Clone, Debug)]
pub struct WinInfo {
    pub hwnd: isize,
    pub title: String,
    pub rect: [i32; 4],
}

pub fn capture_monitors() -> Result<Vec<MonitorShot>> {
    let monitors = xcap::Monitor::all().context("listing monitors")?;
    let mut out = Vec::new();
    for m in monitors {
        let image = m.capture_image().context("capturing monitor")?;
        out.push(MonitorShot {
            x: m.x().unwrap_or(0),
            y: m.y().unwrap_or(0),
            width: image.width(),
            height: image.height(),
            scale: m.scale_factor().unwrap_or(1.0),
            image: Arc::new(image),
        });
    }
    Ok(out)
}

pub fn monitor_at(shots: &[MonitorShot], x: i32, y: i32) -> usize {
    shots
        .iter()
        .position(|m| x >= m.x && y >= m.y && x < m.x + m.width as i32 && y < m.y + m.height as i32)
        .unwrap_or(0)
}

pub fn crop_screen(shots: &[MonitorShot], rect: [i32; 4], title: Option<String>) -> Option<Capture> {
    let [x, y, w, h] = rect;
    let m = &shots[monitor_at(shots, x + w / 2, y + h / 2)];
    let lx = (x - m.x).clamp(0, m.width as i32);
    let ly = (y - m.y).clamp(0, m.height as i32);
    let rx = (x + w - m.x).clamp(0, m.width as i32);
    let ry = (y + h - m.y).clamp(0, m.height as i32);
    if rx - lx < 2 || ry - ly < 2 {
        return None;
    }
    let img = image::imageops::crop_imm(
        m.image.as_ref(),
        lx as u32,
        ly as u32,
        (rx - lx) as u32,
        (ry - ly) as u32,
    )
    .to_image();
    Some(Capture::new(img, m.scale, title))
}

pub fn with_shadow(img: &RgbaImage, dpi: f32) -> RgbaImage {
    let (w, h) = (img.width(), img.height());
    let radius = 8.0 * dpi;
    let pad = (28.0 * dpi).round() as u32;
    let drop = (6.0 * dpi).round() as u32;
    let (ow, oh) = (w + pad * 2, h + pad * 2 + drop);

    let k = 4u32;
    let (sw, sh) = (ow.div_ceil(k), oh.div_ceil(k));
    let mut mask = image::GrayImage::new(sw, sh);
    let (x0, y0) = (pad / k, (pad + drop) / k);
    for y in y0..y0 + h / k {
        for x in x0..x0 + w / k {
            mask.put_pixel(x, y, image::Luma([255]));
        }
    }
    let mask = image::imageops::blur(&mask, 14.0 * dpi / k as f32);
    let mask = image::imageops::resize(&mask, ow, oh, image::imageops::FilterType::Triangle);

    let mut out = RgbaImage::from_fn(ow, oh, |x, y| image::Rgba([0, 0, 0, (mask.get_pixel(x, y)[0] as f32 * 0.42) as u8]));
    for (x, y, p) in img.enumerate_pixels() {
        let cover = corner_coverage(x as f32 + 0.5, y as f32 + 0.5, w as f32, h as f32, radius);
        if cover <= 0.0 {
            continue;
        }
        let dst = out.get_pixel_mut(x + pad, y + pad);
        let a = cover * p[3] as f32 / 255.0;
        let below = dst[3] as f32 / 255.0;
        let out_a = a + below * (1.0 - a);
        for c in 0..3 {
            dst[c] = ((p[c] as f32 * a + dst[c] as f32 * below * (1.0 - a)) / out_a.max(1e-6)).round() as u8;
        }
        dst[3] = (out_a * 255.0).round() as u8;
    }
    out
}

fn corner_coverage(x: f32, y: f32, w: f32, h: f32, r: f32) -> f32 {
    let cx = if x < r { r } else if x > w - r { w - r } else { return 1.0 };
    let cy = if y < r { r } else if y > h - r { h - r } else { return 1.0 };
    let d = ((x - cx).powi(2) + (y - cy).powi(2)).sqrt();
    (r - d + 0.5).clamp(0.0, 1.0)
}

#[cfg(test)]
mod tests {
    #[test]
    fn shadow_pads_rounds_corners_and_keeps_the_middle() {
        let img = image::RgbaImage::from_pixel(200, 100, image::Rgba([10, 200, 30, 255]));
        let out = super::with_shadow(&img, 1.0);
        assert_eq!((out.width(), out.height()), (256, 162));
        assert_eq!(*out.get_pixel(28 + 100, 28 + 50), image::Rgba([10, 200, 30, 255]));
        assert_eq!(out.get_pixel(28, 28)[1], 0, "corner pixel is outside the rounded window");
        assert_eq!(out.get_pixel(0, 0)[3], 0, "far margin is transparent");
        let below = out.get_pixel(128, 28 + 100 + 3)[3];
        assert!((40..120).contains(&below), "shadow under the window, alpha {below}");
    }
}
