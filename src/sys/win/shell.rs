use crate::shell::{SaveChoice, OPEN_WITH_TYPES, OPEN_WITH_VIDEO_TYPES};
use anyhow::Context as _;
use std::cell::RefCell;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::os::windows::ffi::OsStrExt;
use windows::core::{implement, w, BOOL, HRESULT, HSTRING, PCWSTR};
use windows::Win32::Foundation::{
    DATA_S_SAMEFORMATETC, DV_E_FORMATETC, DV_E_TYMED, E_INVALIDARG, E_NOTIMPL, E_OUTOFMEMORY, HANDLE, HGLOBAL, HWND, LPARAM,
    COLORREF, OLE_E_ADVISENOTSUPPORTED, POINT, RECT, SIZE, S_OK,
};
use windows::Win32::Graphics::Gdi::{
    ClientToScreen, CreateDIBSection, DeleteObject, GetMonitorInfoW, MonitorFromPoint, BITMAPINFO, BITMAPINFOHEADER, BI_RGB,
    DIB_RGB_COLORS, MONITORINFO, MONITOR_DEFAULTTONEAREST,
};
use windows::Win32::System::Com::{
    CoCreateInstance, IAdviseSink, IDataObject, IDataObject_Impl, IEnumFORMATETC, IEnumSTATDATA, CLSCTX_INPROC_SERVER,
    DATADIR_GET, DVASPECT_CONTENT, FORMATETC, IStream, STGMEDIUM, STGMEDIUM_0, STREAM_SEEK_SET, TYMED_HGLOBAL, TYMED_ISTREAM,
};
use windows::Win32::System::DataExchange::{CloseClipboard, EmptyClipboard, OpenClipboard, SetClipboardData};
use windows::Win32::System::Memory::{GlobalAlloc, GlobalLock, GlobalSize, GlobalUnlock, GMEM_MOVEABLE};
use windows::Win32::System::Ole::{ReleaseStgMedium, CF_DIB, CF_HDROP, DROPEFFECT_COPY, DROPEFFECT_NONE};
use windows::Win32::System::Registry::{RegGetValueW, RegSetKeyValueW, HKEY_CURRENT_USER, REG_SZ, RRF_RT_REG_SZ};
use windows::Win32::System::Threading::GetCurrentThreadId;
use windows::Win32::UI::Accessibility::{SetWinEventHook, HWINEVENTHOOK};
use windows::Win32::UI::Controls::{
    TaskDialogIndirect, CLR_NONE, TASKDIALOGCONFIG, TASKDIALOGCONFIG_0, TASKDIALOG_BUTTON, TDCBF_CANCEL_BUTTON,
    TDF_ALLOW_DIALOG_CANCELLATION, TD_WARNING_ICON,
};
use windows::Win32::UI::HiDpi::{GetDpiForMonitor, MDT_EFFECTIVE_DPI};
use windows::Win32::UI::Shell::Common::ITEMIDLIST;
use windows::Win32::UI::Shell::{
    IDragSourceHelper, ILCreateFromPathW, ILFindLastID, ILFree, SHCreateMemStream, SHCreateStdEnumFmtEtc, SHDoDragDrop,
    SHOpenFolderAndSelectItems, CLSID_DragDropHelper, DROPFILES, SHDRAGIMAGE,
};
use windows::Win32::UI::WindowsAndMessaging::{
    EnumThreadWindows, GetAncestor, GetClientRect, GetCursorPos, GetWindowRect, SetWindowDisplayAffinity, SetWindowPos, EVENT_OBJECT_SHOW,
    GA_ROOT, HWND_TOPMOST, OBJID_WINDOW, SWP_NOACTIVATE, SWP_SHOWWINDOW, WDA_EXCLUDEFROMCAPTURE, WDA_NONE,
    WINEVENT_OUTOFCONTEXT,
};

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

/// Drags one file into other apps, with its pixels attached for apps that take bitmaps and
/// `thumb` floating under the cursor. `done` gets whether something took the drop. The OLE drag
/// is modal, so `done` runs before this returns. Call it while no GPUI borrow is held: the drag
/// loop pumps window messages.
pub fn drag_out(path: &Path, image: Option<&image::RgbaImage>, thumb: &gpui::RenderImage, done: impl FnOnce(anyhow::Result<bool>) + 'static) {
    done(drag_modal(path, image, thumb))
}

fn drag_modal(path: &Path, image: Option<&image::RgbaImage>, thumb: &gpui::RenderImage) -> anyhow::Result<bool> {
    let data = DragData::default();
    data.set(CF_HDROP.0, Medium::HGlobal, drop_files(std::slice::from_ref(&path.to_path_buf())));
    if let Some(img) = image {
        data.set(CF_DIB.0, Medium::HGlobal, dib(img));
    }
    let data: IDataObject = data.into();
    if let Err(e) = set_drag_image(&data, thumb) {
        eprintln!("dragging without a thumbnail: {e:#}");
    }
    unsafe {
        let effect = SHDoDragDrop(None, &data, None, DROPEFFECT_COPY)?;
        Ok(effect != DROPEFFECT_NONE)
    }
}

/// Without an image on the data object, SHDoDragDrop drags only the cursor. The thumbnail is
/// fitted to 96 DIPs, the size of Explorer's own drag thumbnails.
fn set_drag_image(data: &IDataObject, thumb: &gpui::RenderImage) -> anyhow::Result<()> {
    let size = thumb.size(0);
    let bgra = thumb
        .as_bytes(0)
        .and_then(|b| image::RgbaImage::from_raw(size.width.0 as u32, size.height.0 as u32, b.to_vec()))
        .context("the card thumbnail has no pixels")?;
    let fit = 96.0 * work_area_at_cursor().1 / bgra.width().max(bgra.height()) as f32;
    let (w, h) = (((bgra.width() as f32 * fit).round() as u32).max(1), ((bgra.height() as f32 * fit).round() as u32).max(1));
    let small = image::imageops::thumbnail(&bgra, w, h);
    unsafe {
        let helper: IDragSourceHelper = CoCreateInstance(&CLSID_DragDropHelper, None, CLSCTX_INPROC_SERVER)?;
        let info = BITMAPINFO {
            bmiHeader: BITMAPINFOHEADER {
                biSize: std::mem::size_of::<BITMAPINFOHEADER>() as u32,
                biWidth: w as i32,
                biHeight: -(h as i32),
                biPlanes: 1,
                biBitCount: 32,
                biCompression: BI_RGB.0,
                ..Default::default()
            },
            ..Default::default()
        };
        let mut bits = std::ptr::null_mut();
        let bitmap = CreateDIBSection(None, &info, DIB_RGB_COLORS, &mut bits, None, 0)?;
        let dst = std::slice::from_raw_parts_mut(bits as *mut u8, (w * h * 4) as usize);
        for (d, p) in dst.chunks_exact_mut(4).zip(small.pixels()) {
            let a = p[3] as u32;
            d.copy_from_slice(&[(p[0] as u32 * a / 255) as u8, (p[1] as u32 * a / 255) as u8, (p[2] as u32 * a / 255) as u8, p[3]]);
        }
        let image = SHDRAGIMAGE {
            sizeDragImage: SIZE { cx: w as i32, cy: h as i32 },
            ptOffset: POINT { x: w as i32 / 2, y: h as i32 / 2 },
            hbmpDragImage: bitmap,
            crColorKey: COLORREF(CLR_NONE as u32),
        };
        helper.InitializeFromBitmap(&image, data).inspect_err(|_| {
            let _ = DeleteObject(bitmap.into());
        })?;
    }
    Ok(())
}

/// How a stored format travels. The drag image helper stores its DragContext as a stream.
#[derive(Clone, Copy)]
enum Medium {
    HGlobal,
    Stream,
}

impl Medium {
    fn tymed(self) -> u32 {
        match self {
            Medium::HGlobal => TYMED_HGLOBAL.0 as u32,
            Medium::Stream => TYMED_ISTREAM.0 as u32,
        }
    }
}

fn stored_format(cf: u16, medium: Medium) -> FORMATETC {
    FORMATETC { cfFormat: cf, ptd: std::ptr::null_mut(), dwAspect: DVASPECT_CONTENT.0, lindex: -1, tymed: medium.tymed() }
}

/// The data object behind a card drag: each format kept as bytes and handed out fresh per
/// GetData, HGLOBALs as GMEM_MOVEABLE blocks, which is what Explorer's drags deliver and what
/// GPUI 0.2.2's drop target can read. That target dereferences the CF_HDROP handle as a
/// moveable handle's pointer slot; the GMEM_FIXED blocks the shell's own data objects return
/// crash it.
#[implement(IDataObject)]
#[derive(Default)]
struct DragData(RefCell<Vec<(u16, Medium, Vec<u8>)>>);

impl DragData {
    fn set(&self, cf: u16, medium: Medium, bytes: Vec<u8>) {
        let mut entries = self.0.borrow_mut();
        match entries.iter_mut().find(|(f, _, _)| *f == cf) {
            Some(entry) => *entry = (cf, medium, bytes),
            None => entries.push((cf, medium, bytes)),
        }
    }

    fn has(&self, fmt: &FORMATETC) -> bool {
        fmt.dwAspect == DVASPECT_CONTENT.0
            && self.0.borrow().iter().any(|(f, m, _)| *f == fmt.cfFormat && fmt.tymed & m.tymed() != 0)
    }

    fn get(&self, fmt: &FORMATETC) -> Option<(Medium, Vec<u8>)> {
        if !self.has(fmt) {
            return None;
        }
        self.0.borrow().iter().find(|(f, _, _)| *f == fmt.cfFormat).map(|(_, m, b)| (*m, b.clone()))
    }
}

unsafe fn read_stream(stream: &IStream) -> windows::core::Result<Vec<u8>> {
    unsafe {
        stream.Seek(0, STREAM_SEEK_SET, None)?;
        let mut out = Vec::new();
        let mut buf = [0u8; 4096];
        loop {
            let mut n = 0u32;
            stream.Read(buf.as_mut_ptr().cast(), buf.len() as u32, Some(&mut n)).ok()?;
            if n == 0 {
                return Ok(out);
            }
            out.extend_from_slice(&buf[..n as usize]);
        }
    }
}

impl IDataObject_Impl for DragData_Impl {
    fn GetData(&self, fmt: *const FORMATETC) -> windows::core::Result<STGMEDIUM> {
        let (medium, bytes) = self.get(unsafe { &*fmt }).ok_or(DV_E_FORMATETC)?;
        let u = match medium {
            Medium::HGlobal => STGMEDIUM_0 { hGlobal: hglobal(&bytes).map_err(|_| E_OUTOFMEMORY)? },
            Medium::Stream => {
                let stream = unsafe { SHCreateMemStream(Some(&bytes)) }.ok_or(E_OUTOFMEMORY)?;
                STGMEDIUM_0 { pstm: std::mem::ManuallyDrop::new(Some(stream)) }
            }
        };
        Ok(STGMEDIUM { tymed: medium.tymed(), u, pUnkForRelease: std::mem::ManuallyDrop::new(None) })
    }

    fn GetDataHere(&self, _: *const FORMATETC, _: *mut STGMEDIUM) -> windows::core::Result<()> {
        Err(E_NOTIMPL.into())
    }

    fn QueryGetData(&self, fmt: *const FORMATETC) -> HRESULT {
        if self.has(unsafe { &*fmt }) {
            S_OK
        } else {
            DV_E_FORMATETC
        }
    }

    fn GetCanonicalFormatEtc(&self, fmt: *const FORMATETC, out: *mut FORMATETC) -> HRESULT {
        if fmt.is_null() || out.is_null() {
            return E_INVALIDARG;
        }
        unsafe {
            *out = *fmt;
            (*out).ptd = std::ptr::null_mut();
        }
        DATA_S_SAMEFORMATETC
    }

    fn SetData(&self, fmt: *const FORMATETC, medium: *const STGMEDIUM, release: BOOL) -> windows::core::Result<()> {
        let (fmt, medium) = unsafe { (&*fmt, &*medium) };
        let (kind, bytes) = if medium.tymed == TYMED_HGLOBAL.0 as u32 {
            let bytes = unsafe {
                let h = medium.u.hGlobal;
                let p = GlobalLock(h) as *const u8;
                if p.is_null() {
                    return Err(E_INVALIDARG.into());
                }
                let bytes = std::slice::from_raw_parts(p, GlobalSize(h)).to_vec();
                let _ = GlobalUnlock(h);
                bytes
            };
            (Medium::HGlobal, bytes)
        } else if medium.tymed == TYMED_ISTREAM.0 as u32 {
            let stream = unsafe { medium.u.pstm.as_ref() }.ok_or(E_INVALIDARG)?;
            (Medium::Stream, unsafe { read_stream(stream) }?)
        } else {
            return Err(DV_E_TYMED.into());
        };
        self.set(fmt.cfFormat, kind, bytes);
        if release.as_bool() {
            unsafe { ReleaseStgMedium(medium as *const _ as *mut _) };
        }
        Ok(())
    }

    fn EnumFormatEtc(&self, direction: u32) -> windows::core::Result<IEnumFORMATETC> {
        if direction != DATADIR_GET.0 as u32 {
            return Err(E_NOTIMPL.into());
        }
        let formats: Vec<FORMATETC> = self.0.borrow().iter().map(|(cf, m, _)| stored_format(*cf, *m)).collect();
        unsafe { SHCreateStdEnumFmtEtc(&formats) }
    }

    fn DAdvise(&self, _: *const FORMATETC, _: u32, _: windows::core::Ref<IAdviseSink>) -> windows::core::Result<u32> {
        Err(OLE_E_ADVISENOTSUPPORTED.into())
    }

    fn DUnadvise(&self, _: u32) -> windows::core::Result<()> {
        Err(OLE_E_ADVISENOTSUPPORTED.into())
    }

    fn EnumDAdvise(&self) -> windows::core::Result<IEnumSTATDATA> {
        Err(OLE_E_ADVISENOTSUPPORTED.into())
    }
}

/// Unregisters the OLE drop target GPUI gives every window. GPUI's target answers every file
/// drag with DROPEFFECT_COPY, so a card released back over the stack it came from would
/// report a drop that copied nothing.
pub fn refuse_drops(window: &gpui::Window) {
    let Some(h) = super::desktop::hwnd(window) else { return };
    unsafe {
        let _ = windows::Win32::System::Ole::RevokeDragDrop(h);
    }
}

/// Opens Explorer with the given items selected. They must share a parent folder.
/// Runs on its own COM thread: the shell call waits on Explorer and pumps messages, which
/// would re-enter GPUI (and abort) if it ran inside a click handler on the UI thread.
pub fn reveal(paths: &[PathBuf]) {
    use windows::Win32::System::Com::{CoInitializeEx, CoUninitialize, COINIT_APARTMENTTHREADED};
    let paths = paths.to_vec();
    std::thread::spawn(move || {
        let Some(parent) = paths.first().and_then(|p| p.parent()) else { return };
        unsafe {
            let inited = CoInitializeEx(None, COINIT_APARTMENTTHREADED).is_ok();
            let folder = ILCreateFromPathW(&HSTRING::from(parent.as_os_str()));
            if !folder.is_null() {
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
            if inited {
                CoUninitialize();
            }
        }
    });
}

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

/// Moves a window so its client area covers a physical rectangle, topmost, without
/// activating it. The move runs as a separate task: WM_SIZE must reach GPUI while no app
/// borrow is held, or GPUI keeps rendering at the old size.
pub fn place(window: &gpui::Window, rect: [i32; 4], cx: &gpui::App) {
    let Some(h) = super::desktop::hwnd(window) else { return };
    cx.foreground_executor()
        .spawn(async move {
            unsafe {
                // GPUI windows keep invisible resize borders outside the client area.
                let (mut outer, mut inner, mut origin) = (RECT::default(), RECT::default(), POINT::default());
                let _ = GetWindowRect(h, &mut outer);
                let _ = GetClientRect(h, &mut inner);
                let _ = ClientToScreen(h, &mut origin);
                let (left, top) = (origin.x - outer.left, origin.y - outer.top);
                let extra_w = (outer.right - outer.left) - inner.right;
                let extra_h = (outer.bottom - outer.top) - inner.bottom;
                let _ = SetWindowPos(
                    h,
                    Some(HWND_TOPMOST),
                    rect[0] - left,
                    rect[1] - top,
                    rect[2] + extra_w,
                    rect[3] + extra_h,
                    SWP_NOACTIVATE | SWP_SHOWWINDOW,
                );
            }
        })
        .detach();
}

/// Gives a regular window the Windows 11 Mica backdrop. GPUI must open the window with a
/// see-through background for the backdrop to show; GPUI keeps the title bar's light or
/// dark mode in step with the system.
pub fn mica(window: &gpui::Window) {
    use windows::Win32::Graphics::Dwm::{DwmSetWindowAttribute, DWMSBT_MAINWINDOW, DWMWA_SYSTEMBACKDROP_TYPE};
    let Some(h) = super::desktop::hwnd(window) else { return };
    let backdrop = DWMSBT_MAINWINDOW;
    unsafe {
        let _ = DwmSetWindowAttribute(h, DWMWA_SYSTEMBACKDROP_TYPE, &backdrop as *const _ as *const _, 4);
    }
    caption_text(window);
}

/// Sets the title text color for the current theme. Over GPUI's blurred backdrop DWM draws
/// the active title in white even in light mode, so light mode asks for black explicitly.
pub fn caption_text(window: &gpui::Window) {
    use windows::Win32::Graphics::Dwm::{DwmSetWindowAttribute, DWMWA_TEXT_COLOR};
    let Some(h) = super::desktop::hwnd(window) else { return };
    const DWMWA_COLOR_DEFAULT: u32 = 0xFFFF_FFFF;
    let color: u32 = if crate::ui::theme().dark { DWMWA_COLOR_DEFAULT } else { 0x0000_0000 };
    unsafe {
        let _ = DwmSetWindowAttribute(h, DWMWA_TEXT_COLOR, &color as *const _ as *const _, 4);
    }
}

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

const PROGID: &str = "Glint.Image";

const VIDEO_PROGID: &str = "Glint.Video";

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

fn reg_get(key: &str) -> Option<String> {
    let key = HSTRING::from(key);
    let mut bytes = 0u32;
    unsafe {
        RegGetValueW(HKEY_CURRENT_USER, &key, PCWSTR::null(), RRF_RT_REG_SZ, None, None, Some(&mut bytes)).ok().ok()?;
        let mut buf = vec![0u16; bytes as usize / 2];
        RegGetValueW(HKEY_CURRENT_USER, &key, PCWSTR::null(), RRF_RT_REG_SZ, None, Some(buf.as_mut_ptr().cast()), Some(&mut bytes))
            .ok()
            .ok()?;
        let len = buf.iter().position(|&c| c == 0).unwrap_or(buf.len());
        Some(String::from_utf16_lossy(&buf[..len]))
    }
}

fn command_exe(command: &str) -> Option<PathBuf> {
    let quoted = command.strip_prefix('"')?;
    Some(PathBuf::from(&quoted[..quoted.find('"')?]))
}

/// True when the registered open command already launches this exe, so a newer build at
/// another path takes the registration over and the same build leaves it alone.
fn registered_here(command: &str, exe: &Path) -> bool {
    command_exe(command).is_some_and(|p| p.as_os_str().eq_ignore_ascii_case(exe.as_os_str()))
}

pub fn register_open_with() -> anyhow::Result<()> {
    let app = r"Software\Classes\Applications\glint.exe";
    let command_key = format!(r"{app}\shell\open\command");
    let exe = std::env::current_exe()?;
    if reg_get(&command_key).is_some_and(|c| registered_here(&c, &exe)) {
        return Ok(());
    }
    let command = format!("\"{}\" \"%1\"", exe.display());
    reg_set(app, Some("FriendlyAppName"), "Glint")?;
    reg_set(&command_key, None, &command)?;
    let progid = format!(r"Software\Classes\{PROGID}");
    reg_set(&progid, None, "Glint Image")?;
    reg_set(&format!(r"{progid}\shell\open"), Some("FriendlyAppName"), "Glint")?;
    reg_set(&format!(r"{progid}\shell\open\command"), None, &command)?;
    for ext in OPEN_WITH_TYPES {
        reg_set(&format!(r"{app}\SupportedTypes"), Some(ext), "")?;
        reg_set(&format!(r"Software\Classes\{ext}\OpenWithProgids"), Some(PROGID), "")?;
    }
    let trim = format!("\"{}\" --trim \"%1\"", exe.display());
    let progid = format!(r"Software\Classes\{VIDEO_PROGID}");
    reg_set(&progid, None, "Glint Video")?;
    reg_set(&format!(r"{progid}\shell\open"), Some("FriendlyAppName"), "Glint Trim & Compress")?;
    reg_set(&format!(r"{progid}\shell\open\command"), None, &trim)?;
    for ext in OPEN_WITH_VIDEO_TYPES {
        reg_set(&format!(r"{app}\SupportedTypes"), Some(ext), "")?;
        reg_set(&format!(r"Software\Classes\{ext}\OpenWithProgids"), Some(VIDEO_PROGID), "")?;
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn command_exe_reads_the_quoted_program() {
        let command = r#""C:\Program Files\Glint\glint.exe" --trim "%1""#;
        assert_eq!(command_exe(command), Some(PathBuf::from(r"C:\Program Files\Glint\glint.exe")));
        assert_eq!(command_exe(r#"glint.exe "%1""#), None);
        assert_eq!(command_exe(r#""unterminated"#), None);
    }

    #[test]
    fn only_a_command_for_this_exe_counts_as_registered() {
        let exe = Path::new(r"C:\Users\me\Glint\glint.exe");
        assert!(registered_here(r#""C:\Users\ME\glint\GLINT.EXE" "%1""#, exe));
        assert!(!registered_here(r#""C:\Users\me\old-build\glint.exe" "%1""#, exe));
        assert!(!registered_here("glint.exe %1", exe));
    }
}

#[cfg(test)]
mod drag_data {
    use super::*;
    use windows::Win32::UI::Shell::{DragQueryFileW, HDROP};

    fn hglobal_format(cf: u16) -> FORMATETC {
        stored_format(cf, Medium::HGlobal)
    }

    /// GPUI 0.2.2's DragEnter: GetData(CF_HDROP), dereference the HGLOBAL as a pointer to an
    /// HDROP, DragQueryFileW on what came out.
    unsafe fn read_like_gpui(data: &IDataObject) -> Vec<String> {
        let fmt = hglobal_format(CF_HDROP.0);
        assert_eq!(data.QueryGetData(&fmt), S_OK);
        let mut medium = data.GetData(&fmt).unwrap();
        let hdrop = *(medium.u.hGlobal.0 as *mut HDROP);
        let n = DragQueryFileW(hdrop, 0xFFFF_FFFF, None);
        let mut out = Vec::new();
        for i in 0..n {
            let len = DragQueryFileW(hdrop, i, None) as usize;
            let mut buf = vec![0u16; len + 1];
            DragQueryFileW(hdrop, i, Some(&mut buf));
            out.push(String::from_utf16_lossy(&buf[..len]));
        }
        ReleaseStgMedium(&mut medium);
        out
    }

    #[test]
    fn the_drag_data_object_survives_gpuis_hdrop_read_twice() {
        let path = PathBuf::from(r"C:\Glint\Drag\Capture.png");
        let data = DragData::default();
        data.set(CF_HDROP.0, Medium::HGlobal, drop_files(std::slice::from_ref(&path)));
        let data: IDataObject = data.into();
        let got = unsafe { read_like_gpui(&data) };
        assert_eq!(got, vec![r"C:\Glint\Drag\Capture.png".to_string()]);
        assert_eq!(unsafe { read_like_gpui(&data) }, got);
        assert_ne!(unsafe { data.QueryGetData(&hglobal_format(CF_DIB.0)) }, S_OK);
    }

    #[test]
    fn set_data_stores_a_copy_and_releases_the_medium() {
        let data: IDataObject = DragData::default().into();
        let fmt = hglobal_format(CF_DIB.0);
        let medium = STGMEDIUM {
            tymed: TYMED_HGLOBAL.0 as u32,
            u: STGMEDIUM_0 { hGlobal: hglobal(&[1, 2, 3]).unwrap() },
            pUnkForRelease: std::mem::ManuallyDrop::new(None),
        };
        unsafe {
            data.SetData(&fmt, &medium, true).unwrap();
            let mut out = data.GetData(&fmt).unwrap();
            let p = GlobalLock(out.u.hGlobal) as *const u8;
            assert_eq!(std::slice::from_raw_parts(p, GlobalSize(out.u.hGlobal)), &[1, 2, 3]);
            let _ = GlobalUnlock(out.u.hGlobal);
            ReleaseStgMedium(&mut out);
        }
    }

    #[test]
    fn set_data_refuses_a_medium_without_memory() {
        let data: IDataObject = DragData::default().into();
        let fmt = hglobal_format(CF_DIB.0);
        let medium = STGMEDIUM {
            tymed: TYMED_HGLOBAL.0 as u32,
            u: STGMEDIUM_0 { hGlobal: HGLOBAL(std::ptr::null_mut()) },
            pUnkForRelease: std::mem::ManuallyDrop::new(None),
        };
        unsafe {
            assert_eq!(data.SetData(&fmt, &medium, false).unwrap_err().code(), E_INVALIDARG);
            assert_eq!(data.QueryGetData(&fmt), DV_E_FORMATETC);
        }
    }

    #[test]
    fn the_shell_takes_a_card_thumbnail_as_the_drag_image() {
        use windows::Win32::System::Com::{CoInitializeEx, COINIT_APARTMENTTHREADED};
        use windows::Win32::System::DataExchange::RegisterClipboardFormatW;
        let _ = unsafe { CoInitializeEx(None, COINIT_APARTMENTTHREADED) };
        let data: IDataObject = DragData::default().into();
        let thumb = crate::desktop::render_image(&image::RgbaImage::from_pixel(64, 36, image::Rgba([40, 120, 220, 255])));
        set_drag_image(&data, &thumb).unwrap();
        let bits = unsafe { RegisterClipboardFormatW(w!("DragImageBits")) } as u16;
        assert_eq!(unsafe { data.QueryGetData(&hglobal_format(bits)) }, S_OK);
    }

    #[test]
    fn the_canonical_format_is_the_one_asked_for() {
        let data: IDataObject = DragData::default().into();
        let asked = hglobal_format(CF_HDROP.0);
        let mut out = FORMATETC::default();
        assert_eq!(unsafe { data.GetCanonicalFormatEtc(&asked, &mut out) }, DATA_S_SAMEFORMATETC);
        assert_eq!((out.cfFormat, out.dwAspect, out.lindex, out.tymed), (CF_HDROP.0, DVASPECT_CONTENT.0, -1, TYMED_HGLOBAL.0 as u32));
        assert!(out.ptd.is_null());
    }
}
