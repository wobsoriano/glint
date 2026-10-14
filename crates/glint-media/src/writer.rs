//! Studio export: write an MP4 from RGBA frames and PCM audio, or re-encode an existing one.

use crate::encoder::Mp4Sink;
pub use crate::encoder::{available_codecs, decodable_codecs, AudioParams, VideoCodec, VideoParams};
use crate::frames::FrameReader;
use crate::reader::read_mp4_audio;
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
    /// Leave the source's audio track out.
    pub drop_audio: bool,
}

/// Re-encodes `src` into `dst`, optionally resized, at a new bitrate, or trimmed. Carries the
/// source's audio track along unless `drop_audio` is set. `progress` receives 0..=1; set `cancel` to stop early (the
/// partial file is removed).
pub fn transcode(
    src: &Path,
    dst: &Path,
    opts: &TranscodeOptions,
    cancel: &AtomicBool,
    mut progress: impl FnMut(f32),
) -> anyhow::Result<WriteSummary> {
    let mut reader = FrameReader::open(src)?;
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
    let audio = if opts.drop_audio { None } else { read_mp4_audio(src)?.map(|a| trim_audio(a, start, end)) };
    let mut writer = VideoWriter::create(dst, video, audio.as_ref().map(|a| AudioParams { sample_rate: a.sample_rate, channels: a.channels }))?;
    if let Some(a) = &audio {
        writer.write_audio(&a.samples)?;
    }
    reader.seek(start)?;
    let span = (end - start) as f32;
    let mut buffer = Vec::new();
    while let Some(frame) = reader.next_frame()? {
        if frame.t_ms >= end {
            break;
        }
        if cancel.load(Ordering::Relaxed) {
            drop(writer);
            let _ = std::fs::remove_file(dst);
            bail!("transcode cancelled");
        }
        let rgba = frame.to_rgba(std::mem::take(&mut buffer));
        if rgba.dimensions() == (w, h) {
            writer.write_frame(&rgba)?;
        } else if w <= rgba.width() && h <= rgba.height() {
            writer.write_frame(&shrink(&rgba, w, h))?;
        } else {
            writer.write_frame(&image::imageops::resize(&rgba, w, h, image::imageops::FilterType::Triangle))?;
        }
        buffer = rgba.into_raw();
        progress((frame.t_ms.saturating_sub(start) as f32 / span).clamp(0.0, 1.0));
    }
    progress(1.0);
    writer.finish().with_context(|| format!("finish {}", dst.display()))
}

/// Area-average downscale to `w` x `h`, at most the source size, split across all cores.
/// A 4K frame takes a few milliseconds where `image`'s single-threaded filters take over 100.
pub fn shrink(src: &RgbaImage, w: u32, h: u32) -> RgbaImage {
    let (sw, sh) = (src.width() as usize, src.height() as usize);
    let (w, h) = (w as usize, h as usize);
    let span = |i: usize, n: usize, of: usize| (i * of / n, ((i + 1) * of / n).max(i * of / n + 1).min(of));
    let cols: Vec<(usize, usize)> = (0..w).map(|x| span(x, w, sw)).collect();
    let mut out = vec![0u8; w * h * 4];
    let threads = std::thread::available_parallelism().map_or(1, |n| n.get());
    let rows_per = h.div_ceil(threads).max(1);
    let data = src.as_raw();
    std::thread::scope(|s| {
        for (ci, chunk) in out.chunks_mut(rows_per * w * 4).enumerate() {
            let cols = &cols;
            s.spawn(move || {
                let mut acc = vec![0u32; sw * 4];
                for (ri, row) in chunk.chunks_exact_mut(w * 4).enumerate() {
                    let (y0, y1) = span(ci * rows_per + ri, h, sh);
                    acc.fill(0);
                    for y in y0..y1 {
                        for (a, &v) in acc.iter_mut().zip(&data[y * sw * 4..(y + 1) * sw * 4]) {
                            *a += v as u32;
                        }
                    }
                    for (px, &(x0, x1)) in row.chunks_exact_mut(4).zip(cols) {
                        let n = ((x1 - x0) * (y1 - y0)) as u32;
                        for c in 0..4 {
                            let sum: u32 = (x0..x1).map(|x| acc[x * 4 + c]).sum();
                            px[c] = ((sum + n / 2) / n) as u8;
                        }
                    }
                }
            });
        }
    });
    RgbaImage::from_raw(w as u32, h as u32, out).expect("buffer matches size")
}

fn trim_audio(mut a: crate::wav::WavData, start_ms: u64, end_ms: u64) -> crate::wav::WavData {
    let per_ms = a.sample_rate as u64 * a.channels as u64 / 1000;
    let from = ((start_ms * per_ms) as usize).min(a.samples.len());
    let to = ((end_ms * per_ms) as usize).min(a.samples.len());
    a.samples = a.samples[from..to].to_vec();
    a
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn shrink_averages_each_output_area() {
        // 4x2 source: a black/white checker on the left half, solid red on the right.
        let src = RgbaImage::from_fn(4, 2, |x, y| match (x, y) {
            (0, 0) | (1, 1) => image::Rgba([0, 0, 0, 255]),
            (0, 1) | (1, 0) => image::Rgba([255, 255, 255, 255]),
            _ => image::Rgba([255, 0, 0, 255]),
        });
        let out = shrink(&src, 2, 1);
        assert_eq!(out.as_raw(), &vec![128, 128, 128, 255, 255, 0, 0, 255]);
        let odd = shrink(&RgbaImage::from_pixel(5, 3, image::Rgba([10, 20, 30, 255])), 2, 2);
        assert!(odd.pixels().all(|p| p.0 == [10, 20, 30, 255]));
    }
}
