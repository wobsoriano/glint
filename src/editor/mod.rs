mod canvas;
mod gallery;
mod input;
mod inspector;
mod viewport;

use crate::annotate::{Annotation, Shape, TextStyle, Tool, R, REDACT_STRENGTH};
use crate::settings::{RedactStyle, Settings};
use crate::style::{Background, Chrome, Color, Style, INK};
use crate::{actions, ocr, pin, redact, win};
use glint_render::compose::{self, Compositor, Frame};
use glint_render::doc::EditDoc;
use glint_render::fonts::TextFont;
use glint_render::preset::{self, Preset};
use gpui::prelude::*;
use gpui::{
    px, size, App, Bounds, Context, FocusHandle, Pixels, PromptLevel, SharedString, Task, Timer, TitlebarOptions,
    WeakEntity, Window, WindowBounds, WindowOptions,
};
use std::cell::Cell;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::{Duration, Instant};
use tiny_skia::Pixmap;

static NEXT_ID: AtomicU64 = AtomicU64::new(1);

fn next_id() -> u64 {
    NEXT_ID.fetch_add(1, Ordering::Relaxed)
}

pub struct SaveRequest {
    pub image: Arc<image::RgbaImage>,
    pub doc: EditDoc,
}

pub type OnSave = Rc<dyn Fn(SaveRequest, &mut App) -> Task<anyhow::Result<()>>>;

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

pub fn redaction_doc(findings: &[redact::Finding], kind: RedactStyle, dpi: f32) -> EditDoc {
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
    let mut doc = EditDoc::new(style, dpi, None);
    doc.annotations = redactions_from(findings, kind);
    doc
}

pub fn open_doc(base: Arc<image::RgbaImage>, doc: EditDoc, title: String, on_save: OnSave, cx: &mut App) {
    let bounds = Bounds::centered(None, size(px(1360.), px(880.)), cx);
    let _ = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions {
                title: Some(format!("Glint — {title}").into()),
                appears_transparent: true,
                traffic_light_position: None,
            }),
            window_min_size: Some(size(px(1040.), px(660.))),
            window_background: gpui::WindowBackgroundAppearance::Transparent,
            focus: true,
            ..Default::default()
        },
        move |window, cx| {
            let view = cx.new(|cx| Editor::new(base, doc, title, on_save, cx));
            let weak = view.downgrade();
            window.on_window_should_close(cx, move |window, cx| {
                let dirty = weak.update(cx, |e, _| e.is_dirty()).unwrap_or(false);
                if dirty {
                    let weak = weak.clone();
                    window.defer(cx, move |window, cx| {
                        let _ = weak.update(cx, |e, cx| e.request_close(window, cx));
                    });
                }
                !dirty
            });
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            crate::shell::mica(window);
            win::bring_to_front(window);
            view
        },
    );
}

#[derive(Clone, Copy, PartialEq)]
enum AfterSave {
    Close,
    Export { ask: bool },
}

#[derive(Clone)]
struct Ink {
    color: Color,
    stroke: f32,
    text_size: f32,
    text_style: TextStyle,
    font: TextFont,
    redact_strength: f32,
}

#[derive(Clone, Copy, PartialEq)]
enum Typing {
    Text(u64),
    Watermark,
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
    saved: EditDoc,
    on_save: OnSave,
    tool: Tool,
    ink: Ink,
    selected: Vec<u64>,
    typing: Option<Typing>,
    history: History,
    drag: Option<Drag>,
    crop: Option<CropEdit>,
    viewport: viewport::Viewport,
    font_menu: bool,
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
        let saved = doc.clone();
        gallery::scan();
        let presets = preset::load_store(&presets_path()).unwrap_or_default();
        let auto_colors = crate::render::auto_gradient(&base);
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
            saved,
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
            font_menu: false,
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

    fn checkpoint(&mut self) {
        self.history.undo.push(self.doc.clone());
        if self.history.undo.len() > 200 {
            self.history.undo.remove(0);
        }
        self.history.redo.clear();
        self.history.last = None;
    }

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

    fn copy(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        match win::copy_image(&img) {
            Ok(()) => self.show_toast(format!("Copied {} × {} image", img.width(), img.height()), cx),
            Err(e) => self.show_toast(format!("Copy failed: {e}"), cx),
        }
    }

    fn is_dirty(&self) -> bool {
        self.doc != self.saved
    }

    fn commit(&mut self, then: AfterSave, window: &mut Window, cx: &mut Context<Self>) {
        if self.crop.is_some() {
            self.apply_crop();
        }
        let image = Arc::new(self.export());
        let doc = self.doc.clone();
        let task = (self.on_save)(SaveRequest { image: image.clone(), doc: doc.clone() }, cx);
        let handle = window.window_handle();
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| match result {
                Err(e) => this.show_toast(format!("Couldn't save the edit: {e:#}"), cx),
                Ok(()) => {
                    this.saved = doc;
                    match then {
                        AfterSave::Close => {
                            cx.defer(move |cx| {
                                let _ = handle.update(cx, |_, window, _| window.remove_window());
                            });
                        }
                        AfterSave::Export { ask } => this.export_copy(image, ask, cx),
                    }
                }
            });
        })
        .detach();
    }

    fn export_copy(&mut self, image: Arc<image::RgbaImage>, ask: bool, cx: &mut Context<Self>) {
        let this = cx.entity().downgrade();
        actions::save_image(
            self.title.clone(),
            image,
            ask,
            cx,
            Box::new(move |result, cx| {
                let msg = match result {
                    Ok(path) => format!("Saved to {}", path.display()),
                    Err(e) if e.to_string() == "cancelled" => return,
                    Err(e) => format!("Not saved: {e:#}"),
                };
                let _ = this.update(cx, |this, cx| this.show_toast(msg, cx));
            }),
        );
    }

    fn request_close(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        self.commit_text();
        if !self.is_dirty() {
            window.remove_window();
            return;
        }
        let answer = window.prompt(
            PromptLevel::Warning,
            "Save your changes to this screenshot?",
            Some("Your edits are lost if you don't save them."),
            &["Save", "Discard", "Cancel"],
            cx,
        );
        let handle = window.window_handle();
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let Ok(choice) = answer.await else { return };
            let _ = handle.update(cx, |_, window, cx| match choice {
                0 => {
                    let _ = this.update(cx, |this, cx| this.commit(AfterSave::Close, window, cx));
                }
                1 => window.remove_window(),
                _ => {}
            });
        })
        .detach();
    }

    fn pin(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        pin::open(Arc::new(img), self.doc.dpi, cx);
    }

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
