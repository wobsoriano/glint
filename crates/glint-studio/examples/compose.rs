use glint_media::input::{Click, CursorSprite, InputTimeline, KeyPress, MouseButton, PointerSample};
use glint_render::render::{draw_text, rgba_from_pixmap};
use glint_render::style::Color;
use glint_studio::{
    compose_frame, compose_frame_motion_blur, AspectMode, AspectPreset, Assets, CaptionWord,
    FrameInputs, Project,
};
use image::{Rgba, RgbaImage};
use std::path::PathBuf;
use std::time::Instant;
use tiny_skia::{FillRule, Paint, PathBuilder, Pixmap, Stroke, Transform};

const SRC: (u32, u32) = (2560, 1440);
const DURATION: f64 = 8_000.0;

fn screen() -> RgbaImage {
    let mut img = RgbaImage::from_fn(SRC.0, SRC.1, |x, y| {
        if x % 160 == 0 || y % 160 == 0 {
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
    block(1640, 320, 320, 160, [52, 120, 246]);
    block(560, 1020, 300, 160, [234, 88, 12]);
    block(120, 120, 900, 60, [40, 40, 48]);
    let mut pm = glint_render::render::pixmap_from_rgba(&img);
    draw_text(&mut pm, "Save", 1745.0, 375.0, 48.0, Color::WHITE);
    draw_text(&mut pm, "Export", 630.0, 1075.0, 48.0, Color::WHITE);
    draw_text(&mut pm, "Synthetic screen 2560x1440", 140.0, 128.0, 36.0, Color::WHITE);
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
    RgbaImage::from_fn(640, 480, |x, y| {
        let (dx, dy) = (x as f32 - 320.0, y as f32 - 220.0);
        if dx * dx / 1.0 + dy * dy * 1.3 < 130.0 * 130.0 {
            Rgba([236, 188, 150, 255])
        } else if y > 360 && (x as f32 - 320.0).abs() < 200.0 {
            Rgba([60, 90, 160, 255])
        } else {
            Rgba([(60 + y / 6) as u8, 110, (140 + x / 8) as u8, 255])
        }
    })
}

fn pointer() -> Vec<PointerSample> {
    let keys = [
        (0.0, 400.0, 300.0),
        (600.0, 450.0, 320.0),
        (1_800.0, 1_800.0, 400.0),
        (2_600.0, 1_820.0, 420.0),
        (4_400.0, 700.0, 1_100.0),
        (6_000.0, 720.0, 1_090.0),
        (8_000.0, 1_400.0, 800.0),
    ];
    (0..=500)
        .map(|i| {
            let t = i as f64 * 16.0;
            let k = keys.windows(2).find(|w| t <= w[1].0).unwrap_or(&keys[keys.len() - 2..]);
            let f = ((t - k[0].0) / (k[1].0 - k[0].0)).clamp(0.0, 1.0);
            let e = f * f * (3.0 - 2.0 * f);
            let x = k[0].1 + (k[1].1 - k[0].1) * e;
            let y = k[0].2 + (k[1].2 - k[0].2) * e;
            PointerSample(t as u64, x as f32, y as f32, 0)
        })
        .collect()
}

fn main() -> anyhow::Result<()> {
    let out_dir = PathBuf::from(std::env::args().nth(1).unwrap_or_else(|| "compose-out".into()));
    let session = out_dir.join("session.glintrec");
    std::fs::create_dir_all(session.join("cursors"))?;
    cursor_png().save(session.join("cursors/0.png"))?;
    let input = InputTimeline {
        version: 1,
        size: [SRC.0, SRC.1],
        pointer: pointer(),
        clicks: vec![
            Click { t: 2_000, up: 2_120, x: 1_800.0, y: 400.0, button: MouseButton::Left },
            Click { t: 5_000, up: 5_100, x: 700.0, y: 1_100.0, button: MouseButton::Left },
        ],
        keys: vec![KeyPress { t: 3_000, label: "Ctrl+S".into() }],
        cursors: vec![CursorSprite { id: 0, file: "cursors/0.png".into(), hotspot: [3, 3], scale: 1.5 }],
    };
    input.save(&session)?;

    let word = |text: &str, s: f64, e: f64| CaptionWord { text: text.into(), start_ms: s, end_ms: e };
    let captions = vec![
        word("Click", 1_500.0, 1_800.0),
        word("Save", 1_800.0, 2_200.0),
        word("to", 2_200.0, 2_350.0),
        word("keep", 2_350.0, 2_700.0),
        word("your", 2_700.0, 2_900.0),
        word("work", 2_900.0, 3_300.0),
        word("Then", 4_600.0, 4_900.0),
        word("export", 4_900.0, 5_400.0),
        word("it", 5_400.0, 5_600.0),
    ];
    let assets = Assets::load(&session, captions)?;

    let mut project = Project::new(DURATION);
    project.apply_auto_zoom(assets.input(), DURATION);
    project.aspect = AspectPreset::Wide16x9;
    project.captions.enabled = true;
    project.captions.karaoke = true;
    project.save(&session)?;
    println!("auto zooms: {:?}", project.zooms.iter().map(|z| (z.start_ms, z.end_ms)).collect::<Vec<_>>());

    let screen = screen();
    let cam = camera();
    let out = project.output_size(SRC);
    println!("output size {out:?}");
    let frame = |p: &Project, t: f64, size| {
        compose_frame(p, &assets, &FrameInputs { screen: &screen, camera: Some(&cam), output_ms: t }, size)
    };
    for t in [0.0, 1_900.0, 2_060.0, 2_150.0, 3_150.0, 4_000.0, 5_150.0, 7_900.0] {
        let path = out_dir.join(format!("frame_{t:05}.png"));
        frame(&project, t, out).save(&path)?;
        println!("wrote {}", path.display());
    }

    let mut vertical = project.clone();
    vertical.aspect = AspectPreset::Vertical9x16;
    vertical.aspect_mode = AspectMode::Fill;
    for t in [500.0, 3_150.0, 7_900.0] {
        let size = vertical.output_size(SRC);
        let path = out_dir.join(format!("vertical_{t:05}.png"));
        frame(&vertical, t, size).save(&path)?;
        println!("wrote {}", path.display());
    }

    let blurred = compose_frame_motion_blur(
        &project,
        &assets,
        &FrameInputs { screen: &screen, camera: Some(&cam), output_ms: 4_000.0 },
        out,
    );
    blurred.save(out_dir.join("blur_04000.png"))?;

    let times: Vec<f64> = (0..480).map(|i| i as f64 * 1000.0 / 60.0).collect();
    for (label, blur) in [("plain", false), ("motion blur", true)] {
        let mut ms: Vec<f64> = times
            .iter()
            .map(|&t| {
                let inputs = FrameInputs { screen: &screen, camera: Some(&cam), output_ms: t };
                let start = Instant::now();
                let img = if blur {
                    compose_frame_motion_blur(&project, &assets, &inputs, out)
                } else {
                    compose_frame(&project, &assets, &inputs, out)
                };
                std::hint::black_box(img);
                start.elapsed().as_secs_f64() * 1000.0
            })
            .collect();
        ms.sort_by(f64::total_cmp);
        let mean = ms.iter().sum::<f64>() / ms.len() as f64;
        println!(
            "{label}: {} frames {}x{}, mean {mean:.2} ms, p50 {:.2} ms, p95 {:.2} ms, max {:.2} ms",
            ms.len(),
            out.0,
            out.1,
            ms[ms.len() / 2],
            ms[ms.len() * 95 / 100],
            ms[ms.len() - 1]
        );
    }
    Ok(())
}
