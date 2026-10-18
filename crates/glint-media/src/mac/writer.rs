//! MP4 muxing through `AVAssetWriter`: H.264/HEVC video plus optional AAC, encoded by
//! VideoToolbox and tagged BT.709 like the Windows writer.
//!
//! `AVAssetWriter` stops taking media on one input while the other lags far behind, and
//! callers write in any order (often all audio first). So audio is queued here and appended
//! only up to a little past the video written so far; `finish` flushes the rest.

use super::av::{self, cf_key, constant, ns_error, number, settings};
use crate::writer::{AudioParams, VideoCodec, VideoParams, WriteSummary, AAC_BYTES_PER_SEC};
use anyhow::{bail, Context};
use block2::RcBlock;
use image::RgbaImage;
use objc2::rc::Retained;
use objc2::Message;
use objc2_av_foundation::{
    AVAssetWriter, AVAssetWriterInput, AVAssetWriterInputPixelBufferAdaptor, AVAssetWriterStatus, AVFileTypeMPEG4,
    AVMediaTypeAudio, AVMediaTypeVideo, AVVideoAverageBitRateKey, AVVideoCodecKey, AVVideoCodecTypeH264, AVVideoCodecTypeHEVC,
    AVVideoColorPrimariesKey, AVVideoColorPrimaries_ITU_R_709_2, AVVideoColorPropertiesKey, AVVideoCompressionPropertiesKey,
    AVVideoExpectedSourceFrameRateKey, AVVideoHeightKey, AVVideoProfileLevelH264HighAutoLevel, AVVideoProfileLevelKey,
    AVVideoTransferFunctionKey, AVVideoTransferFunction_ITU_R_709_2, AVVideoWidthKey, AVVideoYCbCrMatrixKey,
    AVVideoYCbCrMatrix_ITU_R_709_2,
};
use objc2_avf_audio::{AVEncoderBitRateKey, AVFormatIDKey, AVNumberOfChannelsKey, AVSampleRateKey};
use objc2_core_audio_types::{
    kAudioFormatFlagIsFloat, kAudioFormatFlagIsPacked, kAudioFormatLinearPCM, kAudioFormatMPEG4AAC, AudioStreamBasicDescription,
};
use objc2_core_foundation::{CFRetained, CFType};
use objc2_core_media::{
    kCMBlockBufferAssureMemoryNowFlag, kCMTimeZero, CMAudioFormatDescription, CMAudioFormatDescriptionCreate,
    CMAudioSampleBufferCreateReadyWithPacketDescriptions, CMBlockBuffer, CMSampleBuffer, CMTime,
};
use objc2_core_video::{
    kCVImageBufferColorPrimariesKey, kCVImageBufferColorPrimaries_ITU_R_709_2, kCVImageBufferTransferFunctionKey,
    kCVImageBufferTransferFunction_ITU_R_709_2, kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_709_2,
    kCVPixelBufferHeightKey, kCVPixelBufferPixelFormatTypeKey, kCVPixelBufferWidthKey, kCVPixelFormatType_32BGRA, CVAttachmentMode,
    CVPixelBuffer, CVPixelBufferGetBaseAddress, CVPixelBufferGetBytesPerRow, CVPixelBufferLockBaseAddress, CVPixelBufferLockFlags,
    CVPixelBufferPool, CVPixelBufferUnlockBaseAddress,
};
use objc2_foundation::{NSArray, NSString, NSURL};
use std::path::{Path, PathBuf};
use std::ptr::{self, NonNull};
use std::sync::mpsc;
use std::time::{Duration, Instant};

/// How long an input may stay busy before the writer gives up on it.
const STALL: Duration = Duration::from_secs(10);
/// How far the audio may run ahead of the video, in seconds.
const AUDIO_LEAD: f64 = 0.25;

pub struct VideoWriter {
    writer: Retained<AVAssetWriter>,
    video_input: Retained<AVAssetWriterInput>,
    adaptor: Retained<AVAssetWriterInputPixelBufferAdaptor>,
    audio: Option<AudioTrack>,
    params: VideoParams,
    frames: u64,
    path: PathBuf,
}

impl VideoWriter {
    pub fn create(path: &Path, video: VideoParams, audio: Option<AudioParams>) -> anyhow::Result<Self> {
        let (writer, video_input, adaptor) = add_video(path, video, false)?;
        let audio = audio.map(|a| AudioTrack::add(&writer, a)).transpose()?;
        start_writing(&writer, path)?;
        Ok(Self { writer, video_input, adaptor, audio, params: video, frames: 0, path: path.to_path_buf() })
    }

    /// Appends the next frame. Its size must match the writer's `VideoParams`.
    pub fn write_frame(&mut self, frame: &RgbaImage) -> anyhow::Result<()> {
        let p = self.params;
        if frame.width() != p.width || frame.height() != p.height {
            bail!("frame is {}x{}, writer expects {}x{}", frame.width(), frame.height(), p.width, p.height);
        }
        let audio_until = (self.frames + 1) as f64 / p.fps as f64 + AUDIO_LEAD;
        let started = Instant::now();
        loop {
            if let Some(a) = &mut self.audio {
                a.pump(audio_until)?;
            }
            if unsafe { self.video_input.isReadyForMoreMediaData() } {
                break;
            }
            self.check_stall(started, "video")?;
        }
        let pool = unsafe { self.adaptor.pixelBufferPool() }.context("pixel buffer pool is gone")?;
        let pixels = bgra_buffer(&pool, frame)?;
        let at = unsafe { CMTime::new(self.frames as i64, p.fps as i32) };
        if !unsafe { self.adaptor.appendPixelBuffer_withPresentationTime(&pixels, at) } {
            return Err(ns_error(unsafe { self.writer.error() }, "append frame"));
        }
        self.frames += 1;
        Ok(())
    }

    /// Appends interleaved f32 PCM at the writer's audio sample rate and channel count.
    pub fn write_audio(&mut self, interleaved: &[f32]) -> anyhow::Result<()> {
        let video_s = self.frames as f64 / self.params.fps as f64;
        let audio = self.audio.as_mut().context("writer has no audio track")?;
        audio.queue.extend_from_slice(interleaved);
        audio.pump(video_s + AUDIO_LEAD)
    }

    pub fn frames(&self) -> u64 {
        self.frames
    }

    pub fn finish(mut self) -> anyhow::Result<WriteSummary> {
        unsafe { self.video_input.markAsFinished() };
        if let Some(mut audio) = self.audio.take() {
            let (started, channels) = (Instant::now(), audio.params.channels.max(1) as usize);
            while audio.queue.len() >= channels {
                audio.pump(f64::INFINITY)?;
                if audio.queue.len() >= channels {
                    self.check_stall(started, "audio")?;
                }
            }
            unsafe { audio.input.markAsFinished() };
        }
        if self.frames > 0 {
            unsafe { self.writer.endSessionAtSourceTime(CMTime::new(self.frames as i64, self.params.fps as i32)) };
        }
        let bytes = finish_writing(&self.writer, &self.path)?;
        let frames = self.frames;
        Ok(WriteSummary { frames, duration_ms: frames * 1000 / self.params.fps as u64, bytes, hardware: true })
    }

    /// Fails when the writer has failed or an input stayed busy for too long; otherwise
    /// gives the encoder a moment to drain.
    fn check_stall(&self, started: Instant, input: &str) -> anyhow::Result<()> {
        if unsafe { self.writer.status() } == AVAssetWriterStatus::Failed {
            return Err(ns_error(unsafe { self.writer.error() }, "encode"));
        }
        if started.elapsed() > STALL {
            bail!("the {input} encoder stopped taking data");
        }
        std::thread::sleep(Duration::from_millis(1));
        Ok(())
    }
}

impl Drop for VideoWriter {
    fn drop(&mut self) {
        if unsafe { self.writer.status() } == AVAssetWriterStatus::Writing {
            unsafe { self.writer.cancelWriting() };
        }
    }
}

/// A writer for `path` with one video input for `video` and a BGRA pixel buffer adaptor on it.
fn add_video(
    path: &Path,
    video: VideoParams,
    realtime: bool,
) -> anyhow::Result<(Retained<AVAssetWriter>, Retained<AVAssetWriterInput>, Retained<AVAssetWriterInputPixelBufferAdaptor>)> {
    if video.width % 2 != 0 || video.height % 2 != 0 || video.width == 0 || video.height == 0 {
        bail!("video size must be even and non-zero, got {}x{}", video.width, video.height);
    }
    if video.fps == 0 {
        bail!("frame rate must be non-zero");
    }
    if path.exists() {
        std::fs::remove_file(path).with_context(|| format!("replace {}", path.display()))?;
    }
    let url = NSURL::from_file_path(path).with_context(|| format!("file URL for {}", path.display()))?;
    let writer = unsafe { AVAssetWriter::assetWriterWithURL_fileType_error(&url, constant(AVFileTypeMPEG4)) }
        .map_err(|e| ns_error(Some(e), &format!("create {}", path.display())))?;

    let input =
        unsafe { AVAssetWriterInput::assetWriterInputWithMediaType_outputSettings(constant(AVMediaTypeVideo), Some(&video_settings(video))) };
    unsafe { input.setExpectsMediaDataInRealTime(realtime) };
    if !unsafe { writer.canAddInput(&input) } {
        bail!("AVAssetWriter rejects {:?} at {}x{}", video.codec, video.width, video.height);
    }
    unsafe { writer.addInput(&input) };
    let (bgra, width, height) = (number(kCVPixelFormatType_32BGRA as i64), number(video.width as i64), number(video.height as i64));
    let source = settings(&[
        (cf_key(unsafe { kCVPixelBufferPixelFormatTypeKey }), &bgra),
        (cf_key(unsafe { kCVPixelBufferWidthKey }), &width),
        (cf_key(unsafe { kCVPixelBufferHeightKey }), &height),
    ]);
    let adaptor = unsafe {
        AVAssetWriterInputPixelBufferAdaptor::assetWriterInputPixelBufferAdaptorWithAssetWriterInput_sourcePixelBufferAttributes(
            &input,
            Some(&source),
        )
    };
    Ok((writer, input, adaptor))
}

fn start_writing(writer: &AVAssetWriter, path: &Path) -> anyhow::Result<()> {
    if !unsafe { writer.startWriting() } {
        return Err(ns_error(unsafe { writer.error() }, &format!("start writing {}", path.display())));
    }
    unsafe { writer.startSessionAtSourceTime(kCMTimeZero) };
    Ok(())
}

/// Waits for `writer` to close the file and returns its size.
fn finish_writing(writer: &AVAssetWriter, path: &Path) -> anyhow::Result<u64> {
    let (tx, rx) = mpsc::channel();
    let done = RcBlock::new(move || {
        let _ = tx.send(());
    });
    unsafe { writer.finishWritingWithCompletionHandler(&done) };
    rx.recv_timeout(Duration::from_secs(60)).context("AVAssetWriter did not finish")?;
    if unsafe { writer.status() } != AVAssetWriterStatus::Completed {
        return Err(ns_error(unsafe { writer.error() }, &format!("finish {}", path.display())));
    }
    Ok(std::fs::metadata(path)?.len())
}

/// A video-only MP4 fed live pixel buffers at the caller's timestamps: the screen and camera
/// tracks. The input runs in real time, so the encoder never holds capture back for long.
pub(super) struct PixelWriter {
    writer: Retained<AVAssetWriter>,
    input: Retained<AVAssetWriterInput>,
    adaptor: Retained<AVAssetWriterInputPixelBufferAdaptor>,
    frames: u64,
    path: PathBuf,
}

impl PixelWriter {
    pub fn create(path: &Path, video: VideoParams) -> anyhow::Result<Self> {
        let (writer, input, adaptor) = add_video(path, video, true)?;
        start_writing(&writer, path)?;
        Ok(Self { writer, input, adaptor, frames: 0, path: path.to_path_buf() })
    }

    /// Appends `pixels`, which must have the writer's size, shown from `at` on.
    pub fn append(&mut self, pixels: &CVPixelBuffer, at: CMTime) -> anyhow::Result<()> {
        let started = Instant::now();
        while !unsafe { self.input.isReadyForMoreMediaData() } {
            if unsafe { self.writer.status() } == AVAssetWriterStatus::Failed {
                return Err(ns_error(unsafe { self.writer.error() }, "encode"));
            }
            if started.elapsed() > STALL {
                bail!("the video encoder stopped taking frames");
            }
            std::thread::sleep(Duration::from_millis(1));
        }
        tag_bt709(pixels);
        if !unsafe { self.adaptor.appendPixelBuffer_withPresentationTime(pixels, at) } {
            return Err(ns_error(unsafe { self.writer.error() }, "append frame"));
        }
        self.frames += 1;
        Ok(())
    }

    pub fn frames(&self) -> u64 {
        self.frames
    }

    /// Ends the last frame at `end` and closes the file. Returns its size in bytes.
    pub fn finish(self, end: CMTime) -> anyhow::Result<u64> {
        unsafe { self.input.markAsFinished() };
        if self.frames > 0 {
            unsafe { self.writer.endSessionAtSourceTime(end) };
        }
        finish_writing(&self.writer, &self.path)
    }
}

impl Drop for PixelWriter {
    fn drop(&mut self) {
        if unsafe { self.writer.status() } == AVAssetWriterStatus::Writing {
            unsafe { self.writer.cancelWriting() };
        }
    }
}

impl VideoCodec {
    fn av_codec(self) -> &'static NSString {
        match self {
            VideoCodec::H264 => constant(unsafe { AVVideoCodecTypeH264 }),
            VideoCodec::Hevc => constant(unsafe { AVVideoCodecTypeHEVC }),
        }
    }

    /// The profile level key's value. VideoToolbox names HEVC Main `HEVC_Main_AutoLevel`
    /// (`kVTProfileLevel_HEVC_Main_AutoLevel`); AVFoundation has no constant for it.
    fn profile(self) -> Retained<NSString> {
        match self {
            VideoCodec::H264 => constant(unsafe { AVVideoProfileLevelH264HighAutoLevel }).retain(),
            VideoCodec::Hevc => NSString::from_str("HEVC_Main_AutoLevel"),
        }
    }
}

/// VideoToolbox overshoots an average bitrate by up to 2x on short clips of moving text
/// (measured at 4 Mbps, 720p30: 1.95x), so the data rate is also capped at 1.5x the average
/// over every second (`kVTCompressionPropertyKey_DataRateLimits`, bytes per seconds), which
/// lands that clip at 1.09x.
fn video_settings(v: VideoParams) -> Retained<av::Settings> {
    let (bitrate, fps, profile) = (number(v.bitrate as i64), number(v.fps as i64), v.codec.profile());
    let limits = NSArray::from_retained_slice(&[number(v.bitrate as i64 * 3 / 2 / 8), number(1)]);
    let compression = settings(&[
        (&NSString::from_str("DataRateLimits"), &limits),
        (constant(unsafe { AVVideoAverageBitRateKey }), &bitrate),
        (constant(unsafe { AVVideoExpectedSourceFrameRateKey }), &fps),
        (constant(unsafe { AVVideoProfileLevelKey }), &profile),
    ]);
    let colour = settings(&[
        (constant(unsafe { AVVideoColorPrimariesKey }), constant::<NSString>(unsafe { AVVideoColorPrimaries_ITU_R_709_2 })),
        (constant(unsafe { AVVideoTransferFunctionKey }), constant::<NSString>(unsafe { AVVideoTransferFunction_ITU_R_709_2 })),
        (constant(unsafe { AVVideoYCbCrMatrixKey }), constant::<NSString>(unsafe { AVVideoYCbCrMatrix_ITU_R_709_2 })),
    ]);
    let (width, height) = (number(v.width as i64), number(v.height as i64));
    settings(&[
        (constant(unsafe { AVVideoCodecKey }), v.codec.av_codec()),
        (constant(unsafe { AVVideoWidthKey }), &width),
        (constant(unsafe { AVVideoHeightKey }), &height),
        (constant(unsafe { AVVideoCompressionPropertiesKey }), &compression),
        (constant(unsafe { AVVideoColorPropertiesKey }), &colour),
    ])
}

/// A BGRA copy of `frame` from the adaptor's pool, tagged BT.709.
fn bgra_buffer(pool: &CVPixelBufferPool, frame: &RgbaImage) -> anyhow::Result<CFRetained<CVPixelBuffer>> {
    let mut out = ptr::null_mut();
    let status = unsafe { CVPixelBufferPool::create_pixel_buffer(None, pool, NonNull::from(&mut out)) };
    let pixels = NonNull::new(out).filter(|_| status == 0).with_context(|| format!("pixel buffer from pool: CVReturn {status}"))?;
    let pixels = unsafe { CFRetained::from_raw(pixels) };
    tag_bt709(&pixels);
    let w = frame.width() as usize;
    let status = unsafe { CVPixelBufferLockBaseAddress(&pixels, CVPixelBufferLockFlags(0)) };
    if status != 0 {
        bail!("lock pixel buffer: CVReturn {status}");
    }
    let pitch = CVPixelBufferGetBytesPerRow(&pixels);
    let base = CVPixelBufferGetBaseAddress(&pixels) as *mut u8;
    for (y, src) in frame.as_raw().chunks_exact(w * 4).enumerate() {
        let row = unsafe { std::slice::from_raw_parts_mut(base.add(y * pitch), w * 4) };
        for (d, s) in row.chunks_exact_mut(4).zip(src.chunks_exact(4)) {
            d[0] = s[2];
            d[1] = s[1];
            d[2] = s[0];
            d[3] = 255;
        }
    }
    unsafe { CVPixelBufferUnlockBaseAddress(&pixels, CVPixelBufferLockFlags(0)) };
    Ok(pixels)
}

/// Tags `pixels` BT.709, the matrix the stream is tagged with, so VideoToolbox converts
/// sRGB-like screen pixels to YCbCr without a color space conversion.
fn tag_bt709(pixels: &CVPixelBuffer) {
    for (key, value) in [
        (unsafe { kCVImageBufferColorPrimariesKey }, unsafe { kCVImageBufferColorPrimaries_ITU_R_709_2 }),
        (unsafe { kCVImageBufferTransferFunctionKey }, unsafe { kCVImageBufferTransferFunction_ITU_R_709_2 }),
        (unsafe { kCVImageBufferYCbCrMatrixKey }, unsafe { kCVImageBufferYCbCrMatrix_ITU_R_709_2 }),
    ] {
        let value: &CFType = value;
        unsafe { pixels.set_attachment(key, value, CVAttachmentMode::ShouldPropagate) };
    }
}

/// The AAC input and the PCM queued for it.
struct AudioTrack {
    input: Retained<AVAssetWriterInput>,
    format: CFRetained<CMAudioFormatDescription>,
    params: AudioParams,
    queue: Vec<f32>,
    written: u64,
}

impl AudioTrack {
    fn add(writer: &AVAssetWriter, a: AudioParams) -> anyhow::Result<Self> {
        let (aac, rate, channels, bitrate) = (
            number(kAudioFormatMPEG4AAC as i64),
            number(a.sample_rate as i64),
            number(a.channels as i64),
            number(AAC_BYTES_PER_SEC as i64 * 8),
        );
        let output = settings(&[
            (constant(unsafe { AVFormatIDKey }), &aac),
            (constant(unsafe { AVSampleRateKey }), &rate),
            (constant(unsafe { AVNumberOfChannelsKey }), &channels),
            (constant(unsafe { AVEncoderBitRateKey }), &bitrate),
        ]);
        let input = unsafe { AVAssetWriterInput::assetWriterInputWithMediaType_outputSettings(constant(AVMediaTypeAudio), Some(&output)) };
        if !unsafe { writer.canAddInput(&input) } {
            bail!("AVAssetWriter rejects AAC at {} Hz, {} channels", a.sample_rate, a.channels);
        }
        // A non-realtime writer holds the video input back while the audio lags, and the AAC
        // encoder keeps its last block until the input is finished, so a track whose audio
        // runs out first would never take another frame. A realtime audio input does not gate
        // the video.
        unsafe { input.setExpectsMediaDataInRealTime(true) };
        unsafe { writer.addInput(&input) };
        let bytes = 4 * a.channels as u32;
        let asbd = AudioStreamBasicDescription {
            mSampleRate: a.sample_rate as f64,
            mFormatID: kAudioFormatLinearPCM,
            mFormatFlags: kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked,
            mBytesPerPacket: bytes,
            mFramesPerPacket: 1,
            mBytesPerFrame: bytes,
            mChannelsPerFrame: a.channels as u32,
            mBitsPerChannel: 32,
            mReserved: 0,
        };
        let mut format = ptr::null();
        let status = unsafe {
            CMAudioFormatDescriptionCreate(None, NonNull::from(&asbd), 0, ptr::null(), 0, ptr::null(), None, NonNull::from(&mut format))
        };
        let format = NonNull::new(format.cast_mut()).filter(|_| status == 0).with_context(|| format!("PCM format: OSStatus {status}"))?;
        Ok(Self { input, format: unsafe { CFRetained::from_raw(format) }, params: a, queue: Vec::new(), written: 0 })
    }

    /// Appends queued audio up to `until_s` seconds, while the input takes it.
    fn pump(&mut self, until_s: f64) -> anyhow::Result<()> {
        let channels = self.params.channels.max(1) as usize;
        let rate = self.params.sample_rate as u64;
        let until = if until_s.is_finite() { (until_s * rate as f64) as u64 } else { u64::MAX };
        let mut taken = 0;
        while self.written < until && taken < self.queue.len() && unsafe { self.input.isReadyForMoreMediaData() } {
            let frames = ((self.queue.len() - taken) / channels).min((until - self.written).min(rate / 10) as usize);
            if frames == 0 {
                break;
            }
            let chunk = &self.queue[taken..taken + frames * channels];
            let sample = self.sample(chunk, frames)?;
            if !unsafe { self.input.appendSampleBuffer(&sample) } {
                bail!("append audio was refused");
            }
            taken += frames * channels;
            self.written += frames as u64;
        }
        self.queue.drain(..taken);
        Ok(())
    }

    fn sample(&self, pcm: &[f32], frames: usize) -> anyhow::Result<CFRetained<CMSampleBuffer>> {
        let len = std::mem::size_of_val(pcm);
        let mut block = ptr::null_mut();
        let status = unsafe {
            CMBlockBuffer::create_with_memory_block(
                None,
                ptr::null_mut(),
                len,
                None,
                ptr::null(),
                0,
                len,
                kCMBlockBufferAssureMemoryNowFlag,
                NonNull::from(&mut block),
            )
        };
        let block = NonNull::new(block).filter(|_| status == 0).with_context(|| format!("audio block: OSStatus {status}"))?;
        let block = unsafe { CFRetained::<CMBlockBuffer>::from_raw(block) };
        let status = unsafe { CMBlockBuffer::replace_data_bytes(NonNull::from(pcm).cast(), &block, 0, len) };
        if status != 0 {
            bail!("fill audio block: OSStatus {status}");
        }
        let at = unsafe { CMTime::new(self.written as i64, self.params.sample_rate as i32) };
        let mut sample = ptr::null_mut();
        let status = unsafe {
            CMAudioSampleBufferCreateReadyWithPacketDescriptions(
                None,
                &block,
                &self.format,
                frames as isize,
                at,
                ptr::null(),
                NonNull::from(&mut sample),
            )
        };
        let sample = NonNull::new(sample).filter(|_| status == 0).with_context(|| format!("audio sample: OSStatus {status}"))?;
        Ok(unsafe { CFRetained::from_raw(sample) })
    }
}
