//! macOS backend for the root crate: AppKit, CoreGraphics, Vision and ScreenCaptureKit behind
//! the same `sys::<module>` functions as the Windows backend.

pub mod capture;
pub mod desktop;
mod drag;
pub mod ipc;
pub mod library;
pub mod ocr;
pub mod shell;

pub mod chrome {
    use crate::record::chrome::Kind;
    use gpui::Window;

    /// Recording chrome floats like the other pop-ups, casts no shadow, and lets clicks
    /// through when it only marks an area. ScreenCaptureKit's filter keeps it out of the
    /// recording.
    pub fn style(window: &Window, kind: Kind) {
        super::desktop::later(window, move |w| {
            super::desktop::float(w);
            w.setHasShadow(false);
            w.setIgnoresMouseEvents(kind == Kind::ClickThrough);
        });
    }
}

pub mod target {
    /// The display that holds the center of window `hwnd`, as in `DisplayInfo::handle`.
    pub fn window_monitor(hwnd: isize) -> isize {
        let displays = glint_media::displays().unwrap_or_default();
        let center = crate::capture::window_rect(hwnd).map(|[x, y, w, h]| (x + w / 2, y + h / 2));
        center
            .and_then(|(x, y)| glint_media::space::display_at(&displays, x, y))
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
    use objc2_app_kit::{NSColor, NSColorSpace};
    use objc2_foundation::{NSString, NSUserDefaults};

    pub fn appearance() -> Appearance {
        let style = NSUserDefaults::standardUserDefaults().stringForKey(&NSString::from_str("AppleInterfaceStyle"));
        let light = style.is_none_or(|s| s.to_string() != "Dark");
        let accent = NSColor::controlAccentColor().colorUsingColorSpace(&NSColorSpace::sRGBColorSpace()).map(|c| {
            let byte = |v: f64| (v.clamp(0.0, 1.0) * 255.0).round() as u8;
            [byte(c.redComponent()), byte(c.greenComponent()), byte(c.blueComponent())]
        });
        Appearance { light, accent, backdrop: false }
    }
}
