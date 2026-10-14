//! Studio export: write an MP4 from RGBA frames and PCM audio, or re-encode an existing one.

use crate::encoder::Mp4Sink;
pub use crate::encoder::{available_codecs, decodable_codecs, AudioParams, VideoCodec, VideoParams};
use crate::reader::{read_mp4_audio, VideoReader};
use anyhow::{bail, Context};
use image::RgbaImage;
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};

/// What a finished write produced.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct WriteSummary {
    pub frames: u64,
    pub duration_ms: u64,
    pub bytes: u64,
    /// The encoder ran on the GPU.
    pub hardware: bool,
}

/// Writes an H.264 or HEVC MP4 at a fixed size and frame rate, with an optional AAC track.
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

/// How to re-encode a video. Unset fields keep the source's values.
#[derive(Clone, Debug, Default)]
pub struct TranscodeOptions {
    /// Output size; odd values are rounded down to even.
    pub size: Option<(u32, u32)>,
    pub bitrate: Option<u32>,
    pub codec: Option<VideoCodec>,
    /// Keep only `start_ms..end_ms` of the source.
    pub trim: Option<(u64, u64)>,
}

/// Re-encodes `src` into `dst`, optionally resized, at a new bitrate, or trimmed. Carries the
/// source's audio track along. `progress` receives 0..=1; set `cancel` to stop early (the
/// partial file is removed).
pub fn transcode(
    src: &Path,
    dst: &Path,
    opts: &TranscodeOptions,
    cancel: &AtomicBool,
    mut progress: impl FnMut(f32),
) -> anyhow::Result<WriteSummary> {
    let mut reader = VideoReader::open(src)?;
    let (sw, sh) = reader.size();
    let (w, h) = opts.size.unwrap_or((sw, sh));
    let (w, h) = (w & !1, h & !1);
    let fps = reader.fps().round().max(1.0) as u32;
    let (start, end) = opts.trim.unwrap_or((0, reader.duration_ms()));
    if end <= start {
        bail!("trim range {start}..{end} is empty");
    }
    let video = VideoParams {
        width: w,
        height: h,
        fps,
        bitrate: opts.bitrate.unwrap_or_else(|| VideoParams::recording_bitrate(w, h, fps)),
        codec: opts.codec.unwrap_or(VideoCodec::H264),
    };
    let audio = read_mp4_audio(src)?.map(|a| trim_audio(a, start, end));
    let mut writer = VideoWriter::create(dst, video, audio.as_ref().map(|a| AudioParams { sample_rate: a.sample_rate, channels: a.channels }))?;
    if let Some(a) = &audio {
        writer.write_audio(&a.samples)?;
    }
    reader.seek(start)?;
    let span = (end - start) as f32;
    while let Some(frame) = reader.next_frame()? {
        if frame.t_ms >= end {
            break;
        }
        if cancel.load(Ordering::Relaxed) {
            drop(writer);
            let _ = std::fs::remove_file(dst);
            bail!("transcode cancelled");
        }
        let image = if (frame.image.width(), frame.image.height()) == (w, h) {
            frame.image
        } else {
            image::imageops::resize(&frame.image, w, h, image::imageops::FilterType::Triangle)
        };
        writer.write_frame(&image)?;
        progress(((frame.t_ms - start) as f32 / span).clamp(0.0, 1.0));
    }
    progress(1.0);
    writer.finish().with_context(|| format!("finish {}", dst.display()))
}

fn trim_audio(mut a: crate::wav::WavData, start_ms: u64, end_ms: u64) -> crate::wav::WavData {
    let per_ms = a.sample_rate as u64 * a.channels as u64 / 1000;
    let from = ((start_ms * per_ms) as usize).min(a.samples.len());
    let to = ((end_ms * per_ms) as usize).min(a.samples.len());
    a.samples = a.samples[from..to].to_vec();
    a
}
