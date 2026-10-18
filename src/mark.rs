//! Glint's mark, "Glare": two warm streaks of light across a dark glass squircle. It is drawn on a
//! 64-unit grid and scaled, so the tray, the exe icon and the app bundle icon share one source.

use crate::annotate::R;
use crate::render;
use tiny_skia::{Color, FillRule, GradientStop, LinearGradient, Mask, Paint, PathBuilder, Pixmap, Point, SpreadMode, Stroke, Transform};

const CARD: R = R { x: 2.0, y: 2.0, w: 60.0, h: 60.0 };
const RADIUS: f32 = 15.0;
const STREAKS: [([(f32, f32); 4], f32); 2] = [
    ([(30.0, -6.0), (46.0, -6.0), (14.0, 70.0), (-2.0, 70.0)], 1.0),
    ([(52.0, -6.0), (58.0, -6.0), (26.0, 70.0), (20.0, 70.0)], 0.75),
];

fn gradient(from: (f32, f32), to: (f32, f32), a: Color, b: Color, opacity: f32) -> Paint<'static> {
    let (mut a, mut b) = (a, b);
    a.apply_opacity(opacity);
    b.apply_opacity(opacity);
    let shader = LinearGradient::new(
        Point::from_xy(from.0, from.1),
        Point::from_xy(to.0, to.1),
        vec![GradientStop::new(0.0, a), GradientStop::new(1.0, b)],
        SpreadMode::Pad,
        Transform::identity(),
    )
    .unwrap();
    Paint { shader, anti_alias: true, ..Default::default() }
}

/// The mark at `size`×`size` pixels. A `template` mark is white streaks inside a white outline on
/// transparency, which macOS tints to match the menu bar.
pub fn pixmap(size: u32, template: bool) -> Pixmap {
    let mut pm = Pixmap::new(size, size).unwrap();
    let t = Transform::from_scale(size as f32 / 64.0, size as f32 / 64.0);
    let card = render::rounded_rect(&CARD, RADIUS).unwrap();
    let mut clip = Mask::new(size, size).unwrap();
    clip.fill_path(&card, FillRule::Winding, true, t);
    let white = Color::from_rgba8(255, 255, 255, 255);

    if !template {
        let bg = gradient((2.0, 2.0), (62.0, 62.0), Color::from_rgba8(0x1D, 0x20, 0x33, 255), Color::from_rgba8(0x0E, 0x0F, 0x18, 255), 1.0);
        pm.fill_path(&card, &bg, FillRule::Winding, t, None);
    }
    for (pts, opacity) in STREAKS {
        let mut pb = PathBuilder::new();
        pb.move_to(pts[0].0, pts[0].1);
        for p in &pts[1..] {
            pb.line_to(p.0, p.1);
        }
        pb.close();
        let (lo, hi) = (pts[3].0, pts[1].0);
        let paint = if template {
            gradient((lo, 0.0), (hi, 0.0), white, white, opacity)
        } else {
            gradient((lo, 0.0), (hi, 0.0), Color::from_rgba8(0xFF, 0xD2, 0x7A, 255), Color::from_rgba8(0xFF, 0x7A, 0x59, 255), opacity)
        };
        pm.fill_path(&pb.finish().unwrap(), &paint, FillRule::Winding, t, Some(&clip));
    }
    let rim = R { x: 2.5, y: 2.5, w: 59.0, h: 59.0 };
    let (alpha, width) = if template { (1.0, 4.0) } else { (0.12, 1.0) };
    let mut paint = Paint { anti_alias: true, ..Default::default() };
    paint.set_color_rgba8(255, 255, 255, (alpha * 255.0) as u8);
    let stroke = Stroke { width, ..Default::default() };
    pm.stroke_path(&render::rounded_rect(&rim, RADIUS - 0.5).unwrap(), &paint, &stroke, t, None);
    pm
}

/// The mark for window chrome, drawn once at 64 px so it stays crisp at any display scale.
pub fn badge() -> std::sync::Arc<gpui::RenderImage> {
    static BADGE: std::sync::OnceLock<std::sync::Arc<gpui::RenderImage>> = std::sync::OnceLock::new();
    BADGE.get_or_init(|| crate::desktop::render_image(&render::rgba_from_pixmap(&pixmap(64, false)))).clone()
}

/// A Windows .ico holding PNG-encoded images at the standard shell sizes.
#[cfg(test)]
fn ico() -> Vec<u8> {
    let sizes = [16u32, 20, 24, 32, 40, 48, 64, 256];
    let pngs: Vec<Vec<u8>> = sizes.iter().map(|&s| pixmap(s, false).encode_png().unwrap()).collect();
    let mut out = Vec::new();
    out.extend_from_slice(&[0, 0, 1, 0]);
    out.extend_from_slice(&(sizes.len() as u16).to_le_bytes());
    let mut offset = 6 + 16 * sizes.len() as u32;
    for (s, png) in sizes.iter().zip(&pngs) {
        let dim = if *s >= 256 { 0 } else { *s as u8 };
        out.extend_from_slice(&[dim, dim, 0, 0]);
        out.extend_from_slice(&1u16.to_le_bytes());
        out.extend_from_slice(&32u16.to_le_bytes());
        out.extend_from_slice(&(png.len() as u32).to_le_bytes());
        out.extend_from_slice(&offset.to_le_bytes());
        offset += png.len() as u32;
    }
    for png in pngs {
        out.extend_from_slice(&png);
    }
    out
}

#[cfg(test)]
mod tests {
    /// Regenerates the committed icon files after a change to the mark:
    /// `cargo test --bin glint write_icon_files -- --ignored`
    #[test]
    #[ignore]
    fn write_icon_files() {
        let dir = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("assets");
        std::fs::create_dir_all(&dir).unwrap();
        std::fs::write(dir.join("glint.ico"), super::ico()).unwrap();
        super::pixmap(1024, false).save_png(dir.join("glint-1024.png")).unwrap();
        // macOS app icons sit on an 824-pixel body inside the 1024 canvas.
        let mut mac = tiny_skia::Pixmap::new(1024, 1024).unwrap();
        let body = super::pixmap(824, false);
        mac.draw_pixmap(100, 100, body.as_ref(), &Default::default(), tiny_skia::Transform::identity(), None);
        mac.save_png(dir.join("glint-mac-1024.png")).unwrap();
    }

    #[test]
    fn corners_are_transparent_and_centre_is_ink() {
        let pm = super::pixmap(64, false);
        assert_eq!(pm.pixel(0, 0).unwrap().alpha(), 0);
        assert_eq!(pm.pixel(32, 4).unwrap().alpha(), 255);
    }
}
