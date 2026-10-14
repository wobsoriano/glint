//! Preview playback of one video file. A background thread decodes frames and publishes
//! them as ready-to-paint images; the window sends commands and polls each tick.
//!
//! While playing, the clock is the audio device's played position when the file has audio,
//! otherwise the wall clock. Late frames are dropped so video keeps up with the clock.

use glint_media::{read_mp4_audio, AudioOutput, FrameReader, WavData};
use gpui::RenderImage;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Condvar, Mutex, OnceLock};
use std::thread::JoinHandle;
use std::time::Instant;

/// Longest side of preview frames; the window never shows them larger.
const PREVIEW_MAX: (u32, u32) = (1280, 720);

pub struct Player {
    shared: Arc<Shared>,
    thread: Option<JoinHandle<()>>,
}

#[derive(Default)]
struct Shared {
    ctl: Mutex<Control>,
    wake: Condvar,
    frame: Mutex<Option<Arc<RenderImage>>>,
    frame_serial: AtomicU64,
    position_ms: AtomicU64,
    playing: AtomicBool,
    /// 48 kHz stereo, or `None` when the file has no audio track. Unset while loading.
    audio: OnceLock<Option<Arc<Vec<f32>>>>,
    error: Mutex<Option<String>>,
}

#[derive(Default)]
struct Control {
    seek: Option<u64>,
    /// Playing until this time.
    play_until: Option<u64>,
    muted: bool,
    quit: bool,
}

impl Player {
    /// Starts decoding `path`, showing the frame at 0.
    pub fn new(path: PathBuf) -> Self {
        let shared = Arc::new(Shared::default());
        shared.ctl.lock().unwrap().seek = Some(0);
        let sh = shared.clone();
        let audio_path = path.clone();
        std::thread::spawn(move || {
            let pcm = match read_mp4_audio(&audio_path) {
                Ok(a) => a.map(|a| Arc::new(to_stereo_48k(&a))),
                Err(e) => {
                    *sh.error.lock().unwrap() = Some(format!("audio unavailable: {e:#}"));
                    None
                }
            };
            let _ = sh.audio.set(pcm);
        });
        let sh = shared.clone();
        let thread = std::thread::Builder::new()
            .name("glint-clip-player".into())
            .spawn(move || {
                if let Err(e) = run(&sh, &path) {
                    *sh.error.lock().unwrap() = Some(format!("{e:#}"));
                    sh.playing.store(false, Ordering::Relaxed);
                }
            })
            .ok();
        Self { shared, thread }
    }

    pub fn seek(&self, ms: u64) {
        self.shared.position_ms.store(ms, Ordering::Relaxed);
        self.command(|c| c.seek = Some(ms));
    }

    /// Plays from the current position to `until_ms`, then pauses there.
    pub fn play(&self, until_ms: u64, muted: bool) {
        self.shared.playing.store(true, Ordering::Relaxed);
        self.command(|c| {
            c.play_until = Some(until_ms);
            c.muted = muted;
        });
    }

    pub fn pause(&self) {
        self.shared.playing.store(false, Ordering::Relaxed);
        self.command(|c| c.play_until = None);
    }

    pub fn playing(&self) -> bool {
        self.shared.playing.load(Ordering::Relaxed)
    }

    pub fn position_ms(&self) -> u64 {
        self.shared.position_ms.load(Ordering::Relaxed)
    }

    /// `Some(true)` once the audio track is decoded, `Some(false)` when there is none.
    pub fn has_audio(&self) -> Option<bool> {
        self.shared.audio.get().map(Option::is_some)
    }

    pub fn error(&self) -> Option<String> {
        self.shared.error.lock().unwrap().clone()
    }

    /// The newest frame when it changed since `seen`, with its serial.
    pub fn frame_since(&self, seen: u64) -> Option<(u64, Arc<RenderImage>)> {
        let serial = self.shared.frame_serial.load(Ordering::Acquire);
        if serial == seen {
            return None;
        }
        self.shared.frame.lock().unwrap().clone().map(|f| (serial, f))
    }

    fn command(&self, f: impl FnOnce(&mut Control)) {
        f(&mut self.shared.ctl.lock().unwrap());
        self.shared.wake.notify_all();
    }
}

impl Drop for Player {
    fn drop(&mut self) {
        self.command(|c| c.quit = true);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

enum Next {
    Quit,
    Seek(u64),
    Play { until: u64, muted: bool },
}

fn next_command(sh: &Shared) -> Next {
    let mut c = sh.ctl.lock().unwrap();
    loop {
        if c.quit {
            return Next::Quit;
        }
        if let Some(ms) = c.seek.take() {
            return Next::Seek(ms);
        }
        if let Some(until) = c.play_until {
            return Next::Play { until, muted: c.muted };
        }
        c = sh.wake.wait(c).unwrap();
    }
}

/// True when a newer command than the running playback has arrived.
fn interrupted(sh: &Shared) -> bool {
    let c = sh.ctl.lock().unwrap();
    c.quit || c.seek.is_some() || c.play_until.is_none()
}

fn run(sh: &Shared, path: &std::path::Path) -> anyhow::Result<()> {
    let mut reader = FrameReader::open(path)?;
    let frame_ms = 1000.0 / reader.fps().max(1.0);
    let mut buffer = Vec::new();
    let publish = |frame: &glint_media::frames::Nv12Frame, buffer: &mut Vec<u8>| {
        let rgba = frame.to_rgba(std::mem::take(buffer));
        let image = preview_image(&rgba);
        *buffer = rgba.into_raw();
        *sh.frame.lock().unwrap() = Some(image);
        sh.frame_serial.fetch_add(1, Ordering::Release);
    };
    loop {
        match next_command(sh) {
            Next::Quit => return Ok(()),
            Next::Seek(ms) => {
                reader.seek(ms)?;
                if let Some(frame) = reader.next_frame()? {
                    publish(&frame, &mut buffer);
                }
            }
            Next::Play { until, muted } => {
                let start = sh.position_ms.load(Ordering::Relaxed);
                let audio = (!muted).then(|| sh.audio.get().cloned().flatten()).flatten();
                let output = audio.and_then(|pcm| {
                    let mut at = (start as usize * 48 * 2).min(pcm.len());
                    AudioOutput::start(move |out: &mut [f32]| {
                        let n = out.len().min(pcm.len() - at);
                        out[..n].copy_from_slice(&pcm[at..at + n]);
                        out[n..].fill(0.0);
                        at += n;
                    })
                    .ok()
                });
                let began = Instant::now();
                let clock = || match &output {
                    Some(o) => start + o.clock().played_ms() as u64,
                    None => start + began.elapsed().as_millis() as u64,
                };
                let finished = loop {
                    if interrupted(sh) {
                        break false;
                    }
                    let Some(frame) = reader.next_frame()? else { break true };
                    if frame.t_ms >= until {
                        break true;
                    }
                    let now = clock();
                    if (frame.t_ms as f64) + frame_ms < now as f64 {
                        continue;
                    }
                    while clock() < frame.t_ms && !interrupted(sh) {
                        std::thread::sleep(std::time::Duration::from_millis(2));
                    }
                    publish(&frame, &mut buffer);
                    sh.position_ms.store(frame.t_ms.max(start), Ordering::Relaxed);
                };
                drop(output);
                if finished {
                    sh.position_ms.store(until, Ordering::Relaxed);
                    sh.playing.store(false, Ordering::Relaxed);
                    let mut c = sh.ctl.lock().unwrap();
                    if c.play_until == Some(until) {
                        c.play_until = None;
                    }
                }
            }
        }
    }
}

/// Downscales to the preview size and swaps to the BGRA order GPUI paints.
fn preview_image(rgba: &image::RgbaImage) -> Arc<RenderImage> {
    let (w, h) = rgba.dimensions();
    let k = (PREVIEW_MAX.0 as f64 / w as f64).min(PREVIEW_MAX.1 as f64 / h as f64).min(1.0);
    let mut img = if k < 1.0 {
        glint_media::shrink(rgba, ((w as f64 * k) as u32).max(1), ((h as f64 * k) as u32).max(1))
    } else {
        rgba.clone()
    };
    for px in img.chunks_exact_mut(4) {
        px.swap(0, 2);
    }
    Arc::new(RenderImage::new(smallvec::smallvec![image::Frame::new(img)]))
}

/// Resamples to [`AudioOutput`]'s 48 kHz stereo: mono doubles up, extra channels drop.
fn to_stereo_48k(a: &WavData) -> Vec<f32> {
    let ch = a.channels.max(1) as usize;
    let frames = a.samples.len() / ch;
    let at = |i: usize, c: usize| a.samples[i * ch + c.min(ch - 1)];
    let out_frames = (frames as u64 * AudioOutput::SAMPLE_RATE as u64 / a.sample_rate.max(1) as u64) as usize;
    let step = a.sample_rate as f64 / AudioOutput::SAMPLE_RATE as f64;
    let mut out = Vec::with_capacity(out_frames * 2);
    for i in 0..out_frames {
        let x = i as f64 * step;
        let i0 = (x as usize).min(frames.saturating_sub(1));
        let i1 = (i0 + 1).min(frames.saturating_sub(1));
        let f = (x - i0 as f64) as f32;
        for c in 0..2 {
            out.push(at(i0, c) + (at(i1, c) - at(i0, c)) * f);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn audio_becomes_48k_stereo() {
        let mono = WavData { sample_rate: 24_000, channels: 1, samples: vec![0.0, 1.0, 0.0] };
        assert_eq!(to_stereo_48k(&mono), vec![0.0, 0.0, 0.5, 0.5, 1.0, 1.0, 0.5, 0.5, 0.0, 0.0, 0.0, 0.0]);
        let quad = WavData { sample_rate: 48_000, channels: 4, samples: vec![0.1, 0.2, 0.3, 0.4] };
        assert_eq!(to_stereo_48k(&quad), vec![0.1, 0.2]);
    }
}
