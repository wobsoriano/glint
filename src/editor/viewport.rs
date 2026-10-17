use crate::annotate::{pt, Pt};

pub const MAX_ZOOM: f32 = 10.0;
const MIN_ZOOM: f32 = 0.05;
const FIT_MARGIN: f32 = 28.0;

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Viewport {
    pub zoom: Option<f32>,
    pub center: Pt,
}

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct View {
    pub zoom: f32,
    pub origin: Pt,
}

impl View {
    pub fn to_screen(&self, p: Pt) -> Pt {
        pt((p.x - self.origin.x) * self.zoom, (p.y - self.origin.y) * self.zoom)
    }
    pub fn to_canvas(&self, s: Pt) -> Pt {
        pt(s.x / self.zoom + self.origin.x, s.y / self.zoom + self.origin.y)
    }
}

impl Default for Viewport {
    fn default() -> Self {
        Self { zoom: None, center: pt(0.0, 0.0) }
    }
}

const STEPS: [f32; 16] = [0.05, 0.1, 0.25, 1.0 / 3.0, 0.5, 2.0 / 3.0, 0.75, 1.0, 1.25, 1.5, 2.0, 3.0, 4.0, 6.0, 8.0, 10.0];

pub fn step(zoom: f32, up: bool) -> f32 {
    if up {
        STEPS.into_iter().find(|s| *s > zoom * 1.01).unwrap_or(MAX_ZOOM)
    } else {
        STEPS.into_iter().rev().find(|s| *s < zoom * 0.99).unwrap_or(STEPS[0])
    }
}

pub fn fit_zoom(canvas: (f32, f32), view: (f32, f32), scale_factor: f32) -> f32 {
    let m = 2.0 * FIT_MARGIN * scale_factor;
    ((view.0 - m).max(40.0) / canvas.0).min((view.1 - m).max(40.0) / canvas.1).min(1.0)
}

impl Viewport {
    pub fn resolve(&self, canvas: (f32, f32), view: (f32, f32), scale_factor: f32) -> View {
        let zoom = self.zoom.unwrap_or_else(|| fit_zoom(canvas, view, scale_factor));
        let axis = |c: f32, len: f32, v: f32| {
            let half = v / zoom / 2.0;
            if len * zoom <= v {
                len / 2.0
            } else {
                c.clamp(half, len - half)
            }
        };
        let center = if self.zoom.is_none() {
            pt(canvas.0 / 2.0, canvas.1 / 2.0)
        } else {
            pt(axis(self.center.x, canvas.0, view.0), axis(self.center.y, canvas.1, view.1))
        };
        View { zoom, origin: pt(center.x - view.0 / zoom / 2.0, center.y - view.1 / zoom / 2.0) }
    }

    pub fn zoom_to(&mut self, zoom: f32, anchor: Pt, canvas: (f32, f32), view: (f32, f32), scale_factor: f32) {
        let current = self.resolve(canvas, view, scale_factor);
        let zoom = zoom.clamp(MIN_ZOOM.min(current.zoom), MAX_ZOOM);
        let fixed = current.to_canvas(anchor);
        let origin = pt(fixed.x - anchor.x / zoom, fixed.y - anchor.y / zoom);
        self.zoom = Some(zoom);
        self.center = pt(origin.x + view.0 / zoom / 2.0, origin.y + view.1 / zoom / 2.0);
        self.settle(canvas, view, scale_factor);
    }

    pub fn pan(&mut self, dx: f32, dy: f32, canvas: (f32, f32), view: (f32, f32), scale_factor: f32) {
        let v = self.resolve(canvas, view, scale_factor);
        self.zoom = Some(v.zoom);
        self.center = pt(v.origin.x + view.0 / v.zoom / 2.0 + dx / v.zoom, v.origin.y + view.1 / v.zoom / 2.0 + dy / v.zoom);
        self.settle(canvas, view, scale_factor);
    }

    fn settle(&mut self, canvas: (f32, f32), view: (f32, f32), scale_factor: f32) {
        let v = self.resolve(canvas, view, scale_factor);
        self.center = pt(v.origin.x + view.0 / v.zoom / 2.0, v.origin.y + view.1 / v.zoom / 2.0);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    const CANVAS: (f32, f32) = (2000.0, 1000.0);
    const VIEW: (f32, f32) = (1000.0, 800.0);

    #[test]
    fn fit_leaves_breathing_room_and_centers() {
        let v = Viewport::default().resolve(CANVAS, VIEW, 1.0);
        assert_eq!(v.zoom, (1000.0 - 56.0) / 2000.0);
        let mid = v.to_canvas(pt(500.0, 400.0));
        assert!((mid.x - 1000.0).abs() < 1e-3 && (mid.y - 500.0).abs() < 1e-3);
        assert_eq!(Viewport::default().resolve((100.0, 50.0), VIEW, 1.0).zoom, 1.0, "small images are not enlarged");
    }

    #[test]
    fn zoom_steps_walk_standard_levels() {
        assert_eq!(step(1.0, true), 1.25);
        assert_eq!(step(6.0, true), 8.0);
        assert_eq!(step(0.43, true), 0.5);
        assert_eq!(step(0.43, false), 1.0 / 3.0);
        assert_eq!(step(10.0, true), MAX_ZOOM);
    }

    #[test]
    fn zoom_around_pointer_keeps_that_point_fixed() {
        let mut vp = Viewport::default();
        let anchor = pt(700.0, 300.0);
        let before = vp.resolve(CANVAS, VIEW, 1.0).to_canvas(anchor);
        vp.zoom_to(2.0, anchor, CANVAS, VIEW, 1.0);
        let after = vp.resolve(CANVAS, VIEW, 1.0);
        assert_eq!(after.zoom, 2.0);
        let p = after.to_canvas(anchor);
        assert!((p.x - before.x).abs() < 1e-2 && (p.y - before.y).abs() < 1e-2, "{p:?} vs {before:?}");
        vp.zoom_to(50.0, anchor, CANVAS, VIEW, 1.0);
        assert_eq!(vp.zoom, Some(MAX_ZOOM));
    }

    #[test]
    fn pan_stops_at_edges_and_fitting_axes_stay_centered() {
        let mut vp = Viewport { zoom: Some(1.0), center: pt(1000.0, 500.0) };
        vp.pan(-5000.0, 0.0, CANVAS, VIEW, 1.0);
        let v = vp.resolve(CANVAS, VIEW, 1.0);
        assert_eq!(v.origin.x, 0.0, "left edge stops the pan");
        vp.pan(10_000.0, 300.0, CANVAS, VIEW, 1.0);
        let v = vp.resolve(CANVAS, VIEW, 1.0);
        assert_eq!(v.origin.x, 1000.0, "right edge stops the pan");
        let mut vp = Viewport { zoom: Some(0.5), center: pt(1000.0, 0.0) };
        vp.pan(0.0, 400.0, CANVAS, VIEW, 1.0);
        let v = vp.resolve(CANVAS, VIEW, 1.0);
        assert_eq!(v.to_canvas(pt(500.0, 400.0)).y, 500.0);
    }
}
