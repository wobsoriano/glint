//! MP4 muxing through the Media Foundation sink writer: H.264/HEVC video plus optional AAC.

use crate::d3d::D3dDevice;
use crate::mf::{self, pack2, pcwstr, wide};
use anyhow::{bail, Context};
use std::path::Path;
use windows::core::{Interface, GUID};
use windows::Win32::Graphics::Direct3D11::{ID3D11Texture2D, D3D11_BIND_RENDER_TARGET, D3D11_BIND_SHADER_RESOURCE};
use windows::Win32::Media::MediaFoundation::*;

/// Video codec written into the MP4.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum VideoCodec {
    H264,
    Hevc,
}

impl VideoCodec {
    fn subtype(self) -> GUID {
        match self {
            VideoCodec::H264 => MFVideoFormat_H264,
            VideoCodec::Hevc => MFVideoFormat_HEVC,
        }
    }

    fn profile(self) -> u32 {
        match self {
            VideoCodec::H264 => eAVEncH264VProfile_High.0 as u32,
            VideoCodec::Hevc => eAVEncH265VProfile_Main_420_8.0 as u32,
        }
    }
}

/// Video stream parameters for an MP4 writer.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct VideoParams {
    pub width: u32,
    pub height: u32,
    pub fps: u32,
    /// Average bitrate in bits per second.
    pub bitrate: u32,
    pub codec: VideoCodec,
}

impl VideoParams {
    /// A bitrate that keeps screen content crisp: 0.1 bits per pixel per frame, clamped to
    /// 4..80 Mbps (3840x2160 at 30 fps lands on 25 Mbps).
    pub fn recording_bitrate(width: u32, height: u32, fps: u32) -> u32 {
        let bits = width as u64 * height as u64 * fps as u64 / 10;
        bits.clamp(4_000_000, 80_000_000) as u32
    }

    pub(crate) fn frame_duration(&self) -> i64 {
        10_000_000 / self.fps as i64
    }
}

/// AAC audio stream parameters for an MP4 writer.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct AudioParams {
    pub sample_rate: u32,
    pub channels: u16,
}

/// One MP4 file being written. Video samples carry explicit timestamps; audio is sequential.
pub(crate) struct Mp4Sink {
    writer: IMFSinkWriter,
    video_stream: u32,
    audio_stream: Option<u32>,
    video: VideoParams,
    audio: Option<AudioParams>,
    audio_frames: u64,
    frames: u64,
    last_t: i64,
    hardware: bool,
}

impl Mp4Sink {
    /// Creates the file. With `d3d`, the sink writer may pick a hardware encoder and accept
    /// GPU textures; without it everything runs in software from CPU buffers.
    pub fn create(path: &Path, video: VideoParams, audio: Option<AudioParams>, d3d: Option<&D3dDevice>) -> anyhow::Result<Self> {
        mf::init()?;
        if video.width % 2 != 0 || video.height % 2 != 0 || video.width == 0 || video.height == 0 {
            bail!("video size must be even and non-zero, got {}x{}", video.width, video.height);
        }
        unsafe {
            let mut attrs = None;
            MFCreateAttributes(&mut attrs, 4).context("MFCreateAttributes")?;
            let attrs = attrs.unwrap();
            attrs.SetUINT32(&MF_READWRITE_ENABLE_HARDWARE_TRANSFORMS, 1)?;
            attrs.SetGUID(&MF_TRANSCODE_CONTAINERTYPE, &MFTranscodeContainerType_MPEG4)?;
            if let Some(d3d) = d3d {
                attrs.SetUnknown(&MF_SINK_WRITER_D3D_MANAGER, &d3d.manager)?;
            }
            let url = wide(&path.to_string_lossy());
            let writer = MFCreateSinkWriterFromURL(pcwstr(&url), None, &attrs)
                .with_context(|| format!("MFCreateSinkWriterFromURL {}", path.display()))?;

            let out = MFCreateMediaType()?;
            out.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
            out.SetGUID(&MF_MT_SUBTYPE, &video.codec.subtype())?;
            out.SetUINT32(&MF_MT_AVG_BITRATE, video.bitrate)?;
            out.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
            out.SetUINT64(&MF_MT_FRAME_SIZE, pack2(video.width, video.height))?;
            out.SetUINT64(&MF_MT_FRAME_RATE, pack2(video.fps, 1))?;
            out.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack2(1, 1))?;
            out.SetUINT32(&MF_MT_MPEG2_PROFILE, video.codec.profile())?;
            let video_stream = writer.AddStream(&out).with_context(|| format!("AddStream {:?}", video.codec))?;

            let input = bgra_type(video.width, video.height, video.fps)?;
            writer
                .SetInputMediaType(video_stream, &input, None)
                .with_context(|| format!("SetInputMediaType ARGB32 for {:?}", video.codec))?;

            let audio_stream = match audio {
                Some(a) => Some(add_aac_stream(&writer, a)?),
                None => None,
            };

            let hardware = encoder_is_hardware(&writer, video_stream);
            writer.BeginWriting().context("BeginWriting")?;
            Ok(Self {
                writer,
                video_stream,
                audio_stream,
                video,
                audio,
                audio_frames: 0,
                frames: 0,
                last_t: -1,
                hardware,
            })
        }
    }

    pub fn params(&self) -> VideoParams {
        self.video
    }

    /// True when the sink writer picked a hardware encoder MFT.
    pub fn hardware(&self) -> bool {
        self.hardware
    }

    pub fn frames(&self) -> u64 {
        self.frames
    }

    /// Writes an already-filled sample (from a [`SampleAllocator`]) at media time `t` (100 ns).
    pub fn write_sample(&mut self, sample: &IMFSample, t: i64) -> anyhow::Result<()> {
        unsafe {
            sample.SetSampleTime(t)?;
            sample.SetSampleDuration(self.video.frame_duration())?;
            self.writer.WriteSample(self.video_stream, sample).context("WriteSample video")?;
        }
        self.frames += 1;
        self.last_t = t;
        Ok(())
    }

    /// Writes a top-down BGRA frame (`width * height * 4` bytes) at media time `t` (100 ns).
    pub fn write_bgra(&mut self, bgra: &[u8], t: i64) -> anyhow::Result<()> {
        let expected = self.video.width as usize * self.video.height as usize * 4;
        if bgra.len() != expected {
            bail!("frame has {} bytes, expected {expected}", bgra.len());
        }
        unsafe {
            let buffer = MFCreateMemoryBuffer(expected as u32)?;
            let mut ptr = std::ptr::null_mut();
            buffer.Lock(&mut ptr, None, None)?;
            std::ptr::copy_nonoverlapping(bgra.as_ptr(), ptr, expected);
            buffer.Unlock()?;
            buffer.SetCurrentLength(expected as u32)?;
            let sample = MFCreateSample()?;
            sample.AddBuffer(&buffer)?;
            self.write_sample(&sample, t)
        }
    }

    /// Writes the next video frame from RGBA pixels at the constant frame rate.
    pub fn write_rgba_sequential(&mut self, rgba: &[u8]) -> anyhow::Result<()> {
        let mut bgra = rgba.to_vec();
        for px in bgra.chunks_exact_mut(4) {
            px.swap(0, 2);
        }
        let t = self.frames as i64 * self.video.frame_duration();
        self.write_bgra(&bgra, t)
    }

    /// Appends interleaved float samples to the AAC track, continuing from the last write.
    pub fn write_audio(&mut self, interleaved: &[f32]) -> anyhow::Result<()> {
        let (Some(stream), Some(audio)) = (self.audio_stream, self.audio) else {
            bail!("this MP4 has no audio track");
        };
        if interleaved.is_empty() {
            return Ok(());
        }
        let frames = interleaved.len() / audio.channels as usize;
        let mut pcm = Vec::with_capacity(interleaved.len() * 2);
        for s in interleaved {
            pcm.extend_from_slice(&((s.clamp(-1.0, 1.0) * 32767.0) as i16).to_le_bytes());
        }
        unsafe {
            let buffer = MFCreateMemoryBuffer(pcm.len() as u32)?;
            let mut ptr = std::ptr::null_mut();
            buffer.Lock(&mut ptr, None, None)?;
            std::ptr::copy_nonoverlapping(pcm.as_ptr(), ptr, pcm.len());
            buffer.Unlock()?;
            buffer.SetCurrentLength(pcm.len() as u32)?;
            let sample = MFCreateSample()?;
            sample.AddBuffer(&buffer)?;
            let rate = audio.sample_rate as i64;
            sample.SetSampleTime(self.audio_frames as i64 * 10_000_000 / rate)?;
            sample.SetSampleDuration(frames as i64 * 10_000_000 / rate)?;
            self.writer.WriteSample(stream, &sample).context("WriteSample audio")?;
        }
        self.audio_frames += frames as u64;
        Ok(())
    }

    /// Flushes the encoder and writes the MP4 index.
    pub fn finish(self) -> anyhow::Result<()> {
        unsafe { self.writer.Finalize().context("IMFSinkWriter::Finalize") }
    }
}

/// BGRA input type. `ARGB32` maps to `DXGI_FORMAT_B8G8R8A8_UNORM`, the format WGC frames come
/// in, so pooled textures can be copied from them (`RGB32` would make `B8G8R8X8` textures and
/// the copy would silently do nothing).
fn bgra_type(width: u32, height: u32, fps: u32) -> anyhow::Result<IMFMediaType> {
    unsafe {
        let t = MFCreateMediaType()?;
        t.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
        t.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_ARGB32)?;
        t.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
        t.SetUINT64(&MF_MT_FRAME_SIZE, pack2(width, height))?;
        t.SetUINT64(&MF_MT_FRAME_RATE, pack2(fps, 1))?;
        t.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack2(1, 1))?;
        t.SetUINT32(&MF_MT_ALL_SAMPLES_INDEPENDENT, 1)?;
        // Positive stride tells MF the rows run top-down, as every buffer we hand it does.
        t.SetUINT32(&MF_MT_DEFAULT_STRIDE, width * 4)?;
        Ok(t)
    }
}

unsafe fn add_aac_stream(writer: &IMFSinkWriter, audio: AudioParams) -> anyhow::Result<u32> {
    let out = MFCreateMediaType()?;
    out.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Audio)?;
    out.SetGUID(&MF_MT_SUBTYPE, &MFAudioFormat_AAC)?;
    out.SetUINT32(&MF_MT_AUDIO_BITS_PER_SAMPLE, 16)?;
    out.SetUINT32(&MF_MT_AUDIO_SAMPLES_PER_SECOND, audio.sample_rate)?;
    out.SetUINT32(&MF_MT_AUDIO_NUM_CHANNELS, audio.channels as u32)?;
    // 24000 bytes/s is the highest rate the Microsoft AAC encoder accepts for 1-2 channels.
    out.SetUINT32(&MF_MT_AUDIO_AVG_BYTES_PER_SECOND, 24000)?;
    let stream = writer.AddStream(&out).context("AddStream AAC")?;

    let input = MFCreateMediaType()?;
    input.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Audio)?;
    input.SetGUID(&MF_MT_SUBTYPE, &MFAudioFormat_PCM)?;
    input.SetUINT32(&MF_MT_AUDIO_BITS_PER_SAMPLE, 16)?;
    input.SetUINT32(&MF_MT_AUDIO_SAMPLES_PER_SECOND, audio.sample_rate)?;
    input.SetUINT32(&MF_MT_AUDIO_NUM_CHANNELS, audio.channels as u32)?;
    input.SetUINT32(&MF_MT_AUDIO_BLOCK_ALIGNMENT, audio.channels as u32 * 2)?;
    input.SetUINT32(&MF_MT_AUDIO_AVG_BYTES_PER_SECOND, audio.sample_rate * audio.channels as u32 * 2)?;
    input.SetUINT32(&MF_MT_ALL_SAMPLES_INDEPENDENT, 1)?;
    writer.SetInputMediaType(stream, &input, None).context("SetInputMediaType PCM for AAC")?;
    Ok(stream)
}

unsafe fn encoder_is_hardware(writer: &IMFSinkWriter, stream: u32) -> bool {
    let mut raw = std::ptr::null_mut();
    if writer.GetServiceForStream(stream, &GUID::zeroed(), &IMFTransform::IID, &mut raw).is_err() || raw.is_null() {
        return false;
    }
    let mft = IMFTransform::from_raw(raw);
    let Ok(attrs) = mft.GetAttributes() else { return false };
    let mut len = 0;
    let mut s = windows::core::PWSTR::null();
    attrs.GetAllocatedString(&MFT_ENUM_HARDWARE_URL_Attribute, &mut s, &mut len).is_ok() && len > 0
}

/// Codecs this machine can encode, hardware or software. H.264 is always present on Windows;
/// HEVC needs a hardware encoder or the HEVC Video Extensions.
pub fn available_codecs() -> Vec<VideoCodec> {
    codecs_with_transform(MFT_CATEGORY_VIDEO_ENCODER)
}

/// Codecs this machine can decode. A machine can encode HEVC on its GPU and still lack the
/// HEVC decoder package, so check this before reading an HEVC file.
pub fn decodable_codecs() -> Vec<VideoCodec> {
    codecs_with_transform(MFT_CATEGORY_VIDEO_DECODER)
}

fn codecs_with_transform(category: GUID) -> Vec<VideoCodec> {
    if mf::init().is_err() {
        return Vec::new();
    }
    [VideoCodec::H264, VideoCodec::Hevc]
        .into_iter()
        .filter(|codec| transform_count(category, codec.subtype()) > 0)
        .collect()
}

fn transform_count(category: GUID, subtype: GUID) -> u32 {
    let info = MFT_REGISTER_TYPE_INFO { guidMajorType: MFMediaType_Video, guidSubtype: subtype };
    let (input, output) = if category == MFT_CATEGORY_VIDEO_ENCODER { (None, Some(&info as *const _)) } else { (Some(&info as *const _), None) };
    let mut activates = std::ptr::null_mut();
    let mut count = 0u32;
    unsafe {
        if MFTEnumEx(
            category,
            MFT_ENUM_FLAG_HARDWARE | MFT_ENUM_FLAG_SYNCMFT | MFT_ENUM_FLAG_ASYNCMFT | MFT_ENUM_FLAG_SORTANDFILTER,
            input,
            output,
            &mut activates,
            &mut count,
        )
        .is_err()
        {
            return 0;
        }
        for i in 0..count as usize {
            drop((*activates.add(i)).take());
        }
        windows::Win32::System::Com::CoTaskMemFree(Some(activates as *const _));
    }
    count
}

/// A pool of GPU-backed samples the encoder consumes asynchronously. A sample's texture returns
/// to the pool when the encoder releases it, so a frame is never overwritten mid-encode.
pub(crate) struct SampleAllocator {
    allocator: IMFVideoSampleAllocatorEx,
}

impl SampleAllocator {
    pub fn new(d3d: &D3dDevice, width: u32, height: u32, fps: u32) -> anyhow::Result<Self> {
        unsafe {
            let mut raw = std::ptr::null_mut();
            MFCreateVideoSampleAllocatorEx(&IMFVideoSampleAllocatorEx::IID, &mut raw).context("MFCreateVideoSampleAllocatorEx")?;
            let allocator = IMFVideoSampleAllocatorEx::from_raw(raw);
            allocator.SetDirectXManager(&d3d.manager)?;
            let mut attrs = None;
            MFCreateAttributes(&mut attrs, 3)?;
            let attrs = attrs.unwrap();
            attrs.SetUINT32(&MF_SA_D3D11_BINDFLAGS, (D3D11_BIND_RENDER_TARGET | D3D11_BIND_SHADER_RESOURCE).0 as u32)?;
            attrs.SetUINT32(&MF_SA_BUFFERS_PER_SAMPLE, 1)?;
            let media_type = bgra_type(width, height, fps)?;
            allocator.InitializeSampleAllocatorEx(4, 12, &attrs, &media_type).context("InitializeSampleAllocatorEx")?;
            Ok(Self { allocator })
        }
    }

    /// A free sample and its texture, or `None` while the encoder holds every sample.
    pub fn take(&self) -> anyhow::Result<Option<(IMFSample, ID3D11Texture2D)>> {
        unsafe {
            let sample = match self.allocator.AllocateSample() {
                Ok(s) => s,
                Err(e) if e.code() == MF_E_SAMPLEALLOCATOR_EMPTY => return Ok(None),
                Err(e) => return Err(e).context("AllocateSample"),
            };
            let buffer = sample.GetBufferByIndex(0)?;
            // Pooled samples come back with a zero length, which the sink writer rejects.
            let len = buffer.cast::<IMF2DBuffer>()?.GetContiguousLength()?;
            buffer.SetCurrentLength(len)?;
            let dxgi: IMFDXGIBuffer = buffer.cast()?;
            let mut raw = std::ptr::null_mut();
            dxgi.GetResource(&ID3D11Texture2D::IID, &mut raw)?;
            Ok(Some((sample, ID3D11Texture2D::from_raw(raw))))
        }
    }
}
