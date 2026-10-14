use crate::audio::Pcm;
use crate::transcript::TimeRange;

#[derive(Debug, Clone, Copy, PartialEq)]
pub struct SilenceOptions {
    /// RMS level below which a window counts as silent, in dBFS.
    pub threshold_db: f32,
    pub min_duration_ms: u64,
    pub window_ms: u64,
}

impl Default for SilenceOptions {
    fn default() -> Self {
        Self { threshold_db: -42.0, min_duration_ms: 1_000, window_ms: 20 }
    }
}

/// Spans where the audio stays below the threshold for at least `min_duration_ms`.
pub fn silence_spans(pcm: Pcm, options: &SilenceOptions) -> Vec<TimeRange> {
    let mono = pcm.to_mono();
    let window = ((pcm.sample_rate as u64 * options.window_ms / 1000) as usize).max(1);
    let threshold = 10f32.powf(options.threshold_db / 20.0);
    let to_ms = |sample: usize| sample as u64 * 1000 / pcm.sample_rate as u64;

    let mut spans = Vec::new();
    let mut run_start: Option<usize> = None;
    let mut push = |start: usize, end: usize| {
        let span = TimeRange { start_ms: to_ms(start), end_ms: to_ms(end) };
        if span.duration_ms() >= options.min_duration_ms {
            spans.push(span);
        }
    };
    for (i, chunk) in mono.chunks(window).enumerate() {
        let rms = (chunk.iter().map(|s| s * s).sum::<f32>() / chunk.len() as f32).sqrt();
        let offset = i * window;
        match (rms < threshold, run_start) {
            (true, None) => run_start = Some(offset),
            (false, Some(start)) => {
                push(start, offset);
                run_start = None;
            }
            _ => {}
        }
    }
    if let Some(start) = run_start {
        push(start, mono.len());
    }
    spans
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tone(ms: usize, rate: usize) -> Vec<f32> {
        (0..ms * rate / 1000).map(|i| 0.3 * (i as f32 * 0.05).sin()).collect()
    }

    #[test]
    fn finds_long_quiet_spans_only() {
        let rate = 16_000;
        let mut samples = tone(500, rate);
        samples.extend(vec![0.0005; 1_200 * rate / 1000]);
        samples.extend(tone(300, rate));
        samples.extend(vec![0.0; 400 * rate / 1000]);
        samples.extend(tone(300, rate));
        samples.extend(vec![0.0; 1_500 * rate / 1000]);
        let spans = silence_spans(Pcm::mono(&samples, rate as u32), &SilenceOptions::default());
        assert_eq!(
            spans,
            vec![
                TimeRange { start_ms: 500, end_ms: 1_700 },
                TimeRange { start_ms: 2_700, end_ms: 4_200 },
            ]
        );
    }
}
