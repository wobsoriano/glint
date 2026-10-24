//! Exports a session with exactly the options its project.json holds, for comparing the Go
//! exporter in `internal/studio` against this one. Run it on a copy of a session, never the
//! Library: the render lands in the copy's `render/`.
//!
//! cargo run --release -p glint-studio --example go_export -- <session_dir>
//! cargo run --release -p glint-studio --example go_export -- palette <frame.png>...
//!
//! The first prints one JSON line: the render's path, wall time, frame count and fps achieved.
//! `palette` trains the GIF palette on the frames as export.rs does and prints its colours
//! and the FNV-1a hash of its 15-bit lookup table.

use glint_studio::{export, Format, Session};
use serde_json::json;
use std::path::PathBuf;
use std::sync::atomic::AtomicBool;
use std::time::Instant;

fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.first().map(String::as_str) == Some("palette") {
        return palette(&args[1..]);
    }
    let dir = PathBuf::from(args.first().expect("usage: go_export <session_dir> | palette <png>..."));
    let session = Session::open(&dir)?;
    let opts = session.project.export;
    let fps = if opts.format == Format::Gif { 15 } else { opts.fps.value() };
    let frames = ((session.project.duration_ms() * fps as f64 / 1000.0).round() as u64).max(1);
    let started = Instant::now();
    let mut last = 0.0f32;
    let path = export(&session, &opts, |p| last = p, &AtomicBool::new(false))?;
    let secs = started.elapsed().as_secs_f64();
    let line = json!({
        "path": path,
        "seconds": secs,
        "frames": frames,
        "fps_achieved": frames as f64 / secs,
        "last_progress": last,
        "duration_ms": session.project.duration_ms(),
        "size": session.project.output_size(session.source_size),
    });
    println!("{line}");
    Ok(())
}

fn palette(paths: &[String]) -> anyhow::Result<()> {
    let mut pixels = Vec::new();
    for p in paths {
        pixels.extend_from_slice(image::open(p)?.to_rgba8().as_raw());
    }
    let nq = color_quant::NeuQuant::new(10, 256, &pixels);
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for c in 0..32768u32 {
        let ch = |shift: u32| (((c >> shift) & 31) << 3 | 4) as u8;
        h ^= nq.index_of(&[ch(10), ch(5), ch(0), 255]) as u8 as u64;
        h = h.wrapping_mul(0x0100_0000_01b3);
    }
    println!("{}", json!({ "rgb": nq.color_map_rgb(), "lut_fnv": format!("{h:016x}") }));
    Ok(())
}
