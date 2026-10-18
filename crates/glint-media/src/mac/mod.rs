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

pub(crate) mod audio {
    use super::*;
    use crate::devices::DeviceId;

    pub struct MicMonitor(Infallible);

    impl MicMonitor {
        pub fn start(_mic: Option<DeviceId>) -> anyhow::Result<Self> {
            unsupported("the microphone level meter")
        }

        pub fn level(&self) -> f32 {
            match self.0 {}
        }
    }
}

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

pub(crate) mod camera {
    use super::*;
    use crate::devices::DeviceId;
    use image::RgbaImage;

    pub struct CameraPreview(Infallible);

    impl CameraPreview {
        pub fn start(_id: &DeviceId, _on_frame: impl FnMut(RgbaImage) + Send + 'static) -> anyhow::Result<Self> {
            unsupported("the camera preview")
        }

        pub fn size(&self) -> (u32, u32) {
            match self.0 {}
        }
    }
}




pub(crate) mod recorder {
    use super::*;
    use crate::recorder::{RecordOptions, RecordSummary};
    use std::path::Path;
    use std::time::Duration;

    pub struct Recorder(Infallible);

    impl Recorder {
        pub fn start(_opts: RecordOptions) -> anyhow::Result<Self> {
            unsupported("screen recording")
        }

        pub fn pause(&self) {
            match self.0 {}
        }

        pub fn resume(&self) {
            match self.0 {}
        }

        pub fn is_paused(&self) -> bool {
            match self.0 {}
        }

        pub fn elapsed(&self) -> Duration {
            match self.0 {}
        }

        pub fn mic_level(&self) -> Option<f32> {
            match self.0 {}
        }

        pub fn warnings(&self) -> &[String] {
            match self.0 {}
        }

        pub fn session_dir(&self) -> &Path {
            match self.0 {}
        }

        pub fn is_live(&self) -> bool {
            match self.0 {}
        }

        pub fn stop(self) -> RecordSummary {
            match self.0 {}
        }

        pub fn cancel(self) {
            match self.0 {}
        }
    }
}
