//! macOS backend. Mostly stubs so far: actions report "not supported on macOS yet", and
//! window styling calls do nothing.

fn unsupported<T>(what: &str) -> anyhow::Result<T> {
    anyhow::bail!("{what} is not supported on macOS yet")
}

pub mod capture {
    use crate::capture::WinInfo;

    pub fn cursor_pos() -> (i32, i32) {
        (0, 0)
    }

    pub fn window_rect(_hwnd: isize) -> Option<[i32; 4]> {
        None
    }

    pub fn list_windows() -> Vec<WinInfo> {
        Vec::new()
    }

    pub fn foreground_window() -> Option<WinInfo> {
        None
    }
}

pub mod chrome {
    use crate::record::chrome::Kind;
    use gpui::Window;

    pub fn style(_window: &Window, _kind: Kind) {}
}

pub mod desktop {
    use std::path::PathBuf;

    pub fn make_topmost(_window: &gpui::Window, _rect: Option<[i32; 4]>, _activate: bool) {}

    pub fn bring_to_front(window: &gpui::Window) {
        window.activate_window();
    }

    pub fn desktop_wallpaper() -> Option<PathBuf> {
        None
    }

    pub fn set_launch_at_login(_enabled: bool) {}

    pub fn single_instance() -> bool {
        true
    }
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

    pub fn work_area_at_cursor() -> ([i32; 4], f32) {
        let primary = xcap::Monitor::all().ok().and_then(|all| all.into_iter().next());
        match primary {
            Some(m) => {
                let scale = m.scale_factor().unwrap_or(1.0);
                let size = |v: Option<u32>| (v.unwrap_or(0) as f32 * scale) as i32;
                ([0, 0, size(m.width().ok()), size(m.height().ok())], scale)
            }
            None => ([0, 0, 1920, 1080], 1.0),
        }
    }

    pub fn place(_window: &gpui::Window, _rect: [i32; 4], _cx: &gpui::App) {}

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
    pub fn window_monitor(_hwnd: isize) -> isize {
        0
    }

    pub fn work_area(_monitor: isize) -> Option<[i32; 4]> {
        None
    }
}

pub mod ui {
    use crate::ui::Appearance;

    pub fn appearance() -> Appearance {
        Appearance { light: true, accent: None }
    }
}
