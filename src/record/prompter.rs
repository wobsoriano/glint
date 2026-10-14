//! The teleprompter: a pill under the top edge of the recorded display that shows a few lines
//! of the script. With a voice source it follows along: microphone buffers go to on-device
//! recognition on its own thread, and recognized words advance a `ScriptTracker`. Without
//! one, or when recognition fails, the user scrolls it.

use super::chrome::{self, Kind, Placement};
use super::Target;
use crate::ui::{self, FONT};
use glint_transcribe::{download_model, is_installed, Cancelled, LiveTranscriber, Model, ScriptTracker};
use gpui::prelude::*;
use gpui::{
    div, font, px, rgb, rgba, AnyWindowHandle, App, Context, FontWeight, ScrollWheelEvent, SharedString, TextRun, Window,
};
use std::ops::Range;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{Receiver, RecvTimeoutError, SyncSender};
use std::sync::{Arc, Mutex};
use std::time::{Duration, Instant};

const WIDTH: f32 = 560.;
const PAD_X: f32 = 22.;
const LINE_H: f32 = 26.;
const WORD_GAP: f32 = 5.;
const FONT_SIZE: f32 = 16.;
const STATUS_H: f32 = 22.;
const PAD_Y: f32 = 10.;
/// Microphone buffers (about 10 ms each) that may wait for the recognizer before new ones
/// are dropped.
pub const AUDIO_QUEUE: usize = 200;
/// Sample rate of the buffers the recorder's microphone tap delivers.
pub const TAP_RATE: u32 = 48_000;

#[derive(Clone, Debug, PartialEq)]
pub enum Status {
    Downloading { done: u64, total: u64 },
    Loading,
    Listening,
    Failed(String),
}

#[derive(Default)]
struct Shared {
    status: Mutex<Option<Status>>,
    index: Mutex<Option<usize>>,
}

/// Recognition running for one recording. Dropping it stops the thread.
pub struct Follower {
    shared: Arc<Shared>,
    cancel: Arc<AtomicBool>,
}

impl Follower {
    /// Starts following `script` from `audio`, mono buffers at `rate`. Downloads the speech
    /// model into `data_dir` first when it is missing.
    pub fn start(script: String, audio: Receiver<Vec<f32>>, rate: u32, data_dir: PathBuf) -> Self {
        let shared = Arc::new(Shared::default());
        let cancel = Arc::new(AtomicBool::new(false));
        let (s, c) = (shared.clone(), cancel.clone());
        let spawned = std::thread::Builder::new()
            .name("glint-prompter".into())
            .spawn(move || follow(&script, &audio, rate, &data_dir, &s, &c));
        if let Err(e) = spawned {
            *shared.status.lock().unwrap() = Some(Status::Failed(e.to_string()));
        }
        Self { shared, cancel }
    }

    fn status(&self) -> Option<Status> {
        self.shared.status.lock().unwrap().clone()
    }

    fn index(&self) -> Option<usize> {
        *self.shared.index.lock().unwrap()
    }
}

impl Drop for Follower {
    fn drop(&mut self) {
        self.cancel.store(true, Ordering::Relaxed);
    }
}

fn follow(script: &str, audio: &Receiver<Vec<f32>>, rate: u32, data_dir: &Path, shared: &Shared, cancel: &AtomicBool) {
    let set = |s: Status| *shared.status.lock().unwrap() = Some(s);
    let model = Model::TinyEn;
    if !is_installed(data_dir, model) {
        set(Status::Downloading { done: 0, total: model.size_bytes() });
    }
    let path = match download_model(data_dir, model, |done, total| set(Status::Downloading { done, total }), cancel) {
        Ok(p) => p,
        Err(e) if e.is::<Cancelled>() => return,
        Err(e) => return set(Status::Failed(format!("speech model download failed: {e:#}"))),
    };
    set(Status::Loading);
    let live = match LiveTranscriber::start(&path, rate) {
        Ok(l) => l,
        Err(e) => return set(Status::Failed(format!("{e:#}"))),
    };
    // Speech from before the model was ready is stale; start from now.
    while audio.try_recv().is_ok() {}
    set(Status::Listening);
    let mut tracker = ScriptTracker::new(script);
    while !cancel.load(Ordering::Relaxed) {
        match audio.recv_timeout(Duration::from_millis(100)) {
            Ok(chunk) => {
                live.push(&chunk);
                for chunk in audio.try_iter() {
                    live.push(&chunk);
                }
            }
            Err(RecvTimeoutError::Timeout) => {}
            Err(RecvTimeoutError::Disconnected) => break,
        }
        let words = live.poll();
        if !words.is_empty() {
            let spoken: Vec<&str> = words.iter().map(|w| w.text.as_str()).collect();
            *shared.index.lock().unwrap() = tracker.feed(&spoken);
        }
    }
}

/// Plays a WAV file into `tx` in real time, as if it were the microphone. Used by
/// `GLINT_TELEPROMPTER_WAV` to exercise the follow path without a speaker in the room.
pub fn feed_wav(path: PathBuf, tx: SyncSender<Vec<f32>>) {
    let _ = std::thread::Builder::new().name("glint-prompter-wav".into()).spawn(move || {
        let Ok(wav) = glint_media::read_wav(&path) else { return };
        let ch = wav.channels.max(1) as usize;
        let mono: Vec<f32> = wav.samples.chunks(ch).map(|f| f.iter().sum::<f32>() / ch as f32).collect();
        let mono = glint_transcribe::resample(&mono, wav.sample_rate, TAP_RATE);
        let chunk = TAP_RATE as usize / 50;
        let start = Instant::now();
        for (i, c) in mono.chunks(chunk).enumerate() {
            let due = start + Duration::from_millis(20 * i as u64);
            if let Some(wait) = due.checked_duration_since(Instant::now()) {
                std::thread::sleep(wait);
            }
            if let Err(std::sync::mpsc::TrySendError::Disconnected(_)) = tx.try_send(c.to_vec()) {
                return;
            }
        }
    });
}

/// Opens the pill centered under the top edge of the recorded display.
pub fn open(target: &Target, script: &str, lines: usize, follower: Option<Follower>, cx: &mut App) -> Option<AnyWindowHandle> {
    let d = target.display();
    let scale = d.scale;
    let (pw, ph) = ((WIDTH * scale).round() as i32, (Prompter::height(lines) * scale).round() as i32);
    let rect = [d.rect.x + (d.rect.width as i32 - pw) / 2, d.rect.y, pw, ph];
    let script = script.to_string();
    let placement = Placement { rect, scale, kind: Kind::DuringRecording };
    let handle = chrome::open(placement, cx, move |_, _| Prompter::new(&script, lines, follower))?;
    chrome::tick(handle.into(), 100, cx);
    Some(handle.into())
}

/// Word index ranges, one per line, wrapping greedily at `max` width.
pub fn wrap(widths: &[f32], space: f32, max: f32) -> Vec<Range<usize>> {
    let mut lines = Vec::new();
    let (mut start, mut used) = (0, 0.);
    for (i, &w) in widths.iter().enumerate() {
        if i > start && used + space + w > max {
            lines.push(start..i);
            (start, used) = (i, w);
        } else {
            used += if i > start { space + w } else { w };
        }
    }
    if start < widths.len() {
        lines.push(start..widths.len());
    }
    lines
}

/// First visible line: the line being read sits at the top, and finishing a line's last word
/// already brings the next one up.
pub fn top_line(lines: &[Range<usize>], active: Option<usize>, visible: usize) -> usize {
    let Some(a) = active else { return 0 };
    let mut line = lines.iter().position(|l| l.contains(&a)).unwrap_or(0);
    if lines.get(line).is_some_and(|l| l.end == a + 1) {
        line += 1;
    }
    line.min(lines.len().saturating_sub(visible))
}

pub struct Prompter {
    words: Vec<SharedString>,
    lines: Option<Vec<Range<usize>>>,
    visible: usize,
    follower: Option<Follower>,
    /// Lines scrolled by hand from where following put the text.
    manual: i32,
    wheel: f32,
    last_index: Option<usize>,
}

impl Prompter {
    fn height(visible: usize) -> f32 {
        PAD_Y * 2. + visible as f32 * LINE_H + STATUS_H
    }

    fn new(script: &str, visible: usize, follower: Option<Follower>) -> Self {
        Self {
            words: script.split_whitespace().map(|w| SharedString::from(w.to_string())).collect(),
            lines: None,
            visible,
            follower,
            manual: 0,
            wheel: 0.,
            last_index: None,
        }
    }

    fn layout(&mut self, window: &Window) -> &[Range<usize>] {
        if self.lines.is_none() {
            let f = gpui::Font { weight: FontWeight::MEDIUM, ..font(FONT) };
            let measure = |text: &str| {
                let run = TextRun { len: text.len(), font: f.clone(), color: gpui::black(), background_color: None, underline: None, strikethrough: None };
                f32::from(window.text_system().shape_line(SharedString::from(text.to_string()), px(FONT_SIZE), &[run], None).width)
            };
            let widths: Vec<f32> = self.words.iter().map(|w| measure(w)).collect();
            // A few pixels of slack for rounding between measured and laid-out text.
            self.lines = Some(wrap(&widths, WORD_GAP, WIDTH - PAD_X * 2. - 4.));
        }
        self.lines.as_deref().unwrap_or_default()
    }

    fn on_wheel(&mut self, e: &ScrollWheelEvent, _: &mut Window, cx: &mut Context<Self>) {
        self.wheel += f32::from(e.delta.pixel_delta(px(LINE_H)).y);
        let steps = (self.wheel / LINE_H).trunc();
        if steps != 0. {
            self.wheel -= steps * LINE_H;
            self.manual -= steps as i32;
            cx.notify();
        }
    }
}

impl Render for Prompter {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let index = self.follower.as_ref().and_then(Follower::index);
        if index != self.last_index {
            self.last_index = index;
            self.manual = 0;
        }
        let status = self.follower.as_ref().and_then(Follower::status);
        let visible = self.visible;
        let lines = self.layout(window).to_vec();
        let max_top = lines.len().saturating_sub(visible) as i32;
        let top = (top_line(&lines, index, visible) as i32 + self.manual).clamp(0, max_top);
        self.manual = top - top_line(&lines, index, visible) as i32;

        let note: SharedString = match &status {
            None => "Scroll to move through the script".into(),
            Some(Status::Downloading { done, total }) => {
                format!("Downloading the speech model · {}%", done * 100 / (*total).max(1)).into()
            }
            Some(Status::Loading) => "Loading the speech model…".into(),
            Some(Status::Listening) => "Following your voice".into(),
            Some(Status::Failed(_)) => "Voice following is unavailable · scroll to move".into(),
        };
        let tip = match &status {
            Some(Status::Failed(e)) => e.clone(),
            _ => String::new(),
        };
        let listening = status == Some(Status::Listening);

        let rows = lines.iter().skip(top as usize).take(visible).map(|line| {
            div().h(px(LINE_H)).flex().items_center().gap(px(WORD_GAP)).children(line.clone().map(|i| {
                let spoken = index.is_some_and(|a| i <= a);
                div()
                    .child(self.words[i].clone())
                    .text_color(if spoken { ui::accent() } else { rgb(0xF4F4F6) })
                    .when(index == Some(i), |d| d.bg(ui::accent_alpha(0x40)).rounded(px(3.)))
            }))
        });

        div().id("prompter").size_full().font_family(FONT).on_scroll_wheel(cx.listener(Self::on_wheel)).child(
            div()
                .size_full()
                .flex()
                .flex_col()
                .px(px(PAD_X))
                .pt(px(PAD_Y))
                .pb(px(PAD_Y))
                .rounded_b(px(16.))
                .bg(rgba(0x0B0B0DF2))
                .border_1()
                .border_t_0()
                .border_color(rgba(0xFFFFFF26))
                .text_size(px(FONT_SIZE))
                .font_weight(FontWeight::MEDIUM)
                .child(div().flex().flex_col().h(px(visible as f32 * LINE_H)).overflow_hidden().children(rows))
                .child(
                    div()
                        .id("prompter-status")
                        .h(px(STATUS_H))
                        .flex()
                        .items_end()
                        .gap_1p5()
                        .text_xs()
                        .text_color(rgba(0xFFFFFF99))
                        .child(div().size(px(6.)).mb(px(5.)).rounded_full().bg(if listening { rgb(0x34C759) } else { rgba(0xFFFFFF99) }))
                        .child(note)
                        .when(!tip.is_empty(), |d| d.tooltip(ui::tooltip(tip))),
                ),
        )
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn wraps_words_greedily_and_keeps_long_words_whole() {
        assert_eq!(wrap(&[30., 30., 30., 30.], 10., 75.), vec![0..2, 2..4]);
        assert_eq!(wrap(&[100., 20.], 10., 75.), vec![0..1, 1..2], "a word wider than a line gets its own");
        assert_eq!(wrap(&[], 10., 75.), Vec::<Range<usize>>::new());
    }

    #[test]
    fn the_line_being_read_scrolls_to_the_top() {
        let lines = vec![0..3, 3..6, 6..9, 9..12];
        assert_eq!(top_line(&lines, None, 2), 0);
        assert_eq!(top_line(&lines, Some(1), 2), 0);
        assert_eq!(top_line(&lines, Some(2), 2), 1, "the last word of a line brings the next up");
        assert_eq!(top_line(&lines, Some(4), 2), 1);
        assert_eq!(top_line(&lines, Some(11), 2), 2, "never past the last full page");
    }
}
