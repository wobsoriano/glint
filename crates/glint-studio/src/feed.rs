use glint_media::frames::{FrameReader, Nv12Frame};
use image::RgbaImage;
use std::path::Path;
use std::sync::{Arc, Mutex, OnceLock};

pub(crate) struct SourceFrame {
    raw: Nv12Frame,
    rgba: OnceLock<RgbaImage>,
}

impl SourceFrame {
    pub fn rgba(&self) -> &RgbaImage {
        self.rgba.get_or_init(|| {
            let buffer = BUFFERS.lock().unwrap_or_else(|e| e.into_inner()).pop().unwrap_or_default();
            self.raw.to_rgba(buffer)
        })
    }
}

static BUFFERS: Mutex<Vec<Vec<u8>>> = Mutex::new(Vec::new());
const MAX_POOLED: usize = 8;

impl Drop for SourceFrame {
    fn drop(&mut self) {
        if let Some(image) = self.rgba.take() {
            let mut pool = BUFFERS.lock().unwrap_or_else(|e| e.into_inner());
            if pool.len() < MAX_POOLED {
                pool.push(image.into_raw());
            }
        }
    }
}

const SEEK_AHEAD_MS: f64 = 1_500.0;

pub(crate) struct FrameFeed {
    reader: FrameReader,
    half_frame_ms: f64,
    current: Option<(f64, Arc<SourceFrame>)>,
    next: Option<Nv12Frame>,
    ended: bool,
    origin_ms: f64,
}

impl FrameFeed {
    pub fn open(path: &Path) -> anyhow::Result<Self> {
        let reader = FrameReader::open(path)?;
        let half_frame_ms = 500.0 / reader.fps().max(1.0);
        Ok(Self { reader, half_frame_ms, current: None, next: None, ended: false, origin_ms: 0.0 })
    }

    pub fn frame_at(&mut self, source_ms: f64) -> anyhow::Result<Option<Arc<SourceFrame>>> {
        let head = self.current.as_ref().map_or(self.origin_ms, |c| c.0);
        let behind = self.current.as_ref().is_some_and(|c| source_ms + self.half_frame_ms < c.0);
        if behind || source_ms > head + SEEK_AHEAD_MS {
            self.reader.seek(source_ms.max(0.0) as u64)?;
            self.current = None;
            self.next = None;
            self.ended = false;
            self.origin_ms = source_ms;
        }
        loop {
            if self.next.is_none() && !self.ended {
                self.next = self.reader.next_frame()?;
                self.ended = self.next.is_none();
            }
            match self.next.take() {
                Some(raw) if self.current.is_none() || raw.t_ms as f64 <= source_ms + self.half_frame_ms => {
                    self.current = Some((raw.t_ms as f64, Arc::new(SourceFrame { raw, rgba: OnceLock::new() })));
                }
                other => {
                    self.next = other;
                    break;
                }
            }
        }
        Ok(self.current.as_ref().map(|c| c.1.clone()))
    }
}
