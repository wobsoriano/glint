//! Trim & Compress: a small window that keeps a span of any video file and re-encodes it
//! smaller, next to the original. The original is only ever read.

mod model;
mod player;

use crate::settings::Settings;
use crate::ui::{self, FONT};
use crate::{shell, toast, win};
use anyhow::{bail, Context as _};
use glint_media::{available_codecs, transcode, TranscodeOptions, VideoCodec, VideoReader};
use gpui::prelude::*;
use gpui::{
    canvas, div, img, px, relative, rgba, size, App, Bounds, Context, DispatchPhase, FocusHandle, ImageSource,
    KeyDownEvent, MouseButton, MouseMoveEvent, MouseUpEvent, ObjectFit, PathPromptOptions, Pixels, Point, RenderImage,
    Timer, TitlebarOptions, WeakEntity, Window, WindowBounds, WindowOptions,
};
use model::{Options, Quality, Resolution, Source, Trim};
use player::Player;
use std::cell::Cell;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::Arc;
use std::time::Duration;

pub use model::is_video;

const THUMBS: u32 = 12;
const THUMB_H: u32 = 112;
const STRIP_H: f32 = 56.;
const HANDLE_W: f32 = 12.;

/// Asks for a video file, then opens it.
pub fn pick(cx: &mut App) {
    let rx = cx.prompt_for_paths(PathPromptOptions {
        files: true,
        directories: false,
        multiple: false,
        prompt: Some("Open".into()),
    });
    cx.spawn(async move |cx| {
        let Ok(Ok(Some(paths))) = rx.await else { return };
        let _ = cx.update(|cx| {
            for p in paths {
                open(p, cx);
            }
        });
    })
    .detach();
}

/// Opens `path` in a new Trim & Compress window.
pub fn open(path: PathBuf, cx: &mut App) {
    if !is_video(&path) {
        let name = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
        return toast::show(format!("{name} is not a video Glint can trim (MP4, MOV, M4V, WMV, AVI)"), cx);
    }
    let name = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
    let bounds = Bounds::centered(None, size(px(980.), px(760.)), cx);
    let _ = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions {
                title: Some(format!("Trim & Compress — {name}").into()),
                appears_transparent: false,
                traffic_light_position: None,
            }),
            window_min_size: Some(size(px(760.), px(600.))),
            focus: true,
            window_background: gpui::WindowBackgroundAppearance::Blurred,
            ..Default::default()
        },
        move |window, cx| {
            shell::mica(window);
            let view = cx.new(|cx| ClipEditor::new(path, cx));
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            win::bring_to_front(window);
            view
        },
    );
}

enum Stage {
    Opening,
    Failed(String),
    Ready(Box<Edit>),
}

/// An open file with its selection and settings.
struct Edit {
    source: Source,
    player: Player,
    trim: Trim,
    opts: Options,
    hevc: bool,
    thumbs: Vec<Arc<RenderImage>>,
    frame: Option<(u64, Arc<RenderImage>)>,
    drag: Option<Grab>,
    job: Job,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Grab {
    Start,
    End,
    Playhead,
}

enum Job {
    Idle,
    Running { progress: Arc<AtomicU32>, cancel: Arc<AtomicBool> },
    Done { path: PathBuf, bytes: u64 },
    Failed(String),
}

pub struct ClipEditor {
    stage: Stage,
    focus: FocusHandle,
    strip: Rc<Cell<Bounds<Pixels>>>,
}

impl ClipEditor {
    fn new(path: PathBuf, cx: &mut Context<Self>) -> Self {
        let probe_path = path.clone();
        let probe = cx.background_spawn(async move { probe(&probe_path) });
        cx.spawn(async move |this, cx| {
            let result = probe.await;
            let _ = this.update(cx, |this, cx| {
                this.stage = match result {
                    Ok(source) => Stage::Ready(Box::new(Edit {
                        trim: Trim::full(source.duration_ms),
                        player: Player::new(source.path.clone()),
                        source,
                        opts: Options::default(),
                        hevc: available_codecs().contains(&VideoCodec::Hevc),
                        thumbs: Vec::new(),
                        frame: None,
                        drag: None,
                        job: Job::Idle,
                    })),
                    Err(e) => Stage::Failed(format!("{e:#}")),
                };
                cx.notify();
            });
            let thumbs = cx.background_spawn(async move { thumbnails(&path) }).await;
            let _ = this.update(cx, |this, cx| {
                if let (Stage::Ready(edit), Ok(t)) = (&mut this.stage, thumbs) {
                    edit.thumbs = t;
                    cx.notify();
                }
            });
        })
        .detach();
        cx.spawn(async move |this, cx| loop {
            Timer::after(Duration::from_millis(16)).await;
            if this.update(cx, |this, cx| this.tick(cx)).is_err() {
                break;
            }
        })
        .detach();
        Self { stage: Stage::Opening, focus: cx.focus_handle(), strip: Rc::default() }
    }

    /// Picks up new preview frames and export progress.
    fn tick(&mut self, cx: &mut Context<Self>) {
        let Stage::Ready(edit) = &mut self.stage else { return };
        let seen = edit.frame.as_ref().map_or(0, |f| f.0);
        let mut changed = edit.player.playing() || matches!(edit.job, Job::Running { .. });
        if let Some(new) = edit.player.frame_since(seen) {
            if let Some((_, old)) = edit.frame.replace(new) {
                cx.drop_image(old, None);
            }
            changed = true;
        }
        if changed {
            cx.notify();
        }
    }

    fn toggle_play(&mut self, cx: &mut Context<Self>) {
        let Stage::Ready(e) = &mut self.stage else { return };
        if e.player.playing() {
            e.player.pause();
        } else {
            let pos = e.player.position_ms();
            if pos < e.trim.start_ms || pos + 40 >= e.trim.end_ms {
                e.player.seek(e.trim.start_ms);
            }
            e.player.play(e.trim.end_ms, !e.opts.keep_audio);
        }
        cx.notify();
    }

    fn mark(&mut self, grab: Grab, cx: &mut Context<Self>) {
        let Stage::Ready(e) = &mut self.stage else { return };
        let at = e.player.position_ms();
        e.trim = match grab {
            Grab::Start => e.trim.with_start(at),
            Grab::End => e.trim.with_end(at, e.source.duration_ms),
            Grab::Playhead => e.trim,
        };
        cx.notify();
    }

    fn time_at(&self, x: Pixels, duration_ms: u64) -> u64 {
        let b = self.strip.get();
        if b.size.width <= px(0.) {
            return 0;
        }
        let f = ((x - b.origin.x) / b.size.width).clamp(0.0, 1.0);
        (f as f64 * duration_ms as f64) as u64
    }

    fn press(&mut self, pos: Point<Pixels>, cx: &mut Context<Self>) {
        let b = self.strip.get();
        let Stage::Ready(e) = &self.stage else { return };
        let dur = e.source.duration_ms.max(1) as f32;
        let x_of = |ms: u64| b.origin.x + b.size.width * (ms as f32 / dur);
        let (xs, xe) = (x_of(e.trim.start_ms), x_of(e.trim.end_ms));
        let grab = if pos.x <= xs + px(4.) && pos.x >= xs - px(HANDLE_W + 4.) {
            Grab::Start
        } else if pos.x >= xe - px(4.) && pos.x <= xe + px(HANDLE_W + 4.) {
            Grab::End
        } else {
            Grab::Playhead
        };
        if let Stage::Ready(e) = &mut self.stage {
            e.player.pause();
            e.drag = Some(grab);
        }
        self.drag_to(pos, cx);
    }

    fn drag_to(&mut self, pos: Point<Pixels>, cx: &mut Context<Self>) {
        let Stage::Ready(e) = &self.stage else { return };
        let Some(grab) = e.drag else { return };
        let t = self.time_at(pos.x, e.source.duration_ms);
        let Stage::Ready(e) = &mut self.stage else { return };
        let frame_ms = 1000 / e.source.fps.max(1) as u64;
        match grab {
            Grab::Start => {
                e.trim = e.trim.with_start(t);
                e.player.seek(e.trim.start_ms);
            }
            Grab::End => {
                e.trim = e.trim.with_end(t, e.source.duration_ms);
                e.player.seek(e.trim.end_ms.saturating_sub(frame_ms));
            }
            Grab::Playhead => e.player.seek(t.min(e.source.duration_ms.saturating_sub(frame_ms))),
        }
        cx.notify();
    }

    fn export(&mut self, save_as: bool, cx: &mut Context<Self>) {
        let Stage::Ready(e) = &mut self.stage else { return };
        if matches!(e.job, Job::Running { .. }) {
            return;
        }
        e.player.pause();
        let topts = model::transcode_options(&e.source, e.trim, &e.opts);
        let src = e.source.path.clone();
        let stem = model::output_stem(&src);
        let dir = cx.global::<Settings>().save_dir.clone();
        if !save_as {
            return self.start(src, shell::unique_path(&dir, &shell::sanitize(&stem), "mp4"), topts, cx);
        }
        let _ = std::fs::create_dir_all(&dir);
        let rx = cx.prompt_for_new_path(&dir, Some(&format!("{stem}.mp4")));
        cx.spawn(async move |this, cx| {
            let Ok(Ok(Some(dest))) = rx.await else { return };
            let _ = this.update(cx, |this, cx| this.start(src, dest, topts, cx));
        })
        .detach();
    }

    fn start(&mut self, src: PathBuf, dest: PathBuf, topts: TranscodeOptions, cx: &mut Context<Self>) {
        let Stage::Ready(e) = &mut self.stage else { return };
        let progress = Arc::new(AtomicU32::new(0));
        let cancel = Arc::new(AtomicBool::new(false));
        e.job = Job::Running { progress: progress.clone(), cancel: cancel.clone() };
        let task = cx.background_spawn(async move {
            let result = write_clip(&src, &dest, &topts, &cancel, &progress);
            (dest, result)
        });
        cx.spawn(async move |this, cx| {
            let (dest, result) = task.await;
            let _ = this.update(cx, |this, cx| {
                let Stage::Ready(e) = &mut this.stage else { return };
                e.job = match result {
                    Ok(bytes) => {
                        toast::show(format!("Exported {}", crate::actions::human_bytes(bytes)), cx);
                        Job::Done { path: dest, bytes }
                    }
                    Err(_) if matches!(&e.job, Job::Running { cancel, .. } if cancel.load(Ordering::Relaxed)) => Job::Idle,
                    Err(err) => Job::Failed(format!("{err:#}")),
                };
                cx.notify();
            });
        })
        .detach();
        cx.notify();
    }

    fn cancel(&mut self, cx: &mut Context<Self>) {
        if let Stage::Ready(e) = &self.stage {
            if let Job::Running { cancel, .. } = &e.job {
                cancel.store(true, Ordering::Relaxed);
            }
        }
        cx.notify();
    }

    fn set_opts(&mut self, cx: &mut Context<Self>, f: impl FnOnce(&mut Options)) {
        if let Stage::Ready(e) = &mut self.stage {
            f(&mut e.opts);
            if matches!(e.job, Job::Done { .. } | Job::Failed(_)) {
                e.job = Job::Idle;
            }
        }
        cx.notify();
    }
}

impl Drop for ClipEditor {
    fn drop(&mut self) {
        if let Stage::Ready(e) = &self.stage {
            if let Job::Running { cancel, .. } = &e.job {
                cancel.store(true, Ordering::Relaxed);
            }
        }
    }
}

fn probe(path: &Path) -> anyhow::Result<Source> {
    let bytes = std::fs::metadata(path).with_context(|| format!("reading {}", path.display()))?.len();
    let reader = VideoReader::open(path).context("Windows cannot decode this video")?;
    let duration_ms = reader.duration_ms();
    if duration_ms < Trim::MIN_MS {
        bail!("the video is too short to trim");
    }
    Ok(Source { path: path.to_path_buf(), size: reader.size(), fps: reader.fps().round().max(1.0) as u32, duration_ms, bytes })
}

fn thumbnails(path: &Path) -> anyhow::Result<Vec<Arc<RenderImage>>> {
    let frames = VideoReader::open(path)?.thumbnails(THUMBS)?;
    Ok(frames
        .iter()
        .map(|f| {
            let (w, h) = f.image.dimensions();
            let tw = (w as u64 * THUMB_H as u64 / h.max(1) as u64).max(1) as u32;
            win::render_image(&image::imageops::thumbnail(&f.image, tw, THUMB_H))
        })
        .collect())
}

/// Encodes beside the destination and moves the result into place, so a failed or
/// cancelled export never leaves a broken file under the chosen name.
fn write_clip(src: &Path, dest: &Path, topts: &TranscodeOptions, cancel: &AtomicBool, progress: &AtomicU32) -> anyhow::Result<u64> {
    if dest.exists() && std::fs::canonicalize(dest).ok() == std::fs::canonicalize(src).ok() {
        bail!("choose another name: the original is never overwritten");
    }
    let dir = dest.parent().context("the destination has no folder")?;
    std::fs::create_dir_all(dir).with_context(|| format!("creating {}", dir.display()))?;
    let stem = dest.file_stem().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default();
    let partial = dir.join(format!("{stem}.partial.mp4"));
    let written = transcode(src, &partial, topts, cancel, |p| progress.store(p.to_bits(), Ordering::Relaxed));
    if let Err(e) = written {
        let _ = std::fs::remove_file(&partial);
        return Err(e);
    }
    if dest.exists() {
        std::fs::remove_file(dest).with_context(|| format!("replacing {}", dest.display()))?;
    }
    std::fs::rename(&partial, dest).with_context(|| format!("writing {}", dest.display()))?;
    Ok(std::fs::metadata(dest)?.len())
}

impl Render for ClipEditor {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let t = ui::theme();
        let root = div()
            .id("clip")
            .track_focus(&self.focus)
            .size_full()
            .flex()
            .flex_col()
            .bg(t.base)
            .font_family(FONT)
            .text_color(t.text)
            .on_key_down(cx.listener(|this, e: &KeyDownEvent, _, cx| match e.keystroke.key.as_str() {
                "space" => this.toggle_play(cx),
                "i" => this.mark(Grab::Start, cx),
                "o" => this.mark(Grab::End, cx),
                _ => {}
            }));
        let body = match &self.stage {
            Stage::Opening => message("Opening…").into_any_element(),
            Stage::Failed(e) => message(format!("This video can't be opened: {e}")).into_any_element(),
            Stage::Ready(_) => return root.children(self.editor(cx)),
        };
        root.child(body)
    }
}

fn message(text: impl Into<gpui::SharedString>) -> gpui::Div {
    div().flex_1().flex().items_center().justify_center().p_8().text_sm().text_color(ui::theme().muted).child(text.into())
}

impl ClipEditor {
    fn editor(&self, cx: &mut Context<Self>) -> Vec<gpui::AnyElement> {
        let Stage::Ready(e) = &self.stage else { return Vec::new() };
        let t = ui::theme();
        let preview = div()
            .flex_1()
            .min_h_0()
            .m_3()
            .mb_0()
            .rounded(px(ui::RADIUS_WINDOW))
            .bg(rgba(0x000000FF))
            .overflow_hidden()
            .flex()
            .items_center()
            .justify_center()
            .children(e.frame.as_ref().map(|(_, f)| img(ImageSource::Render(f.clone())).size_full().object_fit(ObjectFit::Contain)));

        let pos = e.player.position_ms();
        let playing = e.player.playing();
        let transport = div()
            .flex()
            .items_center()
            .gap_3()
            .px_3()
            .pt_2()
            .child(ui::icon_button("play", if playing { "pause" } else { "play" }, false, "Play/Pause (Space)", cx.listener(|this, _, _, cx| this.toggle_play(cx))))
            .child(div().text_sm().child(format!("{} / {}", model::clock(pos), model::clock(e.source.duration_ms))))
            .child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_xs().text_color(t.muted).children(e.player.error()))
            .child(ui::button("set-in", "Set Start", None, false, cx.listener(|this, _, _, cx| this.mark(Grab::Start, cx))).tooltip(ui::tooltip("I")))
            .child(ui::button("set-out", "Set End", None, false, cx.listener(|this, _, _, cx| this.mark(Grab::End, cx))).tooltip(ui::tooltip("O")))
            .child(
                div()
                    .text_sm()
                    .text_color(t.muted)
                    .child(format!("Selection {} – {} · {:.1} s", model::clock(e.trim.start_ms), model::clock(e.trim.end_ms), e.trim.duration_ms() as f64 / 1000.0)),
            );

        vec![preview.into_any_element(), transport.into_any_element(), self.scrubber(e, cx).into_any_element(), self.settings(e, cx).into_any_element(), self.footer(e, cx).into_any_element()]
    }

    fn scrubber(&self, e: &Edit, cx: &mut Context<Self>) -> impl IntoElement {
        let t = ui::theme();
        let dur = e.source.duration_ms.max(1) as f32;
        let (fs, fe) = (e.trim.start_ms as f32 / dur, e.trim.end_ms as f32 / dur);
        let fp = (e.player.position_ms() as f32 / dur).clamp(0.0, 1.0);
        let dim = rgba(0x000000A0);
        let handle = |left: f32, offset: f32| {
            div()
                .absolute()
                .top_0()
                .left(relative(left))
                .ml(px(offset))
                .w(px(HANDLE_W))
                .h_full()
                .bg(t.accent)
                .flex()
                .items_center()
                .justify_center()
                .cursor(gpui::CursorStyle::ResizeLeftRight)
                .child(div().w(px(2.)).h(px(18.)).rounded_full().bg(t.on_accent))
        };
        let weak: WeakEntity<Self> = cx.entity().downgrade();
        let strip = self.strip.clone();
        // The handles hang outside the strip, so the padded row takes the presses.
        div().id("scrubber").px(px(HANDLE_W + 12.)).pt_2().cursor_pointer().on_mouse_down(MouseButton::Left, cx.listener(|this, ev: &gpui::MouseDownEvent, _, cx| this.press(ev.position, cx))).child(
            div()
                .relative()
                .h(px(STRIP_H))
                .w_full()
                .rounded(px(ui::RADIUS_CONTROL))
                .bg(t.canvas)
                .child(div().absolute().top_0().left_0().size_full().flex().overflow_hidden().rounded(px(ui::RADIUS_CONTROL)).children(
                    e.thumbs.iter().map(|th| img(ImageSource::Render(th.clone())).flex_1().h_full().object_fit(ObjectFit::Cover)),
                ))
                .child(div().absolute().top_0().left_0().h_full().w(relative(fs)).bg(dim))
                .child(div().absolute().top_0().left(relative(fe)).h_full().w(relative(1.0 - fe)).bg(dim))
                .child(div().absolute().top_0().left(relative(fs)).w(relative(fe - fs)).h_full().border_y_2().border_color(t.accent))
                .child(handle(fs, -HANDLE_W).rounded_l(px(ui::RADIUS_CONTROL)))
                .child(handle(fe, 0.).rounded_r(px(ui::RADIUS_CONTROL)))
                .child(div().absolute().top(px(-4.)).left(relative(fp)).ml(px(-1.)).w(px(2.)).h(px(STRIP_H + 8.)).bg(rgba(0xFFFFFFFF)).shadow_sm())
                .child(
                    canvas(
                        move |b, _, _| strip.set(b),
                        move |_, _, window, _| {
                            let w = weak.clone();
                            window.on_mouse_event(move |ev: &MouseMoveEvent, phase, _, cx| {
                                if phase == DispatchPhase::Bubble && ev.pressed_button == Some(MouseButton::Left) {
                                    let _ = w.update(cx, |this, cx| this.drag_to(ev.position, cx));
                                }
                            });
                            let w = weak.clone();
                            window.on_mouse_event(move |_: &MouseUpEvent, _, _, cx| {
                                let _ = w.update(cx, |this, _| {
                                    if let Stage::Ready(e) = &mut this.stage {
                                        e.drag = None;
                                    }
                                });
                            });
                        },
                    )
                    .absolute()
                    .top_0()
                    .left_0()
                    .size_full(),
                ),
        )
    }

    fn settings(&self, e: &Edit, cx: &mut Context<Self>) -> impl IntoElement {
        let t = ui::theme();
        let has_audio = e.player.has_audio();
        let row = |label: &str| div().flex().items_center().gap_2().child(div().w(px(84.)).text_sm().text_color(t.muted).child(label.to_string()));
        let res = row("Resolution").children(Resolution::ALL.map(|r| {
            let (w, h) = model::output_size(e.source.size, r);
            ui::chip(gpui::SharedString::from(format!("res-{}", r.label())), r.label(), e.opts.resolution == r, cx.listener(move |this, _, _, cx| this.set_opts(cx, |o| o.resolution = r)))
                .tooltip(ui::tooltip(format!("{w} × {h}")))
        }));
        let quality = row("Quality").children(Quality::ALL.map(|q| {
            let est = model::estimate_bytes(&e.source, e.trim, &Options { quality: q, ..e.opts }, has_audio == Some(true));
            ui::chip(
                gpui::SharedString::from(format!("q-{}", q.label())),
                format!("{} · ≈{}", q.label(), crate::actions::human_bytes(est)),
                e.opts.quality == q,
                cx.listener(move |this, _, _, cx| this.set_opts(cx, |o| o.quality = q)),
            )
        }));
        let mut codecs = vec![(VideoCodec::H264, "H.264")];
        if e.hevc {
            codecs.push((VideoCodec::Hevc, "HEVC"));
        }
        let codec = row("Codec").children(codecs.into_iter().map(|(c, label)| {
            ui::chip(gpui::SharedString::from(format!("c-{label}")), label, e.opts.codec == c, cx.listener(move |this, _, _, cx| this.set_opts(cx, |o| o.codec = c)))
        }));
        let audio = row("Audio").child(match has_audio {
            None => div().text_sm().text_color(t.muted).child("Reading audio…").into_any_element(),
            Some(false) => div().text_sm().text_color(t.muted).child("No audio track").into_any_element(),
            Some(true) => div()
                .flex()
                .gap_2()
                .child(ui::chip("a-keep", "Keep", e.opts.keep_audio, cx.listener(|this, _, _, cx| this.set_opts(cx, |o| o.keep_audio = true))))
                .child(ui::chip("a-drop", "Remove", !e.opts.keep_audio, cx.listener(|this, _, _, cx| this.set_opts(cx, |o| o.keep_audio = false))))
                .into_any_element(),
        });
        div()
            .mx_3()
            .mt_3()
            .p_3()
            .rounded(px(ui::RADIUS_WINDOW))
            .bg(t.layer)
            .border_1()
            .border_color(t.border)
            .flex()
            .flex_col()
            .gap_2()
            .child(res)
            .child(quality)
            .child(codec)
            .child(audio)
    }

    fn footer(&self, e: &Edit, cx: &mut Context<Self>) -> impl IntoElement {
        let t = ui::theme();
        let source_line = format!(
            "{} × {} · {} fps · {}",
            e.source.size.0,
            e.source.size.1,
            e.source.fps,
            crate::actions::human_bytes(e.source.bytes)
        );
        let bar = div().flex().items_center().gap_2().p_3();
        match &e.job {
            Job::Running { progress, .. } => {
                let p = f32::from_bits(progress.load(Ordering::Relaxed)).clamp(0.0, 1.0);
                bar.child(div().text_sm().w(px(110.)).child(format!("Exporting {:.0}%", p * 100.0)))
                    .child(div().flex_1().h(px(6.)).rounded_full().bg(t.border).child(div().h_full().w(relative(p)).rounded_full().bg(t.accent)))
                    .child(ui::button("cancel", "Cancel", None, false, cx.listener(|this, _, _, cx| this.cancel(cx))))
            }
            Job::Done { path, bytes } => {
                let saved = 1.0 - *bytes as f64 / e.source.bytes.max(1) as f64;
                let name = path.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
                let (reveal, copy) = (path.clone(), path.clone());
                bar.child(ui::icon("check", 16., t.accent))
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .overflow_hidden()
                            .whitespace_nowrap()
                            .text_ellipsis()
                            .text_sm()
                            .child(format!("Saved {name} · {} ({:+.0}%)", crate::actions::human_bytes(*bytes), -saved * 100.0)),
                    )
                    .child(ui::button("reveal", "Show in Explorer", Some("folder"), false, move |_, _, _| {
                        shell::reveal(std::slice::from_ref(&reveal))
                    }))
                    .child(ui::button("copy", "Copy", Some("copy"), false, move |_, _, cx| match shell::copy_files(std::slice::from_ref(&copy), None) {
                        Ok(()) => toast::show("Copied the video", cx),
                        Err(err) => toast::show(format!("Copy failed: {err:#}"), cx),
                    }))
                    .child(ui::button("again", "Export Again", Some("save"), true, cx.listener(|this, _, _, cx| this.export(false, cx))))
            }
            Job::Idle | Job::Failed(_) => {
                let status = match &e.job {
                    Job::Failed(err) => div().text_color(rgba(0xE5484DFF)).child(format!("Export failed: {err}")),
                    _ => div().text_color(t.muted).child(format!("Source {source_line}")),
                };
                bar.child(div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_sm().child(status))
                    .child(ui::button("save-as", "Save As…", Some("save-as"), false, cx.listener(|this, _, _, cx| this.export(true, cx))))
                    .child(ui::button("export", "Export", Some("save"), true, cx.listener(|this, _, _, cx| this.export(false, cx))))
            }
        }
    }
}
