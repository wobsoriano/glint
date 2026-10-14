//! Persistent user preferences, stored as JSON in `<data_dir>\settings.json`.

pub use crate::annotate::RedactStyle;
use crate::cards::CardLayout;
use crate::style::Style;
use serde::{Deserialize, Serialize};
use std::path::{Path, PathBuf};

/// What happens after every screenshot. Each toggle is independent.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct AfterShot {
    pub preview: bool,
    pub copy: bool,
    pub save: bool,
    pub edit: bool,
    pub pin: bool,
}

impl Default for AfterShot {
    fn default() -> Self {
        Self { preview: true, copy: true, save: false, edit: false, pin: false }
    }
}

/// What happens after every recording.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct AfterRecording {
    pub preview: bool,
    pub copy: bool,
    pub save: bool,
    pub studio: bool,
}

impl Default for AfterRecording {
    fn default() -> Self {
        Self { preview: true, copy: false, save: false, studio: false }
    }
}

/// The kind of source the recording picker starts on.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum RecordTarget {
    Display,
    Window,
    Area,
}

/// The recording picker's last choices.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct RecordPrefs {
    pub target: RecordTarget,
    /// Last recorded area, physical virtual-desktop pixels: x, y, w, h.
    pub area: Option<[i32; 4]>,
    /// Device ids; `None` records without that input.
    pub camera: Option<String>,
    pub mic: Option<String>,
    pub system_audio: bool,
    /// Start timer in seconds; 0 starts at once.
    pub countdown: u32,
    pub teleprompter: bool,
    pub script: String,
    pub prompter_lines: u32,
}

impl Default for RecordPrefs {
    fn default() -> Self {
        Self {
            target: RecordTarget::Display,
            area: None,
            camera: None,
            mic: None,
            system_audio: true,
            countdown: 3,
            teleprompter: false,
            script: String::new(),
            prompter_lines: 3,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Side {
    Left,
    Right,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct OverlayPrefs {
    pub side: Side,
    /// Seconds before an untouched card closes; 0 keeps cards until dismissed.
    pub auto_close_seconds: u32,
    pub dismiss_after_drag: bool,
    pub layout: CardLayout,
}

impl Default for OverlayPrefs {
    fn default() -> Self {
        Self { side: Side::Left, auto_close_seconds: 0, dismiss_after_drag: true, layout: CardLayout::default() }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum ExportFormat {
    Png,
    Jpeg,
}

impl ExportFormat {
    pub fn extension(self) -> &'static str {
        match self {
            ExportFormat::Png => "png",
            ExportFormat::Jpeg => "jpg",
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum LibraryView {
    Grid,
    List,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum SortKey {
    Captured,
    Name,
    Modified,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct LibraryPrefs {
    pub view: LibraryView,
    pub sort: SortKey,
    pub inspector: bool,
}

impl Default for LibraryPrefs {
    fn default() -> Self {
        Self { view: LibraryView::Grid, sort: SortKey::Captured, inspector: true }
    }
}

/// Categories of sensitive data the auto-redactor looks for.
#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct RedactRules {
    pub emails: bool,
    pub phones: bool,
    pub credit_cards: bool,
    pub secrets: bool,
    pub ip_addresses: bool,
    pub ids: bool,
    pub urls_with_tokens: bool,
}

impl Default for RedactRules {
    fn default() -> Self {
        Self {
            emails: true,
            phones: true,
            credit_cards: true,
            secrets: true,
            ip_addresses: true,
            ids: true,
            urls_with_tokens: true,
        }
    }
}

/// Every action that can have a global shortcut.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum HotkeyAction {
    Fullscreen,
    Window,
    Area,
    PreviousArea,
    Text,
    Recording,
    Library,
}

impl HotkeyAction {
    pub const ALL: [HotkeyAction; 7] = [
        HotkeyAction::Area,
        HotkeyAction::Fullscreen,
        HotkeyAction::Window,
        HotkeyAction::PreviousArea,
        HotkeyAction::Text,
        HotkeyAction::Recording,
        HotkeyAction::Library,
    ];

    pub fn label(self) -> &'static str {
        match self {
            HotkeyAction::Fullscreen => "Capture full screen",
            HotkeyAction::Window => "Capture window",
            HotkeyAction::Area => "Capture area",
            HotkeyAction::PreviousArea => "Capture previous area",
            HotkeyAction::Text => "Capture text",
            HotkeyAction::Recording => "Record screen",
            HotkeyAction::Library => "Open Library",
        }
    }
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Hotkeys {
    pub area: String,
    pub fullscreen: String,
    pub window: String,
    pub previous_area: String,
    pub text: String,
    pub recording: String,
    pub library: String,
}

impl Default for Hotkeys {
    fn default() -> Self {
        Self {
            area: "Ctrl+Shift+4".into(),
            fullscreen: "Ctrl+Shift+3".into(),
            window: "Ctrl+Shift+5".into(),
            previous_area: "Ctrl+Shift+6".into(),
            text: "Ctrl+Shift+2".into(),
            recording: "Ctrl+Shift+7".into(),
            library: "Ctrl+Shift+L".into(),
        }
    }
}

impl Hotkeys {
    pub fn get(&self, a: HotkeyAction) -> &String {
        match a {
            HotkeyAction::Fullscreen => &self.fullscreen,
            HotkeyAction::Window => &self.window,
            HotkeyAction::Area => &self.area,
            HotkeyAction::PreviousArea => &self.previous_area,
            HotkeyAction::Text => &self.text,
            HotkeyAction::Recording => &self.recording,
            HotkeyAction::Library => &self.library,
        }
    }

    pub fn get_mut(&mut self, a: HotkeyAction) -> &mut String {
        match a {
            HotkeyAction::Fullscreen => &mut self.fullscreen,
            HotkeyAction::Window => &mut self.window,
            HotkeyAction::Area => &mut self.area,
            HotkeyAction::PreviousArea => &mut self.previous_area,
            HotkeyAction::Text => &mut self.text,
            HotkeyAction::Recording => &mut self.recording,
            HotkeyAction::Library => &mut self.library,
        }
    }
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Settings {
    pub style: Style,
    pub launch_at_login: bool,
    pub include_own_windows: bool,
    pub timer_seconds: u32,
    pub format: ExportFormat,
    pub jpeg_quality: u8,
    /// The export folder.
    pub save_dir: PathBuf,
    pub save_without_dialog: bool,
    pub window_shadow: bool,
    pub show_loupe: bool,
    pub after_shot: AfterShot,
    pub after_recording: AfterRecording,
    pub record: RecordPrefs,
    pub overlay: OverlayPrefs,
    pub hotkeys: Hotkeys,
    pub auto_redact: bool,
    pub redact_style: RedactStyle,
    pub redact_rules: RedactRules,
    pub last_area: Option<[i32; 4]>,
    pub library: LibraryPrefs,
}

impl Default for Settings {
    fn default() -> Self {
        let save_dir = dirs::picture_dir()
            .or_else(dirs::home_dir)
            .unwrap_or_else(|| PathBuf::from("."))
            .join("Glint");
        Self {
            style: Style::default(),
            launch_at_login: false,
            include_own_windows: false,
            timer_seconds: 5,
            format: ExportFormat::Png,
            jpeg_quality: 85,
            save_dir,
            save_without_dialog: true,
            window_shadow: true,
            show_loupe: true,
            after_shot: AfterShot::default(),
            after_recording: AfterRecording::default(),
            record: RecordPrefs::default(),
            overlay: OverlayPrefs::default(),
            hotkeys: Hotkeys::default(),
            auto_redact: false,
            redact_style: RedactStyle::Blur,
            redact_rules: RedactRules::default(),
            last_area: None,
            library: LibraryPrefs::default(),
        }
    }
}

/// Root for settings, Library and models. `GLINT_DATA_DIR` overrides `%APPDATA%\Glint`.
pub fn data_dir() -> PathBuf {
    std::env::var_os("GLINT_DATA_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|| dirs::config_dir().unwrap_or_else(|| PathBuf::from(".")).join("Glint"))
}

pub fn path() -> PathBuf {
    data_dir().join("settings.json")
}

impl Settings {
    pub fn load() -> Self {
        Self::load_from(&path())
    }

    pub fn save(&self) {
        self.save_to(&path());
    }

    fn load_from(p: &Path) -> Self {
        let mut s: Settings = std::fs::read_to_string(p)
            .ok()
            .and_then(|s| serde_json::from_str(s.trim_start_matches('\u{feff}')).ok())
            .unwrap_or_default();
        s.overlay.layout = s.overlay.layout.normalized();
        s
    }

    fn save_to(&self, p: &Path) {
        if let Some(dir) = p.parent() {
            let _ = std::fs::create_dir_all(dir);
        }
        if let Ok(json) = serde_json::to_string_pretty(self) {
            let _ = std::fs::write(p, json);
        }
    }
}

impl gpui::Global for Settings {}

#[cfg(test)]
mod tests {
    use super::*;

    fn temp_dir(name: &str) -> PathBuf {
        let dir = std::env::temp_dir().join(format!("glint-settings-{name}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&dir);
        std::fs::create_dir_all(&dir).unwrap();
        dir
    }

    #[test]
    fn corrupt_file_is_kept_aside_and_defaults_load() {
        let dir = temp_dir("corrupt");
        let p = dir.join("settings.json");
        std::fs::write(&p, "{ not json").unwrap();
        assert_eq!(Settings::load_from(&p), Settings::default());
        assert_eq!(std::fs::read_to_string(dir.join("settings.json.broken")).unwrap(), "{ not json");
        assert!(!p.exists());
        std::fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn save_round_trips_through_a_replaced_file() {
        let dir = temp_dir("save");
        let p = dir.join("settings.json");
        std::fs::write(&p, "old").unwrap();
        let mut s = Settings::default();
        s.timer_seconds = 9;
        s.hotkeys.area = "Ctrl+Alt+A".into();
        s.save_to(&p);
        let back: Settings = serde_json::from_str(&std::fs::read_to_string(&p).unwrap()).unwrap();
        assert_eq!(back.timer_seconds, 9);
        assert_eq!(back.hotkeys.area, "Ctrl+Alt+A");
        assert_eq!(back, s);
        let names: Vec<_> = std::fs::read_dir(&dir).unwrap().map(|e| e.unwrap().file_name()).collect();
        assert_eq!(names, vec![std::ffi::OsString::from("settings.json")]);
        std::fs::remove_dir_all(&dir).unwrap();
    }
}
