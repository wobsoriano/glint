use image::RgbaImage;
use std::sync::Mutex;

pub use crate::backend::frames::FrameReader;

/// One decoded frame in tightly packed NV12: `width * height` luma bytes, then interleaved
/// chroma for each pair of rows.
pub struct Nv12Frame {
    /// Presentation time in media milliseconds.
    pub t_ms: u64,
    pub width: u32,
    pub height: u32,
    pub(crate) data: Vec<u8>,
    pub(crate) matrix: Matrix,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub(crate) struct Matrix {
    rv: i32,
    gu: i32,
    gv: i32,
    bu: i32,
}

pub(crate) const BT709: Matrix = Matrix { rv: 117_489, gu: -13_975, gv: -34_925, bu: 138_438 };
pub(crate) const BT601: Matrix = Matrix { rv: 104_597, gu: -25_675, gv: -53_279, bu: 132_201 };
const Y_SCALE: i32 = 76_309;

pub(crate) fn copy_planes(src: &[u8], pitch: usize, y_rows: usize, w: usize, out: &mut [u8]) {
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

static POOL: Mutex<Vec<Vec<u8>>> = Mutex::new(Vec::new());
const MAX_POOLED: usize = 8;

pub(crate) fn take_buffer(len: usize) -> Vec<u8> {
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
        nv12_to_rgba(&self.data, w, h, self.matrix, &mut buffer);
        RgbaImage::from_raw(self.width, self.height, buffer).expect("buffer matches size")
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
        let mut out = vec![0u8; 16];
        nv12_to_rgba(&[235, 16, 235, 16, 128, 128], 2, 2, BT709, &mut out);
        assert_eq!(out, [255, 255, 255, 255, 0, 0, 0, 255, 255, 255, 255, 255, 0, 0, 0, 255]);
        nv12_to_rgba(&[63, 63, 63, 63, 102, 240], 2, 2, BT709, &mut out);
        assert!(out[0] > 250 && out[1] < 8 && out[2] < 8, "{:?}", &out[..4]);
        assert_eq!(&out[..4], &out[12..]);
    }
}
