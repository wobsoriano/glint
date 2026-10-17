use crate::library::{CaptureKind, Item};
use crate::settings::{LibraryView, Settings, SortKey};
use crate::store::{self, Store};
use crate::ui::{self, Edit, FONT};
use crate::{actions, clip, flow, overlay, prefs, quicklook, shell, toast, win};
use gpui::prelude::*;
use gpui::{
    div, img, px, rgb, rgba, size, uniform_list, App, Bounds, ClickEvent, Context, ExternalPaths, FocusHandle, Global, ImageSource,
    KeyDownEvent, ObjectFit, PromptLevel, RenderImage, SharedString, TitlebarOptions, UniformListScrollHandle, Window,
    WindowBounds, WindowHandle, WindowOptions,
};
use std::collections::{HashMap, VecDeque};
use std::path::PathBuf;
use std::sync::Arc;

const SIDEBAR_W: f32 = 196.;
const INSPECTOR_W: f32 = 268.;
const TILE_W: f32 = 184.;
const TILE_H: f32 = 156.;
const TILE_GAP: f32 = 14.;
const ROW_H: f32 = 44.;
const THUMB_CACHE: usize = 400;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Filter {
    All,
    Screenshots,
    Recordings,
}

impl Filter {
    fn admits(self, kind: CaptureKind) -> bool {
        match self {
            Filter::All => true,
            Filter::Screenshots => kind == CaptureKind::Screenshot,
            Filter::Recordings => kind == CaptureKind::Recording,
        }
    }
}

enum Thumb {
    Loading,
    Ready(Arc<RenderImage>),
    Missing,
}

#[derive(Default)]
struct Thumbs {
    map: HashMap<(PathBuf, i64), Thumb>,
    order: VecDeque<(PathBuf, i64)>,
}

#[derive(Default)]
struct LibraryState(Option<WindowHandle<LibraryWindow>>);

impl Global for LibraryState {}

pub struct LibraryWindow {
    filter: Filter,
    search: String,
    selection: Vec<PathBuf>,
    anchor: Option<PathBuf>,
    renaming: Option<(PathBuf, String)>,
    thumbs: Thumbs,
    focus: FocusHandle,
    search_focus: FocusHandle,
    scroll: UniformListScrollHandle,
    columns_hint: usize,
}

pub fn show(cx: &mut App) {
    if let Some(h) = cx.default_global::<LibraryState>().0 {
        if h.update(cx, |_, window, _| {
            window.activate_window();
            win::bring_to_front(window);
        })
        .is_ok()
        {
            return;
        }
    }
    let bounds = Bounds::centered(None, size(px(1180.), px(760.)), cx);
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions { title: Some("Glint Library".into()), appears_transparent: false, traffic_light_position: None }),
            window_min_size: Some(size(px(760.), px(480.))),
            focus: true,
            window_background: gpui::WindowBackgroundAppearance::Blurred,
            ..Default::default()
        },
        |window, cx| {
            shell::mica(window);
            let view = cx.new(|cx| {
                cx.observe_global::<Store>(|_, cx| cx.notify()).detach();
                cx.observe_global::<Settings>(|_, cx| cx.notify()).detach();
                LibraryWindow {
                    filter: Filter::All,
                    search: String::new(),
                    selection: Vec::new(),
                    anchor: None,
                    renaming: None,
                    thumbs: Thumbs::default(),
                    focus: cx.focus_handle(),
                    search_focus: cx.focus_handle(),
                    scroll: UniformListScrollHandle::new(),
                    columns_hint: 1,
                }
            });
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            win::bring_to_front(window);
            view
        },
    );
    cx.default_global::<LibraryState>().0 = handle.ok();
}

fn prefs_update(cx: &mut App, f: impl FnOnce(&mut crate::settings::LibraryPrefs)) {
    let s = cx.global_mut::<Settings>();
    f(&mut s.library);
    s.save();
}

impl LibraryWindow {
    fn visible(&self, cx: &App) -> Vec<Item> {
        let sort = cx.global::<Settings>().library.sort;
        let q = self.search.trim().to_lowercase();
        let mut items: Vec<Item> = cx
            .global::<Store>()
            .items
            .iter()
            .filter(|i| self.filter.admits(i.kind()))
            .filter(|i| q.is_empty() || i.meta.title.to_lowercase().contains(&q))
            .cloned()
            .collect();
        match sort {
            SortKey::Captured => items.sort_by(|a, b| b.meta.created.cmp(&a.meta.created)),
            SortKey::Modified => items.sort_by(|a, b| b.meta.modified.cmp(&a.meta.modified)),
            SortKey::Name => items.sort_by(|a, b| a.meta.title.to_lowercase().cmp(&b.meta.title.to_lowercase())),
        }
        items
    }

    fn selected_items(&self, cx: &App) -> Vec<Item> {
        self.visible(cx).into_iter().filter(|i| self.selection.contains(&i.dir)).collect()
    }

    fn thumb(&mut self, item: &Item, cx: &mut Context<Self>) -> Option<Arc<RenderImage>> {
        let key = (item.dir.clone(), item.meta.modified.timestamp_millis());
        match self.thumbs.map.get(&key) {
            Some(Thumb::Ready(img)) => return Some(img.clone()),
            Some(_) => return None,
            None => {}
        }
        self.thumbs.map.insert(key.clone(), Thumb::Loading);
        self.thumbs.order.push_back(key.clone());
        while self.thumbs.order.len() > THUMB_CACHE {
            if let Some(old) = self.thumbs.order.pop_front() {
                self.thumbs.map.remove(&old);
            }
        }
        let path = item.thumb();
        let task = cx.background_spawn(async move { image::open(&path).ok().map(|i| win::render_image(&i.to_rgba8())) });
        cx.spawn(async move |this, cx| {
            let loaded = task.await;
            let _ = this.update(cx, |this, cx| {
                if let Some(slot) = this.thumbs.map.get_mut(&key) {
                    *slot = loaded.map(Thumb::Ready).unwrap_or(Thumb::Missing);
                    cx.notify();
                }
            });
        })
        .detach();
        None
    }

    fn click(&mut self, dir: PathBuf, e: &ClickEvent, window: &mut Window, cx: &mut Context<Self>) {
        window.focus(&self.focus);
        let m = e.modifiers();
        if m.shift {
            let visible = self.visible(cx);
            let pos = |d: &PathBuf| visible.iter().position(|i| &i.dir == d);
            let a = self.anchor.as_ref().and_then(pos).unwrap_or(0);
            if let Some(b) = pos(&dir) {
                let (lo, hi) = (a.min(b), a.max(b));
                self.selection = visible[lo..=hi].iter().map(|i| i.dir.clone()).collect();
            }
        } else if m.control {
            if let Some(i) = self.selection.iter().position(|d| d == &dir) {
                self.selection.remove(i);
            } else {
                self.selection.push(dir.clone());
            }
            self.anchor = Some(dir);
        } else {
            self.selection = vec![dir.clone()];
            self.anchor = Some(dir.clone());
            if e.click_count() >= 2 {
                if let Some(item) = store::get(&dir, cx) {
                    quicklook::open(item, cx);
                }
            }
        }
        if self.renaming.as_ref().is_some_and(|(d, _)| !self.selection.contains(d)) {
            self.renaming = None;
        }
        cx.notify();
    }

    fn move_selection(&mut self, delta: isize, cx: &mut Context<Self>) {
        let visible = self.visible(cx);
        if visible.is_empty() {
            return;
        }
        let cur = self.anchor.as_ref().and_then(|a| visible.iter().position(|i| &i.dir == a));
        let next = match cur {
            Some(i) => (i as isize + delta).clamp(0, visible.len() as isize - 1) as usize,
            None => 0,
        };
        let dir = visible[next].dir.clone();
        self.selection = vec![dir.clone()];
        self.anchor = Some(dir);
        let row = match cx.global::<Settings>().library.view {
            LibraryView::Grid => next / self.columns_hint,
            LibraryView::List => next,
        };
        self.scroll.scroll_to_item(row, gpui::ScrollStrategy::Top);
        cx.notify();
    }

    fn start_rename(&mut self, cx: &mut Context<Self>) {
        if let [dir] = self.selection.as_slice() {
            if let Some(item) = store::get(dir, cx) {
                self.renaming = Some((dir.clone(), item.meta.title));
                cx.notify();
            }
        }
    }

    fn commit_rename(&mut self, cx: &mut Context<Self>) {
        if let Some((dir, title)) = self.renaming.take() {
            if let Err(e) = store::rename(&dir, &title, cx) {
                toast::show(format!("Rename failed: {e}"), cx);
            }
            cx.notify();
        }
    }

    fn confirm_delete(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let items = self.selected_items(cx);
        if items.is_empty() {
            return;
        }
        let msg = if items.len() == 1 {
            format!("Move \u{201c}{}\u{201d} to the Recycle Bin?", items[0].meta.title)
        } else {
            format!("Move {} captures to the Recycle Bin?", items.len())
        };
        let rx = window.prompt(
            PromptLevel::Warning,
            &msg,
            Some("Edits and recording tracks go with them. You can restore them from the Recycle Bin."),
            &["Move to Recycle Bin", "Cancel"],
            cx,
        );
        let dirs: Vec<PathBuf> = items.into_iter().map(|i| i.dir).collect();
        cx.spawn(async move |this, cx| {
            if rx.await != Ok(0) {
                return;
            }
            let _ = this.update(cx, |_, cx| {
                actions::delete(dirs.clone(), cx, |_, _| {});
            });
            let _ = this.update(cx, |this, cx| {
                this.selection.retain(|d| !dirs.contains(d));
                cx.notify();
            });
        })
        .detach();
    }

    fn on_key(&mut self, e: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        if self.search_focus.is_focused(window) {
            match ui::edit_text(&mut self.search, e) {
                Edit::Changed => {
                    self.selection.clear();
                    cx.notify();
                }
                Edit::Submit | Edit::Cancel => {
                    if e.keystroke.key == "escape" {
                        self.search.clear();
                    }
                    window.focus(&self.focus);
                    cx.notify();
                }
                Edit::Ignored => {}
            }
            cx.stop_propagation();
            return;
        }
        if let Some((_, title)) = &mut self.renaming {
            match ui::edit_text(title, e) {
                Edit::Submit => self.commit_rename(cx),
                Edit::Cancel => self.renaming = None,
                Edit::Changed | Edit::Ignored => {}
            }
            cx.stop_propagation();
            cx.notify();
            return;
        }
        let k = &e.keystroke;
        let ctrl = k.modifiers.control;
        let columns = match cx.global::<Settings>().library.view {
            LibraryView::Grid => self.columns_hint as isize,
            LibraryView::List => 1,
        };
        match k.key.as_str() {
            "a" if ctrl => {
                self.selection = self.visible(cx).into_iter().map(|i| i.dir).collect();
                cx.notify();
            }
            "c" if ctrl => {
                let items = self.selected_items(cx);
                match items.as_slice() {
                    [] => {}
                    [one] => actions::copy(one, cx),
                    many => actions::copy_files(many, cx),
                }
            }
            "f" if ctrl => {
                window.focus(&self.search_focus);
                cx.notify();
            }
            "i" if ctrl && k.modifiers.alt => prefs_update(cx, |p| p.inspector = !p.inspector),
            "delete" => self.confirm_delete(window, cx),
            "enter" | "f2" => self.start_rename(cx),
            "space" => {
                if !quicklook::close(cx) {
                    if let Some(item) = self.selected_items(cx).into_iter().next() {
                        quicklook::open(item, cx);
                    }
                }
            }
            "escape" => {
                self.selection.clear();
                cx.notify();
            }
            "left" => self.move_selection(-1, cx),
            "right" => self.move_selection(1, cx),
            "up" => self.move_selection(-columns, cx),
            "down" => self.move_selection(columns, cx),
            _ => return,
        }
        cx.stop_propagation();
    }
}

fn date(d: &chrono::DateTime<chrono::Local>) -> String {
    d.format("%b %-d, %Y at %-I:%M %p").to_string()
}

fn duration(ms: u64) -> String {
    format!("{}:{:02}", ms / 60_000, (ms / 1000) % 60)
}

fn sidebar_entry(id: &'static str, icon: &'static str, label: &'static str, count: usize, active: bool) -> gpui::Stateful<gpui::Div> {
    div()
        .id(id)
        .h(px(30.))
        .px_2()
        .flex()
        .items_center()
        .gap_2()
        .rounded(px(ui::RADIUS_CONTROL))
        .cursor_pointer()
        .text_sm()
        .when(active, |d| d.bg(ui::accent_alpha(0x33)).text_color(ui::theme().text))
        .when(!active, |d| d.text_color(ui::theme().text).hover(|s| s.bg(ui::theme().control_hover)))
        .child(ui::icon(icon, 15., if active { ui::accent() } else { ui::theme().muted.into() }))
        .child(div().flex_1().child(label))
        .child(div().text_xs().text_color(ui::theme().muted).child(count.to_string()))
}

fn block(label: &str, value: String) -> impl IntoElement {
    div()
        .flex()
        .flex_col()
        .gap_1()
        .child(div().text_xs().text_color(ui::theme().muted).child(label.to_string()))
        .child(div().text_xs().text_color(ui::theme().text).child(value))
}

fn row(label: &str, value: String) -> impl IntoElement {
    div()
        .flex()
        .justify_between()
        .gap_3()
        .text_xs()
        .child(div().text_color(ui::theme().muted).child(label.to_string()))
        .child(div().text_color(ui::theme().text).text_right().child(value))
}

impl LibraryWindow {
    fn tile(&mut self, item: &Item, cx: &mut Context<Self>) -> impl IntoElement {
        let selected = self.selection.contains(&item.dir);
        let thumb = self.thumb(item, cx);
        let dir = item.dir.clone();
        let renaming = self.renaming.as_ref().filter(|(d, _)| d == &item.dir).map(|(_, t)| t.clone());
        let recording = item.kind() == CaptureKind::Recording;
        div()
            .id(SharedString::from(format!("tile-{}", item.id())))
            .w(px(TILE_W))
            .h(px(TILE_H))
            .flex_none()
            .flex()
            .flex_col()
            .gap_1p5()
            .p_1p5()
            .rounded(px(ui::RADIUS_WINDOW))
            .cursor_pointer()
            .when(selected, |d| d.bg(ui::accent_alpha(0x2E)))
            .when(!selected, |d| d.hover(|s| s.bg(ui::theme().layer)))
            .group(SharedString::from(format!("tile-{}", item.id())))
            .child(
                div()
                    .relative()
                    .w_full()
                    .h(px(110.))
                    .rounded(px(ui::RADIUS_CONTROL))
                    .overflow_hidden()
                    .bg(ui::theme().canvas)
                    .border_2()
                    .border_color(if selected { ui::accent() } else { ui::theme().border })
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(match thumb {
                        Some(t) => img(ImageSource::Render(t)).size_full().object_fit(ObjectFit::Contain).into_any_element(),
                        None => ui::icon(if recording { "film" } else { "image" }, 26., ui::theme().muted).into_any_element(),
                    })
                    .child(
                        div()
                            .absolute()
                            .top(px(4.))
                            .right(px(4.))
                            .flex()
                            .gap_1()
                            .invisible()
                            .group_hover(SharedString::from(format!("tile-{}", item.id())), |s| s.visible())
                            .child(tile_action(format!("{}-copy", item.id()), "copy", "Copy", item.clone(), |i, cx| actions::copy(i, cx)))
                            .child(tile_action(format!("{}-ql", item.id()), "eye", "Quick Look", item.clone(), |i, cx| quicklook::open(i.clone(), cx)))
                            .child(tile_action(format!("{}-edit", item.id()), "edit", "Edit", item.clone(), |i, cx| actions::edit(i, cx))),
                    )
                    .when_some(item.meta.duration_ms.filter(|_| recording), |d, ms| {
                        d.child(
                            div()
                                .absolute()
                                .bottom(px(4.))
                                .right(px(4.))
                                .px_1()
                                .rounded(px(ui::RADIUS_CONTROL))
                                .bg(rgba(0x000000B3))
                                .text_xs()
                                .text_color(rgb(0xFFFFFF))
                                .child(duration(ms)),
                        )
                    }),
            )
            .child(match renaming {
                Some(t) => ui::text_box(&t, "", true).into_any_element(),
                None => div()
                    .px_1()
                    .text_xs()
                    .text_color(ui::theme().text)
                    .truncate()
                    .child(item.meta.title.clone())
                    .into_any_element(),
            })
            .on_click(cx.listener(move |this, e: &ClickEvent, window, cx| this.click(dir.clone(), e, window, cx)))
    }

    fn list_row(&mut self, item: &Item, cx: &mut Context<Self>) -> impl IntoElement {
        let selected = self.selection.contains(&item.dir);
        let thumb = self.thumb(item, cx);
        let dir = item.dir.clone();
        let renaming = self.renaming.as_ref().filter(|(d, _)| d == &item.dir).map(|(_, t)| t.clone());
        let recording = item.kind() == CaptureKind::Recording;
        div()
            .id(SharedString::from(format!("row-{}", item.id())))
            .h(px(ROW_H))
            .w_full()
            .px_3()
            .flex()
            .items_center()
            .gap_3()
            .cursor_pointer()
            .text_sm()
            .border_b_1()
            .border_color(ui::theme().border)
            .when(selected, |d| d.bg(ui::accent_alpha(0x2E)))
            .when(!selected, |d| d.hover(|s| s.bg(ui::theme().layer)))
            .child(
                div()
                    .w(px(52.))
                    .h(px(32.))
                    .flex_none()
                    .rounded(px(ui::RADIUS_CONTROL))
                    .overflow_hidden()
                    .bg(ui::theme().canvas)
                    .flex()
                    .items_center()
                    .justify_center()
                    .child(match thumb {
                        Some(t) => img(ImageSource::Render(t)).size_full().object_fit(ObjectFit::Cover).into_any_element(),
                        None => ui::icon(if recording { "film" } else { "image" }, 14., ui::theme().muted).into_any_element(),
                    }),
            )
            .child(match renaming {
                Some(t) => div().flex_1().child(ui::text_box(&t, "", true)).into_any_element(),
                None => div().flex_1().min_w_0().overflow_hidden().whitespace_nowrap().text_ellipsis().text_color(ui::theme().text).child(item.meta.title.clone()).into_any_element(),
            })
            .child(div().w(px(90.)).text_xs().text_color(ui::theme().muted).child(if recording { "Recording" } else { "Screenshot" }))
            .child(div().w(px(96.)).text_xs().text_color(ui::theme().muted).child(format!("{} × {}", item.meta.width, item.meta.height)))
            .child(div().w(px(170.)).text_xs().text_color(ui::theme().muted).child(date(&item.meta.created)))
            .child(div().w(px(70.)).text_xs().text_color(ui::theme().muted).text_right().child(actions::human_bytes(item.bytes)))
            .on_click(cx.listener(move |this, e: &ClickEvent, window, cx| this.click(dir.clone(), e, window, cx)))
    }

    fn inspector(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let items = self.selected_items(cx);
        let body = match items.as_slice() {
            [] => div().text_sm().text_color(ui::theme().muted).child("Select a capture to see its details.").into_any_element(),
            [item] => {
                let recording = item.kind() == CaptureKind::Recording;
                let edited = match (item.is_edited(), recording) {
                    (true, false) => "Annotated, edits saved",
                    (false, false) => "Original, no edits",
                    (true, true) => "Studio project saved",
                    (false, true) => "No Studio edits",
                };
                div()
                    .flex()
                    .flex_col()
                    .gap_2()
                    .child(div().text_base().font_weight(gpui::FontWeight::SEMIBOLD).child(item.meta.title.clone()))
                    .child(div().text_xs().text_color(ui::theme().muted).child(if recording { "Recording" } else { "Screenshot" }))
                    .child(div().h(px(1.)).bg(ui::theme().border))
                    .child(row("Dimensions", format!("{} × {}", item.meta.width, item.meta.height)))
                    .child(row("Captured", date(&item.meta.created)))
                    .child(row("Modified", date(&item.meta.modified)))
                    .when_some(item.meta.duration_ms, |d, ms| d.child(row("Duration", duration(ms))))
                    .child(row("Disk usage", actions::human_bytes(item.bytes)))
                    .child(row("Editing", edited.to_string()))
                    .when_some(item.meta.source.clone(), |d, s| d.child(row("Source", s)))
                    .when_some(item.meta.imported_from.clone(), |d, p| d.child(block("Imported from", p.display().to_string())))
                    .child(block("Location", item.dir.display().to_string()))
                    .into_any_element()
            }
            many => div()
                .flex()
                .flex_col()
                .gap_2()
                .child(div().text_base().font_weight(gpui::FontWeight::SEMIBOLD).child(format!("{} captures", many.len())))
                .child(row("Disk usage", actions::human_bytes(many.iter().map(|i| i.bytes).sum())))
                .into_any_element(),
        };
        let has = !items.is_empty();
        let single = items.len() == 1;
        let (edit_items, copy_items, export_items, reveal_items, ql_items) = (items.clone(), items.clone(), items.clone(), items.clone(), items);
        div()
            .w(px(INSPECTOR_W))
            .flex_none()
            .h_full()
            .flex()
            .flex_col()
            .gap_4()
            .p_4()
            .bg(ui::theme().layer)
            .border_l_1()
            .border_color(ui::theme().border)
            .child(div().flex_1().min_h_0().child(body))
            .when(has, |d| {
                d.child(
                    div()
                        .flex()
                        .flex_wrap()
                        .gap_2()
                        .when(single, |d| {
                            d.child(ui::button("i-edit", "Edit", Some("edit"), true, move |_, _, cx| actions::edit(&edit_items[0], cx)))
                                .child(ui::button("i-ql", "Quick Look", Some("eye"), false, move |_, _, cx| quicklook::open(ql_items[0].clone(), cx)))
                        })
                        .child(ui::button("i-copy", "Copy", Some("copy"), false, move |_, _, cx| match copy_items.as_slice() {
                            [one] => actions::copy(one, cx),
                            many => actions::copy_files(many, cx),
                        }))
                        .child(ui::button("i-export", "Export…", Some("save"), false, move |_, _, cx| actions::export_to_folder(export_items.clone(), cx)))
                        .child(ui::button("i-reveal", "Show in Explorer", Some("folder"), false, move |_, _, _| {
                            let dirs: Vec<PathBuf> = reveal_items.iter().map(|i| i.dir.clone()).collect();
                            shell::reveal(&dirs);
                        }))
                        .child(ui::button("i-trash", "Move to Recycle Bin", Some("trash"), false, cx.listener(|this, _, window, cx| this.confirm_delete(window, cx)))),
                )
            })
    }
}

fn columns_for(window: &Window, inspector: bool) -> usize {
    let w = f32::from(window.viewport_size().width) - SIDEBAR_W - if inspector { INSPECTOR_W } else { 0. } - 32.;
    ((w + TILE_GAP) / (TILE_W + TILE_GAP)).floor().max(1.) as usize
}

impl Render for LibraryWindow {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let prefs = cx.global::<Settings>().library.clone();
        let all = cx.global::<Store>().items.clone();
        let loaded = cx.global::<Store>().loaded;
        let visible = self.visible(cx);
        self.selection.retain(|d| all.iter().any(|i| &i.dir == d));
        let columns = columns_for(window, prefs.inspector);
        self.columns_hint = columns;
        let count = |k: Option<CaptureKind>| all.iter().filter(|i| k.is_none_or(|k| i.kind() == k)).count();
        let search_focused = self.search_focus.is_focused(window);

        let sidebar = div()
            .w(px(SIDEBAR_W))
            .flex_none()
            .h_full()
            .flex()
            .flex_col()
            .gap_1()
            .p_3()
            .bg(ui::theme().layer)
            .border_r_1()
            .border_color(ui::theme().border)
            .child(div().px_2().pb_2().text_xs().font_weight(gpui::FontWeight::SEMIBOLD).text_color(ui::theme().muted).child("LIBRARY"))
            .child(sidebar_entry("f-all", "library", "All Captures", count(None), self.filter == Filter::All).on_click(cx.listener(|this, _, _, cx| {
                this.filter = Filter::All;
                cx.notify();
            })))
            .child(sidebar_entry("f-shots", "image", "Screenshots", count(Some(CaptureKind::Screenshot)), self.filter == Filter::Screenshots).on_click(cx.listener(|this, _, _, cx| {
                this.filter = Filter::Screenshots;
                cx.notify();
            })))
            .child(sidebar_entry("f-recs", "film", "Recordings", count(Some(CaptureKind::Recording)), self.filter == Filter::Recordings).on_click(cx.listener(|this, _, _, cx| {
                this.filter = Filter::Recordings;
                cx.notify();
            })))
            .child(div().flex_1())
            .child(div().px_2().pb_1().text_xs().font_weight(gpui::FontWeight::SEMIBOLD).text_color(ui::theme().muted).child("NEW CAPTURE"))
            .child(capture_entry("n-area", "area", "Area", |cx| flow::start_area(overlay::Mode::Image, cx)))
            .child(capture_entry("n-window", "window", "Window", flow::capture_window))
            .child(capture_entry("n-screen", "monitor", "Full Screen", flow::capture_fullscreen))
            .child(capture_entry("n-text", "scan", "Text", |cx| flow::start_area(overlay::Mode::Text, cx)))
            .child(div().h(px(8.)))
            .child(capture_entry("n-settings", "settings", "Settings", prefs::show));

        let sort_chip = |id: &'static str, label: &'static str, key: SortKey| {
            ui::chip(id, label, prefs.sort == key, move |_, _, cx| prefs_update(cx, |p| p.sort = key))
        };
        let toolbar = div()
            .h(px(48.))
            .flex_none()
            .px_4()
            .flex()
            .items_center()
            .gap_2()
            .border_b_1()
            .border_color(ui::theme().border)
            .child(
                div()
                    .id("search")
                    .w(px(240.))
                    .track_focus(&self.search_focus)
                    .cursor_text()
                    .child(ui::text_box(&self.search, "Search by name", search_focused))
                    .on_click(cx.listener(|this, _, window, cx| {
                        window.focus(&this.search_focus);
                        cx.notify();
                    })),
            )
            .child(div().flex_1())
            .child(div().text_xs().text_color(ui::theme().muted).child("Sort"))
            .child(sort_chip("s-date", "Capture Date", SortKey::Captured))
            .child(sort_chip("s-name", "Name", SortKey::Name))
            .child(sort_chip("s-mod", "Modified", SortKey::Modified))
            .child(div().w(px(8.)))
            .child(ui::icon_button("v-grid", "grid", prefs.view == LibraryView::Grid, "Grid", |_, _, cx| prefs_update(cx, |p| p.view = LibraryView::Grid)))
            .child(ui::icon_button("v-list", "list", prefs.view == LibraryView::List, "List", |_, _, cx| prefs_update(cx, |p| p.view = LibraryView::List)))
            .child(ui::icon_button("v-insp", "sidebar", prefs.inspector, "Inspector (Ctrl+Alt+I)", |_, _, cx| prefs_update(cx, |p| p.inspector = !p.inspector)));

        let n = visible.len();
        let content = if n == 0 {
            let msg = if !loaded {
                "Loading…"
            } else if !self.search.is_empty() {
                "No captures match your search."
            } else {
                "No captures yet. Press Ctrl+Shift+4 to capture an area."
            };
            div().flex_1().flex().items_center().justify_center().text_sm().text_color(ui::theme().muted).child(msg).into_any_element()
        } else {
            match prefs.view {
                LibraryView::Grid => {
                    let rows = n.div_ceil(columns);
                    uniform_list(
                        "grid",
                        rows,
                        cx.processor(move |this, range: std::ops::Range<usize>, _, cx| {
                            let visible = this.visible(cx);
                            range
                                .map(|r| {
                                    let tiles: Vec<_> = visible.iter().skip(r * columns).take(columns).cloned().collect();
                                    div()
                                        .px_4()
                                        .pt(px(TILE_GAP))
                                        .flex()
                                        .gap(px(TILE_GAP))
                                        .children(tiles.iter().map(|i| this.tile(i, cx).into_any_element()))
                                })
                                .collect()
                        }),
                    )
                    .track_scroll(self.scroll.clone())
                    .flex_1()
                    .into_any_element()
                }
                LibraryView::List => uniform_list(
                    "list",
                    n,
                    cx.processor(|this, range: std::ops::Range<usize>, _, cx| {
                        let visible = this.visible(cx);
                        range.map(|i| this.list_row(&visible[i], cx).into_any_element()).collect()
                    }),
                )
                .track_scroll(self.scroll.clone())
                .flex_1()
                .into_any_element(),
            }
        };

        let selected = self.selection.len();
        let status = div()
            .h(px(26.))
            .flex_none()
            .px_4()
            .flex()
            .items_center()
            .text_xs()
            .text_color(ui::theme().muted)
            .border_t_1()
            .border_color(ui::theme().border)
            .child(match selected {
                0 => format!("{n} capture(s)"),
                s => format!("{s} of {n} selected"),
            });

        div()
            .id("library")
            .key_context("Library")
            .track_focus(&self.focus)
            .size_full()
            .flex()
            .font_family(FONT)
            .text_color(ui::theme().text)
            .on_key_down(cx.listener(Self::on_key))
            .on_drop(|dropped: &ExternalPaths, _, cx| {
                let (videos, images): (Vec<PathBuf>, Vec<PathBuf>) = dropped.paths().iter().cloned().partition(|p| clip::is_video(p));
                for v in videos {
                    clip::open(v, cx);
                }
                if !images.is_empty() {
                    flow::open_images(images, cx);
                }
            })
            .child(sidebar)
            .child(div().flex_1().min_w_0().h_full().flex().flex_col().bg(ui::theme().base).child(toolbar).child(content).child(status))
            .when(prefs.inspector, |d| d.child(self.inspector(cx)))
    }
}

fn tile_action(id: String, icon: &'static str, tip: &'static str, item: Item, f: fn(&Item, &mut App)) -> impl IntoElement {
    div()
        .id(SharedString::from(id))
        .size(px(24.))
        .flex()
        .items_center()
        .justify_center()
        .rounded_full()
        .bg(rgba(0x1C1C1FE6))
        .border_1()
        .border_color(rgba(0xFFFFFF2E))
        .hover(|s| s.bg(ui::accent()))
        .child(ui::icon(icon, 12., rgb(0xFFFFFF)))
        .tooltip(ui::tooltip(tip))
        .on_click(move |_, _, cx| {
            cx.stop_propagation();
            f(&item, cx);
        })
}

fn capture_entry(id: &'static str, icon: &'static str, label: &'static str, f: fn(&mut App)) -> impl IntoElement {
    div()
        .id(id)
        .h(px(28.))
        .px_2()
        .flex()
        .items_center()
        .gap_2()
        .rounded(px(ui::RADIUS_CONTROL))
        .cursor_pointer()
        .text_sm()
        .text_color(ui::theme().text)
        .hover(|s| s.bg(ui::theme().control))
        .child(ui::icon(icon, 15., ui::theme().muted))
        .child(label)
        .on_click(move |_, _, cx| cx.defer(f))
}
