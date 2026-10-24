pub use crate::annotate::RedactStyle;
use crate::cards::CardLayout;
use crate::style::Style;
use anyhow::Context as _;
use serde::{Deserialize, Serialize};
use std::io::Write as _;
use std::path::{Path, PathBuf};

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

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum RecordTarget {
    Display,
    Window,
    Area,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct RecordPrefs {
    pub target: RecordTarget,
    pub area: Option<[i32; 4]>,
    pub camera: Option<String>,
    pub mic: Option<String>,
    pub system_audio: bool,
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
        let mut s: Settings = match std::fs::read_to_string(p) {
            Ok(text) => serde_json::from_str(text.trim_start_matches('\u{feff}')).unwrap_or_else(|e| {
                let broken = p.with_extension("json.broken");
                match std::fs::rename(p, &broken) {
                    Ok(()) => eprintln!("{} is not valid ({e}); moved it to {} and using default settings", p.display(), broken.display()),
                    Err(re) => eprintln!("{} is not valid ({e}) and could not be moved aside ({re}); using default settings", p.display()),
                }
                Settings::default()
            }),
            Err(_) => Settings::default(),
        };
        s.overlay.layout = s.overlay.layout.normalized();
        s
    }

    fn save_to(&self, p: &Path) {
        if let Err(e) = self.write_atomic(p) {
            eprintln!("Settings were not saved: {e:#}");
        }
    }

    fn write_atomic(&self, p: &Path) -> anyhow::Result<()> {
        if let Some(dir) = p.parent() {
            std::fs::create_dir_all(dir).with_context(|| format!("creating {}", dir.display()))?;
        }
        let json = serde_json::to_string_pretty(self)?;
        let tmp = p.with_extension("json.tmp");
        let mut f = std::fs::File::create(&tmp).with_context(|| format!("creating {}", tmp.display()))?;
        f.write_all(json.as_bytes()).and_then(|()| f.sync_all()).with_context(|| format!("writing {}", tmp.display()))?;
        drop(f);
        std::fs::rename(&tmp, p).with_context(|| format!("replacing {}", p.display()))
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

    fn env_dir(var: &str) -> PathBuf {
        PathBuf::from(std::env::var_os(var).unwrap_or_else(|| panic!("set {var}")))
    }

    #[test]
    #[ignore = "writes settings fixtures for the Go port into GLINT_GO_FIXTURES"]
    fn go_fixtures() {
        use crate::cards::CardAction;
        let dir = env_dir("GLINT_GO_FIXTURES");
        std::fs::create_dir_all(&dir).unwrap();
        let write = |name: &str, s: &Settings| std::fs::write(dir.join(name), serde_json::to_string_pretty(s).unwrap()).unwrap();
        write("settings-default.json", &Settings::default());

        let mut s = Settings::default();
        s.launch_at_login = true;
        s.include_own_windows = true;
        s.timer_seconds = 10;
        s.format = ExportFormat::Jpeg;
        s.jpeg_quality = 70;
        s.save_dir = PathBuf::from("D:\\Shots <&> \u{e9}\u{2028}");
        s.save_without_dialog = false;
        s.window_shadow = false;
        s.show_loupe = false;
        s.after_shot = AfterShot { preview: false, copy: false, save: true, edit: true, pin: true };
        s.after_recording = AfterRecording { preview: false, copy: true, save: true, studio: true };
        s.record = RecordPrefs {
            target: RecordTarget::Window,
            area: Some([-10, 20, 1920, 1080]),
            camera: Some("Integrated Camera".into()),
            mic: Some("Microphone \"USB\"".into()),
            system_audio: false,
            countdown: 0,
            teleprompter: true,
            script: "Line one\nLine two\ttabbed".into(),
            prompter_lines: 5,
        };
        s.overlay.side = Side::Right;
        s.overlay.auto_close_seconds = 8;
        s.overlay.dismiss_after_drag = false;
        s.overlay.layout.corners = [None, Some(CardAction::Copy), Some(CardAction::Pin), None];
        s.overlay.layout.center = vec![CardAction::Edit];
        s.overlay.layout.hidden = vec![];
        s.overlay.layout = s.overlay.layout.normalized();
        s.hotkeys.area = "Ctrl+Alt+A".into();
        s.hotkeys.library = String::new();
        s.auto_redact = true;
        s.redact_style = RedactStyle::Pixelate;
        s.redact_rules.phones = false;
        s.redact_rules.urls_with_tokens = false;
        s.last_area = Some([1, 2, 3, 4]);
        s.library.view = LibraryView::List;
        s.library.sort = SortKey::Modified;
        s.library.inspector = false;
        write("settings-custom.json", &s);

        let partial = r#"{"timer_seconds": 9, "record": {"countdown": 1}, "overlay": {"layout": {"corners": [null, "copy", null, null]}}, "hotkeys": {"text": "F1"}, "not_a_field": [1, 2]}"#;
        std::fs::write(dir.join("settings-partial.json"), partial).unwrap();
        let decoded = Settings::load_from(&dir.join("settings-partial.json"));
        assert_eq!(decoded.timer_seconds, 9);
        write("settings-partial-decoded.json", &decoded);

        for (name, json) in [
            ("bad-enum", r#"{"format": "Gif"}"#),
            ("bad-corners", r#"{"overlay": {"layout": {"corners": ["copy"]}}}"#),
            ("bad-area", r#"{"last_area": [1, 2, 3]}"#),
            ("bad-quality", r#"{"jpeg_quality": 300}"#),
            ("bad-type", r#"{"timer_seconds": "5"}"#),
        ] {
            assert!(serde_json::from_str::<Settings>(json).is_err(), "{name} must be rejected");
            std::fs::write(dir.join(format!("settings-reject-{name}.json")), json).unwrap();
        }
    }

    #[test]
    #[ignore = "reads settings.json written by the Go port from GLINT_GO_WRITTEN"]
    fn reads_go_written_settings() {
        let dir = env_dir("GLINT_GO_WRITTEN");
        for name in ["settings-default.json", "settings-custom.json"] {
            let text = std::fs::read_to_string(dir.join(name)).unwrap();
            let s: Settings = serde_json::from_str(&text).unwrap_or_else(|e| panic!("{name}: {e}"));
            assert_eq!(serde_json::to_string_pretty(&s).unwrap(), text, "{name} round-trips byte for byte");
        }
    }
}
