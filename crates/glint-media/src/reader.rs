use crate::wav::WavData;
use image::RgbaImage;
use std::path::Path;

pub use crate::backend::reader::{AudioReader, VideoReader};

#[derive(Clone, Debug)]
pub struct VideoFrame {
    /// Presentation time in media milliseconds.
    pub t_ms: u64,
    pub image: RgbaImage,
}

impl VideoReader {
    /// `n` frames spaced evenly across the duration, the first at 0.
    pub fn thumbnails(&mut self, n: u32) -> anyhow::Result<Vec<VideoFrame>> {
        let mut out = Vec::with_capacity(n as usize);
        for i in 0..n {
            let t = self.duration_ms() * i as u64 / n as u64;
            self.seek(t)?;
            match self.next_frame()? {
                Some(f) => out.push(f),
                None => break,
            }
        }
        Ok(out)
    }
}

pub(crate) fn frame_at(ms: u64, sample_rate: u32) -> u64 {
    (ms * sample_rate as u64 + 500) / 1000
}

/// The whole first audio track of an MP4 as interleaved float PCM, or `None` when it has no
/// audio. Tests and short clips use it; Studio and the Trim preview stream chunks instead.
pub fn read_mp4_audio(path: &Path) -> anyhow::Result<Option<WavData>> {
    let Some(mut reader) = AudioReader::open(path)? else { return Ok(None) };
    let mut samples = Vec::new();
    while reader.next_chunk(&mut samples)? {}
    Ok(Some(WavData { sample_rate: reader.sample_rate(), channels: reader.channels(), samples }))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn frames_land_on_the_exact_sample_at_44100_hz() {
        assert_eq!(frame_at(60_000, 44_100), 2_646_000);
        assert_eq!(frame_at(61_000, 44_100) - frame_at(60_000, 44_100), 44_100);
        assert_eq!(frame_at(60_000, 48_000), 2_880_000);
    }
}
