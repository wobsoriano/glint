//! The Captions tab: on-device transcription, the caption line editor, and Edit Video, where
//! the transcript's words cut the footage. Cuts and caption edits go through `Studio::edit`,
//! so they are undoable and the preview and export follow them.

use super::geometry::format_time;
use super::sidebar::{eid, hint};
use super::{Studio, Tab};
use crate::settings;
use crate::ui::{self, OnValue};
use glint_media::read_wav;
use glint_transcribe::{
    filler_ranges, is_installed, silence_cut_ranges, silence_spans, transcribe, word_cut_ranges, Cancelled, Model, Pcm, SilenceOptions, TimeRange,
    Transcript,
};
use gpui::prelude::*;
use gpui::{div, px, relative, Context, KeyDownEvent};
use std::ops::Range;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

/// Models offered in Studio, fastest first. Small is left out: it is several times slower
/// than Base on a CPU.
const MODELS: [(Model, &str); 3] = [(Model::TinyEn, "Fast"), (Model::BaseEn, "Accurate"), (Model::Base, "Multilingual")];
const SILENCE_CHOICES: [(u64, &str); 3] = [(500, "0.5 s"), (1_000, "1 s"), (2_000, "2 s")];
/// Quiet kept on each side of a trimmed silence so the narration still breathes.
const SILENCE_KEEP_MS: u64 = 150;
const SILENCE_MIN_CUT_MS: u64 = 200;
const POSITIONS: [(f32, &str); 3] = [(0.1, "Top"), (0.5, "Middle"), (0.92, "Bottom")];

#[derive(Clone, Copy, PartialEq, Eq)]
pub(super) enum View {
    Captions,
    EditVideo,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Source {
    Mic,
    System,
}

#[derive(Clone, Copy)]
enum Stage {
    Downloading(f32),
    Loading,
    Transcribing(f32),
}

struct Job {
    stage: Arc<Mutex<Stage>>,
    cancel: Arc<AtomicBool>,
}

pub(super) struct CaptionsUi {
    pub(super) view: View,
    model: Model,
    source: Source,
    job: Option<Job>,
    /// Shows the transcribe controls again for a session that already has a transcript.
    redo_setup: bool,
    /// The caption cue being edited and its text so far.
    editing: Option<(usize, String)>,
    /// Selected transcript words: the anchor of a Shift-click and the word clicked last.
    words: Option<(usize, usize)>,
    silence_min_ms: u64,
    trimming: bool,
}

impl CaptionsUi {
    pub(super) fn new(has_mic: bool) -> Self {
        let data = settings::data_dir();
        let model = [Model::BaseEn, Model::TinyEn, Model::Base].into_iter().find(|m| is_installed(&data, *m)).unwrap_or(Model::BaseEn);
        Self {
            view: View::Captions,
            model,
            source: if has_mic { Source::Mic } else { Source::System },
            job: None,
            redo_setup: false,
            editing: None,
            words: None,
            silence_min_ms: 1_000,
            trimming: false,
        }
    }

    pub(super) fn is_busy(&self) -> bool {
        self.job.is_some() || self.trimming
    }

    pub(super) fn cancel(&self) {
        if let Some(job) = &self.job {
            job.cancel.store(true, Ordering::Relaxed);
        }
    }

    pub(super) fn is_editing(&self) -> bool {
        self.editing.is_some()
    }

    fn selected_words(&self) -> Option<Range<usize>> {
        self.words.map(|(a, b)| a.min(b)..a.max(b) + 1)
    }
}

impl Studio {
    fn audio_path(&self, source: Source) -> Option<PathBuf> {
        let t = &self.session.tracks;
        match source {
            Source::Mic => t.mic.clone().or_else(|| t.system.clone()),
            Source::System => t.system.clone().or_else(|| t.mic.clone()),
        }
    }

    // --- transcription -------------------------------------------------------------------

    fn start_transcription(&mut self, cx: &mut Context<Self>) {
        if self.cap.job.is_some() {
            return;
        }
        let Some(audio) = self.audio_path(self.cap.source) else {
            return self.notify("This recording has no audio to transcribe", true, cx);
        };
        let model = self.cap.model;
        let data = settings::data_dir();
        let stage = Arc::new(Mutex::new(Stage::Downloading(0.0)));
        let cancel = Arc::new(AtomicBool::new(false));
        self.cap.job = Some(Job { stage: stage.clone(), cancel: cancel.clone() });
        let task = cx.background_spawn(async move {
            let set = |s: Stage| *stage.lock().unwrap_or_else(|e| e.into_inner()) = s;
            let model_path = glint_transcribe::download_model(&data, model, |done, total| set(Stage::Downloading(done as f32 / total.max(1) as f32)), &cancel)?;
            set(Stage::Loading);
            let wav = read_wav(&audio)?;
            let pcm = Pcm { samples: &wav.samples, sample_rate: wav.sample_rate, channels: wav.channels };
            let progress = stage.clone();
            transcribe(&model_path, pcm, move |p| *progress.lock().unwrap_or_else(|e| e.into_inner()) = Stage::Transcribing(p), cancel)
        });
        self.kick(cx);
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |s: &mut Studio, cx| {
                s.cap.job = None;
                match result {
                    Ok(t) => s.transcribed(t, cx),
                    Err(e) if e.is::<Cancelled>() => s.notify("Transcription cancelled", false, cx),
                    Err(e) => s.notify(format!("Transcription failed: {e:#}"), true, cx),
                }
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }

    /// Saves the transcript and makes captions from it, as one undoable step.
    fn transcribed(&mut self, transcript: Transcript, cx: &mut Context<Self>) {
        if transcript.words.is_empty() {
            return self.notify("No speech found in this recording", false, cx);
        }
        let cues = glint_studio::caption_cues(&transcript);
        let words = transcript.words.len();
        if let Err(e) = self.session.set_transcript(transcript) {
            return self.notify(format!("Couldn't save the transcript: {e:#}"), true, cx);
        }
        self.player.set_assets(self.session.assets.clone());
        self.cap.redo_setup = false;
        self.cap.words = None;
        self.cap.editing = None;
        self.edit(None, cx, |p| {
            p.caption_cues = cues;
            p.captions.enabled = true;
        });
        self.notify(format!("Transcribed {words} words"), false, cx);
    }

    // --- caption lines -------------------------------------------------------------------

    fn edit_cue(&mut self, i: usize, cx: &mut Context<Self>) {
        self.commit_cue(cx);
        if let Some(cue) = self.project().caption_cues.get(i) {
            self.cap.editing = Some((i, cue.text.clone()));
        }
        cx.notify();
    }

    fn commit_cue(&mut self, cx: &mut Context<Self>) {
        let Some((i, text)) = self.cap.editing.take() else { return };
        let text = text.split_whitespace().collect::<Vec<_>>().join(" ");
        self.edit(None, cx, |p| {
            if let Some(cue) = p.caption_cues.get_mut(i) {
                cue.text = text;
            }
        });
    }

    pub(super) fn caption_key(&mut self, e: &KeyDownEvent, cx: &mut Context<Self>) {
        let Some((_, text)) = self.cap.editing.as_mut() else { return };
        match ui::edit_text(text, e) {
            ui::Edit::Submit => self.commit_cue(cx),
            ui::Edit::Cancel => self.cap.editing = None,
            _ => {}
        }
        cx.notify();
    }

    /// Output time where source range `src` first shows, or where the footage after it
    /// resumes when all of it was cut.
    fn output_at(&self, src: Range<f64>) -> f64 {
        let p = self.project();
        p.output_ranges(src.clone())
            .first()
            .or(p.output_ranges(src.start..self.session.source_duration_ms).first())
            .map_or(self.duration(), |r| r.start)
    }

    // --- edit video ----------------------------------------------------------------------

    fn click_word(&mut self, i: usize, shift: bool, cx: &mut Context<Self>) {
        self.cap.words = match (shift, self.cap.words) {
            (true, Some((anchor, _))) => Some((anchor, i)),
            _ => Some((i, i)),
        };
        if let Some(w) = self.session.transcript.as_ref().and_then(|t| t.words.get(i)) {
            let at = self.output_at(w.start_ms as f64..w.end_ms as f64);
            if self.player.is_playing() {
                self.player.pause();
            }
            self.seek(at, cx);
        }
        cx.notify();
    }

    pub(super) fn has_word_selection(&self) -> bool {
        self.tab == Tab::Captions && self.cap.view == View::EditVideo && self.cap.words.is_some()
    }

    pub(super) fn clear_word_selection(&mut self) -> bool {
        self.cap.words.take().is_some()
    }

    pub(super) fn cut_selected_words(&mut self, cx: &mut Context<Self>) {
        let (Some(t), Some(sel)) = (&self.session.transcript, self.cap.selected_words()) else { return };
        let ranges = word_cut_ranges(&t.words, &[sel], self.session.source_duration_ms as u64);
        self.cap.words = None;
        self.cut(ranges, |ms| format!("Cut {}", seconds(ms)), cx);
    }

    fn remove_fillers(&mut self, cx: &mut Context<Self>) {
        let Some(t) = &self.session.transcript else { return };
        let p = self.project();
        let fillers: Vec<Range<usize>> = filler_ranges(&t.words)
            .into_iter()
            .filter(|r| t.words[r.clone()].iter().any(|w| p.event_output_time(w.start_ms as f64).is_some()))
            .collect();
        if fillers.is_empty() {
            return self.notify("No filler words left to remove", false, cx);
        }
        let n = fillers.iter().map(|r| r.len()).sum::<usize>();
        let ranges = word_cut_ranges(&t.words, &fillers, self.session.source_duration_ms as u64);
        self.cut(ranges, move |ms| format!("Removed {n} filler word{} ({})", if n == 1 { "" } else { "s" }, seconds(ms)), cx);
    }

    fn trim_silences(&mut self, cx: &mut Context<Self>) {
        if self.cap.trimming {
            return;
        }
        let Some(audio) = self.audio_path(self.cap.source) else {
            return self.notify("This recording has no audio to trim", true, cx);
        };
        let options = SilenceOptions { min_duration_ms: self.cap.silence_min_ms, ..SilenceOptions::default() };
        self.cap.trimming = true;
        let task = cx.background_spawn(async move {
            let wav = read_wav(&audio)?;
            let spans = silence_spans(Pcm { samples: &wav.samples, sample_rate: wav.sample_rate, channels: wav.channels }, &options);
            anyhow::Ok(silence_cut_ranges(&spans, SILENCE_KEEP_MS, SILENCE_MIN_CUT_MS))
        });
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |s: &mut Studio, cx| {
                s.cap.trimming = false;
                match result {
                    Ok(ranges) if ranges.is_empty() => s.notify("No silences that long", false, cx),
                    Ok(ranges) => s.cut(ranges, |ms| format!("Trimmed {} of silence", seconds(ms)), cx),
                    Err(e) => s.notify(format!("Couldn't read the audio: {e:#}"), true, cx),
                }
            });
        })
        .detach();
        cx.notify();
    }

    /// Removes source ranges from the clips as one undo step and reports the time saved.
    fn cut(&mut self, ranges: Vec<TimeRange>, message: impl FnOnce(f64) -> String, cx: &mut Context<Self>) {
        let ranges: Vec<Range<f64>> = ranges.iter().map(|r| r.start_ms as f64..r.end_ms as f64).collect();
        let before = self.duration();
        if !self.edit(None, cx, |p| p.cut_source_ranges(&ranges)) {
            return self.notify("That would remove the whole recording", true, cx);
        }
        let saved = before - self.duration();
        if saved <= 0.0 {
            return self.notify("Already cut", false, cx);
        }
        self.notify(message(saved), false, cx);
    }

    // --- rendering -----------------------------------------------------------------------

    pub(super) fn captions_tab(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let t = &self.session.tracks;
        if t.mic.is_none() && t.system.is_none() {
            return div().child(hint("This recording has no audio, so there is nothing to transcribe."));
        }
        if self.cap.job.is_some() {
            return div().flex().flex_col().child(self.job_section(cx));
        }
        if self.session.transcript.is_none() || self.cap.redo_setup {
            return div().flex().flex_col().child(self.setup_section(cx));
        }
        let view = self.cap.view;
        let switch = div()
            .flex()
            .gap_1()
            .mb_2()
            .child(ui::chip("cap-view-captions", "Captions", view == View::Captions, cx.listener(|s, _, _, cx| {
                s.cap.view = View::Captions;
                cx.notify();
            })))
            .child(ui::chip("cap-view-edit", "Edit video", view == View::EditVideo, cx.listener(|s, _, _, cx| {
                s.commit_cue(cx);
                s.cap.view = View::EditVideo;
                cx.notify();
            })))
            .child(div().flex_1())
            .child(ui::icon_button("cap-redo", "restart", false, "Transcribe again", cx.listener(|s, _, _, cx| {
                s.cap.redo_setup = true;
                cx.notify();
            })));
        let body = match view {
            View::Captions => self.captions_view(cx),
            View::EditVideo => self.edit_video_view(cx),
        };
        div().flex().flex_col().child(switch).child(body)
    }

    fn setup_section(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let data = settings::data_dir();
        let model = self.cap.model;
        let installed = is_installed(&data, model);
        let t = &self.session.tracks;
        let both = t.mic.is_some() && t.system.is_some();
        let source = self.cap.source;
        let again = self.session.transcript.is_some();
        ui::section("Transcribe")
            .child(hint("Turns the narration into text on this PC. Nothing is uploaded."))
            .child(div().text_xs().text_color(ui::theme().muted).child("Model"))
            .child(div().flex().gap_1().children(MODELS.iter().map(|(m, label)| {
                let m = *m;
                ui::chip(eid("cap-model", m as usize), *label, model == m, cx.listener(move |s, _, _, cx| {
                    s.cap.model = m;
                    cx.notify();
                }))
            })))
            .child(hint(if installed {
                format!("{} is installed.", model.label())
            } else {
                format!("{} downloads once ({:.0} MB).", model.label(), model.size_bytes() as f64 / 1e6)
            }))
            .when(both, |d| {
                d.child(div().text_xs().text_color(ui::theme().muted).child("Audio"))
                    .child(
                        div()
                            .flex()
                            .gap_1()
                            .child(ui::chip("cap-src-mic", "Microphone", source == Source::Mic, cx.listener(|s, _, _, cx| {
                                s.cap.source = Source::Mic;
                                cx.notify();
                            })))
                            .child(ui::chip("cap-src-sys", "System audio", source == Source::System, cx.listener(|s, _, _, cx| {
                                s.cap.source = Source::System;
                                cx.notify();
                            }))),
                    )
            })
            .child(
                div()
                    .flex()
                    .gap_2()
                    .child(ui::button("cap-transcribe", if again { "Transcribe again" } else { "Transcribe" }, Some("sparkles"), true, cx.listener(|s, _, _, cx| s.start_transcription(cx))))
                    .when(again, |d| {
                        d.child(ui::button("cap-setup-back", "Back", None, false, cx.listener(|s, _, _, cx| {
                            s.cap.redo_setup = false;
                            cx.notify();
                        })))
                    }),
            )
            .when(again, |d| d.child(hint("A new transcript replaces the caption lines and their edits.")))
    }

    fn job_section(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let stage = self.cap.job.as_ref().map(|j| *j.stage.lock().unwrap_or_else(|e| e.into_inner()));
        let (title, p) = match stage {
            Some(Stage::Downloading(p)) => (format!("Downloading {}\u{2026}", self.cap.model.label()), Some(p)),
            Some(Stage::Transcribing(p)) => ("Transcribing\u{2026}".to_string(), Some(p)),
            Some(Stage::Loading) | None => ("Preparing\u{2026}".to_string(), None),
        };
        ui::section("Transcribe")
            .child(div().text_sm().child(title))
            .child(
                div()
                    .h(px(6.))
                    .w_full()
                    .rounded_full()
                    .bg(ui::theme().control_hover)
                    .child(div().h_full().w(relative(p.unwrap_or(0.0).clamp(0.0, 1.0))).rounded_full().bg(ui::accent())),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .justify_between()
                    .child(div().text_sm().text_color(ui::theme().muted).child(p.map(|p| format!("{:.0}%", p * 100.0)).unwrap_or_default()))
                    .child(ui::button("cap-cancel", "Cancel", None, false, cx.listener(|s, _, _, cx| {
                        s.cap.cancel();
                        cx.notify();
                    }))),
            )
    }

    fn captions_view(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let weak = cx.entity().downgrade();
        let c = self.project().captions;
        let size: OnValue = Rc::new(move |v, _, cx| {
            let _ = weak.update(cx, |s, cx| s.edit(Some("cap-size"), cx, |p| p.captions.size = (v * 20.0).round() / 20.0));
        });
        let style = ui::section("Captions")
            .child(ui::toggle("cap-on", "Show captions", c.enabled, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.captions.enabled = !p.captions.enabled))))
            .child(ui::toggle("cap-karaoke", "Highlight spoken word", c.karaoke, cx.listener(|s, _, _, cx| s.edit(None, cx, |p| p.captions.karaoke = !p.captions.karaoke))))
            .child(div().flex().gap_1().children(POSITIONS.iter().enumerate().map(|(i, (pos, label))| {
                let pos = *pos;
                let near = POSITIONS.iter().min_by(|a, b| (a.0 - c.position).abs().total_cmp(&(b.0 - c.position).abs())).map(|p| p.0);
                ui::chip(eid("cap-pos", i), *label, near == Some(pos), cx.listener(move |s, _, _, cx| s.edit(None, cx, |p| p.captions.position = pos)))
            })))
            .child(ui::slider("cap-size", "Size", c.size, 0.6, 1.8, format!("{:.0}%", c.size * 100.0), size));

        let p = self.project();
        let src = p.source_time(self.position());
        let editing = self.cap.editing.clone();
        let rows = p.caption_cues.iter().enumerate().map(|(i, cue)| {
            let shown = p.output_ranges(cue.start_ms..cue.end_ms).first().map(|r| r.start);
            let active = shown.is_some() && cue.start_ms <= src && src < cue.end_ms;
            let at = self.output_at(cue.start_ms..cue.end_ms);
            let text: gpui::AnyElement = match &editing {
                Some((e, text)) if *e == i => div()
                    .px_1()
                    .rounded(px(4.))
                    .bg(ui::theme().field)
                    .border_1()
                    .border_color(ui::accent())
                    .text_sm()
                    .child(format!("{text}\u{258f}"))
                    .into_any_element(),
                _ => div()
                    .id(eid("cap-text", i))
                    .flex_1()
                    .min_w_0()
                    .px_1()
                    .rounded(px(4.))
                    .cursor_text()
                    .text_sm()
                    .hover(|s| s.bg(ui::theme().control_hover))
                    .when(shown.is_none(), |d| d.line_through().text_color(ui::theme().muted))
                    .when(cue.text.is_empty(), |d| d.text_color(ui::theme().muted).child("(empty)"))
                    .child(cue.text.clone())
                    .on_click(cx.listener(move |s, _, _, cx| s.edit_cue(i, cx)))
                    .into_any_element(),
            };
            div()
                .flex()
                .items_start()
                .gap_2()
                .py_1()
                .px_1()
                .rounded(px(4.))
                .when(active, |d| d.bg(ui::accent_alpha(0x26)))
                .child(
                    div()
                        .id(eid("cap-time", i))
                        .flex_none()
                        .w(px(48.))
                        .pt(px(2.))
                        .text_xs()
                        .cursor_pointer()
                        .text_color(ui::accent())
                        .hover(|s| s.underline())
                        .child(format_time(at))
                        .on_click(cx.listener(move |s, _, _, cx| {
                            s.commit_cue(cx);
                            s.seek(at, cx);
                        })),
                )
                .child(div().flex_1().min_w_0().child(text))
        });
        let lines = ui::section("Lines")
            .child(hint("Click a time to jump there. Click a line to fix its text, Enter to save."))
            .children(rows);
        div().flex().flex_col().child(style).child(lines)
    }

    fn edit_video_view(&mut self, cx: &mut Context<Self>) -> gpui::Div {
        let p = self.project();
        let src = p.source_time(self.position());
        let sel = self.cap.selected_words();
        let words = self.session.transcript.as_ref().map(|t| t.words.as_slice()).unwrap_or_default();
        let active = words.partition_point(|w| (w.start_ms as f64) <= src).checked_sub(1).filter(|&i| src < words[i].end_ms as f64);
        let t = ui::theme();
        let flow = div().flex().flex_wrap().gap_x(px(3.)).gap_y(px(2.)).children(words.iter().enumerate().map(|(i, w)| {
            let cut = p.event_output_time(w.start_ms as f64).is_none();
            let selected = sel.as_ref().is_some_and(|r| r.contains(&i));
            div()
                .id(eid("word", i))
                .px(px(2.))
                .rounded(px(3.))
                .text_sm()
                .cursor_pointer()
                .when(cut, |d| d.line_through().text_color(t.muted))
                .when(selected, |d| d.bg(ui::accent()).text_color(t.on_accent))
                .when(!selected && active == Some(i), |d| d.bg(ui::accent_alpha(0x40)))
                .when(!selected, |d| d.hover(|s| s.bg(t.control_hover)))
                .child(w.text.clone())
                .on_click(cx.listener(move |s, e: &gpui::ClickEvent, _, cx| s.click_word(i, e.modifiers().shift, cx)))
        }));
        let has_sel = sel.is_some();
        let min = self.cap.silence_min_ms;
        let trimming = self.cap.trimming;
        div()
            .flex()
            .flex_col()
            .child(
                ui::section("Transcript")
                    .child(hint("Click a word to jump to it. Shift-click to select a passage, then press Delete to cut it."))
                    .child(flow)
                    .child(
                        div()
                            .flex()
                            .gap_2()
                            .child(div().when(!has_sel, |d| d.opacity(0.4)).child(ui::button("cap-cut", "Cut selection", Some("scissors"), false, cx.listener(|s, _, _, cx| s.cut_selected_words(cx)))))
                            .when(has_sel, |d| {
                                d.child(ui::button("cap-clear", "Clear", None, false, cx.listener(|s, _, _, cx| {
                                    s.cap.words = None;
                                    cx.notify();
                                })))
                            }),
                    ),
            )
            .child(
                ui::section("Clean up")
                    .child(ui::button("cap-fillers", "Remove filler words", Some("sparkles"), false, cx.listener(|s, _, _, cx| s.remove_fillers(cx))))
                    .child(hint("Cuts um, uh, er and similar sounds."))
                    .child(div().text_xs().text_color(t.muted).child("Trim silences longer than"))
                    .child(div().flex().gap_1().children(SILENCE_CHOICES.iter().enumerate().map(|(i, (ms, label))| {
                        let ms = *ms;
                        ui::chip(eid("cap-silence", i), *label, min == ms, cx.listener(move |s, _, _, cx| {
                            s.cap.silence_min_ms = ms;
                            cx.notify();
                        }))
                    })))
                    .child(ui::button("cap-silences", if trimming { "Trimming\u{2026}" } else { "Trim silences" }, Some("scissors"), false, cx.listener(|s, _, _, cx| s.trim_silences(cx)))),
            )
    }
}

fn seconds(ms: f64) -> String {
    format!("{:.1} s", ms / 1000.0)
}
