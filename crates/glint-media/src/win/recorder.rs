use super::audio::{AudioSource, AudioTrack};
use super::camera::CameraTrack;
use super::hooks::{InputTrack, PointerMap};
use super::screen::{ScreenSource, ScreenTrack};
use crate::clock::MediaClock;
use crate::devices::{displays, DisplayInfo};
use crate::input::InputTimeline;
use crate::recorder::{CaptureSource, RecordOptions, RecordSummary};
use anyhow::Context;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;
use windows::Win32::Foundation::HWND;
use windows::Win32::Graphics::Gdi::{MonitorFromWindow, MONITOR_DEFAULTTONEAREST};

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
                InputTrack::start(map, [geometry.width, geometry.height], resolved.scale, &opts.session_dir, clock.clone(), opts.ignored_keys.clone())
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

    /// False once the screen capture has ended on its own, because its window closed or it
    /// failed. [`stop`](Self::stop) still finalizes what was written and says why in `warnings`.
    pub fn is_live(&self) -> bool {
        self.screen.is_live()
    }

    /// Finalizes every file and returns what was written. A track that failed, the screen
    /// master included, keeps what it wrote before failing and is reported in `warnings`.
    pub fn stop(self) -> RecordSummary {
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
        files.insert(0, self.screen.path.clone());
        let hardware_encoder = match self.screen.stop() {
            Ok(screen) => {
                if screen.source_closed {
                    warnings.push("the captured window closed; the recording ends there".into());
                }
                screen.hardware
            }
            Err(e) => {
                warnings.push(format!("the screen capture failed; the recording ends there: {e:#}"));
                false
            }
        };

        let bytes = files.iter().map(|f| std::fs::metadata(f).map(|m| m.len()).unwrap_or(0)).sum();
        RecordSummary { files, duration_ms, bytes, width: self.width, height: self.height, hardware_encoder, warnings }
    }

    /// Stops and deletes everything this recording wrote, including the session directory
    /// when it is empty afterwards.
    pub fn cancel(self) {
        let dir = self.session_dir.clone();
        let summary = self.stop();
        for f in summary.files {
            let _ = std::fs::remove_file(f);
        }
        let _ = std::fs::remove_dir(dir.join("cursors"));
        let _ = std::fs::remove_dir(&dir);
    }
}

struct Resolved {
    screen: ScreenSource,
    origin: (i32, i32),
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
            let scale = list.iter().find(|d| d.handle == monitor).map(|d| d.scale).unwrap_or(1.0);
            Ok(Resolved { screen: ScreenSource::Window { hwnd }, origin: (0, 0), scale })
        }
        CaptureSource::Display(i) => {
            let d = display(&list, i)?;
            Ok(Resolved { screen: ScreenSource::Monitor { hmonitor: d.handle, crop: None }, origin: (d.rect.x, d.rect.y), scale: d.scale })
        }
        CaptureSource::Area { display: i, rect } => {
            let d = display(&list, i)?;
            Ok(Resolved {
                screen: ScreenSource::Monitor { hmonitor: d.handle, crop: Some(rect) },
                origin: (d.rect.x + rect.x, d.rect.y + rect.y),
                scale: d.scale,
            })
        }
    }
}

fn display(list: &[DisplayInfo], i: usize) -> anyhow::Result<&DisplayInfo> {
    list.get(i).with_context(|| format!("display {i} does not exist ({} attached)", list.len()))
}
