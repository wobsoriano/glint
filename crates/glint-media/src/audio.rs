use std::sync::atomic::{AtomicU32, Ordering};

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
