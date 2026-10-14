//! The screenshot editor: framing/background customization and annotations.

use crate::annotate::{pt, Annotation, Pt, Shape, TextStyle, Tool, R};
use crate::capture::Capture;
use crate::render::{self, Inputs, Layout};
use crate::settings::{RedactStyle, Settings};
use crate::style::{Background, Chrome, Color, Gradient, Style, ASPECTS, GRADIENTS, INK, SOLIDS};
use crate::ui::{self, OnValue, ACCENT, BG, BORDER, ELEV, FONT, HOVER, MUTED, PANEL, TEXT};
use crate::{ocr, pin, redact, win};
use gpui::prelude::*;
use gpui::{
    canvas, div, img, px, rgb, rgba, size, App, Bounds, ClickEvent, Context, CursorStyle,
    DispatchPhase, ElementId, FocusHandle, ImageSource, KeyDownEvent, Modifiers, MouseButton,
    MouseDownEvent, MouseMoveEvent, MouseUpEvent, ObjectFit, PathPromptOptions, Pixels, Point,
    RenderImage, SharedString, Timer, TitlebarOptions, WeakEntity, Window, WindowBounds,
    WindowControlArea, WindowOptions,
};
use std::cell::Cell;
use std::path::PathBuf;
use std::rc::Rc;
use std::sync::atomic::{AtomicU64, Ordering};
use std::sync::Arc;
use std::time::Duration;
use tiny_skia::Pixmap;

static NEXT_ID: AtomicU64 = AtomicU64::new(1);

fn next_id() -> u64 {
    NEXT_ID.fetch_add(1, Ordering::Relaxed)
}

pub fn redactions_from(findings: &[redact::Finding], kind: RedactStyle) -> Vec<Annotation> {
    findings
        .iter()
        .map(|f| Annotation {
            id: next_id(),
            shape: Shape::Redact { rect: f.rect, kind },
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
    let full = R {
        x: 0.0,
        y: 0.0,
        w: cap.image.width() as f32,
        h: cap.image.height() as f32,
    };
    let l = render::layout(&style, full, cap.dpi, 1.0);
    let image = render::pixmap_from_rgba(&cap.image);
    let inputs = Inputs {
        style: &style,
        layout: l,
        image: &image,
        blur_bg: None,
        bg_image: None,
        title: None,
    };
    render::rgba_from_pixmap(&render::render_base(&inputs, annotations))
}

pub fn open(cap: Capture, cx: &mut App) {
    open_with(cap, Vec::new(), cx);
}

pub fn open_with(cap: Capture, annotations: Vec<Annotation>, cx: &mut App) {
    let bounds = Bounds::centered(None, size(px(1320.), px(840.)), cx);
    let title = format!("PrettyShot — {} × {}", cap.image.width(), cap.image.height());
    let _ = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions {
                title: Some(title.into()),
                appears_transparent: true,
                traffic_light_position: None,
            }),
            window_min_size: Some(size(px(960.), px(620.))),
            focus: true,
            ..Default::default()
        },
        move |window, cx| {
            let view = cx.new(|cx| Editor::new(cap, annotations, cx));
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            win::bring_to_front(window);
            view
        },
    );
}

#[derive(Clone, PartialEq)]
struct Snapshot {
    annotations: Vec<Annotation>,
    crop: R,
}

enum Drag {
    Create { id: u64, start: Pt },
    Move { id: u64, last: Pt },
    Handle { id: u64, idx: usize },
    Crop { start: Pt },
}

pub struct Editor {
    cap: Capture,
    style: Style,
    annotations: Vec<Annotation>,
    crop: R,
    tool: Tool,
    color: Color,
    stroke: f32,
    text_size: f32,
    text_style: TextStyle,
    fill: bool,
    redact_kind: RedactStyle,
    selected: Option<u64>,
    editing: Option<u64>,
    undo: Vec<Snapshot>,
    redo: Vec<Snapshot>,
    drag: Option<Drag>,
    focus: FocusHandle,
    auto_colors: Vec<Color>,
    hue: f32,

    // Render caches.
    prepared: Option<((u32, u32, u32, u32, u32, u32), Pixmap)>,
    blur_bg: Option<Pixmap>,
    bg_image: Option<(PathBuf, Option<Pixmap>)>,
    base: Option<(String, Pixmap)>,
    preview: Option<Arc<RenderImage>>,
    preview_key: String,
    layout: Option<Layout>,
    area: Rc<Cell<Bounds<Pixels>>>,
    img_bounds: Rc<Cell<Bounds<Pixels>>>,

    toast: Option<SharedString>,
    toast_gen: u64,
    busy: Option<SharedString>,
}

impl Editor {
    fn new(cap: Capture, annotations: Vec<Annotation>, cx: &mut Context<Self>) -> Self {
        let s = cx.global::<Settings>();
        let crop = R {
            x: 0.0,
            y: 0.0,
            w: cap.image.width() as f32,
            h: cap.image.height() as f32,
        };
        let auto_colors = render::auto_gradient(&cap.image);
        Self {
            style: s.style.clone(),
            redact_kind: s.redact_style,
            annotations,
            crop,
            tool: Tool::Arrow,
            color: Color::rgb(INK[0]),
            stroke: 5.0,
            text_size: 28.0,
            text_style: TextStyle::Outline,
            fill: false,
            selected: None,
            editing: None,
            undo: Vec::new(),
            redo: Vec::new(),
            drag: None,
            focus: cx.focus_handle(),
            auto_colors,
            hue: 220.0,
            prepared: None,
            blur_bg: None,
            bg_image: None,
            base: None,
            preview: None,
            preview_key: String::new(),
            layout: None,
            area: Rc::default(),
            img_bounds: Rc::default(),
            toast: None,
            toast_gen: 0,
            busy: None,
            cap,
        }
    }

    fn full(&self) -> R {
        R {
            x: 0.0,
            y: 0.0,
            w: self.cap.image.width() as f32,
            h: self.cap.image.height() as f32,
        }
    }

    fn effective_crop(&self) -> R {
        if self.tool == Tool::Crop {
            self.full()
        } else {
            self.crop
        }
    }

    // --- undo -----------------------------------------------------------------------------

    fn snapshot(&self) -> Snapshot {
        Snapshot {
            annotations: self.annotations.clone(),
            crop: self.crop,
        }
    }

    fn checkpoint(&mut self) {
        self.undo.push(self.snapshot());
        if self.undo.len() > 200 {
            self.undo.remove(0);
        }
        self.redo.clear();
    }

    fn drop_noop_checkpoint(&mut self) {
        if self.undo.last() == Some(&self.snapshot()) {
            self.undo.pop();
        }
    }

    fn undo(&mut self) {
        self.commit_text();
        if let Some(s) = self.undo.pop() {
            self.redo.push(self.snapshot());
            self.annotations = s.annotations;
            self.crop = s.crop;
            self.selected = None;
        }
    }

    fn redo(&mut self) {
        if let Some(s) = self.redo.pop() {
            self.undo.push(self.snapshot());
            self.annotations = s.annotations;
            self.crop = s.crop;
            self.selected = None;
        }
    }

    // --- helpers ----------------------------------------------------------------------------

    fn ann_mut(&mut self, id: u64) -> Option<&mut Annotation> {
        self.annotations.iter_mut().find(|a| a.id == id)
    }

    fn selected_ann(&self) -> Option<&Annotation> {
        let id = self.selected?;
        self.annotations.iter().find(|a| a.id == id)
    }

    fn commit_text(&mut self) {
        if let Some(id) = self.editing.take() {
            self.annotations.retain(|a| match &a.shape {
                Shape::Text { text, .. } => a.id != id || !text.trim().is_empty(),
                _ => true,
            });
            if self.selected == Some(id) && self.ann_mut(id).is_none() {
                self.selected = None;
            }
            self.drop_noop_checkpoint();
        }
    }

    /// Applies a property change to the selected annotation (if any) as an undoable step.
    fn modify_selected(&mut self, f: impl FnOnce(&mut Annotation)) {
        if let Some(id) = self.selected {
            self.checkpoint();
            if let Some(a) = self.ann_mut(id) {
                f(a);
            }
            self.drop_noop_checkpoint();
        }
    }

    fn set_tool(&mut self, tool: Tool) {
        self.commit_text();
        self.tool = tool;
        if tool != Tool::Select {
            self.selected = None;
        }
    }

    fn show_toast(&mut self, msg: impl Into<SharedString>, cx: &mut Context<Self>) {
        self.toast = Some(msg.into());
        self.toast_gen += 1;
        let gen = self.toast_gen;
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            Timer::after(Duration::from_millis(2400)).await;
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

    // --- rendering --------------------------------------------------------------------------

    fn ensure_bg_caches(&mut self) {
        if matches!(self.style.background, Background::Blurred) && self.blur_bg.is_none() {
            self.blur_bg = Some(render::make_blur_bg(&self.cap.image));
        }
        if let Background::Image(path) = &self.style.background {
            if self.bg_image.as_ref().map(|b| &b.0) != Some(path) {
                let pm = image::open(path).ok().map(|i| {
                    let i = i.to_rgba8();
                    let i = if i.width().max(i.height()) > 3200 {
                        image::imageops::thumbnail(&i, 3200, 3200 * i.height() / i.width().max(1))
                    } else {
                        i
                    };
                    render::pixmap_from_rgba(&i)
                });
                self.bg_image = Some((path.clone(), pm));
            }
        }
    }

    fn preview_annotations(&self) -> Vec<Annotation> {
        let mut anns = self.annotations.clone();
        if let Some(id) = self.editing {
            if let Some(a) = anns.iter_mut().find(|a| a.id == id) {
                if let Shape::Text { text, .. } = &mut a.shape {
                    text.push('|');
                }
            }
        }
        anns
    }

    fn ensure_preview(&mut self, window: &mut Window, cx: &mut App) {
        let area = self.area.get();
        let sf = window.scale_factor();
        let (aw, ah) = if area.size.width > px(10.) {
            (f32::from(area.size.width), f32::from(area.size.height))
        } else {
            let vp = window.viewport_size();
            (f32::from(vp.width) - 320., f32::from(vp.height) - 90.)
        };
        let avail = ((aw - 56.).max(120.) * sf, (ah - 56.).max(120.) * sf);
        let crop = self.effective_crop();
        let dpi = self.cap.dpi;
        let l1 = render::layout(&self.style, crop, dpi, 1.0);
        let scale = (avail.0 / l1.width as f32)
            .min(avail.1 / l1.height as f32)
            .min(1.0);
        let l = render::layout(&self.style, crop, dpi, scale);
        let anns = self.preview_annotations();
        let key = format!("{:?}|{:?}|{:?}|{:?}", self.style, l, anns, self.cap.title);
        if key == self.preview_key && self.preview.is_some() {
            return;
        }
        self.ensure_bg_caches();
        let pk = (
            crop.x as u32,
            crop.y as u32,
            crop.w as u32,
            crop.h as u32,
            l.image.w as u32,
            l.image.h as u32,
        );
        if self.prepared.as_ref().map(|p| p.0) != Some(pk) {
            self.prepared = Some((pk, render::prepare_image(&self.cap.image, crop, pk.4, pk.5)));
        }
        let redactions: Vec<Annotation> =
            anns.iter().filter(|a| a.is_redaction()).cloned().collect();
        let bkey = format!("{:?}|{:?}|{:?}|{:?}", self.style, l, redactions, self.cap.title);
        if self.base.as_ref().map(|b| &b.0) != Some(&bkey) {
            let inputs = Inputs {
                style: &self.style,
                layout: l,
                image: &self.prepared.as_ref().unwrap().1,
                blur_bg: self.blur_bg.as_ref(),
                bg_image: self.bg_image.as_ref().and_then(|b| b.1.as_ref()),
                title: self.cap.title.as_deref(),
            };
            let base = render::render_base(&inputs, &redactions);
            self.base = Some((bkey, base));
        }
        let mut pm = self.base.as_ref().unwrap().1.clone();
        render::render_annotations(&mut pm, &l, &anns);
        let image = win::render_image_from_pixmap(&pm);
        if let Some(old) = self.preview.replace(image) {
            cx.drop_image(old, Some(window));
        }
        self.layout = Some(l);
        self.preview_key = key;
    }

    fn export(&mut self) -> image::RgbaImage {
        self.commit_text();
        self.ensure_bg_caches();
        let l = render::layout(&self.style, self.crop, self.cap.dpi, 1.0);
        let image = render::prepare_image(&self.cap.image, self.crop, l.image.w as u32, l.image.h as u32);
        let inputs = Inputs {
            style: &self.style,
            layout: l,
            image: &image,
            blur_bg: self.blur_bg.as_ref(),
            bg_image: self.bg_image.as_ref().and_then(|b| b.1.as_ref()),
            title: self.cap.title.as_deref(),
        };
        let mut pm = render::render_base(&inputs, &self.annotations);
        render::render_annotations(&mut pm, &l, &self.annotations);
        render::rgba_from_pixmap(&pm)
    }

    // --- actions ------------------------------------------------------------------------------

    fn copy(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        match win::copy_image(&img) {
            Ok(()) => self.show_toast(format!("Copied {} × {} image", img.width(), img.height()), cx),
            Err(e) => self.show_toast(format!("Copy failed: {e}"), cx),
        }
    }

    fn save(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        let path = cx.global::<Settings>().save_dir.join(win::file_name(&self.cap.created));
        match win::save_png(&img, &path) {
            Ok(()) => self.show_toast(format!("Saved to {}", path.display()), cx),
            Err(e) => self.show_toast(format!("Save failed: {e}"), cx),
        }
    }

    fn save_as(&mut self, cx: &mut Context<Self>) {
        let img = self.export();
        let dir = cx.global::<Settings>().save_dir.clone();
        let _ = std::fs::create_dir_all(&dir);
        let rx = cx.prompt_for_new_path(&dir, Some(&win::file_name(&self.cap.created)));
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let Ok(Ok(Some(mut path))) = rx.await else { return };
            let ext = path
                .extension()
                .map(|e| e.to_string_lossy().to_lowercase())
                .unwrap_or_default();
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
        pin::open(Arc::new(img), self.cap.dpi, cx);
    }

    fn auto_redact(&mut self, cx: &mut Context<Self>) {
        if self.busy.is_some() {
            return;
        }
        let img = self.cap.image.clone();
        let s = cx.global::<Settings>();
        let rules = s.redact_rules.clone();
        let kind = self.redact_kind;
        let pad = 2.0 * self.cap.dpi;
        self.busy = Some("Scanning for sensitive info…".into());
        cx.notify();
        let task = cx.background_spawn(async move {
            ocr::recognize(&img).map(|lines| redact::find(&lines, &rules, pad))
        });
        cx.spawn(async move |this: WeakEntity<Editor>, cx| {
            let result = task.await;
            let _ = this.update(cx, |this, cx| {
                this.busy = None;
                match result {
                    Ok(findings) => {
                        // Skip anything already covered by an existing redaction.
                        let fresh: Vec<_> = findings
                            .into_iter()
                            .filter(|f| {
                                !this.annotations.iter().any(|a| match &a.shape {
                                    Shape::Redact { rect, .. } => rect
                                        .intersect(&f.rect)
                                        .is_some_and(|i| i.area() > 0.6 * f.rect.area()),
                                    _ => false,
                                })
                            })
                            .collect();
                        if fresh.is_empty() {
                            this.show_toast("No sensitive info found", cx);
                            return;
                        }
                        let mut counts: Vec<(&str, usize)> = Vec::new();
                        for f in &fresh {
                            match counts.iter_mut().find(|c| c.0 == f.kind.label()) {
                                Some(c) => c.1 += 1,
                                None => counts.push((f.kind.label(), 1)),
                            }
                        }
                        this.checkpoint();
                        this.annotations.splice(0..0, redactions_from(&fresh, kind));
                        let summary = counts
                            .iter()
                            .map(|(k, n)| format!("{n} {k}{}", if *n > 1 { "s" } else { "" }))
                            .collect::<Vec<_>>()
                            .join(", ");
                        this.show_toast(format!("Hid {summary}"), cx);
                    }
                    Err(e) => this.show_toast(format!("{e}"), cx),
                }
            });
        })
        .detach();
    }

    fn copy_text(&mut self, cx: &mut Context<Self>) {
        if self.busy.is_some() {
            return;
        }
        let c = self.crop;
        let img = image::imageops::crop_imm(
            self.cap.image.as_ref(),
            c.x as u32,
            c.y as u32,
            c.w as u32,
            c.h as u32,
        )
        .to_image();
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
        if let Some(id) = self.selected.take() {
            self.checkpoint();
            self.annotations.retain(|a| a.id != id);
            self.editing = None;
        }
    }

    // --- pointer --------------------------------------------------------------------------------

    fn to_source(&self, pos: Point<Pixels>) -> Option<Pt> {
        let b = self.img_bounds.get();
        let l = self.layout?;
        if b.size.width <= px(0.) {
            return None;
        }
        let k = l.width as f32 / f32::from(b.size.width);
        let cp = pt(
            f32::from(pos.x - b.origin.x) * k,
            f32::from(pos.y - b.origin.y) * k,
        );
        Some(l.to_source(cp))
    }

    /// Hit tolerance in source pixels (~6 logical px).
    fn tolerance(&self) -> f32 {
        let b = self.img_bounds.get();
        match self.layout {
            Some(l) if b.size.width > px(0.) => {
                6.0 * (l.width as f32 / f32::from(b.size.width)) / l.scale
            }
            _ => 6.0,
        }
    }

    fn clamp_to_image(&self, p: Pt) -> Pt {
        let f = self.full();
        pt(p.x.clamp(0.0, f.w), p.y.clamp(0.0, f.h))
    }

    fn on_mouse_down(&mut self, e: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        window.focus(&self.focus);
        let Some(p) = self.to_source(e.position) else { return };
        let tol = self.tolerance();
        let dpi = self.cap.dpi;

        if self.tool == Tool::Crop {
            self.checkpoint();
            self.drag = Some(Drag::Crop {
                start: self.clamp_to_image(p),
            });
            cx.notify();
            return;
        }

        // Resize handles of the current selection.
        if let Some(sel) = self.selected_ann() {
            let id = sel.id;
            if let Some(idx) = sel.handles().iter().position(|h| h.dist(p) <= tol * 1.6) {
                self.commit_text();
                self.checkpoint();
                self.drag = Some(Drag::Handle { id, idx });
                cx.notify();
                return;
            }
        }

        let hit = self
            .annotations
            .iter()
            .rev()
            .find(|a| a.hit(p, tol))
            .map(|a| (a.id, matches!(a.shape, Shape::Text { .. })));

        // Clicking the text being edited keeps editing.
        if let (Some((id, _)), Some(editing)) = (hit, self.editing) {
            if id == editing {
                return;
            }
        }
        self.commit_text();

        if self.tool == Tool::Select || (hit.is_some() && hit.map(|h| h.0) == self.selected) {
            match hit {
                Some((id, is_text)) => {
                    self.selected = Some(id);
                    if is_text && e.click_count >= 2 {
                        self.checkpoint();
                        self.editing = Some(id);
                    } else {
                        self.checkpoint();
                        self.drag = Some(Drag::Move { id, last: p });
                    }
                }
                None => self.selected = None,
            }
            cx.notify();
            return;
        }

        if self.tool == Tool::Text {
            if let Some((id, true)) = hit {
                self.selected = Some(id);
                self.checkpoint();
                self.editing = Some(id);
                cx.notify();
                return;
            }
        }

        let id = next_id();
        let width = self.stroke * dpi;
        let shape = match self.tool {
            Tool::Arrow => Shape::Arrow { from: p, to: p },
            Tool::Line => Shape::Line { from: p, to: p },
            Tool::Rect => Shape::Rect {
                rect: R::from_pts(p, p),
                filled: self.fill,
            },
            Tool::Ellipse => Shape::Ellipse {
                rect: R::from_pts(p, p),
                filled: self.fill,
            },
            Tool::Highlight => Shape::Highlight {
                rect: R::from_pts(p, p),
            },
            Tool::Redact => Shape::Redact {
                rect: R::from_pts(p, p),
                kind: self.redact_kind,
            },
            Tool::Spotlight => Shape::Spotlight {
                rect: R::from_pts(p, p),
            },
            Tool::Pen => Shape::Pen { points: vec![p] },
            Tool::Text => {
                let size = self.text_size * dpi;
                Shape::Text {
                    pos: pt(p.x, p.y - size * 0.62),
                    text: String::new(),
                    size,
                    style: self.text_style,
                }
            }
            Tool::Counter => {
                let n = self
                    .annotations
                    .iter()
                    .filter_map(|a| match a.shape {
                        Shape::Counter { n, .. } => Some(n),
                        _ => None,
                    })
                    .max()
                    .unwrap_or(0)
                    + 1;
                Shape::Counter {
                    center: p,
                    n,
                    size: self.text_size * 0.5 * dpi,
                }
            }
            Tool::Select | Tool::Crop => return,
        };
        self.checkpoint();
        let color = if self.tool == Tool::Highlight && self.color.luma() < 0.5 {
            Color::rgb(0xFFE14D)
        } else {
            self.color
        };
        self.annotations.push(Annotation {
            id,
            shape,
            color,
            width,
        });
        match self.tool {
            Tool::Text => {
                self.selected = Some(id);
                self.editing = Some(id);
            }
            Tool::Counter => self.selected = Some(id),
            _ => self.drag = Some(Drag::Create { id, start: p }),
        }
        cx.notify();
    }

    fn on_drag_move(&mut self, pos: Point<Pixels>, mods: Modifiers, cx: &mut Context<Self>) {
        let Some(p) = self.to_source(pos) else { return };
        let full = self.full();
        match self.drag {
            Some(Drag::Create { id, start }) => {
                let Some(a) = self.ann_mut(id) else { return };
                match &mut a.shape {
                    Shape::Arrow { to, .. } | Shape::Line { to, .. } => {
                        *to = if mods.shift { snap_angle(start, p) } else { p };
                    }
                    Shape::Rect { rect, .. }
                    | Shape::Ellipse { rect, .. }
                    | Shape::Highlight { rect }
                    | Shape::Redact { rect, .. }
                    | Shape::Spotlight { rect } => {
                        let end = if mods.shift { square(start, p) } else { p };
                        *rect = R::from_pts(start, end);
                    }
                    Shape::Pen { points } => {
                        if points.last().is_none_or(|l| l.dist(p) > 1.5) {
                            points.push(p);
                        }
                    }
                    _ => {}
                }
            }
            Some(Drag::Move { id, last }) => {
                let (dx, dy) = (p.x - last.x, p.y - last.y);
                if let Some(a) = self.ann_mut(id) {
                    a.translate(dx, dy);
                }
                self.drag = Some(Drag::Move { id, last: p });
            }
            Some(Drag::Handle { id, idx }) => {
                if let Some(a) = self.ann_mut(id) {
                    a.set_handle(idx, p);
                }
            }
            Some(Drag::Crop { start }) => {
                let end = pt(p.x.clamp(0.0, full.w), p.y.clamp(0.0, full.h));
                let r = R::from_pts(start, end);
                self.crop = R {
                    x: r.x.round(),
                    y: r.y.round(),
                    w: r.w.round(),
                    h: r.h.round(),
                };
            }
            None => return,
        }
        cx.notify();
    }

    fn on_drag_end(&mut self, cx: &mut Context<Self>) {
        match self.drag.take() {
            Some(Drag::Create { id, .. }) => {
                let tiny = self
                    .annotations
                    .iter()
                    .find(|a| a.id == id)
                    .is_some_and(|a| {
                        let b = a.bounds();
                        match a.shape {
                            Shape::Arrow { .. } | Shape::Line { .. } => {
                                b.w.max(b.h) < a.width * 4.0 + 4.0
                            }
                            Shape::Pen { ref points } => points.len() < 2,
                            _ => b.w < 4.0 || b.h < 4.0,
                        }
                    });
                if tiny {
                    self.annotations.retain(|a| a.id != id);
                    self.undo.pop();
                } else {
                    self.selected = Some(id);
                }
            }
            Some(Drag::Crop { .. }) => {
                if self.crop.w < 8.0 || self.crop.h < 8.0 {
                    self.crop = self.full();
                }
                self.drop_noop_checkpoint();
            }
            Some(_) => self.drop_noop_checkpoint(),
            None => return,
        }
        cx.notify();
    }

    fn on_key(&mut self, e: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let k = &e.keystroke;
        let (ctrl, shift) = (k.modifiers.control, k.modifiers.shift);
        let key = k.key.as_str();

        if let Some(id) = self.editing {
            if !ctrl {
                let mut done = false;
                if let Some(Annotation {
                    shape: Shape::Text { text, .. },
                    ..
                }) = self.ann_mut(id)
                {
                    match key {
                        "escape" => done = true,
                        "enter" if !shift => done = true,
                        "enter" => text.push('\n'),
                        "backspace" => {
                            text.pop();
                        }
                        _ => {
                            if let Some(ch) = &k.key_char {
                                text.push_str(ch);
                            }
                        }
                    }
                }
                if done {
                    self.commit_text();
                }
                cx.stop_propagation();
                cx.notify();
                return;
            }
        }

        match (ctrl, shift, key) {
            (true, false, "c") => self.copy(cx),
            (true, false, "s") => self.save(cx),
            (true, true, "s") => self.save_as(cx),
            (true, false, "z") => self.undo(),
            (true, true, "z") | (true, _, "y") => self.redo(),
            (true, false, "p") => self.pin(cx),
            (true, true, "r") => self.auto_redact(cx),
            (true, true, "t") => self.copy_text(cx),
            (true, false, "w") => window.remove_window(),
            (false, _, "delete") | (false, _, "backspace") => self.delete_selected(),
            (false, _, "escape") => {
                if self.tool == Tool::Crop {
                    self.set_tool(Tool::Select);
                } else {
                    self.selected = None;
                }
            }
            (false, _, "enter") if self.tool == Tool::Crop => self.set_tool(Tool::Select),
            (false, _, "left" | "right" | "up" | "down") => {
                let d = if shift { 10.0 } else { 1.0 };
                let (dx, dy) = match key {
                    "left" => (-d, 0.0),
                    "right" => (d, 0.0),
                    "up" => (0.0, -d),
                    _ => (0.0, d),
                };
                self.modify_selected(|a| a.translate(dx, dy));
            }
            (false, false, _) => {
                if let Some(t) = Tool::from_key(key) {
                    self.set_tool(t);
                }
            }
            _ => return,
        }
        cx.notify();
    }
}

fn snap_angle(a: Pt, b: Pt) -> Pt {
    let (dx, dy) = (b.x - a.x, b.y - a.y);
    let len = (dx * dx + dy * dy).sqrt();
    let ang = dy.atan2(dx);
    let step = std::f32::consts::FRAC_PI_4;
    let snapped = (ang / step).round() * step;
    pt(a.x + len * snapped.cos(), a.y + len * snapped.sin())
}

fn square(a: Pt, b: Pt) -> Pt {
    let s = (b.x - a.x).abs().max((b.y - a.y).abs());
    pt(a.x + s * (b.x - a.x).signum(), a.y + s * (b.y - a.y).signum())
}

// ------------------------------------------------------------------------------------------------
// View

type Listener = Box<dyn Fn(&ClickEvent, &mut Window, &mut App) + 'static>;

fn on(weak: &WeakEntity<Editor>, f: impl Fn(&mut Editor, &mut Context<Editor>) + 'static) -> Listener {
    let weak = weak.clone();
    Box::new(move |_, _, cx| {
        let _ = weak.update(cx, |this, cx| {
            f(this, cx);
            cx.notify();
        });
    })
}

fn on_value(weak: &WeakEntity<Editor>, f: impl Fn(&mut Editor, f32) + 'static) -> OnValue {
    let weak = weak.clone();
    Rc::new(move |v, _, cx| {
        let _ = weak.update(cx, |this, cx| {
            f(this, v);
            cx.notify();
        });
    })
}

fn eid(prefix: &str, i: usize) -> ElementId {
    ElementId::Name(format!("{prefix}-{i}").into())
}

#[derive(PartialEq, Clone, Copy)]
enum BgMode {
    Gradient,
    Solid,
    Blur,
    Image,
    None,
}

impl Editor {
    fn titlebar(&self, weak: &WeakEntity<Editor>, window: &Window) -> impl IntoElement {
        let tools = Tool::ALL.iter().enumerate().map(|(i, t)| {
            let t = *t;
            ui::icon_button(eid("tool", i), t.icon(), self.tool == t, t.name(), on(weak, move |e, _| e.set_tool(t)))
        });
        let caption = |id: &'static str, icon: &'static str, area: WindowControlArea, close: bool| {
            div()
                .id(id)
                .w(px(46.))
                .h_full()
                .flex()
                .items_center()
                .justify_center()
                .window_control_area(area)
                .when(close, |d| d.hover(|s| s.bg(rgb(0xC42B1C))))
                .when(!close, |d| d.hover(|s| s.bg(rgb(HOVER))))
                .child(ui::icon(icon, 14., rgb(TEXT)))
        };
        let _ = window;
        div()
            .h(px(48.))
            .flex_none()
            .flex()
            .items_center()
            .bg(rgb(PANEL))
            .border_b_1()
            .border_color(rgb(BORDER))
            .child(
                div()
                    .id("drag-l")
                    .h_full()
                    .w(px(220.))
                    .flex_none()
                    .flex()
                    .items_center()
                    .gap_2()
                    .pl_4()
                    .window_control_area(WindowControlArea::Drag)
                    .child(
                        div()
                            .size(px(22.))
                            .rounded_md()
                            .flex()
                            .items_center()
                            .justify_center()
                            .bg(gpui::linear_gradient(
                                135.,
                                gpui::linear_color_stop(rgb(0x6366F1), 0.),
                                gpui::linear_color_stop(rgb(0xEC4899), 1.),
                            ))
                            .child(ui::icon("area", 13., rgb(0xFFFFFF))),
                    )
                    .child(div().text_sm().font_weight(gpui::FontWeight::SEMIBOLD).child("PrettyShot")),
            )
            .child(div().id("drag-m1").h_full().flex_1().window_control_area(WindowControlArea::Drag))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_0p5()
                    .p_1()
                    .rounded_lg()
                    .bg(rgb(BG))
                    .border_1()
                    .border_color(rgb(BORDER))
                    .children(tools),
            )
            .child(div().id("drag-m2").h_full().flex_1().window_control_area(WindowControlArea::Drag))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_1()
                    .pr_2()
                    .child(ui::icon_button("undo", "undo", false, "Undo (Ctrl+Z)", on(weak, |e, _| e.undo())))
                    .child(ui::icon_button("redo", "redo", false, "Redo (Ctrl+Y)", on(weak, |e, _| e.redo())))
                    .child(div().w(px(1.)).h(px(20.)).mx_1().bg(rgb(BORDER)))
                    .child(ui::icon_button("redact", "shield", false, "Hide sensitive info (Ctrl+Shift+R)", on(weak, |e, cx| e.auto_redact(cx))))
                    .child(ui::icon_button("ocr", "scan", false, "Copy text (Ctrl+Shift+T)", on(weak, |e, cx| e.copy_text(cx))))
                    .child(ui::icon_button("pin", "pin", false, "Pin to screen (Ctrl+P)", on(weak, |e, cx| e.pin(cx))))
                    .child(ui::icon_button("saveas", "folder", false, "Save as… (Ctrl+Shift+S)", on(weak, |e, cx| e.save_as(cx))))
                    .child(div().w(px(6.)))
                    .child(ui::button("copy", "Copy", Some("copy"), false, on(weak, |e, cx| e.copy(cx))))
                    .child(ui::button("save", "Save", Some("save"), true, on(weak, |e, cx| e.save(cx)))),
            )
            .child(
                div()
                    .h_full()
                    .flex()
                    .child(caption("min", "min", WindowControlArea::Min, false))
                    .child(caption("max", "max", WindowControlArea::Max, false))
                    .child(caption("close", "close", WindowControlArea::Close, true)),
            )
    }

    fn sidebar(&self, weak: &WeakEntity<Editor>) -> impl IntoElement {
        let st = &self.style;
        let mode = match st.background {
            Background::Gradient(_) => BgMode::Gradient,
            Background::Solid(_) => BgMode::Solid,
            Background::Blurred => BgMode::Blur,
            Background::Image(_) => BgMode::Image,
            Background::None => BgMode::None,
        };
        let auto = self.auto_colors.clone();

        let mode_chip = |id: &'static str, label: &'static str, m: BgMode| {
            let auto = auto.clone();
            ui::chip(id, label, mode == m, on(weak, move |e, _| {
                e.style.background = match m {
                    BgMode::Gradient => Background::Gradient(GRADIENTS[0].1.clone()),
                    BgMode::Solid => Background::Solid(Color::rgb(0xF4F4F5)),
                    BgMode::Blur => Background::Blurred,
                    BgMode::Image => match win::desktop_wallpaper() {
                        Some(p) => Background::Image(p),
                        None => Background::Gradient(Gradient { stops: auto.clone(), angle: 135.0 }),
                    },
                    BgMode::None => Background::None,
                };
            }))
        };

        let mut bg_section = ui::section("Background").child(
            div()
                .flex()
                .flex_wrap()
                .gap_1()
                .child(mode_chip("bg-g", "Gradient", BgMode::Gradient))
                .child(mode_chip("bg-s", "Solid", BgMode::Solid))
                .child(mode_chip("bg-b", "Blur", BgMode::Blur))
                .child(mode_chip("bg-i", "Image", BgMode::Image))
                .child(mode_chip("bg-n", "None", BgMode::None)),
        );

        match &st.background {
            Background::Gradient(g) => {
                let current = g.clone();
                let mut grid = div().flex().flex_wrap().gap_1();
                let auto_g = Gradient { stops: auto.clone(), angle: current.angle };
                grid = grid.child(
                    div()
                        .relative()
                        .child(ui::swatch(
                            "g-auto",
                            &auto.iter().map(|c| c.to_gpui()).collect::<Vec<_>>(),
                            current.stops == auto,
                            34.,
                            on(weak, move |e, _| e.style.background = Background::Gradient(auto_g.clone())),
                        ))
                        .child(
                            div()
                                .absolute()
                                .top(px(9.))
                                .left(px(9.))
                                .child(ui::icon("sparkles", 16., rgb(0xFFFFFF))),
                        ),
                );
                for (i, (_, preset)) in GRADIENTS.iter().enumerate() {
                    let p = preset.clone();
                    let angle = current.angle;
                    grid = grid.child(ui::swatch(
                        eid("grad", i),
                        &preset.stops.iter().map(|c| c.to_gpui()).collect::<Vec<_>>(),
                        preset.stops == current.stops,
                        34.,
                        on(weak, move |e, _| {
                            e.style.background = Background::Gradient(Gradient { stops: p.stops.clone(), angle })
                        }),
                    ));
                }
                bg_section = bg_section.child(grid).child(ui::slider(
                    "angle",
                    "Angle",
                    current.angle,
                    0.,
                    360.,
                    format!("{:.0}°", current.angle),
                    on_value(weak, |e, v| {
                        if let Background::Gradient(g) = &mut e.style.background {
                            g.angle = (v / 15.0).round() * 15.0;
                        }
                    }),
                ));
            }
            Background::Solid(c) => {
                let current = *c;
                let mut grid = div().flex().flex_wrap().gap_1();
                for (i, hex) in SOLIDS.iter().enumerate() {
                    let col = Color::rgb(*hex);
                    grid = grid.child(ui::swatch(
                        eid("solid", i),
                        &[col.to_gpui()],
                        current == col,
                        30.,
                        on(weak, move |e, _| e.style.background = Background::Solid(col)),
                    ));
                }
                bg_section = bg_section.child(grid).child(ui::slider(
                    "hue",
                    "Custom hue",
                    self.hue,
                    0.,
                    360.,
                    current.hex(),
                    on_value(weak, |e, v| {
                        e.hue = v;
                        e.style.background = Background::Solid(Color::from_hsl(v, 0.7, 0.62));
                    }),
                ));
            }
            Background::Blurred => {
                bg_section = bg_section.child(
                    div()
                        .text_xs()
                        .text_color(rgb(MUTED))
                        .child("A soft, blurred copy of your screenshot — always matches its colors."),
                );
            }
            Background::Image(p) => {
                let name = p.file_name().map(|n| n.to_string_lossy().to_string()).unwrap_or_default();
                let w1 = weak.clone();
                bg_section = bg_section
                    .child(div().text_xs().text_color(rgb(MUTED)).truncate().child(name))
                    .child(
                        div()
                            .flex()
                            .gap_2()
                            .child(ui::button("bg-pick", "Choose image…", Some("image"), false, move |_, _, cx| {
                                let rx = cx.prompt_for_paths(PathPromptOptions {
                                    files: true,
                                    directories: false,
                                    multiple: false,
                                    prompt: Some("Choose background".into()),
                                });
                                let w = w1.clone();
                                cx.spawn(async move |cx| {
                                    if let Ok(Ok(Some(paths))) = rx.await {
                                        if let Some(p) = paths.into_iter().next() {
                                            let _ = w.update(cx, |e, cx| {
                                                e.style.background = Background::Image(p);
                                                cx.notify();
                                            });
                                        }
                                    }
                                })
                                .detach();
                            }))
                            .child(ui::button("bg-wall", "Wallpaper", Some("monitor"), false, on(weak, |e, cx| {
                                match win::desktop_wallpaper() {
                                    Some(p) => e.style.background = Background::Image(p),
                                    None => e.show_toast("Couldn't find your desktop wallpaper", cx),
                                }
                            }))),
                    );
            }
            Background::None => {
                bg_section = bg_section.child(
                    div()
                        .text_xs()
                        .text_color(rgb(MUTED))
                        .child("Transparent background — great for docs and slides."),
                );
            }
        }

        let frame = ui::section("Frame")
            .child(ui::slider("pad", "Padding", st.padding, 0., 240., format!("{:.0}", st.padding), on_value(weak, |e, v| e.style.padding = v.round())))
            .child(ui::slider("inset", "Inset", st.inset, 0., 120., format!("{:.0}", st.inset), on_value(weak, |e, v| e.style.inset = v.round())))
            .child(ui::slider("radius", "Corner radius", st.radius, 0., 48., format!("{:.0}", st.radius), on_value(weak, |e, v| e.style.radius = v.round())))
            .child(ui::slider("shadow", "Shadow", st.shadow, 0., 120., format!("{:.0}", st.shadow), on_value(weak, |e, v| e.style.shadow = v.round())))
            .child(ui::slider(
                "shadow-o",
                "Shadow opacity",
                st.shadow_opacity,
                0.,
                1.,
                format!("{:.0}%", st.shadow_opacity * 100.),
                on_value(weak, |e, v| e.style.shadow_opacity = v),
            ))
            .child(ui::toggle("balance", "Auto-balance inset color", st.auto_balance, on(weak, |e, _| e.style.auto_balance = !e.style.auto_balance)));

        let chrome = ui::section("Window frame")
            .child(div().flex().flex_wrap().gap_1().children(Chrome::ALL.iter().enumerate().map(|(i, c)| {
                let c = *c;
                ui::chip(eid("chrome", i), c.label(), st.chrome == c, on(weak, move |e, _| e.style.chrome = c))
            })))
            .when(st.chrome != Chrome::None, |d| {
                d.child(ui::toggle("ctitle", "Show window title", st.chrome_title, on(weak, |e, _| e.style.chrome_title = !e.style.chrome_title)))
            });

        let canvas_section = ui::section("Canvas")
            .child(div().flex().flex_wrap().gap_1().children(ASPECTS.iter().enumerate().map(|(i, (label, a))| {
                let a = *a;
                ui::chip(eid("aspect", i), *label, st.aspect == a, on(weak, move |e, _| e.style.aspect = a))
            })))
            .when(st.aspect.is_some(), |d| {
                d.child(
                    div()
                        .flex()
                        .items_center()
                        .gap_3()
                        .child(div().text_xs().text_color(rgb(TEXT)).child("Position"))
                        .child(div().grid().grid_cols(3).gap(px(3.)).children((0..9u8).map(|i| {
                            let selected = st.align == i;
                            div()
                                .id(eid("align", i as usize))
                                .size(px(16.))
                                .rounded_sm()
                                .cursor_pointer()
                                .border_1()
                                .border_color(rgb(BORDER))
                                .when(selected, |d| d.bg(rgb(ACCENT)).border_color(rgb(ACCENT)))
                                .when(!selected, |d| d.bg(rgb(ELEV)).hover(|s| s.bg(rgb(HOVER))))
                                .on_click(on(weak, move |e, _| e.style.align = i))
                        }))),
                )
            });

        let sel_kind = self.selected_ann().map(|a| std::mem::discriminant(&a.shape));
        let ink = ui::section("Annotations")
            .child(div().flex().flex_wrap().gap_1().children(INK.iter().enumerate().map(|(i, hex)| {
                let col = Color::rgb(*hex);
                ui::swatch(eid("ink", i), &[col.to_gpui()], self.color == col, 26., on(weak, move |e, _| {
                    e.color = col;
                    e.modify_selected(|a| {
                        if !a.is_redaction() {
                            a.color = col
                        }
                    });
                }))
            })))
            .child(ui::slider("stroke", "Stroke width", self.stroke, 1., 24., format!("{:.0}", self.stroke), on_value(weak, |e, v| {
                e.stroke = v.round();
                let w = e.stroke * e.cap.dpi;
                if let Some(id) = e.selected {
                    if let Some(a) = e.ann_mut(id) {
                        a.width = w;
                    }
                }
            })))
            .child(ui::slider("tsize", "Text & counter size", self.text_size, 12., 120., format!("{:.0}", self.text_size), on_value(weak, |e, v| {
                e.text_size = v.round();
                let s = e.text_size * e.cap.dpi;
                if let Some(id) = e.selected {
                    if let Some(a) = e.ann_mut(id) {
                        match &mut a.shape {
                            Shape::Text { size, .. } => *size = s,
                            Shape::Counter { size, .. } => *size = s * 0.5,
                            _ => {}
                        }
                    }
                }
            })))
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_1()
                    .child(div().text_xs().w(px(72.)).child("Text style"))
                    .children([(TextStyle::Plain, "Plain"), (TextStyle::Outline, "Outline"), (TextStyle::Pill, "Label")].map(|(ts, label)| {
                        ui::chip(SharedString::from(format!("ts-{label}")), label, self.text_style == ts, on(weak, move |e, _| {
                            e.text_style = ts;
                            e.modify_selected(|a| {
                                if let Shape::Text { style, .. } = &mut a.shape {
                                    *style = ts;
                                }
                            });
                        }))
                    })),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_1()
                    .child(div().text_xs().w(px(72.)).child("Redaction"))
                    .children(RedactStyle::ALL.map(|k| {
                        ui::chip(SharedString::from(format!("rk-{}", k.label())), k.label(), self.redact_kind == k, on(weak, move |e, _| {
                            e.redact_kind = k;
                            e.modify_selected(|a| {
                                if let Shape::Redact { kind, .. } = &mut a.shape {
                                    *kind = k;
                                }
                            });
                        }))
                    })),
            )
            .child(ui::toggle("fill", "Filled shapes", self.fill, on(weak, |e, _| {
                e.fill = !e.fill;
                let f = e.fill;
                e.modify_selected(|a| match &mut a.shape {
                    Shape::Rect { filled, .. } | Shape::Ellipse { filled, .. } => *filled = f,
                    _ => {}
                });
            })))
            .when(sel_kind.is_some(), |d| {
                d.child(ui::button("del", "Delete selected", Some("trash"), false, on(weak, |e, _| e.delete_selected())))
            });

        let privacy = ui::section("Privacy")
            .child(ui::button("p-redact", "Hide sensitive info", Some("shield"), false, on(weak, |e, cx| e.auto_redact(cx))).w_full())
            .child(ui::button("p-ocr", "Copy text from image", Some("scan"), false, on(weak, |e, cx| e.copy_text(cx))).w_full())
            .child(
                div()
                    .text_xs()
                    .text_color(rgb(MUTED))
                    .child("Detects emails, phone numbers, card numbers, API keys & tokens, IPs and more using on-device OCR."),
            );

        let footer = div()
            .flex()
            .gap_2()
            .child(ui::button("def", "Save as default", Some("check"), false, on(weak, |e, cx| {
                let style = e.style.clone();
                cx.update_global::<Settings, _>(|s, _| {
                    s.style = style;
                    s.save();
                });
                e.show_toast("Saved as your default style", cx);
            })))
            .child(ui::button("reset", "Reset", None, false, on(weak, |e, _| e.style = Style::default())));

        div()
            .id("sidebar")
            .w(px(312.))
            .flex_none()
            .h_full()
            .bg(rgb(PANEL))
            .border_l_1()
            .border_color(rgb(BORDER))
            .overflow_y_scroll()
            .child(
                div()
                    .p_4()
                    .flex()
                    .flex_col()
                    .text_color(rgb(TEXT))
                    .child(bg_section)
                    .child(frame)
                    .child(chrome)
                    .child(canvas_section)
                    .child(ink)
                    .child(privacy)
                    .child(footer),
            )
    }

    fn selection_overlay(&self, sf: f32) -> Option<gpui::Div> {
        let l = self.layout?;
        let to_logical = |p: Pt| {
            let c = l.to_canvas(p);
            (c.x / sf, c.y / sf)
        };
        if self.tool == Tool::Crop {
            // Dim everything outside the crop rect, in image space.
            let c = self.crop;
            let (x0, y0) = to_logical(pt(c.x, c.y));
            let (x1, y1) = to_logical(pt(c.right(), c.bottom()));
            let (ix0, iy0) = (l.image.x / sf, l.image.y / sf);
            let (ix1, iy1) = (l.image.right() / sf, l.image.bottom() / sf);
            let shade = |x: f32, y: f32, w: f32, h: f32| {
                div().absolute().left(px(x)).top(px(y)).w(px(w.max(0.))).h(px(h.max(0.))).bg(rgba(0x0000009E))
            };
            return Some(
                div()
                    .absolute()
                    .inset_0()
                    .child(shade(ix0, iy0, ix1 - ix0, y0 - iy0))
                    .child(shade(ix0, y1, ix1 - ix0, iy1 - y1))
                    .child(shade(ix0, y0, x0 - ix0, y1 - y0))
                    .child(shade(x1, y0, ix1 - x1, y1 - y0))
                    .child(
                        div()
                            .absolute()
                            .left(px(x0))
                            .top(px(y0))
                            .w(px(x1 - x0))
                            .h(px(y1 - y0))
                            .border_2()
                            .border_color(rgb(0xFFFFFF)),
                    )
                    .child(
                        div()
                            .absolute()
                            .left(px(x0))
                            .top(px((y0 - 26.).max(4.)))
                            .px_2()
                            .py_0p5()
                            .rounded_md()
                            .bg(rgba(0x000000CC))
                            .text_xs()
                            .text_color(rgb(0xFFFFFF))
                            .child(format!("{} × {}  ·  drag to crop, Enter to apply", c.w as i32, c.h as i32)),
                    ),
            );
        }
        let a = self.selected_ann()?;
        let b = a.bounds();
        let (x0, y0) = to_logical(pt(b.x, b.y));
        let (x1, y1) = to_logical(pt(b.right(), b.bottom()));
        let handles = a.handles().into_iter().map(|h| {
            let (hx, hy) = to_logical(h);
            div()
                .absolute()
                .left(px(hx - 5.))
                .top(px(hy - 5.))
                .size(px(10.))
                .rounded_full()
                .bg(rgb(0xFFFFFF))
                .border_2()
                .border_color(rgb(ACCENT))
        });
        Some(
            div()
                .absolute()
                .inset_0()
                .child(
                    div()
                        .absolute()
                        .left(px(x0 - 3.))
                        .top(px(y0 - 3.))
                        .w(px(x1 - x0 + 6.))
                        .h(px(y1 - y0 + 6.))
                        .border_1()
                        .border_color(rgba(0x7C8CFFCC))
                        .rounded_sm(),
                )
                .children(handles),
        )
    }
}

impl Render for Editor {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        self.ensure_preview(window, cx);
        let weak = cx.entity().downgrade();
        let sf = window.scale_factor();
        let l = self.layout.unwrap();
        let (lw, lh) = (l.width as f32 / sf, l.height as f32 / sf);
        let out = render::layout(&self.style, self.crop, self.cap.dpi, 1.0);

        let area_cell = self.area.clone();
        let img_cell = self.img_bounds.clone();
        let cursor = match self.tool {
            Tool::Select => CursorStyle::Arrow,
            Tool::Text => CursorStyle::IBeam,
            _ => CursorStyle::Crosshair,
        };

        let drag_weak = weak.clone();
        let preview = div()
            .id("preview-area")
            .flex_1()
            .h_full()
            .relative()
            .flex()
            .items_center()
            .justify_center()
            .overflow_hidden()
            .bg(rgb(0x0E0E10))
            .cursor(cursor)
            .on_mouse_down(MouseButton::Left, cx.listener(Self::on_mouse_down))
            .child(
                canvas(
                    move |b, window, _| {
                        let old = area_cell.get();
                        if (old.size.width - b.size.width).abs() > px(0.5)
                            || (old.size.height - b.size.height).abs() > px(0.5)
                        {
                            area_cell.set(b);
                            window.on_next_frame(|w, _| w.refresh());
                        }
                    },
                    move |_, _, window, _| {
                        let w1 = drag_weak.clone();
                        window.on_mouse_event(move |e: &MouseMoveEvent, phase, _, cx| {
                            if phase == DispatchPhase::Bubble && e.pressed_button == Some(MouseButton::Left) {
                                let _ = w1.update(cx, |this, cx| {
                                    if this.drag.is_some() {
                                        this.on_drag_move(e.position, e.modifiers, cx);
                                    }
                                });
                            }
                        });
                        let w2 = drag_weak.clone();
                        window.on_mouse_event(move |_: &MouseUpEvent, phase, _, cx| {
                            if phase == DispatchPhase::Bubble {
                                let _ = w2.update(cx, |this, cx| this.on_drag_end(cx));
                            }
                        });
                    },
                )
                .absolute()
                .top_0()
                .left_0()
                .size_full(),
            )
            .child(
                div()
                    .relative()
                    .w(px(lw))
                    .h(px(lh))
                    .flex_none()
                    .when(matches!(self.style.background, Background::None), |d| {
                        d.bg(rgb(0x1A1A1D)).border_1().border_color(rgb(BORDER))
                    })
                    .when_some(self.preview.clone(), |d, image| {
                        d.child(img(ImageSource::Render(image)).size_full().object_fit(ObjectFit::Fill))
                    })
                    .child(canvas(move |b, _, _| img_cell.set(b), |_, _, _, _| {}).absolute().top_0().left_0().size_full())
                    .children(self.selection_overlay(sf)),
            )
            .when_some(self.toast.clone().or(self.busy.clone()), |d, msg| {
                d.child(
                    div().absolute().bottom(px(18.)).left_0().w_full().flex().justify_center().child(
                        div()
                            .flex()
                            .items_center()
                            .gap_2()
                            .px_4()
                            .py_2()
                            .rounded_full()
                            .bg(rgba(0x1C1C1FF2))
                            .border_1()
                            .border_color(rgba(0xFFFFFF1F))
                            .shadow_lg()
                            .text_sm()
                            .text_color(rgb(TEXT))
                            .child(ui::icon(if self.busy.is_some() { "scan" } else { "check" }, 15., rgb(ACCENT)))
                            .child(msg),
                    ),
                )
            })
            .when(self.tool == Tool::Crop, |d| {
                d.child(
                    div()
                        .absolute()
                        .top(px(14.))
                        .right(px(14.))
                        .flex()
                        .gap_2()
                        .child(ui::button("crop-reset", "Reset", None, false, on(&weak, |e, _| {
                            e.checkpoint();
                            e.crop = e.full();
                        })))
                        .child(ui::button("crop-done", "Done", Some("check"), true, on(&weak, |e, _| e.set_tool(Tool::Select)))),
                )
            });

        let status = div()
            .h(px(28.))
            .flex_none()
            .flex()
            .items_center()
            .justify_between()
            .px_4()
            .bg(rgb(PANEL))
            .border_t_1()
            .border_color(rgb(BORDER))
            .text_xs()
            .text_color(rgb(MUTED))
            .child(format!(
                "Capture {} × {}   →   Export {} × {} px   ·   {} annotation{}",
                self.crop.w as u32,
                self.crop.h as u32,
                out.width,
                out.height,
                self.annotations.len(),
                if self.annotations.len() == 1 { "" } else { "s" }
            ))
            .child("Ctrl+C copy  ·  Ctrl+S save  ·  Ctrl+Z undo  ·  Del remove  ·  Shift constrains shapes");

        div()
            .id("editor")
            .size_full()
            .flex()
            .flex_col()
            .bg(rgb(BG))
            .font_family(FONT)
            .text_color(rgb(TEXT))
            .child(self.titlebar(&weak, window))
            // Focus lives below the title bar: a focusable ancestor would swallow the mouse-down
            // on the native min/max/close buttons.
            .child(
                div()
                    .id("editor-body")
                    .track_focus(&self.focus)
                    .on_key_down(cx.listener(Self::on_key))
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .child(preview)
                    .child(self.sidebar(&weak)),
            )
            .child(status)
    }
}
