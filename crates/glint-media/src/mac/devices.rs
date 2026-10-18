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

/// Built-in, external and Continuity cameras. The device types are spelled out because the
/// newer constants are missing from macOS 13's AVFoundation.
pub fn cameras() -> anyhow::Result<Vec<CameraDevice>> {
    use crate::devices::DeviceId;
    use objc2_av_foundation::{AVCaptureDeviceDiscoverySession, AVCaptureDevicePosition, AVMediaTypeVideo};
    use objc2_foundation::{NSArray, NSOperatingSystemVersion, NSProcessInfo, NSString};

    let macos_14 = NSProcessInfo::processInfo()
        .isOperatingSystemAtLeastVersion(NSOperatingSystemVersion { majorVersion: 14, minorVersion: 0, patchVersion: 0 });
    let types: &[&str] = if macos_14 {
        &["AVCaptureDeviceTypeBuiltInWideAngleCamera", "AVCaptureDeviceTypeExternal", "AVCaptureDeviceTypeContinuityCamera"]
    } else {
        &["AVCaptureDeviceTypeBuiltInWideAngleCamera", "AVCaptureDeviceTypeExternalUnknown"]
    };
    let types: Vec<_> = types.iter().map(|t| NSString::from_str(t)).collect();
    let session = unsafe {
        AVCaptureDeviceDiscoverySession::discoverySessionWithDeviceTypes_mediaType_position(
            &NSArray::from_retained_slice(&types),
            Some(super::av::constant(AVMediaTypeVideo)),
            AVCaptureDevicePosition::Unspecified,
        )
    };
    Ok(unsafe { session.devices() }
        .iter()
        .map(|d| CameraDevice { id: DeviceId(unsafe { d.uniqueID() }.to_string()), name: unsafe { d.localizedName() }.to_string() })
        .collect())
}
