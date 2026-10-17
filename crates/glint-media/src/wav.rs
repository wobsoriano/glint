use anyhow::{bail, Context};
use std::fs::File;
use std::io::{BufWriter, Seek, SeekFrom, Write};
use std::path::Path;

#[derive(Clone, Debug, PartialEq)]
pub struct WavData {
    pub sample_rate: u32,
    pub channels: u16,
    /// Interleaved, `channels` values per frame, in `-1.0..=1.0`.
    pub samples: Vec<f32>,
}

impl WavData {
    /// Number of sample frames (samples per channel).
    pub fn frames(&self) -> usize {
        self.samples.len() / self.channels.max(1) as usize
    }

    pub fn duration_ms(&self) -> u64 {
        self.frames() as u64 * 1000 / self.sample_rate.max(1) as u64
    }
}

/// Reads a PCM (8/16/24/32-bit) or IEEE float (32/64-bit) WAV file, extensible headers included.
pub fn read_wav(path: &Path) -> anyhow::Result<WavData> {
    let bytes = std::fs::read(path).with_context(|| format!("read {}", path.display()))?;
    parse_wav(&bytes).with_context(|| format!("parse {}", path.display()))
}

fn parse_wav(bytes: &[u8]) -> anyhow::Result<WavData> {
    if bytes.len() < 12 || &bytes[0..4] != b"RIFF" || &bytes[8..12] != b"WAVE" {
        bail!("not a RIFF/WAVE file");
    }
    let mut fmt: Option<(u16, u16, u32, u16)> = None;
    let mut pos = 12;
    while pos + 8 <= bytes.len() {
        let id = &bytes[pos..pos + 4];
        let len = u32::from_le_bytes(bytes[pos + 4..pos + 8].try_into().unwrap()) as usize;
        let body = &bytes[pos + 8..(pos + 8 + len).min(bytes.len())];
        match id {
            b"fmt " => {
                if body.len() < 16 {
                    bail!("fmt chunk too short");
                }
                let mut tag = u16::from_le_bytes([body[0], body[1]]);
                let channels = u16::from_le_bytes([body[2], body[3]]);
                let rate = u32::from_le_bytes(body[4..8].try_into().unwrap());
                let bits = u16::from_le_bytes([body[14], body[15]]);
                if tag == 0xFFFE && body.len() >= 26 {
                    tag = u16::from_le_bytes([body[24], body[25]]);
                }
                fmt = Some((tag, channels, rate, bits));
            }
            b"data" => {
                let (tag, channels, rate, bits) = fmt.context("data chunk before fmt chunk")?;
                let samples = decode_samples(body, tag, bits)?;
                return Ok(WavData { sample_rate: rate, channels, samples });
            }
            _ => {}
        }
        pos += 8 + len + (len & 1);
    }
    bail!("no data chunk")
}

fn decode_samples(body: &[u8], tag: u16, bits: u16) -> anyhow::Result<Vec<f32>> {
    let out = match (tag, bits) {
        (1, 8) => body.iter().map(|b| (*b as f32 - 128.0) / 128.0).collect(),
        (1, 16) => body
            .chunks_exact(2)
            .map(|c| i16::from_le_bytes([c[0], c[1]]) as f32 / 32768.0)
            .collect(),
        (1, 24) => body
            .chunks_exact(3)
            .map(|c| (i32::from_le_bytes([0, c[0], c[1], c[2]]) >> 8) as f32 / 8_388_608.0)
            .collect(),
        (1, 32) => body
            .chunks_exact(4)
            .map(|c| i32::from_le_bytes(c.try_into().unwrap()) as f32 / 2_147_483_648.0)
            .collect(),
        (3, 32) => body
            .chunks_exact(4)
            .map(|c| f32::from_le_bytes(c.try_into().unwrap()))
            .collect(),
        (3, 64) => body
            .chunks_exact(8)
            .map(|c| f64::from_le_bytes(c.try_into().unwrap()) as f32)
            .collect(),
        _ => bail!("unsupported WAV format tag {tag} with {bits} bits"),
    };
    Ok(out)
}

pub(crate) struct WavWriter {
    file: BufWriter<File>,
    channels: u16,
    frames: u64,
}

impl WavWriter {
    pub fn create(path: &Path, sample_rate: u32, channels: u16) -> anyhow::Result<Self> {
        let mut file = BufWriter::new(File::create(path).with_context(|| format!("create {}", path.display()))?);
        let block_align = channels * 4;
        let mut header = Vec::with_capacity(46);
        header.extend_from_slice(b"RIFF");
        header.extend_from_slice(&0u32.to_le_bytes());
        header.extend_from_slice(b"WAVE");
        header.extend_from_slice(b"fmt ");
        header.extend_from_slice(&18u32.to_le_bytes());
        header.extend_from_slice(&3u16.to_le_bytes());
        header.extend_from_slice(&channels.to_le_bytes());
        header.extend_from_slice(&sample_rate.to_le_bytes());
        header.extend_from_slice(&(sample_rate * block_align as u32).to_le_bytes());
        header.extend_from_slice(&block_align.to_le_bytes());
        header.extend_from_slice(&32u16.to_le_bytes());
        header.extend_from_slice(&0u16.to_le_bytes());
        header.extend_from_slice(b"data");
        header.extend_from_slice(&0u32.to_le_bytes());
        file.write_all(&header)?;
        Ok(Self { file, channels, frames: 0 })
    }

    pub fn frames_written(&self) -> u64 {
        self.frames
    }

    pub fn write(&mut self, samples: &[f32]) -> anyhow::Result<()> {
        let mut bytes = Vec::with_capacity(samples.len() * 4);
        for s in samples {
            bytes.extend_from_slice(&s.to_le_bytes());
        }
        self.file.write_all(&bytes)?;
        self.frames += (samples.len() / self.channels as usize) as u64;
        Ok(())
    }

    pub fn write_silence(&mut self, frames: u64) -> anyhow::Result<()> {
        let zeros = vec![0u8; 4096];
        let mut left = frames * self.channels as u64 * 4;
        while left > 0 {
            let n = left.min(zeros.len() as u64) as usize;
            self.file.write_all(&zeros[..n])?;
            left -= n as u64;
        }
        self.frames += frames;
        Ok(())
    }

    /// Drops every frame after the first `frames`.
    pub fn truncate(&mut self, frames: u64) -> anyhow::Result<()> {
        if frames >= self.frames {
            return Ok(());
        }
        self.file.flush()?;
        let file = self.file.get_mut();
        file.set_len(46 + frames * self.channels as u64 * 4)?;
        file.seek(SeekFrom::End(0))?;
        self.frames = frames;
        Ok(())
    }

    pub fn finish(mut self) -> anyhow::Result<u64> {
        let data_len = self.frames * self.channels as u64 * 4;
        self.file.flush()?;
        let file = self.file.get_mut();
        file.seek(SeekFrom::Start(4))?;
        file.write_all(&((data_len + 38) as u32).to_le_bytes())?;
        file.seek(SeekFrom::Start(42))?;
        file.write_all(&(data_len as u32).to_le_bytes())?;
        file.flush()?;
        Ok(data_len + 46)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn float_round_trip_keeps_format_and_samples() {
        let dir = std::env::temp_dir().join("glint-media-wav-test");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("rt.wav");
        let mut w = WavWriter::create(&path, 48000, 2).unwrap();
        w.write(&[0.25, -0.5, 1.0, 0.0]).unwrap();
        w.write_silence(2).unwrap();
        let bytes = w.finish().unwrap();
        assert_eq!(bytes, 46 + 8 * 4);
        let data = read_wav(&path).unwrap();
        assert_eq!(
            data,
            WavData { sample_rate: 48000, channels: 2, samples: vec![0.25, -0.5, 1.0, 0.0, 0.0, 0.0, 0.0, 0.0] }
        );
        assert_eq!(data.frames(), 4);
        assert_eq!(std::fs::metadata(&path).unwrap().len(), bytes);
    }

    #[test]
    fn truncate_cuts_the_tail_and_the_header_follows() {
        let dir = std::env::temp_dir().join("glint-media-wav-test");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("cut.wav");
        let mut w = WavWriter::create(&path, 48000, 2).unwrap();
        w.write(&[0.25, -0.5, 1.0, 0.0, 0.5, 0.5]).unwrap();
        w.truncate(5).unwrap();
        w.truncate(2).unwrap();
        let bytes = w.finish().unwrap();
        assert_eq!(bytes, 46 + 4 * 4);
        let data = read_wav(&path).unwrap();
        assert_eq!(data, WavData { sample_rate: 48000, channels: 2, samples: vec![0.25, -0.5, 1.0, 0.0] });
        assert_eq!(std::fs::metadata(&path).unwrap().len(), bytes);
    }

    #[test]
    fn reads_pcm16_mono() {
        let mut bytes = Vec::new();
        bytes.extend_from_slice(b"RIFF");
        bytes.extend_from_slice(&40u32.to_le_bytes());
        bytes.extend_from_slice(b"WAVE");
        bytes.extend_from_slice(b"fmt ");
        bytes.extend_from_slice(&16u32.to_le_bytes());
        bytes.extend_from_slice(&1u16.to_le_bytes());
        bytes.extend_from_slice(&1u16.to_le_bytes());
        bytes.extend_from_slice(&8000u32.to_le_bytes());
        bytes.extend_from_slice(&16000u32.to_le_bytes());
        bytes.extend_from_slice(&2u16.to_le_bytes());
        bytes.extend_from_slice(&16u16.to_le_bytes());
        bytes.extend_from_slice(b"data");
        bytes.extend_from_slice(&4u32.to_le_bytes());
        bytes.extend_from_slice(&(-16384i16).to_le_bytes());
        bytes.extend_from_slice(&16384i16.to_le_bytes());
        let data = parse_wav(&bytes).unwrap();
        assert_eq!(data, WavData { sample_rate: 8000, channels: 1, samples: vec![-0.5, 0.5] });
        assert_eq!(data.duration_ms(), 0);
    }
}
