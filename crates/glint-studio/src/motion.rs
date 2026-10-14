//! Everything that moves over output time, precomputed once per project edit at a fixed
//! 120 Hz step and interpolated per frame, so preview and export are deterministic and a
//! seek never depends on what was rendered before it.

use crate::overlays::{CaptionTrack, CaptionWord, ClickTrack, KeyTrack};
use crate::project::{CropRect, Project};
use crate::zoom::{self, Viewport};
use glint_media::input::InputTimeline;

pub(crate) const STEP_HZ: f64 = 120.0;
pub(crate) const STEP_MS: f64 = 1000.0 / STEP_HZ;
const DT: f64 = 1.0 / STEP_HZ;

/// One damped spring integrated with semi-implicit Euler.
#[derive(Clone, Copy, Debug)]
pub(crate) struct Spring {
    pub pos: f64,
    pub vel: f64,
}

#[derive(Clone, Copy, Debug)]
pub(crate) struct SpringK {
    pub tension: f64,
    pub friction: f64,
    pub inertia: f64,
}

impl Spring {
    pub fn new(pos: f64) -> Self {
        Self { pos, vel: 0.0 }
    }

    pub fn snap(&mut self, pos: f64) {
        self.pos = pos;
        self.vel = 0.0;
    }

    pub fn step(&mut self, target: f64, k: SpringK) {
        let acc = (k.tension * (target - self.pos) - k.friction * self.vel) / k.inertia;
        self.vel += acc * DT;
        self.pos += self.vel * DT;
    }
}

pub(crate) trait Lerp: Copy {
    fn lerp(a: Self, b: Self, f: f64) -> Self;
}

/// Number of fixed steps covering `duration_ms`, both ends included.
pub(crate) fn step_count(duration_ms: f64) -> usize {
    ((duration_ms / STEP_MS).ceil() as usize + 1).max(2)
}

/// Interpolates a track whose frame `k` sits at `k * STEP_MS`.
pub(crate) fn sample<T: Lerp>(frames: &[T], t_ms: f64) -> Option<T> {
    let last = frames.len().checked_sub(1)?;
    let pos = (t_ms / STEP_MS).max(0.0);
    let i = pos as usize;
    if i >= last {
        return Some(frames[last]);
    }
    Some(T::lerp(frames[i], frames[i + 1], pos - i as f64))
}

/// The reconstructed cursor at one instant.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct PointerFrame {
    /// Screen-master pixels.
    pub x: f64,
    pub y: f64,
    /// Shrinks to 0.8 while a button is held.
    pub press_scale: f64,
    pub cursor: u32,
}

impl Lerp for PointerFrame {
    fn lerp(a: Self, b: Self, f: f64) -> Self {
        PointerFrame {
            x: a.x + (b.x - a.x) * f,
            y: a.y + (b.y - a.y) * f,
            press_scale: a.press_scale + (b.press_scale - a.press_scale) * f,
            cursor: if f < 0.5 { a.cursor } else { b.cursor },
        }
    }
}

const GLIDE: SpringK = SpringK { tension: 470.0, friction: 70.0, inertia: 3.0 };
const INTERCEPT: SpringK = SpringK { tension: 538.0, friction: 40.0, inertia: 1.0 };
const SETTLE: SpringK = SpringK { tension: 300.0, friction: 30.0, inertia: 0.3 };
/// Before a click the cursor heads straight for the click point...
const ANTICIPATION_MS: f64 = 500.0;
/// ...and in the last moment switches to a stiffer spring so it lands on time.
const INTERCEPT_MS: f64 = 175.0;
const PRESS_LEAD_MS: f64 = 130.0;

#[derive(Clone, Copy)]
struct PointerEvent {
    t: f64,
    x: f64,
    y: f64,
    cursor: u32,
}

/// Source pointer samples mapped onto output time, plus one seed at each clip start so the
/// spring bridges a cut from where the pointer was at that point in the source.
fn output_pointer_events(project: &Project, input: &InputTimeline) -> Vec<PointerEvent> {
    let src = &input.pointer;
    let mut events: Vec<PointerEvent> = src
        .iter()
        .filter_map(|s| {
            let t = project.event_output_time(s.t() as f64)?;
            Some(PointerEvent { t, x: s.1 as f64, y: s.2 as f64, cursor: s.cursor() })
        })
        .collect();
    let mut clip_start = 0.0;
    for c in &project.clips {
        let i = src.partition_point(|s| (s.t() as f64) <= c.start_ms);
        if let Some(s) = src.get(i.saturating_sub(1)) {
            events.push(PointerEvent { t: clip_start, x: s.1 as f64, y: s.2 as f64, cursor: s.cursor() });
        }
        clip_start += c.output_len();
    }
    events.sort_by(|a, b| a.t.total_cmp(&b.t));
    events
}

/// The smoothed cursor over output time.
pub struct PointerTrack {
    frames: Vec<PointerFrame>,
}

impl PointerTrack {
    pub fn build(project: &Project, input: &InputTimeline) -> Self {
        let events = output_pointer_events(project, input);
        let Some(first) = events.first().copied() else { return Self { frames: Vec::new() } };
        let mut presses: Vec<(f64, f64, f64, f64)> = input
            .clicks
            .iter()
            .filter_map(|c| {
                let t = project.event_output_time(c.t as f64)?;
                let up = project.event_output_time(c.up as f64).filter(|&u| u >= t).unwrap_or(t + 150.0);
                Some((t, up, c.x as f64, c.y as f64))
            })
            .collect();
        presses.sort_by(|a, b| a.0.total_cmp(&b.0));
        let mut held: Vec<std::ops::Range<f64>> = Vec::new();
        for p in &presses {
            let r = p.0 - PRESS_LEAD_MS..p.1;
            match held.last_mut() {
                Some(last) if r.start <= last.end => last.end = last.end.max(r.end),
                _ => held.push(r),
            }
        }
        let mut hi = 0usize;

        let smoothing = project.design.cursor.smoothing.clamp(0.0, 1.0) as f64;
        let glide = SpringK { inertia: (GLIDE.inertia * smoothing * 2.0).max(0.6), ..GLIDE };
        let raw = smoothing < 0.01;

        let n = step_count(project.duration_ms());
        let mut frames = Vec::with_capacity(n);
        let (mut sx, mut sy) = (Spring::new(first.x), Spring::new(first.y));
        let mut scale = Spring::new(1.0);
        let (mut ei, mut pi) = (0usize, 0usize);
        let mut latest = first;
        for k in 0..n {
            let t = k as f64 * STEP_MS;
            while ei < events.len() && events[ei].t <= t {
                latest = events[ei];
                ei += 1;
            }
            while pi < presses.len() && presses[pi].0 <= t {
                pi += 1;
            }
            let (mut tx, mut ty, mut spring) = (latest.x, latest.y, glide);
            if let Some(p) = presses.get(pi).filter(|p| p.0 - t <= ANTICIPATION_MS) {
                (tx, ty) = (p.2, p.3);
                if p.0 - t <= INTERCEPT_MS {
                    spring = INTERCEPT;
                }
            }
            if raw {
                sx.snap(latest.x);
                sy.snap(latest.y);
            } else {
                sx.step(tx, spring);
                sy.step(ty, spring);
            }
            while hi < held.len() && held[hi].end < t {
                hi += 1;
            }
            let pressed = held.get(hi).is_some_and(|r| r.start <= t);
            scale.step(if pressed { 0.8 } else { 1.0 }, SETTLE);
            frames.push(PointerFrame { x: sx.pos, y: sy.pos, press_scale: scale.pos, cursor: latest.cursor });
        }
        Self { frames }
    }

    pub fn at(&self, output_ms: f64) -> Option<PointerFrame> {
        sample(&self.frames, output_ms)
    }
}

/// All precomputed motion for one project state.
pub struct Motion {
    pub camera: Vec<Viewport>,
    /// Pointer-following reframe for Fill on a non-original aspect; it already folds in zooms.
    pub reframe: Option<Vec<Viewport>>,
    pub pointer: PointerTrack,
    pub clicks: ClickTrack,
    pub keys: KeyTrack,
    pub captions: CaptionTrack,
}

impl Motion {
    pub fn build(project: &Project, input: &InputTimeline, captions: &[CaptionWord]) -> Self {
        let pointer = PointerTrack::build(project, input);
        let camera = zoom::camera_track(project, input);
        let reframe = zoom::reframe_track(project, input, &camera, &pointer);
        Self {
            camera,
            reframe,
            pointer,
            clicks: ClickTrack::build(project, input),
            keys: KeyTrack::build(project, input),
            captions: CaptionTrack::build(project, captions),
        }
    }

    /// The camera that frames the screen card at `output_ms`.
    pub fn viewport(&self, output_ms: f64) -> Viewport {
        sample(self.reframe.as_deref().unwrap_or(&self.camera), output_ms)
            .unwrap_or(Viewport::IDENTITY)
    }
}

/// Converts screen-master pixels to coordinates normalized inside the crop.
pub(crate) fn to_crop(crop: &CropRect, size: [u32; 2], x: f64, y: f64) -> (f64, f64) {
    let nx = (x / size[0].max(1) as f64 - crop.x as f64) / crop.w as f64;
    let ny = (y / size[1].max(1) as f64 - crop.y as f64) / crop.h as f64;
    (nx, ny)
}

#[cfg(test)]
mod tests {
    use super::*;
    use glint_media::input::{Click, MouseButton, PointerSample};

    #[test]
    fn cursor_glides_toward_samples_and_lands_on_clicks() {
        let project = Project::new(3_000.0);
        let input = InputTimeline {
            version: 1,
            size: [1000, 1000],
            pointer: vec![PointerSample(0, 100.0, 100.0, 0), PointerSample(500, 900.0, 100.0, 0)],
            clicks: vec![Click { t: 2_000, up: 2_100, x: 500.0, y: 800.0, button: MouseButton::Left }],
            ..Default::default()
        };
        let track = PointerTrack::build(&project, &input);
        let at = |t| track.at(t).unwrap();
        assert_eq!((at(0.0).x, at(0.0).y), (100.0, 100.0));
        let mid = at(550.0).x;
        assert!(mid > 100.0 && mid < 900.0, "spring is still travelling at 550 ms: {mid}");
        assert!((at(1_400.0).x - 900.0).abs() < 2.0, "settled on the sample by 1.4 s");
        let landed = at(2_000.0);
        assert!((landed.x - 500.0).abs() < 10.0 && (landed.y - 800.0).abs() < 10.0, "{landed:?}");
        assert!(at(2_100.0).press_scale < 0.9, "shrinks while pressed");
        assert!(at(2_900.0).press_scale > 0.98, "recovers after release");
    }
}
