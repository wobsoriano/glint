use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;

use anyhow::{anyhow, Context, Result};
use whisper_rs::{FullParams, SamplingStrategy, WhisperContext, WhisperContextParameters};

use crate::audio::{to_whisper_rate, Pcm};
use crate::models::Cancelled;
use crate::transcript::{Transcript, Word};

/// Whisper drops hesitations unless the prompt shows them, so the prompt is written in the
/// disfluent style we want transcribed.
const DISFLUENT_PROMPT: &str = "Um, so, uh, I was like, you know, thinking. Hmm, okay.";

pub(crate) struct Engine {
    ctx: WhisperContext,
}

pub(crate) struct RunOptions {
    pub offset_ms: u64,
    /// Encoder context in 20 ms frames; shortening it speeds up short windows.
    pub audio_ctx: Option<i32>,
    pub disfluent_prompt: bool,
}

impl Engine {
    pub(crate) fn load(model_path: &Path) -> Result<Self> {
        whisper_rs::install_logging_hooks();
        let path = model_path.to_str().context("model path is not UTF-8")?;
        let ctx = WhisperContext::new_with_params(path, WhisperContextParameters::default())
            .map_err(|e| anyhow!("loading whisper model {}: {e}", model_path.display()))?;
        Ok(Self { ctx })
    }

    pub(crate) fn run(
        &self,
        samples: &[f32],
        options: &RunOptions,
        progress: Option<Box<dyn FnMut(f32)>>,
        cancel: Option<Arc<AtomicBool>>,
    ) -> Result<(String, Vec<Word>)> {
        let mut state = self.ctx.create_state().map_err(|e| anyhow!("creating whisper state: {e}"))?;
        let mut params = FullParams::new(SamplingStrategy::Greedy { best_of: 1 });
        let threads = std::thread::available_parallelism().map_or(4, |n| n.get()).min(8);
        params.set_n_threads(threads as i32);
        params.set_token_timestamps(true);
        params.set_print_progress(false);
        params.set_print_realtime(false);
        params.set_print_special(false);
        params.set_print_timestamps(false);
        params.set_suppress_blank(true);
        if self.ctx.is_multilingual() {
            params.set_language(Some("auto"));
        } else {
            params.set_language(Some("en"));
        }
        if options.disfluent_prompt {
            params.set_initial_prompt(DISFLUENT_PROMPT);
        }
        if let Some(audio_ctx) = options.audio_ctx {
            params.set_audio_ctx(audio_ctx);
        }
        if let Some(mut progress) = progress {
            params.set_progress_callback_safe(move |percent: i32| progress(percent as f32 / 100.0));
        }
        if let Some(cancel) = cancel.clone() {
            // whisper-rs 0.16 stores the closure as Box<dyn FnMut> but its trampoline casts to the
            // closure's own type; making that type Box<dyn FnMut> keeps the cast sound.
            let abort: Box<dyn FnMut() -> bool> = Box::new(move || cancel.load(Ordering::Relaxed));
            params.set_abort_callback_safe::<_, Box<dyn FnMut() -> bool>>(abort);
        }

        let result = state.full(params, samples);
        if cancel.is_some_and(|c| c.load(Ordering::Relaxed)) {
            return Err(Cancelled.into());
        }
        result.map_err(|e| anyhow!("whisper inference failed: {e}"))?;

        let language = whisper_rs::get_lang_str(state.full_lang_id_from_state()).unwrap_or("en").to_string();
        let eot = self.ctx.token_eot();
        let mut words: Vec<Word> = Vec::new();
        let mut current: Option<WordBuilder> = None;
        for segment in state.as_iter() {
            if let Some(word) = current.take() {
                words.extend(word.finish());
            }
            for i in 0..segment.n_tokens() {
                let Some(token) = segment.get_token(i) else { continue };
                if token.token_id() >= eot {
                    continue;
                }
                let Ok(piece) = token.to_str_lossy() else { continue };
                let data = token.token_data();
                let starts_word = piece.starts_with(char::is_whitespace) || current.is_none();
                if starts_word {
                    if let Some(word) = current.take() {
                        words.extend(word.finish());
                    }
                    current = Some(WordBuilder::default());
                }
                let word = current.as_mut().expect("word started above");
                word.push(&piece, data.t0, data.t1, data.p, options.offset_ms);
            }
        }
        if let Some(word) = current.take() {
            words.extend(word.finish());
        }
        Ok((language, words))
    }
}

#[derive(Default)]
struct WordBuilder {
    text: String,
    start_ms: Option<u64>,
    end_ms: u64,
    p_sum: f32,
    pieces: u32,
}

impl WordBuilder {
    /// `t0`/`t1` arrive in whisper's 10 ms ticks.
    fn push(&mut self, piece: &str, t0: i64, t1: i64, p: f32, offset_ms: u64) {
        self.text.push_str(piece);
        let start = offset_ms + t0.max(0) as u64 * 10;
        let end = offset_ms + t1.max(0) as u64 * 10;
        self.start_ms.get_or_insert(start);
        self.end_ms = self.end_ms.max(end);
        self.p_sum += p;
        self.pieces += 1;
    }

    fn finish(self) -> Option<Word> {
        let text = self.text.trim();
        if text.is_empty() || text.chars().all(|c| !c.is_alphanumeric()) {
            return None;
        }
        let start_ms = self.start_ms?;
        Some(Word {
            text: text.to_string(),
            start_ms,
            end_ms: self.end_ms.max(start_ms),
            confidence: self.p_sum / self.pieces as f32,
        })
    }
}

/// Transcribes a whole recording with a model file from `models::model_path`. Blocking; run it on
/// a background thread. `progress` receives 0..=1. Setting `cancel` stops inference and returns
/// a `Cancelled` error.
pub fn transcribe(
    model_path: &Path,
    pcm: Pcm,
    progress: impl FnMut(f32) + 'static,
    cancel: Arc<AtomicBool>,
) -> Result<Transcript> {
    let engine = Engine::load(model_path)?;
    let samples = to_whisper_rate(pcm);
    let options = RunOptions { offset_ms: 0, audio_ctx: None, disfluent_prompt: true };
    let (language, words) = engine.run(&samples, &options, Some(Box::new(progress)), Some(cancel))?;
    Ok(Transcript::new(language, words))
}
