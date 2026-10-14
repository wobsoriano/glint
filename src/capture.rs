//! Screen capture and Win32 window enumeration.

use anyhow::{Context as _, Result};
use image::RgbaImage;
use std::sync::Arc;
use windows::Win32::Foundation::{HWND, LPARAM, POINT, RECT};
use windows::Win32::Graphics::Dwm::{
    DwmGetWindowAttribute, DWMWA_CLOAKED, DWMWA_EXTENDED_FRAME_BOUNDS,
};
use windows::Win32::UI::WindowsAndMessaging::{
    EnumWindows, GetCursorPos, GetForegroundWindow, GetWindowLongW, GetWindowTextLengthW,
    GetWindowTextW, GetWindowThreadProcessId, IsIconic, IsWindowVisible, GWL_EXSTYLE,
    WS_EX_TOOLWINDOW,
};
use windows::core::BOOL;

/// A finished capture, ready for the editor.
#[derive(Clone)]
pub struct Capture {
    pub image: Arc<RgbaImage>,
    /// DPI scale of the monitor it came from (1.0 = 96 DPI).
    pub dpi: f32,
    /// Title of the captured window, used by the window-chrome title bar.
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

/// A frozen snapshot of one monitor, in physical pixels.
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
    /// Physical screen coordinates: x, y, w, h.
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

pub fn cursor_pos() -> (i32, i32) {
    let mut p = POINT::default();
    unsafe {
        let _ = GetCursorPos(&mut p);
    }
    (p.x, p.y)
}

pub fn monitor_at(shots: &[MonitorShot], x: i32, y: i32) -> usize {
    shots
        .iter()
        .position(|m| x >= m.x && y >= m.y && x < m.x + m.width as i32 && y < m.y + m.height as i32)
        .unwrap_or(0)
}

/// Crops a physical-screen rectangle out of the monitor snapshots.
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

pub fn window_rect(hwnd: HWND) -> Option<[i32; 4]> {
    let mut r = RECT::default();
    unsafe {
        DwmGetWindowAttribute(
            hwnd,
            DWMWA_EXTENDED_FRAME_BOUNDS,
            &mut r as *mut _ as *mut _,
            std::mem::size_of::<RECT>() as u32,
        )
        .ok()?;
    }
    let (w, h) = (r.right - r.left, r.bottom - r.top);
    (w > 20 && h > 20).then_some([r.left, r.top, w, h])
}

fn window_title(hwnd: HWND) -> String {
    unsafe {
        let len = GetWindowTextLengthW(hwnd);
        if len <= 0 {
            return String::new();
        }
        let mut buf = vec![0u16; len as usize + 1];
        let n = GetWindowTextW(hwnd, &mut buf);
        String::from_utf16_lossy(&buf[..n as usize])
    }
}

/// Visible top-level windows, front to back.
pub fn list_windows() -> Vec<WinInfo> {
    unsafe extern "system" fn cb(hwnd: HWND, lparam: LPARAM) -> BOOL {
        let out = unsafe { &mut *(lparam.0 as *mut Vec<WinInfo>) };
        unsafe {
            if !IsWindowVisible(hwnd).as_bool() || IsIconic(hwnd).as_bool() {
                return BOOL(1);
            }
            if GetWindowLongW(hwnd, GWL_EXSTYLE) as u32 & WS_EX_TOOLWINDOW.0 != 0 {
                return BOOL(1);
            }
            let mut cloaked = 0u32;
            let _ = DwmGetWindowAttribute(
                hwnd,
                DWMWA_CLOAKED,
                &mut cloaked as *mut _ as *mut _,
                4,
            );
            if cloaked != 0 {
                return BOOL(1);
            }
            let mut pid = 0u32;
            GetWindowThreadProcessId(hwnd, Some(&mut pid));
            if pid == std::process::id() {
                return BOOL(1);
            }
        }
        let title = window_title(hwnd);
        if title.is_empty() || title == "Program Manager" {
            return BOOL(1);
        }
        if let Some(rect) = window_rect(hwnd) {
            out.push(WinInfo { hwnd: hwnd.0 as isize, title, rect });
        }
        BOOL(1)
    }
    let mut out: Vec<WinInfo> = Vec::new();
    unsafe {
        let _ = EnumWindows(Some(cb), LPARAM(&mut out as *mut _ as isize));
    }
    out
}

pub fn foreground_window() -> Option<WinInfo> {
    unsafe {
        let hwnd = GetForegroundWindow();
        if hwnd.0.is_null() {
            return None;
        }
        let mut pid = 0u32;
        GetWindowThreadProcessId(hwnd, Some(&mut pid));
        if pid == std::process::id() {
            return None;
        }
        Some(WinInfo {
            hwnd: hwnd.0 as isize,
            title: window_title(hwnd),
            rect: window_rect(hwnd)?,
        })
    }
}

/// Rounds a window capture's corners like Windows 11 and sets it on a soft drop shadow
/// over a transparent margin.
pub fn with_shadow(img: &RgbaImage, dpi: f32) -> RgbaImage {
    let (w, h) = (img.width(), img.height());
    let radius = 8.0 * dpi;
    let pad = (28.0 * dpi).round() as u32;
    let drop = (6.0 * dpi).round() as u32;
    let (ow, oh) = (w + pad * 2, h + pad * 2 + drop);

    // Blur at quarter resolution: the shadow is soft, so detail there is wasted work.
    let k = 4u32;
    let (sw, sh) = (ow.div_ceil(k), oh.div_ceil(k));
    let mut mask = image::GrayImage::new(sw, sh);
    let (x0, y0) = (pad / k, (pad + drop) / k);
    for y in y0..(y0 + h / k).min(sh) {
        for x in x0..(x0 + w / k).min(sw) {
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

/// Fraction of a pixel centered at (x, y) inside a w×h rectangle with rounded corners.
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
