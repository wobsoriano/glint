//! Renders an [`EditDoc`] over its base image: the editor preview and the exported file both
//! come from [`Compositor::render`], so the preview shows exactly what gets saved.

use crate::annotate::{pt, Annotation, Pt, R};
use crate::doc::{BlurEdge, EditDoc};
use crate::effects::{self, Homography};
use crate::render::{self, Inputs, Layout};
use crate::style::Background;
use image::RgbaImage;
use std::path::Path;
use tiny_skia::{PathBuilder, Pixmap, PixmapPaint, Transform};

/// Geometry of a document rendered at some scale, in canvas pixels.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Frame {
    pub layout: Layout,
    /// Border thickness in canvas pixels.
    pub border: f32,
    /// Card plane -> canvas, when a perspective mockup is active.
    pub projection: Option<Homography>,
}

impl Frame {
    pub fn new(doc: &EditDoc, base_w: u32, base_h: u32, scale: f32) -> Frame {
        let crop = doc.crop_rect(base_w, base_h);
        let local = R::new(0.0, 0.0, crop.w, crop.h);
        let border = doc.effects.border.points();
        let layout = render::layout_with_border(&doc.style, local, doc.dpi, scale, border);
        let border = render::border_px(border, layout.unit);
        let projection = effects::projection(layout.card.inflate(border), layout.width as f32, layout.height as f32, &doc.effects.perspective);
        Frame { layout, border, projection }
    }

    /// Bordered card.
    pub fn outer_card(&self) -> R {
        self.layout.card.inflate(self.border)
    }

    /// Image (annotation) coordinates -> canvas pixels, through the mockup projection.
    pub fn to_canvas(&self, p: Pt) -> Pt {
        let c = self.layout.to_canvas(p);
        match &self.projection {
            Some(h) => h.apply(c),
            None => c,
        }
    }

    /// Canvas pixels -> image coordinates; `None` where the point is behind the camera.
    pub fn to_image(&self, p: Pt) -> Option<Pt> {
        let flat = match &self.projection {
            Some(h) => {
                let inv = h.inverse()?;
                if inv.w_at(p) <= 0.0 {
                    return None;
                }
                inv.apply(p)
            }
            None => p,
        };
        Some(self.layout.to_source(flat))
    }
}

/// Decoded images a document can reference.
pub struct Sources<'a> {
    pub base: &'a RgbaImage,
    /// Decoded `Background::Image`, if the document uses one.
    pub bg_image: Option<&'a Pixmap>,
    /// `render::make_blur_bg(base)`, if the document uses `Background::Blurred`.
    pub blur_bg: Option<&'a Pixmap>,
}

/// Renders documents, keeping the layers that did not change since the previous call.
#[derive(Default)]
pub struct Compositor {
    prepared: Option<((u32, u32, u32, u32, u32, u32), Pixmap)>,
    scene: Option<(String, Pixmap)>,
    card: Option<(String, Pixmap, Option<tiny_skia::Mask>)>,
}

impl Compositor {
    /// Renders `region` (canvas pixels at `scale`, whole canvas when `None`) of the document.
    pub fn render(&mut self, doc: &EditDoc, src: &Sources, scale: f32, region: Option<R>) -> Pixmap {
        let (bw, bh) = (src.base.width(), src.base.height());
        let f = Frame::new(doc, bw, bh, scale);
        let l = f.layout;
        let canvas = R::new(0.0, 0.0, l.width as f32, l.height as f32);
        let region = region
            .and_then(|r| r.round_out().intersect(&canvas))
            .unwrap_or(canvas);
        let blur = &doc.effects.blur;
        let blur_extent = match blur.edge {
            BlurEdge::Bleed => canvas,
            BlurEdge::Clipped => l.image,
        };
        let pad = if blur.is_active() {
            (effects::blur_radius(blur, blur_extent.w.min(blur_extent.h)) * 3.0).ceil()
        } else {
            0.0
        };
        let rp = region.inflate(pad).round_out().intersect(&canvas).unwrap_or(region);

        let mut scene = self.scene(doc, src, &f, rp);

        let card_region = match &f.projection {
            None => rp,
            Some(h) => preimage(h, rp).and_then(|r| r.inflate(2.0 + pad).round_out().intersect(&canvas)).unwrap_or(canvas),
        };
        let lc = l.translated(-card_region.x, -card_region.y);
        let (mut card, mask) = self.card(doc, src, &f, card_region);
        render::draw_highlights(&mut card, &lc, &doc.annotations, mask.as_ref());
        for a in doc.annotations.iter().filter(|a| !a.is_redaction()) {
            render::draw_annotation(&mut card, &lc, a);
        }
        effects::draw_watermark(&mut card, lc.image, l.unit, &doc.effects.watermark, mask.as_ref());
        if blur.is_active() && blur.edge == BlurEdge::Clipped {
            effects::progressive_blur(&mut card, lc.image, lc.image, blur, mask.as_ref());
        }

        match &f.projection {
            Some(h) => effects::warp_onto(&mut scene, pt(rp.x, rp.y), &card, pt(card_region.x, card_region.y), h),
            None => scene.draw_pixmap(
                (card_region.x - rp.x) as i32,
                (card_region.y - rp.y) as i32,
                card.as_ref(),
                &PixmapPaint::default(),
                Transform::identity(),
                None,
            ),
        }
        if blur.is_active() && blur.edge == BlurEdge::Bleed {
            let full = R::new(0.0, 0.0, scene.width() as f32, scene.height() as f32);
            effects::progressive_blur(&mut scene, full, canvas.translate(-rp.x, -rp.y), blur, None);
        }
        if rp == region {
            scene
        } else {
            crop_pixmap(&scene, region.translate(-rp.x, -rp.y))
        }
    }

    /// Background and shadow for `rp`.
    fn scene(&mut self, doc: &EditDoc, src: &Sources, f: &Frame, rp: R) -> Pixmap {
        let st = &doc.style;
        let key = format!("{:?}|{:?}|{:?}|{:?}", st, f, rp, doc.effects.perspective);
        if let Some((k, pm)) = &self.scene {
            if *k == key {
                return pm.clone();
            }
        }
        let l = &f.layout;
        let mut pm = Pixmap::new(rp.w as u32, rp.h as u32).unwrap();
        render::draw_background(&mut pm, &st.background, l.width as f32, l.height as f32, (-rp.x, -rp.y), src.blur_bg, src.bg_image);
        if st.shadow > 0.0 && st.shadow_opacity > 0.0 {
            let outline = rounded_outline(f.outer_card(), st.radius * l.unit);
            let outline: Vec<Pt> = outline
                .into_iter()
                .map(|p| f.projection.map_or(p, |h| h.apply(p)))
                .map(|p| pt(p.x - rp.x, p.y - rp.y))
                .collect();
            if let Some(path) = polygon_path(&outline) {
                render::draw_shadow(&mut pm, &path, st.shadow * l.unit * 0.5, st.shadow_opacity);
            }
        }
        self.scene = Some((key, pm.clone()));
        pm
    }

    /// The flat card (border, chrome, screenshot, redactions) for `cr`, and the screenshot mask.
    fn card(&mut self, doc: &EditDoc, src: &Sources, f: &Frame, cr: R) -> (Pixmap, Option<tiny_skia::Mask>) {
        let (bw, bh) = (src.base.width(), src.base.height());
        let crop = doc.crop_rect(bw, bh);
        let l = &f.layout;
        let pk = (crop.x as u32, crop.y as u32, crop.w as u32, crop.h as u32, l.image.w as u32, l.image.h as u32);
        if self.prepared.as_ref().map(|p| p.0) != Some(pk) {
            self.prepared = Some((pk, render::prepare_image(src.base, crop, pk.4, pk.5)));
        }
        let prepared = &self.prepared.as_ref().unwrap().1;
        let lc = l.translated(-cr.x, -cr.y);
        let redactions: Vec<&Annotation> = doc.annotations.iter().filter(|a| a.is_redaction()).collect();
        let key = format!("{:?}|{:?}|{:?}|{:?}|{:?}|{:?}", doc.style, f, cr, doc.effects.border, redactions, doc.window_title);
        if let Some((k, pm, mask)) = &self.card {
            if *k == key {
                return (pm.clone(), mask.clone());
            }
        }
        let inputs = Inputs {
            style: &doc.style,
            layout: lc,
            image: prepared,
            blur_bg: src.blur_bg,
            bg_image: src.bg_image,
            title: doc.window_title.as_deref(),
        };
        let mut pm = Pixmap::new(cr.w as u32, cr.h as u32).unwrap();
        let mask = render::draw_card(&mut pm, &inputs, f.border, doc.effects.border.color);
        render::apply_redactions(&mut pm, &lc, prepared, &redactions, mask.as_ref());
        self.card = Some((key, pm.clone(), mask.clone()));
        (pm, mask)
    }
}

/// Bounding box of the points that `h` maps into `r`, when all of `r` is in front of the camera.
fn preimage(h: &Homography, r: R) -> Option<R> {
    let inv = h.inverse()?;
    let corners = [pt(r.x, r.y), pt(r.right(), r.y), pt(r.right(), r.bottom()), pt(r.x, r.bottom())];
    if corners.iter().any(|c| inv.w_at(*c) <= 0.0) {
        return None;
    }
    let p = corners.map(|c| inv.apply(c));
    let x0 = p.iter().map(|q| q.x).fold(f32::MAX, f32::min);
    let y0 = p.iter().map(|q| q.y).fold(f32::MAX, f32::min);
    let x1 = p.iter().map(|q| q.x).fold(f32::MIN, f32::max);
    let y1 = p.iter().map(|q| q.y).fold(f32::MIN, f32::max);
    Some(R::new(x0, y0, x1 - x0, y1 - y0))
}

/// Points around a rounded rectangle, clockwise from the top-left corner.
fn rounded_outline(r: R, radius: f32) -> Vec<Pt> {
    let rad = radius.min(r.w / 2.0).min(r.h / 2.0).max(0.0);
    let centers = [
        (pt(r.right() - rad, r.y + rad), -90.0f32),
        (pt(r.right() - rad, r.bottom() - rad), 0.0),
        (pt(r.x + rad, r.bottom() - rad), 90.0),
        (pt(r.x + rad, r.y + rad), 180.0),
    ];
    let mut out = Vec::new();
    for (c, start) in centers {
        for i in 0..=8 {
            let a = (start + i as f32 * 90.0 / 8.0).to_radians();
            out.push(pt(c.x + rad * a.cos(), c.y + rad * a.sin()));
        }
    }
    out
}

fn polygon_path(points: &[Pt]) -> Option<tiny_skia::Path> {
    let mut pb = PathBuilder::new();
    pb.move_to(points.first()?.x, points[0].y);
    for p in &points[1..] {
        pb.line_to(p.x, p.y);
    }
    pb.close();
    pb.finish()
}

fn crop_pixmap(pm: &Pixmap, r: R) -> Pixmap {
    let (x, y, w, h) = (r.x as usize, r.y as usize, r.w as usize, r.h as usize);
    let mut out = Pixmap::new(w as u32, h as u32).unwrap();
    let pw = pm.width() as usize;
    let src = pm.data();
    let dst = out.data_mut();
    for row in 0..h {
        let s = ((y + row) * pw + x) * 4;
        dst[row * w * 4..(row + 1) * w * 4].copy_from_slice(&src[s..s + w * 4]);
    }
    out
}

/// Decodes a background image, capped at 3200 px on the long side.
pub fn load_background(path: &Path) -> Option<Pixmap> {
    let img = image::open(path).ok()?.to_rgba8();
    let img = if img.width().max(img.height()) > 3200 {
        let k = 3200.0 / img.width().max(img.height()) as f32;
        image::imageops::resize(
            &img,
            (img.width() as f32 * k).round() as u32,
            (img.height() as f32 * k).round() as u32,
            image::imageops::FilterType::Triangle,
        )
    } else {
        img
    };
    Some(render::pixmap_from_rgba(&img))
}

/// The full-resolution deliverable for `base` edited by `doc`.
pub fn render_doc(base: &RgbaImage, doc: &EditDoc) -> RgbaImage {
    let bg_image = match &doc.style.background {
        Background::Image(p) => load_background(p),
        _ => None,
    };
    let blur_bg = matches!(doc.style.background, Background::Blurred).then(|| render::make_blur_bg(base));
    let src = Sources { base, bg_image: bg_image.as_ref(), blur_bg: blur_bg.as_ref() };
    render::rgba_from_pixmap(&Compositor::default().render(doc, &src, 1.0, None))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::annotate::{RedactStyle, Shape, TextStyle};
    use crate::doc::{BlurMode, Border, Placement, ProgressiveBlur, Watermark};
    use crate::fonts::TextFont;
    use crate::style::{Chrome, Color, Style};

    fn base() -> RgbaImage {
        RgbaImage::from_fn(320, 200, |x, y| image::Rgba([(x % 256) as u8, (y % 256) as u8, ((x * y) % 251) as u8, 255]))
    }

    fn rich_doc() -> EditDoc {
        let mut doc = EditDoc::new(Style { chrome: Chrome::WinDark, ..Style::default() }, 1.0, Some("Window".into()));
        doc.effects.border = Border { enabled: true, width: 5.0, color: Color::rgb(0xFF00FF) };
        doc.effects.watermark = Watermark { enabled: true, text: "Glint".into(), placement: Placement::Grid(8), ..Default::default() };
        let mk = |shape| Annotation { id: 0, shape, color: Color::rgb(0xFF3B30), width: 4.0 };
        doc.annotations = vec![
            mk(Shape::Redact { rect: R::new(10.0, 10.0, 90.0, 40.0), kind: RedactStyle::Pixelate, strength: 9.0 }),
            mk(Shape::Redact { rect: R::new(120.0, 10.0, 90.0, 40.0), kind: RedactStyle::Blur, strength: 9.0 }),
            mk(Shape::Arrow { from: pt(20.0, 180.0), to: pt(200.0, 90.0) }),
            mk(Shape::Highlight { rect: R::new(200.0, 100.0, 80.0, 60.0) }),
            mk(Shape::Text { pos: pt(30.0, 60.0), text: "Hello".into(), size: 24.0, style: TextStyle::Outline, font: TextFont::default() }),
        ];
        doc
    }

    #[test]
    fn region_render_matches_the_same_pixels_of_a_full_render() {
        let base = base();
        let src = Sources { base: &base, bg_image: None, blur_bg: None };
        let doc = rich_doc();
        let full = Compositor::default().render(&doc, &src, 1.0, None);
        let region = R::new(57.0, 41.0, 150.0, 120.0);
        let part = Compositor::default().render(&doc, &src, 1.0, Some(region));
        assert_eq!((part.width(), part.height()), (150, 120));
        let expect = crop_pixmap(&full, region);
        // Gradient shaders round differently by at most one step when translated.
        let diff = part.data().iter().zip(expect.data()).map(|(a, b)| (*a as i32 - *b as i32).abs()).max().unwrap();
        assert!(diff <= 1, "max channel difference {diff}");
    }

    #[test]
    fn cached_render_equals_fresh_render() {
        let base = base();
        let src = Sources { base: &base, bg_image: None, blur_bg: None };
        let mut doc = rich_doc();
        let mut c = Compositor::default();
        c.render(&doc, &src, 0.5, None);
        doc.annotations.pop();
        let cached = c.render(&doc, &src, 0.5, None);
        let fresh = Compositor::default().render(&doc, &src, 0.5, None);
        assert_eq!(cached.data(), fresh.data());
    }

    #[test]
    fn border_surrounds_the_screenshot() {
        let base = base();
        let mut doc = EditDoc::new(Style { padding: 20.0, shadow: 0.0, radius: 0.0, ..Style::default() }, 1.0, None);
        doc.effects.border = Border { enabled: true, width: 6.0, color: Color::rgb(0x00FF00) };
        let out = render_doc(&base, &doc);
        assert_eq!((out.width(), out.height()), (320 + 12 + 40, 200 + 12 + 40));
        assert_eq!(out.get_pixel(23, 100).0, [0, 255, 0, 255], "left border");
        assert_eq!(out.get_pixel(26, 26).0, [0, 0, 0, 255], "screenshot starts after the border");
    }

    #[test]
    fn perspective_and_blur_render_in_regions_too() {
        let base = base();
        let src = Sources { base: &base, bg_image: None, blur_bg: None };
        let mut doc = rich_doc();
        doc.effects.perspective.rot_y = 25.0;
        doc.effects.perspective.rot_x = 10.0;
        doc.effects.blur = ProgressiveBlur { enabled: true, mode: BlurMode::Radial, ..Default::default() };
        let full = Compositor::default().render(&doc, &src, 1.0, None);
        let region = R::new(100.0, 60.0, 120.0, 90.0);
        let part = Compositor::default().render(&doc, &src, 1.0, Some(region));
        let expect = crop_pixmap(&full, region);
        let diff = part.data().iter().zip(expect.data()).map(|(a, b)| (*a as i32 - *b as i32).abs()).max().unwrap();
        assert!(diff <= 2, "max channel difference {diff}");
    }

    /// Writes sample renders of every effect. Run with:
    /// GLINT_SAMPLES=<dir> cargo test -p glint-render effect_samples -- --ignored
    #[test]
    #[ignore]
    fn write_effect_samples() {
        use crate::doc::BlurEdge;
        let dir = std::env::var("GLINT_SAMPLES").expect("GLINT_SAMPLES");
        let base = RgbaImage::from_fn(800, 500, |x, y| {
            if ((x / 40) + (y / 40)) % 2 == 0 {
                image::Rgba([240, 240, 245, 255])
            } else {
                image::Rgba([60, 90, 200, 255])
            }
        });
        let mut doc = EditDoc::new(Style::default(), 1.0, None);
        doc.annotations.push(Annotation {
            id: 1,
            shape: Shape::Text { pos: pt(40.0, 40.0), text: "Glint mockup".into(), size: 48.0, style: TextStyle::Pill, font: TextFont::default() },
            color: Color::rgb(0xFF3B30),
            width: 4.0,
        });
        let save = |name: &str, doc: &EditDoc| render_doc(&base, doc).save(format!("{dir}/{name}.png")).unwrap();
        let mut d = doc.clone();
        d.effects.border = Border { enabled: true, width: 8.0, color: Color::WHITE };
        d.effects.watermark = Watermark { enabled: true, text: "CONFIDENTIAL".into(), placement: Placement::Tiled, opacity: 0.35, ..Default::default() };
        save("border_watermark", &d);
        let mut d = doc.clone();
        d.effects.perspective.rot_y = 28.0;
        d.effects.perspective.rot_x = 12.0;
        d.effects.perspective.zoom = 0.85;
        save("perspective", &d);
        let mut d = doc.clone();
        d.effects.blur = ProgressiveBlur { enabled: true, mode: BlurMode::Radial, edge: BlurEdge::Bleed, strength: 40.0, start: 0.2, angle: 0.0 };
        save("blur_radial_bleed", &d);
        let mut d = doc.clone();
        d.effects.blur = ProgressiveBlur { enabled: true, mode: BlurMode::Directional, edge: BlurEdge::Clipped, strength: 40.0, start: 0.2, angle: 0.0 };
        save("blur_directional_clipped", &d);
    }
}
