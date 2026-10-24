//! Writes the frames the Go compositor in `internal/studio` is compared against, and the f32
//! spellings serde_json writes, which `internal/render` matches.
//!
//! cargo run --release -p glint-studio --example go_compose -- synthetic <out_dir>
//! cargo run --release -p glint-studio --example go_compose -- real <session_copy> <screen.png> <camera.png|-> <out_dir>
//! cargo run --release -p glint-studio --example go_compose -- f32 <out.json>
//!
//! `synthetic` builds a session with no user data: a drawn screen and camera frame, a scripted
//! pointer, two clicks, a shortcut and caption words. `real` composes a copy of a Library
//! session from screen and camera frames decoded beforehand. Both write `manifest.json`
//! naming each frame's project, time, size and inputs, beside the frames as PNG.

use glint_media::input::{Click, CursorSprite, InputTimeline, KeyPress, MouseButton, PointerSample};
use glint_render::render::{draw_text, rgba_from_pixmap};
use glint_render::style::{Background, Color, Gradient};
use glint_studio::{
    compose_frame, compose_frame_motion_blur, AspectMode, AspectPreset, Assets, CaptionCue, CaptionWord, CropRect, FrameInputs,
    KeystrokePosition, Project, Session, ZoomFocus,
};
use image::{Rgba, RgbaImage};
use serde_json::{json, Value};
use std::path::Path;
use std::time::Instant;
use tiny_skia::{FillRule, Paint, PathBuilder, Pixmap, Stroke, Transform};

fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    match args.first().map(String::as_str) {
        Some("synthetic") => synthetic(Path::new(&args[1])),
        Some("real") => real(Path::new(&args[1]), Path::new(&args[2]), &args[3], Path::new(&args[4])),
        Some("f32") => f32_cases(Path::new(&args[1])),
        _ => anyhow::bail!("usage: go_compose synthetic <out> | real <session> <screen.png> <camera.png|-> <out> | f32 <out.json>"),
    }
}

const SRC: (u32, u32) = (1280, 720);
const DURATION: f64 = 8_000.0;

fn screen() -> RgbaImage {
    let mut img = RgbaImage::from_fn(SRC.0, SRC.1, |x, y| {
        if x % 80 == 0 || y % 80 == 0 {
            Rgba([200, 205, 215, 255])
        } else {
            Rgba([246, 247, 250, 255])
        }
    });
    let mut block = |x0: u32, y0: u32, w: u32, h: u32, c: [u8; 3]| {
        for y in y0..y0 + h {
            for x in x0..x0 + w {
                img.put_pixel(x, y, Rgba([c[0], c[1], c[2], 255]));
            }
        }
    };
    block(820, 160, 160, 80, [52, 120, 246]);
    block(280, 510, 150, 80, [234, 88, 12]);
    block(60, 60, 450, 30, [40, 40, 48]);
    let mut pm = glint_render::render::pixmap_from_rgba(&img);
    draw_text(&mut pm, "Save", 872.0, 187.0, 24.0, Color::WHITE);
    draw_text(&mut pm, "Export", 315.0, 537.0, 24.0, Color::WHITE);
    draw_text(&mut pm, "Synthetic screen 1280x720", 70.0, 64.0, 18.0, Color::WHITE);
    rgba_from_pixmap(&pm)
}

fn cursor_png() -> RgbaImage {
    let mut pm = Pixmap::new(48, 48).unwrap();
    let mut pb = PathBuilder::new();
    pb.move_to(3.0, 3.0);
    for (x, y) in [(3.0, 40.0), (13.0, 31.0), (20.0, 45.0), (27.0, 42.0), (20.0, 29.0), (33.0, 29.0)] {
        pb.line_to(x, y);
    }
    pb.close();
    let path = pb.finish().unwrap();
    let mut paint = Paint { anti_alias: true, ..Default::default() };
    paint.set_color_rgba8(10, 10, 10, 255);
    pm.fill_path(&path, &paint, FillRule::Winding, Transform::identity(), None);
    paint.set_color_rgba8(255, 255, 255, 255);
    pm.stroke_path(&path, &paint, &Stroke { width: 2.5, ..Default::default() }, Transform::identity(), None);
    rgba_from_pixmap(&pm)
}

fn camera() -> RgbaImage {
    RgbaImage::from_fn(320, 240, |x, y| {
        let (dx, dy) = (x as f32 - 160.0, y as f32 - 110.0);
        if dx * dx + dy * dy * 1.3 < 65.0 * 65.0 {
            Rgba([236, 188, 150, 255])
        } else if y > 180 && dx.abs() < 100.0 {
            Rgba([60, 90, 160, 255])
        } else {
            Rgba([(60 + y / 3) as u8, 110, (140 + x / 4) as u8, 255])
        }
    })
}

fn pointer() -> Vec<PointerSample> {
    let keys = [
        (0.0, 200.0, 150.0),
        (600.0, 225.0, 160.0),
        (1_800.0, 900.0, 200.0),
        (2_600.0, 910.0, 210.0),
        (4_400.0, 350.0, 550.0),
        (6_000.0, 360.0, 545.0),
        (8_000.0, 700.0, 400.0),
    ];
    (0..=500)
        .map(|i| {
            let t = i as f64 * 16.0;
            let k = keys.windows(2).find(|w| t <= w[1].0).unwrap_or(&keys[keys.len() - 2..]);
            let f = ((t - k[0].0) / (k[1].0 - k[0].0)).clamp(0.0, 1.0);
            let e = f * f * (3.0 - 2.0 * f);
            let x = k[0].1 + (k[1].1 - k[0].1) * e;
            let y = k[0].2 + (k[1].2 - k[0].2) * e;
            // Past 6.5 s the pointer shows a shape that has no sprite, so the fallback arrow draws.
            PointerSample(t as u64, x as f32, y as f32, if t > 6_500.0 { 7 } else { 0 })
        })
        .collect()
}

fn captions() -> Vec<CaptionWord> {
    let word = |text: &str, s: f64, e: f64| CaptionWord { text: text.into(), start_ms: s, end_ms: e };
    vec![
        word("Click", 1_500.0, 1_800.0),
        word("Save", 1_800.0, 2_200.0),
        word("to", 2_200.0, 2_350.0),
        word("keep", 2_350.0, 2_700.0),
        word("your", 2_700.0, 2_900.0),
        word("work", 2_900.0, 3_300.0),
        word("Then", 4_600.0, 4_900.0),
        word("export", 4_900.0, 5_400.0),
        word("it", 5_400.0, 5_600.0),
    ]
}

struct Frame {
    name: String,
    project: Project,
    t: f64,
    size: (u32, u32),
    camera: bool,
    blur: bool,
}

fn synthetic(out: &Path) -> anyhow::Result<()> {
    let session = out.join("session");
    std::fs::create_dir_all(session.join("cursors"))?;
    cursor_png().save(session.join("cursors/0.png"))?;
    let input = InputTimeline {
        version: 1,
        size: [SRC.0, SRC.1],
        pointer: pointer(),
        clicks: vec![
            Click { t: 2_000, up: 2_120, x: 900.0, y: 200.0, button: MouseButton::Left },
            Click { t: 5_000, up: 5_100, x: 350.0, y: 550.0, button: MouseButton::Left },
        ],
        keys: vec![KeyPress { t: 3_000, label: "Ctrl+Shift+S".into() }],
        cursors: vec![CursorSprite { id: 0, file: "cursors/0.png".into(), hotspot: [3, 3], scale: 1.5 }],
    };
    input.save(&session)?;
    let screen = screen();
    let cam = camera();
    screen.save(out.join("screen.png"))?;
    cam.save(out.join("camera.png"))?;
    let words = captions();
    let assets = Assets::load(&session, words.clone())?;

    let mut base = Project::new(DURATION);
    base.apply_auto_zoom(assets.input(), DURATION);
    base.aspect = AspectPreset::Wide16x9;
    base.captions.enabled = true;
    base.captions.karaoke = true;
    base.caption_cues = vec![
        CaptionCue { start_ms: 1_500.0, end_ms: 3_300.0, text: "Click Save to keep your work".into() },
        CaptionCue { start_ms: 4_600.0, end_ms: 5_600.0, text: "Then export it".into() },
    ];
    let wide = (640, 360);
    let mut frames = Vec::new();
    let mut add = |name: &str, project: &Project, t: f64, size: (u32, u32), camera: bool, blur: bool| {
        frames.push(Frame { name: name.into(), project: project.clone(), t, size, camera, blur })
    };

    add("wide_start", &base, 0.0, wide, true, false);
    add("wide_zoom_click", &base, 2_060.0, wide, true, false);
    add("wide_keys_over_captions", &base, 3_150.0, wide, true, false);
    add("wide_motion_blur", &base, 4_000.0, wide, true, true);

    let mut vertical = base.clone();
    vertical.aspect = AspectPreset::Vertical9x16;
    vertical.aspect_mode = AspectMode::Fill;
    add("vertical_fill", &vertical, 3_150.0, (360, 640), true, false);
    add("vertical_fill_late", &vertical, 7_000.0, (360, 640), true, false);

    let mut cropped = base.clone();
    cropped.aspect = AspectPreset::Original;
    cropped.crop = CropRect { x: 0.1, y: 0.15, w: 0.6, h: 0.7 };
    cropped.design.background = Background::Solid(Color::rgb(0x1F2937));
    cropped.design.padding = 0.1;
    cropped.design.corner_radius = 0.05;
    cropped.keystrokes.position = KeystrokePosition::TopLeft;
    cropped.captions.karaoke = false;
    let size = cropped.output_size(SRC);
    let crop_size = (size.0 * 360 / size.1 / 2 * 2, 360);
    add("crop_click_ring", &cropped, 5_150.0, crop_size, false, false);
    add("crop_keys_top_left", &cropped, 3_100.0, crop_size, false, false);

    let mut square = base.clone();
    square.aspect = AspectPreset::Square;
    square.design.background = Background::Blurred;
    square.design.camera.roundness = 0.2;
    square.design.camera.center = [0.2, 0.25];
    square.design.cursor.size = 2.0;
    square.design.cursor.smoothing = 0.0;
    square.zooms.push(glint_studio::Zoom {
        start_ms: 5_500.0,
        end_ms: 7_500.0,
        scale: 2.5,
        focus: ZoomFocus::Fixed { x: 0.4, y: 0.65 },
        enabled: true,
        auto: false,
    });
    add("square_blurred_raw_cursor", &square, 1_000.0, (480, 480), true, false);
    add("square_fixed_zoom_fallback_cursor", &square, 6_800.0, (480, 480), true, false);

    let mut bare = base.clone();
    bare.aspect = AspectPreset::Portrait4x5;
    bare.design.background = Background::None;
    bare.design.shadow = 0.8;
    bare.design.camera.visible = false;
    bare.click_highlights = false;
    bare.captions.enabled = false;
    bare.keystrokes.enabled = false;
    bare.design.background = Background::Gradient(Gradient { stops: vec![Color::rgb(0x0F2027), Color::rgb(0x2C5364)], angle: 90.0 });
    add("portrait_plain", &bare, 2_060.0, (400, 500), false, false);
    let mut none = bare.clone();
    none.design.background = Background::None;
    none.design.cursor.visible = false;
    add("portrait_no_background", &none, 2_060.0, (400, 500), false, false);

    write_frames(out, "session", &words, true, &frames, &assets, &screen, Some(&cam))
}

fn real(session: &Path, screen_png: &Path, camera_png: &str, out: &Path) -> anyhow::Result<()> {
    std::fs::create_dir_all(out)?;
    let s = Session::open(session)?;
    let screen = image::open(screen_png)?.to_rgba8();
    let cam = (camera_png != "-").then(|| image::open(camera_png).map(|i| i.to_rgba8())).transpose()?;
    let words = s.transcript.as_ref().map(glint_studio::caption_words).unwrap_or_default();
    let src = (screen.width(), screen.height());
    let dur = s.project.duration_ms();
    let mut frames = Vec::new();
    let mut add = |name: String, project: &Project, t: f64, blur: bool| {
        let size = project.output_size(src);
        frames.push(Frame { name, project: project.clone(), t, size, camera: cam.is_some(), blur })
    };
    let mut times: Vec<f64> = (1..=5).map(|i| dur * i as f64 / 6.0).collect();
    let mut clicks: Vec<f64> = s.assets.input().clicks.iter().filter_map(|c| s.project.event_output_time(c.t as f64)).collect();
    clicks.truncate(3);
    times.extend(clicks.iter().map(|t| t + 60.0));
    let keys: Vec<f64> = s.assets.input().keys.iter().filter_map(|k| s.project.event_output_time(k.t as f64)).take(2).collect();
    times.extend(keys.iter().map(|t| t + 200.0));
    for (i, &t) in times.iter().enumerate() {
        add(format!("as_saved_{i:02}"), &s.project, t, false);
    }
    let mut all = s.project.clone();
    all.captions.enabled = true;
    all.captions.karaoke = true;
    all.keystrokes.enabled = true;
    all.click_highlights = true;
    for (i, &t) in times.iter().enumerate() {
        add(format!("overlays_{i:02}"), &all, t, i % 3 == 0);
    }
    let mut vertical = all.clone();
    vertical.aspect = AspectPreset::Vertical9x16;
    vertical.aspect_mode = AspectMode::Fill;
    for (i, &t) in times.iter().take(4).enumerate() {
        add(format!("vertical_{i:02}"), &vertical, t, false);
    }
    let mut cropped = all.clone();
    cropped.crop = CropRect { x: 0.2, y: 0.1, w: 0.55, h: 0.6 };
    cropped.design.background = Background::Blurred;
    for (i, &t) in times.iter().take(3).enumerate() {
        add(format!("cropped_{i:02}"), &cropped, t, false);
    }
    let session_str = session.to_string_lossy().into_owned();
    write_frames(out, &session_str, &words, false, &frames, &s.assets, &screen, cam.as_ref())
}

#[allow(clippy::too_many_arguments)]
fn write_frames(
    out: &Path,
    session: &str,
    words: &[CaptionWord],
    copy_inputs: bool,
    frames: &[Frame],
    assets: &Assets,
    screen: &RgbaImage,
    cam: Option<&RgbaImage>,
) -> anyhow::Result<()> {
    let mut entries = Vec::new();
    for f in frames {
        let inputs = FrameInputs { screen, camera: if f.camera { cam } else { None }, output_ms: f.t };
        let start = Instant::now();
        let img = if f.blur {
            compose_frame_motion_blur(&f.project, assets, &inputs, f.size)
        } else {
            compose_frame(&f.project, assets, &inputs, f.size)
        };
        let ms = start.elapsed().as_secs_f64() * 1000.0;
        img.save(out.join(format!("{}.png", f.name)))?;
        println!("{} {}x{} t={} {:.1} ms", f.name, f.size.0, f.size.1, f.t, ms);
        entries.push(json!({
            "name": f.name,
            "project": serde_json::to_value(&f.project)?,
            "t": f.t,
            "size": [f.size.0, f.size.1],
            "camera": f.camera,
            "motion_blur": f.blur,
        }));
    }
    let manifest = json!({
        "session": session,
        "screen": "screen.png",
        "camera": cam.map(|_| "camera.png"),
        "captions": words.iter().map(|w| json!({ "text": w.text, "start_ms": w.start_ms, "end_ms": w.end_ms })).collect::<Vec<Value>>(),
        "frames": entries,
    });
    if !copy_inputs {
        screen.save(out.join("screen.png"))?;
        if let Some(c) = cam {
            c.save(out.join("camera.png"))?;
        }
    }
    std::fs::write(out.join("manifest.json"), serde_json::to_string_pretty(&manifest)?)?;
    println!("wrote {} frames to {}", frames.len(), out.display());
    bench(frames, assets, screen, cam)
}

fn bench(frames: &[Frame], assets: &Assets, screen: &RgbaImage, cam: Option<&RgbaImage>) -> anyhow::Result<()> {
    let Some(f) = frames.iter().find(|f| f.camera || cam.is_none()) else { return Ok(()) };
    let mut project = f.project.clone();
    project.export.resolution = glint_studio::Resolution::P1080;
    let size = project.output_size((screen.width(), screen.height()));
    let mut ms: Vec<f64> = (0..240)
        .map(|i| {
            let inputs = FrameInputs { screen, camera: cam, output_ms: i as f64 * 1000.0 / 60.0 };
            let start = Instant::now();
            std::hint::black_box(compose_frame(&project, assets, &inputs, size));
            start.elapsed().as_secs_f64() * 1000.0
        })
        .collect();
    ms.sort_by(f64::total_cmp);
    let line = format!(
        "rust compose {}x{}: {} frames, p50 {:.2} ms, p95 {:.2} ms, max {:.2} ms",
        size.0,
        size.1,
        ms.len(),
        ms[ms.len() / 2],
        ms[ms.len() * 95 / 100],
        ms[ms.len() - 1]
    );
    println!("{line}");
    Ok(())
}

fn f32_cases(out: &Path) -> anyhow::Result<()> {
    let mut values: Vec<f32> = vec![
        0.0, -0.0, 1.0, -1.0, 0.45, 0.6, 0.35, 64.0, 48.5, 135.0, -30.0, 22.0, 24.0, 0.1, 0.2, 0.3, 1.5, 2.5,
        1e-6, 1e-7, 1.25e-6, 9.99e-6, 1e-5, 1.5e-5, 0.0001, 0.001, 1e12, 1e13, 1.5e13, 9.9e12, 123456789.0, 16777216.0,
        1e20, 1e30, 3.4028235e38, 1.1754944e-38, 1e-45, 33.333332, 0.06, 0.02, f32::NAN, f32::INFINITY, f32::NEG_INFINITY,
    ];
    let mut x: u32 = 0x9E3779B9;
    for _ in 0..2000 {
        x ^= x << 13;
        x ^= x >> 17;
        x ^= x << 5;
        values.push(f32::from_bits(x));
    }
    let cases: Vec<Value> = values
        .iter()
        .map(|v| json!({ "bits": v.to_bits(), "text": serde_json::to_string(v).unwrap() }))
        .collect();
    std::fs::write(out, serde_json::to_string(&cases)?)?;
    println!("wrote {} f32 cases to {}", cases.len(), out.display());
    Ok(())
}
