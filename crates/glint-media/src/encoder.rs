//! MP4 muxing through the Media Foundation sink writer: H.264/HEVC video plus optional AAC.
//!
//! Every stream is written as BT.709 limited range and tagged as such in the output type, so
//! the encoder writes the colour space into the bitstream and decoders stop guessing it from
//! the frame size (Media Foundation assumes BT.601 below 720 rows). CPU frames are converted
//! to NV12 here with that matrix; GPU textures go to the hardware encoder as BGRA, which
//! converts with BT.709 at every size.

use crate::d3d::D3dDevice;
use crate::mf::{self, pack2, pcwstr, wide};
use anyhow::{bail, Context};
use std::path::Path;
use windows::core::{Interface, GUID};
use windows::Win32::Graphics::Direct3D11::{ID3D11Texture2D, D3D11_BIND_RENDER_TARGET, D3D11_BIND_SHADER_RESOURCE};
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Variant::{VARIANT, VARIANT_0, VARIANT_0_0, VARIANT_0_0_0, VT_UI4};

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

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct AudioParams {
    pub sample_rate: u32,
    pub channels: u16,
}

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
    nv12: Vec<u8>,
}

impl Mp4Sink {
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
            out.SetUINT32(&MF_MT_YUV_MATRIX, MFVideoTransferMatrix_BT709.0 as u32)?;
            out.SetUINT32(&MF_MT_VIDEO_NOMINAL_RANGE, MFNominalRange_16_235.0 as u32)?;
            out.SetUINT32(&MF_MT_VIDEO_PRIMARIES, MFVideoPrimaries_BT709.0 as u32)?;
            out.SetUINT32(&MF_MT_TRANSFER_FUNCTION, MFVideoTransFunc_709.0 as u32)?;
            let video_stream = writer.AddStream(&out).with_context(|| format!("AddStream {:?}", video.codec))?;

            let input = if d3d.is_some() {
                bgra_type(video.width, video.height, video.fps)?
            } else {
                nv12_type(video.width, video.height, video.fps)?
            };
            writer
                .SetInputMediaType(video_stream, &input, None)
                .with_context(|| format!("SetInputMediaType for {:?}", video.codec))?;

            let audio_stream = match audio {
                Some(a) => Some(add_aac_stream(&writer, a)?),
                None => None,
            };

            let encoder = encoder_attributes(&writer, video_stream);
            let hardware = encoder.as_ref().is_some_and(|a| string_attribute(a, &MFT_ENUM_HARDWARE_URL_Attribute).is_some());
            let vendor = encoder.as_ref().and_then(|a| string_attribute(a, &MFT_ENUM_HARDWARE_VENDOR_ID_Attribute));
            if video.codec == VideoCodec::H264 && video.fps > 30 && vendor.as_deref() == Some(AMD_VENDOR) {
                // AMD's H.264 MFT budgets bits as if every stream ran at 30 fps, so a 60 fps
                // file comes out at twice the bitrate (measured on an RX 6700 XT: 2.00x at 60,
                // 4.06x at 120, 1.00x at or below 30). Its HEVC MFT does not have the fault.
                let mean = (video.bitrate as u64 * 30 / video.fps as u64) as u32;
                codec_api(&writer, video_stream)
                    .context("AMD H.264 encoder has no ICodecAPI")?
                    .SetValue(&CODECAPI_AVEncCommonMeanBitRate, &variant_u32(mean))
                    .context("set mean bitrate")?;
            }
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
                nv12: Vec::new(),
            })
        }
    }

    pub fn params(&self) -> VideoParams {
        self.video
    }

    pub fn hardware(&self) -> bool {
        self.hardware
    }

    pub fn frames(&self) -> u64 {
        self.frames
    }

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

    pub fn write_bgra(&mut self, bgra: &[u8], t: i64) -> anyhow::Result<()> {
        self.write_rgb(bgra, 2, t)
    }

    pub fn write_rgba_sequential(&mut self, rgba: &[u8]) -> anyhow::Result<()> {
        let t = self.frames as i64 * self.video.frame_duration();
        self.write_rgb(rgba, 0, t)
    }

    fn write_rgb(&mut self, pixels: &[u8], r: usize, t: i64) -> anyhow::Result<()> {
        let (w, h) = (self.video.width as usize, self.video.height as usize);
        if pixels.len() != w * h * 4 {
            bail!("frame has {} bytes, expected {}", pixels.len(), w * h * 4);
        }
        let len = w * h * 3 / 2;
        let mut nv12 = std::mem::take(&mut self.nv12);
        nv12.resize(len, 0);
        rgb_to_nv12(pixels, r, w, h, &mut nv12);
        let written = unsafe {
            let buffer = MFCreateMemoryBuffer(len as u32)?;
            let mut ptr = std::ptr::null_mut();
            buffer.Lock(&mut ptr, None, None)?;
            std::ptr::copy_nonoverlapping(nv12.as_ptr(), ptr, len);
            buffer.Unlock()?;
            buffer.SetCurrentLength(len as u32)?;
            let sample = MFCreateSample()?;
            sample.AddBuffer(&buffer)?;
            self.write_sample(&sample, t)
        };
        self.nv12 = nv12;
        written
    }

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

    pub fn finish(self) -> anyhow::Result<()> {
        unsafe { self.writer.Finalize().context("IMFSinkWriter::Finalize") }
    }
}

const AMD_VENDOR: &str = "VEN_1002";

const Y_R: i32 = 11_966;
const Y_G: i32 = 40_254;
const Y_B: i32 = 4_064;
const CB_R: i32 = -6_596;
const CB_G: i32 = -22_189;
const CB_B: i32 = 28_784;
const CR_R: i32 = 28_784;
const CR_G: i32 = -26_145;
const CR_B: i32 = -2_639;

fn rgb_to_nv12(px: &[u8], r: usize, w: usize, h: usize, out: &mut [u8]) {
    let b = 2 - r;
    let (luma, chroma) = out.split_at_mut(w * h);
    let threads = std::thread::available_parallelism().map_or(1, |n| n.get());
    let rows_per = h.div_ceil(threads).next_multiple_of(2);
    std::thread::scope(|s| {
        for ((ci, luma), chroma) in luma.chunks_mut(rows_per * w).enumerate().zip(chroma.chunks_mut(rows_per / 2 * w)) {
            s.spawn(move || {
                let y0 = ci * rows_per;
                for (ri, yrow) in luma.chunks_exact_mut(w).enumerate() {
                    let src = &px[(y0 + ri) * w * 4..][..w * 4];
                    for (y, p) in yrow.iter_mut().zip(src.chunks_exact(4)) {
                        let v = Y_R * p[r] as i32 + Y_G * p[1] as i32 + Y_B * p[b] as i32;
                        *y = (16 + ((v + 32_768) >> 16)) as u8;
                    }
                }
                for (ri, uvrow) in chroma.chunks_exact_mut(w).enumerate() {
                    let top = &px[(y0 + ri * 2) * w * 4..][..w * 4];
                    let bot = &px[(y0 + ri * 2 + 1) * w * 4..][..w * 4];
                    for (x, uv) in uvrow.chunks_exact_mut(2).enumerate() {
                        let (a, c) = (&top[x * 8..x * 8 + 8], &bot[x * 8..x * 8 + 8]);
                        let sum = |i: usize| a[i] as i32 + a[i + 4] as i32 + c[i] as i32 + c[i + 4] as i32;
                        let (sr, sg, sb) = (sum(r), sum(1), sum(b));
                        uv[0] = (128 + ((CB_R * sr + CB_G * sg + CB_B * sb + (1 << 17)) >> 18)) as u8;
                        uv[1] = (128 + ((CR_R * sr + CR_G * sg + CR_B * sb + (1 << 17)) >> 18)) as u8;
                    }
                }
            });
        }
    });
}

/// BGRA input type. `ARGB32` maps to `DXGI_FORMAT_B8G8R8A8_UNORM`, the format WGC frames come
/// in, so pooled textures can be copied from them (`RGB32` would make `B8G8R8X8` textures and
/// the copy would silently do nothing).
fn bgra_type(width: u32, height: u32, fps: u32) -> anyhow::Result<IMFMediaType> {
    let t = uncompressed_type(MFVideoFormat_ARGB32, width, height, fps)?;
    // Positive stride tells MF the rows run top-down, as every buffer we hand it does.
    unsafe { t.SetUINT32(&MF_MT_DEFAULT_STRIDE, width * 4)? };
    Ok(t)
}

fn nv12_type(width: u32, height: u32, fps: u32) -> anyhow::Result<IMFMediaType> {
    let t = uncompressed_type(MFVideoFormat_NV12, width, height, fps)?;
    unsafe { t.SetUINT32(&MF_MT_DEFAULT_STRIDE, width)? };
    Ok(t)
}

fn uncompressed_type(subtype: GUID, width: u32, height: u32, fps: u32) -> anyhow::Result<IMFMediaType> {
    unsafe {
        let t = MFCreateMediaType()?;
        t.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
        t.SetGUID(&MF_MT_SUBTYPE, &subtype)?;
        t.SetUINT32(&MF_MT_INTERLACE_MODE, MFVideoInterlace_Progressive.0 as u32)?;
        t.SetUINT64(&MF_MT_FRAME_SIZE, pack2(width, height))?;
        t.SetUINT64(&MF_MT_FRAME_RATE, pack2(fps, 1))?;
        t.SetUINT64(&MF_MT_PIXEL_ASPECT_RATIO, pack2(1, 1))?;
        t.SetUINT32(&MF_MT_ALL_SAMPLES_INDEPENDENT, 1)?;
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

unsafe fn encoder_attributes(writer: &IMFSinkWriter, stream: u32) -> Option<IMFAttributes> {
    let mut raw = std::ptr::null_mut();
    writer.GetServiceForStream(stream, &GUID::zeroed(), &IMFTransform::IID, &mut raw).ok()?;
    if raw.is_null() {
        return None;
    }
    IMFTransform::from_raw(raw).GetAttributes().ok()
}

unsafe fn string_attribute(attrs: &IMFAttributes, key: &GUID) -> Option<String> {
    let mut len = 0;
    let mut s = windows::core::PWSTR::null();
    attrs.GetAllocatedString(key, &mut s, &mut len).ok()?;
    let value = s.to_string().ok();
    windows::Win32::System::Com::CoTaskMemFree(Some(s.as_ptr() as *const _));
    value.filter(|v| !v.is_empty())
}

unsafe fn codec_api(writer: &IMFSinkWriter, stream: u32) -> Option<ICodecAPI> {
    let mut raw = std::ptr::null_mut();
    writer.GetServiceForStream(stream, &GUID::zeroed(), &ICodecAPI::IID, &mut raw).ok()?;
    (!raw.is_null()).then(|| ICodecAPI::from_raw(raw))
}

fn variant_u32(v: u32) -> VARIANT {
    VARIANT {
        Anonymous: VARIANT_0 {
            Anonymous: std::mem::ManuallyDrop::new(VARIANT_0_0 {
                vt: VT_UI4,
                wReserved1: 0,
                wReserved2: 0,
                wReserved3: 0,
                Anonymous: VARIANT_0_0_0 { ulVal: v },
            }),
        },
    }
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

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn nv12_conversion_is_bt709_limited_range() {
        let rgba = [255, 255, 255, 255, 0, 0, 0, 255, 255, 0, 0, 255, 255, 0, 0, 255];
        let mut out = vec![0u8; 6];
        rgb_to_nv12(&rgba[..8].repeat(2), 0, 2, 2, &mut out);
        assert_eq!(out, [235, 16, 235, 16, 128, 128]);
        rgb_to_nv12(&rgba[8..].repeat(2), 0, 2, 2, &mut out);
        assert_eq!(out, [63, 63, 63, 63, 102, 240]);
        let bgra = [0, 0, 255, 255];
        rgb_to_nv12(&bgra.repeat(4), 2, 2, 2, &mut out);
        assert_eq!(out, [63, 63, 63, 63, 102, 240]);
    }
}
