use std::path::Path;
use std::sync::mpsc::{self, Receiver, RecvTimeoutError, Sender};
use std::thread::JoinHandle;
use std::time::Duration;

use anyhow::{Context, Result};

use crate::audio::{resample, WHISPER_SAMPLE_RATE};
use crate::engine::{Engine, RunOptions};
use crate::transcript::Word;

const WINDOW_MS: u64 = 8_000;
const STEP_MS: u64 = 1_000;
const EDGE_HOLD_MS: u64 = 250;

const fn samples(ms: u64) -> usize {
    (ms * WHISPER_SAMPLE_RATE as u64 / 1000) as usize
}

/// Near-real-time recognition for the teleprompter. Feed mic audio with `push`; a worker thread
/// re-recognizes the last few seconds about once a second and `poll` returns words it has not
/// returned before, timed from the first pushed sample.
///
/// When inference falls behind real time the worker jumps to the newest audio, so words in the
/// skipped stretch are lost rather than delivered late.
pub struct LiveTranscriber {
    audio: Option<Sender<Vec<f32>>>,
    words: Receiver<Vec<Word>>,
    worker: Option<JoinHandle<()>>,
    sample_rate: u32,
}

impl LiveTranscriber {
    /// Loads the model (use `Model::TinyEn` for speed) and starts the worker. `sample_rate` is
    /// the rate of the mono chunks passed to `push`.
    pub fn start(model_path: &Path, sample_rate: u32) -> Result<Self> {
        let engine = Engine::load(model_path)?;
        let (audio_tx, audio_rx) = mpsc::channel::<Vec<f32>>();
        let (words_tx, words_rx) = mpsc::channel();
        let worker = std::thread::Builder::new()
            .name("glint-live-transcribe".into())
            .spawn(move || Worker::new(engine, words_tx).run(audio_rx))
            .context("spawning live transcription thread")?;
        Ok(Self { audio: Some(audio_tx), words: words_rx, worker: Some(worker), sample_rate })
    }

    pub fn push(&self, mono: &[f32]) {
        if let Some(audio) = &self.audio {
            let _ = audio.send(resample(mono, self.sample_rate, WHISPER_SAMPLE_RATE));
        }
    }

    /// Words recognized since the last call, in spoken order.
    pub fn poll(&self) -> Vec<Word> {
        self.words.try_iter().flatten().collect()
    }

    /// Stops input, waits for a last pass over the remaining audio, and returns its new words.
    pub fn finish(mut self) -> Vec<Word> {
        self.stop();
        self.poll()
    }

    fn stop(&mut self) {
        self.audio = None;
        if let Some(worker) = self.worker.take() {
            let _ = worker.join();
        }
    }
}

impl Drop for LiveTranscriber {
    fn drop(&mut self) {
        self.stop();
    }
}

struct Worker {
    engine: Engine,
    out: Sender<Vec<Word>>,
    window: Vec<f32>,
    total_samples: usize,
    pending_samples: usize,
    emitted_until_ms: u64,
}

impl Worker {
    fn new(engine: Engine, out: Sender<Vec<Word>>) -> Self {
        Self { engine, out, window: Vec::new(), total_samples: 0, pending_samples: 0, emitted_until_ms: 0 }
    }

    fn run(mut self, audio: Receiver<Vec<f32>>) {
        loop {
            match audio.recv_timeout(Duration::from_millis(50)) {
                Ok(chunk) => {
                    self.append(chunk);
                    for chunk in audio.try_iter() {
                        self.append(chunk);
                    }
                }
                Err(RecvTimeoutError::Timeout) => {}
                Err(RecvTimeoutError::Disconnected) => {
                    if self.pending_samples > 0 {
                        self.pass(true);
                    }
                    return;
                }
            }
            if self.pending_samples >= samples(STEP_MS) && !self.pass(false) {
                return;
            }
        }
    }

    fn append(&mut self, chunk: Vec<f32>) {
        self.total_samples += chunk.len();
        self.pending_samples += chunk.len();
        self.window.extend(chunk);
        let excess = self.window.len().saturating_sub(samples(WINDOW_MS));
        self.window.drain(..excess);
    }

    fn pass(&mut self, last: bool) -> bool {
        self.pending_samples = 0;
        let offset_ms = ((self.total_samples - self.window.len()) as u64) * 1000 / WHISPER_SAMPLE_RATE as u64;
        let window_ms = self.window.len() as u64 * 1000 / WHISPER_SAMPLE_RATE as u64;
        let options = RunOptions {
            offset_ms,
            audio_ctx: Some(((window_ms / 20) as i32 + 64).min(1500)),
            disfluent_prompt: false,
        };
        let Ok((_, words)) = self.engine.run(&self.window, &options, None, None) else {
            return true;
        };
        let edge_ms = offset_ms + window_ms;
        let fresh: Vec<Word> = words
            .into_iter()
            .filter(|w| (w.start_ms + w.end_ms) / 2 > self.emitted_until_ms)
            .take_while(|w| last || w.end_ms + EDGE_HOLD_MS < edge_ms)
            .collect();
        if let Some(word) = fresh.last() {
            self.emitted_until_ms = word.end_ms;
        }
        fresh.is_empty() || self.out.send(fresh).is_ok()
    }
}
