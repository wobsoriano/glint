use super::av::{self, cf_key, constant, number, settings, TrackReader};
use crate::reader::{frame_at, VideoFrame};
use anyhow::{bail, Context};
use image::RgbaImage;
use objc2_av_foundation::{AVMediaTypeAudio, AVMediaTypeVideo};
use objc2_avf_audio::{AVFormatIDKey, AVLinearPCMBitDepthKey, AVLinearPCMIsBigEndianKey, AVLinearPCMIsFloatKey, AVLinearPCMIsNonInterleaved};
use objc2_core_audio_types::kAudioFormatLinearPCM;
use objc2_core_foundation::CFRetained;
use objc2_core_media::{CMAudioFormatDescriptionGetStreamBasicDescription, CMFormatDescription};
use objc2_core_video::{
    kCVPixelBufferPixelFormatTypeKey, kCVPixelFormatType_32BGRA, CVPixelBuffer,
    CVPixelBufferGetBaseAddress, CVPixelBufferGetBytesPerRow, CVPixelBufferLockBaseAddress, CVPixelBufferLockFlags, CVPixelBufferUnlockBaseAddress,
};
use std::path::Path;

/// The first video track decoded to `pixel_format`, with the seek semantics both video readers
/// share: after `seek(ms)` the next frame is the first at or after `ms`, within half a frame.
pub(super) struct VideoTrack {
    samples: TrackReader,
    pub width: u32,
    pub height: u32,
    pub fps: f64,
    pub duration_ms: u64,
    skip_until_ms: Option<u64>,
}

impl VideoTrack {
    pub fn open(path: &Path, pixel_format: u32) -> anyhow::Result<Self> {
        let asset = av::open_asset(path)?;
        let track = av::first_track(&asset, constant(unsafe { AVMediaTypeVideo }))?
            .with_context(|| format!("{} has no video track", path.display()))?;
        let size = unsafe { track.naturalSize() };
        let fps = match unsafe { track.nominalFrameRate() } {
            f if f > 0.0 => f as f64,
            _ => 30.0,
        };
        let duration_ms = av::time_ms(unsafe { asset.duration() });
        let format = number(pixel_format as i64);
        let output = settings(&[(cf_key(unsafe { kCVPixelBufferPixelFormatTypeKey }), &format)]);
        let samples = TrackReader::new(asset, track, output).with_context(|| format!("open {}", path.display()))?;
        Ok(Self {
            samples,
            width: size.width.round() as u32,
            height: size.height.round() as u32,
            fps,
            duration_ms,
            skip_until_ms: None,
        })
    }

    pub fn seek(&mut self, ms: u64) -> anyhow::Result<()> {
        self.samples.restart(ms)?;
        self.skip_until_ms = Some(ms);
        Ok(())
    }

    /// The next frame's time and pixels, or `None` at the end of the stream.
    pub fn next(&mut self) -> anyhow::Result<Option<(u64, CFRetained<CVPixelBuffer>)>> {
        let half_frame_ms = (500.0 / self.fps.max(1.0)) as u64;
        loop {
            let Some(sample) = self.samples.next()? else { return Ok(None) };
            let Some(pixels) = (unsafe { sample.image_buffer() }) else { continue };
            let t_ms = av::time_ms(unsafe { sample.presentation_time_stamp() });
            if let Some(target) = self.skip_until_ms {
                if t_ms + half_frame_ms < target {
                    continue;
                }
                self.skip_until_ms = None;
            }
            return Ok(Some((t_ms, pixels)));
        }
    }
}

/// Holds a pixel buffer's base address locked for reading.
pub(super) struct Locked<'a>(pub &'a CVPixelBuffer);

impl<'a> Locked<'a> {
    pub fn new(pixels: &'a CVPixelBuffer) -> anyhow::Result<Self> {
        let status = unsafe { CVPixelBufferLockBaseAddress(pixels, CVPixelBufferLockFlags::ReadOnly) };
        if status != 0 {
            bail!("lock pixel buffer: CVReturn {status}");
        }
        Ok(Self(pixels))
    }
}

impl Drop for Locked<'_> {
    fn drop(&mut self) {
        unsafe { CVPixelBufferUnlockBaseAddress(self.0, CVPixelBufferLockFlags::ReadOnly) };
    }
}

pub struct VideoReader {
    track: VideoTrack,
}

impl VideoReader {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        Ok(Self { track: VideoTrack::open(path, kCVPixelFormatType_32BGRA)? })
    }

    pub fn size(&self) -> (u32, u32) {
        (self.track.width, self.track.height)
    }

    pub fn fps(&self) -> f64 {
        self.track.fps
    }

    pub fn duration_ms(&self) -> u64 {
        self.track.duration_ms
    }

    /// Positions the reader so the next frame is the first one at or after `ms`.
    pub fn seek(&mut self, ms: u64) -> anyhow::Result<()> {
        self.track.seek(ms)
    }

    /// The next frame, or `None` at the end of the stream.
    pub fn next_frame(&mut self) -> anyhow::Result<Option<VideoFrame>> {
        let Some((t_ms, pixels)) = self.track.next()? else { return Ok(None) };
        Ok(Some(VideoFrame { t_ms, image: self.to_image(&pixels)? }))
    }

    fn to_image(&self, pixels: &CVPixelBuffer) -> anyhow::Result<RgbaImage> {
        let (w, h) = (self.track.width as usize, self.track.height as usize);
        let mut rgba = vec![0u8; w * h * 4];
        let locked = Locked::new(pixels)?;
        let pitch = CVPixelBufferGetBytesPerRow(locked.0);
        let base = CVPixelBufferGetBaseAddress(locked.0) as *const u8;
        if base.is_null() {
            bail!("decoded frame has no pixels");
        }
        for (y, dst) in rgba.chunks_exact_mut(w * 4).enumerate() {
            let row = unsafe { std::slice::from_raw_parts(base.add(y * pitch), w * 4) };
            for (d, s) in dst.chunks_exact_mut(4).zip(row.chunks_exact(4)) {
                d[0] = s[2];
                d[1] = s[1];
                d[2] = s[0];
                d[3] = 255;
            }
        }
        RgbaImage::from_raw(self.track.width, self.track.height, rgba).context("frame buffer size")
    }
}

/// Sequential and seekable access to the first audio track of an MP4, decoded to interleaved
/// float PCM one decoder chunk at a time.
pub struct AudioReader {
    samples: TrackReader,
    sample_rate: u32,
    channels: u16,
    skip_until: Option<u64>,
}

impl AudioReader {
    /// Opens the first audio track of `path`, or `None` when the file has no audio.
    pub fn open(path: &Path) -> anyhow::Result<Option<Self>> {
        let asset = av::open_asset(path)?;
        let Some(track) = av::first_track(&asset, constant(unsafe { AVMediaTypeAudio }))? else { return Ok(None) };
        let formats = unsafe { track.formatDescriptions() };
        let format = formats.firstObject().context("audio track has no format")?;
        let format = unsafe { &*(&*format as *const _ as *const CMFormatDescription) };
        let asbd = unsafe { CMAudioFormatDescriptionGetStreamBasicDescription(format) };
        let asbd = unsafe { asbd.as_ref() }.context("audio track has no stream description")?;
        let (sample_rate, channels) = (asbd.mSampleRate.round() as u32, asbd.mChannelsPerFrame as u16);
        let (pcm, bits, no, yes) = (number(kAudioFormatLinearPCM as i64), number(32), number(0), number(1));
        let output = settings(&[
            (constant(unsafe { AVFormatIDKey }), &pcm),
            (constant(unsafe { AVLinearPCMBitDepthKey }), &bits),
            (constant(unsafe { AVLinearPCMIsFloatKey }), &yes),
            (constant(unsafe { AVLinearPCMIsNonInterleaved }), &no),
            (constant(unsafe { AVLinearPCMIsBigEndianKey }), &no),
        ]);
        let samples = TrackReader::new(asset, track, output).with_context(|| format!("open audio of {}", path.display()))?;
        Ok(Some(Self { samples, sample_rate, channels, skip_until: None }))
    }

    pub fn sample_rate(&self) -> u32 {
        self.sample_rate
    }

    pub fn channels(&self) -> u16 {
        self.channels
    }

    /// Positions the reader so the next chunk starts on the frame at `ms`. Decoding resumes
    /// a few blocks earlier: AVFoundation's own pre-roll leaves the first blocks a hair off
    /// the continuous decode, and the dropped run-up absorbs that.
    pub fn seek(&mut self, ms: u64) -> anyhow::Result<()> {
        self.samples.restart(ms.saturating_sub(100))?;
        self.skip_until = Some(frame_at(ms, self.sample_rate));
        Ok(())
    }

    /// Appends the next decoded chunk to `out`. False once the track has ended.
    pub fn next_chunk(&mut self, out: &mut Vec<f32>) -> anyhow::Result<bool> {
        let channels = self.channels.max(1) as usize;
        loop {
            let Some(sample) = self.samples.next()? else { return Ok(false) };
            let Some(data) = (unsafe { sample.data_buffer() }) else { continue };
            let len = unsafe { data.data_length() } / 4;
            let before = out.len();
            out.resize(before + len, 0.0);
            let dst = std::ptr::NonNull::new(out[before..].as_mut_ptr().cast()).context("audio buffer")?;
            let status = unsafe { data.copy_data_bytes(0, len * 4, dst) };
            if status != 0 {
                out.truncate(before);
                bail!("copy decoded audio: OSStatus {status}");
            }
            if let Some(target) = self.skip_until {
                let at = av::time_frames(unsafe { sample.presentation_time_stamp() }, self.sample_rate);
                let skip = target.saturating_sub(at) as usize * channels;
                if before + skip >= out.len() {
                    out.truncate(before);
                    continue;
                }
                out.drain(before..before + skip);
                self.skip_until = None;
            }
            return Ok(true);
        }
    }
}
