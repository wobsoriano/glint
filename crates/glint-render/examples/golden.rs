//! Golden fixtures for the Go port of glint-render (internal/render).
//!
//! cargo run -p glint-render --example golden --release -- write <dir>
//!     Writes base.png, bg.png, one <name>.json + <name>.png per fixture, manifest.json, and
//!     JSON round-trip fixtures under <dir>/json.
//! cargo run -p glint-render --example golden --release -- verify <dir>
//!     Parses the JSON the Go port wrote under <dir>/json/go and checks it equals the originals.
//! cargo run -p glint-render --example golden --release -- bench
//!     Times a 2560x1440 render with a gradient, a shadow and 20 annotations.

use glint_render::annotate::{pt, Annotation, RedactStyle, Shape, TextStyle, R};
use glint_render::compose::{self, Compositor, Sources};
use glint_render::doc::{BlurEdge, BlurMode, Border, EditDoc, Placement, ProgressiveBlur, Watermark};
use glint_render::fonts::{Align, TextFont, Weight};
use glint_render::preset::{self, Preset};
use glint_render::render;
use glint_render::style::{Aspect, Background, Chrome, Color, Gradient, Style, GRADIENTS};
use image::{Rgba, RgbaImage};
use std::path::{Path, PathBuf};

fn base_image(w: u32, h: u32) -> RgbaImage {
    RgbaImage::from_fn(w, h, |x, y| {
        let (fx, fy) = (x as f32 / w as f32, y as f32 / h as f32);
        if y < h / 8 {
            return Rgba([40, 44, 52, 255]);
        }
        if x < w / 5 {
            let band = (y / 18) % 2;
            return if band == 0 { Rgba([236, 238, 242, 255]) } else { Rgba([226, 229, 235, 255]) };
        }
        if fx > 0.55 && fy > 0.45 {
            let r = (255.0 * fx) as u8;
            let g = (180.0 * (1.0 - fy) + 40.0) as u8;
            let b = ((x * 7 + y * 3) % 256) as u8;
            return Rgba([r, g, b, 255]);
        }
        let line = (y - h / 8) % 22;
        if line < 9 && x > w / 4 && x < w / 4 + 40 + (y * 37 % 160) {
            return Rgba([70, 74, 82, 255]);
        }
        if ((x / 12) + (y / 12)) % 2 == 0 && fx > 0.8 && fy < 0.4 {
            return Rgba([255, 204, 0, 255]);
        }
        Rgba([250, 250, 252, 255])
    })
}

fn bg_image() -> RgbaImage {
    RgbaImage::from_fn(640, 400, |x, y| {
        let dx = x as f32 - 320.0;
        let dy = y as f32 - 160.0;
        let d = (dx * dx + dy * dy).sqrt();
        Rgba([(30.0 + d * 0.5).min(255.0) as u8, (90 + (x % 97)) as u8, (200.0 - d * 0.3).max(0.0) as u8, 255])
    })
}

fn ann(id: u64, shape: Shape, color: u32, width: f32) -> Annotation {
    Annotation { id, shape, color: Color::rgb(color), width }
}

fn text(pos: (f32, f32), s: &str, size: f32, style: TextStyle, family: &str, weight: Weight, align: Align) -> Shape {
    Shape::Text { pos: pt(pos.0, pos.1), text: s.into(), size, style, font: TextFont { family: family.into(), weight, align } }
}

struct Fixture {
    name: &'static str,
    doc: EditDoc,
    scale: f32,
    region: Option<R>,
}

fn plain() -> EditDoc {
    EditDoc::new(Style::default(), 1.0, None)
}

fn fixtures() -> Vec<Fixture> {
    let mut out = Vec::new();
    let mut add = |name: &'static str, doc: EditDoc| out.push(Fixture { name, doc, scale: 1.0, region: None });

    let mut d = plain();
    d.style = Style { background: Background::None, padding: 24.0, shadow: 0.0, radius: 0.0, ..Style::default() };
    add("bg_none", d);

    let mut d = plain();
    d.style.background = Background::Solid(Color::rgb(0x93C5FD));
    add("bg_solid", d);

    let mut d = plain();
    d.style.background = Background::Gradient(GRADIENTS[1].1.clone());
    add("bg_gradient_2stop", d);

    let mut d = plain();
    d.style.aspect = Some(Aspect { w: 16.0, h: 9.0 });
    d.style.align = 0;
    add("bg_gradient_3stop_aspect", d);

    let mut d = plain();
    d.style.background = Background::Gradient(GRADIENTS[11].1.clone());
    d.style.aspect = Some(Aspect { w: 1.0, h: 1.0 });
    d.style.align = 8;
    d.style.radius = 24.0;
    add("bg_gradient_sky_square", d);

    let mut d = plain();
    d.style.background = Background::Gradient(Gradient { stops: vec![Color::rgb(0x112233), Color(0xFF000080), Color::rgb(0x00FF88), Color::WHITE], angle: 30.0 });
    add("bg_gradient_4stop_alpha", d);

    let mut d = plain();
    d.style.background = Background::Blurred;
    add("bg_blurred", d);

    let mut d = plain();
    d.style.background = Background::Image("bg.png".into());
    add("bg_image", d);

    let mut d = EditDoc::new(Style { chrome: Chrome::MacLight, inset: 16.0, ..Style::default() }, 1.0, Some("Notes — Glint".into()));
    d.style.background = Background::Solid(Color::rgb(0xF4F4F5));
    add("chrome_mac", d);

    let d = EditDoc::new(Style { chrome: Chrome::WinDark, inset: 10.0, auto_balance: false, ..Style::default() }, 1.0, Some("Windows Terminal".into()));
    add("chrome_win_dark", d);

    let mut d = plain();
    d.style.padding = 32.0;
    d.annotations = vec![
        ann(1, Shape::Rect { rect: R::new(20.0, 40.0, 120.0, 70.0), filled: false }, 0xFF3B30, 4.0),
        ann(2, Shape::Rect { rect: R::new(160.0, 40.0, 60.0, 40.0), filled: true }, 0x007AFF, 3.0),
        ann(3, Shape::Ellipse { rect: R::new(230.0, 30.0, 110.0, 60.0), filled: false }, 0x34C759, 5.0),
        ann(4, Shape::Ellipse { rect: R::new(250.0, 120.0, 50.0, 50.0), filled: true }, 0xAF52DE, 2.0),
        ann(5, Shape::Line { from: pt(20.0, 140.0), to: pt(200.0, 200.0) }, 0xFF9500, 6.0),
        ann(6, Shape::Arrow { from: pt(40.0, 220.0), to: pt(220.0, 130.0) }, 0xFF3B30, 4.0),
        ann(7, Shape::Arrow { from: pt(330.0, 220.0), to: pt(300.0, 190.0) }, 0xFFFFFF, 3.0),
        ann(
            8,
            Shape::Pen { points: (0..40).map(|i| pt(120.0 + i as f32 * 5.0, 170.0 + (i as f32 * 0.5).sin() * 18.0)).collect() },
            0x5856D6,
            3.0,
        ),
        ann(9, Shape::Counter { center: pt(30.0, 30.0), n: 1, size: 14.0 }, 0xFF3B30, 0.0),
        ann(10, Shape::Counter { center: pt(340.0, 150.0), n: 12, size: 18.0 }, 0xFFCC00, 0.0),
        ann(11, Shape::Line { from: pt(10.0, 230.0), to: pt(100.0, 232.0) }, 0x1C1C1E, 1.0),
    ];
    add("annotations_shapes", d);

    let mut d = plain();
    d.style.padding = 32.0;
    d.annotations = vec![
        ann(1, text((20.0, 30.0), "Plain Segoe UI", 22.0, TextStyle::Plain, "Segoe UI", Weight::Semibold, Align::Left), 0x1C1C1E, 2.0),
        ann(2, text((20.0, 70.0), "Outline AVTo", 28.0, TextStyle::Outline, "Arial", Weight::Bold, Align::Left), 0xFF3B30, 2.0),
        ann(3, text((40.0, 120.0), "Pill label", 20.0, TextStyle::Pill, "Segoe UI", Weight::Regular, Align::Left), 0x007AFF, 2.0),
        ann(4, text((200.0, 150.0), "Centered\nGeorgia two", 18.0, TextStyle::Plain, "Georgia", Weight::Bold, Align::Center), 0xAF52DE, 2.0),
        ann(5, text((180.0, 30.0), "Right\naligned text", 16.0, TextStyle::Pill, "Segoe UI", Weight::Black, Align::Right), 0xFFCC00, 2.0),
        ann(6, text((20.0, 200.0), "light 14", 14.0, TextStyle::Plain, "Segoe UI", Weight::Light, Align::Left), 0x34C759, 2.0),
        ann(7, text((90.0, 196.0), "AVAWAY Tea", 17.0, TextStyle::Plain, "Corbel", Weight::Bold, Align::Left), 0x1C1C1E, 2.0),
        ann(8, text((90.0, 216.0), "Wave To AV", 17.0, TextStyle::Plain, "Calibri", Weight::Regular, Align::Left), 0x007AFF, 2.0),
    ];
    add("annotations_text", d);

    let mut d = plain();
    d.style.padding = 32.0;
    d.annotations = vec![
        ann(1, Shape::Redact { rect: R::new(80.0, 40.0, 120.0, 50.0), kind: RedactStyle::Pixelate, strength: 9.0 }, 0x000000, 0.0),
        ann(2, Shape::Redact { rect: R::new(220.0, 130.0, 120.0, 80.0), kind: RedactStyle::Blur, strength: 12.0 }, 0x000000, 0.0),
        ann(3, Shape::Redact { rect: R::new(80.0, 120.0, 70.0, 20.0), kind: RedactStyle::Solid, strength: 12.0 }, 0x000000, 0.0),
        ann(4, Shape::Highlight { rect: R::new(60.0, 30.0, 160.0, 120.0) }, 0x000000, 0.0),
        ann(5, Shape::Highlight { rect: R::new(200.0, 160.0, 100.0, 60.0) }, 0x000000, 0.0),
    ];
    add("annotations_redact", d);

    let mut d = plain();
    d.effects.border = Border { enabled: true, width: 8.0, color: Color::WHITE };
    d.effects.watermark = Watermark { enabled: true, text: "Glint".into(), placement: Placement::Grid(8), ..Default::default() };
    add("border_watermark_grid", d);

    let mut d = plain();
    d.effects.watermark = Watermark { enabled: true, text: "CONFIDENTIAL".into(), placement: Placement::Tiled, opacity: 0.35, size: 16.0, ..Default::default() };
    add("watermark_tiled", d);

    let mut d = plain();
    d.effects.perspective.rot_y = 28.0;
    d.effects.perspective.rot_x = 12.0;
    d.effects.perspective.zoom = 0.85;
    d.annotations = vec![ann(1, Shape::Arrow { from: pt(40.0, 200.0), to: pt(200.0, 90.0) }, 0xFF3B30, 4.0)];
    add("perspective", d);

    let mut d = plain();
    d.effects.perspective.rot_z = -8.0;
    d.effects.perspective.pan_x = 0.05;
    d.effects.border = Border { enabled: true, width: 4.0, color: Color::rgb(0x111111) };
    add("perspective_rotz_pan", d);

    let mut d = plain();
    d.effects.blur = ProgressiveBlur { enabled: true, mode: BlurMode::Radial, edge: BlurEdge::Bleed, strength: 40.0, start: 0.2, angle: 0.0 };
    add("blur_radial_bleed", d);

    let mut d = plain();
    d.effects.blur = ProgressiveBlur { enabled: true, mode: BlurMode::Directional, edge: BlurEdge::Clipped, strength: 40.0, start: 0.2, angle: 20.0 };
    add("blur_directional_clipped", d);

    let mut d = EditDoc::new(Style::default(), 1.5, None);
    d.annotations = vec![
        ann(1, Shape::Rect { rect: R::new(100.0, 60.0, 80.0, 50.0), filled: false }, 0xFF3B30, 3.0),
        ann(2, Shape::Counter { center: pt(200.0, 150.0), n: 3, size: 12.0 }, 0x007AFF, 0.0),
    ];
    d.set_crop(R::new(40.0, 30.0, 260.0, 180.0), 360, 240);
    add("crop_dpi", d);

    let mut d = plain();
    d.annotations = vec![
        ann(1, Shape::Arrow { from: pt(40.0, 200.0), to: pt(200.0, 90.0) }, 0xFF3B30, 4.0),
        ann(2, Shape::Redact { rect: R::new(80.0, 40.0, 120.0, 50.0), kind: RedactStyle::Pixelate, strength: 9.0 }, 0x000000, 0.0),
        ann(3, text((30.0, 150.0), "Half scale", 24.0, TextStyle::Pill, "Segoe UI", Weight::Semibold, Align::Left), 0x34C759, 2.0),
    ];
    out.push(Fixture { name: "scale_half", doc: d, scale: 0.5, region: None });

    let mut d = plain();
    d.effects.perspective.rot_y = 20.0;
    d.effects.blur = ProgressiveBlur { enabled: true, ..Default::default() };
    d.annotations = vec![ann(1, Shape::Highlight { rect: R::new(60.0, 30.0, 160.0, 120.0) }, 0x000000, 0.0)];
    out.push(Fixture { name: "region", doc: d, scale: 1.0, region: Some(R::new(57.0, 41.0, 250.0, 180.0)) });
    out
}

fn sources<'a>(base: &'a RgbaImage, doc: &EditDoc, dir: &Path, bg: &'a mut Option<tiny_skia::Pixmap>, blur: &'a mut Option<tiny_skia::Pixmap>) -> Sources<'a> {
    *bg = match &doc.style.background {
        Background::Image(p) => compose::load_background(&dir.join(p)),
        _ => None,
    };
    *blur = matches!(doc.style.background, Background::Blurred).then(|| render::make_blur_bg(base));
    Sources { base, bg_image: bg.as_ref(), blur_bg: blur.as_ref() }
}

fn json_fixtures() -> Vec<(&'static str, String)> {
    let mut out = Vec::new();
    let mut d = plain();
    d.window_title = Some("Notepad".into());
    d.style.background = Background::Image("C:\\Users\\me\\Pictures\\beach.jpg".into());
    d.effects.blur = ProgressiveBlur { enabled: true, mode: BlurMode::Directional, edge: BlurEdge::Clipped, ..Default::default() };
    d.effects.watermark = Watermark { enabled: true, text: "Draft".into(), placement: Placement::Tiled, ..Default::default() };
    d.crop = Some(R::new(10.0, 20.0, 300.0, 200.0));
    d.annotations = vec![
        ann(1, Shape::Text { pos: pt(5.0, 6.0), text: "Hi\nthere \"quoted\" ✓".into(), size: 28.0, style: TextStyle::Pill, font: TextFont { family: "Georgia".into(), weight: Weight::Bold, align: Align::Center } }, 0xFFFFFF, 2.0),
        ann(2, Shape::Pen { points: vec![pt(0.5, 0.25), pt(1e-5, 123456.75), pt(-3.0, 0.1)] }, 0xFF0000, 0.3),
    ];
    out.push(("doc_everything.json", d.to_json()));
    let presets = vec![
        Preset::from_doc("Beach", &d),
        Preset::from_doc("Default", &EditDoc::default()),
        { let mut p = Preset::from_doc("Solid", &EditDoc::default()); p.style.background = Background::Solid(Color(0x12345678)); p.style.chrome = Chrome::WinLight; p },
        { let mut p = Preset::from_doc("Blurred", &EditDoc::default()); p.style.background = Background::Blurred; p.style.aspect = Some(Aspect { w: 4.0, h: 5.0 }); p },
    ];
    out.push(("presets_export.glintpreset", preset::export(&presets)));
    out.push(("presets_store.json", serde_json::to_string_pretty(&presets).unwrap()));
    out
}

/// Handwritten partial documents; Rust fills the defaults and the Go port must fill the same.
const PARTIAL: &[(&str, &str)] = &[
    ("partial_empty", r#"{}"#),
    ("partial_dpi", r#"{"version":1,"dpi":2.0}"#),
    ("partial_style", r#"{"style":{"padding":10,"background":"Blurred"},"effects":{"border":{"enabled":true},"watermark":{"placement":"tiled","font":{"weight":"black"}}}}"#),
    ("partial_shapes", r#"{"annotations":[{"id":1,"shape":{"type":"text","pos":{"x":1,"y":2},"text":"a","size":10,"style":"plain"},"color":255,"width":1},{"id":2,"shape":{"type":"redact","rect":{"x":0,"y":0,"w":5,"h":5},"kind":"Blur"},"color":255,"width":0}]}"#),
];

fn write(dir: &Path) {
    std::fs::create_dir_all(dir.join("json")).unwrap();
    let base = base_image(360, 240);
    base.save(dir.join("base.png")).unwrap();
    bg_image().save(dir.join("bg.png")).unwrap();
    let mut manifest = Vec::new();
    for f in fixtures() {
        let (mut bg, mut blur) = (None, None);
        let src = sources(&base, &f.doc, dir, &mut bg, &mut blur);
        let pm = Compositor::default().render(&f.doc, &src, f.scale, f.region);
        render::rgba_from_pixmap(&pm).save(dir.join(format!("{}.png", f.name))).unwrap();
        std::fs::write(dir.join(format!("{}.json", f.name)), f.doc.to_json()).unwrap();
        manifest.push(serde_json::json!({ "name": f.name, "scale": f.scale, "region": f.region.map(|r| [r.x, r.y, r.w, r.h]) }));
        println!("{} {}x{}", f.name, pm.width(), pm.height());
    }
    std::fs::write(dir.join("manifest.json"), serde_json::to_string_pretty(&manifest).unwrap()).unwrap();
    for (name, json) in json_fixtures() {
        std::fs::write(dir.join("json").join(name), json).unwrap();
    }
    for (name, input) in PARTIAL {
        std::fs::write(dir.join("json").join(format!("{name}.in.json")), input).unwrap();
        std::fs::write(dir.join("json").join(format!("{name}.out.json")), EditDoc::from_json(input).unwrap().to_json()).unwrap();
    }
}

fn verify(dir: &Path) {
    let go = dir.join("json").join("go");
    let mut checked = 0;
    let mut docs: Vec<(String, EditDoc)> = fixtures().into_iter().map(|f| (format!("{}.json", f.name), f.doc)).collect();
    for (name, json) in json_fixtures() {
        if name.ends_with(".json") && name.starts_with("doc_") {
            docs.push((name.to_string(), EditDoc::from_json(&json).unwrap()));
        }
    }
    for (name, input) in PARTIAL {
        docs.push((format!("{name}.out.json"), EditDoc::from_json(input).unwrap()));
    }
    for (name, want) in docs {
        let got = std::fs::read_to_string(go.join(&name)).unwrap_or_else(|e| panic!("{name}: {e}"));
        let back = EditDoc::from_json(&got).unwrap_or_else(|e| panic!("{name}: Rust cannot parse Go output: {e}"));
        assert_eq!(back, want, "{name}");
        checked += 1;
    }
    let originals: Vec<(String, String)> = json_fixtures().into_iter().map(|(n, j)| (n.to_string(), j)).collect();
    for (name, json) in &originals {
        let got = match std::fs::read_to_string(go.join(name)) {
            Ok(s) => s,
            Err(_) => continue,
        };
        if name.ends_with(".glintpreset") {
            assert_eq!(preset::import(&got).unwrap(), preset::import(json).unwrap(), "{name}");
            checked += 1;
        } else if name.starts_with("presets_store") {
            let a: Vec<Preset> = serde_json::from_str(&got).unwrap_or_else(|e| panic!("{name}: {e}"));
            let b: Vec<Preset> = serde_json::from_str(json).unwrap();
            assert_eq!(a, b, "{name}");
            checked += 1;
        }
    }
    println!("Rust read {checked} Go-written JSON files and found them equal to the originals");
}

fn bench_doc() -> EditDoc {
    let mut d = EditDoc::new(Style::default(), 1.0, None);
    let colors = [0xFF3B30u32, 0x007AFF, 0x34C759, 0xFFCC00, 0xAF52DE];
    for i in 0..20u64 {
        let x = 100.0 + (i % 5) as f32 * 450.0;
        let y = 100.0 + (i / 5) as f32 * 320.0;
        let c = colors[(i % 5) as usize];
        let shape = match i % 10 {
            0 => Shape::Rect { rect: R::new(x, y, 300.0, 180.0), filled: false },
            1 => Shape::Arrow { from: pt(x, y + 200.0), to: pt(x + 300.0, y) },
            2 => Shape::Ellipse { rect: R::new(x, y, 260.0, 160.0), filled: false },
            3 => Shape::Line { from: pt(x, y), to: pt(x + 320.0, y + 150.0) },
            4 => Shape::Pen { points: (0..120).map(|k| pt(x + k as f32 * 2.5, y + 80.0 + (k as f32 * 0.2).sin() * 60.0)).collect() },
            5 => Shape::Counter { center: pt(x + 40.0, y + 40.0), n: i as u32, size: 24.0 },
            6 => Shape::Text { pos: pt(x, y), text: "Release notes".into(), size: 40.0, style: TextStyle::Pill, font: TextFont::default() },
            7 => Shape::Redact { rect: R::new(x, y, 300.0, 120.0), kind: RedactStyle::Pixelate, strength: 12.0 },
            8 => Shape::Redact { rect: R::new(x, y, 300.0, 120.0), kind: RedactStyle::Blur, strength: 12.0 },
            _ => Shape::Rect { rect: R::new(x, y, 200.0, 100.0), filled: true },
        };
        d.annotations.push(ann(i, shape, c, 5.0));
    }
    d
}

fn bench() {
    let base = base_image(2560, 1440);
    let doc = bench_doc();
    let src = Sources { base: &base, bg_image: None, blur_bg: None };
    let mut times = Vec::new();
    for _ in 0..7 {
        let t = std::time::Instant::now();
        let pm = Compositor::default().render(&doc, &src, 1.0, None);
        times.push(t.elapsed().as_secs_f64() * 1000.0);
        assert_eq!(pm.width(), 2560 + 128);
    }
    times.sort_by(f64::total_cmp);
    println!("rust render 2560x1440 + 20 annotations: median {:.1} ms, range {:.1}..{:.1} ms over {} runs", times[3], times[0], times[6], times.len());
}

fn main() {
    let args: Vec<String> = std::env::args().collect();
    match args.get(1).map(String::as_str) {
        Some("write") => write(&PathBuf::from(&args[2])),
        Some("verify") => verify(&PathBuf::from(&args[2])),
        Some("bench") => bench(),
        _ => eprintln!("usage: golden write <dir> | verify <dir> | bench"),
    }
}
