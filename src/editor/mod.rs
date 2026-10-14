//! The screenshot editor. It edits an [`EditDoc`] over an untouched base image and hands the
//! flattened image plus the document to a save callback, so every edit stays re-editable.

mod canvas;
mod gallery;
mod input;
mod inspector;
mod viewport;

use crate::annotate::{Annotation, Shape, TextStyle, Tool, R, REDACT_STRENGTH};
use crate::capture::Capture;
use crate::settings::{RedactStyle, Settings};
use crate::style::{Background, Chrome, Color, Style, INK};
use crate::{ocr, pin, redact, win};
use anyhow::Context as _;
use glint_render::compose::{self, Compositor, Frame};
use glint_render::doc::EditDoc;
use glint_render::fonts::TextFont;
use glint_render::preset::{self, Preset};
use gpui::prelude::*;
use gpui::{
    px, size, App, Bounds, Context, FocusHandle, Pixels, SharedString, Timer, TitlebarOptions, WeakEntity, Window,
    WindowBounds, WindowOptions,
};
use std::cell::Cell;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tiny_skia::Pixmap;

static NEXT_ID: AtomicU64 = AtomicU64::new(1);

fn next_id() -> u64 {
    NEXT_ID.fetch_add(1, Ordering::Relaxed)
}

/// What a save hands to its owner: the flattened full-resolution image and the document that
/// reproduces it from the base image.
pub struct SaveRequest {
    pub image: image::RgbaImage,
    pub doc: EditDoc,
}

/// Persists a save. Returns the message to show; an error keeps the editor open with its edits.
pub type OnSave = Rc<dyn Fn(&SaveRequest, &mut App) -> anyhow::Result<String>>;

pub fn redactions_from(findings: &[redact::Finding], kind: RedactStyle) -> Vec<Annotation> {
    findings
        .iter()
        .map(|f| Annotation {
            id: next_id(),
            shape: Shape::Redact { rect: f.rect, kind, strength: REDACT_STRENGTH },
            color: Color::BLACK,
            width: 0.0,
        })
        .collect()
}

/// The raw screenshot with only redactions applied (no frame), for quick copy/save.
pub fn flatten_redactions(cap: &Capture, annotations: &[Annotation]) -> image::RgbaImage {
    if annotations.is_empty() {
        return (*cap.image).clone();
    }
    let style = Style {
        background: Background::None,
        padding: 0.0,
        inset: 0.0,
        radius: 0.0,
        shadow: 0.0,
        chrome: Chrome::None,
        aspect: None,
        ..Style::default()
    };
    let mut doc = EditDoc::new(style, cap.dpi, None);
    doc.annotations = annotations.to_vec();
    compose::render_doc(&cap.image, &doc)
}

pub fn open(cap: Capture, cx: &mut App) {
    open_with(cap, Vec::new(), cx);
}

/// Opens a fresh capture. Saving writes a PNG to the save folder.
pub fn open_with(cap: Capture, annotations: Vec<Annotation>, cx: &mut App) {
    let mut doc = EditDoc::new(cx.global::<Settings>().style.clone(), cap.dpi, cap.title.clone());
    doc.annotations = annotations;
    let created = cap.created;
    let title = format!("Screenshot {}", created.format("%Y-%m-%d at %H.%M.%S"));
    let on_save: OnSave = Rc::new(move |req, cx| {
        let path = cx.global::<Settings>().save_dir.join(win::file_name(&created));
        win::save_png(&req.image, &path)?;
        Ok(format!("Saved to {}", path.display()))
    });
    open_doc(cap.image, Some(doc), title, on_save, cx);
}

/// Opens a capture directory laid out like a Library entry (`base.png`, optional `edit.json`).
/// Saving writes `image.png` and `edit.json` next to the base.
pub fn open_dir(dir: PathBuf, cx: &mut App) -> anyhow::Result<()> {
    let base_path = [dir.join("base.png"), dir.join("image.png")]
        .into_iter()
        .find(|p| p.exists())
        .with_context(|| format!("no base.png in {}", dir.display()))?;
    let base = image::open(&base_path).with_context(|| format!("reading {}", base_path.display()))?.to_rgba8();
    let doc = match std::fs::read_to_string(dir.join("edit.json")) {
        Ok(json) => Some(EditDoc::from_json(&json)?),
        Err(_) => None,
    };
    let title = dir.file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
    let on_save: OnSave = Rc::new(move |req, _| {
        write_atomic(&dir.join("edit.json"), req.doc.to_json().as_bytes())?;
        let tmp = dir.join("image.png.tmp");
        win::save_png(&req.image, &tmp)?;
        std::fs::rename(&tmp, dir.join("image.png")).context("replacing image.png")?;
        Ok(format!("Saved {}", dir.display()))
    });
    open_doc(Arc::new(base), doc, title, on_save, cx);
    Ok(())
}

fn write_atomic(path: &Path, bytes: &[u8]) -> anyhow::Result<()> {
    let tmp = path.with_extension("tmp");
    std::fs::write(&tmp, bytes).with_context(|| format!("writing {}", tmp.display()))?;
    std::fs::rename(&tmp, path).with_context(|| format!("replacing {}", path.display()))
}

/// Opens the editor on `base` with `doc` (a new document when `None`).
pub fn open_doc(base: Arc<image::RgbaImage>, doc: Option<EditDoc>, title: String, on_save: OnSave, cx: &mut App) {
    let doc = doc.unwrap_or_else(|| EditDoc::new(cx.global::<Settings>().style.clone(), 1.0, None));
    let bounds = Bounds::centered(None, size(px(1360.), px(880.)), cx);
    let _ = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions {
                title: Some(format!("Glint — {title}").into()),
                appears_transparent: true,
                traffic_light_position: None,
            }),
            window_min_size: Some(size(px(980.), px(640.))),
            focus: true,
            ..Default::default()
        },
        move |window, cx| {
            let view = cx.new(|cx| Editor::new(base, doc, title, on_save, cx));
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            win::bring_to_front(window);
            view
        },
    );
}

/// Drawing defaults for new annotations, in points.
#[derive(Clone)]
struct Ink {
    color: Color,
    stroke: f32,
    text_size: f32,
    text_style: TextStyle,
    font: TextFont,
    redact_strength: f32,
}

/// Where typed characters go.
#[derive(Clone, Copy, PartialEq)]
enum Typing {
    Text(u64),
    Watermark,
}

#[derive(Clone, Copy, PartialEq)]
enum Tab {
    Style,
    Effects,
    Annotate,
}

#[derive(Clone, Copy, PartialEq, Debug)]
enum CropPreset {
    Freeform,
    Original,
    Ratio(f32, f32),
}

const CROP_PRESETS: [(&str, CropPreset); 7] = [
    ("Freeform", CropPreset::Freeform),
    ("Original", CropPreset::Original),
    ("1:1", CropPreset::Ratio(1.0, 1.0)),
    ("16:9", CropPreset::Ratio(16.0, 9.0)),
    ("9:16", CropPreset::Ratio(9.0, 16.0)),
    ("4:3", CropPreset::Ratio(4.0, 3.0)),
    ("3:2", CropPreset::Ratio(3.0, 2.0)),
];

/// A crop being edited, in base-image pixels. Applied as one undoable step.
#[derive(Clone, Copy)]
struct CropEdit {
    rect: R,
    preset: CropPreset,
}

enum Drag {
    Create { id: u64, start: crate::annotate::Pt },
    Move { last: crate::annotate::Pt },
    Handle { id: u64, idx: usize },
    Marquee { start: crate::annotate::Pt, now: crate::annotate::Pt, base: Vec<u64> },
    CropNew { start: crate::annotate::Pt },
    CropMove { last: crate::annotate::Pt },
    CropHandle { idx: usize },
}

/// Undo stack of whole documents. Repeated edits of one control within a moment coalesce.
#[derive(Default)]
struct History {
    undo: Vec<EditDoc>,
    redo: Vec<EditDoc>,
    last: Option<(&'static str, Instant)>,
}

pub struct Editor {
    base: Arc<image::RgbaImage>,
    title: String,
    doc: EditDoc,
    on_save: OnSave,
    tool: Tool,
    ink: Ink,
    selected: Vec<u64>,
    typing: Option<Typing>,
    history: History,
    drag: Option<Drag>,
    crop: Option<CropEdit>,
    viewport: viewport::Viewport,
    tab: Tab,
    focus: FocusHandle,
    auto_colors: Vec<Color>,
    hue: f32,
    wallpapers: Vec<gallery::Wallpaper>,
    presets: Vec<Preset>,

    compositor: Compositor,
    blur_bg: Option<Pixmap>,
    bg_image: Option<(PathBuf, Option<Pixmap>)>,
    preview: Option<canvas::Preview>,
    area: Rc<Cell<Bounds<Pixels>>>,

    toast: Option<SharedString>,
    toast_gen: u64,
    busy: Option<SharedString>,
}

fn presets_path() -> PathBuf {
    crate::settings::data_dir().join("presets.json")
}

impl Editor {
    fn new(base: Arc<image::RgbaImage>, mut doc: EditDoc, title: String, on_save: OnSave, cx: &mut Context<Self>) -> Self {
        for a in &mut doc.annotations {
            a.id = next_id();
        }
        gallery::scan();
        let presets = preset::load_store(&presets_path()).unwrap_or_default();
        let auto_colors = crate::render::auto_gradient(&base);
        // Poll for wallpaper thumbnails while they decode in the background.
        cx.spawn(async move |this: WeakEntity<Editor>, cx| loop {
            Timer::after(Duration::from_millis(400)).await;
            let alive = this.update(cx, |this, cx| {
                if gallery::count() != this.wallpapers.len() {
                    this.wallpapers = gallery::all();
                    cx.notify();
                }
            });
            if alive.is_err() {
                break;
            }
        })
        .detach();
        Self {
            base,
            title,
            doc,
            on_save,
            tool: Tool::Arrow,
            ink: Ink {
                color: Color::rgb(INK[0]),
                stroke: 5.0,
                text_size: 28.0,
                text_style: TextStyle::Outline,
                font: TextFont::default(),
                redact_strength: REDACT_STRENGTH,
            },
            selected: Vec::new(),
            typing: None,
            history: History::default(),
            drag: None,
            crop: None,
            viewport: viewport::Viewport::default(),
            tab: Tab::Style,
            focus: cx.focus_handle(),
            auto_colors,
            hue: 220.0,
            wallpapers: Vec::new(),
            presets,
            compositor: Compositor::default(),
            blur_bg: None,
            bg_image: None,
            preview: None,
            area: Rc::default(),
            toast: None,
            toast_gen: 0,
            busy: None,
        }
    }

    fn base_size(&self) -> (u32, u32) {
        (self.base.width(), self.base.height())
    }

    // --- undo -----------------------------------------------------------------------------

    fn checkpoint(&mut self) {
        self.history.undo.push(self.doc.clone());
        if self.history.undo.len() > 200 {
            self.history.undo.remove(0);
        }
        self.history.redo.clear();
        self.history.last = None;
    }

    /// Checkpoints unless the previous checkpoint came from the same control a moment ago, so a
    /// slider drag is one undo step.
    fn checkpoint_for(&mut self, control: &'static str) {
        let now = Instant::now();
        let recent = self
            .history
            .last
            .is_some_and(|(c, t)| c == control && now.duration_since(t) < Duration::from_millis(900));
        if !recent {
            self.checkpoint();
        }
        self.history.last = Some((control, now));
    }

    fn drop_noop_checkpoint(&mut self) {
        if self.history.undo.last() == Some(&self.doc) {
            self.history.undo.pop();
        }
    }

    /// An undoable document change from a control.
    fn edit(&mut self, control: &'static str, f: impl FnOnce(&mut EditDoc)) {
        self.checkpoint_for(control);
        f(&mut self.doc);
    }

    fn undo(&mut self) {
        self.commit_text();
        if let Some(d) = self.history.undo.pop() {
            self.history.redo.push(std::mem::replace(&mut self.doc, d));
            self.history.last = None;
            self.prune_selection();
        }
    }

    fn redo(&mut self) {
        if let Some(d) = self.history.redo.pop() {
            self.history.undo.push(std::mem::replace(&mut self.doc, d));
            self.history.last = None;
            self.prune_selection();
        }
    }

    fn prune_selection(&mut self) {
        let ids: Vec<u64> = self.doc.annotations.iter().map(|a| a.id).collect();
        self.selected.retain(|id| ids.contains(id));
    }

    // --- helpers ----------------------------------------------------------------------------

    fn ann_mut(&mut self, id: u64) -> Option<&mut Annotation> {
        self.doc.annotations.iter_mut().find(|a| a.id == id)
    }

    fn selected_anns(&self) -> impl Iterator<Item = &Annotation> {
        self.doc.annotations.iter().filter(|a| self.selected.contains(&a.id))
    }

    fn single_selected(&self) -> Option<&Annotation> {
        if self.selected.len() == 1 {
            self.selected_anns().next()
        } else {
            None
        }
    }

    fn commit_text(&mut self) {
        if let Some(Typing::Text(id)) = self.typing.take() {
            self.doc.annotations.retain(|a| match &a.shape {
                Shape::Text { text, .. } => a.id != id || !text.trim().is_empty(),
                _ => true,
            });
            self.prune_selection();
            self.drop_noop_checkpoint();
        }
        if self.typing == Some(Typing::Watermark) {
            self.typing = None;
        }
    }

    /// Applies a change to every selected annotation as one undoable step.
    fn modify_selected(&mut self, control: &'static str, f: impl Fn(&mut Annotation)) {
        if self.selected.is_empty() {
            return;
        }
        self.checkpoint_for(control);
        let sel = self.selected.clone();
        for a in self.doc.annotations.iter_mut().filter(|a| sel.contains(&a.id)) {
            f(a);
        }
        self.drop_noop_checkpoint();
    }

    fn set_tool(&mut self, tool: Tool) {
        self.commit_text();
        if tool == Tool::Crop {
            let (w, h) = self.base_size();
            self.crop = Some(CropEdit { rect: self.doc.crop_rect(w, h), preset: CropPreset::Freeform });
            self.selected.clear();
        } else {
            self.crop = None;
        }
        self.tool = tool;
        if tool != Tool::Select && tool != Tool::Crop {
            self.tab = Tab::Annotate;
        }
    }

    fn apply_crop(&mut self) {
        if let Some(c) = self.crop.take() {
            let (w, h) = self.base_size();
            self.checkpoint();
            self.doc.set_crop(c.rect, w, h);
            self.drop_noop_checkpoint();
        }
        self.tool = Tool::Select;
    }

    fn set_crop_preset(&mut self, preset: CropPreset) {
        let (w, h) = self.base_size();
        let full = R::new(0.0, 0.0, w as f32, h as f32);
        if let Some(c) = &mut self.crop {
            c.preset = preset;
            c.rect = match preset {
                CropPreset::Freeform => c.rect,
                CropPreset::Original => full,
                CropPreset::Ratio(a, b) => glint_render::doc::ratio_crop(full, a / b),
            };
        }
    }

    fn crop_ratio(&self) -> Option<f32> {
        let (w, h) = self.base_size();
        match self.crop?.preset {
            CropPreset::Freeform => None,
            CropPreset::Original => Some(w as f32 / h as f32),
            CropPreset::Ratio(a, b) => Some(a / b),
        }
    }

    fn show_toast(&mut self, msg: impl Into<SharedString>, cx: &mut Context<Self>) {
        self.toast = Some(msg.into());
        self.toast_gen += 1;
        let gen = self.toast_gen;
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            Timer::after(Duration::from_millis(2600)).await;
            let _ = this.update(cx, |this, cx| {
                if this.toast_gen == gen {
                    this.toast = None;
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }

    // --- export -----------------------------------------------------------------------------

    fn ensure_sources(&mut self) {
        if matches!(self.doc.style.background, Background::Blurred) && self.blur_bg.is_none() {
            self.blur_bg = Some(crate::render::make_blur_bg(&self.base));
        }
        if let Background::Image(path) = &self.doc.style.background {
            if self.bg_image.as_ref().map(|b| &b.0) != Some(path) {
                self.bg_image = Some((path.clone(), compose::load_background(path)));
            }
        }
    }

    fn sources(&self) -> compose::Sources<'_> {
        compose::Sources {
            base: &self.base,
            bg_image: match &self.doc.style.background {
                Background::Image(_) => self.bg_image.as_ref().and_then(|b| b.1.as_ref()),
                _ => None,
            },
            blur_bg: self.blur_bg.as_ref(),
        }
    }

    fn export(&mut self) -> image::RgbaImage {
        self.commit_text();
        self.ensure_sources();
        let pm = Compositor::default().render(&self.doc, &self.sources(), 1.0, None);
        crate::render::rgba_from_pixmap(&pm)
    }

    fn export_size(&self) -> (u32, u32) {
        let (w, h) = self.base_size();
        let l = Frame::new(&self.doc, w, h, 1.0).layout;
        (l.width, l.height)
    }

    // --- actions ------------------------------------------------------------------------------

    fn copy(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        match win::copy_image(&img) {
            Ok(()) => self.show_toast(format!("Copied {} × {} image", img.width(), img.height()), cx),
            Err(e) => self.show_toast(format!("Copy failed: {e}"), cx),
        }
    }

    /// Saves through the owner's callback. A failure keeps the editor open with its changes.
    fn save(&mut self, close: bool, window: &mut Window, cx: &mut Context<Self>) {
        if self.crop.is_some() {
            self.apply_crop();
        }
        let req = SaveRequest { image: self.export(), doc: self.doc.clone() };
        let on_save = self.on_save.clone();
        match on_save(&req, cx) {
            Ok(_) if close => window.remove_window(),
            Ok(msg) => self.show_toast(msg, cx),
            Err(e) => self.show_toast(format!("Save failed: {e:#}"), cx),
        }
    }

    fn save_as(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        let dir = cx.global::<Settings>().save_dir.clone();
        let _ = std::fs::create_dir_all(&dir);
        let name = format!("{}.png", self.title.replace(['\\', '/', ':', '*', '?', '"', '<', '>', '|'], "-"));
        let rx = cx.prompt_for_new_path(&dir, Some(&name));
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let Ok(Ok(Some(mut path))) = rx.await else { return };
            let ext = path.extension().map(|e| e.to_string_lossy().to_lowercase()).unwrap_or_default();
            let result = match ext.as_str() {
                "jpg" | "jpeg" => image::DynamicImage::ImageRgba8(img).to_rgb8().save(&path).map_err(anyhow::Error::from),
                "webp" => img.save(&path).map_err(anyhow::Error::from),
                _ => {
                    if ext != "png" {
                        path.set_extension("png");
                    }
                    win::save_png(&img, &path)
                }
            };
            let _ = this.update(cx, |this, cx| match result {
                Ok(()) => this.show_toast(format!("Saved to {}", path.display()), cx),
                Err(e) => this.show_toast(format!("Save failed: {e}"), cx),
            });
        })
        .detach();
    }

    fn pin(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        pin::open(Arc::new(img), self.doc.dpi, cx);
    }

    /// The cropped region of the base image.
    fn cropped_base(&self) -> image::RgbaImage {
        let (w, h) = self.base_size();
        let c = self.doc.crop_rect(w, h);
        image::imageops::crop_imm(self.base.as_ref(), c.x as u32, c.y as u32, c.w as u32, c.h as u32).to_image()
    }

    fn auto_redact(&mut self, cx: &mut Context<Self>) {
        if self.busy.is_some() {
            return;
        }
        let img = self.cropped_base();
        let rules = cx.global::<Settings>().redact_rules.clone();
        let kind = cx.global::<Settings>().redact_style;
        let strength = self.ink.redact_strength * self.doc.dpi;
        let pad = 2.0 * self.doc.dpi;
        self.busy = Some("Scanning for sensitive info…".into());
        cx.notify();
        let task = cx.background_spawn(async move { ocr::recognize(&img).map(|lines| redact::find(&lines, &rules, pad)) });
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.busy = None;
                match result {
                    Ok(findings) => this.add_findings(findings, kind, strength, cx),
                    Err(e) => this.show_toast(format!("{e}"), cx),
                }
            });
        })
        .detach();
    }

    fn add_findings(&mut self, findings: Vec<redact::Finding>, kind: RedactStyle, strength: f32, cx: &mut Context<Self>) {
        // Skip anything already covered by an existing redaction.
        let fresh: Vec<_> = findings
            .into_iter()
            .filter(|f| {
                !self.doc.annotations.iter().any(|a| match &a.shape {
                    Shape::Redact { rect, .. } => rect.intersect(&f.rect).is_some_and(|i| i.area() > 0.6 * f.rect.area()),
                    _ => false,
                })
            })
            .collect();
        if fresh.is_empty() {
            self.show_toast("No sensitive info found", cx);
            return;
        }
        let mut counts: Vec<(&str, usize)> = Vec::new();
        for f in &fresh {
            match counts.iter_mut().find(|c| c.0 == f.kind.label()) {
                Some(c) => c.1 += 1,
                None => counts.push((f.kind.label(), 1)),
            }
        }
        self.checkpoint();
        let mut added = redactions_from(&fresh, kind);
        for a in &mut added {
            if let Shape::Redact { strength: s, .. } = &mut a.shape {
                *s = strength;
            }
        }
        self.selected = added.iter().map(|a| a.id).collect();
        self.doc.annotations.splice(0..0, added);
        self.tool = Tool::Select;
        self.tab = Tab::Annotate;
        let summary = counts
            .iter()
            .map(|(k, n)| format!("{n} {k}{}", if *n > 1 { "s" } else { "" }))
            .collect::<Vec<_>>()
            .join(", ");
        self.show_toast(format!("Hid {summary}; strength stays editable"), cx);
    }

    fn copy_text(&mut self, cx: &mut Context<Self>) {
        if self.busy.is_some() {
            return;
        }
        let img = self.cropped_base();
        self.busy = Some("Recognizing text…".into());
        cx.notify();
        let task = cx.background_spawn(async move { ocr::recognize(&img).map(|l| ocr::to_text(&l)) });
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.busy = None;
                match result {
                    Ok(t) if !t.trim().is_empty() => {
                        let _ = win::copy_text(&t);
                        let n = t.split_whitespace().count();
                        this.show_toast(format!("Copied {n} words of text"), cx);
                    }
                    Ok(_) => this.show_toast("No text found", cx),
                    Err(e) => this.show_toast(format!("{e}"), cx),
                }
            });
        })
        .detach();
    }

    fn delete_selected(&mut self) {
        if self.selected.is_empty() {
            return;
        }
        self.checkpoint();
        let sel = std::mem::take(&mut self.selected);
        self.doc.annotations.retain(|a| !sel.contains(&a.id));
        self.typing = None;
    }

    // --- presets ------------------------------------------------------------------------------

    fn save_presets(&mut self, cx: &mut Context<Self>) {
        if let Err(e) = preset::save_store(&presets_path(), &self.presets) {
            self.show_toast(format!("Couldn't save presets: {e:#}"), cx);
        }
    }

    fn add_preset(&mut self, cx: &mut Context<Self>) {
        let n = (1..).find(|n| !self.presets.iter().any(|p| p.name == format!("Preset {n}"))).unwrap_or(1);
        let name = format!("Preset {n}");
        self.presets.push(Preset::from_doc(&name, &self.doc));
        self.save_presets(cx);
        self.show_toast(format!("Saved “{name}”"), cx);
    }

    fn apply_preset(&mut self, i: usize) {
        if let Some(p) = self.presets.get(i).cloned() {
            self.checkpoint();
            p.apply(&mut self.doc);
            self.drop_noop_checkpoint();
        }
    }

    fn delete_preset(&mut self, i: usize, cx: &mut Context<Self>) {
        if i < self.presets.len() {
            let p = self.presets.remove(i);
            self.save_presets(cx);
            self.show_toast(format!("Deleted “{}”", p.name), cx);
        }
    }

    fn export_presets(&mut self, cx: &mut Context<Self>) {
        if self.presets.is_empty() {
            self.show_toast("Save a preset first", cx);
            return;
        }
        let json = preset::export(&self.presets);
        let dir = dirs::document_dir().unwrap_or_else(|| PathBuf::from("."));
        let rx = cx.prompt_for_new_path(&dir, Some(&format!("Glint presets.{}", preset::EXTENSION)));
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let Ok(Ok(Some(mut path))) = rx.await else { return };
            path.set_extension(preset::EXTENSION);
            let result = std::fs::write(&path, json);
            let _ = this.update(cx, |this, cx| match result {
                Ok(()) => this.show_toast(format!("Exported to {}", path.display()), cx),
                Err(e) => this.show_toast(format!("Export failed: {e}"), cx),
            });
        })
        .detach();
    }

    fn import_presets(&mut self, cx: &mut Context<Self>) {
        let rx = cx.prompt_for_paths(gpui::PathPromptOptions {
            files: true,
            directories: false,
            multiple: false,
            prompt: Some("Import presets".into()),
        });
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let Ok(Ok(Some(paths))) = rx.await else { return };
            let Some(path) = paths.into_iter().next() else { return };
            let result = std::fs::read_to_string(&path).map_err(anyhow::Error::from).and_then(|s| preset::import(&s));
            let _ = this.update(cx, |this, cx| match result {
                Ok(list) => {
                    let n = list.len();
                    for mut p in list {
                        let base = p.name.clone();
                        let mut k = 2;
                        while this.presets.iter().any(|q| q.name == p.name) {
                            p.name = format!("{base} {k}");
                            k += 1;
                        }
                        this.presets.push(p);
                    }
                    this.save_presets(cx);
                    this.show_toast(format!("Imported {n} preset{}", if n == 1 { "" } else { "s" }), cx);
                }
                Err(e) => this.show_toast(format!("Import failed: {e:#}"), cx),
            });
        })
        .detach();
    }
}
