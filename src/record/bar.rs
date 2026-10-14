//! The control bar while recording: elapsed time, pause or resume, restart, stop, delete.

use super::chrome::{self, Kind, Placement};
use super::target::work_area;
use super::{Stage, Target};
use crate::ui::{self, FONT};
use gpui::prelude::*;
use gpui::{div, px, rgb, rgba, AnyWindowHandle, App, ClickEvent, Context, FontWeight, SharedString, Window};
use std::time::Instant;

const W: f32 = 300.;
const H: f32 = 48.;

pub fn open(target: &Target, cx: &mut App) -> Option<AnyWindowHandle> {
    let d = target.display();
    let area = work_area(d);
    let (pw, ph) = ((W * d.scale).round() as i32, (H * d.scale).round() as i32);
    let rect = [area[0] + (area[2] - pw) / 2, area[1] + area[3] - ph - (28. * d.scale) as i32, pw, ph];
    let placement = Placement { rect, scale: d.scale, kind: Kind::DuringRecording };
    let handle = chrome::open(placement, cx, |_, _| Bar { confirm_delete: false, born: Instant::now() })?;
    chrome::tick(handle.into(), 200, cx);
    Some(handle.into())
}

struct Bar {
    confirm_delete: bool,
    born: Instant,
}

/// Elapsed media time and whether it is paused; `None` while the recorder is (re)starting.
fn progress(cx: &App) -> Option<(u64, bool)> {
    match &cx.try_global::<super::Rec>()?.stage {
        Stage::Live(live) => Some((live.recorder.elapsed().as_millis() as u64, live.recorder.is_paused())),
        _ => None,
    }
}

pub fn clock(ms: u64) -> String {
    let s = ms / 1000;
    if s >= 3600 {
        format!("{}:{:02}:{:02}", s / 3600, s / 60 % 60, s % 60)
    } else {
        format!("{:02}:{:02}", s / 60, s % 60)
    }
}

fn button(id: &'static str, icon: &str, danger: bool, on: impl Fn(&ClickEvent, &mut Window, &mut App) + 'static) -> impl IntoElement {
    div()
        .id(id)
        .size(px(32.))
        .flex()
        .items_center()
        .justify_center()
        .rounded(px(ui::RADIUS_CONTROL))
        .cursor_pointer()
        .hover(move |s| if danger { s.bg(rgba(0xC42B1CE6)) } else { s.bg(ui::theme().control_hover) })
        .child(ui::icon(icon, 16., ui::theme().text))
        .on_click(on)
}

/// Runs a lifecycle step after this event; the step may close this very window.
fn later(f: fn(&mut App)) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    move |_, _, cx| cx.defer(f)
}

impl Render for Bar {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let progress = progress(cx);
        let shell = div()
            .size_full()
            .flex()
            .items_center()
            .gap_1()
            .px_2()
            .font_family(FONT)
            .rounded(px(ui::RADIUS_WINDOW))
            .bg(ui::theme().flyout)
            .border_1()
            .border_color(ui::theme().border)
            .text_color(ui::theme().text);
        if self.confirm_delete {
            return shell
                .justify_between()
                .child(div().pl_2().text_sm().child("Delete this recording?"))
                .child(
                    div()
                        .flex()
                        .gap_1p5()
                        .child(ui::button("keep", "Keep", None, false, cx.listener(|this, _, _, cx| {
                            this.confirm_delete = false;
                            cx.notify();
                        })))
                        .child(
                            div()
                                .id("confirm-delete")
                                .h(px(30.))
                                .px_3()
                                .flex()
                                .items_center()
                                .gap_1p5()
                                .rounded(px(ui::RADIUS_CONTROL))
                                .cursor_pointer()
                                .bg(rgb(0xC42B1C))
                                .hover(|s| s.bg(rgb(0xD83B2C)))
                                .text_sm()
                                .text_color(rgb(0xFFFFFF))
                                .child(ui::icon("trash", 14., rgb(0xFFFFFF)))
                                .child("Delete")
                                .on_click(later(super::delete)),
                        ),
                )
                .into_any_element();
        }
        let (label, paused) = match progress {
            Some((ms, paused)) => (SharedString::from(clock(ms)), paused),
            None => ("Starting".into(), false),
        };
        // The dot blinks while recording and holds still when paused.
        let lit = paused || self.born.elapsed().as_millis() / 600 % 2 == 0;
        shell
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .pl_2()
                    .pr_2()
                    .w(px(118.))
                    .flex_none()
                    .child(div().size(px(10.)).rounded_full().bg(if paused { rgb(0xF7B500) } else if lit { rgb(0xFF453A) } else { rgba(0xFF453A40) }))
                    .child(div().text_sm().font_weight(FontWeight::SEMIBOLD).child(label))
                    .when(paused, |d| d.child(div().text_xs().text_color(ui::theme().muted).child("Paused"))),
            )
            .child(div().w(px(1.)).h(px(22.)).bg(ui::theme().border))
            .child(button("pause", if paused { "play" } else { "pause" }, false, later(super::pause_toggle)))
            .child(button("restart", "restart", false, later(super::restart)))
            .child(button("stop", "stop", false, later(super::stop)))
            .child(button("delete", "trash", true, cx.listener(|this, _, _, cx| {
                this.confirm_delete = true;
                cx.notify();
            })))
            .into_any_element()
    }
}

#[cfg(test)]
mod tests {
    use super::clock;

    #[test]
    fn clock_shows_minutes_then_hours() {
        assert_eq!(clock(0), "00:00");
        assert_eq!(clock(61_999), "01:01");
        assert_eq!(clock(3_725_000), "1:02:05");
    }
}
