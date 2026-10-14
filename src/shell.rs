//! Windows shell integration: exports, file clipboard, drag out, Explorer, Open With,
//! task dialogs and keeping Glint's own windows out of screen captures.

use crate::settings::ExportFormat;
use anyhow::Context as _;
use std::os::windows::ffi::OsStrExt;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use windows::core::{w, HSTRING, PCWSTR};
use windows::Win32::Foundation::{HANDLE, HGLOBAL, HWND, LPARAM, POINT};
use windows::Win32::Graphics::Gdi::{
    GetMonitorInfoW, MonitorFromPoint, BITMAPINFOHEADER, BI_RGB, MONITORINFO, MONITOR_DEFAULTTONEAREST,
};
use windows::Win32::System::Com::{IDataObject, DVASPECT_CONTENT, FORMATETC, STGMEDIUM, STGMEDIUM_0, TYMED_HGLOBAL};
use windows::Win32::System::DataExchange::{CloseClipboard, EmptyClipboard, OpenClipboard, SetClipboardData};
use windows::Win32::System::Memory::{GlobalAlloc, GlobalLock, GlobalUnlock, GMEM_MOVEABLE};
use windows::Win32::System::Ole::{CF_DIB, CF_HDROP, DROPEFFECT_COPY, DROPEFFECT_NONE};
use windows::Win32::System::Registry::{RegSetKeyValueW, HKEY_CURRENT_USER, REG_SZ};
use windows::Win32::System::Threading::GetCurrentThreadId;
use windows::Win32::UI::Accessibility::{SetWinEventHook, HWINEVENTHOOK};
use windows::Win32::UI::Controls::{
    TaskDialogIndirect, TASKDIALOGCONFIG, TASKDIALOGCONFIG_0, TASKDIALOG_BUTTON, TDCBF_CANCEL_BUTTON,
    TDF_ALLOW_DIALOG_CANCELLATION, TD_WARNING_ICON,
};
use windows::Win32::UI::HiDpi::{GetDpiForMonitor, MDT_EFFECTIVE_DPI};
use windows::Win32::UI::Shell::Common::ITEMIDLIST;
use windows::Win32::UI::Shell::{
    BHID_DataObject, ILCreateFromPathW, ILFindLastID, ILFree, IShellItem, SHCreateItemFromParsingName,
    SHDoDragDrop, SHOpenFolderAndSelectItems, DROPFILES,
};
use windows::Win32::UI::WindowsAndMessaging::{
    EnumThreadWindows, GetAncestor, GetCursorPos, SetWindowDisplayAffinity, SetWindowPos, EVENT_OBJECT_SHOW,
    GA_ROOT, HWND_TOPMOST, OBJID_WINDOW, SWP_NOACTIVATE, SWP_SHOWWINDOW, WDA_EXCLUDEFROMCAPTURE, WDA_NONE,
    WINEVENT_OUTOFCONTEXT,
};

pub fn encode(img: &image::RgbaImage, format: ExportFormat, quality: u8) -> anyhow::Result<Vec<u8>> {
    let mut out = std::io::Cursor::new(Vec::new());
    match format {
        ExportFormat::Png => img.write_to(&mut out, image::ImageFormat::Png)?,
        ExportFormat::Jpeg => {
            let rgb = image::DynamicImage::ImageRgba8(img.clone()).to_rgb8();
            image::codecs::jpeg::JpegEncoder::new_with_quality(&mut out, quality.clamp(1, 100)).encode_image(&rgb)?;
        }
    }
    Ok(out.into_inner())
}

/// Writes an export, creating the folder. Never overwrites: a taken name gets " (2)".
pub fn export(img: &image::RgbaImage, dir: &Path, stem: &str, format: ExportFormat, quality: u8) -> anyhow::Result<PathBuf> {
    let bytes = encode(img, format, quality)?;
    std::fs::create_dir_all(dir).with_context(|| format!("creating {}", dir.display()))?;
    let path = unique_path(dir, &sanitize(stem), format.extension());
    std::fs::write(&path, bytes).with_context(|| format!("writing {}", path.display()))?;
    Ok(path)
}

pub fn unique_path(dir: &Path, stem: &str, ext: &str) -> PathBuf {
    let mut p = dir.join(format!("{stem}.{ext}"));
    let mut n = 2;
    while p.exists() {
        p = dir.join(format!("{stem} ({n}).{ext}"));
        n += 1;
    }
    p
}

/// Makes a title safe as a Windows file name.
pub fn sanitize(name: &str) -> String {
    let s: String = name
        .chars()
        .map(|c| if c.is_control() || r#"<>:"/\|?*"#.contains(c) { '_' } else { c })
        .collect();
    let s = s.trim().trim_end_matches('.').to_string();
    if s.is_empty() {
        "Capture".into()
    } else {
        s
    }
}

fn dib(img: &image::RgbaImage) -> Vec<u8> {
    let (w, h) = (img.width(), img.height());
    let header = BITMAPINFOHEADER {
        biSize: std::mem::size_of::<BITMAPINFOHEADER>() as u32,
        biWidth: w as i32,
        biHeight: h as i32,
        biPlanes: 1,
        biBitCount: 32,
        biCompression: BI_RGB.0,
        biSizeImage: w * h * 4,
        ..Default::default()
    };
    let mut out = Vec::with_capacity(header.biSize as usize + (w * h * 4) as usize);
    // SAFETY: BITMAPINFOHEADER is plain old data.
    out.extend_from_slice(unsafe { std::slice::from_raw_parts(&header as *const _ as *const u8, header.biSize as usize) });
    for row in img.rows().rev() {
        for p in row {
            out.extend_from_slice(&[p[2], p[1], p[0], p[3]]);
        }
    }
    out
}

fn drop_files(paths: &[PathBuf]) -> Vec<u8> {
    let header = DROPFILES { pFiles: std::mem::size_of::<DROPFILES>() as u32, fWide: true.into(), ..Default::default() };
    // SAFETY: DROPFILES is plain old data.
    let mut out =
        unsafe { std::slice::from_raw_parts(&header as *const _ as *const u8, std::mem::size_of::<DROPFILES>()).to_vec() };
    for p in paths {
        for u in p.as_os_str().encode_wide().chain(Some(0)) {
            out.extend_from_slice(&u.to_le_bytes());
        }
    }
    out.extend_from_slice(&[0, 0]);
    out
}

fn hglobal(bytes: &[u8]) -> anyhow::Result<HGLOBAL> {
    unsafe {
        let h = GlobalAlloc(GMEM_MOVEABLE, bytes.len())?;
        let p = GlobalLock(h) as *mut u8;
        std::ptr::copy_nonoverlapping(bytes.as_ptr(), p, bytes.len());
        let _ = GlobalUnlock(h);
        Ok(h)
    }
}

/// Puts files on the clipboard (paste into Explorer or a chat app), plus pixels for one image.
pub fn copy_files(paths: &[PathBuf], image: Option<&image::RgbaImage>) -> anyhow::Result<()> {
    unsafe {
        let mut opened = false;
        for _ in 0..10 {
            if OpenClipboard(None).is_ok() {
                opened = true;
                break;
            }
            std::thread::sleep(std::time::Duration::from_millis(20));
        }
        anyhow::ensure!(opened, "the clipboard is busy");
        let result = (|| -> anyhow::Result<()> {
            EmptyClipboard()?;
            SetClipboardData(CF_HDROP.0 as u32, Some(HANDLE(hglobal(&drop_files(paths))?.0)))?;
            if let Some(img) = image {
                SetClipboardData(CF_DIB.0 as u32, Some(HANDLE(hglobal(&dib(img))?.0)))?;
            }
            Ok(())
        })();
        let _ = CloseClipboard();
        result
    }
}

/// Runs a modal OLE drag of one file, with its pixels attached for apps that take bitmaps.
/// Call it while no GPUI borrow is held: the drag loop pumps window messages.
pub fn drag_out(path: &Path, image: Option<&image::RgbaImage>) -> anyhow::Result<bool> {
    unsafe {
        let item: IShellItem = SHCreateItemFromParsingName(&HSTRING::from(path.as_os_str()), None)
            .with_context(|| format!("locating {}", path.display()))?;
        let data: IDataObject = item.BindToHandler(None, &BHID_DataObject)?;
        if let Some(img) = image {
            let fmt = FORMATETC {
                cfFormat: CF_DIB.0,
                ptd: std::ptr::null_mut(),
                dwAspect: DVASPECT_CONTENT.0,
                lindex: -1,
                tymed: TYMED_HGLOBAL.0 as u32,
            };
            let medium = STGMEDIUM {
                tymed: TYMED_HGLOBAL.0 as u32,
                u: STGMEDIUM_0 { hGlobal: hglobal(&dib(img))? },
                pUnkForRelease: std::mem::ManuallyDrop::new(None),
            };
            let _ = data.SetData(&fmt, &medium, true);
        }
        let effect = SHDoDragDrop(None, &data, None, DROPEFFECT_COPY)?;
        Ok(effect != DROPEFFECT_NONE)
    }
}

/// Opens Explorer with the given items selected. They must share a parent folder.
pub fn reveal(paths: &[PathBuf]) {
    let Some(parent) = paths.first().and_then(|p| p.parent()) else { return };
    unsafe {
        let folder = ILCreateFromPathW(&HSTRING::from(parent.as_os_str()));
        if folder.is_null() {
            return;
        }
        let items: Vec<*mut ITEMIDLIST> = paths
            .iter()
            .map(|p| ILCreateFromPathW(&HSTRING::from(p.as_os_str())))
            .filter(|p| !p.is_null())
            .collect();
        let children: Vec<*const ITEMIDLIST> = items.iter().map(|p| ILFindLastID(*p) as *const _).collect();
        let _ = SHOpenFolderAndSelectItems(folder, Some(&children), 0);
        for p in items {
            ILFree(Some(p));
        }
        ILFree(Some(folder));
    }
}

/// The physical work area (without the taskbar) and DPI scale of the monitor under the cursor.
pub fn work_area_at_cursor() -> ([i32; 4], f32) {
    unsafe {
        let mut pt = POINT::default();
        let _ = GetCursorPos(&mut pt);
        let mon = MonitorFromPoint(pt, MONITOR_DEFAULTTONEAREST);
        let mut info = MONITORINFO { cbSize: std::mem::size_of::<MONITORINFO>() as u32, ..Default::default() };
        let _ = GetMonitorInfoW(mon, &mut info);
        let (mut dx, mut dy) = (96u32, 96u32);
        let _ = GetDpiForMonitor(mon, MDT_EFFECTIVE_DPI, &mut dx, &mut dy);
        let r = info.rcWork;
        ([r.left, r.top, r.right - r.left, r.bottom - r.top], dx as f32 / 96.0)
    }
}

/// Moves a window to a physical rectangle, topmost, without activating it.
pub fn place(window: &gpui::Window, rect: [i32; 4]) {
    let Some(h) = crate::win::hwnd(window) else { return };
    unsafe {
        let _ = SetWindowPos(h, Some(HWND_TOPMOST), rect[0], rect[1], rect[2], rect[3], SWP_NOACTIVATE | SWP_SHOWWINDOW);
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SaveChoice {
    Retry,
    ChooseFolder,
    Cancel,
}

/// Blocking task dialog shown when an export cannot be written. Call it off the main thread.
pub fn save_failed_dialog(detail: &str) -> SaveChoice {
    let detail = HSTRING::from(detail);
    let retry = HSTRING::from("Retry");
    let choose = HSTRING::from("Choose Another Folder…");
    let buttons = [
        TASKDIALOG_BUTTON { nButtonID: 100, pszButtonText: PCWSTR(retry.as_ptr()) },
        TASKDIALOG_BUTTON { nButtonID: 101, pszButtonText: PCWSTR(choose.as_ptr()) },
    ];
    let config = TASKDIALOGCONFIG {
        cbSize: std::mem::size_of::<TASKDIALOGCONFIG>() as u32,
        dwFlags: TDF_ALLOW_DIALOG_CANCELLATION,
        dwCommonButtons: TDCBF_CANCEL_BUTTON,
        pszWindowTitle: w!("Glint"),
        Anonymous1: TASKDIALOGCONFIG_0 { pszMainIcon: TD_WARNING_ICON },
        pszMainInstruction: w!("The screenshot could not be saved"),
        pszContent: PCWSTR(detail.as_ptr()),
        cButtons: buttons.len() as u32,
        pButtons: buttons.as_ptr(),
        nDefaultButton: 100,
        ..Default::default()
    };
    let mut pressed = 0i32;
    unsafe {
        if TaskDialogIndirect(&config, Some(&mut pressed), None, None).is_err() {
            return SaveChoice::Cancel;
        }
    }
    match pressed {
        100 => SaveChoice::Retry,
        101 => SaveChoice::ChooseFolder,
        _ => SaveChoice::Cancel,
    }
}

pub const OPEN_WITH_TYPES: [&str; 6] = [".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif"];
const PROGID: &str = "Glint.Image";

fn reg_set(key: &str, name: Option<&str>, value: &str) -> anyhow::Result<()> {
    let data: Vec<u16> = value.encode_utf16().chain(Some(0)).collect();
    let name = name.map(HSTRING::from);
    unsafe {
        RegSetKeyValueW(
            HKEY_CURRENT_USER,
            &HSTRING::from(key),
            name.as_ref().map(|n| PCWSTR(n.as_ptr())).unwrap_or(PCWSTR::null()),
            REG_SZ.0,
            Some(data.as_ptr() as *const _),
            (data.len() * 2) as u32,
        )
        .ok()
        .with_context(|| format!("writing HKCU\\{key}"))
    }
}

/// Registers "Open with Glint" for common image types under HKCU. Safe to repeat.
pub fn register_open_with() -> anyhow::Result<()> {
    let exe = std::env::current_exe()?;
    let command = format!("\"{}\" \"%1\"", exe.display());
    let app = r"Software\Classes\Applications\glint.exe";
    reg_set(app, Some("FriendlyAppName"), "Glint")?;
    reg_set(&format!(r"{app}\shell\open\command"), None, &command)?;
    let progid = format!(r"Software\Classes\{PROGID}");
    reg_set(&progid, None, "Glint Image")?;
    reg_set(&format!(r"{progid}\shell\open"), Some("FriendlyAppName"), "Glint")?;
    reg_set(&format!(r"{progid}\shell\open\command"), None, &command)?;
    for ext in OPEN_WITH_TYPES {
        reg_set(&format!(r"{app}\SupportedTypes"), Some(ext), "")?;
        reg_set(&format!(r"Software\Classes\{ext}\OpenWithProgids"), Some(PROGID), "")?;
    }
    Ok(())
}

static INCLUDE_OWN: AtomicBool = AtomicBool::new(false);

fn apply_affinity(h: HWND) {
    let affinity = if INCLUDE_OWN.load(Ordering::Relaxed) { WDA_NONE } else { WDA_EXCLUDEFROMCAPTURE };
    unsafe {
        let _ = SetWindowDisplayAffinity(h, affinity);
    }
}

unsafe extern "system" fn on_show(_: HWINEVENTHOOK, event: u32, h: HWND, id_object: i32, _: i32, _: u32, _: u32) {
    if event == EVENT_OBJECT_SHOW && id_object == OBJID_WINDOW.0 && unsafe { GetAncestor(h, GA_ROOT) } == h {
        apply_affinity(h);
    }
}

unsafe extern "system" fn each_window(h: HWND, _: LPARAM) -> windows::core::BOOL {
    apply_affinity(h);
    true.into()
}

/// Keeps every Glint top-level window out of screen captures unless `include` is set.
/// Hooks window creation once so later windows (editor, pins, cards) are covered too.
/// Call it on the main thread, which owns every Glint window.
pub fn exclude_own_windows_from_capture(include: bool) {
    static HOOKED: AtomicBool = AtomicBool::new(false);
    INCLUDE_OWN.store(include, Ordering::Relaxed);
    unsafe {
        if !HOOKED.swap(true, Ordering::Relaxed) {
            SetWinEventHook(EVENT_OBJECT_SHOW, EVENT_OBJECT_SHOW, None, Some(on_show), std::process::id(), 0, WINEVENT_OUTOFCONTEXT);
        }
        let _ = EnumThreadWindows(GetCurrentThreadId(), Some(each_window), LPARAM(0));
    }
}
