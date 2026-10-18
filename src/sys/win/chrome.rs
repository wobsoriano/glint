use crate::record::chrome::Kind;
use gpui::Window;
use windows::Win32::Graphics::Dwm::{
    DwmSetWindowAttribute, DWMWA_BORDER_COLOR, DWMWA_COLOR_NONE, DWMWA_WINDOW_CORNER_PREFERENCE, DWMWCP_DONOTROUND,
};
use windows::Win32::UI::WindowsAndMessaging::{
    GetWindowLongPtrW, SetLayeredWindowAttributes, SetWindowDisplayAffinity, SetWindowLongPtrW, GWL_EXSTYLE, LWA_ALPHA,
    WDA_EXCLUDEFROMCAPTURE, WS_EX_LAYERED, WS_EX_NOACTIVATE, WS_EX_TRANSPARENT,
};

pub fn style(window: &Window, kind: Kind) {
    let Some(h) = super::desktop::hwnd(window) else { return };
    unsafe {
        // Windows 11 frames every popup with a rounded 1 px border; the chrome draws its own.
        let none = DWMWA_COLOR_NONE;
        let square = DWMWCP_DONOTROUND;
        let _ = DwmSetWindowAttribute(h, DWMWA_BORDER_COLOR, &none as *const _ as *const _, 4);
        let _ = DwmSetWindowAttribute(h, DWMWA_WINDOW_CORNER_PREFERENCE, &square as *const _ as *const _, 4);
        if matches!(kind, Kind::DuringRecording | Kind::ClickThrough) && !crate::record::chrome::capturable() {
            let _ = SetWindowDisplayAffinity(h, WDA_EXCLUDEFROMCAPTURE);
        }
        // Layered, or the transparent parts of the window show a pale tint. Only the picker
        // takes focus: the rest must not pull it from the picker (Esc) or from the app being
        // recorded.
        let ex = GetWindowLongPtrW(h, GWL_EXSTYLE);
        let want = ex
            | WS_EX_LAYERED.0 as isize
            | match kind {
                Kind::Focused => 0,
                Kind::Passive | Kind::DuringRecording => WS_EX_NOACTIVATE.0 as isize,
                Kind::ClickThrough => (WS_EX_NOACTIVATE.0 | WS_EX_TRANSPARENT.0) as isize,
            };
        if want != ex {
            SetWindowLongPtrW(h, GWL_EXSTYLE, want);
            let _ = SetLayeredWindowAttributes(h, windows::Win32::Foundation::COLORREF(0), 255, LWA_ALPHA);
        }
    }
}
