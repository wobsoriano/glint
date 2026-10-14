//! The floating preview stack: one card per recent capture, docked to the left or right
//! edge of the display the capture came from. It collapses into a small peek tab.

use crate::cards::{CardAction, CardLayout, Corner};
use crate::flow::Shot;
use crate::settings::{Settings, Side};
use crate::ui::{self, FONT};
use crate::library::Item;
use crate::{actions, pin, quicklook, shell, win};
use gpui::prelude::*;
use gpui::{
    div, img, point, px, rgb, rgba, size, App, Bounds, Context, Global, ImageSource, MouseButton, MouseDownEvent,
    MouseMoveEvent, ObjectFit, Pixels, Point, RenderImage, SharedString, Timer, Window, WindowBounds, WindowHandle,
    WindowKind, WindowOptions,
};
use std::sync::Arc;
use std::time::{Duration, Instant};

const CARD_W: f32 = 224.;
const CARD_H: f32 = 148.;
const GAP: f32 = 12.;
const EDGE: f32 = 12.;
const PAD: f32 = 10.;
const HEADER_H: f32 = 28.;
const TAB_W: f32 = 30.;
const TAB_H: f32 = 84.;
const DRAG_SLOP: f32 = 6.;

#[derive(Default)]
struct StackState(Option<WindowHandle<Stack>>);

impl Global for StackState {}

struct Card {
    id: u64,
    shot: Shot,
    thumb: Arc<RenderImage>,
    deadline: Option<Instant>,
    status: Option<(SharedString, Instant)>,
}

pub struct Stack {
    cards: Vec<Card>,
    collapsed: bool,
    hovered: bool,
    /// Physical work area and scale of the display the stack lives on.
    area: [i32; 4],
    scale: f32,
    side: Side,
    press: Option<(u64, Point<Pixels>)>,
    /// The card whose More menu is open.
    more: Option<u64>,
    next_id: u64,
}

fn capacity(area_h: f32) -> usize {
    (((area_h - HEADER_H - PAD * 2. - EDGE * 2.) / (CARD_H + GAP)) as usize).max(1)
}

impl Stack {
    fn size(&self) -> (f32, f32) {
        if self.collapsed {
            return (TAB_W, TAB_H);
        }
        let n = self.cards.len() as f32;
        (PAD * 2. + CARD_W, PAD * 2. + HEADER_H + GAP + n * (CARD_H + GAP) - GAP)
    }

    /// Physical rectangle for the current state, anchored to the bottom of the docked edge.
    fn rect(&self) -> [i32; 4] {
        let (w, h) = self.size();
        let (pw, ph) = ((w * self.scale).round() as i32, (h * self.scale).round() as i32);
        let [ax, ay, aw, ah] = self.area;
        // The peek tab sits flush against the edge; the open stack keeps a margin.
        let edge = if self.collapsed { 0 } else { (EDGE * self.scale).round() as i32 };
        let x = match self.side {
            Side::Left => ax + edge,
            Side::Right => ax + aw - pw - edge,
        };
        let y = if self.collapsed { ay + ah - ph - (ah as f32 * 0.18) as i32 } else { ay + ah - ph - edge };
        [x, y, pw, ph]
    }

    fn relayout(&self, window: &mut Window, cx: &App) {
        shell::place(window, self.rect(), cx);
    }

    fn card(&self, id: u64) -> Option<&Card> {
        self.cards.iter().find(|c| c.id == id)
    }

    fn remove(&mut self, id: u64, window: &mut Window, cx: &App) {
        self.cards.retain(|c| c.id != id);
        if self.cards.is_empty() {
            window.remove_window();
        } else {
            self.relayout(window, cx);
        }
    }

    fn flash(&mut self, id: u64, msg: impl Into<SharedString>, cx: &mut Context<Self>) {
        if let Some(c) = self.cards.iter_mut().find(|c| c.id == id) {
            c.status = Some((msg.into(), Instant::now() + Duration::from_millis(1300)));
        }
        cx.notify();
    }

    fn run(&mut self, id: u64, action: CardAction, window: &mut Window, cx: &mut Context<Self>) {
        if self.more == Some(id) {
            self.more = None;
        }
        let Some(card) = self.card(id) else { return };
        let shot = card.shot.clone();
        let item = shot.item.clone();
        let this = cx.entity().downgrade();
        match action {
            CardAction::Copy => match win::copy_image(&shot.image) {
                Ok(()) => self.flash(id, "Copied", cx),
                Err(e) => self.flash(id, format!("Copy failed: {e}"), cx),
            },
            CardAction::Save | CardAction::SaveAs => {
                let ask = action == CardAction::SaveAs;
                actions::save(
                    &item,
                    ask,
                    cx,
                    Box::new(move |result, cx| {
                        let msg = match &result {
                            Ok(_) => Some("Saved"),
                            Err(e) if e.to_string() == "cancelled" => None,
                            Err(_) => Some("Not saved"),
                        };
                        if let Some(msg) = msg {
                            let _ = this.update(cx, |s, cx| s.flash(id, msg, cx));
                        }
                        actions::report_save(&result, cx);
                    }),
                );
            }
            CardAction::Compress => actions::compress(&item, cx),
            CardAction::CopyText => actions::copy_text(&item, cx),
            CardAction::QuickLook => quicklook::open(item, cx),
            CardAction::Pin => {
                pin::open(shot.image.clone(), shot.dpi, cx);
                self.remove(id, window, cx);
            }
            CardAction::Edit => actions::edit(&item, cx),
            CardAction::Delete => {
                let handle = window.window_handle();
                actions::delete(vec![item.dir.clone()], cx, move |ok, cx| {
                    if ok {
                        let _ = handle.update(cx, |_, window, cx| {
                            let _ = this.update(cx, |s, cx| s.remove(id, window, cx));
                        });
                    }
                });
            }
            CardAction::Dismiss => self.remove(id, window, cx),
            CardAction::More => {
                self.more = if self.more == Some(id) { None } else { Some(id) };
                cx.notify();
                return;
            }
        }
    }

    /// Starts a system drag of the card's file once the pointer travels past the slop.
    fn on_move(&mut self, id: u64, e: &MouseMoveEvent, window: &mut Window, cx: &mut Context<Self>) {
        let Some((pid, start)) = self.press else { return };
        if pid != id || e.pressed_button != Some(MouseButton::Left) {
            return;
        }
        let d = e.position - start;
        if f32::from(d.x).hypot(f32::from(d.y)) < DRAG_SLOP {
            return;
        }
        // OLE never reports the button release to GPUI; disarm so the next move can't re-drag.
        self.press = None;
        let Some(card) = self.card(id) else { return };
        let (item, image) = (card.shot.item.clone(), card.shot.image.clone());
        let dismiss = cx.global::<Settings>().overlay.dismiss_after_drag;
        let (area, side) = (self.area, self.side);
        let handle = window.window_handle();
        let this = cx.entity().downgrade();
        // The drag loop pumps messages, so it must run outside any GPUI update.
        cx.spawn(async move |_, cx| {
            Timer::after(Duration::from_millis(1)).await;
            let dropped = actions::drag_file(&item).and_then(|path| shell::drag_out(&path, Some(&image)));
            let _ = handle.update(cx, |_, window, cx| {
                let _ = this.update(cx, |s, cx| match dropped {
                    Ok(true) if dismiss => s.remove(id, window, cx),
                    Ok(false) if past_edge(area, side) => s.remove(id, window, cx),
                    Ok(_) => cx.notify(),
                    Err(e) => s.flash(id, format!("Drag failed: {e}"), cx),
                });
            });
        })
        .detach();
    }

    fn tick(&mut self, window: &mut Window, cx: &mut Context<Self>) {
        let now = Instant::now();
        let mut changed = false;
        for c in &mut self.cards {
            if c.status.as_ref().is_some_and(|(_, until)| *until <= now) {
                c.status = None;
                changed = true;
            }
        }
        if !self.hovered && !self.collapsed {
            let before = self.cards.len();
            self.cards.retain(|c| c.deadline.is_none_or(|d| d > now));
            if self.cards.len() != before {
                if self.cards.is_empty() {
                    window.remove_window();
                    return;
                }
                self.relayout(window, cx);
                changed = true;
            }
        }
        if changed {
            cx.notify();
        }
    }
}

/// True when the pointer was released at or beyond the docked screen edge: a card thrown
/// off the screen there is dismissed.
fn past_edge(area: [i32; 4], side: Side) -> bool {
    let (x, _) = crate::capture::cursor_pos();
    match side {
        Side::Left => x <= area[0] + 2,
        Side::Right => x >= area[0] + area[2] - 3,
    }
}

fn thumb_of(shot: &Shot) -> Arc<RenderImage> {
    win::render_image(&crate::library::thumbnail(&shot.image))
}

/// Shows a capture's new deliverable on every card of it, after an edit was saved.
pub fn show_edit(item: &Item, image: Arc<image::RgbaImage>, cx: &mut App) {
    let Some(h) = cx.default_global::<StackState>().0 else { return };
    let _ = h.update(cx, |this, _, cx| {
        for c in this.cards.iter_mut().filter(|c| c.shot.item.dir == item.dir) {
            c.shot.item = item.clone();
            c.shot.image = image.clone();
            c.thumb = thumb_of(&c.shot);
        }
        cx.notify();
    });
}

/// Adds a capture to the stack, opening the stack on the display under the pointer.
pub fn push(shot: Shot, cx: &mut App) {
    let s = cx.global::<Settings>().overlay.clone();
    let deadline = (s.auto_close_seconds > 0).then(|| Instant::now() + Duration::from_secs(s.auto_close_seconds as u64));
    let thumb = thumb_of(&shot);
    if let Some(h) = cx.default_global::<StackState>().0 {
        let added = h.update(cx, |this, window, cx| {
            this.next_id += 1;
            let card = Card { id: this.next_id, shot: shot.clone(), thumb: thumb.clone(), deadline, status: None };
            this.cards.push(card);
            let cap = capacity(this.area[3] as f32 / this.scale);
            if this.cards.len() > cap {
                let extra = this.cards.len() - cap;
                this.cards.drain(..extra);
            }
            this.collapsed = false;
            this.relayout(window, cx);
            cx.notify();
        });
        if added.is_ok() {
            return;
        }
    }
    let (area, scale) = shell::work_area_at_cursor();
    let stack = Stack {
        cards: vec![Card { id: 1, shot, thumb, deadline, status: None }],
        collapsed: false,
        hovered: false,
        area,
        scale,
        side: s.side,
        press: None,
        more: None,
        next_id: 1,
    };
    let rect = stack.rect();
    let (w, h) = stack.size();
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(Bounds { origin: point(px(0.), px(0.)), size: size(px(w), px(h)) })),
            titlebar: None,
            kind: WindowKind::PopUp,
            focus: false,
            is_movable: false,
            is_resizable: false,
            window_background: gpui::WindowBackgroundAppearance::Blurred,
            ..Default::default()
        },
        move |window, cx| {
            shell::place(window, rect, cx);
            cx.new(|_| stack)
        },
    );
    if let Ok(h) = handle {
        cx.default_global::<StackState>().0 = Some(h);
        cx.spawn(async move |cx| loop {
            Timer::after(Duration::from_millis(250)).await;
            if h.update(cx, |this, window, cx| this.tick(window, cx)).is_err() {
                break;
            }
        })
        .detach();
    }
}

/// Re-reads overlay settings (side, layout) into an open stack.
pub fn refresh(cx: &mut App) {
    let side = cx.global::<Settings>().overlay.side;
    if let Some(h) = cx.default_global::<StackState>().0 {
        let _ = h.update(cx, |this, window, cx| {
            this.side = side;
            this.relayout(window, cx);
            cx.notify();
        });
    }
}

fn round_button(id: SharedString, action: CardAction) -> gpui::Stateful<gpui::Div> {
    div()
        .id(id)
        .size(px(28.))
        .rounded_full()
        .flex()
        .items_center()
        .justify_center()
        .cursor_pointer()
        .bg(rgba(0x1C1C1FE6))
        .border_1()
        .border_color(rgba(0xFFFFFF2E))
        .hover(|s| if action == CardAction::Delete { s.bg(rgba(0xC42B1CF0)) } else { s.bg(ui::accent()) })
        .child(ui::icon(action.icon(), 14., rgb(0xFFFFFF)))
        .tooltip(ui::tooltip(action.help()))
}

fn pill(id: SharedString, action: CardAction) -> gpui::Stateful<gpui::Div> {
    div()
        .id(id)
        .w(px(112.))
        .h(px(32.))
        .flex()
        .items_center()
        .justify_center()
        .gap_1p5()
        .rounded_full()
        .cursor_pointer()
        .bg(rgba(0x1C1C1FE6))
        .border_1()
        .border_color(rgba(0xFFFFFF2E))
        .hover(|s| s.bg(ui::accent()))
        .text_sm()
        .font_weight(gpui::FontWeight::MEDIUM)
        .text_color(rgb(0xFFFFFF))
        .child(ui::icon(action.icon(), 14., rgb(0xFFFFFF)))
        .child(action.label())
        .tooltip(ui::tooltip(action.help()))
}

/// The action overlay of one card, laid out from the user's card layout.
pub fn card_actions(
    layout: &CardLayout,
    key: &str,
    on: impl Fn(CardAction) -> Box<dyn Fn(&gpui::ClickEvent, &mut Window, &mut App)>,
) -> gpui::Div {
    let mut overlay = div()
        .absolute()
        .inset_0()
        .bg(rgba(0x0000008C))
        .flex()
        .flex_col()
        .items_center()
        .justify_center()
        .gap_1p5()
        .children(layout.center.iter().map(|&a| pill(format!("{key}-c-{a:?}").into(), a).on_click(on(a))));
    for corner in Corner::ALL {
        let Some(a) = layout.corner(corner) else { continue };
        let b = round_button(format!("{key}-{corner:?}").into(), a).absolute().on_click(on(a));
        let b = match corner {
            Corner::TopLeft => b.top(px(6.)).left(px(6.)),
            Corner::TopRight => b.top(px(6.)).right(px(6.)),
            Corner::BottomLeft => b.bottom(px(6.)).left(px(6.)),
            Corner::BottomRight => b.bottom(px(6.)).right(px(6.)),
        };
        overlay = overlay.child(b);
    }
    overlay
}

impl Render for Stack {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let left = self.side == Side::Left;
        let t = ui::theme();
        if self.collapsed {
            let n = self.cards.len();
            return div()
                .id("peek")
                .size_full()
                .font_family(FONT)
                .flex()
                .flex_col()
                .items_center()
                .justify_center()
                .gap_1()
                .bg(t.flyout)
                .border_1()
                .border_color(t.border)
                .when(left, |d| d.rounded_r_lg())
                .when(!left, |d| d.rounded_l_lg())
                .cursor_pointer()
                .hover(|s| s.bg(t.control_hover))
                .text_color(t.text)
                .text_xs()
                .child(ui::icon(if left { "chevron-right" } else { "chevron-left" }, 14., t.text))
                .child(n.to_string())
                .tooltip(ui::tooltip(format!("Show {n} capture(s)")))
                .on_click(cx.listener(|this, _, window, cx| {
                    this.collapsed = false;
                    this.relayout(window, cx);
                    cx.notify();
                }))
                .into_any_element();
        }

        let layout = cx.global::<Settings>().overlay.layout.clone();
        let n = self.cards.len();
        let header = div()
            .h(px(HEADER_H))
            .flex()
            .items_center()
            .gap_1p5()
            .when(!left, |d| d.justify_end())
            .child(
                div()
                    .h(px(26.))
                    .px_3()
                    .flex()
                    .items_center()
                    .rounded_full()
                    .bg(t.flyout)
                    .border_1()
                    .border_color(t.border)
                    .text_xs()
                    .text_color(t.text)
                    .child(if n == 1 { "1 Screenshot".to_string() } else { format!("{n} Screenshots") }),
            )
            .child(
                div()
                    .id("clear-all")
                    .h(px(26.))
                    .px_3()
                    .flex()
                    .items_center()
                    .rounded_full()
                    .cursor_pointer()
                    .bg(t.flyout)
                    .border_1()
                    .border_color(t.border)
                    .hover(|s| s.bg(ui::accent_alpha(0xF0)))
                    .text_xs()
                    .text_color(t.text)
                    .child("Clear All")
                    .on_click(|_, window, _| window.remove_window()),
            )
            .child(
                div()
                    .id("collapse")
                    .size(px(26.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .rounded_full()
                    .cursor_pointer()
                    .bg(t.flyout)
                    .border_1()
                    .border_color(t.border)
                    .hover(|s| s.bg(ui::accent_alpha(0xF0)))
                    .child(ui::icon(if left { "chevron-left" } else { "chevron-right" }, 13., t.text))
                    .tooltip(ui::tooltip("Tuck into the edge"))
                    .on_click(cx.listener(|this, _, window, cx| {
                        this.collapsed = true;
                        this.relayout(window, cx);
                        cx.notify();
                    })),
            );

        let weak = cx.entity().downgrade();
        let cards = self.cards.iter().rev().map(|c| {
            let id = c.id;
            let key = format!("card{id}");
            let menu_open = self.more == Some(id);
            let w = weak.clone();
            let actions = card_actions(&layout, &key, move |a| {
                let w = w.clone();
                Box::new(move |_, window, cx| {
                    cx.stop_propagation();
                    let _ = w.update(cx, |s, cx| s.run(id, a, window, cx));
                })
            });
            div()
                .id(SharedString::from(key.clone()))
                .group(SharedString::from(key.clone()))
                .relative()
                .w(px(CARD_W))
                .h(px(CARD_H))
                .flex_none()
                .rounded(px(ui::RADIUS_WINDOW))
                .overflow_hidden()
                .bg(t.canvas)
                .border_1()
                .border_color(t.border)
                .shadow_lg()
                .cursor_pointer()
                .child(img(ImageSource::Render(c.thumb.clone())).size_full().object_fit(ObjectFit::Cover))
                .child(if menu_open { actions } else { actions.invisible().group_hover(SharedString::from(key.clone()), |s| s.visible()) })
                .when(menu_open, |d| {
                    let w = weak.clone();
                    d.child(
                        div()
                            .absolute()
                            .top(px(6.))
                            .bottom(px(6.))
                            .left(px(40.))
                            .right(px(40.))
                            .p_1()
                            .flex()
                            .flex_col()
                            .gap_0p5()
                            .rounded(px(ui::RADIUS_WINDOW))
                            .bg(t.flyout)
                            .border_1()
                            .border_color(t.border)
                            .shadow_lg()
                            .children(layout.hidden.iter().filter(|&&a| a != CardAction::More).map(|&a| {
                                let w = w.clone();
                                div()
                                    .id(SharedString::from(format!("{key}-more-{a:?}")))
                                    .h(px(22.))
                                    .px_2()
                                    .flex()
                                    .flex_none()
                                    .items_center()
                                    .gap_2()
                                    .rounded(px(ui::RADIUS_CONTROL))
                                    .cursor_pointer()
                                    .text_xs()
                                    .text_color(t.text)
                                    .hover(|s| s.bg(ui::accent()))
                                    .child(ui::icon(a.icon(), 12., t.text))
                                    .child(a.label())
                                    .on_click(move |_, window, cx| {
                                        cx.stop_propagation();
                                        let _ = w.update(cx, |s, cx| s.run(id, a, window, cx));
                                    })
                            }))
                            .when(layout.hidden.iter().all(|&a| a == CardAction::More), |d| {
                                d.child(div().p_2().text_xs().text_color(t.muted).child("No hidden actions"))
                            }),
                    )
                })
                .on_mouse_down(
                    MouseButton::Left,
                    cx.listener(move |this, e: &MouseDownEvent, window, cx| {
                        if e.click_count >= 2 {
                            this.run(id, CardAction::Edit, window, cx);
                        } else {
                            this.press = Some((id, e.position));
                        }
                    }),
                )
                .on_mouse_move(cx.listener(move |this, e: &MouseMoveEvent, window, cx| this.on_move(id, e, window, cx)))
                .on_mouse_up(MouseButton::Left, cx.listener(|this, _, _, _| this.press = None))
                .when_some(c.status.clone(), |d, (msg, _)| {
                    d.child(
                        div()
                            .absolute()
                            .inset_0()
                            .bg(rgba(0x000000B3))
                            .flex()
                            .items_center()
                            .justify_center()
                            .gap_2()
                            .text_sm()
                            .text_color(rgb(0xFFFFFF))
                            .child(ui::icon("check", 16., rgb(0x34C759)))
                            .child(msg),
                    )
                })
        });

        div()
            .id("stack")
            .size_full()
            .font_family(FONT)
            .flex()
            .flex_col()
            .gap(px(GAP))
            .p(px(PAD))
            .bg(t.base)
            .on_hover(cx.listener(|this, h: &bool, _, cx| {
                this.hovered = *h;
                if !*h {
                    let floor = Instant::now() + Duration::from_secs(2);
                    for c in &mut this.cards {
                        if let Some(d) = &mut c.deadline {
                            *d = (*d).max(floor);
                        }
                    }
                }
                cx.notify();
            }))
            .child(header)
            .children(cards)
            .into_any_element()
    }
}
