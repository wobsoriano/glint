use crate::ui::Appearance;
use windows::core::{w, PCWSTR};
use windows::Win32::System::Registry::{RegGetValueW, HKEY_CURRENT_USER, RRF_RT_REG_DWORD};

pub fn appearance() -> Appearance {
    let light = reg_dword(w!(r"Software\Microsoft\Windows\CurrentVersion\Themes\Personalize"), w!("AppsUseLightTheme")) == Some(1);
    // AccentColor is stored as 0xAABBGGRR.
    let accent = reg_dword(w!(r"Software\Microsoft\Windows\DWM"), w!("AccentColor")).map(|v| [v as u8, (v >> 8) as u8, (v >> 16) as u8]);
    Appearance { light, accent }
}

fn reg_dword(key: PCWSTR, value: PCWSTR) -> Option<u32> {
    let mut v = 0u32;
    let mut size = 4u32;
    let r = unsafe {
        RegGetValueW(HKEY_CURRENT_USER, key, value, RRF_RT_REG_DWORD, None, Some(&mut v as *mut u32 as *mut _), Some(&mut size))
    };
    r.is_ok().then_some(v)
}

