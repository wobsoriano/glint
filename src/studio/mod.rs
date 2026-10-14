//! The Recording Studio window: a large preview, the timeline across the bottom and a
//! settings sidebar on the right. Every edit goes through [`Studio::edit`], which records
//! undo history, hands the project to the player and schedules a save.

mod export;
mod geometry;
mod preview;
mod sidebar;
mod timeline;

use crate::settings;
use crate::ui::{self, FONT};
use crate::{library, toast, win};
use geometry::CropRatio;
use glint_studio::{CropRect, History, Player, Preset, PresetStore, Project, Session};
use gpui::prelude::*;
use gpui::{
    canvas, div, px, rgb, rgba, size, AnyWindowHandle, App, Bounds, Context, DispatchPhase, FocusHandle, Global,
    KeyDownEvent, MouseButton, MouseMoveEvent, MouseUpEvent, Pixels, Point, RenderImage, SharedString, Timer,
    TitlebarOptions, WeakEntity, Window, WindowBounds, WindowOptions,
};
use image::RgbaImage;
use std::cell::Cell;
use std::path::{Path, PathBuf};
use std::rc::Rc;
use std::sync::Arc;
use std::time::{Duration, Instant};

/// Slider and drag edits of the same control this close together are one undo step.
const COALESCE: Duration = Duration::from_millis(1000);
const SAVE_DELAY: Duration = Duration::from_millis(700);
/// After a command the player is polled this long for the frame it renders.
const SETTLE: Duration = Duration::from_millis(1500);
const FRAME_MS: f64 = 1000.0 / 60.0;

#[derive(Default)]
struct OpenStudios(Vec<(PathBuf, AnyWindowHandle)>);

impl Global for OpenStudios {}

/// Opens a recording session in Studio, or brings its window forward if it is open.
pub fn open(cx: &mut App, session_dir: PathBuf) {
    let open = cx.default_global::<OpenStudios>().0.clone();
    for (dir, handle) in open {
        if dir == session_dir && handle.update(cx, |_, window, _| win::bring_to_front(window)).is_ok() {
            return;
        }
    }
    let studio = match Prepared::load(&session_dir) {
        Ok(s) => s,
        Err(e) => return toast::show(format!("Couldn't open the recording: {e:#}"), cx),
    };
    let bounds = Bounds::centered(None, size(px(1440.), px(900.)), cx);
    let title = format!("{} - Glint Studio", studio.title);
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions { title: Some(title.into()), appears_transparent: false, traffic_light_position: None }),
            window_min_size: Some(size(px(1040.), px(680.))),
            focus: true,
            window_background: gpui::WindowBackgroundAppearance::Blurred,
            ..Default::default()
        },
        move |window, cx| {
            crate::shell::mica(window);
            let view = cx.new(|cx| Studio::new(studio, cx));
            let weak = view.downgrade();
            window.on_window_should_close(cx, move |_, cx| weak.update(cx, |s, cx| s.close_requested(cx)).unwrap_or(true));
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            win::bring_to_front(window);
            view
        },
    );
    if let Ok(h) = handle {
        let list = &mut cx.default_global::<OpenStudios>().0;
        list.retain(|(d, _)| *d != session_dir);
        list.push((session_dir, h.into()));
    }
}

/// Everything loaded before the window opens, so a broken session never opens a window.
struct Prepared {
    session: Session,
    player: Player,
    presets: PresetStore,
    title: String,
}

impl Prepared {
    fn load(dir: &Path) -> anyhow::Result<Self> {
        let fresh = !dir.join(Project::FILE).is_file();
        let mut session = Session::open(dir)?;
        let presets = PresetStore::new(settings::data_dir().join("StudioPresets"));
        if fresh {
            if let Some(p) = presets.default_preset() {
                p.apply(&mut session.project);
            }
            session.save_project()?;
        }
        let player = Player::new(&session)?;
        let title = library::load(dir)
            .map(|i| i.meta.title)
            .unwrap_or_else(|_| dir.file_stem().map(|s| s.to_string_lossy().into_owned()).unwrap_or_default());
        Ok(Self { session, player, presets, title })
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Selection {
    Clip(usize),
    Zoom(usize),
}

/// What the preview is being used for. Crop and Focus show the unzoomed recording so the
/// handles drawn over it line up with the footage.
#[derive(Clone, Copy, Debug, PartialEq)]
enum Mode {
    Normal,
    Crop { ratio: CropRatio, before: CropRect },
    Focus { zoom: usize },
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Tab {
    Background,
    Layout,
    Cursor,
    Keystrokes,
    Camera,
    Audio,
    Zoom,
    Presets,
}

/// Sidebar tabs in display order: icon and title. Captions joins here as one more row.
const TABS: [(Tab, &str, &str); 8] = [
    (Tab::Background, "image", "Background"),
    (Tab::Layout, "area", "Layout"),
    (Tab::Cursor, "select", "Cursor"),
    (Tab::Keystrokes, "keyboard", "Keystrokes"),
    (Tab::Camera, "camera", "Camera"),
    (Tab::Audio, "volume", "Audio"),
    (Tab::Zoom, "zoom", "Zoom"),
    (Tab::Presets, "stack", "Presets"),
];

#[derive(Clone, Debug)]
enum Drag {
    Scrub,
    Trim { clip: usize, end: bool, base: Project, from_ms: f64, scale: geometry::TimeScale },
    Zoom { zoom: usize, part: ZoomPart, base: Project, range: std::ops::Range<f64>, from_ms: f64, scale: geometry::TimeScale },
    Camera { grab: (f32, f32) },
    CameraSize,
    Focus { zoom: usize },
    CropFrom { anchor: (f32, f32) },
    CropMove { grab: (f32, f32), base: CropRect },
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum ZoomPart {
    Start,
    End,
    Body,
}

struct Notice {
    text: SharedString,
    file: Option<PathBuf>,
    error: bool,
}

pub struct Studio {
    session: Session,
    player: Player,
    history: History,
    last_group: Option<(&'static str, Instant)>,
    selection: Option<Selection>,
    mode: Mode,
    drag: Option<Drag>,
    tab: Tab,
    focus: FocusHandle,
    title: String,

    frame: Option<(Arc<RgbaImage>, Arc<RenderImage>)>,
    /// Playhead while paused; the player's clock is read while playing.
    playhead: f64,
    preview_area: Rc<Cell<Bounds<Pixels>>>,
    /// The displayed frame in window coordinates.
    frame_bounds: Rc<Cell<Bounds<Pixels>>>,
    poll_until: Instant,
    polling: bool,

    thumbs: Vec<Arc<RenderImage>>,
    timeline_bounds: Rc<Cell<Bounds<Pixels>>>,
    tl_zoom: f64,
    tl_start: f64,
    speed_menu: bool,

    presets: PresetStore,
    preset_list: Vec<Preset>,
    default_preset: Option<String>,
    preset_name: Option<String>,
    hue: f32,

    export: export::ExportUi,
    hevc: bool,

    save_gen: u64,
    dirty: bool,
    save_error: Option<String>,
    notice: Option<Notice>,
    notice_gen: u64,
}

impl Studio {
    fn new(p: Prepared, cx: &mut Context<Self>) -> Self {
        let mut s = Self {
            session: p.session,
            player: p.player,
            history: History::default(),
            last_group: None,
            selection: None,
            mode: Mode::Normal,
            drag: None,
            tab: Tab::Background,
            focus: cx.focus_handle(),
            title: p.title,
            frame: None,
            playhead: 0.0,
            preview_area: Rc::default(),
            frame_bounds: Rc::default(),
            poll_until: Instant::now(),
            polling: false,
            thumbs: Vec::new(),
            timeline_bounds: Rc::default(),
            tl_zoom: 1.0,
            tl_start: 0.0,
            speed_menu: false,
            presets: p.presets,
            preset_list: Vec::new(),
            default_preset: None,
            preset_name: None,
            hue: 210.0,
            export: export::ExportUi::Closed,
            hevc: glint_media::available_codecs().contains(&glint_media::VideoCodec::Hevc),
            save_gen: 0,
            dirty: false,
            save_error: None,
            notice: None,
            notice_gen: 0,
        };
        s.reload_presets();
        s.load_thumbnails(cx);
        s.kick(cx);
        s
    }

    fn project(&self) -> &Project {
        &self.session.project
    }

    fn duration(&self) -> f64 {
        self.project().duration_ms()
    }

    /// A copy of the session for background work: export, thumbnails.
    fn snapshot(&self) -> Session {
        let s = &self.session;
        Session {
            dir: s.dir.clone(),
            project: s.project.clone(),
            assets: s.assets.clone(),
            tracks: s.tracks.clone(),
            transcript: s.transcript.clone(),
            source_size: s.source_size,
            source_duration_ms: s.source_duration_ms,
        }
    }

    // --- edits ---------------------------------------------------------------------------

    /// Applies one edit. Edits in the same `group` (a slider, a drag) within `COALESCE`, or
    /// during one drag, share an undo step.
    fn edit<R>(&mut self, group: Option<&'static str>, cx: &mut Context<Self>, f: impl FnOnce(&mut Project) -> R) -> R {
        let mut next = self.session.project.clone();
        let r = f(&mut next);
        if next == self.session.project {
            return r;
        }
        let coalesce = match (group, self.last_group) {
            (Some(g), Some((last, at))) => g == last && (self.drag.is_some() || at.elapsed() < COALESCE),
            _ => false,
        };
        if !coalesce {
            self.history.record(&self.session.project);
        }
        self.last_group = group.map(|g| (g, Instant::now()));
        self.session.project = next;
        self.project_changed(cx);
        r
    }

    fn undo(&mut self, cx: &mut Context<Self>) {
        if self.history.undo(&mut self.session.project) {
            self.last_group = None;
            self.project_changed(cx);
        }
    }

    fn redo(&mut self, cx: &mut Context<Self>) {
        if self.history.redo(&mut self.session.project) {
            self.last_group = None;
            self.project_changed(cx);
        }
    }

    fn project_changed(&mut self, cx: &mut Context<Self>) {
        let p = &self.session.project;
        self.selection = match self.selection {
            Some(Selection::Clip(i)) if i < p.clips.len() => Some(Selection::Clip(i)),
            Some(Selection::Zoom(i)) if i < p.zooms.len() => Some(Selection::Zoom(i)),
            _ => None,
        };
        if let Mode::Focus { zoom } = self.mode {
            if zoom >= p.zooms.len() {
                self.mode = Mode::Normal;
            }
        }
        self.playhead = self.playhead.min(self.duration());
        self.player.set_project(self.preview_project());
        self.schedule_save(cx);
        self.kick(cx);
        cx.notify();
    }

    /// The project the preview shows for the current mode.
    fn preview_project(&self) -> Project {
        let mut p = self.session.project.clone();
        match self.mode {
            Mode::Normal => {}
            Mode::Crop { .. } => {
                p.crop = CropRect::FULL;
                p.zooms.clear();
                p.aspect = glint_studio::AspectPreset::Original;
                p.aspect_mode = glint_studio::AspectMode::Fit;
            }
            Mode::Focus { .. } => {
                p.zooms.clear();
                p.aspect_mode = glint_studio::AspectMode::Fit;
            }
        }
        p
    }

    fn set_mode(&mut self, mode: Mode, cx: &mut Context<Self>) {
        self.mode = mode;
        self.player.set_project(self.preview_project());
        self.kick(cx);
        cx.notify();
    }

    // --- persistence ---------------------------------------------------------------------

    fn schedule_save(&mut self, cx: &mut Context<Self>) {
        self.dirty = true;
        self.save_gen += 1;
        let gen = self.save_gen;
        cx.spawn(async move |this: WeakEntity<Studio>, cx| {
            Timer::after(SAVE_DELAY).await;
            let _ = this.update(cx, |s, cx| {
                if s.save_gen == gen {
                    s.save_now(cx);
                }
            });
        })
        .detach();
    }

    fn save_now(&mut self, cx: &mut Context<Self>) -> bool {
        let result = self.session.save_project();
        let ok = result.is_ok();
        self.save_error = result.err().map(|e| format!("{e:#}"));
        self.dirty = !ok;
        cx.notify();
        ok
    }

    /// The window closes only once the project is on disk.
    fn close_requested(&mut self, cx: &mut Context<Self>) -> bool {
        if let export::ExportUi::Running { cancel, .. } = &self.export {
            cancel.store(true, std::sync::atomic::Ordering::Relaxed);
        }
        !self.dirty || self.save_now(cx)
    }

    fn discard_and_close(&mut self, window: &mut Window) {
        self.dirty = false;
        window.remove_window();
    }

    // --- playback ------------------------------------------------------------------------

    /// Polls the player at display rate while it plays or a requested frame is due.
    fn kick(&mut self, cx: &mut Context<Self>) {
        self.poll_until = Instant::now() + SETTLE;
        if self.polling {
            return;
        }
        self.polling = true;
        cx.spawn(async move |this: WeakEntity<Studio>, cx| loop {
            Timer::after(Duration::from_millis(8)).await;
            if !this.update(cx, |s, cx| s.poll(cx)).unwrap_or(false) {
                break;
            }
        })
        .detach();
    }

    fn poll(&mut self, cx: &mut Context<Self>) -> bool {
        if let Some((_, img)) = self.player.latest_frame() {
            if self.frame.as_ref().is_none_or(|(a, _)| !Arc::ptr_eq(a, &img)) {
                let render = win::render_image(&img);
                if let Some((_, old)) = self.frame.replace((img, render)) {
                    cx.drop_image(old, None);
                }
                cx.notify();
            }
        }
        let playing = self.player.is_playing();
        if playing {
            self.playhead = self.player.position_ms();
            self.follow_playhead();
            cx.notify();
        }
        let busy = self.export.is_running();
        if busy {
            cx.notify();
        }
        let keep = playing || busy || Instant::now() < self.poll_until;
        if !keep {
            self.polling = false;
        }
        keep
    }

    fn position(&self) -> f64 {
        if self.player.is_playing() {
            self.player.position_ms()
        } else {
            self.playhead
        }
    }

    fn toggle_play(&mut self, cx: &mut Context<Self>) {
        if self.player.is_playing() {
            self.player.pause();
            self.playhead = self.player.position_ms();
        } else {
            if self.mode != Mode::Normal {
                self.set_mode(Mode::Normal, cx);
            }
            self.player.play();
        }
        self.kick(cx);
        cx.notify();
    }

    fn seek(&mut self, ms: f64, cx: &mut Context<Self>) {
        self.playhead = ms.clamp(0.0, self.duration());
        self.player.seek(self.playhead);
        self.kick(cx);
        cx.notify();
    }

    fn step(&mut self, delta_ms: f64, cx: &mut Context<Self>) {
        if self.player.is_playing() {
            self.player.pause();
        }
        let base = self.position();
        self.seek(base + delta_ms, cx);
    }

    // --- timeline commands ---------------------------------------------------------------

    fn split(&mut self, cx: &mut Context<Self>) {
        let t = self.position();
        if let Some(i) = self.edit(None, cx, |p| p.split_at(t)) {
            self.selection = Some(Selection::Clip(i));
        } else {
            self.notify("Too close to a cut to split here", false, cx);
        }
    }

    fn delete_selection(&mut self, cx: &mut Context<Self>) {
        match self.selection {
            Some(Selection::Clip(i)) => {
                if !self.edit(None, cx, |p| p.delete_clip(i)) {
                    self.notify("The last clip can't be deleted", false, cx);
                }
            }
            Some(Selection::Zoom(i)) => {
                if matches!(self.mode, Mode::Focus { .. }) {
                    self.mode = Mode::Normal;
                }
                self.edit(None, cx, |p| p.remove_zoom(i));
            }
            None => return,
        }
        self.selection = None;
        cx.notify();
    }

    fn add_zoom(&mut self, cx: &mut Context<Self>) {
        let t = self.position();
        let end = (t + 2_000.0).min(self.duration());
        let start = (end - 2_000.0).max(0.0).min(t);
        let i = self.edit(None, cx, |p| p.add_zoom(start..end));
        self.selection = Some(Selection::Zoom(i));
        self.tab = Tab::Zoom;
        cx.notify();
    }

    fn set_speed(&mut self, speed: f64, cx: &mut Context<Self>) {
        if let Some(Selection::Clip(i)) = self.selection {
            self.edit(None, cx, |p| p.set_speed(i, speed));
        }
        self.speed_menu = false;
        cx.notify();
    }

    fn select(&mut self, sel: Option<Selection>, cx: &mut Context<Self>) {
        self.selection = sel;
        self.speed_menu = false;
        if matches!(sel, Some(Selection::Zoom(_))) {
            self.tab = Tab::Zoom;
        }
        if let Mode::Focus { zoom } = self.mode {
            if sel != Some(Selection::Zoom(zoom)) {
                self.set_mode(Mode::Normal, cx);
            }
        }
        cx.notify();
    }

    // --- presets -------------------------------------------------------------------------

    fn reload_presets(&mut self) {
        self.preset_list = self.presets.list();
        self.default_preset = self.presets.default_preset().map(|p| p.name);
    }

    fn save_preset(&mut self, cx: &mut Context<Self>) {
        let Some(name) = self.preset_name.take().filter(|n| !n.trim().is_empty()) else { return };
        let preset = Preset::new(&name, self.project().design.clone());
        match self.presets.save(&preset) {
            Ok(()) => self.notify(format!("Saved preset \u{201c}{}\u{201d}", preset.name), false, cx),
            Err(e) => self.notify(format!("Couldn't save the preset: {e:#}"), true, cx),
        }
        self.reload_presets();
    }

    fn preset_result(&mut self, result: anyhow::Result<()>, cx: &mut Context<Self>) {
        if let Err(e) = result {
            self.notify(format!("{e:#}"), true, cx);
        }
        self.reload_presets();
        cx.notify();
    }

    // --- notices -------------------------------------------------------------------------

    fn notify(&mut self, text: impl Into<SharedString>, error: bool, cx: &mut Context<Self>) {
        self.show_notice(Notice { text: text.into(), file: None, error }, cx);
    }

    fn show_notice(&mut self, notice: Notice, cx: &mut Context<Self>) {
        let linger = if notice.file.is_some() { 8_000 } else { 3_000 };
        self.notice = Some(notice);
        self.notice_gen += 1;
        let gen = self.notice_gen;
        cx.spawn(async move |this: WeakEntity<Studio>, cx| {
            Timer::after(Duration::from_millis(linger)).await;
            let _ = this.update(cx, |s, cx| {
                if s.notice_gen == gen {
                    s.notice = None;
                    cx.notify();
                }
            });
        })
        .detach();
        cx.notify();
    }

    // --- input ---------------------------------------------------------------------------

    fn on_key(&mut self, e: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        if let Some(name) = self.preset_name.as_mut() {
            match ui::edit_text(name, e) {
                ui::Edit::Submit => self.save_preset(cx),
                ui::Edit::Cancel => self.preset_name = None,
                _ => {}
            }
            cx.notify();
            return;
        }
        if !matches!(self.export, export::ExportUi::Closed) {
            return self.export_key(e, cx);
        }
        let k = &e.keystroke;
        let (ctrl, shift) = (k.modifiers.control, k.modifiers.shift);
        match k.key.as_str() {
            "z" if ctrl && shift => self.redo(cx),
            "z" if ctrl => self.undo(cx),
            "y" if ctrl => self.redo(cx),
            "e" if ctrl => self.open_export(cx),
            "c" if ctrl => self.run_job(export::Job::Copy, cx),
            "s" if ctrl => self.run_job(export::Job::Save, cx),
            _ if ctrl => {}
            "space" => self.toggle_play(cx),
            "k" => {
                if self.player.is_playing() {
                    self.toggle_play(cx);
                }
            }
            "left" | "j" => self.step(if shift { -1000.0 } else { -FRAME_MS }, cx),
            "right" | "l" => self.step(if shift { 1000.0 } else { FRAME_MS }, cx),
            "home" => self.seek(0.0, cx),
            "end" => self.seek(self.duration(), cx),
            "s" => self.split(cx),
            "z" => self.add_zoom(cx),
            "delete" | "backspace" => self.delete_selection(cx),
            "enter" if matches!(self.mode, Mode::Crop { .. } | Mode::Focus { .. }) => self.set_mode(Mode::Normal, cx),
            "escape" => self.escape(cx),
            _ => {}
        }
    }

    fn escape(&mut self, cx: &mut Context<Self>) {
        match self.mode {
            Mode::Crop { before, .. } => {
                self.edit(Some("crop"), cx, |p| p.crop = before);
                self.set_mode(Mode::Normal, cx);
            }
            Mode::Focus { .. } => self.set_mode(Mode::Normal, cx),
            Mode::Normal if self.speed_menu => self.speed_menu = false,
            Mode::Normal => self.select(None, cx),
        }
        cx.notify();
    }

    fn drag_move(&mut self, pos: Point<Pixels>, cx: &mut Context<Self>) {
        let Some(drag) = self.drag.clone() else { return };
        match drag {
            Drag::Scrub | Drag::Trim { .. } | Drag::Zoom { .. } => self.timeline_drag(drag, pos, cx),
            _ => self.preview_drag(drag, pos, cx),
        }
    }

    fn drag_end(&mut self, cx: &mut Context<Self>) {
        if self.drag.take().is_some() {
            self.last_group = None;
            cx.notify();
        }
    }
}

impl Drop for Studio {
    fn drop(&mut self) {
        if let export::ExportUi::Running { cancel, .. } = &self.export {
            cancel.store(true, std::sync::atomic::Ordering::Relaxed);
        }
    }
}

impl Render for Studio {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let weak = cx.entity().downgrade();
        let undo = self.history.can_undo();
        let redo = self.history.can_redo();
        let header = div()
            .h(px(48.))
            .flex_none()
            .flex()
            .items_center()
            .gap_1()
            .px_3()
            .bg(ui::theme().layer)
            .border_b_1()
            .border_color(ui::theme().border)
            .child(ui::icon("film", 16., ui::theme().muted))
            .child(div().ml_1().text_sm().font_weight(gpui::FontWeight::SEMIBOLD).truncate().child(self.title.clone()))
            .when(self.dirty && self.save_error.is_none(), |d| d.child(div().text_xs().text_color(ui::theme().muted).child("Saving\u{2026}")))
            .child(div().flex_1())
            .child(div().child(ui::icon_button("undo", "undo", false, "Undo (Ctrl+Z)", cx.listener(|s, _, _, cx| s.undo(cx)))).when(!undo, |d| d.opacity(0.4)))
            .child(div().child(ui::icon_button("redo", "redo", false, "Redo (Ctrl+Y)", cx.listener(|s, _, _, cx| s.redo(cx)))).when(!redo, |d| d.opacity(0.4)))
            .child(div().w(px(1.)).h(px(20.)).mx_1().bg(ui::theme().border))
            .child(ui::button("copy", "Copy", Some("copy"), false, cx.listener(|s, _, _, cx| s.run_job(export::Job::Copy, cx))))
            .child(ui::button("save", "Save", Some("save"), false, cx.listener(|s, _, _, cx| s.run_job(export::Job::Save, cx))))
            .child(ui::button("export", "Export\u{2026}", Some("film"), true, cx.listener(|s, _, _, cx| s.open_export(cx))));

        let drag_weak = weak.clone();
        let drag_layer = canvas(
            |_, _, _| {},
            move |_, _, window, _| {
                let w1 = drag_weak.clone();
                window.on_mouse_event(move |e: &MouseMoveEvent, phase, _, cx| {
                    if phase == DispatchPhase::Bubble && e.pressed_button == Some(MouseButton::Left) {
                        let _ = w1.update(cx, |s, cx| s.drag_move(e.position, cx));
                    }
                });
                let w2 = drag_weak.clone();
                window.on_mouse_event(move |_: &MouseUpEvent, phase, _, cx| {
                    if phase == DispatchPhase::Bubble {
                        let _ = w2.update(cx, |s, cx| s.drag_end(cx));
                    }
                });
            },
        )
        .absolute()
        .size_full();

        let save_error = self.save_error.clone().map(|err| {
            div()
                .flex_none()
                .flex()
                .items_center()
                .gap_2()
                .px_3()
                .py_2()
                .bg(rgba(0xD1343833))
                .border_b_1()
                .border_color(rgb(0xD13438))
                .text_sm()
                .child(div().flex_1().truncate().child(format!("Couldn't save the project: {err}")))
                .child(ui::button("retry-save", "Retry", None, false, cx.listener(|s, _, _, cx| {
                    s.save_now(cx);
                })))
                .child(ui::button("discard", "Close without saving", None, false, cx.listener(|s, _, window, _| s.discard_and_close(window))))
        });

        div()
            .id("studio")
            .relative()
            .size_full()
            .flex()
            .flex_col()
            .bg(ui::theme().base)
            .font_family(FONT)
            .text_color(ui::theme().text)
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::on_key))
            .child(header)
            .children(save_error)
            .child(
                div()
                    .flex_1()
                    .min_h_0()
                    .flex()
                    .child(
                        div()
                            .flex_1()
                            .min_w_0()
                            .flex()
                            .flex_col()
                            .child(self.preview(window, cx))
                            .child(self.timeline(cx)),
                    )
                    .child(self.sidebar(cx)),
            )
            .children(self.export_overlay(cx))
            .child(drag_layer)
    }
}
