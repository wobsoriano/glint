//! The Studio preview engine. A background thread decodes, composes and publishes frames;
//! the UI thread sends commands and polls [`Player::latest_frame`] each time it paints.
//!
//! While playing, the clock is the audio device's played position when the project has
//! audible audio, otherwise the wall clock. Each frame shows the clock's current time, so a
//! slow composite skips frames instead of falling behind.

use crate::compose::{compose_frame, recycle_frame, Assets, FrameInputs};
use crate::feed::FrameFeed;
use crate::mix::{AudioMixer, AudioSources};
use crate::project::Project;
use crate::session::Session;
use anyhow::Context;
use glint_media::{AudioClock, AudioOutput};
use image::RgbaImage;
use std::sync::atomic::{AtomicBool, AtomicU64, Ordering};
use std::sync::{Arc, Condvar, Mutex, MutexGuard};
use std::thread::JoinHandle;
use std::time::{Duration, Instant};

const FRAME_INTERVAL: Duration = Duration::from_micros(16_667);

pub struct Player {
    shared: Arc<Shared>,
    thread: Option<JoinHandle<()>>,
}

#[derive(Default)]
struct Shared {
    ctl: Mutex<Control>,
    wake: Condvar,
    frame: Mutex<Option<(f64, Arc<RgbaImage>)>>,
    position_bits: AtomicU64,
    live: Mutex<Option<(Clock, f64)>>,
    playing: AtomicBool,
    error: Mutex<Option<String>>,
}

#[derive(Default)]
struct Control {
    project: Option<Project>,
    assets: Option<Arc<Assets>>,
    preview: (u32, u32),
    redraw: bool,
    seek: Option<f64>,
    play: bool,
    quit: bool,
}

impl Player {
    /// Starts the engine on `session`'s current project, showing the first frame.
    pub fn new(session: &Session) -> anyhow::Result<Self> {
        let shared = Arc::new(Shared::default());
        {
            let mut c = shared.ctl.lock().unwrap();
            c.project = Some(session.project.clone());
            c.assets = Some(session.assets.clone());
            c.preview = (960, 540);
        }
        let tracks = session.tracks.clone();
        let source_size = session.source_size;
        let (tx, rx) = std::sync::mpsc::channel::<anyhow::Result<()>>();
        let sh = shared.clone();
        let thread = std::thread::Builder::new().name("glint-player".into()).spawn(move || {
            let opened = (|| {
                let screen = FrameFeed::open(&tracks.screen)?;
                let camera = tracks.camera.as_deref().map(FrameFeed::open).transpose()?;
                anyhow::Ok((screen, camera))
            })();
            let (screen, camera) = match opened {
                Ok(v) => v,
                Err(e) => {
                    let _ = tx.send(Err(e));
                    return;
                }
            };
            let _ = tx.send(Ok(()));
            let audio = AudioSources::load(&tracks).unwrap_or_else(|e| {
                *sh.error.lock().unwrap() = Some(format!("audio unavailable: {e:#}"));
                AudioSources::default()
            });
            Engine { shared: sh, screen, camera, audio: Arc::new(audio), source_size }.run();
        })?;
        match rx.recv() {
            Ok(Ok(())) => Ok(Self { shared, thread: Some(thread) }),
            Ok(Err(e)) => {
                let _ = thread.join();
                Err(e)
            }
            Err(_) => {
                let _ = thread.join();
                anyhow::bail!("player thread exited during startup")
            }
        }
    }

    fn control(&self) -> MutexGuard<'_, Control> {
        self.shared.ctl.lock().unwrap_or_else(|e| e.into_inner())
    }

    fn send(&self, f: impl FnOnce(&mut Control)) {
        f(&mut self.control());
        self.shared.wake.notify_one();
    }

    /// Moves the playhead; playback continues from there if playing.
    pub fn seek(&self, output_ms: f64) {
        self.send(|c| c.seek = Some(output_ms));
    }

    /// Starts playback; from the start when the playhead is at the end.
    pub fn play(&self) {
        self.send(|c| c.play = true);
    }

    pub fn pause(&self) {
        self.send(|c| c.play = false);
    }

    /// Replaces the project after an edit and redraws the current frame.
    pub fn set_project(&self, project: Project) {
        self.send(|c| c.project = Some(project));
    }

    /// Replaces the assets, e.g. after [`Session::set_transcript`].
    pub fn set_assets(&self, assets: Arc<Assets>) {
        self.send(|c| c.assets = Some(assets));
    }

    /// The box frames are composed to fit, in pixels. Frames keep the project's aspect.
    pub fn set_preview_size(&self, width: u32, height: u32) {
        self.send(|c| {
            if c.preview != (width, height) {
                c.preview = (width, height);
                c.redraw = true;
            }
        });
    }

    /// The most recently composed frame and its output time. A new frame is a different
    /// `Arc` (`Arc::ptr_eq`). Never blocks on rendering.
    pub fn latest_frame(&self) -> Option<(f64, Arc<RgbaImage>)> {
        self.shared.frame.lock().unwrap_or_else(|e| e.into_inner()).clone()
    }

    /// The playhead in output milliseconds. While playing it reads the playback clock
    /// itself, so it runs ahead of the latest frame by however long that frame took.
    pub fn position_ms(&self) -> f64 {
        match &*self.shared.live.lock().unwrap_or_else(|e| e.into_inner()) {
            Some((clock, duration)) => clock.now().min(*duration),
            None => f64::from_bits(self.shared.position_bits.load(Ordering::Relaxed)),
        }
    }

    pub fn is_playing(&self) -> bool {
        self.shared.playing.load(Ordering::Relaxed)
    }

    /// The last decode or audio problem, for display.
    pub fn error(&self) -> Option<String> {
        self.shared.error.lock().unwrap_or_else(|e| e.into_inner()).clone()
    }
}

impl Drop for Player {
    fn drop(&mut self) {
        self.send(|c| c.quit = true);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

/// Output time while playing, readable from any thread.
#[derive(Clone)]
enum Clock {
    Audio { played: AudioClock, from_ms: f64 },
    Wall { start: Instant, from_ms: f64 },
}

impl Clock {
    fn now(&self) -> f64 {
        match self {
            Clock::Audio { played, from_ms } => from_ms + played.played_ms(),
            Clock::Wall { start, from_ms } => from_ms + start.elapsed().as_secs_f64() * 1000.0,
        }
    }
}

struct Playback {
    clock: Clock,
    _output: Option<AudioOutput>,
}

impl Playback {
    fn start(audio: &Arc<AudioSources>, project: &Project, from_ms: f64) -> (Self, Option<String>) {
        let wall = || Playback { clock: Clock::Wall { start: Instant::now(), from_ms }, _output: None };
        if !audio.audible(&project.audio) {
            return (wall(), None);
        }
        let mut mixer = AudioMixer::new(audio.clone(), project, from_ms);
        match AudioOutput::start(move |buf| mixer.render(buf)) {
            Ok(out) => (Playback { clock: Clock::Audio { played: out.clock(), from_ms }, _output: Some(out) }, None),
            Err(e) => (wall(), Some(format!("audio output failed: {e:#}"))),
        }
    }
}

struct Engine {
    shared: Arc<Shared>,
    screen: FrameFeed,
    camera: Option<FrameFeed>,
    audio: Arc<AudioSources>,
    source_size: (u32, u32),
}

impl Engine {
    fn run(mut self) {
        let shared = self.shared.clone();
        let mut project = Project::default();
        let mut assets: Option<Arc<Assets>> = None;
        let mut preview = (0, 0);
        let mut position = 0.0f64;
        let mut playback: Option<Playback> = None;
        let mut rendered: Option<f64> = None;
        let mut next_due = Instant::now();
        loop {
            let mut c = shared.ctl.lock().unwrap_or_else(|e| e.into_inner());
            loop {
                if c.quit {
                    return;
                }
                let changed = c.project.is_some() || c.assets.is_some() || c.redraw || c.seek.is_some() || c.play != playback.is_some();
                if changed || (playback.is_none() && rendered.is_none()) {
                    break;
                }
                match playback {
                    Some(_) => {
                        let now = Instant::now();
                        if now >= next_due {
                            break;
                        }
                        // Condvar timeouts round up to the 15.6 ms system tick on Windows, which
                        // halves the frame rate; `sleep` uses a high-resolution timer.
                        drop(c);
                        std::thread::sleep((next_due - now).min(Duration::from_millis(2)));
                        c = shared.ctl.lock().unwrap_or_else(|e| e.into_inner());
                    }
                    None => c = shared.wake.wait(c).unwrap_or_else(|e| e.into_inner()),
                }
            }
            let mut restart_clock = false;
            if let Some(p) = c.project.take() {
                restart_clock = p.clips != project.clips || p.audio != project.audio;
                project = p;
                rendered = None;
            }
            if let Some(a) = c.assets.take() {
                assets = Some(a);
                rendered = None;
            }
            if std::mem::take(&mut c.redraw) {
                rendered = None;
            }
            preview = if c.preview.0 > 0 && c.preview.1 > 0 { c.preview } else { preview };
            let seek = c.seek.take();
            let play = c.play;
            drop(c);

            let duration = project.duration_ms();
            if let Some(p) = &playback {
                position = p.clock.now();
                if let Clock::Audio { played, .. } = &p.clock {
                    if let Some(e) = played.error() {
                        *shared.error.lock().unwrap_or_else(|e| e.into_inner()) = Some(format!("audio output stopped: {e}"));
                    }
                }
            }
            if let Some(t) = seek {
                position = t;
                restart_clock = true;
            }
            position = position.clamp(0.0, duration);
            let start_clock = match (play, playback.is_some()) {
                (true, false) => {
                    if position >= duration - 1.0 {
                        position = 0.0;
                    }
                    true
                }
                (true, true) => restart_clock,
                (false, _) => {
                    playback = None;
                    false
                }
            };
            if start_clock {
                drop(playback.take());
                let (clk, err) = Playback::start(&self.audio, &project, position);
                if err.is_some() {
                    *shared.error.lock().unwrap_or_else(|e| e.into_inner()) = err;
                }
                playback = Some(clk);
            }
            if playback.is_some() && position >= duration {
                playback = None;
                shared.ctl.lock().unwrap_or_else(|e| e.into_inner()).play = false;
            }
            shared.position_bits.store(position.to_bits(), Ordering::Relaxed);
            shared.playing.store(playback.is_some(), Ordering::Relaxed);
            *shared.live.lock().unwrap_or_else(|e| e.into_inner()) = playback.as_ref().map(|p| (p.clock.clone(), duration));

            let Some(assets) = assets.as_ref() else { continue };
            if rendered != Some(position) && preview.0 > 0 {
                next_due = Instant::now() + FRAME_INTERVAL;
                match self.render(&project, assets, position, preview) {
                    Ok(img) => {
                        let old = shared.frame.lock().unwrap_or_else(|e| e.into_inner()).replace((position, Arc::new(img)));
                        if let Some(frame) = old.and_then(|(_, f)| Arc::try_unwrap(f).ok()) {
                            recycle_frame(frame);
                        }
                    }
                    Err(e) => *shared.error.lock().unwrap_or_else(|e| e.into_inner()) = Some(format!("{e:#}")),
                }
                rendered = Some(position);
            }
        }
    }

    fn render(&mut self, project: &Project, assets: &Assets, output_ms: f64, preview: (u32, u32)) -> anyhow::Result<RgbaImage> {
        let source_ms = project.source_time(output_ms);
        let screen = self.screen.frame_at(source_ms)?.context("screen.mp4 has no frames")?;
        let camera = match self.camera.as_mut() {
            Some(feed) => feed.frame_at(source_ms)?,
            None => None,
        };
        let size = fit(project.output_size(self.source_size), preview);
        let inputs = FrameInputs { screen: screen.rgba(), camera: camera.as_ref().map(|c| c.rgba()), output_ms };
        Ok(compose_frame(project, assets, &inputs, size))
    }
}

/// The largest even size with `out`'s aspect inside `bounds`.
pub(crate) fn fit(out: (u32, u32), bounds: (u32, u32)) -> (u32, u32) {
    let k = (bounds.0 as f64 / out.0 as f64).min(bounds.1 as f64 / out.1 as f64);
    let even = |v: u32| (((v as f64 * k).floor() as u32) & !1).max(2);
    (even(out.0), even(out.1))
}

#[cfg(test)]
mod tests {
    use super::fit;

    #[test]
    fn fit_keeps_aspect_and_even_sides() {
        assert_eq!(fit((1920, 1080), (960, 960)), (960, 540));
        assert_eq!(fit((1080, 1920), (1000, 600)), (336, 600));
        assert_eq!(fit((2049, 1001), (1025, 1025)), (1024, 500));
    }
}
