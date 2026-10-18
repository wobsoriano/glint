use super::encoder::Mp4Sink;
use crate::writer::{AudioParams, VideoParams, WriteSummary};
use anyhow::bail;
use image::RgbaImage;
use std::path::Path;

pub struct VideoWriter {
    sink: Mp4Sink,
    path: std::path::PathBuf,
}

impl VideoWriter {
    pub fn create(path: &Path, video: VideoParams, audio: Option<AudioParams>) -> anyhow::Result<Self> {
        let sink = Mp4Sink::create(path, video, audio, None)?;
        Ok(Self { sink, path: path.to_path_buf() })
    }

    /// Appends the next frame. Its size must match the writer's `VideoParams`.
    pub fn write_frame(&mut self, frame: &RgbaImage) -> anyhow::Result<()> {
        let p = self.sink.params();
        if frame.width() != p.width || frame.height() != p.height {
            bail!("frame is {}x{}, writer expects {}x{}", frame.width(), frame.height(), p.width, p.height);
        }
        self.sink.write_rgba_sequential(frame.as_raw())
    }

    /// Appends interleaved f32 PCM at the writer's audio sample rate and channel count.
    pub fn write_audio(&mut self, interleaved: &[f32]) -> anyhow::Result<()> {
        self.sink.write_audio(interleaved)
    }

    pub fn frames(&self) -> u64 {
        self.sink.frames()
    }

    pub fn finish(self) -> anyhow::Result<WriteSummary> {
        let frames = self.sink.frames();
        let fps = self.sink.params().fps;
        let hardware = self.sink.hardware();
        self.sink.finish()?;
        let bytes = std::fs::metadata(&self.path)?.len();
        Ok(WriteSummary { frames, duration_ms: frames * 1000 / fps as u64, bytes, hardware })
    }
}
