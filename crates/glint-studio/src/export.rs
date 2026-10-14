//! Rendering the edited recording to a file: MP4 (H.264, or HEVC where the machine has an
//! encoder) with the mixed soundtrack, or a palette GIF.
//!
//! Frames flow through three stages on their own threads: one decoder walks the clip list
//! in output order, several compositors draw frames in parallel, and the calling thread
//! puts them back in order and encodes. Finished files are cached in `render/` under a hash
//! of everything that affects the pixels and samples, so an unchanged export returns at once.

use crate::compose::{compose_frame_motion_blur, recycle_frame, FrameInputs};
use crate::feed::{FrameFeed, SourceFrame};
use crate::mix::{AudioMixer, AudioSources, CHANNELS, SAMPLE_RATE};
use crate::project::{Codec, ExportOptions, Format, Project, Quality, Resolution};
use crate::session::Session;
use anyhow::{bail, Context};
use glint_media::{available_codecs, transcode, AudioParams, TranscodeOptions, VideoCodec, VideoParams, VideoReader, VideoWriter, WriteSummary};
use image::RgbaImage;
use std::collections::BTreeMap;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::mpsc::sync_channel;
use std::sync::{Arc, Mutex};

/// Bump when rendering changes so stale cached exports are not reused.
const RENDER_VERSION: u32 = 3;
const GIF_FPS: u32 = 15;
const GIF_MAX_SHORT_SIDE: u32 = 480;
/// Frames composed in parallel. Each compositor already splits the screen blit and the NV12
/// conversion across all cores; on a 12-thread machine a third compositor only contends.
const COMPOSITORS: usize = 2;

/// Renders the session's current project with `opts` and returns the file in `render/`,
/// reusing a finished render of the same state. `progress` receives 0..=1. Setting
/// `cancel` stops the render, removes the partial file and returns an error.
pub fn export(session: &Session, opts: &ExportOptions, mut progress: impl FnMut(f32), cancel: &AtomicBool) -> anyhow::Result<PathBuf> {
    let mut project = session.project.clone();
    project.export = *opts;
    let ext = match opts.format {
        Format::Mp4 => "mp4",
        Format::Gif => "gif",
    };
    let key = cache_key(session, &project);
    let dir = session.render_dir();
    let out = dir.join(format!("export-{key}.{ext}"));
    if out.is_file() {
        progress(1.0);
        return Ok(out);
    }
    std::fs::create_dir_all(&dir).with_context(|| format!("creating {}", dir.display()))?;
    static PARTIAL: AtomicU64 = AtomicU64::new(0);
    let partial = dir.join(format!(
        "export-{key}.{}-{}.partial.{ext}",
        std::process::id(),
        PARTIAL.fetch_add(1, Ordering::Relaxed)
    ));
    let rendered = match opts.format {
        Format::Mp4 => write_mp4(session, &project, &partial, &mut progress, cancel),
        Format::Gif => write_gif(session, &project, &partial, &mut progress, cancel),
    };
    if let Err(e) = rendered {
        let _ = std::fs::remove_file(&partial);
        return Err(e);
    }
    if std::fs::rename(&partial, &out).is_err() {
        // Another render of the same state finished first and its file is in use.
        let _ = std::fs::remove_file(&partial);
        if !out.is_file() {
            bail!("could not move the render into {}", out.display());
        }
    }
    remove_stale_exports(&dir, &out, ext);
    progress(1.0);
    Ok(out)
}

/// The current edited recording as a file, rendering it with the project's own export
/// options when the cache does not have it. Used by copy and save outside Studio.
pub fn deliverable(session: &Session) -> anyhow::Result<PathBuf> {
    export(session, &session.project.export, |_| {}, &AtomicBool::new(false))
}

/// Average bitrate for a quality: bits per pixel per frame, so screen content stays crisp
/// at any size and frame rate.
pub fn bitrate(width: u32, height: u32, fps: u32, quality: Quality) -> u32 {
    let bpp = match quality {
        Quality::High => 0.1,
        Quality::Medium => 0.065,
        Quality::Low => 0.04,
    };
    (width as f64 * height as f64 * fps as f64 * bpp).clamp(1_500_000.0, 80_000_000.0) as u32
}

/// Options for the trim-and-compress editor.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct CompressOptions {
    /// Shrinks the short side to this; never upscales.
    pub resolution: Resolution,
    pub quality: Quality,
    pub codec: Codec,
    /// Keep only `start_ms..end_ms`.
    pub trim: Option<(u64, u64)>,
}

/// Re-encodes any MP4 smaller: resized, at the quality's bitrate, optionally trimmed.
pub fn compress_video(
    src: &Path,
    dst: &Path,
    opts: &CompressOptions,
    cancel: &AtomicBool,
    progress: impl FnMut(f32),
) -> anyhow::Result<WriteSummary> {
    let reader = VideoReader::open(src)?;
    let (w, h) = reader.size();
    let fps = reader.fps().round().max(1.0) as u32;
    drop(reader);
    let size = match opts.resolution.short_side() {
        Some(s) if s < w.min(h) => {
            let k = s as f64 / w.min(h) as f64;
            (((w as f64 * k).round() as u32) & !1, ((h as f64 * k).round() as u32) & !1)
        }
        _ => (w & !1, h & !1),
    };
    let topts = TranscodeOptions {
        size: Some(size),
        bitrate: Some(bitrate(size.0, size.1, fps, opts.quality)),
        codec: Some(pick_codec(opts.codec)),
        trim: opts.trim,
    };
    transcode(src, dst, &topts, cancel, progress)
}

fn pick_codec(codec: Codec) -> VideoCodec {
    match codec {
        Codec::Hevc if available_codecs().contains(&VideoCodec::Hevc) => VideoCodec::Hevc,
        _ => VideoCodec::H264,
    }
}

fn frame_count(duration_ms: f64, fps: u32) -> u64 {
    ((duration_ms * fps as f64 / 1000.0).round() as u64).max(1)
}

fn write_mp4(
    session: &Session,
    project: &Project,
    path: &Path,
    progress: &mut dyn FnMut(f32),
    cancel: &AtomicBool,
) -> anyhow::Result<()> {
    let opts = project.export;
    let fps = opts.fps.value();
    let (w, h) = project.output_size(session.source_size);
    let frames = frame_count(project.duration_ms(), fps);
    let sources = AudioSources::load(&session.tracks)?;
    let audible = sources.audible(&project.audio);
    let video = VideoParams { width: w, height: h, fps, bitrate: bitrate(w, h, fps, opts.quality), codec: pick_codec(opts.codec) };
    let audio = audible.then_some(AudioParams { sample_rate: SAMPLE_RATE, channels: CHANNELS });
    let mut writer = VideoWriter::create(path, video, audio)?;
    let mut mixer = audible.then(|| AudioMixer::new(Arc::new(sources), project, 0.0));
    let mut audio_frames = 0u64;
    let mut pcm = Vec::new();
    render_frames(session, project, (w, h), fps, frames, cancel, |i, frame| {
        writer.write_frame(&frame)?;
        recycle_frame(frame);
        if let Some(m) = mixer.as_mut() {
            let target = (i + 1) * SAMPLE_RATE as u64 / fps as u64;
            pcm.resize((target - audio_frames) as usize * CHANNELS as usize, 0.0);
            m.render(&mut pcm);
            writer.write_audio(&pcm)?;
            audio_frames = target;
        }
        progress((i + 1) as f32 / frames as f32);
        Ok(())
    })?;
    writer.finish().with_context(|| format!("finishing {}", path.display()))?;
    Ok(())
}

struct Job {
    index: u64,
    output_ms: f64,
    screen: Arc<SourceFrame>,
    camera: Option<Arc<SourceFrame>>,
}

/// Composes output frames `0..frames` at `fps` and hands them to `sink` in order.
fn render_frames(
    session: &Session,
    project: &Project,
    size: (u32, u32),
    fps: u32,
    frames: u64,
    cancel: &AtomicBool,
    mut sink: impl FnMut(u64, RgbaImage) -> anyhow::Result<()>,
) -> anyhow::Result<()> {
    let assets = &*session.assets;
    let tracks = &session.tracks;
    std::thread::scope(|s| {
        let (job_tx, job_rx) = sync_channel::<anyhow::Result<Job>>(COMPOSITORS * 2);
        let (out_tx, out_rx) = sync_channel::<anyhow::Result<(u64, RgbaImage)>>(COMPOSITORS * 2);
        s.spawn(move || {
            let decoded = (|| {
                let mut screen = FrameFeed::open(&tracks.screen)?;
                let mut camera = tracks.camera.as_deref().map(FrameFeed::open).transpose()?;
                for index in 0..frames {
                    if cancel.load(Ordering::Relaxed) {
                        break;
                    }
                    let output_ms = index as f64 * 1000.0 / fps as f64;
                    let source_ms = project.source_time(output_ms);
                    let screen = screen.frame_at(source_ms)?.context("screen.mp4 has no frames")?;
                    let camera = match camera.as_mut() {
                        Some(feed) => feed.frame_at(source_ms)?,
                        None => None,
                    };
                    if job_tx.send(Ok(Job { index, output_ms, screen, camera })).is_err() {
                        break;
                    }
                }
                anyhow::Ok(())
            })();
            if let Err(e) = decoded {
                let _ = job_tx.send(Err(e));
            }
        });
        let job_rx = Arc::new(Mutex::new(job_rx));
        for _ in 0..COMPOSITORS {
            let (job_rx, out_tx) = (job_rx.clone(), out_tx.clone());
            s.spawn(move || loop {
                let job = job_rx.lock().unwrap_or_else(|e| e.into_inner()).recv();
                let Ok(job) = job else { break };
                let composed = job.map(|j| {
                    let inputs = FrameInputs { screen: j.screen.rgba(), camera: j.camera.as_ref().map(|c| c.rgba()), output_ms: j.output_ms };
                    (j.index, compose_frame_motion_blur(project, assets, &inputs, size))
                });
                if out_tx.send(composed).is_err() {
                    break;
                }
            });
        }
        drop(job_rx);
        drop(out_tx);
        let mut pending = BTreeMap::new();
        let mut next = 0u64;
        let result = (|| {
            for msg in &out_rx {
                let (i, frame) = msg?;
                pending.insert(i, frame);
                while let Some(frame) = pending.remove(&next) {
                    sink(next, frame)?;
                    next += 1;
                }
                if cancel.load(Ordering::Relaxed) {
                    bail!("export cancelled");
                }
            }
            if cancel.load(Ordering::Relaxed) {
                bail!("export cancelled");
            }
            if next != frames {
                bail!("rendered {next} of {frames} frames");
            }
            Ok(())
        })();
        drop(out_rx);
        result
    })
}

fn write_gif(
    session: &Session,
    project: &Project,
    path: &Path,
    progress: &mut dyn FnMut(f32),
    cancel: &AtomicBool,
) -> anyhow::Result<()> {
    let size = gif_size(project.output_size(session.source_size));
    let frames = frame_count(project.duration_ms(), GIF_FPS);
    let palette = Palette::train(&sample_frames(session, project, size, 8)?);
    let file = std::io::BufWriter::new(std::fs::File::create(path).with_context(|| format!("creating {}", path.display()))?);
    let mut encoder = gif::Encoder::new(file, size.0 as u16, size.1 as u16, &palette.rgb)?;
    encoder.set_repeat(gif::Repeat::Infinite)?;
    render_frames(session, project, size, GIF_FPS, frames, cancel, |i, frame| {
        // Centiseconds, alternating so the total stays exact at frame rates like 15.
        let delay = (((i + 1) * 100).div_ceil(GIF_FPS as u64) - (i * 100).div_ceil(GIF_FPS as u64)) as u16;
        let indices = palette.map(&frame);
        recycle_frame(frame);
        let gif_frame = gif::Frame { width: size.0 as u16, height: size.1 as u16, delay, buffer: indices.into(), ..Default::default() };
        encoder.write_frame(&gif_frame)?;
        progress((i + 1) as f32 / frames as f32);
        Ok(())
    })?;
    drop(encoder);
    Ok(())
}

/// A GIF is the export canvas shrunk so its short side is at most 480 pixels.
fn gif_size(out: (u32, u32)) -> (u32, u32) {
    let short = out.0.min(out.1);
    if short <= GIF_MAX_SHORT_SIDE {
        return out;
    }
    let k = GIF_MAX_SHORT_SIDE as f64 / short as f64;
    let even = |v: u32| (((v as f64 * k).round() as u32) & !1).max(2);
    (even(out.0), even(out.1))
}

/// `n` composed frames spread across the output, for training the GIF palette.
fn sample_frames(session: &Session, project: &Project, size: (u32, u32), n: u32) -> anyhow::Result<Vec<RgbaImage>> {
    let mut screen = FrameFeed::open(&session.tracks.screen)?;
    let mut camera = session.tracks.camera.as_deref().map(FrameFeed::open).transpose()?;
    let duration = project.duration_ms();
    let mut out = Vec::with_capacity(n as usize);
    for i in 0..n {
        let output_ms = duration * (i as f64 + 0.5) / n as f64;
        let source_ms = project.source_time(output_ms);
        let frame = screen.frame_at(source_ms)?.context("screen.mp4 has no frames")?;
        let cam = match camera.as_mut() {
            Some(feed) => feed.frame_at(source_ms)?,
            None => None,
        };
        let inputs = FrameInputs { screen: frame.rgba(), camera: cam.as_ref().map(|c| c.rgba()), output_ms };
        out.push(compose_frame_motion_blur(project, &session.assets, &inputs, size));
    }
    Ok(out)
}

/// One 256-colour palette for the whole GIF, so colours do not shimmer between frames,
/// with ordered dithering to hide banding in gradients.
struct Palette {
    rgb: Vec<u8>,
    /// Palette index for every 5-bit-per-channel colour.
    lut: Vec<u8>,
}

const BAYER4: [[i32; 4]; 4] = [[0, 8, 2, 10], [12, 4, 14, 6], [3, 11, 1, 9], [15, 7, 13, 5]];

impl Palette {
    fn train(frames: &[RgbaImage]) -> Self {
        let pixels: Vec<u8> = frames.iter().flat_map(|f| f.as_raw().iter().copied()).collect();
        let nq = color_quant::NeuQuant::new(10, 256, &pixels);
        let lut = (0..32768u32)
            .map(|c| {
                let ch = |shift: u32| (((c >> shift) & 31) << 3 | 4) as u8;
                nq.index_of(&[ch(10), ch(5), ch(0), 255]) as u8
            })
            .collect();
        Self { rgb: nq.color_map_rgb(), lut }
    }

    fn map(&self, frame: &RgbaImage) -> Vec<u8> {
        let w = frame.width() as usize;
        frame
            .as_raw()
            .chunks_exact(4)
            .enumerate()
            .map(|(i, px)| {
                let d = BAYER4[(i / w) % 4][i % w % 4] - 8;
                let q = |v: u8| ((v as i32 + d).clamp(0, 255) >> 3) as usize;
                self.lut[q(px[0]) << 10 | q(px[1]) << 5 | q(px[2])]
            })
            .collect()
    }
}

/// FNV-1a over everything a render depends on: the project with its export options and the
/// identity of every session file the frames and samples come from.
fn cache_key(session: &Session, project: &Project) -> String {
    let stamp = |p: &Path| {
        std::fs::metadata(p).ok().map(|m| {
            let modified = m.modified().ok().and_then(|t| t.duration_since(std::time::UNIX_EPOCH).ok()).map_or(0, |d| d.as_millis());
            (m.len(), modified)
        })
    };
    let t = &session.tracks;
    let files: Vec<_> = [Some(&t.screen), t.camera.as_ref(), t.mic.as_ref(), t.system.as_ref()]
        .into_iter()
        .flatten()
        .cloned()
        .chain([session.dir.join("input.json"), session.dir.join(glint_transcribe::TRANSCRIPT_FILE)])
        .map(|p| stamp(&p))
        .collect();
    let json = serde_json::to_string(&(RENDER_VERSION, project, files)).unwrap_or_default();
    let mut h: u64 = 0xcbf2_9ce4_8422_2325;
    for b in json.bytes() {
        h ^= b as u64;
        h = h.wrapping_mul(0x0100_0000_01b3);
    }
    format!("{h:016x}")
}

/// Keeps one finished render per format.
fn remove_stale_exports(dir: &Path, keep: &Path, ext: &str) {
    let Ok(entries) = std::fs::read_dir(dir) else { return };
    for e in entries.flatten() {
        let p = e.path();
        let name = e.file_name().to_string_lossy().into_owned();
        if p != keep && name.starts_with("export-") && !name.contains(".partial.") && name.ends_with(&format!(".{ext}")) {
            let _ = std::fs::remove_file(p);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn gif_canvas_shrinks_to_480_short_side() {
        assert_eq!(gif_size((1920, 1080)), (852, 480));
        assert_eq!(gif_size((1080, 1920)), (480, 852));
        assert_eq!(gif_size((640, 360)), (640, 360));
    }

    #[test]
    fn frame_counts_and_bitrates() {
        assert_eq!(frame_count(7_000.0, 60), 420);
        assert_eq!(frame_count(5_975.0, 30), 179);
        assert_eq!(bitrate(1920, 1080, 30, Quality::High), 6_220_800);
        assert_eq!(bitrate(1920, 1080, 60, Quality::Low), 4_976_640);
        assert_eq!(bitrate(320, 240, 30, Quality::Low), 1_500_000);
    }

    #[test]
    fn palette_maps_flat_colours_to_their_nearest_entry() {
        let mut img = RgbaImage::new(8, 8);
        for (x, _, p) in img.enumerate_pixels_mut() {
            *p = if x < 4 { image::Rgba([250, 20, 20, 255]) } else { image::Rgba([20, 20, 250, 255]) };
        }
        let palette = Palette::train(std::slice::from_ref(&img));
        let idx = palette.map(&img);
        let colour = |i: u8| &palette.rgb[i as usize * 3..i as usize * 3 + 3];
        assert!(colour(idx[0])[0] > 200 && colour(idx[0])[2] < 60, "{:?}", colour(idx[0]));
        assert!(colour(idx[7])[2] > 200 && colour(idx[7])[0] < 60, "{:?}", colour(idx[7]));
    }
}
