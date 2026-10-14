//! Windows media engine: screen, camera, audio and input recording, MP4 encode and decode.
//!
//! Everything here is blocking and meant for background threads. Nothing depends on a UI
//! framework. See `docs/architecture.md` for the on-disk session contract.

pub mod clock;
pub mod input;
mod mf;
pub mod wav;

pub use clock::MediaClock;
pub use wav::{read_wav, WavData};
