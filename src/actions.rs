//! What you can do with a Library capture, shared by preview cards, the Library window
//! and Quick Look. Heavy work (decode, encode, OCR, file moves) runs off the main thread.

use crate::library::{self, CaptureKind, Item};
use crate::settings::{ExportFormat, Settings};
use crate::shell::{self, SaveChoice};
use crate::{editor, ocr, stack, store, toast, win};
use anyhow::Context as _;
use glint_render::doc::EditDoc;
use gpui::{App, AppContext as _, PathPromptOptions, Task};
use image::RgbaImage;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::Arc;

pub type Done = Box<dyn FnOnce(anyhow::Result<PathBuf>, &mut App)>;

pub fn load_pixels(item: &Item) -> anyhow::Result<RgbaImage> {
    let path = item.deliverable();
    Ok(image::open(&path).with_context(|| format!("reading {}", path.display()))?.to_rgba8())
}

/// Decodes the deliverable in the background, then continues on the main thread.
pub fn with_pixels(item: &Item, cx: &mut App, f: impl FnOnce(Arc<RgbaImage>, &mut App) + 'static) {
    if item.kind() != CaptureKind::Screenshot {
        toast::show("Recordings open in Studio, which is not part of this build yet", cx);
        return;
    }
    let item = item.clone();
    let task = cx.background_spawn(async move { load_pixels(&item) });
    cx.spawn(async move |cx| {
        let result = task.await;
        let _ = cx.update(|cx| match result {
            Ok(img) => f(Arc::new(img), cx),
            Err(e) => toast::show(format!("{e:#}"), cx),
        });
    })
    .detach();
}

fn display_scale() -> f32 {
    shell::work_area_at_cursor().1
}

pub fn copy(item: &Item, cx: &mut App) {
    if item.kind() == CaptureKind::Recording {
        return copy_files(std::slice::from_ref(item), cx);
    }
    with_pixels(item, cx, |img, cx| match win::copy_image(&img) {
        Ok(()) => toast::show("Copied to clipboard", cx),
        Err(e) => toast::show(format!("Copy failed: {e}"), cx),
    });
}

/// Copies several captures as files (paste into Explorer, chat apps or mail).
pub fn copy_files(items: &[Item], cx: &mut App) {
    let paths: Vec<PathBuf> = items.iter().map(|i| i.deliverable()).collect();
    match shell::copy_files(&paths, None) {
        Ok(()) => toast::show(format!("Copied {} file(s)", paths.len()), cx),
        Err(e) => toast::show(format!("Copy failed: {e}"), cx),
    }
}

/// Opens a screenshot in the editor on its untouched base.png, restoring edit.json when
/// present. Done and Save write the edit back into the capture and refresh every view of it.
/// A recording opens in Studio.
pub fn edit(item: &Item, cx: &mut App) {
    if item.kind() == CaptureKind::Recording {
        return crate::studio::open(cx, item.dir.clone());
    }
    let loaded = item.clone();
    let task = cx.background_spawn(async move { library::load_edit(&loaded) });
    let item = item.clone();
    cx.spawn(async move |cx| {
        let result = task.await;
        let _ = cx.update(|cx| match result {
            Ok((base, doc)) => {
                let doc = doc.unwrap_or_else(|| {
                    EditDoc::new(cx.global::<Settings>().style.clone(), display_scale(), item.meta.source.clone())
                });
                let title = item.meta.title.clone();
                editor::open_doc(Arc::new(base), doc, title, save_edit_into(item.dir), cx);
            }
            Err(e) => toast::show(format!("{e:#}"), cx),
        });
    })
    .detach();
}

fn save_edit_into(dir: PathBuf) -> editor::OnSave {
    Rc::new(move |req, cx| {
        let Some(item) = store::get(&dir, cx) else {
            return Task::ready(Err(anyhow::anyhow!("this capture is no longer in the Library")));
        };
        let write = cx.background_spawn(async move {
            let saved = library::save_edit(&item, &req.image, &req.doc)?;
            anyhow::Ok((saved, req.image))
        });
        cx.spawn(async move |cx| {
            let (saved, image) = write.await?;
            cx.update(|cx| {
                stack::show_edit(&saved, image, cx);
                store::upsert(saved, cx);
            })
        })
    })
}

pub fn copy_text(item: &Item, cx: &mut App) {
    with_pixels(item, cx, |img, cx| {
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
    });
}

/// Writes a smaller JPEG to a temp folder and copies it as a file.
pub fn compress(item: &Item, cx: &mut App) {
    let quality = cx.global::<Settings>().jpeg_quality.min(80);
    let title = item.meta.title.clone();
    with_pixels(item, cx, move |img, cx| {
        let task = cx.background_spawn(async move {
            let dir = std::env::temp_dir().join("Glint").join("Compressed");
            let path = shell::export(&img, &dir, &title, ExportFormat::Jpeg, quality)?;
            shell::copy_files(std::slice::from_ref(&path), None)?;
            anyhow::Ok(std::fs::metadata(&path)?.len())
        });
        cx.spawn(async move |cx| {
            let result = task.await;
            let _ = cx.update(|cx| match result {
                Ok(bytes) => toast::show(format!("Copied a {} JPEG", human_bytes(bytes)), cx),
                Err(e) => toast::show(format!("Compress failed: {e:#}"), cx),
            });
        })
        .detach();
    });
}

pub fn human_bytes(n: u64) -> String {
    match n {
        n if n >= 1 << 30 => format!("{:.1} GB", n as f64 / (1u64 << 30) as f64),
        n if n >= 1 << 20 => format!("{:.1} MB", n as f64 / (1u64 << 20) as f64),
        n if n >= 1 << 10 => format!("{} KB", n >> 10),
        n => format!("{n} B"),
    }
}

/// Where Save writes. `Folder` bypasses the dialog; `Ask` shows a save dialog.
#[derive(Clone)]
pub enum SaveTarget {
    Folder(PathBuf),
    Ask,
}

/// Saves to the export folder or through a dialog, per `ask` and settings. A failed write
/// shows a dialog offering Retry or another folder; `done` sees the final outcome.
pub fn save(item: &Item, ask: bool, cx: &mut App, done: Done) {
    let title = item.meta.title.clone();
    with_pixels(item, cx, move |img, cx| save_image(title, img, ask, cx, done));
}

/// Saves pixels named after `title`, with the same rules as [`save`].
pub fn save_image(title: String, img: Arc<RgbaImage>, ask: bool, cx: &mut App, done: Done) {
    let s = cx.global::<Settings>();
    let target = if ask || !s.save_without_dialog { SaveTarget::Ask } else { SaveTarget::Folder(s.save_dir.clone()) };
    save_to(title, img, target, cx, done);
}

fn save_to(stem: String, img: Arc<RgbaImage>, target: SaveTarget, cx: &mut App, done: Done) {
    let s = cx.global::<Settings>().clone();
    match target {
        SaveTarget::Ask => {
            let name = format!("{}.{}", shell::sanitize(&stem), s.format.extension());
            let _ = std::fs::create_dir_all(&s.save_dir);
            let rx = cx.prompt_for_new_path(&s.save_dir, Some(&name));
            cx.spawn(async move |cx| {
                let Ok(Ok(Some(path))) = rx.await else {
                    let _ = cx.update(|cx| done(Err(anyhow::anyhow!("cancelled")), cx));
                    return;
                };
                let format = match path.extension().and_then(|e| e.to_str()).map(str::to_ascii_lowercase).as_deref() {
                    Some("jpg" | "jpeg") => ExportFormat::Jpeg,
                    Some("png") => ExportFormat::Png,
                    _ => s.format,
                };
                let pixels = img.clone();
                let write = cx
                    .background_spawn({
                        let path = path.clone();
                        async move {
                            let bytes = shell::encode(&pixels, format, s.jpeg_quality)?;
                            std::fs::write(&path, bytes).with_context(|| format!("writing {}", path.display()))?;
                            anyhow::Ok(path)
                        }
                    })
                    .await;
                let folder = path.parent().map(PathBuf::from).unwrap_or_default();
                let _ = cx.update(|cx| finish_save(stem, img, write, SaveTarget::Folder(folder), cx, done));
            })
            .detach();
        }
        SaveTarget::Folder(dir) => {
            let pixels = img.clone();
            let (task_dir, task_stem) = (dir.clone(), stem.clone());
            let write = cx.background_spawn(async move { shell::export(&pixels, &task_dir, &task_stem, s.format, s.jpeg_quality) });
            cx.spawn(async move |cx| {
                let result = write.await;
                let _ = cx.update(|cx| finish_save(stem, img, result, SaveTarget::Folder(dir), cx, done));
            })
            .detach();
        }
    }
}

fn finish_save(stem: String, img: Arc<RgbaImage>, result: anyhow::Result<PathBuf>, retry: SaveTarget, cx: &mut App, done: Done) {
    let err = match result {
        Ok(path) => return done(Ok(path), cx),
        Err(e) => e,
    };
    let detail = format!("{err:#}");
    let ask = cx.background_spawn(async move { shell::save_failed_dialog(&detail) });
    cx.spawn(async move |cx| {
        let choice = ask.await;
        let _ = cx.update(|cx| match choice {
            SaveChoice::Retry => save_to(stem, img, retry, cx, done),
            SaveChoice::ChooseFolder => {
                let rx = cx.prompt_for_paths(PathPromptOptions {
                    files: false,
                    directories: true,
                    multiple: false,
                    prompt: Some("Save Here".into()),
                });
                cx.spawn(async move |cx| {
                    let picked = match rx.await {
                        Ok(Ok(Some(paths))) => paths.into_iter().next(),
                        _ => None,
                    };
                    let _ = cx.update(|cx| match picked {
                        Some(dir) => save_to(stem, img, SaveTarget::Folder(dir), cx, done),
                        None => done(Err(err), cx),
                    });
                })
                .detach();
            }
            SaveChoice::Cancel => done(Err(err), cx),
        });
    })
    .detach();
}

pub fn report_save(result: &anyhow::Result<PathBuf>, cx: &mut App) {
    match result {
        Ok(path) => toast::show(format!("Saved {}", path.file_name().unwrap_or_default().to_string_lossy()), cx),
        Err(e) if e.to_string() == "cancelled" => {}
        Err(e) => toast::show(format!("Not saved: {e:#}"), cx),
    }
}

/// Exports several captures into one folder in the configured format.
pub fn export_to_folder(items: Vec<Item>, cx: &mut App) {
    let s = cx.global::<Settings>().clone();
    let rx = cx.prompt_for_paths(PathPromptOptions {
        files: false,
        directories: true,
        multiple: false,
        prompt: Some("Export Here".into()),
    });
    cx.spawn(async move |cx| {
        let Ok(Ok(Some(paths))) = rx.await else { return };
        let Some(dir) = paths.into_iter().next() else { return };
        let task = cx.background_spawn(async move {
            let mut n = 0;
            for item in items.iter().filter(|i| i.kind() == CaptureKind::Screenshot) {
                let img = load_pixels(item)?;
                shell::export(&img, &dir, &item.meta.title, s.format, s.jpeg_quality)?;
                n += 1;
            }
            anyhow::Ok(n)
        });
        let result = task.await;
        let _ = cx.update(|cx| match result {
            Ok(n) => toast::show(format!("Exported {n} capture(s)"), cx),
            Err(e) => toast::show(format!("Export failed: {e:#}"), cx),
        });
    })
    .detach();
}

/// A copy of the deliverable named after the title, for dragging into other apps.
/// Hard-linked when possible so it is the Library file itself, not a duplicate.
pub fn drag_file(item: &Item) -> anyhow::Result<PathBuf> {
    let dir = crate::settings::data_dir().join("Drag").join(item.id());
    std::fs::create_dir_all(&dir)?;
    let ext = item.deliverable().extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_default();
    let path = dir.join(format!("{}.{ext}", shell::sanitize(&item.meta.title)));
    let _ = std::fs::remove_file(&path);
    if std::fs::hard_link(item.deliverable(), &path).is_err() {
        std::fs::copy(item.deliverable(), &path)?;
    }
    Ok(path)
}

pub fn delete(dirs: Vec<PathBuf>, cx: &mut App, done: impl FnOnce(bool, &mut App) + 'static) {
    let n = dirs.len();
    store::recycle(dirs, cx, move |result, cx| match result {
        Ok(()) => {
            toast::show(if n == 1 { "Moved to the Recycle Bin".to_string() } else { format!("Moved {n} captures to the Recycle Bin") }, cx);
            done(true, cx);
        }
        Err(e) => {
            toast::show(format!("Delete failed: {e:#}"), cx);
            done(false, cx);
        }
    });
}
