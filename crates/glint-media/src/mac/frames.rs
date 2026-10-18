use super::reader::{Locked, VideoTrack};
use crate::frames::{take_buffer, Matrix, Nv12Frame, BT601, BT709};
use anyhow::bail;
use objc2_core_foundation::CFString;
use objc2_core_video::{
    kCVImageBufferYCbCrMatrixKey, kCVImageBufferYCbCrMatrix_ITU_R_601_4, kCVImageBufferYCbCrMatrix_ITU_R_709_2,
    kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange, CVPixelBuffer, CVPixelBufferGetBaseAddressOfPlane,
    CVPixelBufferGetBytesPerRowOfPlane,
};
use std::path::Path;

pub struct FrameReader {
    track: VideoTrack,
}

impl FrameReader {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        Ok(Self { track: VideoTrack::open(path, kCVPixelFormatType_420YpCbCr8BiPlanarVideoRange)? })
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

    /// VideoToolbox decodes on the media engine whenever the codec has one.
    pub fn hardware(&self) -> bool {
        true
    }

    /// Positions the reader so the next frame is the first one at or after `ms`.
    pub fn seek(&mut self, ms: u64) -> anyhow::Result<()> {
        self.track.seek(ms)
    }

    /// The next frame, or `None` at the end of the stream.
    pub fn next_frame(&mut self) -> anyhow::Result<Option<Nv12Frame>> {
        let Some((t_ms, pixels)) = self.track.next()? else { return Ok(None) };
        let (w, h) = (self.track.width as usize, self.track.height as usize);
        let mut data = take_buffer(w * h + w * h.div_ceil(2));
        let matrix = self.matrix(&pixels);
        let locked = Locked::new(&pixels)?;
        let (luma, chroma) = data.split_at_mut(w * h);
        for (plane, out) in [luma, chroma].into_iter().enumerate() {
            let base = CVPixelBufferGetBaseAddressOfPlane(locked.0, plane) as *const u8;
            if base.is_null() {
                bail!("decoded frame has no plane {plane}");
            }
            let pitch = CVPixelBufferGetBytesPerRowOfPlane(locked.0, plane);
            for (y, row) in out.chunks_exact_mut(w).enumerate() {
                row.copy_from_slice(unsafe { std::slice::from_raw_parts(base.add(y * pitch), w) });
            }
        }
        drop(locked);
        Ok(Some(Nv12Frame { t_ms, width: self.track.width, height: self.track.height, data, matrix }))
    }

    /// The buffer's tagged matrix. Untagged SD content is BT.601 and HD is BT.709, as Media
    /// Foundation assumes on Windows.
    fn matrix(&self, pixels: &CVPixelBuffer) -> Matrix {
        let tag = unsafe { pixels.attachment(kCVImageBufferYCbCrMatrixKey, std::ptr::null_mut()) };
        let tag = tag.as_deref().and_then(|t| t.downcast_ref::<CFString>());
        match tag {
            Some(t) if t == unsafe { kCVImageBufferYCbCrMatrix_ITU_R_601_4 } => BT601,
            Some(t) if t == unsafe { kCVImageBufferYCbCrMatrix_ITU_R_709_2 } => BT709,
            _ if self.track.height < 720 => BT601,
            _ => BT709,
        }
    }
}
