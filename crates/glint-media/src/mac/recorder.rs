use super::audio::AudioTrack;
use super::camera::CameraTrack;
use super::hooks::{InputTrack, PointerMap};
use super::screen::{ScreenSource, ScreenTrack};
use super::space;
use crate::clock::MediaClock;
use crate::devices::{displays, DisplayInfo};
use crate::input::InputTimeline;
use crate::recorder::{CaptureSource, RecordOptions, RecordSummary};
use anyhow::Context;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::time::Duration;

/// A running recording. Drop without `stop` or `cancel` leaves the files as they are.
pub struct Recorder {
    clock: Arc<MediaClock>,
    session_dir: PathBuf,
    screen: ScreenTrack,
    mic: Option<AudioTrack>,
    camera: Option<CameraTrack>,
    input: Option<InputTrack>,
    width: u32,
    height: u32,
    warnings: Vec<String>,
}

impl Recorder {
    /// Starts every track, then starts media time. The screen master is required; a missing
    /// or failing optional device becomes a warning in the summary. System audio comes from
    /// the screen's ScreenCaptureKit stream.
    pub fn start(opts: RecordOptions) -> anyhow::Result<Self> {
        std::fs::create_dir_all(&opts.session_dir).with_context(|| format!("create {}", opts.session_dir.display()))?;
        let clock = Arc::new(MediaClock::paused());
        let mut warnings = Vec::new();

        let resolved = resolve_source(opts.source)?;
        let [width, height] = resolved.size;
        let system = opts.system_audio.then(|| opts.session_dir.join("system.wav"));
        let screen = ScreenTrack::start(
            resolved.screen,
            resolved.size,
            opts.fps.value(),
            &opts.session_dir.join("screen.mp4"),
            system.as_deref(),
            clock.clone(),
        )?;
        warnings.extend(screen.warnings.iter().cloned());

        let input = if opts.record_input {
            start_optional(&mut warnings, "input", || {
                InputTrack::start(resolved.pointer, resolved.size, resolved.scale, &opts.session_dir, clock.clone(), opts.ignored_keys.clone())
            })
        } else {
            None
        };

        let mic = opts.mic.as_ref().and_then(|id| {
            start_optional(&mut warnings, "microphone", || {
                AudioTrack::start_mic(id, &opts.session_dir.join("mic.wav"), clock.clone(), opts.mic_tap.clone())
            })
        });

        let camera = opts.camera.as_ref().and_then(|id| {
            start_optional(&mut warnings, "camera", || CameraTrack::start(id, &opts.session_dir.join("camera.mp4"), clock.clone()))
        });

        clock.resume();
        Ok(Self { clock, session_dir: opts.session_dir, screen, mic, camera, input, width, height, warnings })
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
        let mut input_files = Vec::new();

        if let Some(input) = self.input {
            match input.stop().and_then(|timeline| {
                timeline.save(&self.session_dir)?;
                Ok(timeline)
            }) {
                Ok(timeline) => {
                    input_files.push(self.session_dir.join(InputTimeline::FILE));
                    input_files.extend(timeline.cursors.iter().map(|c| self.session_dir.join(&c.file)));
                }
                Err(e) => warnings.push(format!("input.json: {e:#}")),
            }
        }
        let mic = self.mic.map(|track| (track.path.clone(), track.stop().map(drop)));
        let camera = self.camera.map(|track| (track.path.clone(), track.stop().map(drop)));
        let (screen_path, system_path) = (self.screen.path.clone(), self.screen.system_path.clone());
        let end = self.screen.stop();
        let system = system_path.zip(end.system).map(|(path, stats)| (path, stats.map(drop)));

        let mut files = vec![screen_path];
        files.extend(input_files);
        for (path, result) in [system, mic, camera].into_iter().flatten() {
            match result {
                Ok(()) => files.push(path),
                Err(e) => warnings.push(format!("{}: {e:#}", path.display())),
            }
        }
        match end.screen {
            Ok(screen) if screen.source_closed => warnings.push("the captured window closed; the recording ends there".into()),
            Ok(_) => {}
            Err(e) => warnings.push(format!("the screen capture failed; the recording ends there: {e:#}")),
        }

        let bytes = files.iter().map(|f| std::fs::metadata(f).map(|m| m.len()).unwrap_or(0)).sum();
        RecordSummary { files, duration_ms, bytes, width: self.width, height: self.height, hardware_encoder: true, warnings }
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

/// A capture source resolved to what ScreenCaptureKit and the input track need.
struct Resolved {
    screen: ScreenSource,
    /// Pixel size of the screen master, even.
    size: [u32; 2],
    pointer: PointerMap,
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
    let even = |w: u32, h: u32| [w & !1, h & !1];
    match source {
        CaptureSource::Display(i) => {
            let d = display(&list, i)?;
            Ok(Resolved {
                screen: ScreenSource::Display { id: d.handle as u32, crop: None },
                size: even(d.rect.width, d.rect.height),
                pointer: PointerMap::Fixed { x: d.rect.x, y: d.rect.y },
                scale: d.scale,
            })
        }
        CaptureSource::Area { display: i, rect } => {
            let d = display(&list, i)?;
            let size = even(rect.width, rect.height);
            let (x, y) = (d.rect.x + rect.x, d.rect.y + rect.y);
            let [px, py, pw, ph] = space::rect_to_points(&list, [x, y, size[0] as i32, size[1] as i32]);
            let [dx, dy, _, _] = space::point_bounds(&list, d);
            Ok(Resolved {
                screen: ScreenSource::Display { id: d.handle as u32, crop: Some([px - dx, py - dy, pw, ph]) },
                size,
                pointer: PointerMap::Fixed { x, y },
                scale: d.scale,
            })
        }
        CaptureSource::Window(id) => {
            let id = u32::try_from(id).with_context(|| format!("{id} is not a window id"))?;
            let bounds = super::window::bounds(id).with_context(|| format!("window {id} is gone"))?;
            let [x, y, w, h] = space::rect_to_pixels(&list, bounds);
            let scale = space::display_at(&list, x + w / 2, y + h / 2).map_or(1.0, |d| d.scale);
            Ok(Resolved {
                screen: ScreenSource::Window { id },
                size: even(w.max(0) as u32, h.max(0) as u32),
                pointer: PointerMap::Window { id },
                scale,
            })
        }
    }
}

fn display(list: &[DisplayInfo], i: usize) -> anyhow::Result<&DisplayInfo> {
    list.get(i).with_context(|| format!("display {i} does not exist ({} attached)", list.len()))
}
