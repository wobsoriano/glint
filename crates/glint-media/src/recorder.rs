//! A recording session: one screen master plus optional audio, camera and input tracks, all
//! on one media clock, written into a session directory.

use crate::audio::{AudioSource, AudioTrack};
use crate::clock::MediaClock;
use crate::devices::{displays, DeviceId, Rect};
use crate::screen::{ScreenSource, ScreenTrack};
use anyhow::{bail, Context};
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

/// What to record.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CaptureSource {
    /// A display by index into [`displays()`].
    Display(usize),
    /// A top-level window by `HWND`.
    Window(isize),
    /// A rectangle of a display, in that display's own physical pixels.
    Area { display: usize, rect: Rect },
}

/// Frame rates the screen master can be written at.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Fps {
    F30,
    F60,
}

impl Fps {
    pub fn value(self) -> u32 {
        match self {
            Fps::F30 => 30,
            Fps::F60 => 60,
        }
    }
}

/// Everything [`Recorder::start`] needs.
#[derive(Clone, Debug)]
pub struct RecordOptions {
    pub source: CaptureSource,
    /// Created if missing. The session package files are written directly into it.
    pub session_dir: PathBuf,
    pub fps: Fps,
    /// Record what the default output device plays to `system.wav`.
    pub system_audio: bool,
    /// Record this microphone to `mic.wav`.
    pub mic: Option<DeviceId>,
    /// Record this camera to `camera.mp4`.
    pub camera: Option<DeviceId>,
    /// Record pointer, clicks, shortcuts and cursor sprites to `input.json`.
    pub record_input: bool,
}

/// What a finished recording produced.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RecordSummary {
    /// Files inside the session directory, screen master first.
    pub files: Vec<PathBuf>,
    /// Media duration, paused spans excluded.
    pub duration_ms: u64,
    /// Total size of `files` on disk.
    pub bytes: u64,
    /// Pixel size of the screen master.
    pub width: u32,
    pub height: u32,
    /// The screen encoder ran on the GPU.
    pub hardware_encoder: bool,
    /// Optional tracks that were skipped or cut short, and anything else worth telling the user.
    pub warnings: Vec<String>,
}

/// A running recording. Drop without `stop` or `cancel` leaves the files as they are.
pub struct Recorder {
    clock: Arc<MediaClock>,
    session_dir: PathBuf,
    screen: ScreenTrack,
    system: Option<AudioTrack>,
    mic: Option<AudioTrack>,
    width: u32,
    height: u32,
    warnings: Vec<String>,
}

impl Recorder {
    /// Starts every track, then starts media time. The screen master is required; a missing
    /// or failing optional device becomes a warning in the summary.
    pub fn start(opts: RecordOptions) -> anyhow::Result<Self> {
        std::fs::create_dir_all(&opts.session_dir).with_context(|| format!("create {}", opts.session_dir.display()))?;
        let clock = Arc::new(MediaClock::paused());
        let mut warnings = Vec::new();

        let source = resolve_source(opts.source)?;
        let (screen, geometry) = ScreenTrack::start(source, opts.fps.value(), &opts.session_dir.join("screen.mp4"), clock.clone())?;
        warnings.extend(screen.warnings.iter().cloned());

        let system = if opts.system_audio {
            start_optional(&mut warnings, "system audio", || {
                AudioTrack::start(AudioSource::Loopback, &opts.session_dir.join("system.wav"), 2, clock.clone())
            })
        } else {
            None
        };
        let mic = opts.mic.as_ref().and_then(|id| {
            start_optional(&mut warnings, "microphone", || {
                AudioTrack::start(AudioSource::Microphone(Some(id.clone())), &opts.session_dir.join("mic.wav"), 1, clock.clone())
            })
        });

        clock.resume();
        Ok(Self { clock, session_dir: opts.session_dir, screen, system, mic, width: geometry.width, height: geometry.height, warnings })
    }

    /// Freezes media time; every track stops writing until [`resume`](Self::resume).
    pub fn pause(&self) {
        self.clock.pause();
    }

    pub fn resume(&self) {
        self.clock.resume();
    }

    pub fn is_paused(&self) -> bool {
        self.clock.is_paused()
    }

    /// Media time recorded so far.
    pub fn elapsed(&self) -> Duration {
        self.clock.now()
    }

    /// RMS of the last microphone buffer, when a microphone is recording.
    pub fn mic_level(&self) -> Option<f32> {
        self.mic.as_ref().map(|m| m.level().rms())
    }

    pub fn session_dir(&self) -> &Path {
        &self.session_dir
    }

    /// Finalizes every file and returns what was written.
    pub fn stop(self) -> anyhow::Result<RecordSummary> {
        self.clock.pause();
        let duration_ms = self.clock.now_ms();
        let mut warnings = self.warnings;
        let mut files = Vec::new();

        for track in [self.system, self.mic].into_iter().flatten() {
            let path = track.path.clone();
            match track.stop() {
                Ok(_) => files.push(path),
                Err(e) => warnings.push(format!("{}: {e:#}", path.display())),
            }
        }
        let screen = self.screen.stop().context("screen track")?;
        if screen.source_closed {
            warnings.push("the captured window closed; the recording ends there".into());
        }
        files.insert(0, self.session_dir.join("screen.mp4"));

        let bytes = files.iter().map(|f| std::fs::metadata(f).map(|m| m.len()).unwrap_or(0)).sum();
        Ok(RecordSummary {
            files,
            duration_ms,
            bytes,
            width: self.width,
            height: self.height,
            hardware_encoder: screen.hardware,
            warnings,
        })
    }

    /// Stops and deletes everything this recording wrote, including the session directory
    /// when it is empty afterwards.
    pub fn cancel(self) -> anyhow::Result<()> {
        let dir = self.session_dir.clone();
        let summary = self.stop()?;
        for f in summary.files {
            let _ = std::fs::remove_file(f);
        }
        let _ = std::fs::remove_dir(&dir);
        Ok(())
    }
}

fn start_optional<T>(warnings: &mut Vec<String>, what: &str, start: impl FnOnce() -> anyhow::Result<T>) -> Option<T> {
    match start() {
        Ok(t) => Some(t),
        Err(e) => {
            warnings.push(format!("{what} not recorded: {e:#}"));
            None
        }
    }
}

fn resolve_source(source: CaptureSource) -> anyhow::Result<ScreenSource> {
    match source {
        CaptureSource::Window(hwnd) => Ok(ScreenSource::Window { hwnd }),
        CaptureSource::Display(i) | CaptureSource::Area { display: i, .. } => {
            let list = displays()?;
            let Some(display) = list.get(i) else { bail!("display {i} does not exist ({} attached)", list.len()) };
            let crop = match source {
                CaptureSource::Area { rect, .. } => Some(rect),
                _ => None,
            };
            Ok(ScreenSource::Monitor { hmonitor: display.hmonitor, crop })
        }
    }
}
