//! Studio decode: frames out of an MP4 as RGBA images, plus its audio as float PCM.

use crate::mf::{self, pcwstr, unpack2, wide};
use crate::wav::WavData;
use anyhow::{bail, Context};
use image::RgbaImage;
use std::path::Path;
use windows::core::{Interface, GUID};
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Com::StructuredStorage::{PROPVARIANT, PROPVARIANT_0, PROPVARIANT_0_0, PROPVARIANT_0_0_0};
use windows::Win32::System::Variant::VT_I8;

/// One decoded frame.
#[derive(Clone, Debug)]
pub struct VideoFrame {
    /// Presentation time in media milliseconds.
    pub t_ms: u64,
    pub image: RgbaImage,
}

/// Sequential and seekable access to the video track of an MP4.
pub struct VideoReader {
    reader: IMFSourceReader,
    width: u32,
    height: u32,
    fps: f64,
    duration_ms: u64,
    /// After a seek, frames before this time are decoded and dropped.
    skip_until_ms: Option<u64>,
}

impl VideoReader {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        mf::init()?;
        unsafe {
            let mut attrs = None;
            MFCreateAttributes(&mut attrs, 2)?;
            let attrs = attrs.unwrap();
            attrs.SetUINT32(&MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING, 1)?;
            let url = wide(&path.to_string_lossy());
            let reader = MFCreateSourceReaderFromURL(pcwstr(&url), &attrs).with_context(|| format!("open {}", path.display()))?;
            reader.SetStreamSelection(MF_SOURCE_READER_ALL_STREAMS.0 as u32, false)?;
            reader.SetStreamSelection(MF_SOURCE_READER_FIRST_VIDEO_STREAM.0 as u32, true)?;

            let native = reader.GetNativeMediaType(MF_SOURCE_READER_FIRST_VIDEO_STREAM.0 as u32, 0)?;
            let (width, height) = unpack2(native.GetUINT64(&MF_MT_FRAME_SIZE)?);
            let fps = match native.GetUINT64(&MF_MT_FRAME_RATE) {
                Ok(v) => {
                    let (n, d) = unpack2(v);
                    n as f64 / d.max(1) as f64
                }
                Err(_) => 30.0,
            };
            let out = MFCreateMediaType()?;
            out.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
            out.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_RGB32)?;
            reader
                .SetCurrentMediaType(MF_SOURCE_READER_FIRST_VIDEO_STREAM.0 as u32, None, &out)
                .context("SetCurrentMediaType RGB32")?;

            let duration = reader.GetPresentationAttribute(MF_SOURCE_READER_MEDIASOURCE.0 as u32, &MF_PD_DURATION)?;
            let duration_ms = (duration.Anonymous.Anonymous.Anonymous.uhVal / 10_000) as u64;
            Ok(Self { reader, width, height, fps, duration_ms, skip_until_ms: None })
        }
    }

    pub fn size(&self) -> (u32, u32) {
        (self.width, self.height)
    }

    pub fn fps(&self) -> f64 {
        self.fps
    }

    pub fn duration_ms(&self) -> u64 {
        self.duration_ms
    }

    /// Positions the reader so the next frame is the first one at or after `ms`.
    pub fn seek(&mut self, ms: u64) -> anyhow::Result<()> {
        let pos = PROPVARIANT {
            Anonymous: PROPVARIANT_0 {
                Anonymous: std::mem::ManuallyDrop::new(PROPVARIANT_0_0 {
                    vt: VT_I8,
                    wReserved1: 0,
                    wReserved2: 0,
                    wReserved3: 0,
                    Anonymous: PROPVARIANT_0_0_0 { hVal: ms as i64 * 10_000 },
                }),
            },
        };
        unsafe { self.reader.SetCurrentPosition(&GUID::zeroed(), &pos).context("SetCurrentPosition")? };
        self.skip_until_ms = Some(ms);
        Ok(())
    }

    /// The next frame, or `None` at the end of the stream.
    pub fn next_frame(&mut self) -> anyhow::Result<Option<VideoFrame>> {
        let half_frame_ms = (500.0 / self.fps.max(1.0)) as u64;
        loop {
            let Some((t_ms, sample)) = self.read_sample()? else { return Ok(None) };
            if let Some(target) = self.skip_until_ms {
                if t_ms + half_frame_ms < target {
                    continue;
                }
                self.skip_until_ms = None;
            }
            return Ok(Some(VideoFrame { t_ms, image: self.to_image(&sample)? }));
        }
    }

    /// `n` frames spaced evenly across the duration, the first at 0.
    pub fn thumbnails(&mut self, n: u32) -> anyhow::Result<Vec<VideoFrame>> {
        let mut out = Vec::with_capacity(n as usize);
        for i in 0..n {
            let t = self.duration_ms * i as u64 / n.max(1) as u64;
            self.seek(t)?;
            match self.next_frame()? {
                Some(f) => out.push(f),
                None => break,
            }
        }
        Ok(out)
    }

    fn read_sample(&mut self) -> anyhow::Result<Option<(u64, IMFSample)>> {
        loop {
            let mut flags = 0u32;
            let mut ts = 0i64;
            let mut sample = None;
            unsafe {
                self.reader
                    .ReadSample(MF_SOURCE_READER_FIRST_VIDEO_STREAM.0 as u32, 0, None, Some(&mut flags), Some(&mut ts), Some(&mut sample))
                    .context("ReadSample")?;
            }
            if flags & MF_SOURCE_READERF_ENDOFSTREAM.0 as u32 != 0 {
                return Ok(None);
            }
            if let Some(s) = sample {
                return Ok(Some(((ts.max(0) / 10_000) as u64, s)));
            }
        }
    }

    fn to_image(&self, sample: &IMFSample) -> anyhow::Result<RgbaImage> {
        let (w, h) = (self.width as usize, self.height as usize);
        let mut rgba = vec![0u8; w * h * 4];
        unsafe {
            let buffer = sample.ConvertToContiguousBuffer()?;
            let mut scan0 = std::ptr::null_mut();
            let mut pitch = 0i32;
            let locked_2d = buffer.cast::<IMF2DBuffer>().ok();
            let (base, pitch) = match &locked_2d {
                Some(b2d) => {
                    b2d.Lock2D(&mut scan0, &mut pitch)?;
                    (scan0, pitch as isize)
                }
                None => {
                    buffer.Lock(&mut scan0, None, None)?;
                    (scan0, (w * 4) as isize)
                }
            };
            for y in 0..h {
                let row = std::slice::from_raw_parts(base.offset(y as isize * pitch), w * 4);
                let dst = &mut rgba[y * w * 4..(y + 1) * w * 4];
                for (d, s) in dst.chunks_exact_mut(4).zip(row.chunks_exact(4)) {
                    d[0] = s[2];
                    d[1] = s[1];
                    d[2] = s[0];
                    d[3] = 255;
                }
            }
            match &locked_2d {
                Some(b2d) => b2d.Unlock2D()?,
                None => buffer.Unlock()?,
            }
        }
        RgbaImage::from_raw(self.width, self.height, rgba).context("frame buffer size")
    }
}

/// The first audio track of an MP4 as interleaved float PCM, or `None` when it has no audio.
pub fn read_mp4_audio(path: &Path) -> anyhow::Result<Option<WavData>> {
    mf::init()?;
    unsafe {
        let url = wide(&path.to_string_lossy());
        let reader = MFCreateSourceReaderFromURL(pcwstr(&url), None).with_context(|| format!("open {}", path.display()))?;
        reader.SetStreamSelection(MF_SOURCE_READER_ALL_STREAMS.0 as u32, false)?;
        let audio_stream = MF_SOURCE_READER_FIRST_AUDIO_STREAM.0 as u32;
        if reader.SetStreamSelection(audio_stream, true).is_err() {
            return Ok(None);
        }
        let out = MFCreateMediaType()?;
        out.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Audio)?;
        out.SetGUID(&MF_MT_SUBTYPE, &MFAudioFormat_Float)?;
        reader.SetCurrentMediaType(audio_stream, None, &out).context("SetCurrentMediaType float audio")?;
        let current = reader.GetCurrentMediaType(audio_stream)?;
        let sample_rate = current.GetUINT32(&MF_MT_AUDIO_SAMPLES_PER_SECOND)?;
        let channels = current.GetUINT32(&MF_MT_AUDIO_NUM_CHANNELS)? as u16;
        if current.GetUINT32(&MF_MT_AUDIO_BITS_PER_SAMPLE)? != 32 {
            bail!("audio decoder did not produce 32-bit float");
        }
        let mut samples = Vec::new();
        loop {
            let mut flags = 0u32;
            let mut sample = None;
            reader.ReadSample(audio_stream, 0, None, Some(&mut flags), None, Some(&mut sample))?;
            if flags & MF_SOURCE_READERF_ENDOFSTREAM.0 as u32 != 0 {
                break;
            }
            let Some(sample) = sample else { continue };
            let buffer = sample.ConvertToContiguousBuffer()?;
            let mut ptr = std::ptr::null_mut();
            let mut len = 0u32;
            buffer.Lock(&mut ptr, None, Some(&mut len))?;
            let bytes = std::slice::from_raw_parts(ptr, len as usize);
            samples.extend(bytes.chunks_exact(4).map(|c| f32::from_le_bytes([c[0], c[1], c[2], c[3]])));
            buffer.Unlock()?;
        }
        Ok(Some(WavData { sample_rate, channels, samples }))
    }
}
