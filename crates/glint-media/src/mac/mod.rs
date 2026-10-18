//! macOS backend. Display enumeration and the screen space work. Everything else is a stub:
//! constructors return an error, and the types hold `Infallible`, so their methods can never run.

use std::convert::Infallible;

pub(crate) mod devices;
pub mod space;

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

pub(crate) mod encoder {
    use crate::writer::VideoCodec;

    pub fn available_codecs() -> Vec<VideoCodec> {
        Vec::new()
    }

    pub fn decodable_codecs() -> Vec<VideoCodec> {
        Vec::new()
    }
}

pub(crate) mod frames {
    use super::*;
    use crate::frames::Nv12Frame;
    use std::path::Path;

    pub struct FrameReader(Infallible);

    impl FrameReader {
        pub fn open(_path: &Path) -> anyhow::Result<Self> {
            unsupported("video decoding")
        }

        pub fn size(&self) -> (u32, u32) {
            match self.0 {}
        }

        pub fn fps(&self) -> f64 {
            match self.0 {}
        }

        pub fn duration_ms(&self) -> u64 {
            match self.0 {}
        }

        pub fn hardware(&self) -> bool {
            match self.0 {}
        }

        pub fn seek(&mut self, _ms: u64) -> anyhow::Result<()> {
            match self.0 {}
        }

        pub fn next_frame(&mut self) -> anyhow::Result<Option<Nv12Frame>> {
            match self.0 {}
        }
    }
}

pub(crate) mod reader {
    use super::*;
    use crate::reader::VideoFrame;
    use std::path::Path;

    pub struct VideoReader(Infallible);

    impl VideoReader {
        pub fn open(_path: &Path) -> anyhow::Result<Self> {
            unsupported("video decoding")
        }

        pub fn size(&self) -> (u32, u32) {
            match self.0 {}
        }

        pub fn fps(&self) -> f64 {
            match self.0 {}
        }

        pub fn duration_ms(&self) -> u64 {
            match self.0 {}
        }

        pub fn seek(&mut self, _ms: u64) -> anyhow::Result<()> {
            match self.0 {}
        }

        pub fn next_frame(&mut self) -> anyhow::Result<Option<VideoFrame>> {
            match self.0 {}
        }
    }

    pub struct AudioReader(Infallible);

    impl AudioReader {
        pub fn open(_path: &Path) -> anyhow::Result<Option<Self>> {
            unsupported("audio decoding")
        }

        pub fn sample_rate(&self) -> u32 {
            match self.0 {}
        }

        pub fn channels(&self) -> u16 {
            match self.0 {}
        }

        pub fn seek(&mut self, _ms: u64) -> anyhow::Result<()> {
            match self.0 {}
        }

        pub fn next_chunk(&mut self, _out: &mut Vec<f32>) -> anyhow::Result<bool> {
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

pub(crate) mod writer {
    use super::*;
    use crate::writer::{AudioParams, VideoParams, WriteSummary};
    use image::RgbaImage;
    use std::path::Path;

    pub struct VideoWriter(Infallible);

    impl VideoWriter {
        pub fn create(_path: &Path, _video: VideoParams, _audio: Option<AudioParams>) -> anyhow::Result<Self> {
            unsupported("video encoding")
        }

        pub fn write_frame(&mut self, _frame: &RgbaImage) -> anyhow::Result<()> {
            match self.0 {}
        }

        pub fn write_audio(&mut self, _interleaved: &[f32]) -> anyhow::Result<()> {
            match self.0 {}
        }

        pub fn frames(&self) -> u64 {
            match self.0 {}
        }

        pub fn finish(self) -> anyhow::Result<WriteSummary> {
            match self.0 {}
        }
    }
}
