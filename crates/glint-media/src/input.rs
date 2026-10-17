//! The input timeline recorded beside the screen master (`input.json`).
//! Times are media milliseconds with pauses removed; positions are screen-master pixels.

use serde::{Deserialize, Serialize};
use std::path::Path;

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
pub struct InputTimeline {
    pub version: u32,
    pub size: [u32; 2],
    /// `[t_ms, x, y, cursor_id]`, sorted by time.
    pub pointer: Vec<PointerSample>,
    pub clicks: Vec<Click>,
    pub keys: Vec<KeyPress>,
    pub cursors: Vec<CursorSprite>,
}

#[derive(Clone, Copy, Debug, PartialEq, Serialize, Deserialize)]
pub struct PointerSample(pub u64, pub f32, pub f32, pub u32);

impl PointerSample {
    pub fn t(&self) -> u64 {
        self.0
    }
    pub fn pos(&self) -> (f32, f32) {
        (self.1, self.2)
    }
    pub fn cursor(&self) -> u32 {
        self.3
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum MouseButton {
    Left,
    Right,
    Middle,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Click {
    pub t: u64,
    pub up: u64,
    pub x: f32,
    pub y: f32,
    pub button: MouseButton,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct KeyPress {
    pub t: u64,
    pub label: String,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct CursorSprite {
    pub id: u32,
    /// Relative to the session directory, e.g. `cursors/0.png`.
    pub file: String,
    pub hotspot: [u32; 2],
    pub scale: f32,
}

impl InputTimeline {
    pub const FILE: &'static str = "input.json";

    pub fn load(session_dir: &Path) -> anyhow::Result<Self> {
        let json = std::fs::read_to_string(session_dir.join(Self::FILE))?;
        Ok(serde_json::from_str(&json)?)
    }

    pub fn save(&self, session_dir: &Path) -> anyhow::Result<()> {
        std::fs::write(session_dir.join(Self::FILE), serde_json::to_string(self)?)?;
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn reads_the_documented_shape() {
        let json = r#"{
            "version": 1,
            "size": [2560, 1440],
            "pointer": [[0, 100.5, 200.0, 0], [16, 104.0, 203.5, 0]],
            "clicks": [{ "t": 1200, "up": 1310, "x": 640.0, "y": 360.0, "button": "left" }],
            "keys": [{ "t": 3000, "label": "Ctrl+S" }],
            "cursors": [{ "id": 0, "file": "cursors/0.png", "hotspot": [1, 1], "scale": 1.5 }]
        }"#;
        let tl: InputTimeline = serde_json::from_str(json).unwrap();
        assert_eq!(tl.pointer[1], PointerSample(16, 104.0, 203.5, 0));
        assert_eq!(tl.clicks[0].button, MouseButton::Left);
        assert_eq!(tl.keys[0].label, "Ctrl+S");
        assert_eq!(tl.cursors[0].hotspot, [1, 1]);
    }
}
