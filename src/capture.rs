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

fn window_rect(hwnd: HWND) -> Option<[i32; 4]> {
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
            out.push(WinInfo { title, rect });
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
            title: window_title(hwnd),
            rect: window_rect(hwnd)?,
        })
    }
}
