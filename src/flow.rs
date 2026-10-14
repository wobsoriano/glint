//! Capture entry points and what happens after a capture: every screenshot is imported
//! into the Library first, then the after-capture automation runs on the imported item.

use crate::capture::{self, Capture};
use crate::library::Item;
use crate::settings::Settings;
use crate::{actions, editor, ocr, overlay, pin, redact, stack, store, toast, win};
use gpui::{AnyWindowHandle, App, AppContext as _, Global};
use std::path::PathBuf;
use std::sync::Arc;
use std::time::Duration;

#[derive(Default)]
pub struct AppState {
    pub last: Option<PathBuf>,
    pub overlays: Vec<AnyWindowHandle>,
}

impl Global for AppState {}

pub fn start_area(mode: overlay::Mode, cx: &mut App) {
    if !cx.global::<AppState>().overlays.is_empty() {
        return;
    }
    let shots = match capture::capture_monitors() {
        Ok(s) => s,
        Err(e) => {
            toast::show(format!("Capture failed: {e}"), cx);
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
    if let Some(mut cap) = capture::crop_screen(&shots, w.rect, Some(w.title)) {
        if cx.global::<Settings>().window_shadow {
            cap.image = Arc::new(capture::with_shadow(&cap.image, cap.dpi));
        }
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

/// Full screen after the configured self-timer.
pub fn capture_after_timer(cx: &mut App) {
    let secs = cx.global::<Settings>().timer_seconds.max(1);
    toast::show(format!("Capturing in {secs} seconds…"), cx);
    cx.spawn(async move |cx| {
        gpui::Timer::after(Duration::from_secs(secs as u64)).await;
        let _ = cx.update(capture_fullscreen);
    })
    .detach();
}

pub fn open_last(cx: &mut App) {
    match cx.global::<AppState>().last.clone().and_then(|dir| store::get(&dir, cx)) {
        Some(item) => actions::edit(&item, cx),
        None => toast::show("No captures yet", cx),
    }
}

pub fn finish(cap: Capture, cx: &mut App) {
    let s = cx.global::<Settings>().clone();
    if s.auto_redact {
        // OCR takes a moment; run it off the main thread, then continue.
        let img = cap.image.clone();
        let rules = s.redact_rules.clone();
        let pad = 2.0 * cap.dpi;
        let task = cx.background_spawn(async move { ocr::recognize(&img).map(|lines| redact::find(&lines, &rules, pad)) });
        cx.spawn(async move |cx| {
            let findings = task.await.unwrap_or_default();
            let _ = cx.update(|cx| import(cap, findings, cx));
        })
        .detach();
    } else {
        import(cap, Vec::new(), cx);
    }
}

/// A capture that is in the Library, plus what the editor needs to keep redactions editable.
#[derive(Clone)]
pub struct Shot {
    pub item: Item,
    pub cap: Capture,
    pub redactions: Vec<crate::annotate::Annotation>,
    pub image: Arc<image::RgbaImage>,
}

fn import(cap: Capture, findings: Vec<redact::Finding>, cx: &mut App) {
    let redactions = editor::redactions_from(&findings, cx.global::<Settings>().redact_style);
    let image = Arc::new(editor::flatten_redactions(&cap, &redactions));
    let lib = store::library(cx);
    let (base, deliverable, created, source) = (cap.image.clone(), image.clone(), cap.created, cap.title.clone());
    let task = cx.background_spawn(async move { lib.import_screenshot(&base, &deliverable, created, source, None, None) });
    cx.spawn(async move |cx| {
        let result = task.await;
        let _ = cx.update(|cx| match result {
            Ok(item) => {
                store::upsert(item.clone(), cx);
                cx.global_mut::<AppState>().last = Some(item.dir.clone());
                if !findings.is_empty() {
                    toast::show(format!("Hid {} sensitive item(s)", findings.len()), cx);
                }
                automate(Shot { item, cap, redactions, image }, cx);
            }
            Err(e) => toast::show(format!("Could not add the capture to the Library: {e:#}"), cx),
        });
    })
    .detach();
}

fn automate(shot: Shot, cx: &mut App) {
    let a = cx.global::<Settings>().after_shot.clone();
    if a.copy {
        if let Err(e) = win::copy_image(&shot.image) {
            toast::show(format!("Copy failed: {e}"), cx);
        }
    }
    if a.save {
        actions::save(&shot.item, false, cx, Box::new(|r, cx| actions::report_save(&r, cx)));
    }
    if a.pin {
        pin::open(shot.image.clone(), shot.cap.dpi, cx);
    }
    if a.edit {
        editor::open_with(shot.cap.clone(), shot.redactions.clone(), cx);
    }
    if a.preview {
        stack::push(shot, cx);
    } else if a.copy && !a.edit && !a.pin {
        toast::show("Copied to clipboard", cx);
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
                toast::show(format!("Copied text: {}", preview.replace('\n', " ")), cx);
            }
            Ok(_) => toast::show("No text found", cx),
            Err(e) => toast::show(format!("OCR failed: {e}"), cx),
        });
    })
    .detach();
}

/// Validates and imports images opened from Explorer, each in its own editor window.
pub fn open_images(paths: Vec<PathBuf>, cx: &mut App) {
    let lib = store::library(cx);
    for path in paths {
        let lib = lib.clone();
        let task = cx.background_spawn(async move {
            let item = lib.import_file(&path)?;
            let img = actions::load_pixels(&item)?;
            anyhow::Ok((item, img))
        });
        cx.spawn(async move |cx| {
            let result = task.await;
            let _ = cx.update(|cx| match result {
                Ok((item, img)) => {
                    store::upsert(item.clone(), cx);
                    let scale = crate::shell::work_area_at_cursor().1;
                    editor::open(Capture::new(img, scale, None), cx);
                }
                Err(e) => toast::show(format!("{e:#}"), cx),
            });
        })
        .detach();
    }
}
