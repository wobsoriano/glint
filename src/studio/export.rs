//! Rendering the edited recording: the Export dialog, and the Copy and Save quick actions,
//! which render with the project's remembered export options.

use super::{Notice, Studio};
use crate::settings::{self, Settings};
use crate::shell;
use crate::ui::{self, BORDER, HOVER, MUTED, PANEL};
use glint_studio::{Codec, ExportOptions, Format, Fps, Quality, Resolution};
use gpui::prelude::*;
use gpui::{div, px, relative, rgb, rgba, Context, KeyDownEvent, SharedString};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, AtomicU32, Ordering};
use std::sync::Arc;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(super) enum Job {
    /// Render with the dialog's options and save to the export folder.
    Export,
    /// Render with the project's options and save to the export folder.
    Save,
    /// Render with the project's options and put the file on the clipboard.
    Copy,
}

pub(super) enum ExportUi {
    Closed,
    Dialog { opts: ExportOptions, audio: bool },
    Running { job: Job, progress: Arc<AtomicU32>, cancel: Arc<AtomicBool> },
}

impl ExportUi {
    pub(super) fn is_running(&self) -> bool {
        matches!(self, ExportUi::Running { .. })
    }
}

const RESOLUTIONS: [(Resolution, &str); 5] = [
    (Resolution::Source, "Source"),
    (Resolution::P720, "720p"),
    (Resolution::P1080, "1080p"),
    (Resolution::P1440, "1440p"),
    (Resolution::P2160, "4K"),
];

impl Studio {
    pub(super) fn open_export(&mut self, cx: &mut Context<Self>) {
        if self.export.is_running() {
            return;
        }
        let mut opts = self.project().export;
        if !self.hevc {
            opts.codec = Codec::H264;
        }
        self.export = ExportUi::Dialog { opts, audio: true };
        cx.notify();
    }

    fn set_opts(&mut self, f: impl FnOnce(&mut ExportOptions, &mut bool), cx: &mut Context<Self>) {
        if let ExportUi::Dialog { opts, audio } = &mut self.export {
            f(opts, audio);
        }
        cx.notify();
    }

    pub(super) fn export_key(&mut self, e: &KeyDownEvent, cx: &mut Context<Self>) {
        match e.keystroke.key.as_str() {
            "escape" => self.cancel_export(cx),
            "enter" => self.run_job(Job::Export, cx),
            _ => {}
        }
    }

    fn cancel_export(&mut self, cx: &mut Context<Self>) {
        match &self.export {
            ExportUi::Running { cancel, .. } => cancel.store(true, Ordering::Relaxed),
            _ => self.export = ExportUi::Closed,
        }
        cx.notify();
    }

    /// Renders on a background thread and finishes `job` with the file.
    pub(super) fn run_job(&mut self, job: Job, cx: &mut Context<Self>) {
        if self.export.is_running() {
            return;
        }
        let mut snap = self.snapshot();
        let opts = match (&self.export, job) {
            (ExportUi::Dialog { opts, audio }, Job::Export) => {
                if !audio {
                    snap.project.audio.mic.enabled = false;
                    snap.project.audio.system.enabled = false;
                }
                // Confirmed options become the project's own, used by Copy and Save.
                let opts = *opts;
                if self.session.project.export != opts {
                    self.session.project.export = opts;
                    self.schedule_save(cx);
                }
                opts
            }
            (_, Job::Export) => return self.open_export(cx),
            _ => self.project().export,
        };
        let progress = Arc::new(AtomicU32::new(0));
        let cancel = Arc::new(AtomicBool::new(false));
        self.export = ExportUi::Running { job, progress: progress.clone(), cancel: cancel.clone() };
        let save_dir = cx.global::<Settings>().save_dir.clone();
        let stem = shell::sanitize(&self.title);
        let task = cx.background_spawn(async move {
            let rendered = glint_studio::export(&snap, &opts, |p| progress.store(p.to_bits(), Ordering::Relaxed), &cancel)?;
            match job {
                Job::Copy => {
                    let file = named_copy(&rendered, &snap.dir, &stem)?;
                    shell::copy_files(std::slice::from_ref(&file), None)?;
                    anyhow::Ok(file)
                }
                Job::Export | Job::Save => {
                    std::fs::create_dir_all(&save_dir)?;
                    let ext = rendered.extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_default();
                    let out = shell::unique_path(&save_dir, &stem, &ext);
                    std::fs::copy(&rendered, &out).map_err(|e| anyhow::anyhow!("writing {}: {e}", out.display()))?;
                    anyhow::Ok(out)
                }
            }
        });
        self.kick(cx);
        cx.spawn(async move |this, cx| {
            let result = task.await;
            let _ = this.update(cx, |s: &mut Studio, cx| {
                s.export = ExportUi::Closed;
                match result {
                    Ok(file) => {
                        let name = file.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
                        let text = match job {
                            Job::Copy => format!("Copied {name}"),
                            Job::Export | Job::Save => format!("Saved {name}"),
                        };
                        s.show_notice(Notice { text: text.into(), file: (job != Job::Copy).then_some(file), error: false }, cx);
                    }
                    Err(e) if e.to_string() == "export cancelled" => s.notify("Export cancelled", false, cx),
                    Err(e) => s.notify(format!("Export failed: {e:#}"), true, cx),
                }
            });
        })
        .detach();
        cx.notify();
    }

    pub(super) fn export_overlay(&mut self, cx: &mut Context<Self>) -> Option<impl IntoElement> {
        let card = match &self.export {
            ExportUi::Closed => return None,
            ExportUi::Dialog { opts, audio } => self.export_dialog(*opts, *audio, cx),
            ExportUi::Running { job, progress, .. } => {
                let p = f32::from_bits(progress.load(Ordering::Relaxed)).clamp(0.0, 1.0);
                let title = match job {
                    Job::Copy => "Rendering for the clipboard\u{2026}",
                    Job::Export | Job::Save => "Exporting\u{2026}",
                };
                div()
                    .flex()
                    .flex_col()
                    .gap_3()
                    .child(div().text_base().font_weight(gpui::FontWeight::SEMIBOLD).child(title))
                    .child(
                        div()
                            .h(px(6.))
                            .w_full()
                            .rounded_full()
                            .bg(rgb(HOVER))
                            .child(div().h_full().w(relative(p)).rounded_full().bg(ui::accent())),
                    )
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .justify_between()
                            .child(div().text_sm().text_color(rgb(MUTED)).child(format!("{:.0}%", p * 100.0)))
                            .child(ui::button("export-cancel", "Cancel", None, false, cx.listener(|s, _, _, cx| s.cancel_export(cx)))),
                    )
            }
        };
        Some(
            div()
                .id("export-scrim")
                .occlude()
                .absolute()
                .inset_0()
                .flex()
                .items_center()
                .justify_center()
                .bg(rgba(0x00000080))
                .child(
                    div()
                        .w(px(440.))
                        .p_5()
                        .rounded(px(ui::RADIUS_WINDOW))
                        .bg(rgb(PANEL))
                        .border_1()
                        .border_color(rgb(BORDER))
                        .shadow_lg()
                        .child(card),
                ),
        )
    }

    fn export_dialog(&mut self, opts: ExportOptions, audio: bool, cx: &mut Context<Self>) -> gpui::Div {
        let gif = opts.format == Format::Gif;
        let mut probe = self.project().clone();
        probe.export = opts;
        let (w, h) = probe.output_size(self.session.source_size);
        let has_audio = self.session.tracks.mic.is_some() || self.session.tracks.system.is_some();
        let row = |label: &'static str| {
            div().flex().items_center().gap_2().child(div().w(px(96.)).flex_none().text_sm().text_color(rgb(MUTED)).child(label))
        };
        let chips = || div().flex().flex_wrap().gap_1();
        let chip = |id: SharedString, label: &'static str, on: bool, f: Box<dyn Fn(&mut ExportOptions, &mut bool)>| {
            ui::chip(id, label, on, cx.listener(move |s, _, _, cx| s.set_opts(|o, a| f(o, a), cx)))
        };
        div()
            .flex()
            .flex_col()
            .gap_3()
            .child(div().text_base().font_weight(gpui::FontWeight::SEMIBOLD).child("Export recording"))
            .child(row("Format").child(chips().children([(Format::Mp4, "MP4"), (Format::Gif, "GIF")].map(|(f, l)| {
                chip(format!("fmt-{l}").into(), l, opts.format == f, Box::new(move |o, _| o.format = f))
            }))))
            .when(!gif, |d| {
                let mut codecs = vec![(Codec::H264, "H.264")];
                if self.hevc {
                    codecs.push((Codec::Hevc, "HEVC"));
                }
                d.child(row("Codec").child(chips().children(codecs.into_iter().map(|(c, l)| {
                    chip(format!("codec-{l}").into(), l, opts.codec == c, Box::new(move |o, _| o.codec = c))
                }))))
            })
            .child(row("Resolution").child(chips().children(RESOLUTIONS.iter().map(|(r, l)| {
                let r = *r;
                chip(format!("res-{l}").into(), l, opts.resolution == r, Box::new(move |o, _| o.resolution = r))
            }))))
            .child(row("").child(div().text_xs().text_color(rgb(MUTED)).child(if gif {
                "GIFs are 15 fps, at most 480 px on the short side".to_string()
            } else {
                format!("{w} \u{00d7} {h}")
            })))
            .child(row("Quality").child(chips().children([(Quality::Low, "Low"), (Quality::Medium, "Medium"), (Quality::High, "High")].map(|(q, l)| {
                chip(format!("q-{l}").into(), l, opts.quality == q, Box::new(move |o, _| o.quality = q))
            }))))
            .when(!gif, |d| {
                d.child(row("Frame rate").child(chips().children([(Fps::Fps30, "30 fps"), (Fps::Fps60, "60 fps")].map(|(f, l)| {
                    chip(format!("fps-{l}").into(), l, opts.fps == f, Box::new(move |o, _| o.fps = f))
                }))))
            })
            .child(ui::toggle("blur", "Motion blur", opts.motion_blur, cx.listener(|s, _, _, cx| s.set_opts(|o, _| o.motion_blur = !o.motion_blur, cx))))
            .when(!gif && has_audio, |d| d.child(ui::toggle("audio", "Include audio", audio, cx.listener(|s, _, _, cx| s.set_opts(|_, a| *a = !*a, cx)))))
            .child(
                div()
                    .flex()
                    .justify_end()
                    .gap_2()
                    .pt_2()
                    .child(ui::button("export-close", "Cancel", None, false, cx.listener(|s, _, _, cx| s.cancel_export(cx))))
                    .child(ui::button("export-go", "Export", Some("save"), true, cx.listener(|s, _, _, cx| s.run_job(Job::Export, cx)))),
            )
            .child(div().text_xs().text_color(rgb(MUTED)).child(format!("Saves to {}", cx.global::<Settings>().save_dir.display())))
    }
}

/// A copy of the render named after the recording, for pasting into other apps. Hard-linked
/// when possible so it costs no space.
fn named_copy(rendered: &Path, session: &Path, stem: &str) -> anyhow::Result<PathBuf> {
    let id = session.file_stem().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default();
    let dir = settings::data_dir().join("Drag").join(id);
    std::fs::create_dir_all(&dir)?;
    let ext = rendered.extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_default();
    let path = dir.join(format!("{stem}.{ext}"));
    let _ = std::fs::remove_file(&path);
    if std::fs::hard_link(rendered, &path).is_err() {
        std::fs::copy(rendered, &path)?;
    }
    Ok(path)
}
