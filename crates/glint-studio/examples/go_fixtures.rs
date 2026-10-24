//! Writes the fixtures the Go port in `internal/studio` and `internal/transcribe` checks itself
//! against: serde decode results for hand-picked JSON, default and fully populated projects, a
//! saved transcript, and (given recording sessions) auto zooms and motion samples.
//!
//! cargo run -p glint-studio --example go_fixtures -- <out_dir> [<session.glintrec>...]

use glint_media::input::InputTimeline;
use glint_media::reader::VideoReader;
use glint_render::style::{Background, Color, Gradient};
use glint_studio::motion::Motion;
use glint_studio::preset::Preset;
use glint_studio::project::*;
use glint_studio::zoom::auto_zooms;
use glint_transcribe::{Transcript, Word};
use serde_json::{json, Value};
use std::path::{Path, PathBuf};

fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let out = PathBuf::from(&args[0]);
    std::fs::create_dir_all(&out)?;
    write(&out.join("serde_cases.json"), &serde_json::to_string_pretty(&serde_cases())?)?;
    write(&out.join("project_default.json"), &serde_json::to_string_pretty(&Project::default())?)?;
    write(&out.join("project_full.json"), &serde_json::to_string_pretty(&full_project())?)?;
    let transcript = Transcript::new(
        "en",
        vec![
            Word { text: "Hello,".into(), start_ms: 120, end_ms: 480, confidence: 0.9 },
            Word { text: "world.".into(), start_ms: 520, end_ms: 910, confidence: 0.73 },
        ],
    );
    transcript.save(&out.join("transcript.json"))?;
    for dir in &args[1..] {
        let dir = PathBuf::from(dir);
        let name = dir.file_name().unwrap().to_string_lossy().into_owned();
        std::fs::create_dir_all(out.join("parity"))?;
        write(&out.join("parity").join(format!("{name}.json")), &serde_json::to_string(&session_parity(&dir)?)?)?;
        println!("parity {name}");
    }
    Ok(())
}

fn write(path: &Path, s: &str) -> anyhow::Result<()> {
    std::fs::write(path, s)?;
    println!("wrote {}", path.display());
    Ok(())
}

fn case(kind: &str, input: &str, duration: Option<f64>) -> Value {
    let result: Result<String, String> = match kind {
        "project" => serde_json::from_str::<Project>(input)
            .map_err(|e| e.to_string())
            .map(|p| serde_json::to_string(&p).unwrap()),
        "project_parse" => Project::parse(input.as_bytes(), duration.unwrap())
            .map_err(|e| e.to_string())
            .map(|p| serde_json::to_string(&p).unwrap()),
        "preset" => serde_json::from_str::<Preset>(input)
            .map_err(|e| e.to_string())
            .map(|p| serde_json::to_string(&p).unwrap()),
        "transcript" => Transcript::parse(input.as_bytes())
            .map_err(|e| e.to_string())
            .map(|t| serde_json::to_string(&t).unwrap()),
        _ => unreachable!(),
    };
    match result {
        Ok(output) => json!({ "kind": kind, "input": input, "duration": duration, "ok": true, "output": output }),
        Err(error) => json!({ "kind": kind, "input": input, "duration": duration, "ok": false, "error": error }),
    }
}

fn serde_cases() -> Vec<Value> {
    let project = [
        r#"{}"#,
        r#"{ "version": 1, "clips": [{ "start_ms": 0, "end_ms": 5000 }], "zooms": [{ "start_ms": 100, "end_ms": 900 }] }"#,
        r#"{ "clips": [{ "start_ms": 0 }] }"#,
        r#"{ "clips": [{ "end_ms": 10, "speed": 2 }] }"#,
        r#"{ "zooms": [{ "start_ms": 1, "end_ms": 2, "focus": { "kind": "fixed", "x": 0.25, "y": 0.75 }, "scale": 2.5, "enabled": false, "auto": true }] }"#,
        r#"{ "zooms": [{ "start_ms": 1, "end_ms": 2, "focus": { "kind": "fixed", "x": 0.25 } }] }"#,
        r#"{ "zooms": [{ "start_ms": 1, "end_ms": 2, "focus": { "kind": "other" } }] }"#,
        r#"{ "zooms": [{ "start_ms": 1, "end_ms": 2, "focus": { "kind": "pointer", "x": 0.5 } }] }"#,
        r#"{ "zooms": [{ "start_ms": 1, "end_ms": 2, "focus": {} }] }"#,
        r#"{ "aspect": "wide16x9", "aspect_mode": "fill", "export": { "fps": "30", "codec": "hevc" } }"#,
        r#"{ "aspect": "vertical9x16" }"#,
        r#"{ "aspect": "portrait4x5" }"#,
        r#"{ "aspect": "Wide16x9" }"#,
        r#"{ "crop": { "x": 0.1, "y": 0.2, "w": 0.5 } }"#,
        r#"{ "crop": { "x": 0.1, "y": 0.2, "w": 0.5, "h": 0.25 } }"#,
        r#"{ "design": { "padding": 0.1 } }"#,
        r#"{ "design": { "background": "Blurred", "camera": { "center": [0.1, 0.2] } } }"#,
        r#"{ "design": { "background": "None", "cursor": { "smoothing": 0 } } }"#,
        r#"{ "design": { "background": { "Solid": 287454207 } } }"#,
        r#"{ "design": { "background": { "Gradient": { "stops": [4278190335, 16711935], "angle": 90 } } } }"#,
        r#"{ "design": { "background": { "Image": "C:\\bg.png" } } }"#,
        r#"{ "design": { "background": "Sparkles" } }"#,
        r#"{ "design": { "background": { "Gradient": { "stops": [4278190335] } } } }"#,
        r#"{ "design": { "background": { "Gradient": { "angle": 90 } } } }"#,
        r#"{ "design": { "camera": { "center": [0.1] } } }"#,
        r#"{ "caption_cues": [{ "start_ms": 0, "end_ms": 100, "text": "hi" }], "captions": { "enabled": true, "karaoke": true } }"#,
        r#"{ "caption_cues": [{ "start_ms": 0, "end_ms": 100 }] }"#,
        r#"{ "caption_cues": [] }"#,
        r#"{ "unknown_field": 1, "keystrokes": { "position": "top_left" } }"#,
        r#"{ "keystrokes": { "enabled": false, "position": "bottom_right" } }"#,
        r#"{ "keystrokes": { "position": "middle" } }"#,
        r#"{ "export": { "fps": 60 } }"#,
        r#"{ "export": { "fps": "24" } }"#,
        r#"{ "export": { "resolution": "p2160", "quality": "low", "format": "gif", "motion_blur": false } }"#,
        r#"{ "export": { "resolution": "source", "quality": "medium", "codec": "h264" } }"#,
        r#"{ "export": { "resolution": "p720" } }"#,
        r#"{ "audio": { "mic": { "volume": 0.5 }, "system": { "enabled": false } } }"#,
        r#"{ "version": 2 }"#,
        r#"{ "clips": [] }"#,
        r#"{ "click_highlights": false }"#,
        r#"{ "version": "1" }"#,
        r#"[]"#,
    ];
    let parse = [
        r#"{ "clips": [{ "start_ms": 3000, "end_ms": 9000, "speed": 20 }, { "start_ms": 0, "end_ms": 4000 }] }"#,
        r#"{ "version": 2 }"#,
        r#"{}"#,
        r#"{ "clips": [{ "start_ms": 0, "end_ms": 50 }] }"#,
        r#"{ "clips": [{ "start_ms": -100, "end_ms": 3000, "speed": 0.1 }, { "start_ms": 2000, "end_ms": 2100 }, { "start_ms": 2050, "end_ms": 7000 }] }"#,
        r#"{ "zooms": [{ "start_ms": 5000, "end_ms": 6000 }, { "start_ms": 900, "end_ms": 900 }, { "start_ms": 1000, "end_ms": 2000 }] }"#,
        r#"{ "caption_cues": [{ "start_ms": 500, "end_ms": 900, "text": "b" }, { "start_ms": 100, "end_ms": 300, "text": "a" }] }"#,
        r#"{ "version": 0, "clips": [{ "start_ms": 7950, "end_ms": 9000 }] }"#,
    ];
    let preset = [
        r#"{ "name": "A" }"#,
        r#"{ "version": 1 }"#,
        r#"{ "name": "B", "version": 3, "design": { "shadow": 0.2, "background": "Blurred" } }"#,
    ];
    let transcript = [
        r#"{ "version": 1, "language": "en", "words": [{ "text": "Hi", "start_ms": 0, "end_ms": 200, "confidence": 0.5 }] }"#,
        r#"{ "version": 1, "words": [] }"#,
        r#"{ "version": 2, "language": "en", "words": [] }"#,
        r#"{ "version": 1, "language": "en", "words": [{ "text": "Hi", "start_ms": 0, "end_ms": 200 }] }"#,
        r#"{ "version": 1, "language": "en", "words": [{ "text": "Hi", "start_ms": -1, "end_ms": 200, "confidence": 1 }] }"#,
        r#"{ "version": 1, "language": "de", "words": [], "extra": true }"#,
    ];
    let mut out = Vec::new();
    out.extend(project.iter().map(|j| case("project", j, None)));
    out.extend(parse.iter().map(|j| case("project_parse", j, Some(8000.0))));
    out.extend(preset.iter().map(|j| case("preset", j, None)));
    out.extend(transcript.iter().map(|j| case("transcript", j, None)));
    out
}

fn full_project() -> Project {
    let mut p = Project::new(12_345.5);
    p.clips = vec![
        Clip { start_ms: 0.0, end_ms: 4_000.25, speed: 1.0 },
        Clip { start_ms: 5_000.0, end_ms: 12_345.5, speed: 2.5 },
    ];
    p.zooms = vec![
        Zoom { start_ms: 100.0, end_ms: 900.0, scale: 1.5, focus: ZoomFocus::Pointer, enabled: true, auto: true },
        Zoom { start_ms: 6_000.0, end_ms: 8_000.0, scale: 2.25, focus: ZoomFocus::Fixed { x: 0.3, y: 0.7 }, enabled: false, auto: false },
    ];
    p.design.background = Background::Gradient(Gradient { stops: vec![Color::rgb(0x112233), Color(0xFFFFFF80)], angle: 45.5 });
    p.design.padding = 0.08;
    p.design.corner_radius = 0.035;
    p.design.shadow = 0.7;
    p.design.cursor = CursorSettings { visible: false, size: 1.75, smoothing: 0.9 };
    p.design.camera = CameraSettings { visible: false, center: [0.15, 0.2], size: 0.31, roundness: 0.1 };
    p.aspect = AspectPreset::Portrait4x5;
    p.aspect_mode = AspectMode::Fill;
    p.crop = CropRect { x: 0.1, y: 0.05, w: 0.8, h: 0.9 };
    p.click_highlights = false;
    p.keystrokes = KeystrokeSettings { enabled: false, position: KeystrokePosition::TopRight };
    p.captions = CaptionSettings { enabled: true, position: 0.5, size: 1.4, karaoke: true };
    p.caption_cues = vec![CaptionCue { start_ms: 120.0, end_ms: 910.0, text: "Hello, world.".into() }];
    p.audio.mic = AudioTrack { enabled: false, volume: 0.4 };
    p.audio.system = AudioTrack { enabled: true, volume: 1.6 };
    p.export = ExportOptions {
        fps: Fps::Fps30,
        motion_blur: false,
        resolution: Resolution::P1440,
        quality: Quality::Medium,
        codec: Codec::Hevc,
        format: Format::Gif,
    };
    p
}

fn session_parity(dir: &Path) -> anyhow::Result<Value> {
    let input = InputTimeline::load(dir)?;
    let duration = VideoReader::open(&dir.join("screen.mp4"))?.duration_ms() as f64;
    let zooms = auto_zooms(&input, duration);

    let mut auto = Project::new(duration);
    auto.apply_auto_zoom(&input, duration);

    let mut edited = auto.clone();
    edited.split_at(duration * 0.3);
    edited.split_at(duration * 0.6);
    edited.set_speed(1, 2.0);
    edited.delete_clip(2);
    edited.add_zoom(0.0..edited.duration_ms() * 0.5);
    let late = edited.duration_ms() * 0.55;
    let i = edited.add_zoom(late..edited.duration_ms());
    edited.zooms[i].focus = ZoomFocus::Fixed { x: 0.8, y: 0.2 };
    edited.zooms[i].scale = 2.0;
    edited.design.cursor.smoothing = 0.8;

    let mut fill = edited.clone();
    fill.aspect = AspectPreset::Vertical9x16;
    fill.aspect_mode = AspectMode::Fill;
    fill.crop = CropRect { x: 0.05, y: 0.05, w: 0.9, h: 0.9 };

    let projects: Vec<Value> = [("auto", &auto), ("edited", &edited), ("fill", &fill)]
        .into_iter()
        .map(|(name, p)| json!({ "name": name, "project": serde_json::to_string(p).unwrap(), "samples": samples(p, &input) }))
        .collect();
    Ok(json!({
        "dir": dir.to_string_lossy(),
        "duration_ms": duration,
        "auto_zooms": serde_json::to_string(&zooms)?,
        "projects": projects,
    }))
}

fn samples(p: &Project, input: &InputTimeline) -> Vec<Value> {
    let motion = Motion::build(p, input, &[]);
    let n = (p.duration_ms() * 60.0 / 1000.0).ceil() as usize;
    (0..=n)
        .map(|k| {
            let t = k as f64 * 1000.0 / 60.0;
            let v = motion.viewport(t);
            let ptr = motion.pointer.at(t).map(|f| json!([f.x, f.y, f.press_scale, f.cursor]));
            let clicks: Vec<Value> = motion
                .clicks
                .at(t, 10.0)
                .iter()
                .map(|c| json!([c.x, c.y, c.impact_radius, c.impact_alpha, c.ring_radius, c.ring_alpha, c.ring_width]))
                .collect();
            let key = motion.keys.at(t).map(|k| json!([k.modifiers, k.key, k.opacity, k.scale]));
            json!({ "t": t, "viewport": [v.scale, v.x, v.y], "pointer": ptr, "clicks": clicks, "key": key })
        })
        .collect()
}
