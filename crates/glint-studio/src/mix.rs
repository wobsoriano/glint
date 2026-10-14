//! The edited soundtrack: mic and system audio mixed with the project's gains and laid out
//! along output time through the clip list.
//!
//! A sped-up or slowed clip keeps its audio, time-stretched with WSOLA so voices keep their
//! pitch: windowed grains are read from the source at `speed` times the output rate, each one
//! nudged to the offset that best continues the previous grain, and overlap-added. A clip at
//! speed 1 copies samples unchanged. The mixer is sequential, so preview playback and export
//! produce the same samples from the same start time.

use crate::project::{AudioSettings, Clip, Project};
use crate::session::Tracks;
use glint_media::{read_wav, WavData};
use std::collections::VecDeque;
use std::sync::Arc;

pub const SAMPLE_RATE: u32 = 48_000;
/// The mix is always stereo.
pub const CHANNELS: u16 = 2;

const WIN: usize = 1920;
const HOP: usize = WIN / 2;
const TOLERANCE: usize = 480;

/// The session's audio tracks at 48 kHz: mic as mono, system audio as stereo.
#[derive(Default)]
pub struct AudioSources {
    mic: Option<Vec<f32>>,
    system: Option<Vec<f32>>,
}

impl AudioSources {
    pub fn load(tracks: &Tracks) -> anyhow::Result<Self> {
        let mic = tracks.mic.as_deref().map(read_wav).transpose()?.map(|w| to_layout(w, 1));
        let system = tracks.system.as_deref().map(read_wav).transpose()?.map(|w| to_layout(w, 2));
        Ok(Self { mic, system })
    }

    /// Whether `settings` leave anything audible.
    pub fn audible(&self, settings: &AudioSettings) -> bool {
        (self.mic.is_some() && settings.mic.enabled && settings.mic.volume > 0.0)
            || (self.system.is_some() && settings.system.enabled && settings.system.volume > 0.0)
    }

    /// One mixed stereo frame of source audio; silence outside the recording.
    fn frame(&self, i: i64, gains: (f32, f32)) -> [f32; 2] {
        let mut out = [0.0; 2];
        if i < 0 {
            return out;
        }
        let i = i as usize;
        if let Some(m) = self.mic.as_ref().filter(|_| gains.0 != 0.0) {
            let s = m.get(i).copied().unwrap_or(0.0) * gains.0;
            out = [s, s];
        }
        if let Some(s) = self.system.as_ref().filter(|_| gains.1 != 0.0) {
            if let Some(f) = s.get(2 * i..2 * i + 2) {
                out[0] += f[0] * gains.1;
                out[1] += f[1] * gains.1;
            }
        }
        out
    }
}

/// Any channel count to mono (1) or stereo (2), any rate to 48 kHz.
fn to_layout(w: WavData, channels: usize) -> Vec<f32> {
    let ch = w.channels.max(1) as usize;
    let frames = w.samples.len() / ch;
    let mut mapped = Vec::with_capacity(frames * channels);
    for f in w.samples.chunks_exact(ch) {
        match (ch, channels) {
            (_, 1) => mapped.push(f.iter().sum::<f32>() / ch as f32),
            (1, _) => mapped.extend([f[0], f[0]]),
            _ => mapped.extend([f[0], f[1]]),
        }
    }
    if w.sample_rate == SAMPLE_RATE || frames < 2 {
        return mapped;
    }
    let step = w.sample_rate as f64 / SAMPLE_RATE as f64;
    let out_frames = ((frames - 1) as f64 / step) as usize;
    let mut out = Vec::with_capacity(out_frames * channels);
    for k in 0..out_frames {
        let pos = k as f64 * step;
        let i = pos as usize;
        let t = (pos - i as f64) as f32;
        for c in 0..channels {
            let a = mapped[i * channels + c];
            let b = mapped[(i + 1) * channels + c];
            out.push(a + (b - a) * t);
        }
    }
    out
}

/// Output-timeline audio for one project state, rendered sequentially from a start time.
pub struct AudioMixer {
    sources: Arc<AudioSources>,
    clips: Vec<ClipFrames>,
    gains: (f32, f32),
    /// Next output frame to render.
    pos: u64,
    clip: usize,
    stretch: Option<(usize, Stretch)>,
}

/// A clip in sample frames: output start and end, source start, speed.
struct ClipFrames {
    out_start: u64,
    out_end: u64,
    src_start: f64,
    speed: f64,
}

fn ms_to_frames(ms: f64) -> f64 {
    ms * SAMPLE_RATE as f64 / 1000.0
}

impl AudioMixer {
    pub fn new(sources: Arc<AudioSources>, project: &Project, start_output_ms: f64) -> Self {
        let mut clips = Vec::with_capacity(project.clips.len());
        let mut out_ms = 0.0;
        for c in &project.clips {
            let end_ms = out_ms + c.output_len();
            clips.push(ClipFrames {
                out_start: ms_to_frames(out_ms).round() as u64,
                out_end: ms_to_frames(end_ms).round() as u64,
                src_start: ms_to_frames(c.start_ms),
                speed: c.speed,
            });
            out_ms = end_ms;
        }
        let gain = |t: &crate::project::AudioTrack| if t.enabled { t.volume.clamp(0.0, 2.0) } else { 0.0 };
        Self {
            sources,
            clips,
            gains: (gain(&project.audio.mic), gain(&project.audio.system)),
            pos: ms_to_frames(start_output_ms.max(0.0)).round() as u64,
            clip: 0,
            stretch: None,
        }
    }

    /// Fills `out` (interleaved stereo) with the next frames; silence past the end.
    pub fn render(&mut self, out: &mut [f32]) {
        for frame in out.chunks_exact_mut(2) {
            let [l, r] = self.next_frame();
            frame[0] = l;
            frame[1] = r;
        }
    }

    fn next_frame(&mut self) -> [f32; 2] {
        let pos = self.pos;
        self.pos += 1;
        let inside = |c: &ClipFrames| pos >= c.out_start && pos < c.out_end;
        if !self.clips.get(self.clip).is_some_and(inside) {
            match self.clips.iter().position(inside) {
                Some(i) => self.clip = i,
                None => return [0.0; 2],
            }
        }
        let ci = self.clip;
        let c = &self.clips[ci];
        let offset = (pos - c.out_start) as f64;
        if c.speed == 1.0 {
            return self.sources.frame((c.src_start + offset).round() as i64, self.gains);
        }
        if self.stretch.as_ref().is_none_or(|(i, s)| *i != ci || s.out_pos != pos) {
            self.stretch = Some((ci, Stretch::new(c.src_start + offset * c.speed, c.speed, pos)));
        }
        let (_, s) = self.stretch.as_mut().expect("set above");
        s.next(&self.sources, self.gains)
    }
}

/// WSOLA time stretch over one clip, producing one output frame at a time.
struct Stretch {
    speed: f64,
    /// Source frame the next grain nominally starts at.
    nominal: f64,
    prev: Option<i64>,
    tail: Vec<[f32; 2]>,
    ready: VecDeque<[f32; 2]>,
    /// Output frame the next `next` call returns.
    out_pos: u64,
}

impl Stretch {
    fn new(src_pos: f64, speed: f64, out_pos: u64) -> Self {
        Self { speed, nominal: src_pos, prev: None, tail: vec![[0.0; 2]; HOP], ready: VecDeque::new(), out_pos }
    }

    fn next(&mut self, src: &AudioSources, gains: (f32, f32)) -> [f32; 2] {
        if self.ready.is_empty() {
            self.grain(src, gains);
        }
        self.out_pos += 1;
        self.ready.pop_front().unwrap_or([0.0; 2])
    }

    fn grain(&mut self, src: &AudioSources, gains: (f32, f32)) {
        let nominal = self.nominal.round() as i64;
        let pos = match self.prev {
            None => nominal,
            Some(prev) => {
                // The grain should continue where the previous grain's second half leads.
                let target: Vec<f32> = (0..HOP as i64).map(|k| mono(src.frame(prev + HOP as i64 + k, gains))).collect();
                let lo = nominal - TOLERANCE as i64;
                let span: Vec<f32> = (0..(2 * TOLERANCE + HOP) as i64).map(|k| mono(src.frame(lo + k, gains))).collect();
                best_offset(&target, &span) as i64 + lo
            }
        };
        let grain: Vec<[f32; 2]> = (0..WIN).map(|k| src.frame(pos + k as i64, gains)).collect();
        for k in 0..HOP {
            let (a, b) = (hann(k), hann(k + HOP));
            let head = grain[k];
            let tail = self.tail[k];
            self.ready.push_back([tail[0] + head[0] * a, tail[1] + head[1] * a]);
            let t = grain[k + HOP];
            self.tail[k] = [t[0] * b, t[1] * b];
        }
        self.prev = Some(pos);
        self.nominal += HOP as f64 * self.speed;
    }
}

fn mono(f: [f32; 2]) -> f32 {
    f[0] + f[1]
}

/// Periodic Hann window: two copies offset by half a window sum to exactly one.
fn hann(k: usize) -> f32 {
    let x = std::f32::consts::PI * k as f32 / WIN as f32;
    x.sin() * x.sin()
}

/// Offset into `span` where a `target`-long window correlates best with `target`.
fn best_offset(target: &[f32], span: &[f32]) -> usize {
    let mut best = (f32::MIN, TOLERANCE);
    for off in (0..=span.len() - target.len()).step_by(2) {
        let corr: f32 = target.iter().zip(&span[off..]).step_by(4).map(|(a, b)| a * b).sum();
        if corr > best.0 {
            best = (corr, off);
        }
    }
    best.1
}

/// Output frames covered by `clips`, for sizing a full render.
pub fn output_frames(clips: &[Clip]) -> u64 {
    ms_to_frames(clips.iter().map(Clip::output_len).sum()).round() as u64
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::project::Clip;

    fn sources(mic: Vec<f32>) -> Arc<AudioSources> {
        Arc::new(AudioSources { mic: Some(mic), system: None })
    }

    fn render(mixer: &mut AudioMixer, frames: usize) -> Vec<f32> {
        let mut out = vec![0.0; frames * 2];
        mixer.render(&mut out);
        out.chunks_exact(2).map(|f| f[0]).collect()
    }

    #[test]
    fn unit_speed_clips_copy_source_samples_across_a_cut() {
        let ramp: Vec<f32> = (0..48_000).map(|i| i as f32 / 48_000.0).collect();
        let mut p = Project::new(1000.0);
        p.clips = vec![
            Clip { start_ms: 0.0, end_ms: 100.0, speed: 1.0 },
            Clip { start_ms: 500.0, end_ms: 600.0, speed: 1.0 },
        ];
        p.audio.mic.volume = 2.0;
        let mut m = AudioMixer::new(sources(ramp.clone()), &p, 99.0);
        let out = render(&mut m, 5_000);
        assert_eq!(out[0], ramp[4752] * 2.0);
        assert_eq!(out[48], ramp[24_000] * 2.0, "output 100 ms is source 500 ms");
        assert_eq!(out[4847], ramp[28_799] * 2.0);
        assert_eq!(out[4848], 0.0, "silence after the last clip");
    }

    #[test]
    fn stretched_clip_keeps_pitch_and_level() {
        let hz = 440.0;
        let tone: Vec<f32> = (0..96_000).map(|i| (i as f32 * hz * std::f32::consts::TAU / 48_000.0).sin() * 0.5).collect();
        let mut p = Project::new(2000.0);
        p.clips = vec![Clip { start_ms: 0.0, end_ms: 2000.0, speed: 2.0 }];
        let mut m = AudioMixer::new(sources(tone), &p, 0.0);
        let out = render(&mut m, 48_000);
        let body = &out[4_800..43_200];
        let crossings = body.windows(2).filter(|w| w[0] < 0.0 && w[1] >= 0.0).count();
        let measured_hz = crossings as f32 / (body.len() as f32 / 48_000.0);
        assert!((measured_hz - hz).abs() < 6.0, "pitch kept: {measured_hz} Hz");
        let rms = (body.iter().map(|s| s * s).sum::<f32>() / body.len() as f32).sqrt();
        assert!((rms - 0.5 / 2f32.sqrt()).abs() < 0.03, "level kept: rms {rms}");
        assert_eq!(output_frames(&p.clips), 48_000);
    }

    #[test]
    fn disabled_track_is_silent() {
        let mut p = Project::new(100.0);
        p.audio.mic.enabled = false;
        let s = sources(vec![0.5; 4800]);
        assert!(!s.audible(&p.audio));
        let mut m = AudioMixer::new(s, &p, 0.0);
        assert!(render(&mut m, 100).iter().all(|v| *v == 0.0));
    }
}
