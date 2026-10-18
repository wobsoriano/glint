//! Platform layer. Each backend (`win`, `mac`) exposes the same modules and functions; the
//! cross-platform modules re-export them, so callers never name a backend.

#[cfg(windows)]
mod win;
#[cfg(windows)]
pub use win::*;

#[cfg(target_os = "macos")]
mod mac;
#[cfg(target_os = "macos")]
pub use mac::*;
