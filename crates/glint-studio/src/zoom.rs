use crate::motion::{sample, step_count, to_crop, Lerp, PointerTrack, Spring, SpringK, STEP_MS};
use crate::project::{AspectMode, Project, Zoom, ZoomFocus};
use glint_media::input::InputTimeline;

/// A virtual camera state. `x`/`y` is the point, normalized inside the cropped screen,
/// that sits at the card center; `scale` is the magnification (1 shows the whole crop).
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Viewport {
    pub scale: f64,
    pub x: f64,
    pub y: f64,
}

impl Viewport {
    pub const IDENTITY: Viewport = Viewport { scale: 1.0, x: 0.5, y: 0.5 };
}

impl Lerp for Viewport {
    fn lerp(a: Self, b: Self, f: f64) -> Self {
        Viewport {
            scale: a.scale + (b.scale - a.scale) * f,
            x: a.x + (b.x - a.x) * f,
            y: a.y + (b.y - a.y) * f,
        }
    }
}

const LEAD_IN_MS: f64 = 300.0;
const HOLD_MS: f64 = 2_500.0;
const JOIN_MS: f64 = 2_500.0;
const TAIL_EXCLUSION_MS: f64 = 1_000.0;
const TRAILING_GUARD_MS: f64 = 800.0;
pub const AUTO_ZOOM_SCALE: f32 = 1.5;

/// Builds auto zooms in source time from the recorded clicks: each click inside the frame
/// gets a lead-in and hold, and zooms that overlap or nearly touch merge into one.
pub fn auto_zooms(input: &InputTimeline, source_duration_ms: f64) -> Vec<Zoom> {
    let [w, h] = input.size;
    let mut clicks: Vec<_> = input
        .clicks
        .iter()
        .filter(|c| {
            (c.t as f64) < source_duration_ms - TAIL_EXCLUSION_MS
                && (0.0..=w as f32).contains(&c.x)
                && (0.0..=h as f32).contains(&c.y)
        })
        .collect();
    clicks.sort_by_key(|c| c.t);
    let mut zooms: Vec<Zoom> = Vec::new();
    for c in clicks {
        let start = (c.t as f64 - LEAD_IN_MS).max(0.0);
        let end = (c.t as f64 + HOLD_MS).min(source_duration_ms - TRAILING_GUARD_MS);
        if end <= start {
            continue;
        }
        match zooms.last_mut() {
            Some(last) if start <= last.end_ms + JOIN_MS => last.end_ms = last.end_ms.max(end),
            _ => zooms.push(Zoom {
                start_ms: start,
                end_ms: end,
                scale: AUTO_ZOOM_SCALE,
                focus: ZoomFocus::Pointer,
                enabled: true,
                auto: true,
            }),
        }
    }
    zooms
}

impl Project {
    /// Regenerates auto zooms from the clicks, keeping manual ones.
    pub fn apply_auto_zoom(&mut self, input: &InputTimeline, source_duration_ms: f64) {
        self.zooms.retain(|z| !z.auto);
        self.zooms.extend(auto_zooms(input, source_duration_ms));
        self.zooms.sort_by(|a, b| a.start_ms.total_cmp(&b.start_ms));
    }
}

const CAMERA: SpringK = SpringK { tension: 200.0, friction: 40.0, inertia: 2.25 };
const COMFORT_WIDTHS: f64 = 1.4;
const BOUNDS_BIAS: f64 = 0.25;
const CLICK_GUARD_MS: f64 = 150.0;
const CLICK_MARGIN: f64 = 0.9;

fn active_zoom(zooms: &[Zoom], source_ms: f64) -> Option<&Zoom> {
    let rank = |z: &Zoom| (!z.auto as u8, matches!(z.focus, ZoomFocus::Fixed { .. }) as u8);
    zooms
        .iter()
        .filter(|z| z.enabled && source_ms >= z.start_ms && source_ms <= z.end_ms)
        .max_by(|a, b| rank(a).cmp(&rank(b)).then(a.start_ms.total_cmp(&b.start_ms)))
}

fn clamp_to_frame(v: f64, scale: f64) -> f64 {
    let half = 0.5 / scale.max(1.0);
    v.clamp(half, 1.0 - half)
}

pub(crate) fn camera_track(project: &Project, input: &InputTimeline) -> Vec<Viewport> {
    let n = step_count(project.duration_ms());
    if project.zooms.iter().all(|z| !z.enabled) {
        return vec![Viewport::IDENTITY; 2];
    }
    let norm = |x: f32, y: f32| to_crop(&project.crop, input.size, x as f64, y as f64);
    let inside = |&(x, y): &(f64, f64)| (0.0..=1.0).contains(&x) && (0.0..=1.0).contains(&y);
    let pointer: Vec<(f64, (f64, f64))> = input
        .pointer
        .iter()
        .map(|s| (s.t() as f64, norm(s.1, s.2)))
        .filter(|(_, p)| inside(p))
        .collect();
    let mut clicks: Vec<(f64, (f64, f64))> = input
        .clicks
        .iter()
        .map(|c| (c.t as f64, norm(c.x, c.y)))
        .filter(|(t, p)| inside(p) && project.event_output_time(*t).is_some())
        .collect();
    clicks.sort_by(|a, b| a.0.total_cmp(&b.0));

    let (mut half, mut ax, mut ay) = (Spring::new(0.5), Spring::new(0.5), Spring::new(0.5));
    let mut frames = Vec::with_capacity(n);
    for k in 0..n {
        let src = project.source_time(k as f64 * STEP_MS);
        let active = active_zoom(&project.zooms, src);
        let scale = active.map_or(1.0, |z| (z.scale as f64).max(1.0));
        let (tx, ty) = match active {
            None => (0.5, 0.5),
            Some(Zoom { focus: ZoomFocus::Fixed { x, y }, .. }) => {
                (clamp_to_frame(*x as f64, scale), clamp_to_frame(*y as f64, scale))
            }
            Some(_) => {
                let i = pointer.partition_point(|(t, _)| *t <= src);
                let (px, py) = pointer.get(i.saturating_sub(1)).map_or((0.5, 0.5), |s| s.1);
                let keep = |p: f64| {
                    let h = 0.5 / scale;
                    p + (h + p * (1.0 - 2.0 * h) - p) * BOUNDS_BIAS
                };
                (clamp_to_frame(keep(px), scale), clamp_to_frame(keep(py), scale))
            }
        };
        let travel = (tx - ax.pos).hypot(ty - ay.pos);
        let pursuit = if travel > 1e-4 { scale.min((COMFORT_WIDTHS / travel).max(1.0)) } else { scale };
        if k > 0 {
            half.step(0.5 / pursuit, CAMERA);
            ax.step(tx, CAMERA);
            ay.step(ty, CAMERA);
        }
        let s = (0.5 / half.pos.clamp(1e-6, 0.5)).max(1.0);
        let (mut x, mut y) = (clamp_to_frame(ax.pos, s), clamp_to_frame(ay.pos, s));

        let guarded = !matches!(active.map(|z| z.focus), None | Some(ZoomFocus::Fixed { .. }));
        let ci = clicks.partition_point(|(t, _)| *t <= src);
        if let Some((ct, (cx, cy))) = ci.checked_sub(1).and_then(|i| clicks.get(i)).filter(|_| guarded) {
            if src - ct <= CLICK_GUARD_MS {
                x = keep_visible(*cx, x, s);
                y = keep_visible(*cy, y, s);
                ax.pos = x;
                ay.pos = y;
            }
        }
        frames.push(Viewport { scale: s, x, y });
    }
    frames
}

fn keep_visible(p: f64, anchor: f64, scale: f64) -> f64 {
    let half = 0.5 / scale;
    let safe = half * CLICK_MARGIN;
    let (lo, hi) = ((p - safe).max(half), (p + safe).min(1.0 - half));
    if lo <= hi {
        anchor.clamp(lo, hi)
    } else {
        anchor.clamp(half, 1.0 - half)
    }
}

pub(crate) fn card_aspect(project: &Project, canvas_aspect: f64) -> f64 {
    let p = project.design.padding.clamp(0.0, 0.475) as f64;
    let short = canvas_aspect.min(1.0);
    (canvas_aspect - 2.0 * p * short) / (1.0 - 2.0 * p * short)
}

const DEAD_ZONE: f64 = 0.28;
const REFRAME_TAU_S: f64 = 0.45;
const HANDOFF_SPAN: f64 = 0.2;

pub(crate) fn reframe_track(
    project: &Project,
    input: &InputTimeline,
    camera: &[Viewport],
    pointer: &PointerTrack,
) -> Option<Vec<Viewport>> {
    let ratio = project.aspect.ratio()? as f64;
    if project.aspect_mode != AspectMode::Fill {
        return None;
    }
    let [w, h] = input.size;
    let content = (w as f64 * project.crop.w as f64) / (h as f64 * project.crop.h as f64).max(1.0);
    let card = card_aspect(project, ratio);
    let fill_scale = (card / content).max(1.0);
    let (fill_w, fill_h) = (content * fill_scale, fill_scale);
    let focus = |t: f64| {
        pointer.at(t).map(|p| to_crop(&project.crop, input.size, p.x, p.y))
    };

    let n = step_count(project.duration_ms());
    let k_smooth = 1.0 - (-(STEP_MS / 1000.0) / REFRAME_TAU_S).exp();
    let (mut cx, mut cy) = focus(0.0).unwrap_or((0.5, 0.5));
    let mut frames = Vec::with_capacity(n);
    for k in 0..n {
        let t = k as f64 * STEP_MS;
        let vp = sample(camera, t).unwrap_or(Viewport::IDENTITY);
        let m = vp.scale.max(1.0);
        let vis_x = (card / (fill_w * m)).min(1.0);
        let vis_y = (1.0 / (fill_h * m)).min(1.0);
        let (fx, fy) = focus(t).unwrap_or((vp.x, vp.y));
        let follow = |c: f64, f: f64, window: f64| {
            if f > c + window {
                f - window
            } else if f < c - window {
                f + window
            } else {
                c
            }
        };
        cx += (follow(cx, fx, vis_x * DEAD_ZONE) - cx) * k_smooth;
        cy += (follow(cy, fy, vis_y * DEAD_ZONE) - cy) * k_smooth;
        let hand = ((m - 1.0) / HANDOFF_SPAN).clamp(0.0, 1.0);
        let fit = |c: f64, z: f64, vis: f64| {
            if vis >= 1.0 {
                0.5
            } else {
                (c + (z - c) * hand).clamp(vis / 2.0, 1.0 - vis / 2.0)
            }
        };
        (cx, cy) = (fit(cx, vp.x, vis_x), fit(cy, vp.y, vis_y));
        frames.push(Viewport { scale: m, x: cx, y: cy });
    }
    Some(frames)
}

#[cfg(test)]
mod tests {
    use super::*;
    use glint_media::input::{Click, MouseButton, PointerSample};

    fn click(t: u64, x: f32, y: f32) -> Click {
        Click { t, up: t + 100, x, y, button: MouseButton::Left }
    }

    #[test]
    fn auto_zoom_clusters_clicks_with_lead_in_and_hold() {
        let input = InputTimeline {
            version: 1,
            size: [1920, 1080],
            clicks: vec![
                click(2_000, 100.0, 100.0),
                click(3_000, 200.0, 100.0),
                click(9_000, 900.0, 500.0),
                click(10_000, 5_000.0, 500.0),
                click(19_500, 500.0, 500.0),
            ],
            ..Default::default()
        };
        let zooms = auto_zooms(&input, 20_000.0);
        let spans: Vec<(f64, f64)> = zooms.iter().map(|z| (z.start_ms, z.end_ms)).collect();
        assert_eq!(spans, vec![(1_700.0, 5_500.0), (8_700.0, 11_500.0)]);
        assert!(zooms.iter().all(|z| z.auto && z.scale == 1.5 && z.focus == ZoomFocus::Pointer));
    }

    #[test]
    fn regenerating_auto_zoom_keeps_manual_zooms() {
        let input = InputTimeline {
            version: 1,
            size: [100, 100],
            clicks: vec![click(1_000, 50.0, 50.0)],
            ..Default::default()
        };
        let mut p = Project::new(10_000.0);
        p.add_zoom(6_000.0..7_000.0);
        p.apply_auto_zoom(&input, 10_000.0);
        p.apply_auto_zoom(&input, 10_000.0);
        let spans: Vec<(f64, f64, bool)> = p.zooms.iter().map(|z| (z.start_ms, z.end_ms, z.auto)).collect();
        assert_eq!(spans, vec![(700.0, 3_500.0, true), (6_000.0, 7_000.0, false)]);
    }

    #[test]
    fn camera_eases_in_holds_and_eases_out() {
        let mut p = Project::new(8_000.0);
        p.zooms.push(Zoom {
            start_ms: 1_000.0,
            end_ms: 4_000.0,
            scale: 2.0,
            focus: ZoomFocus::Fixed { x: 0.25, y: 0.25 },
            enabled: true,
            auto: false,
        });
        let input = InputTimeline { version: 1, size: [1000, 1000], ..Default::default() };
        let track = camera_track(&p, &input);
        let at = |t: f64| sample(&track, t).unwrap();
        assert_eq!(at(500.0), Viewport::IDENTITY);
        let easing = at(1_150.0);
        assert!(easing.scale > 1.0 && easing.scale < 2.0, "{easing:?}");
        let held = at(3_500.0);
        assert!((held.scale - 2.0).abs() < 0.01 && (held.x - 0.25).abs() < 0.01, "{held:?}");
        let out = at(7_500.0);
        assert!((out.scale - 1.0).abs() < 0.01 && (out.x - 0.5).abs() < 0.01, "{out:?}");
    }

    #[test]
    fn camera_follows_pointer_inside_a_zoom() {
        let mut p = Project::new(6_000.0);
        p.zooms.push(Zoom {
            start_ms: 0.0,
            end_ms: 6_000.0,
            scale: 2.0,
            focus: ZoomFocus::Pointer,
            enabled: true,
            auto: false,
        });
        let input = InputTimeline {
            version: 1,
            size: [1000, 1000],
            pointer: vec![PointerSample(0, 200.0, 500.0, 0), PointerSample(2_000, 800.0, 500.0, 0)],
            ..Default::default()
        };
        let track = camera_track(&p, &input);
        let x = |t: f64| sample(&track, t).unwrap().x;
        assert!(x(1_900.0) < 0.35, "framing the left: {}", x(1_900.0));
        assert!(x(5_000.0) > 0.65, "followed to the right: {}", x(5_000.0));
    }
}
