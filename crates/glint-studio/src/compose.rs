//! The frame compositor: background, the zoomed screen card, cursor, click highlights,
//! keystroke captions, camera bubble and spoken captions, drawn with tiny-skia.
//!
//! Background and shadows are drawn once per output size and design into a cached stage,
//! and motion is precomputed once per project state, so a frame costs one copy of the stage
//! plus the moving layers.

use crate::motion::Motion;
use crate::overlays::CaptionWord;
use crate::project::{AspectPreset, Project};
use crate::zoom::Viewport;
use anyhow::Context;
use glint_media::input::InputTimeline;
use glint_render::annotate::R;
use glint_render::render::{
    draw_shadow, draw_text, make_blur_bg, measure_text, paint_background, pixmap_from_rgba,
    rounded_rect,
};
use glint_render::style::{Background, Color};
use image::RgbaImage;
use std::collections::HashMap;
use std::path::Path;
use std::sync::{Arc, Mutex, OnceLock};
use tiny_skia::{
    FillRule, FilterQuality, Mask, Paint, PathBuilder, Pattern, Pixmap, PixmapPaint, PixmapRef,
    SpreadMode, Stroke, Transform,
};

/// A cursor sprite in screen-master pixels.
pub struct CursorImage {
    pub pixmap: Pixmap,
    pub hotspot: [u32; 2],
}

/// Session data the compositor needs besides the project: the input timeline, cursor
/// sprites and caption words (source time). Also holds the per-project caches.
pub struct Assets {
    input: InputTimeline,
    cursors: HashMap<u32, CursorImage>,
    captions: Vec<CaptionWord>,
    cache: Mutex<Cache>,
}

#[derive(Default)]
struct Cache {
    motion: Option<(String, Arc<Motion>)>,
    stage: Option<(String, Arc<Stage>)>,
}

impl Assets {
    pub fn new(input: InputTimeline, cursors: HashMap<u32, CursorImage>, captions: Vec<CaptionWord>) -> Self {
        Self { input, cursors, captions, cache: Mutex::default() }
    }

    /// Reads `input.json` and the cursor sprites it references from a session directory.
    pub fn load(session_dir: &Path, captions: Vec<CaptionWord>) -> anyhow::Result<Self> {
        let input = InputTimeline::load(session_dir)
            .with_context(|| format!("reading input timeline in {}", session_dir.display()))?;
        let mut cursors = HashMap::new();
        for c in &input.cursors {
            let path = session_dir.join(&c.file);
            let img = image::open(&path)
                .with_context(|| format!("reading cursor {}", path.display()))?
                .to_rgba8();
            cursors.insert(c.id, CursorImage { pixmap: pixmap_from_rgba(&img), hotspot: c.hotspot });
        }
        Ok(Self::new(input, cursors, captions))
    }

    pub fn input(&self) -> &InputTimeline {
        &self.input
    }

    /// Motion for the project, rebuilt only when a field it depends on changed.
    pub fn motion(&self, project: &Project) -> Arc<Motion> {
        let key = serde_json::to_string(&(
            &project.clips,
            &project.zooms,
            project.aspect,
            project.aspect_mode,
            project.crop,
            project.design.padding,
            project.design.cursor.smoothing,
        ))
        .unwrap_or_default();
        let mut cache = self.cache.lock().unwrap_or_else(|e| e.into_inner());
        match &cache.motion {
            Some((k, m)) if *k == key => m.clone(),
            _ => {
                let m = Arc::new(Motion::build(project, &self.input, &self.captions));
                cache.motion = Some((key, m.clone()));
                m
            }
        }
    }

    fn stage(&self, project: &Project, screen: &RgbaImage, has_camera: bool, out: (u32, u32)) -> Arc<Stage> {
        let d = &project.design;
        let key = serde_json::to_string(&(
            out,
            screen.dimensions(),
            has_camera,
            &d.background,
            d.padding,
            d.corner_radius,
            d.shadow,
            d.camera,
            project.aspect,
            project.aspect_mode,
            project.crop,
        ))
        .unwrap_or_default();
        let mut cache = self.cache.lock().unwrap_or_else(|e| e.into_inner());
        match &cache.stage {
            Some((k, s)) if *k == key => s.clone(),
            _ => {
                let s = Arc::new(Stage::build(project, screen, has_camera, out));
                cache.stage = Some((key, s.clone()));
                s
            }
        }
    }
}

pub struct FrameInputs<'a> {
    /// Decoded screen frame at the source time of `output_ms`, opaque RGBA.
    pub screen: &'a RgbaImage,
    /// Decoded camera frame, opaque RGBA.
    pub camera: Option<&'a RgbaImage>,
    pub output_ms: f64,
}

/// Where things sit on the canvas.
struct Layout {
    card: R,
    radius: f32,
    /// Size the cropped screen draws at with no zoom; larger than the card in Fill mode.
    fill_w: f64,
    fill_h: f64,
    bubble: Option<(R, f32)>,
}

fn layout(project: &Project, src: (u32, u32), out: (u32, u32)) -> Layout {
    let d = &project.design;
    let (w, h) = (out.0 as f32, out.1 as f32);
    let short = w.min(h);
    let inset = d.padding.clamp(0.0, 0.475) * short;
    let (aw, ah) = (w - 2.0 * inset, h - 2.0 * inset);
    let content = (src.0 as f32 * project.crop.w) / (src.1 as f32 * project.crop.h).max(1.0);
    let fill_mode = project.aspect != AspectPreset::Original
        && project.aspect_mode == crate::project::AspectMode::Fill;
    let (cw, ch) = if fill_mode { (aw, ah) } else if aw / ah > content { (ah * content, ah) } else { (aw, aw / content) };
    let card = R { x: (w - cw) / 2.0, y: (h - ch) / 2.0, w: cw, h: ch };
    let fill_h = (ch as f64).max(cw as f64 / content as f64);
    let fill_w = fill_h * content as f64;

    let cam = &d.camera;
    let bubble = cam.visible.then(|| {
        let dia = (cam.size * short).max(24.0);
        let margin = (short * 0.03).round();
        let hx = (dia / 2.0 + margin).min(w / 2.0);
        let hy = (dia / 2.0 + margin).min(h / 2.0);
        let cx = (cam.center[0] * w).clamp(hx, w - hx);
        let cy = (cam.center[1] * h).clamp(hy, h - hy);
        (R { x: cx - dia / 2.0, y: cy - dia / 2.0, w: dia, h: dia }, cam.roundness.clamp(0.0, 0.5) * dia)
    });
    Layout { card, radius: d.corner_radius * short, fill_w, fill_h, bubble }
}

/// The static part of a frame for one size and design.
struct Stage {
    layout: Layout,
    base: Pixmap,
    card_mask: Mask,
}

impl Stage {
    fn build(project: &Project, screen: &RgbaImage, has_camera: bool, out: (u32, u32)) -> Self {
        let layout = layout(project, screen.dimensions(), out);
        let mut base = Pixmap::new(out.0, out.1).expect("non-zero output size");
        let bg = &project.design.background;
        let blur = matches!(bg, Background::Blurred).then(|| make_blur_bg(screen));
        let image = match bg {
            Background::Image(p) => image::open(p).ok().map(|i| pixmap_from_rgba(&i.to_rgba8())),
            _ => None,
        };
        paint_background(&mut base, bg, blur.as_ref(), image.as_ref());
        let short = out.0.min(out.1) as f32;
        let shadow = project.design.shadow.clamp(0.0, 1.0);
        if shadow > 0.0 {
            draw_shadow(&mut base, &layout.card, layout.radius, short * 0.035, shadow);
            if let Some((b, r)) = layout.bubble.filter(|_| has_camera) {
                draw_shadow(&mut base, &b, r, short * 0.02, shadow);
            }
        }
        let mut card_mask = Mask::new(out.0, out.1).expect("non-zero output size");
        if let Some(p) = rounded_rect(&layout.card, layout.radius) {
            card_mask.fill_path(&p, FillRule::Winding, true, Transform::identity());
        }
        Self { layout, base, card_mask }
    }
}

/// Maps screen-master pixels to canvas pixels for one camera state.
#[derive(Clone, Copy)]
struct ScreenXf {
    k: f64,
    tx: f64,
    ty: f64,
}

impl ScreenXf {
    fn new(project: &Project, l: &Layout, src: (u32, u32), v: Viewport) -> Self {
        let (dw, dh) = (l.fill_w * v.scale, l.fill_h * v.scale);
        let x0 = (l.card.x + l.card.w / 2.0) as f64 - v.x * dw;
        let y0 = (l.card.y + l.card.h / 2.0) as f64 - v.y * dh;
        let k = dw / (project.crop.w as f64 * src.0 as f64);
        Self {
            k,
            tx: x0 - project.crop.x as f64 * src.0 as f64 * k,
            ty: y0 - project.crop.y as f64 * src.1 as f64 * k,
        }
    }

    fn point(&self, x: f64, y: f64) -> (f32, f32) {
        ((x * self.k + self.tx) as f32, (y * self.k + self.ty) as f32)
    }
}

/// Composes the frame at `inputs.output_ms` without motion blur (preview).
pub fn compose_frame(project: &Project, assets: &Assets, inputs: &FrameInputs, out_size: (u32, u32)) -> RgbaImage {
    compose(project, assets, inputs, out_size, false)
}

/// Composes an export frame: with `export.motion_blur` on and the camera moving, the screen
/// layer is the average of sub-frames spread over a one-frame shutter.
pub fn compose_frame_motion_blur(
    project: &Project,
    assets: &Assets,
    inputs: &FrameInputs,
    out_size: (u32, u32),
) -> RgbaImage {
    compose(project, assets, inputs, out_size, project.export.motion_blur)
}

const MAX_SUBFRAMES: usize = 8;

fn compose(project: &Project, assets: &Assets, inputs: &FrameInputs, out: (u32, u32), blur: bool) -> RgbaImage {
    let motion = assets.motion(project);
    let stage = assets.stage(project, inputs.screen, inputs.camera.is_some(), out);
    let l = &stage.layout;
    let src = inputs.screen.dimensions();
    let t = inputs.output_ms;
    let xf_at = |t: f64| ScreenXf::new(project, l, src, motion.viewport(t));
    let xf = xf_at(t);

    let mut pm = canvas_from(&stage.base);
    let samples = if blur { shutter_samples(project, t, &xf_at, l) } else { vec![xf] };
    for (i, s) in samples.iter().enumerate() {
        // Drawing sample i at 1/(i+1) keeps the result the running average.
        draw_screen(&mut pm, &stage.card_mask, &l.card, inputs.screen, *s, 256 / (i as u32 + 1));
    }

    // Overlays inside the card do not grow with zoom: they scale with the unzoomed card.
    let unzoomed = l.fill_h / (project.crop.h as f64 * src.1 as f64);
    if project.click_highlights {
        let base = l.fill_h * 21.0 / 1080.0 * project.design.cursor.size as f64;
        for c in motion.clicks.at(t, base) {
            let (cx, cy) = xf.point(c.x, c.y);
            draw_click(&mut pm, cx, cy, &c, &stage.card_mask);
        }
    }
    if project.design.cursor.visible {
        if let Some(p) = motion.pointer.at(t) {
            let (cx, cy) = xf.point(p.x, p.y);
            let s = (unzoomed * project.design.cursor.size as f64 * p.press_scale) as f32;
            let sprite = assets.cursors.get(&p.cursor).unwrap_or_else(|| fallback_cursor());
            let tf = Transform::from_translate(cx, cy)
                .pre_scale(s, s)
                .pre_translate(-(sprite.hotspot[0] as f32), -(sprite.hotspot[1] as f32));
            let paint = PixmapPaint { quality: FilterQuality::Bicubic, ..Default::default() };
            pm.draw_pixmap(0, 0, sprite.pixmap.as_ref(), &paint, tf, Some(&stage.card_mask));
        }
    }
    if let (Some((b, r)), Some(cam)) = (l.bubble, inputs.camera) {
        draw_camera(&mut pm, &b, r, cam);
    }
    let caption_bar = match motion.captions.at(t) {
        Some(c) if project.captions.enabled => Some(draw_captions(&mut pm, project, &c)),
        _ => None,
    };
    if project.keystrokes.enabled {
        if let Some(k) = motion.keys.at(t) {
            draw_keystroke(&mut pm, &l.card, project.keystrokes.position, &k, caption_bar);
        }
    }
    rgba_from_opaque_pixmap(pm)
}

/// Bilinear blit of the opaque screen frame through `xf`, clipped by the card mask and mixed
/// over what is there with `weight`/256. tiny-skia's general pattern pipeline costs about
/// 19 ms for a 1080p card; this scale-and-translate special case splits rows across threads.
fn draw_screen(pm: &mut Pixmap, mask: &Mask, card: &R, src: &RgbaImage, xf: ScreenXf, weight: u32) {
    let (pw, ph) = (pm.width() as usize, pm.height() as usize);
    let x0 = (card.x.floor().max(0.0) as usize).min(pw);
    let x1 = (card.right().ceil().max(0.0) as usize).min(pw);
    let y0 = (card.y.floor().max(0.0) as usize).min(ph);
    let y1 = (card.bottom().ceil().max(0.0) as usize).min(ph);
    if x0 >= x1 || y0 >= y1 {
        return;
    }
    let (sw, sh) = (src.width() as usize, src.height() as usize);
    let src = src.as_raw();
    let inv = 1.0 / xf.k;
    // Source column pair and 8-bit fraction for each destination column.
    let cols: Vec<(usize, usize, u32)> = (x0..x1)
        .map(|x| {
            let sx = ((x as f64 + 0.5 - xf.tx) * inv - 0.5).clamp(0.0, (sw - 1) as f64);
            let i = sx as usize;
            (i * 4, (i + 1).min(sw - 1) * 4, ((sx - i as f64) * 256.0) as u32)
        })
        .collect();
    let mask = mask.data();
    let row = |y: usize, out: &mut [u8]| {
        let sy = ((y as f64 + 0.5 - xf.ty) * inv - 0.5).clamp(0.0, (sh - 1) as f64);
        let iy = sy as usize;
        let fy = ((sy - iy as f64) * 256.0) as u32;
        let top = &src[iy * sw * 4..][..sw * 4];
        let bot = &src[(iy + 1).min(sh - 1) * sw * 4..][..sw * 4];
        let mrow = &mask[y * pw..][..pw];
        for (j, &(a, b, fx)) in cols.iter().enumerate() {
            let x = x0 + j;
            let m = mrow[x] as u32;
            if m == 0 {
                continue;
            }
            let alpha = if m == 255 { weight } else { (m * weight + 127) / 255 };
            let d = &mut out[x * 4..x * 4 + 4];
            for c in 0..4 {
                let t = top[a + c] as u32 * (256 - fx) + top[b + c] as u32 * fx;
                let u = bot[a + c] as u32 * (256 - fx) + bot[b + c] as u32 * fx;
                let v = (t * (256 - fy) + u * fy + (1 << 15)) >> 16;
                let o = d[c] as u32;
                d[c] = ((o * (256 - alpha) + v * alpha + 128) >> 8) as u8;
            }
        }
    };
    let stride = pw * 4;
    let rows = &mut pm.data_mut()[y0 * stride..y1 * stride];
    let threads = std::thread::available_parallelism().map_or(1, |n| n.get()).min(8);
    let per = (y1 - y0).div_ceil(threads);
    std::thread::scope(|scope| {
        for (ci, chunk) in rows.chunks_mut(per * stride).enumerate() {
            let row = &row;
            scope.spawn(move || {
                for (ri, out) in chunk.chunks_mut(stride).enumerate() {
                    row(y0 + ci * per + ri, out);
                }
            });
        }
    });
}

/// Camera states across a one-frame shutter centred on `t`. A settled camera gets one
/// sample; a moving one gets roughly one sample per two pixels of travel, capped.
fn shutter_samples(project: &Project, t: f64, xf_at: &dyn Fn(f64) -> ScreenXf, l: &Layout) -> Vec<ScreenXf> {
    let shutter = 1000.0 / project.export.fps.value() as f64;
    let corners = |x: ScreenXf| {
        let (a, b) = (x.tx, x.ty);
        let (c, d) = (x.tx + l.fill_w * x.k, x.ty + l.fill_h * x.k);
        [a, b, c, d]
    };
    let (a, b) = (corners(xf_at(t - shutter / 2.0)), corners(xf_at(t + shutter / 2.0)));
    let travel = a.iter().zip(&b).map(|(p, q)| (p - q).abs()).fold(0.0, f64::max);
    let n = if travel > 1.5 { ((travel / 2.0).ceil() as usize).clamp(2, MAX_SUBFRAMES) } else { 1 };
    (0..n)
        .map(|i| xf_at(t - shutter / 2.0 + shutter * (i as f64 + 0.5) / n as f64))
        .collect()
}

/// White marks over a faint dark halo, so a click reads on light and dark content alike.
fn draw_click(pm: &mut Pixmap, x: f32, y: f32, c: &crate::overlays::ClickFrame, mask: &Mask) {
    let color = |base: Color, a: f64| {
        let mut p = Paint { anti_alias: true, ..Default::default() };
        p.set_color(base.with_alpha(alpha(a)).to_skia());
        p
    };
    let id = Transform::identity();
    if c.impact_alpha > 0.0 {
        if let Some(p) = PathBuilder::from_circle(x, y, c.impact_radius as f32) {
            let halo = Stroke { width: (c.impact_radius * 0.25).max(1.0) as f32, ..Default::default() };
            pm.stroke_path(&p, &color(Color::BLACK, c.impact_alpha * 0.4), &halo, id, Some(mask));
            pm.fill_path(&p, &color(Color::WHITE, c.impact_alpha), FillRule::Winding, id, Some(mask));
        }
    }
    if c.ring_alpha > 0.0 {
        if let Some(p) = PathBuilder::from_circle(x, y, c.ring_radius as f32) {
            let w = c.ring_width as f32;
            let halo = Stroke { width: w * 2.2, ..Default::default() };
            pm.stroke_path(&p, &color(Color::BLACK, c.ring_alpha * 0.3), &halo, id, Some(mask));
            pm.stroke_path(&p, &color(Color::WHITE, c.ring_alpha), &Stroke { width: w, ..Default::default() }, id, Some(mask));
        }
    }
}

fn fill_rounded(pm: &mut Pixmap, r: &R, radius: f32, color: Color) {
    if let Some(p) = rounded_rect(r, radius) {
        let mut paint = Paint { anti_alias: true, ..Default::default() };
        paint.set_color(color.to_skia());
        pm.fill_path(&p, &paint, FillRule::Winding, Transform::identity(), None);
    }
}

fn alpha(a: f64) -> u8 {
    (a.clamp(0.0, 1.0) * 255.0).round() as u8
}

/// A pill pinned to one of six spots on the card edge, unaffected by zoom. It steps clear
/// of the caption bar instead of covering it.
fn draw_keystroke(
    pm: &mut Pixmap,
    card: &R,
    pos: crate::project::KeystrokePosition,
    k: &crate::overlays::KeyFrame,
    caption_bar: Option<R>,
) {
    use crate::project::KeystrokePosition::*;
    let short = card.w.min(card.h);
    let font = (short * 0.033).max(11.0) * k.scale as f32;
    let (pad_x, pad_y) = (font * 0.72, font * 0.46);
    let margin = short * 0.045;
    let (mw, th) = if k.modifiers.is_empty() { (0.0, font * 1.25) } else { measure_text(&k.modifiers, font) };
    let (kw, _) = measure_text(&k.key, font);
    let (w, h) = (mw + kw + 2.0 * pad_x, th + 2.0 * pad_y);
    let x = match pos {
        TopLeft | BottomLeft => card.x + margin,
        TopCenter | BottomCenter => card.x + (card.w - w) / 2.0,
        TopRight | BottomRight => card.right() - margin - w,
    };
    let mut y = match pos {
        TopLeft | TopCenter | TopRight => card.y + margin,
        _ => card.bottom() - margin - h,
    };
    if let Some(bar) = caption_bar {
        if x < bar.right() && bar.x < x + w && y < bar.bottom() && bar.y < y + h {
            y = match pos {
                TopLeft | TopCenter | TopRight => bar.bottom() + margin / 2.0,
                _ => bar.y - margin / 2.0 - h,
            };
        }
    }
    fill_rounded(pm, &R { x, y, w, h }, font * 0.55, Color::BLACK.with_alpha(alpha(0.62 * k.opacity)));
    if !k.modifiers.is_empty() {
        draw_text(pm, &k.modifiers, x + pad_x, y + pad_y, font, Color::WHITE.with_alpha(alpha(0.7 * k.opacity)));
    }
    draw_text(pm, &k.key, x + pad_x + mw, y + pad_y, font, Color::WHITE.with_alpha(alpha(k.opacity)));
}

fn draw_camera(pm: &mut Pixmap, b: &R, radius: f32, cam: &RgbaImage) {
    let (cw, ch) = (cam.width() as f32, cam.height() as f32);
    let (Some(path), Some(src)) = (rounded_rect(b, radius), PixmapRef::from_bytes(cam.as_raw(), cam.width(), cam.height())) else {
        return;
    };
    let s = (b.w / cw).max(b.h / ch);
    let tf = Transform::from_row(s, 0.0, 0.0, s, b.x + (b.w - cw * s) / 2.0, b.y + (b.h - ch * s) / 2.0);
    let paint = Paint {
        shader: Pattern::new(src, SpreadMode::Pad, FilterQuality::Bilinear, 1.0, tf),
        anti_alias: true,
        ..Default::default()
    };
    pm.fill_path(&path, &paint, FillRule::Winding, Transform::identity(), None);
}

const KARAOKE: Color = Color::rgb(0xFFD60A);
const CAPTION_MAX_LINES: usize = 3;

/// A centred bar on the canvas; long text wraps, and shrinks past three lines.
/// Returns the bar's rect.
fn draw_captions(pm: &mut Pixmap, project: &Project, c: &crate::overlays::CaptionFrame) -> R {
    let (w, h) = (pm.width() as f32, pm.height() as f32);
    let s = &project.captions;
    let mut font = (w.min(h) * 0.032 * s.size.clamp(0.6, 1.8)).max(11.0);
    let mut lines;
    let mut widths;
    loop {
        let pad_x = font * 0.85;
        let max_w = w * 0.92 - 2.0 * pad_x;
        let space = measure_text(" ", font).0;
        widths = c.words.iter().map(|wd| measure_text(wd, font).0).collect::<Vec<_>>();
        lines = vec![(0usize, 0usize, 0.0f32)];
        for (i, &ww) in widths.iter().enumerate() {
            let last = lines.last_mut().unwrap();
            if last.1 > last.0 && last.2 + space + ww > max_w {
                lines.push((i, i + 1, ww));
            } else {
                last.2 += if last.1 > last.0 { space + ww } else { ww };
                last.1 = i + 1;
            }
        }
        if lines.len() <= CAPTION_MAX_LINES || font <= 11.0 {
            break;
        }
        font = (font * CAPTION_MAX_LINES as f32 / lines.len() as f32).max(11.0);
    }
    let (pad_x, pad_y, line_h) = (font * 0.85, font * 0.5, font * 1.25);
    let space = measure_text(" ", font).0;
    let widest = lines.iter().map(|l| l.2).fold(0.0, f32::max);
    let (bw, bh) = (widest + 2.0 * pad_x, line_h * lines.len() as f32 + 2.0 * pad_y);
    let bar = R { x: (w - bw) / 2.0, y: h * s.position.clamp(0.1, 0.95) - bh / 2.0, w: bw, h: bh };
    fill_rounded(pm, &bar, (font * 0.6).min(bh / 2.0), Color::BLACK.with_alpha(alpha(0.78)));
    for (li, &(a, b, lw)) in lines.iter().enumerate() {
        let mut x = (w - lw) / 2.0;
        let y = bar.y + pad_y + li as f32 * line_h;
        for i in a..b {
            let color = if !s.karaoke {
                Color::WHITE
            } else if Some(i) == c.active {
                KARAOKE
            } else if i < c.spoken {
                Color::WHITE
            } else {
                Color::WHITE.with_alpha(alpha(0.55))
            };
            draw_text(pm, c.words[i], x, y, font, color);
            x += widths[i] + space;
        }
    }
    bar
}

/// Arrow drawn when a recording has no sprite for a cursor id.
fn fallback_cursor() -> &'static CursorImage {
    static ARROW: OnceLock<CursorImage> = OnceLock::new();
    ARROW.get_or_init(|| {
        let mut pm = Pixmap::new(26, 38).unwrap();
        let mut pb = PathBuilder::new();
        for (i, (x, y)) in [(2.0, 2.0), (2.0, 29.0), (9.0, 23.0), (14.0, 34.0), (19.0, 32.0), (14.0, 21.0), (23.0, 21.0)]
            .into_iter()
            .enumerate()
        {
            if i == 0 { pb.move_to(x, y) } else { pb.line_to(x, y) }
        }
        pb.close();
        let path = pb.finish().unwrap();
        let mut paint = Paint { anti_alias: true, ..Default::default() };
        paint.set_color(Color::BLACK.to_skia());
        pm.fill_path(&path, &paint, FillRule::Winding, Transform::identity(), None);
        paint.set_color(Color::WHITE.to_skia());
        pm.stroke_path(&path, &paint, &Stroke { width: 1.8, ..Default::default() }, Transform::identity(), None);
        CursorImage { pixmap: pm, hotspot: [2, 2] }
    })
}

/// Frames handed back by consumers, reused as canvases: a fresh multi-megabyte buffer per
/// frame costs more in page faults than drawing the frame.
static CANVASES: Mutex<Vec<Vec<u8>>> = Mutex::new(Vec::new());
const MAX_POOLED: usize = 8;

/// Returns a finished frame's buffer for reuse by later frames.
pub fn recycle_frame(frame: RgbaImage) {
    let mut pool = CANVASES.lock().unwrap_or_else(|e| e.into_inner());
    if pool.len() < MAX_POOLED {
        pool.push(frame.into_raw());
    }
}

fn canvas_from(base: &Pixmap) -> Pixmap {
    let pooled = CANVASES.lock().unwrap_or_else(|e| e.into_inner()).pop();
    match pooled {
        Some(mut data) => {
            data.clear();
            data.extend_from_slice(base.data());
            let size = tiny_skia::IntSize::from_wh(base.width(), base.height()).expect("non-zero output size");
            Pixmap::from_vec(data, size).expect("buffer matches size")
        }
        None => base.clone(),
    }
}

/// Unpremultiplies in place; opaque pixels, nearly all of them, are left untouched.
fn rgba_from_opaque_pixmap(pm: Pixmap) -> RgbaImage {
    let (w, h) = (pm.width(), pm.height());
    let mut data = pm.take();
    for px in data.chunks_exact_mut(4) {
        let a = px[3] as u32;
        if a != 255 && a != 0 {
            for c in &mut px[..3] {
                *c = ((*c as u32 * 255 + a / 2) / a).min(255) as u8;
            }
        }
    }
    RgbaImage::from_raw(w, h, data).expect("buffer matches size")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::project::Resolution;
    use glint_media::input::{CursorSprite, InputTimeline, PointerSample};

    /// A 4K recording from a 150% display, whose arrow is a 48 px sprite, exported at 1080p
    /// with the pointer resting at (1000, 1000). Returns the drawn cursor's size in canvas px.
    fn cursor_size_at_1080p(multiplier: f32) -> (u32, u32) {
        let input = InputTimeline {
            version: 1,
            size: [3840, 2160],
            pointer: vec![PointerSample(0, 1000.0, 1000.0, 0), PointerSample(5000, 1000.0, 1000.0, 0)],
            cursors: vec![CursorSprite { id: 0, file: "cursors/0.png".into(), hotspot: [0, 0], scale: 1.5 }],
            ..Default::default()
        };
        let mut sprite = Pixmap::new(48, 48).unwrap();
        sprite.fill(tiny_skia::Color::BLACK);
        let cursors = HashMap::from([(0, CursorImage { pixmap: sprite, hotspot: [0, 0] })]);
        let assets = Assets::new(input, cursors, Vec::new());
        let mut project = Project::new(5000.0);
        project.design.padding = 0.0;
        project.design.shadow = 0.0;
        project.design.cursor.size = multiplier;
        project.design.cursor.smoothing = 0.0;
        project.export.resolution = Resolution::P1080;
        let out = project.output_size((3840, 2160));
        assert_eq!(out, (1920, 1080));
        let screen = RgbaImage::from_pixel(3840, 2160, image::Rgba([120, 120, 120, 255]));
        let inputs = FrameInputs { screen: &screen, camera: None, output_ms: 1000.0 };
        let shown = compose_frame(&project, &assets, &inputs, out);
        project.design.cursor.visible = false;
        let hidden = compose_frame(&project, &assets, &inputs, out);
        let (mut x0, mut y0, mut x1, mut y1) = (u32::MAX, u32::MAX, 0, 0);
        for (x, y, p) in shown.enumerate_pixels() {
            if p != hidden.get_pixel(x, y) {
                (x0, y0, x1, y1) = (x0.min(x), y0.min(y), x1.max(x), y1.max(y));
            }
        }
        assert_eq!((x0, y0), (500, 500), "cursor tip follows the content: 1000 source px at half scale");
        (x1 - x0 + 1, y1 - y0 + 1)
    }

    #[test]
    fn cursor_keeps_its_recorded_size_relative_to_the_content() {
        // 48 px over 2160 rows becomes 24 px over 1080 rows, times the project's multiplier.
        assert_eq!(cursor_size_at_1080p(1.0), (24, 24));
        assert_eq!(cursor_size_at_1080p(1.5), (36, 36));
    }
}
