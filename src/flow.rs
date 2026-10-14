//! Capture entry points and what happens after a capture.

use crate::capture::{self, Capture};
use crate::settings::{AfterCapture, Settings};
use crate::{editor, ocr, overlay, quick, redact, win};
use gpui::{AnyWindowHandle, App, AppContext as _, Global};

#[derive(Default)]
pub struct AppState {
    pub last: Option<Capture>,
    pub overlays: Vec<AnyWindowHandle>,
}

impl Global for AppState {}

pub fn start_area(mode: overlay::Mode, cx: &mut App) {
    if !cx.global::<AppState>().overlays.is_empty() {
        return;
    }
    quick::close(cx);
    let shots = match capture::capture_monitors() {
        Ok(s) => s,
        Err(e) => {
            quick::show_toast(format!("Capture failed: {e}"), cx);
            return;
        }
    };
    let windows = capture::list_windows();
    overlay::open(shots, windows, mode, cx);
}

pub fn capture_fullscreen(cx: &mut App) {
    let Ok(shots) = capture::capture_monitors() else { return };
    let (x, y) = capture::cursor_pos();
    let m = &shots[capture::monitor_at(&shots, x, y)];
    let cap = Capture::new((*m.image).clone(), m.scale, None);
    finish(cap, cx);
}

pub fn capture_window(cx: &mut App) {
    let Some(w) = capture::foreground_window() else {
        return capture_fullscreen(cx);
    };
    let Ok(shots) = capture::capture_monitors() else { return };
    if let Some(cap) = capture::crop_screen(&shots, w.rect, Some(w.title)) {
        finish(cap, cx);
    }
}

pub fn capture_previous(cx: &mut App) {
    let Some(rect) = cx.global::<Settings>().last_area else {
        return start_area(overlay::Mode::Image, cx);
    };
    let Ok(shots) = capture::capture_monitors() else { return };
    if let Some(cap) = capture::crop_screen(&shots, rect, None) {
        finish(cap, cx);
    }
}

pub fn open_last(cx: &mut App) {
    match cx.global::<AppState>().last.clone() {
        Some(cap) => editor::open(cap, cx),
        None => quick::show_toast("No captures yet", cx),
    }
}

/// Saves a raw capture (with auto-redactions burned in if enabled) to the save folder.
fn save_raw(img: &image::RgbaImage, cap: &Capture, cx: &App) -> anyhow::Result<std::path::PathBuf> {
    let path = cx.global::<Settings>().save_dir.join(win::file_name(&cap.created));
    win::save_png(img, &path)?;
    Ok(path)
}

pub fn finish(cap: Capture, cx: &mut App) {
    cx.global_mut::<AppState>().last = Some(cap.clone());
    let s = cx.global::<Settings>().clone();

    if s.auto_redact {
        // OCR takes a moment; run it off the main thread, then continue.
        let img = cap.image.clone();
        let rules = s.redact_rules.clone();
        let pad = 2.0 * cap.dpi;
        let task = cx.background_spawn(async move {
            ocr::recognize(&img).map(|lines| redact::find(&lines, &rules, pad))
        });
        cx.spawn(async move |cx| {
            let findings = task.await.unwrap_or_default();
            let _ = cx.update(|cx| deliver(cap, findings, cx));
        })
        .detach();
    } else {
        deliver(cap, Vec::new(), cx);
    }
}

fn deliver(cap: Capture, findings: Vec<redact::Finding>, cx: &mut App) {
    let s = cx.global::<Settings>().clone();
    let redactions = editor::redactions_from(&findings, s.redact_style);

    if s.copy_on_capture || s.save_on_capture {
        // The quick actions export the raw screenshot plus any redactions.
        let img = editor::flatten_redactions(&cap, &redactions);
        if s.copy_on_capture {
            let _ = win::copy_image(&img);
        }
        if s.save_on_capture {
            if let Err(e) = save_raw(&img, &cap, cx) {
                quick::show_toast(format!("Save failed: {e}"), cx);
            }
        }
    }

    match s.after_capture {
        AfterCapture::QuickAccess => quick::show(cap, redactions, cx),
        AfterCapture::OpenEditor => editor::open_with(cap, redactions, cx),
        AfterCapture::Nothing => {
            if !findings.is_empty() {
                quick::show_toast(format!("Redacted {} sensitive item(s)", findings.len()), cx);
            } else if s.copy_on_capture {
                quick::show_toast("Copied to clipboard", cx);
            }
        }
    }
}

pub fn finish_text(cap: Capture, cx: &mut App) {
    let img = cap.image.clone();
    let task = cx.background_spawn(async move { ocr::recognize(&img).map(|l| ocr::to_text(&l)) });
    cx.spawn(async move |cx| {
        let result = task.await;
        let _ = cx.update(|cx| match result {
            Ok(text) if !text.trim().is_empty() => {
                let _ = win::copy_text(&text);
                let preview: String = text.chars().take(60).collect();
                quick::show_toast(format!("Copied text: {}", preview.replace('\n', " ")), cx);
            }
            Ok(_) => quick::show_toast("No text found", cx),
            Err(e) => quick::show_toast(format!("OCR failed: {e}"), cx),
        });
    })
    .detach();
}
