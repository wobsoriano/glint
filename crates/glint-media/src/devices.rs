use std::fmt;

pub use crate::backend::devices::{cameras, displays, microphones};

/// A rectangle in physical (DPI-unscaled) pixels.
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub struct Rect {
    pub x: i32,
    pub y: i32,
    pub width: u32,
    pub height: u32,
}

/// A stable identifier for an audio endpoint or camera, valid across runs on the same machine.
#[derive(Clone, Debug, PartialEq, Eq, Hash)]
pub struct DeviceId(pub String);

impl fmt::Display for DeviceId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

#[derive(Clone, Debug, PartialEq)]
pub struct DisplayInfo {
    /// Index into [`displays()`]; the primary display is 0.
    pub index: usize,
    /// Monitor model from the display configuration, or `Display N` when the system has none.
    pub name: String,
    /// Position and size on the virtual desktop in physical pixels.
    pub rect: Rect,
    /// DPI scale factor (1.0 = 96 dpi on Windows, 1x on macOS).
    pub scale: f32,
    /// The platform's monitor handle: `HMONITOR` on Windows, `CGDirectDisplayID` on macOS.
    pub handle: isize,
    pub primary: bool,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct AudioDevice {
    pub id: DeviceId,
    pub name: String,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct CameraDevice {
    pub id: DeviceId,
    pub name: String,
}
