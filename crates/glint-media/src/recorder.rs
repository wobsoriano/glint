//! A recording session: one screen master plus optional audio, camera and input tracks, all
//! on one media clock, written into a session directory.

use crate::audio::{AudioSource, AudioTrack};
use crate::camera::CameraTrack;
use crate::clock::MediaClock;
use crate::devices::{displays, DeviceId, DisplayInfo, Rect};
use crate::hooks::{InputTrack, PointerMap};
use crate::input::InputTimeline;
use crate::screen::{ScreenSource, ScreenTrack};
use anyhow::Context;
use std::path::{Path, PathBuf};
use std::sync::mpsc::SyncSender;
use std::sync::Arc;
use std::time::Duration;
use windows::Win32::Foundation::HWND;
use windows::Win32::Graphics::Gdi::{MonitorFromWindow, MONITOR_DEFAULTTONEAREST};

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
    /// Also offer every microphone buffer (48 kHz mono f32, paused or not) to this channel,
    /// for live speech recognition. A full channel drops the buffer; the recording never waits.
    pub mic_tap: Option<SyncSender<Vec<f32>>>,
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
    camera: Option<CameraTrack>,
    input: Option<InputTrack>,
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

        let resolved = resolve_source(opts.source)?;
        let (screen, geometry) =
            ScreenTrack::start(resolved.screen, opts.fps.value(), &opts.session_dir.join("screen.mp4"), clock.clone())?;
        warnings.extend(screen.warnings.iter().cloned());

        let input = if opts.record_input {
            let map = match (resolved.screen, geometry.window_bounds) {
                (ScreenSource::Window { hwnd }, Some(bounds)) => PointerMap::Window { hwnd, bounds },
                _ => PointerMap::Fixed { x: resolved.origin.0, y: resolved.origin.1 },
            };
            start_optional(&mut warnings, "input", || {
                InputTrack::start(map, [geometry.width, geometry.height], resolved.scale, &opts.session_dir, clock.clone())
            })
        } else {
            None
        };

        let system = if opts.system_audio {
            start_optional(&mut warnings, "system audio", || {
                AudioTrack::start(AudioSource::Loopback, &opts.session_dir.join("system.wav"), 2, clock.clone(), None)
            })
        } else {
            None
        };
        let mic = opts.mic.as_ref().and_then(|id| {
            start_optional(&mut warnings, "microphone", || {
                AudioTrack::start(
                    AudioSource::Microphone(Some(id.clone())),
                    &opts.session_dir.join("mic.wav"),
                    1,
                    clock.clone(),
                    opts.mic_tap.clone(),
                )
            })
        });

        let camera = opts.camera.as_ref().and_then(|id| {
            start_optional(&mut warnings, "camera", || CameraTrack::start(id, &opts.session_dir.join("camera.mp4"), clock.clone()))
        });

        clock.resume();
        Ok(Self {
            clock,
            session_dir: opts.session_dir,
            screen,
            system,
            mic,
            camera,
            input,
            width: geometry.width,
            height: geometry.height,
            warnings,
        })
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

    /// Optional tracks that failed to start, so the UI can warn while recording continues.
    pub fn warnings(&self) -> &[String] {
        &self.warnings
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

        if let Some(input) = self.input {
            match input.stop().and_then(|timeline| {
                timeline.save(&self.session_dir)?;
                Ok(timeline)
            }) {
                Ok(timeline) => {
                    files.push(self.session_dir.join(InputTimeline::FILE));
                    files.extend(timeline.cursors.iter().map(|c| self.session_dir.join(&c.file)));
                }
                Err(e) => warnings.push(format!("input.json: {e:#}")),
            }
        }
        for track in [self.system, self.mic].into_iter().flatten() {
            let path = track.path.clone();
            match track.stop() {
                Ok(_) => files.push(path),
                Err(e) => warnings.push(format!("{}: {e:#}", path.display())),
            }
        }
        if let Some(camera) = self.camera {
            let path = camera.path.clone();
            match camera.stop() {
                Ok(_) => files.push(path),
                Err(e) => warnings.push(format!("{}: {e:#}", path.display())),
            }
        }
        let screen_path = self.screen.path.clone();
        let screen = self.screen.stop().context("screen track")?;
        if screen.source_closed {
            warnings.push("the captured window closed; the recording ends there".into());
        }
        files.insert(0, screen_path);

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
        let _ = std::fs::remove_dir(dir.join("cursors"));
        let _ = std::fs::remove_dir(&dir);
        Ok(())
    }
}

/// A capture source turned into handles, plus what the input track needs to map positions.
struct Resolved {
    screen: ScreenSource,
    /// Virtual-desktop origin of the captured pixels (unused for windows, which move).
    origin: (i32, i32),
    /// DPI scale of the display the source is on.
    scale: f32,
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

fn resolve_source(source: CaptureSource) -> anyhow::Result<Resolved> {
    let list = displays()?;
    match source {
        CaptureSource::Window(hwnd) => {
            let monitor = unsafe { MonitorFromWindow(HWND(hwnd as *mut _), MONITOR_DEFAULTTONEAREST) }.0 as isize;
            let scale = list.iter().find(|d| d.hmonitor == monitor).map(|d| d.scale).unwrap_or(1.0);
            Ok(Resolved { screen: ScreenSource::Window { hwnd }, origin: (0, 0), scale })
        }
        CaptureSource::Display(i) => {
            let d = display(&list, i)?;
            Ok(Resolved { screen: ScreenSource::Monitor { hmonitor: d.hmonitor, crop: None }, origin: (d.rect.x, d.rect.y), scale: d.scale })
        }
        CaptureSource::Area { display: i, rect } => {
            let d = display(&list, i)?;
            Ok(Resolved {
                screen: ScreenSource::Monitor { hmonitor: d.hmonitor, crop: Some(rect) },
                origin: (d.rect.x + rect.x, d.rect.y + rect.y),
                scale: d.scale,
            })
        }
    }
}

fn display(list: &[DisplayInfo], i: usize) -> anyhow::Result<&DisplayInfo> {
    list.get(i).with_context(|| format!("display {i} does not exist ({} attached)", list.len()))
}
