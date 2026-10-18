use dispatch2::DispatchQueue;
use glint_media::{space, DisplayInfo};
use objc2::rc::Retained;
use objc2::MainThreadMarker;
use objc2_app_kit::{NSApplication, NSApplicationActivationPolicy, NSPopUpMenuWindowLevel, NSScreen, NSView, NSWindow, NSWorkspace, NSWindowCollectionBehavior, NSWindowStyleMask};
use objc2_core_graphics::{CGDisplayBounds, CGMainDisplayID};
use objc2_foundation::{NSNumber, NSPoint, NSRect, NSSize, NSString};
use raw_window_handle::{HasWindowHandle, RawWindowHandle};
use std::path::PathBuf;

pub(crate) fn ns_window(window: &gpui::Window) -> Option<Retained<NSWindow>> {
    match HasWindowHandle::window_handle(window).ok()?.as_raw() {
        RawWindowHandle::AppKit(h) => unsafe { h.ns_view.cast::<NSView>().as_ref() }.window(),
        _ => None,
    }
}

/// An `NSWindow` handed to the main queue. It is only touched there.
struct MainOnly(Retained<NSWindow>);
unsafe impl Send for MainOnly {}

/// Runs `f` on the window from the main queue, after the current GPUI callback returns:
/// moving or ordering a window synchronously calls back into GPUI while it is borrowed.
pub(crate) fn later(window: &gpui::Window, f: impl FnOnce(&NSWindow) + Send + 'static) {
    let Some(w) = ns_window(window) else { return };
    let w = MainOnly(w);
    DispatchQueue::main().exec_async(move || {
        let w = w;
        f(&w.0)
    });
}

/// Converts a top-left point rectangle to an AppKit frame, whose origin is the bottom-left
/// corner of the primary display.
pub(crate) fn to_frame([x, y, w, h]: [f64; 4]) -> NSRect {
    let primary = CGDisplayBounds(CGMainDisplayID()).size.height;
    NSRect::new(NSPoint::new(x, primary - (y + h)), NSSize::new(w, h))
}

/// Converts an AppKit frame to a top-left point rectangle.
pub(crate) fn from_frame(r: NSRect) -> [f64; 4] {
    let primary = CGDisplayBounds(CGMainDisplayID()).size.height;
    [r.origin.x, primary - (r.origin.y + r.size.height), r.size.width, r.size.height]
}

/// Keeps a window above other apps, the menu bar and the Dock, on every Space. GPUI opens
/// pop-ups as titled windows, which AppKit keeps below the menu bar; borderless ones can cover
/// it. GPUI's window class still lets them take the keyboard.
pub(crate) fn float(w: &NSWindow) {
    w.setStyleMask(w.styleMask() & !NSWindowStyleMask::Titled);
    w.setLevel(NSPopUpMenuWindowLevel);
    w.setCollectionBehavior(
        NSWindowCollectionBehavior::CanJoinAllSpaces
            | NSWindowCollectionBehavior::FullScreenAuxiliary
            | NSWindowCollectionBehavior::Stationary,
    );
}

/// The `NSScreen` that shows display `handle` (a `CGDirectDisplayID`).
pub(crate) fn screen_for(handle: isize) -> Option<Retained<NSScreen>> {
    let mtm = MainThreadMarker::new()?;
    let key = NSString::from_str("NSScreenNumber");
    NSScreen::screens(mtm).into_iter().find(|s| {
        s.deviceDescription()
            .objectForKey(&key)
            .and_then(|n| n.downcast::<NSNumber>().ok())
            .is_some_and(|n| n.unsignedIntValue() as isize == handle)
    })
}

/// The part of display `d` that the menu bar and Dock leave free, in Glint pixels.
pub(crate) fn work_area(displays: &[DisplayInfo], d: &DisplayInfo) -> Option<[i32; 4]> {
    let visible = from_frame(screen_for(d.handle)?.visibleFrame());
    Some(space::rect_to_pixels(displays, visible))
}

pub fn make_topmost(window: &gpui::Window, rect: Option<[i32; 4]>, activate: bool) {
    let frame = rect.and_then(|r| Some(to_frame(space::rect_to_points(&glint_media::displays().ok()?, r))));
    later(window, move |w| {
        float(w);
        if let Some(f) = frame {
            w.setFrame_display(f, true);
        }
        if activate {
            w.makeKeyAndOrderFront(None);
        } else {
            w.orderFrontRegardless();
        }
    });
}

pub fn bring_to_front(window: &gpui::Window) {
    window.activate_window();
}

/// The picture on the main display's desktop, when it is a file.
pub fn desktop_wallpaper() -> Option<PathBuf> {
    let screen = NSScreen::mainScreen(MainThreadMarker::new()?)?;
    let url = NSWorkspace::sharedWorkspace().desktopImageURLForScreen(&screen)?;
    let path = PathBuf::from(url.path()?.to_string());
    path.is_file().then_some(path)
}

/// Runs Glint as a menu bar app, without a Dock icon or a menu bar of its own.
pub fn init_app() {
    if let Some(mtm) = MainThreadMarker::new() {
        NSApplication::sharedApplication(mtm).setActivationPolicy(NSApplicationActivationPolicy::Accessory);
    }
}

fn launch_agent() -> Option<PathBuf> {
    Some(dirs::home_dir()?.join("Library/LaunchAgents/com.glint.app.plist"))
}

/// Starts Glint in the background at login through a per-user LaunchAgent.
pub fn set_launch_at_login(enabled: bool) {
    let (Some(plist), Ok(exe)) = (launch_agent(), std::env::current_exe()) else { return };
    if !enabled {
        let _ = std::fs::remove_file(plist);
        return;
    }
    let exe = exe.to_string_lossy().replace('&', "&amp;").replace('<', "&lt;");
    let body = format!(
        r#"<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.glint.app</string>
    <key>ProgramArguments</key>
    <array>
        <string>{exe}</string>
        <string>--background</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
</dict>
</plist>
"#
    );
    if let Some(dir) = plist.parent() {
        let _ = std::fs::create_dir_all(dir);
    }
    let _ = std::fs::write(plist, body);
}

pub fn single_instance() -> bool {
    super::ipc::claim()
}
