use super::space::{layout, RawDisplay};
use super::unsupported;
use crate::devices::{AudioDevice, CameraDevice, DisplayInfo};
use objc2_core_graphics::{
    CGDirectDisplayID, CGDisplayBounds, CGDisplayCopyDisplayMode, CGDisplayIsBuiltin, CGDisplayMode, CGError, CGGetActiveDisplayList,
    CGMainDisplayID,
};

/// All active displays in Glint pixels (see [`super::space`]), primary first.
pub fn displays() -> anyhow::Result<Vec<DisplayInfo>> {
    let mut ids = [0 as CGDirectDisplayID; 32];
    let mut count = 0u32;
    let err = unsafe { CGGetActiveDisplayList(ids.len() as u32, ids.as_mut_ptr(), &mut count) };
    if err != CGError::Success {
        anyhow::bail!("CGGetActiveDisplayList failed: {err:?}");
    }
    let main = CGMainDisplayID();
    let raw = ids[..count as usize]
        .iter()
        .map(|&id| {
            let b = CGDisplayBounds(id);
            let pixels = CGDisplayMode::pixel_width(CGDisplayCopyDisplayMode(id).as_deref());
            let scale = if b.size.width > 0.0 && pixels > 0 { (pixels as f64 / b.size.width) as f32 } else { 1.0 };
            RawDisplay {
                id,
                bounds: [b.origin.x, b.origin.y, b.size.width, b.size.height],
                scale,
                primary: id == main,
                builtin: CGDisplayIsBuiltin(id),
            }
        })
        .collect();
    Ok(layout(raw))
}

pub fn microphones() -> anyhow::Result<Vec<AudioDevice>> {
    unsupported("microphone enumeration")
}

pub fn cameras() -> anyhow::Result<Vec<CameraDevice>> {
    unsupported("camera enumeration")
}
