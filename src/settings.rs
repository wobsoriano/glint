//! Persistent user preferences, stored as JSON in %APPDATA%\Glint\settings.json.

pub use crate::annotate::RedactStyle;
use crate::style::Style;
use serde::{Deserialize, Serialize};
use std::path::PathBuf;

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum AfterCapture {
    QuickAccess,
    OpenEditor,
    Nothing,
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

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Hotkeys {
    pub area: String,
    pub fullscreen: String,
    pub window: String,
    pub previous_area: String,
    pub text: String,
}

impl Default for Hotkeys {
    fn default() -> Self {
        Self {
            area: "Ctrl+Shift+4".into(),
            fullscreen: "Ctrl+Shift+3".into(),
            window: "Ctrl+Shift+5".into(),
            previous_area: "Ctrl+Shift+6".into(),
            text: "Ctrl+Shift+2".into(),
        }
    }
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Settings {
    pub style: Style,
    pub after_capture: AfterCapture,
    pub copy_on_capture: bool,
    pub save_on_capture: bool,
    pub auto_redact: bool,
    pub redact_style: RedactStyle,
    pub redact_rules: RedactRules,
    pub save_dir: PathBuf,
    pub hotkeys: Hotkeys,
    pub quick_access_seconds: u32,
    pub show_loupe: bool,
    pub launch_at_login: bool,
    pub last_area: Option<[i32; 4]>,
}

impl Default for Settings {
    fn default() -> Self {
        let save_dir = dirs::picture_dir()
            .or_else(dirs::home_dir)
            .unwrap_or_else(|| PathBuf::from("."))
            .join("Glint");
        Self {
            style: Style::default(),
            after_capture: AfterCapture::QuickAccess,
            copy_on_capture: true,
            save_on_capture: false,
            auto_redact: false,
            redact_style: RedactStyle::Blur,
            redact_rules: RedactRules::default(),
            save_dir,
            hotkeys: Hotkeys::default(),
            quick_access_seconds: 8,
            show_loupe: true,
            launch_at_login: false,
            last_area: None,
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
        std::fs::read_to_string(path())
            .ok()
            .and_then(|s| serde_json::from_str(&s).ok())
            .unwrap_or_default()
    }

    pub fn save(&self) {
        let p = path();
        if let Some(dir) = p.parent() {
            let _ = std::fs::create_dir_all(dir);
        }
        if let Ok(json) = serde_json::to_string_pretty(self) {
            let _ = std::fs::write(p, json);
        }
    }
}

impl gpui::Global for Settings {}
