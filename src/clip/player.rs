use glint_media::{AudioOutput, AudioReader, FrameReader};
use gpui::RenderImage;
use std::collections::VecDeque;
use std::path::PathBuf;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Condvar, Mutex, OnceLock};
use std::thread::JoinHandle;
use std::time::Instant;

const PREVIEW_MAX: (u32, u32) = (1280, 720);

const QUEUE_AHEAD: usize = AudioOutput::SAMPLE_RATE as usize * AudioOutput::CHANNELS as usize / 2;

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
    has_audio: OnceLock<bool>,
    error: Mutex<Option<String>>,
}

#[derive(Default)]
struct Control {
    seek: Option<u64>,
    play_until: Option<u64>,
    muted: bool,
    quit: bool,
}

impl Player {
    pub fn new(path: PathBuf) -> Self {
        let shared = Arc::new(Shared::default());
        shared.ctl.lock().unwrap().seek = Some(0);
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

    pub fn has_audio(&self) -> Option<bool> {
        self.shared.has_audio.get().copied()
    }

    pub fn error(&self) -> Option<String> {
        self.shared.error.lock().unwrap().clone()
    }

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

fn interrupted(sh: &Shared) -> bool {
    let c = sh.ctl.lock().unwrap();
    c.quit || c.seek.is_some() || c.play_until.is_none()
}

fn run(sh: &Shared, path: &std::path::Path) -> anyhow::Result<()> {
    let mut reader = FrameReader::open(path)?;
    let mut audio = match AudioReader::open(path) {
        Ok(a) => a.map(Soundtrack::new),
        Err(e) => {
            *sh.error.lock().unwrap() = Some(format!("audio unavailable: {e:#}"));
            None
        }
    };
    let _ = sh.has_audio.set(audio.is_some());
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
                let mut track = audio.as_mut().filter(|_| !muted);
                let output = match &mut track {
                    Some(t) => {
                        t.seek(start)?;
                        t.output().ok()
                    }
                    None => None,
                };
                if output.is_none() {
                    track = None;
                }
                let began = Instant::now();
                let clock = || match &output {
                    Some(o) => start + o.clock().played_ms() as u64,
                    None => start + began.elapsed().as_millis() as u64,
                };
                let finished = loop {
                    if interrupted(sh) {
                        break false;
                    }
                    if let Some(t) = &mut track {
                        t.top_up()?;
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
                        if let Some(t) = &mut track {
                            t.top_up()?;
                        }
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

struct Soundtrack {
    reader: AudioReader,
    resampler: Resampler,
    queue: Arc<Mutex<VecDeque<f32>>>,
    chunk: Vec<f32>,
    scratch: Vec<f32>,
    ended: bool,
}

impl Soundtrack {
    fn new(reader: AudioReader) -> Self {
        let resampler = Resampler::new(reader.sample_rate(), reader.channels());
        Self { reader, resampler, queue: Arc::default(), chunk: Vec::new(), scratch: vec![0.0; 4096], ended: false }
    }

    fn seek(&mut self, ms: u64) -> anyhow::Result<()> {
        self.reader.seek(ms)?;
        self.resampler.reset();
        self.queue.lock().unwrap().clear();
        self.ended = false;
        self.top_up()
    }

    fn top_up(&mut self) -> anyhow::Result<()> {
        while self.queue.lock().unwrap().len() < QUEUE_AHEAD {
            let n = self.resampler.pull(&mut self.scratch, self.ended);
            if n > 0 {
                self.queue.lock().unwrap().extend(&self.scratch[..n]);
            } else if self.ended {
                return Ok(());
            } else {
                self.chunk.clear();
                self.ended = !self.reader.next_chunk(&mut self.chunk)?;
                self.resampler.push(&self.chunk);
            }
        }
        Ok(())
    }

    fn output(&self) -> anyhow::Result<AudioOutput> {
        let queue = self.queue.clone();
        AudioOutput::start(move |out: &mut [f32]| {
            let mut q = queue.lock().unwrap_or_else(|e| e.into_inner());
            let n = out.len().min(q.len());
            for (o, s) in out[..n].iter_mut().zip(q.drain(..n)) {
                *o = s;
            }
            out[n..].fill(0.0);
        })
    }
}

struct Resampler {
    channels: usize,
    step: f64,
    pending: Vec<f32>,
    pos: f64,
}

impl Resampler {
    fn new(sample_rate: u32, channels: u16) -> Self {
        let step = sample_rate.max(1) as f64 / AudioOutput::SAMPLE_RATE as f64;
        Self { channels: channels.max(1) as usize, step, pending: Vec::new(), pos: 0.0 }
    }

    fn reset(&mut self) {
        self.pending.clear();
        self.pos = 0.0;
    }

    fn push(&mut self, chunk: &[f32]) {
        self.pending.extend_from_slice(chunk);
    }

    fn pull(&mut self, out: &mut [f32], ended: bool) -> usize {
        let ch = self.channels;
        let frames = self.pending.len() / ch;
        let mut written = 0;
        for o in out.chunks_exact_mut(2) {
            let i0 = self.pos as usize;
            if i0 >= frames || (i0 + 1 >= frames && !ended) {
                break;
            }
            let i1 = (i0 + 1).min(frames - 1);
            let f = (self.pos - i0 as f64) as f32;
            for c in 0..2 {
                let (a, b) = (self.pending[i0 * ch + c.min(ch - 1)], self.pending[i1 * ch + c.min(ch - 1)]);
                o[c] = a + (b - a) * f;
            }
            self.pos += self.step;
            written += 2;
        }
        let used = (self.pos as usize).min(frames);
        self.pending.drain(..used * ch);
        self.pos -= used as f64;
        written
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn chunks_become_48k_stereo() {
        let mut mono = Resampler::new(24_000, 1);
        let mut out = [0.0; 12];
        let mut got = Vec::new();
        mono.push(&[0.0, 1.0]);
        let n = mono.pull(&mut out, false);
        got.extend_from_slice(&out[..n]);
        mono.push(&[0.0]);
        let n = mono.pull(&mut out, false);
        got.extend_from_slice(&out[..n]);
        let n = mono.pull(&mut out, true);
        got.extend_from_slice(&out[..n]);
        assert_eq!(got, vec![0.0, 0.0, 0.5, 0.5, 1.0, 1.0, 0.5, 0.5, 0.0, 0.0, 0.0, 0.0]);
        assert_eq!(mono.pull(&mut out, true), 0);

        let mut quad = Resampler::new(48_000, 4);
        quad.push(&[0.1, 0.2, 0.3, 0.4]);
        let n = quad.pull(&mut out, true);
        assert_eq!(&out[..n], &[0.1, 0.2]);
    }
}
