//! A recording session package (`<id>.glintrec`) opened for Studio: the project, the
//! compositor assets, which tracks exist, and the transcript.

use crate::compose::Assets;
use crate::overlays::CaptionWord;
use crate::project::Project;
use anyhow::Context;
use glint_media::input::InputTimeline;
use glint_media::VideoReader;
use glint_transcribe::{Transcript, TRANSCRIPT_FILE};
use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::Arc;

/// Media files present in a session. The screen master always exists.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Tracks {
    pub screen: PathBuf,
    pub camera: Option<PathBuf>,
    pub mic: Option<PathBuf>,
    pub system: Option<PathBuf>,
}

impl Tracks {
    fn find(dir: &Path) -> anyhow::Result<Self> {
        let screen = dir.join("screen.mp4");
        if !screen.is_file() {
            anyhow::bail!("{} has no screen.mp4", dir.display());
        }
        let opt = |name: &str| Some(dir.join(name)).filter(|p| p.is_file());
        Ok(Self { screen, camera: opt("camera.mp4"), mic: opt("mic.wav"), system: opt("system.wav") })
    }
}

pub struct Session {
    pub dir: PathBuf,
    pub project: Project,
    pub assets: Arc<Assets>,
    pub tracks: Tracks,
    pub transcript: Option<Transcript>,
    /// Pixel size of the screen master.
    pub source_size: (u32, u32),
    pub source_duration_ms: f64,
}

impl Session {
    /// Opens a session directory. Without `project.json` the project covers the whole
    /// recording with auto zooms generated from the recorded clicks.
    pub fn open(dir: &Path) -> anyhow::Result<Self> {
        let tracks = Tracks::find(dir)?;
        let reader = VideoReader::open(&tracks.screen)?;
        let source_size = reader.size();
        let source_duration_ms = reader.duration_ms() as f64;
        drop(reader);

        let transcript_path = dir.join(TRANSCRIPT_FILE);
        let transcript = transcript_path.is_file().then(|| Transcript::load(&transcript_path)).transpose()?;
        let assets = load_assets(dir, source_size, transcript.as_ref())?;
        let project = if dir.join(Project::FILE).is_file() {
            Project::load(dir, source_duration_ms)?
        } else {
            let mut p = Project::new(source_duration_ms);
            p.apply_auto_zoom(assets.input(), source_duration_ms);
            p
        };
        Ok(Self {
            dir: dir.to_path_buf(),
            project,
            assets: Arc::new(assets),
            tracks,
            transcript,
            source_size,
            source_duration_ms,
        })
    }

    pub fn save_project(&self) -> anyhow::Result<()> {
        self.project.save(&self.dir)
    }

    /// Saves a new transcript and rebuilds the assets so captions use it. Hand the new
    /// `assets` to a running [`crate::Player`] with `set_assets`.
    pub fn set_transcript(&mut self, transcript: Transcript) -> anyhow::Result<()> {
        transcript.save(&self.dir.join(TRANSCRIPT_FILE))?;
        self.assets = Arc::new(load_assets(&self.dir, self.source_size, Some(&transcript))?);
        self.transcript = Some(transcript);
        Ok(())
    }

    /// Export cache and thumbnails; safe to delete.
    pub fn render_dir(&self) -> PathBuf {
        self.dir.join("render")
    }
}

/// Transcript words as caption words on the source timeline.
pub fn caption_words(transcript: &Transcript) -> Vec<CaptionWord> {
    transcript
        .words
        .iter()
        .map(|w| CaptionWord { text: w.text.clone(), start_ms: w.start_ms as f64, end_ms: w.end_ms as f64 })
        .collect()
}

/// A session recorded with input capture off has no `input.json`: it gets an empty timeline.
fn load_assets(dir: &Path, source_size: (u32, u32), transcript: Option<&Transcript>) -> anyhow::Result<Assets> {
    let captions = transcript.map(caption_words).unwrap_or_default();
    if dir.join(InputTimeline::FILE).is_file() {
        Assets::load(dir, captions).with_context(|| format!("loading session assets in {}", dir.display()))
    } else {
        let input = InputTimeline { version: 1, size: [source_size.0, source_size.1], ..Default::default() };
        Ok(Assets::new(input, HashMap::new(), captions))
    }
}
