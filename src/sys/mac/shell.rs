use super::unsupported;
use crate::shell::SaveChoice;
use dispatch2::DispatchQueue;
use glint_media::space;
use objc2::rc::Retained;
use objc2::runtime::ProtocolObject;
use objc2::MainThreadMarker;
use objc2_app_kit::{NSAlert, NSAlertFirstButtonReturn, NSAlertSecondButtonReturn, NSAlertStyle, NSPasteboard, NSPasteboardTypePNG, NSPasteboardWriting};
use objc2_foundation::{NSArray, NSData, NSString, NSURL};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};

/// Puts files on the pasteboard, as Finder's Copy does, with `image`'s pixels as PNG for apps
/// that paste images.
pub fn copy_files(paths: &[PathBuf], image: Option<&image::RgbaImage>) -> anyhow::Result<()> {
    let urls: Vec<Retained<ProtocolObject<dyn NSPasteboardWriting>>> = paths
        .iter()
        .map(|p| ProtocolObject::from_retained(NSURL::fileURLWithPath(&NSString::from_str(&p.to_string_lossy()))))
        .collect();
    let png = image.map(|img| crate::shell::encode(img, crate::settings::ExportFormat::Png, 100)).transpose()?;
    let board = NSPasteboard::generalPasteboard();
    board.clearContents();
    anyhow::ensure!(board.writeObjects(&NSArray::from_retained_slice(&urls)), "the pasteboard refused the files");
    if let Some(png) = png {
        board.setData_forType(Some(&NSData::with_bytes(&png)), unsafe { NSPasteboardTypePNG });
    }
    Ok(())
}

pub fn drag_out(_path: &Path, _image: Option<&image::RgbaImage>, _thumb: &gpui::RenderImage) -> anyhow::Result<bool> {
    unsupported("dragging into other apps")
}

pub fn refuse_drops(_window: &gpui::Window) {}

/// Opens Finder with the given items selected.
pub fn reveal(paths: &[PathBuf]) {
    if !paths.is_empty() {
        let _ = std::process::Command::new("open").arg("-R").args(paths).spawn();
    }
}

/// The work area of the display under the pointer, and that display's scale.
pub fn work_area_at_cursor() -> ([i32; 4], f32) {
    let displays = glint_media::displays().unwrap_or_default();
    let (x, y) = crate::capture::cursor_pos();
    let Some(d) = space::display_at(&displays, x, y).or(displays.first()) else {
        return ([0, 0, 1920, 1080], 1.0);
    };
    let rect = super::desktop::work_area(&displays, d).unwrap_or([d.rect.x, d.rect.y, d.rect.width as i32, d.rect.height as i32]);
    (rect, d.scale)
}

/// Moves a window to cover a pixel rectangle, above other apps, without activating it.
pub fn place(window: &gpui::Window, rect: [i32; 4], _cx: &gpui::App) {
    let Ok(displays) = glint_media::displays() else { return };
    let frame = super::desktop::to_frame(space::rect_to_points(&displays, rect));
    super::desktop::later(window, move |w| {
        super::desktop::float(w);
        w.setFrame_display(frame, true);
        w.orderFrontRegardless();
    });
}

pub fn mica(_window: &gpui::Window) {}

pub fn caption_text(_window: &gpui::Window) {}

/// Asks what to do after a failed save. Blocks the calling (background) thread while the alert
/// runs on the main thread.
pub fn save_failed_dialog(detail: &str) -> SaveChoice {
    let detail = detail.to_string();
    let mut choice = SaveChoice::Cancel;
    DispatchQueue::main().exec_sync(|| {
        let Some(mtm) = MainThreadMarker::new() else { return };
        let alert = NSAlert::new(mtm);
        alert.setAlertStyle(NSAlertStyle::Warning);
        alert.setMessageText(&NSString::from_str("The screenshot could not be saved"));
        alert.setInformativeText(&NSString::from_str(&detail));
        alert.addButtonWithTitle(&NSString::from_str("Retry"));
        alert.addButtonWithTitle(&NSString::from_str("Choose Another Folder…"));
        alert.addButtonWithTitle(&NSString::from_str("Cancel"));
        choice = match alert.runModal() {
            r if r == NSAlertFirstButtonReturn => SaveChoice::Retry,
            r if r == NSAlertSecondButtonReturn => SaveChoice::ChooseFolder,
            _ => SaveChoice::Cancel,
        };
    });
    choice
}

pub fn register_open_with() -> anyhow::Result<()> {
    Ok(())
}

static INCLUDE_OWN: AtomicBool = AtomicBool::new(false);

/// Sets whether Glint's windows show up in captures. Captures read it through
/// [`include_own_windows`]; the ScreenCaptureKit filter does the excluding.
pub fn exclude_own_windows_from_capture(include: bool) {
    INCLUDE_OWN.store(include, Ordering::Relaxed);
}

pub(crate) fn include_own_windows() -> bool {
    INCLUDE_OWN.load(Ordering::Relaxed)
}
