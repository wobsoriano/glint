use crate::capture::{MonitorShot, WinInfo};
use anyhow::{Context as _, Result};
use glint_media::space;
use objc2_core_foundation::{CFDictionary, CFNumber, CFNumberType, CFRetained, CFString, CGRect};
use objc2_core_graphics::{
    kCGWindowAlpha, kCGWindowBounds, kCGWindowLayer, kCGWindowName, kCGWindowNumber, kCGWindowOwnerName, kCGWindowOwnerPID,
    CGEvent, CGRectMakeWithDictionaryRepresentation, CGWindowListCopyWindowInfo, CGWindowListOption,
};
use std::ffi::c_void;
use std::sync::Arc;

/// Captures every display at its backing resolution, placed in Glint pixels. ScreenCaptureKit
/// (macOS 14 and later) leaves Glint's own windows out unless the setting includes them; the
/// window-list capture on macOS 13 cannot.
pub fn capture_monitors() -> Result<Vec<MonitorShot>> {
    let displays = glint_media::displays()?;
    let include_own = super::shell::include_own_windows();
    let monitors = if objc2::available!(macos = 14.0) { Vec::new() } else { xcap::Monitor::all().context("listing monitors")? };
    let mut out = Vec::new();
    for d in &displays {
        let image = if objc2::available!(macos = 14.0) {
            glint_media::sck::screenshot(d.handle as u32, d.rect.width, d.rect.height, include_own).context("capturing display")?
        } else {
            let Some(m) = monitors.iter().find(|m| m.id().ok() == Some(d.handle as u32)) else { continue };
            m.capture_image().context("capturing monitor")?
        };
        let [lx, ly, lw, lh] = space::point_bounds(&displays, d);
        let (width, height) = image.dimensions();
        out.push(MonitorShot {
            x: d.rect.x,
            y: d.rect.y,
            width,
            height,
            scale: d.scale,
            logical: [lx as f32, ly as f32, lw as f32, lh as f32],
            image: Arc::new(image),
        });
    }
    Ok(out)
}

pub fn cursor_pos() -> (i32, i32) {
    let p = CGEvent::location(CGEvent::new(None).as_deref());
    let displays = glint_media::displays().unwrap_or_default();
    space::to_pixels(&displays, p.x, p.y)
}

pub fn window_rect(hwnd: isize) -> Option<[i32; 4]> {
    let displays = glint_media::displays().ok()?;
    let list = CGWindowListCopyWindowInfo(CGWindowListOption::OptionIncludingWindow, hwnd as u32)?;
    if list.count() == 0 {
        return None;
    }
    let info = unsafe { WindowEntry::read(&*(list.value_at_index(0) as *const CFDictionary)) }?;
    pixel_rect(&displays, info.bounds)
}

/// Normal windows of other apps, front to back.
pub fn list_windows() -> Vec<WinInfo> {
    let Ok(displays) = glint_media::displays() else { return Vec::new() };
    on_screen()
        .into_iter()
        .filter_map(|w| Some(WinInfo { hwnd: w.id as isize, rect: pixel_rect(&displays, w.bounds)?, title: w.title }))
        .collect()
}

/// The frontmost normal window of another app.
pub fn foreground_window() -> Option<WinInfo> {
    list_windows().into_iter().next()
}

fn pixel_rect(displays: &[glint_media::DisplayInfo], bounds: CGRect) -> Option<[i32; 4]> {
    let r = space::rect_to_pixels(displays, [bounds.origin.x, bounds.origin.y, bounds.size.width, bounds.size.height]);
    (r[2] > 20 && r[3] > 20).then_some(r)
}

struct WindowEntry {
    id: u32,
    pid: i32,
    layer: i32,
    alpha: f64,
    title: String,
    bounds: CGRect,
}

impl WindowEntry {
    /// Reads one `CGWindowListCopyWindowInfo` entry. The title falls back to the app name:
    /// window names are only visible with the Screen Recording permission.
    unsafe fn read(dict: &CFDictionary) -> Option<Self> {
        let value = |key: &CFString| unsafe { dict.value(key as *const CFString as *const c_void) };
        let number = |key: &CFString, kind: CFNumberType, out: *mut c_void| {
            let v = value(key) as *const CFNumber;
            !v.is_null() && unsafe { (*v).value(kind, out) }
        };
        let string = |key: &CFString| {
            let v = value(key) as *const CFString;
            (!v.is_null()).then(|| unsafe { (*v).to_string() }).unwrap_or_default()
        };
        let (mut id, mut pid, mut layer, mut alpha) = (0i32, 0i32, 0i32, 1f64);
        unsafe {
            if !number(kCGWindowNumber, CFNumberType::IntType, &mut id as *mut _ as *mut c_void) {
                return None;
            }
            number(kCGWindowOwnerPID, CFNumberType::IntType, &mut pid as *mut _ as *mut c_void);
            number(kCGWindowLayer, CFNumberType::IntType, &mut layer as *mut _ as *mut c_void);
            number(kCGWindowAlpha, CFNumberType::DoubleType, &mut alpha as *mut _ as *mut c_void);
        }
        let mut bounds = CGRect::default();
        let dict_bounds = value(unsafe { kCGWindowBounds }) as *const CFDictionary;
        if dict_bounds.is_null() || !unsafe { CGRectMakeWithDictionaryRepresentation(Some(&*dict_bounds), &mut bounds) } {
            return None;
        }
        let title = Some(string(unsafe { kCGWindowName })).filter(|t| !t.is_empty()).unwrap_or_else(|| string(unsafe { kCGWindowOwnerName }));
        Some(Self { id: id as u32, pid, layer, alpha, title, bounds })
    }
}

/// On-screen windows at the normal level that belong to other apps, front to back.
fn on_screen() -> Vec<WindowEntry> {
    let options = CGWindowListOption::OptionOnScreenOnly | CGWindowListOption::ExcludeDesktopElements;
    let Some(list): Option<CFRetained<_>> = CGWindowListCopyWindowInfo(options, 0) else { return Vec::new() };
    let own = std::process::id() as i32;
    (0..list.count())
        .filter_map(|i| unsafe { WindowEntry::read(&*(list.value_at_index(i) as *const CFDictionary)) })
        .filter(|w| w.layer == 0 && w.alpha > 0.0 && w.pid != own && !w.title.is_empty())
        .collect()
}
