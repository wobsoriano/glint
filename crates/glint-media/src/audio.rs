use crate::clock::MediaClock;
use crate::wav::WavWriter;
use std::path::Path;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::Arc;

pub use crate::backend::audio::MicMonitor;

/// Every recorded audio track is resampled to this rate.
pub const SAMPLE_RATE: u32 = 48_000;

#[derive(Default)]
pub struct LevelMeter {
    rms: AtomicU32,
}

impl LevelMeter {
    /// RMS level of the last buffer, 0.0 (silence) to about 1.0 (full scale).
    pub fn rms(&self) -> f32 {
        f32::from_bits(self.rms.load(Ordering::Relaxed))
    }

    pub(crate) fn update(&self, samples: &[f32]) {
        if samples.is_empty() {
            return;
        }
        let rms = (samples.iter().map(|s| s * s).sum::<f32>() / samples.len() as f32).sqrt();
        self.rms.store(rms.to_bits(), Ordering::Relaxed);
    }
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct AudioStats {
    pub frames: u64,
    pub bytes: u64,
}

/// Converts interleaved f32 from a device's rate and channel count to `out_channels` at
/// [`SAMPLE_RATE`]: downmixes to mono by averaging, duplicates mono to stereo, keeps the first
/// two channels otherwise, and resamples linearly.
pub(crate) struct Converter {
    rate: u32,
    in_channels: usize,
    out_channels: usize,
    pos: f64,
    mapped: Vec<f32>,
}

impl Converter {
    pub fn new(rate: u32, in_channels: u16, out_channels: u16) -> Self {
        Self { rate, in_channels: in_channels.max(1) as usize, out_channels: out_channels as usize, pos: 0.0, mapped: Vec::new() }
    }

    /// Appends the converted `input` to `out`.
    pub fn push(&mut self, input: &[f32], out: &mut Vec<f32>) {
        let (n, ch) = (self.in_channels, self.out_channels);
        let frames = input.len() / n;
        self.mapped.clear();
        for f in input.chunks_exact(n) {
            match (n, ch) {
                (n, 1) => self.mapped.push(f.iter().sum::<f32>() / n as f32),
                (1, 2) => self.mapped.extend([f[0], f[0]]),
                _ => self.mapped.extend([f[0], f[1]]),
            }
        }
        if self.rate == SAMPLE_RATE {
            out.extend_from_slice(&self.mapped);
            return;
        }
        let step = self.rate as f64 / SAMPLE_RATE as f64;
        while self.pos + 1.0 < frames as f64 {
            let i = self.pos.floor() as usize;
            let t = (self.pos - i as f64) as f32;
            for c in 0..ch {
                let a = self.mapped[i * ch + c];
                let b = self.mapped[(i + 1) * ch + c];
                out.push(a + (b - a) * t);
            }
            self.pos += step;
        }
        self.pos -= frames as f64;
    }
}

/// A WAV track at [`SAMPLE_RATE`] that follows the media clock. Nothing is written while the
/// clock is paused. When the device runs ahead of or behind media time by more than 60 ms,
/// samples are dropped or silence is written, and `finish` pads or cuts the file to media time.
pub(crate) struct ClockedWav {
    writer: WavWriter,
    clock: Arc<MediaClock>,
    channels: u16,
}

impl ClockedWav {
    pub fn create(path: &Path, channels: u16, clock: Arc<MediaClock>) -> anyhow::Result<Self> {
        Ok(Self { writer: WavWriter::create(path, SAMPLE_RATE, channels)?, clock, channels })
    }

    /// Writes 48 kHz interleaved samples that arrived now.
    pub fn write(&mut self, samples: &[f32]) -> anyhow::Result<()> {
        if self.clock.is_paused() {
            return Ok(());
        }
        let tolerance = SAMPLE_RATE as i64 * 60 / 1000;
        let target = (self.clock.now().as_secs_f64() * SAMPLE_RATE as f64) as i64;
        let behind = target - self.writer.frames_written() as i64;
        if behind > tolerance {
            self.writer.write_silence((behind - tolerance / 2) as u64)?;
        } else if behind < -tolerance {
            let drop = ((-behind - tolerance / 2) as usize * self.channels as usize).min(samples.len());
            return self.writer.write(&samples[drop..]);
        }
        self.writer.write(samples)
    }

    pub fn finish(mut self) -> anyhow::Result<AudioStats> {
        let target = (self.clock.now().as_secs_f64() * SAMPLE_RATE as f64).round() as u64;
        let have = self.writer.frames_written();
        if target > have {
            self.writer.write_silence(target - have)?;
        } else {
            self.writer.truncate(target)?;
        }
        let frames = self.writer.frames_written();
        let bytes = self.writer.finish()?;
        Ok(AudioStats { frames, bytes })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[test]
    fn converter_maps_channels() {
        let mut out = Vec::new();
        Converter::new(SAMPLE_RATE, 2, 1).push(&[0.2, 0.4, -1.0, 1.0], &mut out);
        assert_eq!(out, vec![0.3, 0.0], "stereo averages to mono");
        out.clear();
        Converter::new(SAMPLE_RATE, 1, 2).push(&[0.5, -0.5], &mut out);
        assert_eq!(out, vec![0.5, 0.5, -0.5, -0.5], "mono duplicates to stereo");
        out.clear();
        Converter::new(SAMPLE_RATE, 6, 2).push(&[0.1, 0.2, 0.3, 0.4, 0.5, 0.6], &mut out);
        assert_eq!(out, vec![0.1, 0.2], "surround keeps front left and right");
    }

    #[test]
    fn converter_resamples_across_buffers_without_drift() {
        let mut c = Converter::new(44_100, 1, 1);
        let mut out = Vec::new();
        for _ in 0..10 {
            c.push(&vec![0.25; 4_410], &mut out);
        }
        let expected = 48_000;
        assert!((out.len() as i64 - expected).abs() <= 10, "{} frames for one second", out.len());
        assert!(out.iter().all(|&s| (s - 0.25).abs() < 1e-6));
    }

    #[test]
    fn clocked_wav_pads_to_media_time_and_skips_pauses() {
        let path = std::env::temp_dir().join("glint-clocked-wav.wav");
        let clock = Arc::new(MediaClock::paused());
        let mut wav = ClockedWav::create(&path, 1, clock.clone()).unwrap();
        wav.write(&[1.0; 480]).unwrap();
        clock.resume();
        std::thread::sleep(Duration::from_millis(200));
        wav.write(&[1.0; 480]).unwrap();
        clock.pause();
        let media = clock.now();
        let stats = wav.finish().unwrap();
        let expected = (media.as_secs_f64() * SAMPLE_RATE as f64).round() as u64;
        assert_eq!(stats.frames, expected, "the file ends at media time");
        let data = crate::wav::read_wav(&path).unwrap();
        assert!(data.samples[..480].iter().all(|&s| s == 0.0), "the paused write was skipped and the gap is silence");
    }
}
