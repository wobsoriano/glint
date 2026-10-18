use crate::ipc::Request;
use anyhow::{bail, Context as _, Result};
use std::time::Duration;
use windows::core::{w, PCWSTR};
use windows::Win32::Foundation::{HWND, LPARAM, LRESULT, WPARAM};
use windows::Win32::System::DataExchange::COPYDATASTRUCT;
use windows::Win32::System::LibraryLoader::GetModuleHandleW;
use windows::Win32::UI::WindowsAndMessaging::{
    AllowSetForegroundWindow, CreateWindowExW, DefWindowProcW, DispatchMessageW, FindWindowExW,
    GetMessageW, GetWindowThreadProcessId, RegisterClassW, SendMessageTimeoutW, HWND_MESSAGE, MSG,
    SMTO_ABORTIFHUNG, WINDOW_EX_STYLE, WINDOW_STYLE, WM_COPYDATA, WNDCLASSW,
};

const CLASS: PCWSTR = w!("Glint.Ipc");

const MAGIC: usize = 0x6C6E_7467;

unsafe extern "system" fn wndproc(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if msg == WM_COPYDATA {
        let data = unsafe { &*(lparam.0 as *const COPYDATASTRUCT) };
        if data.dwData == MAGIC && !data.lpData.is_null() {
            let bytes = unsafe { std::slice::from_raw_parts(data.lpData as *const u8, data.cbData as usize) };
            if let Ok(reqs) = serde_json::from_slice::<Vec<Request>>(bytes) {
                crate::ipc::deliver(reqs);
                return LRESULT(1);
            }
        }
        return LRESULT(0);
    }
    unsafe { DefWindowProcW(hwnd, msg, wparam, lparam) }
}

pub fn listen() {
    std::thread::spawn(|| unsafe {
        let Ok(module) = GetModuleHandleW(None) else { return };
        let class = WNDCLASSW { lpfnWndProc: Some(wndproc), hInstance: module.into(), lpszClassName: CLASS, ..Default::default() };
        RegisterClassW(&class);
        let Ok(_hwnd) = CreateWindowExW(
            WINDOW_EX_STYLE(0),
            CLASS,
            w!("Glint IPC"),
            WINDOW_STYLE(0),
            0,
            0,
            0,
            0,
            Some(HWND_MESSAGE),
            None,
            Some(module.into()),
            None,
        ) else {
            return;
        };
        let mut msg = MSG::default();
        while GetMessageW(&mut msg, None, 0, 0).as_bool() {
            DispatchMessageW(&msg);
        }
    });
}

pub fn forward(requests: &[Request]) -> Result<()> {
    let payload = serde_json::to_vec(requests)?;
    let mut target = None;
    for _ in 0..50 {
        if let Ok(h) = unsafe { FindWindowExW(Some(HWND_MESSAGE), None, CLASS, PCWSTR::null()) } {
            target = Some(h);
            break;
        }
        std::thread::sleep(Duration::from_millis(100));
    }
    let hwnd = target.context("Glint is starting but not answering")?;
    unsafe {
        let mut pid = 0u32;
        GetWindowThreadProcessId(hwnd, Some(&mut pid));
        // Lets the running instance bring its windows to the front for this request.
        let _ = AllowSetForegroundWindow(pid);
        let data = COPYDATASTRUCT { dwData: MAGIC, cbData: payload.len() as u32, lpData: payload.as_ptr() as *mut _ };
        let mut result = 0usize;
        let sent = SendMessageTimeoutW(
            hwnd,
            WM_COPYDATA,
            WPARAM(0),
            LPARAM(&data as *const _ as isize),
            SMTO_ABORTIFHUNG,
            5000,
            Some(&mut result),
        );
        if sent.0 == 0 || result != 1 {
            bail!("the running Glint did not accept the request");
        }
    }
    Ok(())
}
