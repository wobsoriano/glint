//! CPU compositor: turns a screenshot + style + annotations into the final image.
//! The editor preview uses this exact code at a reduced scale, so what you see is what you export.

use crate::annotate::{pt, Annotation, Pt, Shape, TextStyle, R};
use crate::annotate::RedactStyle;
use crate::style::{Background, Chrome, Color, Style};
use ab_glyph::{Font, FontVec, PxScale, ScaleFont};
use std::sync::LazyLock;
use tiny_skia::{
    FillRule, FilterQuality, GradientStop, IntSize, LineCap, LineJoin, LinearGradient,
    Mask, Paint, Path, PathBuilder, Pattern, Pixmap, PixmapPaint, Point, Rect, Shader,
    SpreadMode, Stroke, Transform,
};

// ---------------------------------------------------------------------------------------------
// Fonts

fn load_font(names: &[&str]) -> Option<FontVec> {
    let dir = std::env::var("WINDIR").unwrap_or_else(|_| "C:\\Windows".into());
    names.iter().find_map(|n| {
        let bytes = std::fs::read(format!("{dir}\\Fonts\\{n}")).ok()?;
        FontVec::try_from_vec(bytes).ok()
    })
}

static BOLD: LazyLock<Option<FontVec>> =
    LazyLock::new(|| load_font(&["seguisb.ttf", "segoeuib.ttf", "arialbd.ttf"]));
static REGULAR: LazyLock<Option<FontVec>> =
    LazyLock::new(|| load_font(&["segoeui.ttf", "arial.ttf"]));

pub fn measure_text(text: &str, size: f32) -> (f32, f32) {
    measure_with(BOLD.as_ref(), text, size)
}

fn measure_with(font: Option<&FontVec>, text: &str, size: f32) -> (f32, f32) {
    let Some(font) = font else {
        return (text.len() as f32 * size * 0.55, size * 1.25);
    };
    let sf = font.as_scaled(PxScale::from(size));
    let mut w: f32 = 0.0;
    let mut lines = 0;
    for line in text.split('\n') {
        lines += 1;
        let mut caret = 0.0;
        let mut prev = None;
        for c in line.chars() {
            let id = font.glyph_id(c);
            if let Some(p) = prev {
                caret += sf.kern(p, id);
            }
            caret += sf.h_advance(id);
            prev = Some(id);
        }
        w = w.max(caret);
    }
    (w, lines as f32 * size * 1.25)
}

/// Draws bold UI text (the face `measure_text` measures) with its top-left corner at (x, y).
pub fn draw_text(pm: &mut Pixmap, text: &str, x: f32, y: f32, size: f32, color: Color) {
    draw_text_with(pm, BOLD.as_ref(), text, x, y, size, color);
}

/// Draws (possibly multi-line) text with its top-left corner at (x, y).
fn draw_text_with(
    pm: &mut Pixmap,
    font: Option<&FontVec>,
    text: &str,
    x: f32,
    y: f32,
    size: f32,
    color: Color,
) {
    let Some(font) = font else { return };
    let sf = font.as_scaled(PxScale::from(size));
    let line_h = size * 1.25;
    let ascent = sf.ascent() + (line_h - (sf.ascent() - sf.descent())) / 2.0;
    let (pw, ph) = (pm.width() as i32, pm.height() as i32);
    let pixels = pm.pixels_mut();
    for (li, line) in text.split('\n').enumerate() {
        let baseline = y + ascent + li as f32 * line_h;
        let mut caret = x;
        let mut prev = None;
        for c in line.chars() {
            let id = font.glyph_id(c);
            if let Some(p) = prev {
                caret += sf.kern(p, id);
            }
            let glyph = id.with_scale_and_position(sf.scale(), ab_glyph::point(caret, baseline));
            caret += sf.h_advance(id);
            prev = Some(id);
            if let Some(og) = font.outline_glyph(glyph) {
                let b = og.px_bounds();
                og.draw(|gx, gy, cov| {
                    let px = b.min.x as i32 + gx as i32;
                    let py = b.min.y as i32 + gy as i32;
                    if px >= 0 && py >= 0 && px < pw && py < ph {
                        blend_px(&mut pixels[(py * pw + px) as usize], color, cov);
                    }
                });
            }
        }
    }
}

fn blend_px(dst: &mut tiny_skia::PremultipliedColorU8, c: Color, cov: f32) {
    let a = (c.a() as f32 / 255.0) * cov.clamp(0.0, 1.0);
    if a <= 0.0 {
        return;
    }
    let inv = 1.0 - a;
    let mix = |s: u8, d: u8| ((s as f32 * a) + d as f32 * inv).round().min(255.0) as u8;
    let na = (255.0 * a + dst.alpha() as f32 * inv).round().min(255.0) as u8;
    let r = mix(c.r(), dst.red()).min(na);
    let g = mix(c.g(), dst.green()).min(na);
    let b = mix(c.b(), dst.blue()).min(na);
    if let Some(p) = tiny_skia::PremultipliedColorU8::from_rgba(r, g, b, na) {
        *dst = p;
    }
}

// ---------------------------------------------------------------------------------------------
// Layout

#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Layout {
    pub width: u32,
    pub height: u32,
    /// Canvas pixels per source pixel.
    pub scale: f32,
    /// Points → canvas pixels (DPI scale × render scale).
    pub unit: f32,
    pub card: R,
    pub image: R,
    pub chrome_h: f32,
    /// Region of the source screenshot shown, in source pixels.
    pub crop: R,
}

impl Layout {
    pub fn to_canvas(&self, p: Pt) -> Pt {
        pt(
            self.image.x + (p.x - self.crop.x) * self.scale,
            self.image.y + (p.y - self.crop.y) * self.scale,
        )
    }
    pub fn to_source(&self, p: Pt) -> Pt {
        pt(
            (p.x - self.image.x) / self.scale + self.crop.x,
            (p.y - self.image.y) / self.scale + self.crop.y,
        )
    }
    pub fn rect_to_canvas(&self, r: &R) -> R {
        let a = self.to_canvas(pt(r.x, r.y));
        R {
            x: a.x,
            y: a.y,
            w: r.w * self.scale,
            h: r.h * self.scale,
        }
    }
}

/// `dpi` is the scale factor of the monitor the screenshot came from; `scale` is the render scale.
pub fn layout(style: &Style, crop: R, dpi: f32, scale: f32) -> Layout {
    let unit = dpi * scale;
    let iw = (crop.w * scale).round().max(1.0);
    let ih = (crop.h * scale).round().max(1.0);
    let pad = (style.padding * unit).round();
    let inset = (style.inset * unit).round();
    let chrome_h = match style.chrome {
        Chrome::None => 0.0,
        Chrome::MacLight | Chrome::MacDark => (30.0 * unit).round(),
        Chrome::WinLight | Chrome::WinDark => (32.0 * unit).round(),
    };
    let card_w = iw + 2.0 * inset;
    let card_h = ih + 2.0 * inset + chrome_h;
    let mut cw = card_w + 2.0 * pad;
    let mut ch = card_h + 2.0 * pad;
    if let Some(a) = style.aspect {
        let target = a.w / a.h;
        if cw / ch < target {
            cw = (ch * target).round();
        } else {
            ch = (cw / target).round();
        }
    }
    let col = (style.align % 3) as f32;
    let row = (style.align / 3) as f32;
    let card_x = (pad + (cw - card_w - 2.0 * pad) * col / 2.0).round();
    let card_y = (pad + (ch - card_h - 2.0 * pad) * row / 2.0).round();
    let card = R {
        x: card_x,
        y: card_y,
        w: card_w,
        h: card_h,
    };
    let image = R {
        x: card_x + inset,
        y: card_y + inset + chrome_h,
        w: iw,
        h: ih,
    };
    Layout {
        width: cw.max(1.0) as u32,
        height: ch.max(1.0) as u32,
        scale: iw / crop.w.max(1.0),
        unit,
        card,
        image,
        chrome_h,
        crop,
    }
}

// ---------------------------------------------------------------------------------------------
// Image helpers

pub fn pixmap_from_rgba(img: &image::RgbaImage) -> Pixmap {
    let mut data = img.as_raw().clone();
    // Premultiply (screenshots are opaque, but images used as backgrounds might not be).
    for px in data.chunks_exact_mut(4) {
        let a = px[3] as u16;
        if a != 255 {
            px[0] = ((px[0] as u16 * a + 127) / 255) as u8;
            px[1] = ((px[1] as u16 * a + 127) / 255) as u8;
            px[2] = ((px[2] as u16 * a + 127) / 255) as u8;
        }
    }
    Pixmap::from_vec(data, IntSize::from_wh(img.width(), img.height()).unwrap()).unwrap()
}

pub fn rgba_from_pixmap(pm: &Pixmap) -> image::RgbaImage {
    let mut out = Vec::with_capacity(pm.data().len());
    for p in pm.pixels() {
        let c = p.demultiply();
        out.extend_from_slice(&[c.red(), c.green(), c.blue(), c.alpha()]);
    }
    image::RgbaImage::from_raw(pm.width(), pm.height(), out).unwrap()
}

/// Crops a region of `src` and resizes it to `w`×`h`.
pub fn prepare_image(src: &image::RgbaImage, crop: R, w: u32, h: u32) -> Pixmap {
    let x = crop.x.max(0.0) as u32;
    let y = crop.y.max(0.0) as u32;
    let cw = (crop.w as u32).min(src.width().saturating_sub(x)).max(1);
    let ch = (crop.h as u32).min(src.height().saturating_sub(y)).max(1);
    let view = image::imageops::crop_imm(src, x, y, cw, ch).to_image();
    let resized = if view.width() == w && view.height() == h {
        view
    } else {
        image::imageops::resize(&view, w.max(1), h.max(1), image::imageops::FilterType::Triangle)
    };
    pixmap_from_rgba(&resized)
}

/// Fast approximate gaussian blur: three box-blur passes on premultiplied RGBA.
pub fn blur_rgba(data: &mut [u8], w: usize, h: usize, radius: usize) {
    if radius == 0 || w == 0 || h == 0 {
        return;
    }
    let mut tmp = vec![0u8; data.len()];
    for _ in 0..3 {
        box_pass(data, &mut tmp, w, h, radius, 4, true);
        box_pass(&tmp, data, w, h, radius, 4, false);
    }
}

pub fn blur_alpha(data: &mut [u8], w: usize, h: usize, radius: usize) {
    if radius == 0 || w == 0 || h == 0 {
        return;
    }
    let mut tmp = vec![0u8; data.len()];
    for _ in 0..3 {
        box_pass(data, &mut tmp, w, h, radius, 1, true);
        box_pass(&tmp, data, w, h, radius, 1, false);
    }
}

fn box_pass(src: &[u8], dst: &mut [u8], w: usize, h: usize, r: usize, ch: usize, horizontal: bool) {
    let (outer, inner) = if horizontal { (h, w) } else { (w, h) };
    let idx = |o: usize, i: usize| {
        if horizontal {
            (o * w + i) * ch
        } else {
            (i * w + o) * ch
        }
    };
    let div = (2 * r + 1) as u32;
    let mut sums = [0u32; 4];
    for o in 0..outer {
        for c in 0..ch {
            // Edge pixels are clamped.
            let first = src[idx(o, 0) + c] as u32;
            let mut s = first * (r as u32 + 1);
            for i in 1..=r {
                s += src[idx(o, i.min(inner - 1)) + c] as u32;
            }
            sums[c] = s;
        }
        for i in 0..inner {
            let d = idx(o, i);
            for c in 0..ch {
                dst[d + c] = (sums[c] / div) as u8;
            }
            let add = idx(o, (i + r + 1).min(inner - 1));
            let sub = idx(o, i.saturating_sub(r));
            for c in 0..ch {
                sums[c] = sums[c] + src[add + c] as u32 - src[sub + c] as u32;
            }
        }
    }
}

fn rect(r: &R) -> Option<Rect> {
    Rect::from_xywh(r.x, r.y, r.w.max(0.5), r.h.max(0.5))
}

pub fn rounded_rect(r: &R, radius: f32) -> Option<Path> {
    let rad = radius.min(r.w / 2.0).min(r.h / 2.0).max(0.0);
    if rad < 0.5 {
        return Some(PathBuilder::from_rect(rect(r)?));
    }
    // Cubic approximation of a quarter circle.
    let k = rad * 0.4477;
    let (x, y, rr, b) = (r.x, r.y, r.right(), r.bottom());
    let mut pb = PathBuilder::new();
    pb.move_to(x + rad, y);
    pb.line_to(rr - rad, y);
    pb.cubic_to(rr - k, y, rr, y + k, rr, y + rad);
    pb.line_to(rr, b - rad);
    pb.cubic_to(rr, b - k, rr - k, b, rr - rad, b);
    pb.line_to(x + rad, b);
    pb.cubic_to(x + k, b, x, b - k, x, b - rad);
    pb.line_to(x, y + rad);
    pb.cubic_to(x, y + k, x + k, y, x + rad, y);
    pb.close();
    pb.finish()
}

fn solid(c: Color) -> Paint<'static> {
    let mut p = Paint::default();
    p.set_color(c.to_skia());
    p.anti_alias = true;
    p
}

fn gradient_shader(stops: &[Color], angle: f32, w: f32, h: f32) -> Shader<'static> {
    let rad = angle.to_radians();
    let (dx, dy) = (rad.cos(), rad.sin());
    let half = (w * dx).abs() / 2.0 + (h * dy).abs() / 2.0;
    let (cx, cy) = (w / 2.0, h / 2.0);
    let n = stops.len().max(2) - 1;
    let gs = stops
        .iter()
        .enumerate()
        .map(|(i, c)| GradientStop::new(i as f32 / n as f32, c.to_skia()))
        .collect();
    LinearGradient::new(
        Point::from_xy(cx - dx * half, cy - dy * half),
        Point::from_xy(cx + dx * half, cy + dy * half),
        gs,
        SpreadMode::Pad,
        Transform::identity(),
    )
    .unwrap_or(Shader::SolidColor(stops[0].to_skia()))
}

/// Draws `img` scaled to cover the `w`×`h` canvas.
fn draw_cover(pm: &mut Pixmap, img: &Pixmap, quality: FilterQuality) {
    let (w, h) = (pm.width() as f32, pm.height() as f32);
    let s = (w / img.width() as f32).max(h / img.height() as f32);
    let tx = (w - img.width() as f32 * s) / 2.0;
    let ty = (h - img.height() as f32 * s) / 2.0;
    let paint = Paint {
        shader: Pattern::new(
            img.as_ref(),
            SpreadMode::Pad,
            quality,
            1.0,
            Transform::from_scale(s, s).post_translate(tx, ty),
        ),
        ..Default::default()
    };
    pm.fill_rect(Rect::from_xywh(0.0, 0.0, w, h).unwrap(), &paint, Transform::identity(), None);
}

/// Average color around the image border, used for "auto balance" insets.
pub fn edge_color(img: &Pixmap) -> Color {
    let (w, h) = (img.width() as usize, img.height() as usize);
    let px = img.pixels();
    let (mut r, mut g, mut b, mut n) = (0u64, 0u64, 0u64, 0u64);
    let mut add = |x: usize, y: usize| {
        let p = px[y * w + x].demultiply();
        r += p.red() as u64;
        g += p.green() as u64;
        b += p.blue() as u64;
        n += 1;
    };
    let step = ((w + h) / 400).max(1);
    for x in (0..w).step_by(step) {
        add(x, 0);
        add(x, h - 1);
    }
    for y in (0..h).step_by(step) {
        add(0, y);
        add(w - 1, y);
    }
    let n = n.max(1);
    Color::from_rgba((r / n) as u8, (g / n) as u8, (b / n) as u8, 255)
}

/// Two vivid colors derived from the screenshot, for the "Auto" gradient.
pub fn auto_gradient(img: &image::RgbaImage) -> Vec<Color> {
    let small = image::imageops::thumbnail(img, 24, 24);
    // Pick the most saturated pixels of each half.
    let pick = |range: std::ops::Range<u32>| {
        let mut best = (Color::rgb(0x6366F1), -1.0f32);
        for y in 0..small.height() {
            for x in range.clone() {
                let p = small.get_pixel(x, y);
                let c = Color::from_rgba(p[0], p[1], p[2], 255);
                let (_, s, l) = c.to_hsl();
                let score = s * (1.0 - (l - 0.5).abs() * 1.6);
                if score > best.1 {
                    best = (c, score);
                }
            }
        }
        let (hh, s, _) = best.0.to_hsl();
        Color::from_hsl(hh, s.max(0.55), 0.6)
    };
    let mid = small.width() / 2;
    let a = pick(0..mid);
    let mut b = pick(mid..small.width());
    if a == b {
        let (h, s, l) = a.to_hsl();
        b = Color::from_hsl(h + 40.0, s, l);
    }
    vec![a, b]
}

// ---------------------------------------------------------------------------------------------
// Composition

pub struct Inputs<'a> {
    pub style: &'a Style,
    pub layout: Layout,
    /// The cropped screenshot, already resized to `layout.image` size.
    pub image: &'a Pixmap,
    /// Small pre-blurred image used for `Background::Blurred`.
    pub blur_bg: Option<&'a Pixmap>,
    /// Decoded `Background::Image`.
    pub bg_image: Option<&'a Pixmap>,
    pub title: Option<&'a str>,
}

/// Renders background, shadow, window chrome, screenshot and redactions.
pub fn render_base(inp: &Inputs, annotations: &[Annotation]) -> Pixmap {
    let l = &inp.layout;
    let mut pm = Pixmap::new(l.width, l.height).unwrap();
    paint_background(&mut pm, &inp.style.background, inp.blur_bg, inp.bg_image);

    let radius = inp.style.radius * l.unit;
    let card_path = rounded_rect(&l.card, radius);

    if inp.style.shadow > 0.0 && inp.style.shadow_opacity > 0.0 {
        draw_shadow(
            &mut pm,
            &l.card,
            radius,
            inp.style.shadow * l.unit * 0.5,
            inp.style.shadow_opacity,
        );
    }

    let Some(card_path) = card_path else { return pm };
    let mut card_mask = Mask::new(l.width, l.height).unwrap();
    card_mask.fill_path(&card_path, FillRule::Winding, true, Transform::identity());

    // Card background (visible through inset).
    if inp.style.inset > 0.0 || l.chrome_h > 0.0 {
        let fill = if inp.style.auto_balance {
            edge_color(inp.image)
        } else if inp.style.chrome.is_dark() {
            Color::rgb(0x1E1E1E)
        } else {
            Color::WHITE
        };
        pm.fill_path(&card_path, &solid(fill), FillRule::Winding, Transform::identity(), None);
    }

    if l.chrome_h > 0.0 {
        draw_chrome(&mut pm, l, inp.style.chrome, inp.title.filter(|_| inp.style.chrome_title), &card_mask);
    }

    let mut image_mask = card_mask.clone();
    image_mask.intersect_path(
        &PathBuilder::from_rect(rect(&l.image).unwrap()),
        FillRule::Winding,
        true,
        Transform::identity(),
    );
    pm.draw_pixmap(
        l.image.x as i32,
        l.image.y as i32,
        inp.image.as_ref(),
        &PixmapPaint::default(),
        Transform::identity(),
        Some(&image_mask),
    );

    for a in annotations.iter().filter(|a| a.is_redaction()) {
        if let Shape::Redact { rect: r, kind } = &a.shape {
            apply_redaction(&mut pm, l, r, *kind);
        }
    }
    pm
}

/// Fills the whole pixmap with a background. `blur_bg` and `bg_image` are the decoded
/// sources for `Background::Blurred` and `Background::Image`.
pub fn paint_background(
    pm: &mut Pixmap,
    background: &Background,
    blur_bg: Option<&Pixmap>,
    bg_image: Option<&Pixmap>,
) {
    let (w, h) = (pm.width() as f32, pm.height() as f32);
    let full = Rect::from_xywh(0.0, 0.0, w, h).unwrap();
    match background {
        Background::None => {}
        Background::Solid(c) => pm.fill(c.to_skia()),
        Background::Gradient(g) => {
            let paint = Paint {
                shader: gradient_shader(&g.stops, g.angle, w, h),
                ..Default::default()
            };
            pm.fill_rect(full, &paint, Transform::identity(), None);
        }
        Background::Blurred => {
            if let Some(b) = blur_bg {
                draw_cover(pm, b, FilterQuality::Bilinear);
                pm.fill_rect(full, &solid(Color(0x0000001F)), Transform::identity(), None);
            }
        }
        Background::Image(_) => match bg_image {
            Some(b) => draw_cover(pm, b, FilterQuality::Bicubic),
            None => pm.fill(Color::rgb(0x27272A).to_skia()),
        },
    }
}

/// Soft drop shadow under a rounded card, offset slightly downward.
pub fn draw_shadow(pm: &mut Pixmap, card: &R, radius: f32, blur: f32, opacity: f32) {
    let blur_r = blur.max(1.0);
    let off_y = blur * 0.45;
    let margin = (blur_r * 3.0).ceil();
    let region = R {
        x: (card.x - margin).floor(),
        y: (card.y - margin + off_y).floor(),
        w: (card.w + margin * 2.0).ceil(),
        h: (card.h + margin * 2.0).ceil(),
    };
    let (rw, rh) = (region.w as usize, region.h as usize);
    let Some(mut mask) = Mask::new(rw as u32, rh as u32) else { return };
    let local = R {
        x: card.x - region.x,
        y: card.y + off_y - region.y,
        w: card.w,
        h: card.h,
    };
    if let Some(p) = rounded_rect(&local, radius) {
        mask.fill_path(&p, FillRule::Winding, true, Transform::identity());
    }
    blur_alpha(mask.data_mut(), rw, rh, (blur_r / 1.7).round() as usize);
    let (pw, ph) = (pm.width() as i32, pm.height() as i32);
    let pixels = pm.pixels_mut();
    let data = mask.data();
    for y in 0..rh as i32 {
        let py = region.y as i32 + y;
        if py < 0 || py >= ph {
            continue;
        }
        for x in 0..rw as i32 {
            let px = region.x as i32 + x;
            if px < 0 || px >= pw {
                continue;
            }
            let a = data[(y as usize) * rw + x as usize] as f32 / 255.0 * opacity;
            if a > 0.0 {
                blend_px(&mut pixels[(py * pw + px) as usize], Color::BLACK, a);
            }
        }
    }
}

fn draw_chrome(pm: &mut Pixmap, l: &Layout, chrome: Chrome, title: Option<&str>, mask: &Mask) {
    let u = l.unit;
    let bar = R {
        x: l.card.x,
        y: l.card.y,
        w: l.card.w,
        h: l.chrome_h,
    };
    let (bg, fg, line) = if chrome.is_dark() {
        (Color::rgb(0x2B2B2E), Color::rgb(0xA1A1A6), Color::rgb(0x161618))
    } else {
        (Color::rgb(0xF2F2F4), Color::rgb(0x5F5F66), Color::rgb(0xDADADF))
    };
    if let Some(r) = rect(&bar) {
        pm.fill_rect(r, &solid(bg), Transform::identity(), Some(mask));
    }
    if let Some(r) = Rect::from_xywh(bar.x, bar.bottom() - u.max(1.0), bar.w, u.max(1.0)) {
        pm.fill_rect(r, &solid(line), Transform::identity(), Some(mask));
    }
    let cy = bar.y + bar.h / 2.0;
    match chrome {
        Chrome::MacLight | Chrome::MacDark => {
            for (i, c) in [0xFF5F57u32, 0xFEBC2E, 0x28C840].iter().enumerate() {
                if let Some(p) = PathBuilder::from_circle(bar.x + 18.0 * u + i as f32 * 20.0 * u, cy, 6.0 * u) {
                    pm.fill_path(&p, &solid(Color::rgb(*c)), FillRule::Winding, Transform::identity(), None);
                }
            }
        }
        _ => {
            let stroke = Stroke {
                width: (1.0 * u).max(1.0),
                ..Default::default()
            };
            let paint = solid(fg);
            let bw = 46.0 * u;
            let s = 5.0 * u;
            // Close, maximize, minimize from the right.
            let x0 = bar.right() - bw / 2.0;
            let mut pb = PathBuilder::new();
            pb.move_to(x0 - s, cy - s);
            pb.line_to(x0 + s, cy + s);
            pb.move_to(x0 + s, cy - s);
            pb.line_to(x0 - s, cy + s);
            let x1 = x0 - bw;
            pb.push_rect(Rect::from_xywh(x1 - s, cy - s, s * 2.0, s * 2.0).unwrap());
            let x2 = x1 - bw;
            pb.move_to(x2 - s, cy);
            pb.line_to(x2 + s, cy);
            if let Some(p) = pb.finish() {
                pm.stroke_path(&p, &paint, &stroke, Transform::identity(), None);
            }
        }
    }
    if let Some(t) = title {
        let size = 12.5 * u;
        let font = REGULAR.as_ref();
        let max_w = bar.w - 200.0 * u;
        let mut t = t.to_string();
        while !t.is_empty() && measure_with(font, &t, size).0 > max_w {
            t.pop();
        }
        let (tw, th) = measure_with(font, &t, size);
        draw_text_with(pm, font, &t, bar.x + (bar.w - tw) / 2.0, cy - th / 2.0, size, fg);
    }
}

fn apply_redaction(pm: &mut Pixmap, l: &Layout, r: &R, kind: RedactStyle) {
    let Some(area) = l.rect_to_canvas(r).intersect(&l.image) else { return };
    let x0 = area.x.floor().max(0.0) as usize;
    let y0 = area.y.floor().max(0.0) as usize;
    let x1 = (area.right().ceil() as usize).min(pm.width() as usize);
    let y1 = (area.bottom().ceil() as usize).min(pm.height() as usize);
    if x1 <= x0 + 1 || y1 <= y0 + 1 {
        return;
    }
    let (rw, rh) = (x1 - x0, y1 - y0);
    let pw = pm.width() as usize;
    let data = pm.data_mut();
    let mut region = vec![0u8; rw * rh * 4];
    for y in 0..rh {
        let s = ((y0 + y) * pw + x0) * 4;
        region[y * rw * 4..(y + 1) * rw * 4].copy_from_slice(&data[s..s + rw * 4]);
    }
    match kind {
        RedactStyle::Solid => {
            for px in region.chunks_exact_mut(4) {
                px.copy_from_slice(&[24, 24, 27, 255]);
            }
        }
        RedactStyle::Pixelate => {
            let block = ((10.0 * l.unit).round() as usize).max(4);
            pixelate(&mut region, rw, rh, block);
        }
        RedactStyle::Blur => {
            // Pixelate first so that the blur cannot be reversed, then smooth it out.
            let block = ((6.0 * l.unit).round() as usize).max(3);
            pixelate(&mut region, rw, rh, block);
            blur_rgba(&mut region, rw, rh, ((8.0 * l.unit).round() as usize).max(2));
        }
    }
    for y in 0..rh {
        let s = ((y0 + y) * pw + x0) * 4;
        data[s..s + rw * 4].copy_from_slice(&region[y * rw * 4..(y + 1) * rw * 4]);
    }
}

fn pixelate(data: &mut [u8], w: usize, h: usize, block: usize) {
    for by in (0..h).step_by(block) {
        for bx in (0..w).step_by(block) {
            let (ex, ey) = ((bx + block).min(w), (by + block).min(h));
            let mut sum = [0u32; 4];
            for y in by..ey {
                for x in bx..ex {
                    let i = (y * w + x) * 4;
                    for c in 0..4 {
                        sum[c] += data[i + c] as u32;
                    }
                }
            }
            let n = ((ex - bx) * (ey - by)) as u32;
            let avg = sum.map(|s| (s / n) as u8);
            for y in by..ey {
                for x in bx..ex {
                    let i = (y * w + x) * 4;
                    data[i..i + 4].copy_from_slice(&avg);
                }
            }
        }
    }
}

/// Draws all non-redaction annotations on top of a base render.
pub fn render_annotations(pm: &mut Pixmap, l: &Layout, annotations: &[Annotation]) {
    for a in annotations.iter().filter(|a| !a.is_redaction()) {
        draw_annotation(pm, l, a);
    }
}

fn contrast(c: Color) -> Color {
    if c.luma() > 0.65 {
        Color::rgb(0x111111)
    } else {
        Color::WHITE
    }
}

fn draw_annotation(pm: &mut Pixmap, l: &Layout, a: &Annotation) {
    let s = l.scale;
    let w = (a.width * s).max(1.0);
    let paint = solid(a.color);
    let stroke = Stroke {
        width: w,
        line_cap: LineCap::Round,
        line_join: LineJoin::Round,
        ..Default::default()
    };
    let id = Transform::identity();
    match &a.shape {
        Shape::Arrow { from, to } => {
            let (p0, p1) = (l.to_canvas(*from), l.to_canvas(*to));
            if let Some(path) = arrow_path(p0, p1, w) {
                // Soft contrast outline keeps arrows readable on any background.
                let outline = Stroke {
                    width: (w * 0.35).max(1.0),
                    line_join: LineJoin::Round,
                    ..Default::default()
                };
                pm.stroke_path(&path, &solid(contrast(a.color).with_alpha(110)), &outline, id, None);
                pm.fill_path(&path, &paint, FillRule::Winding, id, None);
            }
        }
        Shape::Line { from, to } => {
            let (p0, p1) = (l.to_canvas(*from), l.to_canvas(*to));
            let mut pb = PathBuilder::new();
            pb.move_to(p0.x, p0.y);
            pb.line_to(p1.x, p1.y);
            if let Some(p) = pb.finish() {
                pm.stroke_path(&p, &paint, &stroke, id, None);
            }
        }
        Shape::Rect { rect: r, filled } => {
            let cr = l.rect_to_canvas(r);
            if let Some(p) = rounded_rect(&cr, w * 0.9) {
                if *filled {
                    pm.fill_path(&p, &paint, FillRule::Winding, id, None);
                } else {
                    pm.stroke_path(&p, &paint, &stroke, id, None);
                }
            }
        }
        Shape::Ellipse { rect: r, filled } => {
            if let Some(p) = rect(&l.rect_to_canvas(r)).and_then(PathBuilder::from_oval) {
                if *filled {
                    pm.fill_path(&p, &paint, FillRule::Winding, id, None);
                } else {
                    pm.stroke_path(&p, &paint, &stroke, id, None);
                }
            }
        }
        Shape::Highlight { rect: r } => {
            if let Some(rr) = rect(&l.rect_to_canvas(r)) {
                // Translucent so it reads on both light and dark UIs.
                pm.fill_rect(rr, &solid(a.color.with_alpha(110)), id, None);
            }
        }
        Shape::Pen { points } => {
            if points.len() < 2 {
                return;
            }
            let pts: Vec<Pt> = points.iter().map(|p| l.to_canvas(*p)).collect();
            let mut pb = PathBuilder::new();
            pb.move_to(pts[0].x, pts[0].y);
            for win in pts.windows(2).skip(1) {
                let mid = pt((win[0].x + win[1].x) / 2.0, (win[0].y + win[1].y) / 2.0);
                pb.quad_to(win[0].x, win[0].y, mid.x, mid.y);
            }
            let last = pts[pts.len() - 1];
            pb.line_to(last.x, last.y);
            if let Some(p) = pb.finish() {
                pm.stroke_path(&p, &paint, &stroke, id, None);
            }
        }
        Shape::Text {
            pos,
            text,
            size,
            style,
        } => {
            if text.is_empty() {
                return;
            }
            let p = l.to_canvas(*pos);
            let size = size * s;
            let font = BOLD.as_ref();
            match style {
                TextStyle::Plain => draw_text_with(pm, font, text, p.x, p.y, size, a.color),
                TextStyle::Outline => {
                    let o = (size * 0.07).max(1.0);
                    let oc = contrast(a.color);
                    for (dx, dy) in [(-o, 0.0), (o, 0.0), (0.0, -o), (0.0, o), (-o, -o), (o, o), (-o, o), (o, -o)] {
                        draw_text_with(pm, font, text, p.x + dx, p.y + dy, size, oc);
                    }
                    draw_text_with(pm, font, text, p.x, p.y, size, a.color);
                }
                TextStyle::Pill => {
                    let (tw, th) = measure_with(font, text, size);
                    let pad = size * 0.35;
                    let bg = R {
                        x: p.x - pad,
                        y: p.y - pad * 0.6,
                        w: tw + pad * 2.0,
                        h: th + pad * 1.2,
                    };
                    if let Some(path) = rounded_rect(&bg, size * 0.4) {
                        pm.fill_path(&path, &paint, FillRule::Winding, id, None);
                    }
                    draw_text_with(pm, font, text, p.x, p.y, size, contrast(a.color));
                }
            }
        }
        Shape::Counter { center, n, size } => {
            let c = l.to_canvas(*center);
            let r = size * s;
            if let Some(p) = PathBuilder::from_circle(c.x, c.y, r) {
                pm.fill_path(&p, &paint, FillRule::Winding, id, None);
                let ring = Stroke {
                    width: (r * 0.12).max(1.0),
                    ..Default::default()
                };
                pm.stroke_path(&p, &solid(Color::WHITE.with_alpha(230)), &ring, id, None);
            }
            let label = n.to_string();
            let fs = r * 1.05;
            let (tw, th) = measure_text(&label, fs);
            draw_text_with(pm, BOLD.as_ref(), &label, c.x - tw / 2.0, c.y - th / 2.0, fs, contrast(a.color));
        }
        Shape::Spotlight { rect: r } => {
            let hole = l.rect_to_canvas(r);
            let mut pb = PathBuilder::new();
            pb.push_rect(rect(&l.image).unwrap());
            if let Some(h) = rect(&hole) {
                pb.push_rect(h);
            }
            if let Some(p) = pb.finish() {
                pm.fill_path(&p, &solid(Color(0x0000008C)), FillRule::EvenOdd, id, None);
            }
        }
        Shape::Redact { .. } => {}
    }
}

/// A tapered arrow with a sharp head.
fn arrow_path(a: Pt, b: Pt, w: f32) -> Option<Path> {
    let len = a.dist(b);
    if len < 1.0 {
        return None;
    }
    let (dx, dy) = ((b.x - a.x) / len, (b.y - a.y) / len);
    let (nx, ny) = (-dy, dx);
    let head_len = (w * 4.2).min(len * 0.6);
    let head_w = w * 3.4;
    let hb = pt(b.x - dx * head_len, b.y - dy * head_len);
    let tail = w * 0.3;
    let neck = w * 1.0;
    let mut pb = PathBuilder::new();
    pb.move_to(a.x + nx * tail / 2.0, a.y + ny * tail / 2.0);
    pb.line_to(hb.x + nx * neck / 2.0, hb.y + ny * neck / 2.0);
    pb.line_to(hb.x + nx * head_w / 2.0 - dx * w * 0.6, hb.y + ny * head_w / 2.0 - dy * w * 0.6);
    pb.line_to(b.x, b.y);
    pb.line_to(hb.x - nx * head_w / 2.0 - dx * w * 0.6, hb.y - ny * head_w / 2.0 - dy * w * 0.6);
    pb.line_to(hb.x - nx * neck / 2.0, hb.y - ny * neck / 2.0);
    pb.line_to(a.x - nx * tail / 2.0, a.y - ny * tail / 2.0);
    pb.close();
    pb.finish()
}

/// Small blurred version of the screenshot for blurred backgrounds.
pub fn make_blur_bg(img: &image::RgbaImage) -> Pixmap {
    let small = image::imageops::thumbnail(img, (img.width() / 12).max(8), (img.height() / 12).max(8));
    let mut pm = pixmap_from_rgba(&small);
    let (w, h) = (pm.width() as usize, pm.height() as usize);
    blur_rgba(pm.data_mut(), w, h, (w.max(h) / 20).max(2));
    pm
}
