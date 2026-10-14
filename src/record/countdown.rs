//! The start timer: big numerals centered on what is about to be recorded.

use super::chrome::{self, Kind, Placement};
use super::Target;
use crate::ui::FONT;
use gpui::prelude::*;
use gpui::{div, px, rgb, rgba, AnyWindowHandle, App, Context, FontWeight, Window};

const SIDE: f32 = 220.;

pub fn open(target: &Target, cx: &mut App) -> Option<AnyWindowHandle> {
    let scale = target.display().scale;
    let [x, y, w, h] = target.frame();
    let side = (SIDE * scale).round() as i32;
    let rect = [x + (w - side) / 2, y + (h - side) / 2, side, side];
    let handle = chrome::open(Placement { rect, scale, kind: Kind::Passive }, cx, |_, _| Countdown)?;
    chrome::tick(handle.into(), 100, cx);
    Some(handle.into())
}

struct Countdown;

impl Render for Countdown {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let left = super::countdown_left(cx).unwrap_or(0);
        div().size_full().flex().items_center().justify_center().font_family(FONT).child(
            div()
                .size(px(SIDE - 20.))
                .flex()
                .flex_col()
                .items_center()
                .justify_center()
                .rounded_full()
                .bg(rgba(0x0B0B0DD9))
                .border_1()
                .border_color(rgba(0xFFFFFF26))
                .child(div().text_size(px(104.)).font_weight(FontWeight::SEMIBOLD).line_height(px(112.)).text_color(rgb(0xFFFFFF)).child(left.to_string()))
                .child(div().text_xs().text_color(rgba(0xFFFFFFB3)).child("Esc to cancel")),
        )
    }
}
