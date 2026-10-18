use raw_window_handle::{HasWindowHandle, RawWindowHandle};
use std::path::PathBuf;
use windows::core::{w, HSTRING, PCWSTR};
use windows::Win32::Foundation::{GetLastError, ERROR_ALREADY_EXISTS, HWND};
use windows::Win32::System::Registry::{
    RegDeleteKeyValueW, RegSetKeyValueW, HKEY_CURRENT_USER, REG_SZ,
};
use windows::Win32::System::Threading::CreateMutexW;
use windows::Win32::UI::WindowsAndMessaging::{
    SetForegroundWindow, SetWindowPos, SystemParametersInfoW, HWND_TOPMOST, SPI_GETDESKWALLPAPER,
    SWP_NOACTIVATE, SWP_NOMOVE, SWP_NOSIZE, SWP_SHOWWINDOW, SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS,
};

pub fn hwnd(window: &gpui::Window) -> Option<HWND> {
    match HasWindowHandle::window_handle(window).ok()?.as_raw() {
        RawWindowHandle::Win32(h) => Some(HWND(h.hwnd.get() as *mut _)),
        _ => None,
    }
}

pub fn make_topmost(window: &gpui::Window, rect: Option<[i32; 4]>, activate: bool) {
    let Some(h) = hwnd(window) else { return };
    unsafe {
        let mut flags = SWP_SHOWWINDOW;
        if !activate {
            flags |= SWP_NOACTIVATE;
        }
        let [x, y, w, hh] = rect.unwrap_or([0, 0, 0, 0]);
        if rect.is_none() {
            flags |= SWP_NOMOVE | SWP_NOSIZE;
        }
        let _ = SetWindowPos(h, Some(HWND_TOPMOST), x, y, w, hh, flags);
        if activate {
            let _ = SetForegroundWindow(h);
        }
    }
}

pub fn bring_to_front(window: &gpui::Window) {
    if let Some(h) = hwnd(window) {
        unsafe {
            let _ = SetForegroundWindow(h);
        }
    }
}

pub fn desktop_wallpaper() -> Option<PathBuf> {
    let mut buf = [0u16; 520];
    unsafe {
        SystemParametersInfoW(
            SPI_GETDESKWALLPAPER,
            buf.len() as u32,
            Some(buf.as_mut_ptr() as *mut _),
            SYSTEM_PARAMETERS_INFO_UPDATE_FLAGS(0),
        )
        .ok()?;
    }
    let len = buf.iter().position(|&c| c == 0).unwrap_or(0);
    let p = PathBuf::from(String::from_utf16_lossy(&buf[..len]));
    p.exists().then_some(p)
}

const RUN_KEY: PCWSTR = w!("Software\\Microsoft\\Windows\\CurrentVersion\\Run");

pub fn set_launch_at_login(enabled: bool) {
    unsafe {
        if enabled {
            let Ok(exe) = std::env::current_exe() else { return };
            let value: Vec<u16> = format!("\"{}\" --background", exe.display())
                .encode_utf16()
                .chain(Some(0))
                .collect();
            let _ = RegSetKeyValueW(
                HKEY_CURRENT_USER,
                RUN_KEY,
                w!("Glint"),
                REG_SZ.0,
                Some(value.as_ptr() as *const _),
                (value.len() * 2) as u32,
            );
        } else {
            let _ = RegDeleteKeyValueW(HKEY_CURRENT_USER, RUN_KEY, w!("Glint"));
        }
    }
}

pub fn single_instance() -> bool {
    unsafe {
        let name = HSTRING::from("Local\\Glint.SingleInstance");
        let _ = CreateMutexW(None, true, &name);
        GetLastError() != ERROR_ALREADY_EXISTS
    }
}
