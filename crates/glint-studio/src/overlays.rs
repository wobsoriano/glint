//! Timed overlays: click highlights, keystroke captions and spoken captions, all mapped
//! from source time onto output time so cut footage takes its overlays with it and a
//! sped-up clip does not shorten how long a caption stays readable.

use crate::project::Project;
use glint_media::input::InputTimeline;
use serde::{Deserialize, Serialize};

pub const CLICK_MS: f64 = 400.0;
const IMPACT_MS: f64 = 120.0;
const RIPPLE_DELAY_MS: f64 = 60.0;

fn ease_out_cubic(p: f64) -> f64 {
    1.0 - (1.0 - p.clamp(0.0, 1.0)).powi(3)
}

/// One click's highlight at an instant: a filled impact disc that fades quickly, then a
/// thinner ring that expands and fades. Sizes are multiples of `base`.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct ClickFrame {
    /// Screen-master pixels of the recorded click (not the smoothed cursor).
    pub x: f64,
    pub y: f64,
    pub impact_radius: f64,
    pub impact_alpha: f64,
    pub ring_radius: f64,
    pub ring_alpha: f64,
    pub ring_width: f64,
}

pub struct ClickTrack {
    presses: Vec<(f64, f64, f64)>,
}

impl ClickTrack {
    pub fn build(project: &Project, input: &InputTimeline) -> Self {
        let mut presses: Vec<_> = input
            .clicks
            .iter()
            .filter_map(|c| Some((project.event_output_time(c.t as f64)?, c.x as f64, c.y as f64)))
            .collect();
        presses.sort_by(|a, b| a.0.total_cmp(&b.0));
        Self { presses }
    }

    /// Highlights visible at `output_ms`, sized relative to `base` pixels.
    pub fn at(&self, output_ms: f64, base: f64) -> Vec<ClickFrame> {
        let end = self.presses.partition_point(|p| p.0 <= output_ms);
        let start = self.presses.partition_point(|p| p.0 < output_ms - CLICK_MS);
        self.presses[start..end]
            .iter()
            .map(|&(t, x, y)| {
                let age = output_ms - t;
                let impact = ease_out_cubic(age / IMPACT_MS);
                let ripple = ease_out_cubic((age - RIPPLE_DELAY_MS) / (CLICK_MS - RIPPLE_DELAY_MS));
                ClickFrame {
                    x,
                    y,
                    impact_radius: base * (0.38 + 0.34 * impact),
                    impact_alpha: if age <= IMPACT_MS { 0.5 * (1.0 - impact) } else { 0.0 },
                    ring_radius: base * (0.62 + 0.93 * ripple),
                    ring_alpha: if age >= RIPPLE_DELAY_MS { 0.75 * (1.0 - ripple) } else { 0.0 },
                    ring_width: (base * (0.18 - 0.08 * ripple)).max(1.0),
                }
            })
            .collect()
    }
}

const POP_IN_MS: f64 = 160.0;
const KEY_HOLD_MS: f64 = 1_100.0;
const POP_OUT_MS: f64 = 300.0;
const KEY_LIFE_MS: f64 = POP_IN_MS + KEY_HOLD_MS + POP_OUT_MS;

#[derive(Clone, Debug, PartialEq)]
pub struct KeyFrame {
    /// Modifier part including its trailing `+`, drawn dimmer (e.g. `Ctrl+Shift+`).
    pub modifiers: String,
    pub key: String,
    pub opacity: f64,
    pub scale: f64,
}

pub struct KeyTrack {
    events: Vec<(f64, String)>,
}

impl KeyTrack {
    pub fn build(project: &Project, input: &InputTimeline) -> Self {
        let mut events: Vec<_> = input
            .keys
            .iter()
            .filter(|k| !k.label.is_empty())
            .filter_map(|k| Some((project.event_output_time(k.t as f64)?, k.label.clone())))
            .collect();
        events.sort_by(|a, b| a.0.total_cmp(&b.0));
        Self { events }
    }

    /// The caption at `output_ms`. A key pressed while the previous caption is up replaces
    /// it in place without popping in; only the last of a run fades out.
    pub fn at(&self, output_ms: f64) -> Option<KeyFrame> {
        let i = self.events.partition_point(|e| e.0 <= output_ms).checked_sub(1)?;
        let (t, label) = &self.events[i];
        let natural_end = t + KEY_LIFE_MS;
        let next = self.events.get(i + 1).map_or(f64::INFINITY, |e| e.0);
        if output_ms >= natural_end.min(next) {
            return None;
        }
        let continues = i > 0 && self.events[i - 1].0 + KEY_LIFE_MS > *t;
        let (mut opacity, mut scale) = (1.0, 1.0);
        if !continues && output_ms - t < POP_IN_MS {
            let e = ease_out_cubic((output_ms - t) / POP_IN_MS);
            (opacity, scale) = (e, 0.92 + 0.08 * e);
        }
        if natural_end <= next && output_ms > natural_end - POP_OUT_MS {
            let e = ease_out_cubic((natural_end - output_ms) / POP_OUT_MS);
            opacity = f64::min(opacity, e);
            scale = f64::min(scale, 0.97 + 0.03 * e);
        }
        if opacity <= 0.005 {
            return None;
        }
        let split = label.rfind('+').filter(|&i| i + 1 < label.len()).map_or(0, |i| i + 1);
        Some(KeyFrame {
            modifiers: label[..split].to_string(),
            key: label[split..].to_string(),
            opacity,
            scale,
        })
    }
}

/// A transcribed word in source time, produced by the transcription crate.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct CaptionWord {
    pub text: String,
    pub start_ms: f64,
    pub end_ms: f64,
}

const LINE_MAX_CHARS: usize = 42;
const LINE_MAX_MS: f64 = 4_000.0;
const LINE_GAP_MS: f64 = 900.0;
const LINE_MIN_MS: f64 = 800.0;

#[derive(Clone, Debug, PartialEq)]
struct Line {
    start: f64,
    end: f64,
    words: Vec<(String, f64, f64)>,
}

/// What the caption bar shows at an instant.
#[derive(Clone, Debug, PartialEq)]
pub struct CaptionFrame<'a> {
    pub words: Vec<&'a str>,
    /// Word being spoken now, if any.
    pub active: Option<usize>,
    /// Words that have started; they render fully lit.
    pub spoken: usize,
}

pub struct CaptionTrack {
    lines: Vec<Line>,
}

impl CaptionTrack {
    /// Chunks words that survive the edit into short lines at pauses, so a caption reads at
    /// a glance: at most 42 characters or 4 s, and a new line after a 0.9 s pause.
    pub fn build(project: &Project, words: &[CaptionWord]) -> Self {
        let mut lines: Vec<Line> = Vec::new();
        for w in words {
            let text = w.text.trim();
            if text.is_empty() {
                continue;
            }
            let Some(start) = project.event_output_time(w.start_ms) else { continue };
            let end = project
                .output_time(w.end_ms)
                .filter(|&e| e >= start)
                .unwrap_or(start + (w.end_ms - w.start_ms).max(0.0));
            let fits = lines.last().is_some_and(|l| {
                let chars: usize = l.words.iter().map(|w| w.0.chars().count() + 1).sum();
                start - l.end < LINE_GAP_MS
                    && end - l.start <= LINE_MAX_MS
                    && chars + text.chars().count() <= LINE_MAX_CHARS
            });
            if fits {
                let l = lines.last_mut().unwrap();
                l.end = end;
                l.words.push((text.to_string(), start, end));
            } else {
                lines.push(Line { start, end, words: vec![(text.to_string(), start, end)] });
            }
        }
        for i in 0..lines.len() {
            let next = lines.get(i + 1).map_or(f64::INFINITY, |l| l.start);
            let l = &mut lines[i];
            l.end = l.end.max(l.start + LINE_MIN_MS).min(next);
        }
        Self { lines }
    }

    pub fn at(&self, output_ms: f64) -> Option<CaptionFrame<'_>> {
        let i = self.lines.partition_point(|l| l.start <= output_ms).checked_sub(1)?;
        let line = &self.lines[i];
        if output_ms >= line.end {
            return None;
        }
        let spoken = line.words.iter().filter(|w| w.1 <= output_ms).count();
        let active = line.words.iter().position(|w| w.1 <= output_ms && output_ms < w.2);
        Some(CaptionFrame { words: line.words.iter().map(|w| w.0.as_str()).collect(), active, spoken })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use glint_media::input::KeyPress;

    fn word(text: &str, start: f64, end: f64) -> CaptionWord {
        CaptionWord { text: text.into(), start_ms: start, end_ms: end }
    }

    #[test]
    fn captions_chunk_at_pauses_and_track_the_spoken_word() {
        let p = Project::new(20_000.0);
        let words = [
            word("Open", 1_000.0, 1_300.0),
            word("the", 1_300.0, 1_450.0),
            word("settings", 1_450.0, 2_000.0),
            word("Then", 4_000.0, 4_300.0),
            word("save", 4_300.0, 4_800.0),
        ];
        let track = CaptionTrack::build(&p, &words);
        let f = track.at(1_350.0).unwrap();
        assert_eq!(f.words, vec!["Open", "the", "settings"]);
        assert_eq!((f.active, f.spoken), (Some(1), 2));
        assert_eq!(track.at(3_000.0), None);
        let g = track.at(4_500.0).unwrap();
        assert_eq!((g.words, g.active, g.spoken), (vec!["Then", "save"], Some(1), 2));
    }

    #[test]
    fn cut_words_disappear_from_captions() {
        let mut p = Project::new(10_000.0);
        p.cut_source_ranges(&[1_000.0..2_000.0]);
        let track = CaptionTrack::build(&p, &[word("um", 1_200.0, 1_500.0), word("hello", 2_100.0, 2_600.0)]);
        let f = track.at(1_200.0).unwrap();
        assert_eq!((f.words, f.active), (vec!["hello"], Some(0)));
    }

    #[test]
    fn keystroke_pops_in_swaps_and_fades() {
        let p = Project::new(10_000.0);
        let input = InputTimeline {
            keys: vec![
                KeyPress { t: 1_000, label: "Ctrl+Shift+S".into() },
                KeyPress { t: 1_500, label: "Enter".into() },
            ],
            ..Default::default()
        };
        let track = KeyTrack::build(&p, &input);
        assert_eq!(track.at(900.0), None);
        let first = track.at(1_080.0).unwrap();
        assert_eq!((first.modifiers.as_str(), first.key.as_str()), ("Ctrl+Shift+", "S"));
        assert!(first.opacity > 0.5 && first.opacity < 1.0);
        let swapped = track.at(1_520.0).unwrap();
        assert_eq!((swapped.modifiers.as_str(), swapped.key.as_str(), swapped.opacity), ("", "Enter", 1.0));
        assert!(track.at(3_000.0).unwrap().opacity < 0.5);
        assert_eq!(track.at(3_100.0), None);
    }

    #[test]
    fn click_ring_expands_and_fades() {
        let p = Project::new(5_000.0);
        let input = InputTimeline {
            clicks: vec![glint_media::input::Click {
                t: 1_000,
                up: 1_100,
                x: 10.0,
                y: 20.0,
                button: glint_media::input::MouseButton::Left,
            }],
            ..Default::default()
        };
        let track = ClickTrack::build(&p, &input);
        assert_eq!(track.at(999.0, 10.0), vec![]);
        let early = track.at(1_000.0, 10.0)[0];
        assert_eq!((early.x, early.y, early.impact_radius, early.ring_alpha), (10.0, 20.0, 3.8, 0.0));
        let late = track.at(1_390.0, 10.0)[0];
        assert!(late.ring_radius > 15.0 && late.ring_alpha < 0.05, "{late:?}");
        assert_eq!(track.at(1_401.0, 10.0), vec![]);
    }
}
