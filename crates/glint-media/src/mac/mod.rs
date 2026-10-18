//! macOS backend. Display enumeration, the screen space, and MP4 encode and decode through
//! AVFoundation work. Everything else is a stub:
//! constructors return an error, and the types hold `Infallible`, so their methods can never run.

use std::convert::Infallible;

mod av;
pub(crate) mod devices;
pub(crate) mod encoder;
pub(crate) mod frames;
pub(crate) mod hooks;
mod keys;
pub(crate) mod reader;
pub mod sck;
pub mod space;
mod window;
pub(crate) mod writer;

fn unsupported<T>(what: &str) -> anyhow::Result<T> {
    anyhow::bail!("{what} is not supported on macOS yet")
}

pub(crate) mod audio;

pub(crate) mod audio_out {
    use super::*;

    pub struct AudioOutput(Infallible);

    #[derive(Clone)]
    pub struct AudioClock(Infallible);

    impl AudioOutput {
        pub const SAMPLE_RATE: u32 = 48_000;
        pub const CHANNELS: u16 = 2;

        pub fn start(_fill: impl FnMut(&mut [f32]) + Send + 'static) -> anyhow::Result<Self> {
            unsupported("audio playback")
        }

        pub fn clock(&self) -> AudioClock {
            match self.0 {}
        }
    }

    impl AudioClock {
        pub fn played_ms(&self) -> f64 {
            match self.0 {}
        }

        pub fn error(&self) -> Option<String> {
            match self.0 {}
        }
    }
}

pub(crate) mod camera;
pub(crate) mod recorder;
mod screen;
