use super::mf;
use crate::devices::{AudioDevice, CameraDevice, DeviceId, DisplayInfo, Rect};
use anyhow::Context;
use windows::core::PWSTR;
use windows::Win32::Devices::Display::{
    DisplayConfigGetDeviceInfo, GetDisplayConfigBufferSizes, QueryDisplayConfig, DISPLAYCONFIG_DEVICE_INFO_GET_SOURCE_NAME,
    DISPLAYCONFIG_DEVICE_INFO_GET_TARGET_NAME, DISPLAYCONFIG_DEVICE_INFO_HEADER, DISPLAYCONFIG_MODE_INFO,
    DISPLAYCONFIG_PATH_INFO, DISPLAYCONFIG_SOURCE_DEVICE_NAME, DISPLAYCONFIG_TARGET_DEVICE_NAME, QDC_ONLY_ACTIVE_PATHS,
};
use windows::Win32::Devices::FunctionDiscovery::PKEY_Device_FriendlyName;
use windows::Win32::Foundation::{ERROR_SUCCESS, LPARAM, RECT};
use windows::Win32::Graphics::Gdi::{EnumDisplayMonitors, GetMonitorInfoW, HDC, HMONITOR, MONITORINFOEXW};
use windows::Win32::Media::Audio::{eCapture, IMMDeviceEnumerator, MMDeviceEnumerator, DEVICE_STATE_ACTIVE};
use windows::Win32::Media::MediaFoundation::{
    IMFActivate, MFCreateAttributes, MFEnumDeviceSources, MF_DEVSOURCE_ATTRIBUTE_FRIENDLY_NAME, MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE,
    MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_GUID, MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_SYMBOLIC_LINK,
};
use windows::Win32::System::Com::{CoCreateInstance, CoTaskMemFree, CLSCTX_ALL, STGM_READ};
use windows::Win32::UI::HiDpi::{
    GetDpiForMonitor, SetThreadDpiAwarenessContext, DPI_AWARENESS_CONTEXT, DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2,
    MDT_EFFECTIVE_DPI,
};
use windows::Win32::UI::WindowsAndMessaging::MONITORINFOF_PRIMARY;

/// Makes the calling thread per-monitor DPI aware until dropped, so monitor and window
/// rectangles come back in physical pixels whatever the process manifest says.
pub(crate) struct PhysicalPixels(DPI_AWARENESS_CONTEXT);

impl PhysicalPixels {
    pub fn enter() -> Self {
        Self(unsafe { SetThreadDpiAwarenessContext(DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2) })
    }
}

impl Drop for PhysicalPixels {
    fn drop(&mut self) {
        if !self.0.0.is_null() {
            unsafe { SetThreadDpiAwarenessContext(self.0) };
        }
    }
}

impl Rect {
    pub(crate) fn from_win32(r: RECT) -> Self {
        Self { x: r.left, y: r.top, width: (r.right - r.left).max(0) as u32, height: (r.bottom - r.top).max(0) as u32 }
    }
}

/// All active displays, primary first, then in enumeration order.
pub fn displays() -> anyhow::Result<Vec<DisplayInfo>> {
    let _physical = PhysicalPixels::enter();
    let mut handles: Vec<HMONITOR> = Vec::new();
    unsafe extern "system" fn collect(h: HMONITOR, _: HDC, _: *mut RECT, data: LPARAM) -> windows::core::BOOL {
        unsafe { (*(data.0 as *mut Vec<HMONITOR>)).push(h) };
        true.into()
    }
    unsafe { EnumDisplayMonitors(None, None, Some(collect), LPARAM(&mut handles as *mut _ as isize)).ok().context("EnumDisplayMonitors")? };
    let names = monitor_names();
    let mut out = Vec::new();
    for h in handles {
        let mut info = MONITORINFOEXW::default();
        info.monitorInfo.cbSize = std::mem::size_of::<MONITORINFOEXW>() as u32;
        unsafe { GetMonitorInfoW(h, &mut info.monitorInfo).ok().context("GetMonitorInfoW")? };
        let mut dpi = (96u32, 96u32);
        unsafe { GetDpiForMonitor(h, MDT_EFFECTIVE_DPI, &mut dpi.0, &mut dpi.1).context("GetDpiForMonitor")? };
        let device = utf16_str(&info.szDevice);
        let primary = info.monitorInfo.dwFlags & MONITORINFOF_PRIMARY != 0;
        out.push(DisplayInfo {
            index: 0,
            name: names.iter().find(|(d, _)| *d == device).map(|(_, n)| n.clone()).unwrap_or_default(),
            rect: Rect::from_win32(info.monitorInfo.rcMonitor),
            scale: dpi.0 as f32 / 96.0,
            handle: h.0 as isize,
            primary,
        });
    }
    out.sort_by_key(|d| !d.primary);
    for (i, d) in out.iter_mut().enumerate() {
        d.index = i;
        if d.name.is_empty() {
            d.name = format!("Display {}", i + 1);
        }
    }
    Ok(out)
}

fn monitor_names() -> Vec<(String, String)> {
    let mut out = Vec::new();
    unsafe {
        let (mut paths, mut modes) = (0u32, 0u32);
        if GetDisplayConfigBufferSizes(QDC_ONLY_ACTIVE_PATHS, &mut paths, &mut modes) != ERROR_SUCCESS {
            return out;
        }
        let mut path_buf = vec![DISPLAYCONFIG_PATH_INFO::default(); paths as usize];
        let mut mode_buf = vec![DISPLAYCONFIG_MODE_INFO::default(); modes as usize];
        if QueryDisplayConfig(QDC_ONLY_ACTIVE_PATHS, &mut paths, path_buf.as_mut_ptr(), &mut modes, mode_buf.as_mut_ptr(), None)
            != ERROR_SUCCESS
        {
            return out;
        }
        for path in &path_buf[..paths as usize] {
            let mut source = DISPLAYCONFIG_SOURCE_DEVICE_NAME {
                header: DISPLAYCONFIG_DEVICE_INFO_HEADER {
                    r#type: DISPLAYCONFIG_DEVICE_INFO_GET_SOURCE_NAME,
                    size: std::mem::size_of::<DISPLAYCONFIG_SOURCE_DEVICE_NAME>() as u32,
                    adapterId: path.sourceInfo.adapterId,
                    id: path.sourceInfo.id,
                },
                ..Default::default()
            };
            let mut target = DISPLAYCONFIG_TARGET_DEVICE_NAME {
                header: DISPLAYCONFIG_DEVICE_INFO_HEADER {
                    r#type: DISPLAYCONFIG_DEVICE_INFO_GET_TARGET_NAME,
                    size: std::mem::size_of::<DISPLAYCONFIG_TARGET_DEVICE_NAME>() as u32,
                    adapterId: path.targetInfo.adapterId,
                    id: path.targetInfo.id,
                },
                ..Default::default()
            };
            if DisplayConfigGetDeviceInfo(&mut source.header) == 0 && DisplayConfigGetDeviceInfo(&mut target.header) == 0 {
                let name = utf16_str(&target.monitorFriendlyDeviceName);
                if !name.is_empty() {
                    out.push((utf16_str(&source.viewGdiDeviceName), name));
                }
            }
        }
    }
    out
}

/// Active microphones, the default one first.
pub fn microphones() -> anyhow::Result<Vec<AudioDevice>> {
    mf::init()?;
    unsafe {
        let enumerator: IMMDeviceEnumerator = CoCreateInstance(&MMDeviceEnumerator, None, CLSCTX_ALL).context("MMDeviceEnumerator")?;
        let default_id = enumerator
            .GetDefaultAudioEndpoint(eCapture, windows::Win32::Media::Audio::eConsole)
            .ok()
            .and_then(|d| d.GetId().ok())
            .map(|p| take_pwstr(p))
            .unwrap_or_default();
        let collection = enumerator.EnumAudioEndpoints(eCapture, DEVICE_STATE_ACTIVE).context("EnumAudioEndpoints")?;
        let mut out = Vec::new();
        for i in 0..collection.GetCount()? {
            let device = collection.Item(i)?;
            let id = take_pwstr(device.GetId()?);
            let store = device.OpenPropertyStore(STGM_READ)?;
            let mut value = store.GetValue(&PKEY_Device_FriendlyName)?;
            let name = value.Anonymous.Anonymous.Anonymous.pwszVal.to_string().unwrap_or_default();
            let _ = windows::Win32::System::Com::StructuredStorage::PropVariantClear(&mut value);
            out.push(AudioDevice { id: DeviceId(id), name });
        }
        out.sort_by_key(|d| d.id.0 != default_id);
        Ok(out)
    }
}

/// Attached cameras in Media Foundation enumeration order.
pub fn cameras() -> anyhow::Result<Vec<CameraDevice>> {
    mf::init()?;
    unsafe {
        let mut attrs = None;
        MFCreateAttributes(&mut attrs, 1)?;
        let attrs = attrs.unwrap();
        attrs.SetGUID(&MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE, &MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_GUID)?;
        let mut activates = std::ptr::null_mut();
        let mut count = 0u32;
        MFEnumDeviceSources(&attrs, &mut activates, &mut count).context("MFEnumDeviceSources")?;
        let mut out = Vec::new();
        for i in 0..count as usize {
            if let Some(activate) = (*activates.add(i)).take() {
                let name = allocated_string(&activate, &MF_DEVSOURCE_ATTRIBUTE_FRIENDLY_NAME);
                let link = allocated_string(&activate, &MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_SYMBOLIC_LINK);
                out.push(CameraDevice { id: DeviceId(link), name });
            }
        }
        CoTaskMemFree(Some(activates as *const _));
        Ok(out)
    }
}

pub(crate) fn camera_activate(id: &DeviceId) -> anyhow::Result<IMFActivate> {
    mf::init()?;
    unsafe {
        let mut attrs = None;
        MFCreateAttributes(&mut attrs, 1)?;
        let attrs = attrs.unwrap();
        attrs.SetGUID(&MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE, &MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_GUID)?;
        let mut activates = std::ptr::null_mut();
        let mut count = 0u32;
        MFEnumDeviceSources(&attrs, &mut activates, &mut count).context("MFEnumDeviceSources")?;
        let mut found = None;
        for i in 0..count as usize {
            if let Some(activate) = (*activates.add(i)).take() {
                if found.is_none() && allocated_string(&activate, &MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_SYMBOLIC_LINK) == id.0 {
                    found = Some(activate);
                }
            }
        }
        CoTaskMemFree(Some(activates as *const _));
        found.with_context(|| format!("no camera with id {id}"))
    }
}

unsafe fn allocated_string(activate: &IMFActivate, key: &windows::core::GUID) -> String {
    let mut p = PWSTR::null();
    let mut len = 0u32;
    if unsafe { activate.GetAllocatedString(key, &mut p, &mut len) }.is_err() {
        return String::new();
    }
    unsafe { take_pwstr(p) }
}

unsafe fn take_pwstr(p: PWSTR) -> String {
    let s = unsafe { p.to_string() }.unwrap_or_default();
    unsafe { CoTaskMemFree(Some(p.0 as *const _)) };
    s
}

fn utf16_str(buf: &[u16]) -> String {
    let end = buf.iter().position(|&c| c == 0).unwrap_or(buf.len());
    String::from_utf16_lossy(&buf[..end])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    #[ignore = "needs an interactive desktop session"]
    fn primary_display_is_first_with_physical_size() {
        let list = displays().unwrap();
        let d = &list[0];
        assert!(d.primary);
        assert_eq!(d.index, 0);
        assert!(d.rect.width >= 640 && d.rect.height >= 480, "{:?}", d.rect);
        assert!(d.scale >= 1.0, "scale {}", d.scale);
        assert_ne!(d.handle, 0);
        assert!(!d.name.is_empty());
        for (i, d) in list.iter().enumerate() {
            assert_eq!(d.index, i);
        }
    }

    #[test]
    fn audio_and_camera_enumeration_do_not_fail() {
        for m in microphones().unwrap() {
            assert!(m.id.0.starts_with("{0.0.1.00000000}"), "endpoint id {}", m.id);
            assert!(!m.name.is_empty());
        }
        for c in cameras().unwrap() {
            assert!(!c.id.0.is_empty());
            assert!(!c.name.is_empty());
        }
    }
}
