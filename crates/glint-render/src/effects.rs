use crate::annotate::{pt, Pt, R};
use crate::doc::{BlurMode, Perspective, Placement, ProgressiveBlur, Watermark};
use crate::fonts;
use crate::render::blur_rgba;
use tiny_skia::{FillRule, FilterQuality, Mask, Paint, Pattern, Pixmap, Rect, SpreadMode, Transform};

/// 3x3 projective transform, row-major: (x, y) -> ((a x + b y + c) / w, (d x + e y + f) / w)
/// with w = g x + h y + i.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Homography(pub [f64; 9]);

impl Homography {
    pub const IDENTITY: Homography = Homography([1.0, 0.0, 0.0, 0.0, 1.0, 0.0, 0.0, 0.0, 1.0]);

    /// Maps `src`'s corners (top-left, top-right, bottom-right, bottom-left) onto `quad`.
    pub fn rect_to_quad(src: R, quad: [Pt; 4]) -> Option<Homography> {
        if src.w <= 0.0 || src.h <= 0.0 {
            return None;
        }
        let p: Vec<(f64, f64)> = quad.iter().map(|q| (q.x as f64, q.y as f64)).collect();
        let (dx1, dx2, dx3) = (p[1].0 - p[2].0, p[3].0 - p[2].0, p[0].0 - p[1].0 + p[2].0 - p[3].0);
        let (dy1, dy2, dy3) = (p[1].1 - p[2].1, p[3].1 - p[2].1, p[0].1 - p[1].1 + p[2].1 - p[3].1);
        let unit = if dx3.abs() < 1e-9 && dy3.abs() < 1e-9 {
            [p[1].0 - p[0].0, p[3].0 - p[0].0, p[0].0, p[1].1 - p[0].1, p[3].1 - p[0].1, p[0].1, 0.0, 0.0, 1.0]
        } else {
            let den = dx1 * dy2 - dx2 * dy1;
            if den.abs() < 1e-12 {
                return None;
            }
            let g = (dx3 * dy2 - dx2 * dy3) / den;
            let h = (dx1 * dy3 - dx3 * dy1) / den;
            [
                p[1].0 - p[0].0 + g * p[1].0,
                p[3].0 - p[0].0 + h * p[3].0,
                p[0].0,
                p[1].1 - p[0].1 + g * p[1].1,
                p[3].1 - p[0].1 + h * p[3].1,
                p[0].1,
                g,
                h,
                1.0,
            ]
        };
        let (sx, sy) = (1.0 / src.w as f64, 1.0 / src.h as f64);
        let (ox, oy) = (src.x as f64, src.y as f64);
        let n = Homography([sx, 0.0, -ox * sx, 0.0, sy, -oy * sy, 0.0, 0.0, 1.0]);
        Some(Homography(unit).then_after(&n))
    }

    fn then_after(&self, first: &Homography) -> Homography {
        let (a, b) = (&self.0, &first.0);
        let mut m = [0.0; 9];
        for r in 0..3 {
            for c in 0..3 {
                m[r * 3 + c] = (0..3).map(|k| a[r * 3 + k] * b[k * 3 + c]).sum();
            }
        }
        Homography(m)
    }

    pub fn apply(&self, p: Pt) -> Pt {
        let m = &self.0;
        let (x, y) = (p.x as f64, p.y as f64);
        let w = m[6] * x + m[7] * y + m[8];
        if w.abs() < 1e-12 {
            return p;
        }
        pt(((m[0] * x + m[1] * y + m[2]) / w) as f32, ((m[3] * x + m[4] * y + m[5]) / w) as f32)
    }

    pub fn inverse(&self) -> Option<Homography> {
        let [a, b, c, d, e, f, g, h, i] = self.0;
        let det = a * (e * i - f * h) - b * (d * i - f * g) + c * (d * h - e * g);
        if det.abs() < 1e-12 {
            return None;
        }
        let r = 1.0 / det;
        Some(Homography([
            (e * i - f * h) * r,
            (c * h - b * i) * r,
            (b * f - c * e) * r,
            (f * g - d * i) * r,
            (a * i - c * g) * r,
            (c * d - a * f) * r,
            (d * h - e * g) * r,
            (b * g - a * h) * r,
            (a * e - b * d) * r,
        ]))
    }

    /// Moves both the input and output planes by (dx, dy).
    pub fn translated(&self, dx: f32, dy: f32) -> Homography {
        let (dx, dy) = (dx as f64, dy as f64);
        let to = Homography([1.0, 0.0, dx, 0.0, 1.0, dy, 0.0, 0.0, 1.0]);
        let from = Homography([1.0, 0.0, -dx, 0.0, 1.0, -dy, 0.0, 0.0, 1.0]);
        to.then_after(&self.then_after(&from))
    }

    /// The denominator at `p`; projected points are only meaningful where it is positive.
    pub fn w_at(&self, p: Pt) -> f64 {
        self.0[6] * p.x as f64 + self.0[7] * p.y as f64 + self.0[8]
    }
}

fn project_point(p: Pt, card: R, canvas_w: f32, canvas_h: f32, s: &Perspective) -> Pt {
    let pivot = card.center();
    let (mut x, mut y, mut z) = ((p.x - pivot.x) as f64, (p.y - pivot.y) as f64, 0.0f64);
    let rad = |d: f32| (d as f64).to_radians();
    let (sa, ca) = rad(s.rot_x).sin_cos();
    (y, z) = (y * ca - z * sa, y * sa + z * ca);
    let (sb, cb) = rad(s.rot_y).sin_cos();
    (x, z) = (x * cb + z * sb, -x * sb + z * cb);
    let fov = (s.fov as f64).clamp(18.0, 80.0);
    let longest = card.w.max(card.h) as f64;
    let focal = longest * (0.35 + 0.5 / (fov.to_radians() / 2.0).tan().max(0.01));
    let k = focal / (focal - z).max(focal * 0.12);
    let (px, py) = (x * k, y * k);
    let (sr, cr) = rad(s.rot_z).sin_cos();
    let (rx, ry) = (px * cr - py * sr, px * sr + py * cr);
    let zoom = (s.zoom as f64).clamp(0.2, 4.0);
    pt(
        pivot.x + (rx * zoom) as f32 + s.pan_x * canvas_w,
        pivot.y + (ry * zoom) as f32 + s.pan_y * canvas_h,
    )
}

/// Card plane -> mockup homography. `card` is the bordered card rect in canvas pixels.
pub fn projection(card: R, canvas_w: f32, canvas_h: f32, s: &Perspective) -> Option<Homography> {
    if !s.is_active() {
        return None;
    }
    let quad = [
        pt(card.x, card.y),
        pt(card.right(), card.y),
        pt(card.right(), card.bottom()),
        pt(card.x, card.bottom()),
    ]
    .map(|c| project_point(c, card, canvas_w, canvas_h, s));
    Homography::rect_to_quad(card, quad)
}

/// Draws `src` (whose pixel (0,0) sits at `src_origin` in plane coordinates) through `h` onto
/// `dst` (whose pixel (0,0) sits at `dst_origin`), with bilinear sampling, source-over.
pub fn warp_onto(dst: &mut Pixmap, dst_origin: Pt, src: &Pixmap, src_origin: Pt, h: &Homography) {
    let Some(inv) = h.inverse() else { return };
    let (sw, sh) = (src.width() as i64, src.height() as i64);
    let corners = [pt(0.0, 0.0), pt(sw as f32, 0.0), pt(sw as f32, sh as f32), pt(0.0, sh as f32)]
        .map(|c| pt(c.x + src_origin.x, c.y + src_origin.y));
    let (dw, dh) = (dst.width() as i64, dst.height() as i64);
    let (x0, y0, x1, y1) = if corners.iter().all(|c| h.w_at(*c) > 0.0) {
        let p = corners.map(|c| h.apply(c));
        let minx = p.iter().map(|q| q.x).fold(f32::MAX, f32::min) - dst_origin.x;
        let miny = p.iter().map(|q| q.y).fold(f32::MAX, f32::min) - dst_origin.y;
        let maxx = p.iter().map(|q| q.x).fold(f32::MIN, f32::max) - dst_origin.x;
        let maxy = p.iter().map(|q| q.y).fold(f32::MIN, f32::max) - dst_origin.y;
        (
            (minx.floor() as i64 - 1).clamp(0, dw),
            (miny.floor() as i64 - 1).clamp(0, dh),
            (maxx.ceil() as i64 + 1).clamp(0, dw),
            (maxy.ceil() as i64 + 1).clamp(0, dh),
        )
    } else {
        (0, 0, dw, dh)
    };
    let m = inv.0;
    let sdata = src.data();
    let dst_w = dw as usize;
    let ddata = dst.data_mut();
    for y in y0..y1 {
        let fy = y as f64 + 0.5 + dst_origin.y as f64;
        for x in x0..x1 {
            let fx = x as f64 + 0.5 + dst_origin.x as f64;
            let w = m[6] * fx + m[7] * fy + m[8];
            if w <= 1e-12 {
                continue;
            }
            let u = (m[0] * fx + m[1] * fy + m[2]) / w - src_origin.x as f64 - 0.5;
            let v = (m[3] * fx + m[4] * fy + m[5]) / w - src_origin.y as f64 - 0.5;
            if u < -1.0 || v < -1.0 || u >= sw as f64 || v >= sh as f64 {
                continue;
            }
            let (ui, vi) = (u.floor() as i64, v.floor() as i64);
            let (fu, fv) = ((u - ui as f64) as f32, (v - vi as f64) as f32);
            let mut acc = [0.0f32; 4];
            for (dy, wy) in [(0, 1.0 - fv), (1, fv)] {
                let sy = vi + dy;
                if sy < 0 || sy >= sh || wy == 0.0 {
                    continue;
                }
                for (dx, wx) in [(0, 1.0 - fu), (1, fu)] {
                    let sx = ui + dx;
                    if sx < 0 || sx >= sw || wx == 0.0 {
                        continue;
                    }
                    let i = ((sy * sw + sx) * 4) as usize;
                    let k = wx * wy;
                    for c in 0..4 {
                        acc[c] += sdata[i + c] as f32 * k;
                    }
                }
            }
            if acc[3] < 0.5 {
                continue;
            }
            let di = (y as usize * dst_w + x as usize) * 4;
            let inv_a = 1.0 - acc[3] / 255.0;
            for c in 0..4 {
                ddata[di + c] = (acc[c] + ddata[di + c] as f32 * inv_a).round().min(255.0) as u8;
            }
        }
    }
}

/// Blur strength in pixels for an area whose shortest edge is `shortest`.
pub fn blur_radius(s: &ProgressiveBlur, shortest: f32) -> f32 {
    (s.strength * shortest / 1000.0).max(0.5)
}

/// How blurred (0..1) each point of `extent` is.
pub struct BlurField {
    focus: Pt,
    normal: (f32, f32),
    mode: BlurMode,
    focus_size: f32,
    transition: f32,
}

impl BlurField {
    pub fn new(extent: R, s: &ProgressiveBlur) -> Self {
        let focus = extent.center();
        let corners = [pt(extent.x, extent.y), pt(extent.right(), extent.y), pt(extent.right(), extent.bottom()), pt(extent.x, extent.bottom())];
        let a = s.angle.to_radians();
        let normal = (-a.sin(), a.cos());
        let shortest = extent.w.min(extent.h);
        let start = s.start.clamp(0.0, 1.0);
        let transition = shortest * (0.10 + 0.55 * 0.48);
        let focus_size = match s.mode {
            BlurMode::Radial => {
                let min = shortest * 0.04;
                let max = corners.iter().map(|c| c.dist(focus)).fold(min, f32::max);
                min + start * (max - min)
            }
            BlurMode::Directional => {
                let min = shortest * 0.025;
                let max = corners.iter().map(|c| ((c.x - focus.x) * normal.0 + (c.y - focus.y) * normal.1).abs()).fold(min, f32::max);
                min + start * (max - min)
            }
        };
        Self { focus, normal, mode: s.mode, focus_size, transition: transition.max(1.0) }
    }

    pub fn at(&self, p: Pt) -> f32 {
        let d = match self.mode {
            BlurMode::Radial => p.dist(self.focus),
            BlurMode::Directional => ((p.x - self.focus.x) * self.normal.0 + (p.y - self.focus.y) * self.normal.1).abs(),
        };
        let t = ((d - self.focus_size) / self.transition).clamp(0.0, 1.0);
        t * t * (3.0 - 2.0 * t)
    }
}

/// Blurs `area` of `pm` progressively. `extent` (same coordinates as `pm`) defines the focus
/// geometry; `mask` limits which pixels change.
pub fn progressive_blur(pm: &mut Pixmap, area: R, extent: R, s: &ProgressiveBlur, mask: Option<&Mask>) {
    let screen = R::new(0.0, 0.0, pm.width() as f32, pm.height() as f32);
    let Some(area) = area.round_out().intersect(&screen) else { return };
    let (ax, ay, aw, ah) = (area.x as usize, area.y as usize, area.w as usize, area.h as usize);
    if aw < 2 || ah < 2 {
        return;
    }
    let radius = blur_radius(s, extent.w.min(extent.h));
    let pw = pm.width() as usize;
    let mut base = vec![0u8; aw * ah * 4];
    {
        let data = pm.data();
        for y in 0..ah {
            let s0 = ((ay + y) * pw + ax) * 4;
            base[y * aw * 4..(y + 1) * aw * 4].copy_from_slice(&data[s0..s0 + aw * 4]);
        }
    }
    let mut levels = vec![base];
    for f in [0.25f32, 0.5, 1.0] {
        let mut l = levels[0].clone();
        blur_rgba(&mut l, aw, ah, ((radius * f) / 1.7).round().max(1.0) as usize);
        levels.push(l);
    }
    let field = BlurField::new(extent, s);
    let mask_data = mask.map(|m| (m.data(), m.width() as usize));
    let data = pm.data_mut();
    for y in 0..ah {
        for x in 0..aw {
            let (gx, gy) = (ax + x, ay + y);
            let mut t = field.at(pt(gx as f32 + 0.5, gy as f32 + 0.5)) * 3.0;
            if let Some((m, mw)) = mask_data {
                t *= m[gy * mw + gx] as f32 / 255.0;
            }
            if t <= 0.0 {
                continue;
            }
            let lo = (t.floor() as usize).min(2);
            let f = t - lo as f32;
            let i = (y * aw + x) * 4;
            let di = (gy * pw + gx) * 4;
            for c in 0..4 {
                let v = levels[lo][i + c] as f32 * (1.0 - f) + levels[lo + 1][i + c] as f32 * f;
                data[di + c] = v.round() as u8;
            }
        }
    }
}

/// Draws the watermark over `image` (the screenshot rect), clipped by `mask`. `unit` converts
/// points to canvas pixels.
pub fn draw_watermark(pm: &mut Pixmap, image: R, unit: f32, wm: &Watermark, mask: Option<&Mask>) {
    if !wm.is_visible() {
        return;
    }
    let face = fonts::font(&wm.font.family, wm.font.weight);
    let face = face.as_deref();
    let size = (wm.size * unit).max(1.0);
    let (tw, th) = fonts::measure(face, &wm.text, size);
    let color = wm.color.with_alpha(((wm.color.a() as f32) * wm.opacity.clamp(0.0, 1.0)).round() as u8);
    match wm.placement {
        Placement::Grid(i) => {
            let margin = (image.w.min(image.h) * 0.04).round();
            let col = (i % 3) as f32;
            let row = (i / 3).min(2) as f32;
            let x = image.x + margin + (image.w - 2.0 * margin - tw) * col / 2.0;
            let y = image.y + margin + (image.h - 2.0 * margin - th) * row / 2.0;
            let Some(mut layer) = Pixmap::new(pm.width(), pm.height()) else { return };
            fonts::draw(&mut layer, face, &wm.text, x, y, size, wm.font.align, color);
            pm.draw_pixmap(0, 0, layer.as_ref(), &tiny_skia::PixmapPaint::default(), Transform::identity(), mask);
        }
        Placement::Tiled => {
            let gap = size * 2.2;
            let (cw, ch) = ((tw + gap).ceil().max(1.0), (th + gap * 0.8).ceil().max(1.0));
            let Some(mut tile) = Pixmap::new(cw as u32, ch as u32) else { return };
            fonts::draw(&mut tile, face, &wm.text, gap / 2.0, gap * 0.4, size, wm.font.align, color);
            let c = image.center();
            let ts = Transform::from_translate(c.x - cw / 2.0, c.y - ch / 2.0).post_concat(Transform::from_rotate_at(wm.rotation, c.x, c.y));
            let paint = Paint {
                shader: Pattern::new(tile.as_ref(), SpreadMode::Repeat, FilterQuality::Bilinear, 1.0, ts),
                anti_alias: true,
                ..Default::default()
            };
            if let Some(r) = Rect::from_xywh(image.x, image.y, image.w, image.h) {
                pm.fill_rect(r, &paint, Transform::identity(), mask);
            }
        }
    }
}

pub fn polygon_mask(w: u32, h: u32, points: &[Pt]) -> Option<Mask> {
    let mut pb = tiny_skia::PathBuilder::new();
    pb.move_to(points[0].x, points[0].y);
    for p in &points[1..] {
        pb.line_to(p.x, p.y);
    }
    pb.close();
    let path = pb.finish()?;
    let mut m = Mask::new(w, h)?;
    m.fill_path(&path, FillRule::Winding, true, Transform::identity());
    Some(m)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn close(a: Pt, b: Pt) -> bool {
        (a.x - b.x).abs() < 1e-2 && (a.y - b.y).abs() < 1e-2
    }

    #[test]
    fn rect_to_quad_maps_corners_exactly_and_inverts() {
        let src = R::new(10.0, 20.0, 300.0, 200.0);
        let quad = [pt(40.0, 30.0), pt(290.0, 60.0), pt(260.0, 250.0), pt(20.0, 210.0)];
        let h = Homography::rect_to_quad(src, quad).unwrap();
        let corners = [pt(10.0, 20.0), pt(310.0, 20.0), pt(310.0, 220.0), pt(10.0, 220.0)];
        for (c, q) in corners.iter().zip(quad) {
            assert!(close(h.apply(*c), q), "{:?} -> {:?}, want {:?}", c, h.apply(*c), q);
        }
        let inv = h.inverse().unwrap();
        for (c, q) in corners.iter().zip(quad) {
            assert!(close(inv.apply(q), *c));
        }
        let mid = h.apply(src.center());
        assert!(!close(mid, pt(152.5, 137.5)), "{mid:?}");
    }

    #[test]
    fn perspective_projects_card_corners_like_a_pinhole_camera() {
        let card = R::new(100.0, 100.0, 400.0, 300.0);
        let s = Perspective { rot_y: 30.0, ..Default::default() };
        let h = projection(card, 600.0, 500.0, &s).unwrap();
        let tl = h.apply(pt(100.0, 100.0));
        let tr = h.apply(pt(500.0, 100.0));
        let br = h.apply(pt(500.0, 400.0));
        let bl = h.apply(pt(100.0, 400.0));
        let left_h = bl.y - tl.y;
        let right_h = br.y - tr.y;
        assert!(left_h > 300.0 && right_h < 300.0, "left {left_h} right {right_h}");
        assert!((tl.y + bl.y - 500.0).abs() < 1e-3 && (tr.y + br.y - 500.0).abs() < 1e-3);
        assert!(close(h.apply(pt(300.0, 250.0)), pt(300.0, 250.0)), "pivot is fixed");
        let focal = 400.0 * (0.35 + 0.5 / (12.0f64.to_radians()).tan());
        let z = 200.0 * 30.0f64.to_radians().sin();
        let x = 200.0 * 30.0f64.to_radians().cos();
        let expect_tr = pt(300.0 + (x * focal / (focal + z)) as f32, 250.0 - (150.0 * focal / (focal + z)) as f32);
        assert!(close(tr, expect_tr), "{tr:?} vs {expect_tr:?}");
        assert!(projection(card, 600.0, 500.0, &Perspective::default()).is_none());
    }

    #[test]
    fn warp_identity_reproduces_source() {
        let mut src = Pixmap::new(8, 8).unwrap();
        src.fill(tiny_skia::Color::from_rgba8(200, 10, 10, 255));
        let mut dst = Pixmap::new(12, 12).unwrap();
        warp_onto(&mut dst, pt(-2.0, -2.0), &src, pt(0.0, 0.0), &Homography::IDENTITY);
        let px = |x: u32, y: u32| dst.pixel(x, y).unwrap();
        assert_eq!(px(2, 2).red(), 200);
        assert_eq!(px(9, 9).red(), 200);
        assert_eq!(px(1, 1).alpha(), 0);
        assert_eq!(px(10, 10).alpha(), 0);
    }

    #[test]
    fn blur_field_is_sharp_at_focus_and_blurred_at_the_edges() {
        let extent = R::new(0.0, 0.0, 1000.0, 600.0);
        let radial = BlurField::new(extent, &ProgressiveBlur { enabled: true, start: 0.2, ..Default::default() });
        assert_eq!(radial.at(pt(500.0, 300.0)), 0.0);
        assert_eq!(radial.at(pt(0.0, 0.0)), 1.0);
        let band = BlurField::new(extent, &ProgressiveBlur { enabled: true, mode: BlurMode::Directional, start: 0.2, ..Default::default() });
        assert_eq!(band.at(pt(0.0, 300.0)), 0.0, "horizontal band stays sharp along its length");
        assert_eq!(band.at(pt(500.0, 0.0)), 1.0);
    }
}
