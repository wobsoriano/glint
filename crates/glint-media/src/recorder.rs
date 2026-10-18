use crate::devices::{DeviceId, Rect};
use std::path::PathBuf;
use std::sync::mpsc::SyncSender;

pub use crate::backend::recorder::Recorder;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum CaptureSource {
    /// A display by index into [`displays()`].
    Display(usize),
    /// A top-level window by platform id: `HWND` on Windows, `CGWindowID` on macOS.
    Window(isize),
    /// A rectangle of a display, in that display's own physical pixels.
    Area { display: usize, rect: Rect },
}

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

#[derive(Clone, Debug, PartialEq, Eq)]
pub struct RecordSummary {
    /// Files inside the session directory, screen master first.
    pub files: Vec<PathBuf>,
    /// Media duration, paused spans excluded.
    pub duration_ms: u64,
    pub bytes: u64,
    /// Pixel size of the screen master.
    pub width: u32,
    pub height: u32,
    pub hardware_encoder: bool,
    /// Optional tracks that were skipped or cut short, and anything else worth telling the user.
    pub warnings: Vec<String>,
}
