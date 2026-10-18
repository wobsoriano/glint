use windows::Win32::Foundation::HWND;
use windows::Win32::Graphics::Gdi::{GetMonitorInfoW, MonitorFromWindow, HMONITOR, MONITORINFO, MONITOR_DEFAULTTONEAREST};

/// The handle of the display that holds most of the window, as in `DisplayInfo::handle`.
pub fn window_monitor(hwnd: isize) -> isize {
    unsafe { MonitorFromWindow(HWND(hwnd as *mut _), MONITOR_DEFAULTTONEAREST) }.0 as isize
}

pub fn work_area(monitor: isize) -> Option<[i32; 4]> {
    let mut info = MONITORINFO { cbSize: std::mem::size_of::<MONITORINFO>() as u32, ..Default::default() };
    unsafe { GetMonitorInfoW(HMONITOR(monitor as *mut _), &mut info) }.as_bool().then(|| {
        let r = info.rcWork;
        [r.left, r.top, r.right - r.left, r.bottom - r.top]
    })
}
