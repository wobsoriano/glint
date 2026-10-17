use crate::compose::Assets;
use crate::overlays::CaptionWord;
use crate::project::{CaptionCue, Project};
use anyhow::Context;
use glint_media::input::InputTimeline;
use glint_media::VideoReader;
use glint_transcribe::{caption_lines, CaptionOptions, Transcript, TRANSCRIPT_FILE};
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
    /// One sentence for the user per sidecar file that could not be read and was renamed to
    /// `<name>.broken`, so the session opened with defaults in its place.
    pub notices: Vec<String>,
}

impl Session {
    /// Opens a session directory. Without a readable `project.json` the project covers the
    /// whole recording with auto zooms generated from the recorded clicks.
    pub fn open(dir: &Path) -> anyhow::Result<Self> {
        let tracks = Tracks::find(dir)?;
        let reader = VideoReader::open(&tracks.screen)?;
        let source_size = reader.size();
        let source_duration_ms = reader.duration_ms() as f64;
        drop(reader);

        let mut notices = Vec::new();
        let transcript = read_sidecar(dir, TRANSCRIPT_FILE, Transcript::parse, &mut notices)?;
        let assets = load_assets(dir, source_size, transcript.as_ref())?;
        let project = read_sidecar(dir, Project::FILE, |json| Project::parse(json, source_duration_ms), &mut notices)?;
        let mut project = project.unwrap_or_else(|| {
            let mut p = Project::new(source_duration_ms);
            p.apply_auto_zoom(assets.input(), source_duration_ms);
            p
        });
        if let (true, Some(t)) = (project.caption_cues.is_empty(), &transcript) {
            project.caption_cues = caption_cues(t);
        }
        Ok(Self {
            dir: dir.to_path_buf(),
            project,
            assets: Arc::new(assets),
            tracks,
            transcript,
            source_size,
            source_duration_ms,
            notices,
        })
    }

    pub fn save_project(&self) -> anyhow::Result<()> {
        self.project.save(&self.dir)
    }

    /// Saves a new transcript and rebuilds the assets so captions use it. Give the project
    /// cues from [`caption_cues`] to show it, and hand the new
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

pub fn caption_words(transcript: &Transcript) -> Vec<CaptionWord> {
    transcript
        .words
        .iter()
        .map(|w| CaptionWord { text: w.text.clone(), start_ms: w.start_ms as f64, end_ms: w.end_ms as f64 })
        .collect()
}

pub fn caption_cues(transcript: &Transcript) -> Vec<CaptionCue> {
    caption_lines(&transcript.words, &CaptionOptions::default())
        .into_iter()
        .map(|l| CaptionCue { start_ms: l.start_ms as f64, end_ms: l.end_ms as f64, text: l.text })
        .collect()
}

fn read_sidecar<T>(
    dir: &Path,
    name: &str,
    parse: impl FnOnce(&[u8]) -> anyhow::Result<T>,
    notices: &mut Vec<String>,
) -> anyhow::Result<Option<T>> {
    let path = dir.join(name);
    let bytes = match std::fs::read(&path) {
        Ok(bytes) => bytes,
        Err(e) if e.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(e) => return Err(e).with_context(|| format!("reading {}", path.display())),
    };
    let err = match parse(&bytes) {
        Ok(value) => return Ok(Some(value)),
        Err(e) => e,
    };
    let broken = dir.join(format!("{name}.broken"));
    std::fs::rename(&path, &broken).with_context(|| format!("renaming {} to {}", path.display(), broken.display()))?;
    notices.push(format!("{name} could not be read ({err:#}), so it was renamed {name}.broken and defaults are used"));
    Ok(None)
}

fn load_assets(dir: &Path, source_size: (u32, u32), transcript: Option<&Transcript>) -> anyhow::Result<Assets> {
    let captions = transcript.map(caption_words).unwrap_or_default();
    if dir.join(InputTimeline::FILE).is_file() {
        Assets::load(dir, captions).with_context(|| format!("loading session assets in {}", dir.display()))
    } else {
        let input = InputTimeline { version: 1, size: [source_size.0, source_size.1], ..Default::default() };
        Ok(Assets::new(input, HashMap::new(), captions))
    }
}
