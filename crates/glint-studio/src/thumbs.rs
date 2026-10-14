//! Timeline thumbnails: frames spread across the recording, cached in `render/` as one strip.

use crate::session::Session;
use anyhow::Context;
use glint_media::FrameReader;
use image::RgbaImage;

/// `count` frames `height` pixels tall, thumbnail `i` taken at the middle of the `i`-th of
/// `count` equal slices of source time. Blocking; run it off the UI thread.
pub fn timeline_thumbnails(session: &Session, count: u32, height: u32) -> anyhow::Result<Vec<RgbaImage>> {
    let (sw, sh) = session.source_size;
    let width = ((sw as u64 * height as u64 / sh.max(1) as u64) as u32).max(1);
    let dir = session.render_dir();
    let path = dir.join(format!("thumbs-{count}x{height}.png"));
    if let Ok(strip) = image::open(&path) {
        let strip = strip.to_rgba8();
        if strip.dimensions() == (width * count, height) {
            return Ok((0..count).map(|i| image::imageops::crop_imm(&strip, i * width, 0, width, height).to_image()).collect());
        }
    }

    let mut reader = FrameReader::open(&session.tracks.screen)?;
    let mut thumbs = Vec::with_capacity(count as usize);
    for i in 0..count {
        let t = session.source_duration_ms * (i as f64 + 0.5) / count as f64;
        reader.seek(t as u64)?;
        let frame = match reader.next_frame()? {
            Some(f) => f.to_rgba(Vec::new()),
            None => thumbs.last().cloned().context("screen.mp4 has no frames")?,
        };
        thumbs.push(if frame.dimensions() == (width, height) { frame } else { image::imageops::thumbnail(&frame, width, height) });
    }

    let mut strip = RgbaImage::new(width * count, height);
    for (i, t) in thumbs.iter().enumerate() {
        image::imageops::replace(&mut strip, t, (i as u32 * width) as i64, 0);
    }
    std::fs::create_dir_all(&dir)?;
    let tmp = dir.join(format!("thumbs-{count}x{height}.{}.tmp.png", std::process::id()));
    strip.save(&tmp).with_context(|| format!("writing {}", tmp.display()))?;
    if std::fs::rename(&tmp, &path).is_err() {
        let _ = std::fs::remove_file(&tmp);
    }
    Ok(thumbs)
}
