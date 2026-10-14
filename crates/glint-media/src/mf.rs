//! Process and thread initialisation shared by every Media Foundation and WinRT caller.

use std::sync::OnceLock;
use windows::core::PCWSTR;
use windows::Win32::Foundation::RPC_E_CHANGED_MODE;
use windows::Win32::Media::MediaFoundation::{MFStartup, MFSTARTUP_NOSOCKET, MF_VERSION};
use windows::Win32::System::WinRT::{RoInitialize, RO_INIT_MULTITHREADED};

/// Initialises COM/WinRT for the calling thread and Media Foundation for the process.
///
/// Media Foundation stays started for the life of the process: tests and background jobs
/// overlap, and `MFShutdown` from one thread would tear down objects another still uses.
pub(crate) fn init() -> anyhow::Result<()> {
    static MF: OnceLock<Result<(), String>> = OnceLock::new();
    unsafe {
        if let Err(e) = RoInitialize(RO_INIT_MULTITHREADED) {
            if e.code() != RPC_E_CHANGED_MODE {
                return Err(anyhow::anyhow!("RoInitialize: {e}"));
            }
        }
    }
    MF.get_or_init(|| unsafe { MFStartup(MF_VERSION, MFSTARTUP_NOSOCKET) }.map_err(|e| e.to_string()))
        .clone()
        .map_err(|e| anyhow::anyhow!("MFStartup: {e}"))
}

/// Null-terminated UTF-16 copy of `s` for `PCWSTR` arguments. Keep the `Vec` alive while the
/// pointer is in use.
pub(crate) fn wide(s: &str) -> Vec<u16> {
    s.encode_utf16().chain(std::iter::once(0)).collect()
}

pub(crate) fn pcwstr(buf: &[u16]) -> PCWSTR {
    PCWSTR(buf.as_ptr())
}

/// Packs a `(numerator, denominator)` or `(width, height)` pair the way
/// `MF_MT_FRAME_SIZE` and `MF_MT_FRAME_RATE` store it.
pub(crate) fn pack2(hi: u32, lo: u32) -> u64 {
    ((hi as u64) << 32) | lo as u64
}

pub(crate) fn unpack2(v: u64) -> (u32, u32) {
    ((v >> 32) as u32, v as u32)
}
