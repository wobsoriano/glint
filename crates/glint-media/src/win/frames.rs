use super::d3d::D3dDevice;
use super::mf::{self, pcwstr, unpack2, wide};
use crate::frames::{copy_planes, take_buffer, Matrix, Nv12Frame, BT601, BT709};
use anyhow::Context;
use std::path::Path;
use windows::core::{Interface, GUID};
use windows::Win32::Graphics::Direct3D11::{
    ID3D11Texture2D, D3D11_CPU_ACCESS_READ, D3D11_MAPPED_SUBRESOURCE, D3D11_MAP_READ, D3D11_TEXTURE2D_DESC, D3D11_USAGE_STAGING,
};
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Com::StructuredStorage::{PROPVARIANT, PROPVARIANT_0, PROPVARIANT_0_0, PROPVARIANT_0_0_0};
use windows::Win32::System::Variant::VT_I8;

pub struct FrameReader {
    reader: IMFSourceReader,
    width: u32,
    height: u32,
    fps: f64,
    duration_ms: u64,
    matrix: Matrix,
    skip_until_ms: Option<u64>,
    d3d: Option<D3dDevice>,
    staging: Option<((u32, u32), ID3D11Texture2D)>,
}

impl FrameReader {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        mf::init()?;
        D3dDevice::new().and_then(|d3d| Self::open_with(path, Some(d3d))).or_else(|_| Self::open_with(path, None))
    }

    fn open_with(path: &Path, d3d: Option<D3dDevice>) -> anyhow::Result<Self> {
        unsafe {
            let mut attrs = None;
            MFCreateAttributes(&mut attrs, 2)?;
            let attrs = attrs.context("MFCreateAttributes")?;
            if let Some(d3d) = &d3d {
                attrs.SetUnknown(&MF_SOURCE_READER_D3D_MANAGER, &d3d.manager)?;
                attrs.SetUINT32(&MF_READWRITE_ENABLE_HARDWARE_TRANSFORMS, 1)?;
            }
            let url = wide(&path.to_string_lossy());
            let reader = MFCreateSourceReaderFromURL(pcwstr(&url), &attrs).with_context(|| format!("open {}", path.display()))?;
            let stream = MF_SOURCE_READER_FIRST_VIDEO_STREAM.0 as u32;
            reader.SetStreamSelection(MF_SOURCE_READER_ALL_STREAMS.0 as u32, false)?;
            reader.SetStreamSelection(stream, true)?;
            let native = reader.GetNativeMediaType(stream, 0)?;
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
            out.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_NV12)?;
            reader.SetCurrentMediaType(stream, None, &out).context("SetCurrentMediaType NV12")?;
            let current = reader.GetCurrentMediaType(stream)?;
            // Media Foundation treats unflagged SD content as BT.601 and HD as BT.709.
            let matrix = match current.GetUINT32(&MF_MT_YUV_MATRIX) {
                Ok(m) if m == MFVideoTransferMatrix_BT601.0 as u32 => BT601,
                Ok(m) if m == MFVideoTransferMatrix_BT709.0 as u32 => BT709,
                _ if height < 720 => BT601,
                _ => BT709,
            };
            let duration = reader.GetPresentationAttribute(MF_SOURCE_READER_MEDIASOURCE.0 as u32, &MF_PD_DURATION)?;
            let duration_ms = duration.Anonymous.Anonymous.Anonymous.uhVal / 10_000;
            Ok(Self { reader, width, height, fps, duration_ms, matrix, skip_until_ms: None, d3d, staging: None })
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

    /// The reader has a GPU device, so Media Foundation decodes on the GPU when the codec
    /// has a hardware decoder.
    pub fn hardware(&self) -> bool {
        self.d3d.is_some()
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
    pub fn next_frame(&mut self) -> anyhow::Result<Option<Nv12Frame>> {
        let half_frame_ms = (500.0 / self.fps.max(1.0)) as u64;
        loop {
            let Some((t_ms, sample)) = self.read_sample()? else { return Ok(None) };
            if let Some(target) = self.skip_until_ms {
                if t_ms + half_frame_ms < target {
                    continue;
                }
                self.skip_until_ms = None;
            }
            return Ok(Some(self.copy_out(t_ms, &sample)?));
        }
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

    fn copy_out(&mut self, t_ms: u64, sample: &IMFSample) -> anyhow::Result<Nv12Frame> {
        let (w, h) = (self.width as usize, self.height as usize);
        let mut data = take_buffer(w * h + w * h.div_ceil(2));
        unsafe {
            let buffer = sample.GetBufferByIndex(0)?;
            if let (Ok(dxgi), Some(d3d)) = (buffer.cast::<IMFDXGIBuffer>(), self.d3d.as_ref()) {
                let mut raw = std::ptr::null_mut();
                dxgi.GetResource(&ID3D11Texture2D::IID, &mut raw)?;
                let texture = ID3D11Texture2D::from_raw(raw);
                let subresource = dxgi.GetSubresourceIndex()?;
                let mut desc = D3D11_TEXTURE2D_DESC::default();
                texture.GetDesc(&mut desc);
                let staging = match &self.staging {
                    Some((size, tex)) if *size == (desc.Width, desc.Height) => tex.clone(),
                    _ => {
                        let staging_desc = D3D11_TEXTURE2D_DESC {
                            MipLevels: 1,
                            ArraySize: 1,
                            Usage: D3D11_USAGE_STAGING,
                            BindFlags: 0,
                            CPUAccessFlags: D3D11_CPU_ACCESS_READ.0 as u32,
                            MiscFlags: 0,
                            ..desc
                        };
                        let mut tex = None;
                        d3d.device.CreateTexture2D(&staging_desc, None, Some(&mut tex)).context("CreateTexture2D NV12 staging")?;
                        let tex = tex.context("CreateTexture2D returned nothing")?;
                        self.staging = Some(((desc.Width, desc.Height), tex.clone()));
                        tex
                    }
                };
                d3d.context.CopySubresourceRegion(&staging, 0, 0, 0, 0, &texture, subresource, None);
                let mut mapped = D3D11_MAPPED_SUBRESOURCE::default();
                d3d.context.Map(&staging, 0, D3D11_MAP_READ, 0, Some(&mut mapped)).context("Map NV12 staging")?;
                let pitch = mapped.RowPitch as usize;
                let len = pitch * desc.Height as usize * 3 / 2;
                copy_planes(std::slice::from_raw_parts(mapped.pData as *const u8, len), pitch, desc.Height as usize, w, &mut data);
                d3d.context.Unmap(&staging, 0);
            } else {
                let b2d = buffer.cast::<IMF2DBuffer>().ok();
                let mut scan0 = std::ptr::null_mut();
                let (pitch, len) = match &b2d {
                    Some(b) => {
                        let mut pitch = 0i32;
                        b.Lock2D(&mut scan0, &mut pitch)?;
                        (pitch as usize, b.GetContiguousLength()? as usize)
                    }
                    None => {
                        let mut len = 0u32;
                        buffer.Lock(&mut scan0, None, Some(&mut len))?;
                        (w, len as usize)
                    }
                };
                // Decoders pad the luma plane to whole macroblocks; the chroma plane follows
                // the padded height, which the buffer length reveals.
                let y_rows = (len / pitch * 2 / 3).max(h);
                copy_planes(std::slice::from_raw_parts(scan0 as *const u8, len), pitch, y_rows, w, &mut data);
                match &b2d {
                    Some(b) => b.Unlock2D()?,
                    None => buffer.Unlock()?,
                }
            }
        }
        Ok(Nv12Frame { t_ms, width: self.width, height: self.height, data, matrix: self.matrix })
    }
}
