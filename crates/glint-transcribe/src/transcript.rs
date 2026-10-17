use std::fs;
use std::path::Path;

use anyhow::{bail, Context, Result};
use serde::{Deserialize, Serialize};

pub const TRANSCRIPT_FILE: &str = "transcript.json";
pub const TRANSCRIPT_VERSION: u32 = 1;

#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Transcript {
    pub version: u32,
    /// ISO 639-1 code reported by the model, e.g. "en".
    pub language: String,
    pub words: Vec<Word>,
}

/// One recognized word on the source timeline. `text` has no surrounding whitespace and keeps
/// attached punctuation ("know," or "Okay.").
#[derive(Debug, Clone, PartialEq, Serialize, Deserialize)]
pub struct Word {
    pub text: String,
    pub start_ms: u64,
    pub end_ms: u64,
    /// Mean token probability, 0..=1.
    pub confidence: f32,
}

/// A half-open span of source media time in milliseconds.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
pub struct TimeRange {
    pub start_ms: u64,
    pub end_ms: u64,
}

impl TimeRange {
    pub fn duration_ms(&self) -> u64 {
        self.end_ms.saturating_sub(self.start_ms)
    }
}

impl Transcript {
    pub fn new(language: impl Into<String>, words: Vec<Word>) -> Self {
        Self { version: TRANSCRIPT_VERSION, language: language.into(), words }
    }

    pub fn text(&self) -> String {
        self.words.iter().map(|w| w.text.as_str()).collect::<Vec<_>>().join(" ")
    }

    pub fn load(path: &Path) -> Result<Self> {
        let bytes = fs::read(path).with_context(|| format!("reading {}", path.display()))?;
        Self::parse(&bytes).with_context(|| format!("parsing {}", path.display()))
    }

    pub fn parse(json: &[u8]) -> Result<Self> {
        let transcript: Transcript = serde_json::from_slice(json)?;
        if transcript.version > TRANSCRIPT_VERSION {
            bail!("transcript version {} is newer than supported {TRANSCRIPT_VERSION}", transcript.version);
        }
        Ok(transcript)
    }

    /// Writes through a temp file and rename so a crash never leaves a half-written transcript.
    pub fn save(&self, path: &Path) -> Result<()> {
        let tmp = path.with_extension("json.tmp");
        let json = serde_json::to_vec_pretty(self)?;
        fs::write(&tmp, json).with_context(|| format!("writing {}", tmp.display()))?;
        fs::rename(&tmp, path).with_context(|| format!("renaming into {}", path.display()))?;
        Ok(())
    }
}

/// Lowercase letters and digits only, so "Like," compares equal to "like" and "it's" to "its".
pub fn normalize_word(text: &str) -> String {
    text.chars().filter(|c| c.is_alphanumeric()).flat_map(char::to_lowercase).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn save_then_load_round_trips() {
        let dir = std::env::temp_dir().join(format!("glint-transcript-{}", std::process::id()));
        fs::create_dir_all(&dir).unwrap();
        let path = dir.join(TRANSCRIPT_FILE);
        let transcript = Transcript::new(
            "en",
            vec![Word { text: "Hello,".into(), start_ms: 120, end_ms: 480, confidence: 0.9 }],
        );
        transcript.save(&path).unwrap();
        assert_eq!(Transcript::load(&path).unwrap(), transcript);
        let json = fs::read_to_string(&path).unwrap();
        assert!(json.contains("\"version\": 1"));
        assert!(json.contains("\"start_ms\": 120"));
        fs::remove_dir_all(&dir).unwrap();
    }

    #[test]
    fn normalize_strips_case_and_punctuation() {
        assert_eq!(normalize_word("Like,"), "like");
        assert_eq!(normalize_word("it's"), "its");
        assert_eq!(normalize_word("--"), "");
    }
}
