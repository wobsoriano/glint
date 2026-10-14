/// Interleaved f32 PCM borrowed from a decoder or recorder.
#[derive(Debug, Clone, Copy)]
pub struct Pcm<'a> {
    pub samples: &'a [f32],
    pub sample_rate: u32,
    pub channels: u16,
}

impl<'a> Pcm<'a> {
    pub fn mono(samples: &'a [f32], sample_rate: u32) -> Self {
        Self { samples, sample_rate, channels: 1 }
    }

    pub fn duration_ms(&self) -> u64 {
        let frames = self.samples.len() as u64 / self.channels.max(1) as u64;
        frames * 1000 / self.sample_rate.max(1) as u64
    }

    pub fn to_mono(&self) -> Vec<f32> {
        let channels = self.channels.max(1) as usize;
        if channels == 1 {
            return self.samples.to_vec();
        }
        self.samples
            .chunks_exact(channels)
            .map(|frame| frame.iter().sum::<f32>() / channels as f32)
            .collect()
    }
}

pub const WHISPER_SAMPLE_RATE: u32 = 16_000;

/// Mono at 16 kHz, the only input whisper accepts. Downsampling averages the source samples
/// each output sample covers, which is enough low-pass for speech.
pub fn to_whisper_rate(pcm: Pcm) -> Vec<f32> {
    resample(&pcm.to_mono(), pcm.sample_rate, WHISPER_SAMPLE_RATE)
}

pub fn resample(mono: &[f32], from: u32, to: u32) -> Vec<f32> {
    if from == to || mono.is_empty() {
        return mono.to_vec();
    }
    let ratio = from as f64 / to as f64;
    let out_len = ((mono.len() as f64) / ratio).floor() as usize;
    let last = mono.len() - 1;
    (0..out_len)
        .map(|i| {
            let center = i as f64 * ratio;
            if ratio > 1.0 {
                let lo = (center - ratio / 2.0).ceil().max(0.0) as usize;
                let hi = ((center + ratio / 2.0).floor() as usize).min(last);
                let span = &mono[lo.min(hi)..=hi];
                span.iter().sum::<f32>() / span.len() as f32
            } else {
                let i0 = center.floor() as usize;
                let i1 = (i0 + 1).min(last);
                let t = (center - i0 as f64) as f32;
                mono[i0] * (1.0 - t) + mono[i1] * t
            }
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn downsampling_48k_stereo_halves_and_thirds_length() {
        let stereo: Vec<f32> = (0..48_000).flat_map(|_| [0.5, -0.5]).collect();
        let out = to_whisper_rate(Pcm { samples: &stereo, sample_rate: 48_000, channels: 2 });
        assert_eq!(out.len(), 16_000);
        assert!(out.iter().all(|s| s.abs() < 1e-6));
    }

    #[test]
    fn downsampling_preserves_a_low_tone() {
        let tone: Vec<f32> =
            (0..48_000).map(|i| (i as f32 / 48_000.0 * 440.0 * std::f32::consts::TAU).sin()).collect();
        let out = resample(&tone, 48_000, 16_000);
        let expected: Vec<f32> =
            (0..16_000).map(|i| (i as f32 / 16_000.0 * 440.0 * std::f32::consts::TAU).sin()).collect();
        let max_err = out.iter().zip(&expected).map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
        assert!(max_err < 0.05, "max error {max_err}");
    }

    #[test]
    fn upsampling_interpolates() {
        assert_eq!(resample(&[0.0, 1.0], 8_000, 16_000), vec![0.0, 0.5, 1.0, 1.0]);
    }
}
