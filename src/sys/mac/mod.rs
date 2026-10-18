//! macOS backend. Mostly stubs so far: actions report "not supported on macOS yet", and
//! window styling calls do nothing.

pub mod capture;
pub mod desktop;

fn unsupported<T>(what: &str) -> anyhow::Result<T> {
    anyhow::bail!("{what} is not supported on macOS yet")
}

pub mod chrome {
    use crate::record::chrome::Kind;
    use gpui::Window;

    pub fn style(_window: &Window, _kind: Kind) {}
}

pub mod ipc {
    use super::unsupported;
    use crate::ipc::Request;

    pub fn listen() {}

    pub fn forward(_requests: &[Request]) -> anyhow::Result<()> {
        unsupported("forwarding to a running Glint")
    }
}

pub mod library {
    use super::unsupported;
    use std::path::PathBuf;

    pub fn recycle(_paths: &[PathBuf]) -> anyhow::Result<()> {
        unsupported("moving to the Trash")
    }
}

pub mod ocr {
    use super::unsupported;
    use crate::ocr::Line;
    use image::RgbaImage;

    pub fn recognize(_img: &RgbaImage) -> anyhow::Result<Vec<Line>> {
        unsupported("text recognition")
    }
}

pub mod shell {
    use super::unsupported;
    use crate::shell::SaveChoice;
    use std::path::{Path, PathBuf};

    pub fn copy_files(_paths: &[PathBuf], _image: Option<&image::RgbaImage>) -> anyhow::Result<()> {
        unsupported("copying files")
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
        let Some(d) = displays.iter().find(|d| d.rect.x <= x && d.rect.y <= y && x < d.rect.x + d.rect.width as i32 && y < d.rect.y + d.rect.height as i32).or(displays.first()) else {
            return ([0, 0, 1920, 1080], 1.0);
        };
        let rect = super::desktop::work_area(&displays, d).unwrap_or([d.rect.x, d.rect.y, d.rect.width as i32, d.rect.height as i32]);
        (rect, d.scale)
    }

    /// Moves a window to cover a pixel rectangle, above other apps, without activating it.
    pub fn place(window: &gpui::Window, rect: [i32; 4], _cx: &gpui::App) {
        let Ok(displays) = glint_media::displays() else { return };
        let frame = super::desktop::to_frame(glint_media::space::rect_to_points(&displays, rect));
        super::desktop::later(window, move |w| {
            super::desktop::float(w);
            w.setFrame_display(frame, true);
            w.orderFrontRegardless();
        });
    }

    pub fn mica(_window: &gpui::Window) {}

    pub fn caption_text(_window: &gpui::Window) {}

    pub fn save_failed_dialog(_detail: &str) -> SaveChoice {
        SaveChoice::Cancel
    }

    pub fn register_open_with() -> anyhow::Result<()> {
        Ok(())
    }

    pub fn exclude_own_windows_from_capture(_include: bool) {}
}

pub mod target {
    /// The display that holds the center of window `hwnd`, as in `DisplayInfo::handle`.
    pub fn window_monitor(hwnd: isize) -> isize {
        let displays = glint_media::displays().unwrap_or_default();
        let center = crate::capture::window_rect(hwnd).map(|[x, y, w, h]| (x + w / 2, y + h / 2));
        center
            .and_then(|(x, y)| {
                displays.iter().find(|d| d.rect.x <= x && d.rect.y <= y && x < d.rect.x + d.rect.width as i32 && y < d.rect.y + d.rect.height as i32)
            })
            .or(displays.first())
            .map_or(0, |d| d.handle)
    }

    pub fn work_area(monitor: isize) -> Option<[i32; 4]> {
        let displays = glint_media::displays().ok()?;
        let d = displays.iter().find(|d| d.handle == monitor)?;
        super::desktop::work_area(&displays, d)
    }
}

pub mod ui {
    use crate::ui::Appearance;

    pub fn appearance() -> Appearance {
        Appearance { light: true, accent: None }
    }
}
