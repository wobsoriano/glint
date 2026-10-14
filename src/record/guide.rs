//! The area guide: dims the display outside the recorded area and outlines it, or outlines
//! the recorded window. Click-through, and never part of the recording.

use super::chrome::{self, Kind, Placement};
use super::target::display_rect;
use super::Target;
use crate::ui;
use gpui::prelude::*;
use gpui::{div, px, rgba, AnyWindowHandle, App, Context, Window};

/// `None` for a whole display, which needs no guide.
pub fn open(target: Target, cx: &mut App) -> Option<AnyWindowHandle> {
    if matches!(target, Target::Display(_)) {
        return None;
    }
    let d = target.display();
    let placement = Placement { rect: display_rect(d), scale: d.scale, kind: Kind::ClickThrough };
    let follows_window = matches!(target, Target::Window { .. });
    let handle = chrome::open(placement, cx, move |_, _| Guide { target })?;
    if follows_window {
        chrome::tick(handle.into(), 200, cx);
    }
    Some(handle.into())
}

struct Guide {
    target: Target,
}

fn shade(x: f32, y: f32, w: f32, h: f32) -> gpui::Div {
    div().absolute().left(px(x)).top(px(y)).w(px(w.max(0.))).h(px(h.max(0.))).bg(rgba(0x00000080))
}

impl Render for Guide {
    fn render(&mut self, window: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        let s = window.scale_factor();
        let d = display_rect(self.target.display());
        let f = self.target.frame();
        let (x, y, w, h) = ((f[0] - d[0]) as f32 / s, (f[1] - d[1]) as f32 / s, f[2] as f32 / s, f[3] as f32 / s);
        let (vw, vh) = (d[2] as f32 / s, d[3] as f32 / s);
        let area = matches!(self.target, Target::Area { .. });
        div()
            .size_full()
            .relative()
            .when(area, |root| {
                root.child(shade(0., 0., vw, y))
                    .child(shade(0., y + h, vw, vh - y - h))
                    .child(shade(0., y, x, h))
                    .child(shade(x + w, y, vw - x - w, h))
            })
            .child(
                div()
                    .absolute()
                    .left(px(x - 2.))
                    .top(px(y - 2.))
                    .w(px(w + 4.))
                    .h(px(h + 4.))
                    .border_2()
                    .border_dashed()
                    .border_color(ui::accent()),
            )
    }
}
