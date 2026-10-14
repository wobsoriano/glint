//! Command line parsing and forwarding a second launch to the running instance.
//! The running instance owns a hidden message-only window; a later `glint.exe` finds it,
//! sends its requests as JSON in WM_COPYDATA, and exits.

use anyhow::{bail, Context as _, Result};
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};
use std::sync::Mutex;
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

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum CaptureCmd {
    Area,
    Window,
    Screen,
    Text,
    Previous,
}

#[derive(Clone, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Request {
    Capture(CaptureCmd),
    Library,
    /// Opens the recording picker, or stops the recording in progress.
    Record,
    /// An image to open in the editor: a Library capture as it is, any other file imported
    /// as a copy first. Always absolute.
    Open(PathBuf),
    /// A recording session directory to open in Studio. Always absolute.
    Studio(PathBuf),
}

#[derive(Debug, PartialEq, Eq)]
pub struct Launch {
    pub background: bool,
    pub requests: Vec<Request>,
}

pub const USAGE: &str = "usage: glint.exe [--background] [--library] [--record] [--capture area|window|screen|text|previous] [--studio <session dir>] [image or Library capture...]";

pub fn parse(args: &[String], cwd: &Path) -> Result<Launch> {
    let mut launch = Launch { background: false, requests: Vec::new() };
    let mut it = args.iter();
    while let Some(a) = it.next() {
        match a.as_str() {
            "--background" => launch.background = true,
            "--library" => launch.requests.push(Request::Library),
            "--record" => launch.requests.push(Request::Record),
            "--capture" => {
                let what = it.next().map(String::as_str).unwrap_or("");
                let cmd = match what {
                    "area" => CaptureCmd::Area,
                    "window" => CaptureCmd::Window,
                    "screen" => CaptureCmd::Screen,
                    "text" => CaptureCmd::Text,
                    "previous" => CaptureCmd::Previous,
                    _ => bail!("unknown capture kind {what:?}\n{USAGE}"),
                };
                launch.requests.push(Request::Capture(cmd));
            }
            "--studio" => match it.next() {
                Some(dir) => launch.requests.push(Request::Studio(cwd.join(dir))),
                None => bail!("--studio needs a session directory\n{USAGE}"),
            },
            flag if flag.starts_with("--") => bail!("unknown option {flag}\n{USAGE}"),
            path => launch.requests.push(Request::Open(cwd.join(path))),
        }
    }
    Ok(launch)
}

static INBOX: Mutex<Vec<Request>> = Mutex::new(Vec::new());

/// Requests forwarded by later launches since the last call.
pub fn drain() -> Vec<Request> {
    std::mem::take(&mut *INBOX.lock().unwrap())
}

unsafe extern "system" fn wndproc(hwnd: HWND, msg: u32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if msg == WM_COPYDATA {
        let data = unsafe { &*(lparam.0 as *const COPYDATASTRUCT) };
        if data.dwData == MAGIC && !data.lpData.is_null() {
            let bytes = unsafe { std::slice::from_raw_parts(data.lpData as *const u8, data.cbData as usize) };
            if let Ok(reqs) = serde_json::from_slice::<Vec<Request>>(bytes) {
                INBOX.lock().unwrap().extend(reqs);
                return LRESULT(1);
            }
        }
        return LRESULT(0);
    }
    unsafe { DefWindowProcW(hwnd, msg, wparam, lparam) }
}

/// Starts the receiving window on its own thread. Call once, in the first instance.
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

/// Sends requests to the running instance, waiting a few seconds for it to finish starting.
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

#[cfg(test)]
mod tests {
    use super::*;

    fn args(a: &[&str]) -> Vec<String> {
        a.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn parses_flags_captures_and_relative_paths() {
        let cwd = Path::new(r"C:\Users\me\Pictures");
        let l = parse(&args(&["--background", "--capture", "window", "shot.png", r"D:\x\y.jpg", "--library", "--record", "--studio", "rec.glintrec"]), cwd).unwrap();
        assert_eq!(
            l,
            Launch {
                background: true,
                requests: vec![
                    Request::Capture(CaptureCmd::Window),
                    Request::Open(PathBuf::from(r"C:\Users\me\Pictures\shot.png")),
                    Request::Open(PathBuf::from(r"D:\x\y.jpg")),
                    Request::Library,
                    Request::Record,
                    Request::Studio(PathBuf::from(r"C:\Users\me\Pictures\rec.glintrec")),
                ],
            }
        );
    }

    #[test]
    fn rejects_unknown_capture_kinds_and_flags() {
        let cwd = Path::new(r"C:\");
        assert!(parse(&args(&["--capture", "video"]), cwd).unwrap_err().to_string().starts_with("unknown capture kind \"video\""));
        assert!(parse(&args(&["--capture"]), cwd).unwrap_err().to_string().starts_with("unknown capture kind \"\""));
        assert!(parse(&args(&["--studio"]), cwd).unwrap_err().to_string().starts_with("--studio needs a session directory"));
        assert!(parse(&args(&["--nope"]), cwd).unwrap_err().to_string().starts_with("unknown option --nope"));
    }
}
