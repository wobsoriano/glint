//! Full-screen frozen overlay for picking an area or a window.

use crate::capture::{Capture, MonitorShot, WinInfo};
use crate::flow::{self, AppState};
use crate::settings::Settings;
use crate::ui::{self, ACCENT, FONT};
use crate::win;
use gpui::prelude::*;
use gpui::{
    div, img, px, rgb, rgba, size, App, Bounds, Context, CursorStyle, FocusHandle, ImageSource,
    KeyDownEvent, MouseButton, MouseDownEvent, MouseMoveEvent, MouseUpEvent, ObjectFit,
    RenderImage, Window, WindowBackgroundAppearance, WindowBounds, WindowKind, WindowOptions,
};
use image::RgbaImage;
use std::sync::Arc;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Mode {
    Image,
    Text,
}

pub struct Overlay {
    mode: Mode,
    shot: Arc<RgbaImage>,
    origin: (i32, i32),
    dpi: f32,
    image: Arc<RenderImage>,
    /// Window rects relative to this monitor, physical pixels.
    windows: Vec<WinInfo>,
    drag_start: Option<(f32, f32)>,
    cursor: Option<(f32, f32)>,
    selection: Option<[f32; 4]>,
    hover: Option<usize>,
    loupe: Option<(Arc<RenderImage>, (i32, i32), [u8; 3])>,
    show_loupe: bool,
    focus: FocusHandle,
}

pub fn open(shots: Vec<MonitorShot>, windows: Vec<WinInfo>, mode: Mode, cx: &mut App) {
    let (cx_, cy_) = crate::capture::cursor_pos();
    let active = crate::capture::monitor_at(&shots, cx_, cy_);
    let displays = cx.displays();
    let show_loupe = cx.global::<Settings>().show_loupe;
    let mut handles = Vec::new();
    for (i, m) in shots.iter().enumerate() {
        // Match the GPUI display whose (logical) origin corresponds to this monitor.
        let display = displays.iter().find(|d| {
            let b = d.bounds();
            ((f32::from(b.origin.x) * m.scale) - m.x as f32).abs() < 4.0
                && ((f32::from(b.origin.y) * m.scale) - m.y as f32).abs() < 4.0
        });
        let bounds = display.map(|d| d.bounds()).unwrap_or(Bounds {
            origin: gpui::point(px(m.x as f32 / m.scale), px(m.y as f32 / m.scale)),
            size: size(px(m.width as f32 / m.scale), px(m.height as f32 / m.scale)),
        });
        let rect = [m.x, m.y, m.width as i32, m.height as i32];
        let local: Vec<WinInfo> = windows
            .iter()
            .map(|w| WinInfo {
                title: w.title.clone(),
                rect: [w.rect[0] - m.x, w.rect[1] - m.y, w.rect[2], w.rect[3]],
            })
            .collect();
        let shot = m.image.clone();
        let (origin, dpi) = ((m.x, m.y), m.scale);
        let focus_this = i == active;
        let handle = cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(bounds)),
                display_id: display.map(|d| d.id()),
                titlebar: None,
                kind: WindowKind::PopUp,
                focus: focus_this,
                show: true,
                is_movable: false,
                is_resizable: false,
                window_background: WindowBackgroundAppearance::Opaque,
                ..Default::default()
            },
            move |window, cx| {
                win::make_topmost(window, Some(rect), focus_this);
                let image = win::render_image(&shot);
                let view = cx.new(|cx| Overlay {
                    mode,
                    shot,
                    origin,
                    dpi,
                    image,
                    windows: local,
                    drag_start: None,
                    cursor: None,
                    selection: None,
                    hover: None,
                    loupe: None,
                    show_loupe,
                    focus: cx.focus_handle(),
                });
                if focus_this {
                    let f = view.read(cx).focus.clone();
                    window.focus(&f);
                }
                view
            },
        );
        if let Ok(h) = handle {
            handles.push(h.into());
        }
    }
    cx.global_mut::<AppState>().overlays = handles;
}

pub fn close_all(cx: &mut App) {
    let handles = std::mem::take(&mut cx.global_mut::<AppState>().overlays);
    for h in handles {
        let _ = h.update(cx, |_, window, _| window.remove_window());
    }
}

impl Overlay {
    fn phys(&self, window: &Window, p: gpui::Point<gpui::Pixels>) -> (f32, f32) {
        let s = window.scale_factor();
        (f32::from(p.x) * s, f32::from(p.y) * s)
    }

    fn window_at(&self, x: f32, y: f32) -> Option<usize> {
        self.windows.iter().position(|w| {
            let [wx, wy, ww, wh] = w.rect;
            x >= wx as f32 && y >= wy as f32 && x < (wx + ww) as f32 && y < (wy + wh) as f32
        })
    }

    fn update_loupe(&mut self, window: &mut Window, cx: &mut App) {
        let Some((x, y)) = self.cursor else { return };
        let (px_, py_) = (x as i32, y as i32);
        if self.loupe.as_ref().is_some_and(|l| l.1 == (px_, py_)) {
            return;
        }
        const N: i32 = 15;
        let zoom = (8.0 * window.scale_factor()).round() as u32;
        let side = N as u32 * zoom;
        let mut out = RgbaImage::new(side, side);
        let (w, h) = (self.shot.width() as i32, self.shot.height() as i32);
        let mut center = [0u8; 3];
        for gy in 0..N {
            for gx in 0..N {
                let sx = (px_ + gx - N / 2).clamp(0, w - 1);
                let sy = (py_ + gy - N / 2).clamp(0, h - 1);
                let p = *self.shot.get_pixel(sx as u32, sy as u32);
                if gx == N / 2 && gy == N / 2 {
                    center = [p[0], p[1], p[2]];
                }
                for yy in 0..zoom {
                    for xx in 0..zoom {
                        let grid = xx == 0 || yy == 0;
                        let mut c = p;
                        if grid {
                            c = image::Rgba([
                                (p[0] as u16 * 7 / 8) as u8,
                                (p[1] as u16 * 7 / 8) as u8,
                                (p[2] as u16 * 7 / 8) as u8,
                                255,
                            ]);
                        }
                        out.put_pixel(gx as u32 * zoom + xx, gy as u32 * zoom + yy, c);
                    }
                }
            }
        }
        // Outline the center pixel.
        let c0 = (N as u32 / 2) * zoom;
        for i in 0..=zoom {
            for (x, y) in [(c0 + i, c0), (c0 + i, c0 + zoom), (c0, c0 + i), (c0 + zoom, c0 + i)] {
                if x < side && y < side {
                    out.put_pixel(x, y, image::Rgba([255, 255, 255, 255]));
                }
            }
        }
        let new = win::render_image(&out);
        if let Some((old, _, _)) = self.loupe.replace((new, (px_, py_), center)) {
            cx.drop_image(old, Some(window));
        }
    }

    fn on_down(&mut self, e: &MouseDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let p = self.phys(window, e.position);
        self.drag_start = Some(p);
        self.selection = None;
        cx.notify();
    }

    fn on_move(&mut self, e: &MouseMoveEvent, window: &mut Window, cx: &mut Context<Self>) {
        let p = self.phys(window, e.position);
        self.cursor = Some(p);
        if let Some(s) = self.drag_start {
            if e.pressed_button == Some(MouseButton::Left) {
                let r = [
                    s.0.min(p.0).round(),
                    s.1.min(p.1).round(),
                    (s.0 - p.0).abs().round(),
                    (s.1 - p.1).abs().round(),
                ];
                if r[2] > 3.0 || r[3] > 3.0 {
                    self.selection = Some(r);
                    self.hover = None;
                }
            }
        } else {
            self.hover = self.window_at(p.0, p.1);
        }
        if self.show_loupe {
            self.update_loupe(window, cx);
        }
        cx.notify();
    }

    fn on_up(&mut self, e: &MouseUpEvent, window: &mut Window, cx: &mut Context<Self>) {
        let p = self.phys(window, e.position);
        let Some(start) = self.drag_start.take() else { return };
        let moved = (p.0 - start.0).abs() > 3.0 || (p.1 - start.1).abs() > 3.0;
        if moved {
            if let Some(r) = self.selection {
                self.complete([r[0] as i32, r[1] as i32, r[2] as i32, r[3] as i32], None, cx);
            }
        } else if let Some(i) = self.window_at(p.0, p.1) {
            let w = self.windows[i].clone();
            self.complete(w.rect, Some(w.title), cx);
        }
    }

    fn on_key(&mut self, e: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        match e.keystroke.key.as_str() {
            "escape" => cx.defer(close_all),
            "space" | "f" | "enter" => {
                let (w, h) = (self.shot.width() as i32, self.shot.height() as i32);
                let rect = self
                    .selection
                    .map(|r| [r[0] as i32, r[1] as i32, r[2] as i32, r[3] as i32])
                    .unwrap_or([0, 0, w, h]);
                self.complete(rect, None, cx);
            }
            _ => {}
        }
    }

    fn complete(&mut self, rect: [i32; 4], title: Option<String>, cx: &mut Context<Self>) {
        let (w, h) = (self.shot.width() as i32, self.shot.height() as i32);
        let x = rect[0].clamp(0, w);
        let y = rect[1].clamp(0, h);
        let r = (rect[0] + rect[2]).clamp(0, w);
        let b = (rect[1] + rect[3]).clamp(0, h);
        if r - x < 2 || b - y < 2 {
            return;
        }
        let img = image::imageops::crop_imm(self.shot.as_ref(), x as u32, y as u32, (r - x) as u32, (b - y) as u32)
            .to_image();
        let cap = Capture::new(img, self.dpi, title);
        let global = [x + self.origin.0, y + self.origin.1, r - x, b - y];
        let mode = self.mode;
        cx.defer(move |cx| {
            close_all(cx);
            let s = cx.global_mut::<Settings>();
            s.last_area = Some(global);
            s.save();
            match mode {
                Mode::Image => flow::finish(cap, cx),
                Mode::Text => flow::finish_text(cap, cx),
            }
        });
    }
}

fn dim(x: f32, y: f32, w: f32, h: f32) -> gpui::Div {
    div()
        .absolute()
        .left(px(x))
        .top(px(y))
        .w(px(w.max(0.)))
        .h(px(h.max(0.)))
        .bg(rgba(0x00000073))
}

impl Render for Overlay {
    fn render(&mut self, window: &mut Window, _cx: &mut Context<Self>) -> impl IntoElement {
        let s = window.scale_factor();
        let vw = f32::from(window.viewport_size().width);
        let vh = f32::from(window.viewport_size().height);

        // The highlighted region: selection, or the hovered window.
        let focus_rect = self.selection.or_else(|| {
            self.hover.map(|i| {
                let r = self.windows[i].rect;
                [r[0] as f32, r[1] as f32, r[2] as f32, r[3] as f32]
            })
        });

        let mut root = div()
            .id("overlay")
            .track_focus(&self.focus)
            .size_full()
            .relative()
            .font_family(FONT)
            .cursor(CursorStyle::Crosshair)
            .on_mouse_down(MouseButton::Left, _cx.listener(Self::on_down))
            .on_mouse_move(_cx.listener(Self::on_move))
            .on_mouse_up(MouseButton::Left, _cx.listener(Self::on_up))
            .on_key_down(_cx.listener(Self::on_key))
            .child(
                img(ImageSource::Render(self.image.clone()))
                    .absolute()
                    .top_0()
                    .left_0()
                    .size_full()
                    .object_fit(ObjectFit::Fill),
            );

        match focus_rect {
            Some([x, y, w, h]) => {
                let (x, y, w, h) = (x / s, y / s, w / s, h / s);
                let is_sel = self.selection.is_some();
                root = root
                    .child(dim(0., 0., vw, y))
                    .child(dim(0., y + h, vw, vh - y - h))
                    .child(dim(0., y, x, h))
                    .child(dim(x + w, y, vw - x - w, h))
                    .child(
                        div()
                            .absolute()
                            .left(px(x - 1.))
                            .top(px(y - 1.))
                            .w(px(w + 2.))
                            .h(px(h + 2.))
                            .border_2()
                            .border_color(if is_sel { rgb(ACCENT) } else { rgb(0xFFFFFF) })
                            .when(!is_sel, |d| d.bg(rgba(0x7C8CFF1A))),
                    );
                let label = match (is_sel, self.hover) {
                    (false, Some(i)) => {
                        let t = &self.windows[i].title;
                        let t: String = t.chars().take(48).collect();
                        format!("{t}  ·  {} × {}", (w * s) as i32, (h * s) as i32)
                    }
                    _ => format!("{} × {}", (w * s) as i32, (h * s) as i32),
                };
                let ly = if y + h + 34. < vh { y + h + 8. } else { (y - 30.).max(4.) };
                root = root.child(
                    div()
                        .absolute()
                        .left(px(x))
                        .top(px(ly))
                        .px_2()
                        .py_1()
                        .rounded_md()
                        .bg(rgba(0x000000CC))
                        .text_xs()
                        .text_color(rgb(0xFFFFFF))
                        .child(label),
                );
            }
            None => root = root.child(dim(0., 0., vw, vh).bg(rgba(0x00000040))),
        }

        // Hint bar.
        let hint = match self.mode {
            Mode::Image => "Drag to capture an area  ·  Click a window  ·  Space for full screen  ·  Esc to cancel",
            Mode::Text => "Select text to copy  ·  Esc to cancel",
        };
        root = root.child(
            div().absolute().top(px(24.)).left_0().w_full().flex().justify_center().child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .px_4()
                    .py_2()
                    .rounded_full()
                    .bg(rgba(0x18181BE6))
                    .border_1()
                    .border_color(rgba(0xFFFFFF1F))
                    .text_sm()
                    .text_color(rgb(0xEDEDED))
                    .child(ui::icon(if self.mode == Mode::Text { "scan" } else { "area" }, 15., rgb(ACCENT)))
                    .child(hint),
            ),
        );

        // Magnifier.
        if let (Some((cxp, cyp)), Some((loupe, _, c))) = (self.cursor, &self.loupe) {
            let (lx, ly) = (cxp / s, cyp / s);
            let side = 120.;
            let mut bx = lx + 24.;
            let mut by = ly + 24.;
            if bx + side > vw - 8. {
                bx = lx - side - 24.;
            }
            if by + side + 40. > vh - 8. {
                by = ly - side - 64.;
            }
            root = root.child(
                div()
                    .absolute()
                    .left(px(bx))
                    .top(px(by))
                    .flex()
                    .flex_col()
                    .rounded_lg()
                    .overflow_hidden()
                    .border_2()
                    .border_color(rgba(0xFFFFFFE6))
                    .shadow_lg()
                    .child(img(ImageSource::Render(loupe.clone())).w(px(side)).h(px(side)))
                    .child(
                        div()
                            .w(px(side))
                            .px_2()
                            .py_1()
                            .flex()
                            .justify_between()
                            .bg(rgba(0x000000D9))
                            .text_xs()
                            .text_color(rgb(0xFFFFFF))
                            .child(format!("#{:02X}{:02X}{:02X}", c[0], c[1], c[2]))
                            .child(format!("{}, {}", cxp as i32, cyp as i32)),
                    ),
            );
        }
        root
    }
}
