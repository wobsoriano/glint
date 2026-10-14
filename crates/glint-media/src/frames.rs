//! Fast sequential decode for playback and export. Frames come out of the decoder (on the
//! GPU when it can) as NV12 and are converted to RGBA on demand, across all cores, by whoever
//! needs the pixels. [`crate::VideoReader`] instead converts on the decoding thread through
//! Media Foundation's single-threaded RGB32 path, which caps 4K decode near 50 fps.

use crate::d3d::D3dDevice;
use crate::mf::{self, pcwstr, unpack2, wide};
use anyhow::Context;
use image::RgbaImage;
use std::path::Path;
use std::sync::Mutex;
use windows::core::{Interface, GUID};
use windows::Win32::Graphics::Direct3D11::{
    ID3D11Texture2D, D3D11_CPU_ACCESS_READ, D3D11_MAPPED_SUBRESOURCE, D3D11_MAP_READ, D3D11_TEXTURE2D_DESC, D3D11_USAGE_STAGING,
};
use windows::Win32::Media::MediaFoundation::*;
use windows::Win32::System::Com::StructuredStorage::{PROPVARIANT, PROPVARIANT_0, PROPVARIANT_0_0, PROPVARIANT_0_0_0};
use windows::Win32::System::Variant::VT_I8;

/// Sequential and seekable NV12 frames from the video track of an MP4.
pub struct FrameReader {
    reader: IMFSourceReader,
    width: u32,
    height: u32,
    fps: f64,
    duration_ms: u64,
    matrix: Matrix,
    skip_until_ms: Option<u64>,
    d3d: Option<D3dDevice>,
    /// Readback target for hardware frames, by texture size.
    staging: Option<((u32, u32), ID3D11Texture2D)>,
}

/// One decoded frame in tightly packed NV12: `width * height` luma bytes, then interleaved
/// chroma for each pair of rows.
pub struct Nv12Frame {
    /// Presentation time in media milliseconds.
    pub t_ms: u64,
    pub width: u32,
    pub height: u32,
    data: Vec<u8>,
    matrix: Matrix,
}

/// Y'CbCr to RGB coefficients, 16.16 fixed point, for limited-range input.
#[derive(Clone, Copy, Debug, PartialEq)]
struct Matrix {
    rv: i32,
    gu: i32,
    gv: i32,
    bu: i32,
}

const BT709: Matrix = Matrix { rv: 117_489, gu: -13_975, gv: -34_925, bu: 138_438 };
const BT601: Matrix = Matrix { rv: 104_597, gu: -25_675, gv: -53_279, bu: 132_201 };
const Y_SCALE: i32 = 76_309;

impl FrameReader {
    /// Opens `path`, decoding on the GPU when a hardware decoder is available.
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        mf::init()?;
        match D3dDevice::new().and_then(|d3d| Self::open_with(path, Some(d3d))) {
            Ok(r) => Ok(r),
            Err(_) => Self::open_with(path, None),
        }
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

    /// Copies the planes out of the decoder's buffer without row padding. A hardware frame is
    /// read back through a staging texture kept for the next frame; Media Foundation's own
    /// `Lock2D` readback allocates one per call.
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

/// Copies an NV12 image whose luma plane spans `y_rows` rows of `pitch` bytes into `out`,
/// packed at `w` bytes per row.
fn copy_planes(src: &[u8], pitch: usize, y_rows: usize, w: usize, out: &mut [u8]) {
    let h = out.len() / w * 2 / 3;
    let (luma, chroma) = out.split_at_mut(w * h);
    for (y, row) in luma.chunks_exact_mut(w).enumerate() {
        row.copy_from_slice(&src[y * pitch..y * pitch + w]);
    }
    for (y, row) in chroma.chunks_exact_mut(w).enumerate() {
        let at = (y_rows + y) * pitch;
        row.copy_from_slice(&src[at..at + w]);
    }
}

/// Plane buffers of dropped frames. Reusing them avoids faulting in fresh pages for every
/// frame, which costs more than the copy at 4K.
static POOL: Mutex<Vec<Vec<u8>>> = Mutex::new(Vec::new());
const MAX_POOLED: usize = 8;

fn take_buffer(len: usize) -> Vec<u8> {
    let mut v = POOL.lock().unwrap_or_else(|e| e.into_inner()).pop().unwrap_or_default();
    v.resize(len, 0);
    v
}

impl Drop for Nv12Frame {
    fn drop(&mut self) {
        let mut pool = POOL.lock().unwrap_or_else(|e| e.into_inner());
        if pool.len() < MAX_POOLED {
            pool.push(std::mem::take(&mut self.data));
        }
    }
}

impl Nv12Frame {
    /// Converts to opaque RGBA, splitting rows across all cores. `buffer` is reused when it
    /// is large enough: a fresh 4K frame costs more in page faults than in conversion.
    pub fn to_rgba(&self, mut buffer: Vec<u8>) -> RgbaImage {
        let (w, h) = (self.width as usize, self.height as usize);
        buffer.resize(w * h * 4, 0);
        let mut rgba = buffer;
        nv12_to_rgba(&self.data, w, h, self.matrix, &mut rgba);
        RgbaImage::from_raw(self.width, self.height, rgba).expect("buffer matches size")
    }
}

fn nv12_to_rgba(data: &[u8], w: usize, h: usize, m: Matrix, out: &mut [u8]) {
    let (luma, chroma) = data.split_at(w * h);
    let threads = std::thread::available_parallelism().map_or(1, |n| n.get());
    let rows_per = h.div_ceil(threads).next_multiple_of(2);
    std::thread::scope(|s| {
        for (ci, chunk) in out.chunks_mut(rows_per * w * 4).enumerate() {
            s.spawn(move || {
                for (ri, row) in chunk.chunks_exact_mut(w * 4).enumerate() {
                    let y = ci * rows_per + ri;
                    let yrow = &luma[y * w..(y + 1) * w];
                    let uvrow = &chroma[(y / 2) * w..(y / 2 + 1) * w];
                    for ((px, ys), uv) in row.chunks_exact_mut(8).zip(yrow.chunks_exact(2)).zip(uvrow.chunks_exact(2)) {
                        let (u, v) = (uv[0] as i32 - 128, uv[1] as i32 - 128);
                        let (dr, dg, db) = (m.rv * v + 32_768, m.gu * u + m.gv * v + 32_768, m.bu * u + 32_768);
                        for (p, &yv) in px.chunks_exact_mut(4).zip(ys) {
                            let c = Y_SCALE * (yv as i32 - 16);
                            p[0] = ((c + dr) >> 16).clamp(0, 255) as u8;
                            p[1] = ((c + dg) >> 16).clamp(0, 255) as u8;
                            p[2] = ((c + db) >> 16).clamp(0, 255) as u8;
                            p[3] = 255;
                        }
                    }
                }
            });
        }
    });
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn nv12_converts_limited_range_bt709() {
        // 2x2 frames: a white and a black column, then flat pure red.
        let mut out = vec![0u8; 16];
        nv12_to_rgba(&[235, 16, 235, 16, 128, 128], 2, 2, BT709, &mut out);
        assert_eq!(out, [255, 255, 255, 255, 0, 0, 0, 255, 255, 255, 255, 255, 0, 0, 0, 255]);
        nv12_to_rgba(&[63, 63, 63, 63, 102, 240], 2, 2, BT709, &mut out);
        assert!(out[0] > 250 && out[1] < 8 && out[2] < 8, "{:?}", &out[..4]);
        assert_eq!(&out[..4], &out[12..]);
    }
}
