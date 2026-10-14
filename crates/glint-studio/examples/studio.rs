//! Drives the Studio engines on a recorded session.
//!
//! ```text
//! cargo run --release -p glint-studio --example studio -- <session.glintrec> export [--fps 30|60] [--no-blur] [--gif] [--edit]
//! cargo run --release -p glint-studio --example studio -- <session.glintrec> player <out_dir>
//! cargo run --release -p glint-studio --example studio -- <session.glintrec> thumbs <count> <height> <out_dir>
//! ```
//!
//! `--edit` splits the timeline at 2, 4 and 6 s, plays the second clip at 2x and deletes
//! the third.

use glint_studio::{export, timeline_thumbnails, Format, Fps, Player, Session};
use std::path::PathBuf;
use std::sync::atomic::AtomicBool;
use std::sync::Arc;
use std::time::{Duration, Instant};

fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let has = |flag: &str| args.iter().any(|a| a == flag);
    let get = |flag: &str| args.iter().position(|a| a == flag).and_then(|i| args.get(i + 1)).cloned();
    let dir = PathBuf::from(&args[0]);
    let opened = Instant::now();
    let mut session = Session::open(&dir)?;
    println!(
        "opened in {:?}: {}x{} {} ms, tracks {:?}, {} clips, {} zooms {:?}",
        opened.elapsed(),
        session.source_size.0,
        session.source_size.1,
        session.source_duration_ms,
        session.tracks,
        session.project.clips.len(),
        session.project.zooms.len(),
        session.project.zooms.iter().map(|z| (z.start_ms, z.end_ms)).collect::<Vec<_>>()
    );
    match args[1].as_str() {
        "export" => {
            if has("--edit") {
                let p = &mut session.project;
                p.split_at(2_000.0);
                p.split_at(4_000.0);
                p.split_at(6_000.0);
                p.set_speed(1, 2.0);
                p.delete_clip(2);
                println!("clips {:?}", p.clips.iter().map(|c| (c.start_ms, c.end_ms, c.speed)).collect::<Vec<_>>());
            }
            let mut opts = session.project.export;
            opts.fps = if get("--fps").as_deref() == Some("30") { Fps::Fps30 } else { Fps::Fps60 };
            opts.motion_blur = !has("--no-blur");
            if has("--gif") {
                opts.format = Format::Gif;
            }
            let fps = if opts.format == Format::Gif { 15 } else { opts.fps.value() };
            let duration = session.project.duration_ms();
            let frames = (duration * fps as f64 / 1000.0).round();
            println!("export {opts:?}: expect {duration} ms = {frames} frames at {fps} fps, canvas {:?}", session.project.output_size(session.source_size));
            let started = Instant::now();
            let mut last = 0.0;
            let path = export(&session, &opts, |p| {
                if p - last >= 0.25 || p == 1.0 {
                    println!("  progress {:.0}% at {:?}", p * 100.0, started.elapsed());
                    last = p;
                }
            }, &AtomicBool::new(false))?;
            let secs = started.elapsed().as_secs_f64();
            println!("{} in {secs:.2} s = {:.1} frames/s, {} bytes", path.display(), frames / secs, std::fs::metadata(&path)?.len());
        }
        "player" => {
            let out = PathBuf::from(&args[2]);
            std::fs::create_dir_all(&out)?;
            let player = Player::new(&session)?;
            player.set_preview_size(1280, 720);
            for t in [800.0, 2_300.0, 5_200.0] {
                let asked = Instant::now();
                player.seek(t);
                let frame = wait_for(&player, |ms| ms == t);
                println!("seek {t} ms: frame {}x{} after {:?}", frame.width(), frame.height(), asked.elapsed());
                frame.save(out.join(format!("seek-{t}.png")))?;
            }
            player.seek(0.0);
            wait_for(&player, |ms| ms == 0.0);
            player.play();
            let started = Instant::now();
            let mut last: Option<Arc<image::RgbaImage>> = None;
            let mut lags = Vec::new();
            let mut clock_samples = Vec::new();
            while started.elapsed() < Duration::from_secs(2) {
                let clock = player.position_ms();
                if clock > 0.0 {
                    clock_samples.push((started.elapsed().as_secs_f64() * 1000.0, clock));
                }
                if let Some((ms, f)) = player.latest_frame() {
                    if !last.as_ref().is_some_and(|l| Arc::ptr_eq(l, &f)) {
                        lags.push(clock - ms);
                        last = Some(f);
                    }
                }
                std::thread::sleep(Duration::from_micros(500));
            }
            player.pause();
            let n = clock_samples.len() as f64;
            let (mx, my) = clock_samples.iter().fold((0.0, 0.0), |a, s| (a.0 + s.0 / n, a.1 + s.1 / n));
            let slope = clock_samples.iter().map(|s| (s.0 - mx) * (s.1 - my)).sum::<f64>()
                / clock_samples.iter().map(|s| (s.0 - mx).powi(2)).sum::<f64>();
            let (first, last_sample) = (clock_samples[0], clock_samples[clock_samples.len() - 1]);
            let mean = lags.iter().sum::<f64>() / lags.len() as f64;
            let max = lags.iter().cloned().fold(f64::MIN, f64::max);
            println!(
                "played 2 s: {} new frames = {:.1} fps; audio clock minus shown frame time: mean {mean:.1} ms, max {max:.1} ms",
                lags.len(),
                lags.len() as f64 / 2.0
            );
            println!(
                "audio clock: first audible at {:.0} ms wall, advanced {:.1} ms over {:.1} ms wall, rate {slope:.4} of real time; error {:?}",
                first.0,
                last_sample.1 - first.1,
                last_sample.0 - first.0,
                player.error()
            );
            if let Some((ms, f)) = player.latest_frame() {
                f.save(out.join(format!("played-{ms:.0}.png")))?;
            }
        }
        "fake-camera" => {
            // A 640x480 camera track whose white bar sits at x = source seconds * 60 px and
            // whose label prints the source time, to check camera alignment after edits.
            let path = dir.join("camera.mp4");
            let params = glint_media::VideoParams { width: 640, height: 480, fps: 30, bitrate: 2_000_000, codec: glint_media::VideoCodec::H264 };
            let mut writer = glint_media::VideoWriter::create(&path, params, None)?;
            let frames = (session.source_duration_ms * 30.0 / 1000.0).round() as u32;
            for i in 0..frames {
                let t = i as f32 / 30.0;
                let mut img = image::RgbaImage::from_pixel(640, 480, image::Rgba([30, 60 + (t * 20.0) as u8, 90, 255]));
                let x0 = (t * 60.0) as u32;
                for y in 0..480 {
                    for x in x0..(x0 + 24).min(640) {
                        img.put_pixel(x, y, image::Rgba([255, 255, 255, 255]));
                    }
                }
                writer.write_frame(&img)?;
            }
            println!("wrote {} frames to {}", writer.finish()?.frames, path.display());
        }
        "compress" => {
            let src = PathBuf::from(&args[2]);
            let dst = PathBuf::from(&args[3]);
            let opts = glint_studio::CompressOptions {
                resolution: glint_studio::Resolution::P720,
                quality: glint_studio::Quality::Medium,
                codec: glint_studio::Codec::H264,
                trim: Some((1_000, 4_000)),
            };
            let started = Instant::now();
            let summary = glint_studio::compress_video(&src, &dst, &opts, &AtomicBool::new(false), |_| {})?;
            println!("compressed in {:?}: {summary:?}", started.elapsed());
        }
        "thumbs" => {
            let count: u32 = args[2].parse()?;
            let height: u32 = args[3].parse()?;
            let out = PathBuf::from(&args[4]);
            std::fs::create_dir_all(&out)?;
            for pass in ["first", "cached"] {
                let started = Instant::now();
                let thumbs = timeline_thumbnails(&session, count, height)?;
                println!("{pass}: {} thumbnails {:?} in {:?}", thumbs.len(), thumbs[0].dimensions(), started.elapsed());
                if pass == "first" {
                    for (i, t) in thumbs.iter().enumerate() {
                        t.save(out.join(format!("thumb-{i}.png")))?;
                    }
                }
            }
        }
        "bench" => {
            let started = Instant::now();
            let track = if has("--camera") { session.tracks.camera.clone().unwrap() } else { session.tracks.screen.clone() };
            let mut reader = glint_media::VideoReader::open(&track)?;
            let mut frames = Vec::new();
            while let Some(f) = reader.next_frame()? {
                frames.push(f.image);
            }
            let secs = started.elapsed().as_secs_f64();
            println!("decode: {} frames in {secs:.2} s = {:.1} fps", frames.len(), frames.len() as f64 / secs);
            let mut fast = glint_media::FrameReader::open(&track)?;
            let started = Instant::now();
            let (mut n, mut reused, mut kept) = (0usize, Vec::new(), Vec::new());
            let mut convert = Duration::ZERO;
            while let Some(f) = fast.next_frame()? {
                let t = Instant::now();
                let rgba = f.to_rgba(reused);
                convert += t.elapsed();
                if n == 0 || n == frames.len() / 2 {
                    kept.push((n, rgba.clone()));
                }
                reused = rgba.into_raw();
                n += 1;
            }
            let secs = started.elapsed().as_secs_f64();
            println!(
                "streaming NV12 decode (hardware {}) + RGBA: {n} frames in {secs:.2} s = {:.1} fps, of which RGBA conversion {:.1} ms/frame",
                fast.hardware(),
                n as f64 / secs,
                convert.as_secs_f64() * 1000.0 / n as f64
            );
            for (i, img) in &kept {
                let (a, b) = (frames[*i].as_raw(), img.as_raw());
                let max = a.iter().zip(b).map(|(x, y)| (*x as i32 - *y as i32).abs()).max().unwrap();
                let mean = a.iter().zip(b).map(|(x, y)| (*x as i32 - *y as i32).abs() as f64).sum::<f64>() / a.len() as f64;
                println!("frame {i}: RGB32 vs NV12 path max diff {max}, mean {mean:.3}");
            }
            let size = session.project.output_size(session.source_size);
            for blur in [false, true] {
                session.project.export.motion_blur = blur;
                let started = Instant::now();
                for (i, f) in frames.iter().enumerate().take(120) {
                    let inputs = glint_studio::FrameInputs { screen: f, camera: None, output_ms: i as f64 * 1000.0 / 30.0 };
                    std::hint::black_box(glint_studio::compose_frame_motion_blur(&session.project, &session.assets, &inputs, size));
                }
                let secs = started.elapsed().as_secs_f64();
                println!("compose {size:?} blur={blur}: {:.1} fps single stream", 120.0 / secs);
            }
            let started = Instant::now();
            let out = std::env::temp_dir().join("glint-bench-encode.mp4");
            let (w, h) = size;
            let frame = image::RgbaImage::from_pixel(w, h, image::Rgba([40, 90, 200, 255]));
            let mut writer = glint_media::VideoWriter::create(&out, glint_media::VideoParams { width: w, height: h, fps: 60, bitrate: glint_studio::bitrate(w, h, 60, glint_studio::Quality::High), codec: glint_media::VideoCodec::H264 }, None)?;
            for _ in 0..240 {
                writer.write_frame(&frame)?;
            }
            let summary = writer.finish()?;
            println!("encode: 240 frames in {:.2} s = {:.1} fps, hardware {}", started.elapsed().as_secs_f64(), 240.0 / started.elapsed().as_secs_f64(), summary.hardware);
        }
        other => anyhow::bail!("unknown command {other}"),
    }
    Ok(())
}

fn wait_for(player: &Player, done: impl Fn(f64) -> bool) -> Arc<image::RgbaImage> {
    loop {
        if let Some((ms, f)) = player.latest_frame() {
            if done(ms) {
                return f;
            }
        }
        std::thread::sleep(Duration::from_millis(1));
    }
}
