//! The recording picker: one floating bar to choose the source, camera, microphone, system
//! audio, start timer and teleprompter script, then start. Choices are remembered.

use super::chrome::{self, Kind, Placement};
use super::target::work_area;
use super::{prefs, rec, update_prefs, Rec, Stage, Target};
use crate::settings::RecordTarget;
use crate::ui::{self, FONT, MUTED, TEXT};
use crate::{toast, win};
use glint_media::{AudioDevice, CameraDevice, CameraPreview, DeviceId, DisplayInfo};
use gpui::prelude::*;
use gpui::{
    div, img, px, rgb, rgba, AnyWindowHandle, App, ClickEvent, Context, FocusHandle, FontWeight, ImageSource, KeyDownEvent,
    ObjectFit, RenderImage, SharedString, Window, WindowHandle,
};
use image::RgbaImage;
use std::sync::{Arc, Mutex};

const BAR_W: f32 = 820.;
const BAR_H: f32 = 56.;
const GAP: f32 = 8.;
const MARGIN: f32 = 28.;
const ROW_H: f32 = 30.;
const SCRIPT_H: f32 = 262.;
const COUNTDOWNS: [u32; 4] = [0, 1, 3, 5];

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Panel {
    Camera,
    Mic,
    Timer,
    Script,
}

pub struct Picker {
    panel: Option<Panel>,
    mics: Vec<AudioDevice>,
    cameras: Vec<CameraDevice>,
    focus: FocusHandle,
    display: DisplayInfo,
}

fn display_for_picker(cx: &mut App) -> Option<DisplayInfo> {
    match rec(cx).target.as_ref() {
        Some(t) => Some(t.display().clone()),
        None => Target::display_at_cursor().ok().map(|t| t.display().clone()),
    }
}

pub fn open(cx: &mut App) {
    let old = rec(cx).windows.picker.take();
    chrome::close(old, cx);
    let Some(display) = display_for_picker(cx) else { return };
    let rect = rect_for(&display, None, 0, 0);
    let placement = Placement { rect, scale: display.scale, kind: Kind::Focused };
    let Some(handle) = chrome::open(placement, cx, move |window, cx| {
        let focus = cx.focus_handle();
        window.focus(&focus);
        Picker { panel: None, mics: Vec::new(), cameras: Vec::new(), focus, display }
    }) else {
        return;
    };
    rec(cx).windows.picker = Some(handle.into());
    chrome::tick(handle.into(), 50, cx);
    load_devices(handle, cx);
}

fn load_devices(handle: WindowHandle<Picker>, cx: &mut App) {
    let task = cx.background_spawn(async move { (glint_media::microphones(), glint_media::cameras()) });
    cx.spawn(async move |cx| {
        let (mics, cameras) = task.await;
        let _ = handle.update(cx, |this, window, cx| {
            this.mics = mics.unwrap_or_default();
            this.cameras = cameras.unwrap_or_default();
            this.relayout(window, cx);
            cx.notify();
        });
    })
    .detach();
}

/// Physical rectangle of the picker: centered at the bottom of the display, growing upward
/// to make room for an open panel.
fn rect_for(display: &DisplayInfo, panel: Option<Panel>, mics: usize, cameras: usize) -> [i32; 4] {
    let panel_h = match panel {
        None => 0.,
        Some(Panel::Camera) => GAP + 16. + ROW_H * (cameras + 1) as f32,
        Some(Panel::Mic) => GAP + 16. + ROW_H * (mics + 1) as f32,
        Some(Panel::Timer) => GAP + 16. + ROW_H,
        Some(Panel::Script) => GAP + SCRIPT_H,
    };
    let s = display.scale;
    let area = work_area(display);
    let (pw, ph) = ((BAR_W * s).round() as i32, ((BAR_H + panel_h) * s).round() as i32);
    [area[0] + (area[2] - pw) / 2, area[1] + area[3] - ph - (MARGIN * s) as i32, pw, ph]
}

/// Runs a lifecycle step after this event; it may close this very window.
fn later(f: impl Fn(&mut App) + 'static) -> impl Fn(&ClickEvent, &mut Window, &mut App) + 'static {
    let f = std::rc::Rc::new(f);
    move |_, _, cx| {
        let f = f.clone();
        cx.defer(move |cx| f(cx))
    }
}

impl Picker {
    fn relayout(&self, window: &Window, cx: &App) {
        chrome::place(window, rect_for(&self.display, self.panel, self.mics.len(), self.cameras.len()), cx);
    }

    fn toggle_panel(&mut self, panel: Panel, window: &mut Window, cx: &mut Context<Self>) {
        self.panel = if self.panel == Some(panel) { None } else { Some(panel) };
        self.relayout(window, cx);
        cx.notify();
    }

    fn on_key(&mut self, e: &KeyDownEvent, window: &mut Window, cx: &mut Context<Self>) {
        let key = e.keystroke.key.as_str();
        if key == "escape" {
            if super::countdown_left(cx).is_some() {
                cx.defer(super::cancel_countdown);
            } else if self.panel.is_some() {
                self.panel = None;
                self.relayout(window, cx);
                cx.notify();
            } else {
                cx.defer(super::close_picker);
            }
            return;
        }
        if self.panel == Some(Panel::Script) {
            let mut script = prefs(cx).script;
            if edit_script(&mut script, e) {
                update_prefs(cx, |p| p.script = script);
                cx.notify();
            }
            return;
        }
        if key == "enter" && super::countdown_left(cx).is_none() {
            cx.defer(super::record);
        }
    }
}

/// Applies one key press to the script. The caret stays at the end.
fn edit_script(text: &mut String, e: &KeyDownEvent) -> bool {
    let k = &e.keystroke;
    let ctrl = k.modifiers.control;
    match k.key.as_str() {
        "enter" => text.push('\n'),
        "backspace" if ctrl => {
            let trimmed = text.trim_end().len();
            let cut = text[..trimmed].rfind(char::is_whitespace).map(|i| i + 1).unwrap_or(0);
            text.truncate(cut);
        }
        "backspace" => {
            text.pop();
        }
        "v" if ctrl => match arboard::Clipboard::new().and_then(|mut c| c.get_text()) {
            Ok(t) => text.push_str(&t.replace("\r\n", "\n")),
            Err(_) => return false,
        },
        _ if ctrl || k.modifiers.alt => return false,
        "space" => text.push(' '),
        _ => match &k.key_char {
            Some(c) if !c.chars().any(char::is_control) => text.push_str(c),
            _ => return false,
        },
    }
    true
}

fn chip(id: impl Into<SharedString>, active: bool, enabled: bool) -> gpui::Stateful<gpui::Div> {
    div()
        .id(gpui::ElementId::Name(id.into()))
        .h(px(36.))
        .px_2p5()
        .flex()
        .flex_none()
        .items_center()
        .gap_1p5()
        .rounded(px(ui::RADIUS_CONTROL))
        .text_sm()
        .text_color(rgb(TEXT))
        .when(active, |d| d.bg(ui::accent_alpha(0x40)))
        .when(enabled, |d| d.cursor_pointer().hover(|s| s.bg(rgba(0xFFFFFF1A))))
        .when(!enabled, |d| d.opacity(0.4))
}

fn separator() -> impl IntoElement {
    div().w(px(1.)).h(px(26.)).mx_1().bg(rgba(0xFFFFFF26))
}

/// Meter fill for an RMS level, on a -60..0 dB scale.
fn meter_fraction(rms: f32) -> f32 {
    ((20. * rms.max(1e-6).log10() + 60.) / 60.).clamp(0., 1.)
}

fn row(id: impl Into<SharedString>, label: impl Into<SharedString>, selected: bool) -> gpui::Stateful<gpui::Div> {
    div()
        .id(gpui::ElementId::Name(id.into()))
        .h(px(ROW_H))
        .px_2()
        .flex()
        .items_center()
        .gap_2()
        .rounded(px(ui::RADIUS_CONTROL))
        .cursor_pointer()
        .text_sm()
        .text_color(rgb(TEXT))
        .hover(|s| s.bg(rgba(0xFFFFFF1A)))
        .child(div().w(px(16.)).child(if selected { ui::icon("check", 14., ui::accent()).into_any_element() } else { div().into_any_element() }))
        .child(div().overflow_hidden().whitespace_nowrap().text_ellipsis().child(label.into()))
}

fn surface() -> gpui::Div {
    div().rounded(px(ui::RADIUS_WINDOW)).bg(rgba(0x1C1C1FF5)).border_1().border_color(rgba(0xFFFFFF26)).font_family(FONT)
}

impl Picker {
    fn panel_view(&self, panel: Panel, cx: &mut Context<Self>) -> gpui::AnyElement {
        let p = prefs(cx);
        let list = surface().p_2().flex().flex_col();
        match panel {
            Panel::Camera => list
                .child(row("cam-none", "No camera", p.camera.is_none()).on_click(cx.listener(|this, _, w, cx| this.set_camera(None, w, cx))))
                .children(self.cameras.iter().map(|c| {
                    let id = c.id.0.clone();
                    row(format!("cam-{}", c.id.0), c.name.clone(), p.camera.as_deref() == Some(&c.id.0))
                        .on_click(cx.listener(move |this, _, w, cx| this.set_camera(Some(id.clone()), w, cx)))
                }))
                .into_any_element(),
            Panel::Mic => list
                .child(row("mic-none", "No microphone", p.mic.is_none()).on_click(cx.listener(|this, _, w, cx| this.set_mic(None, w, cx))))
                .children(self.mics.iter().map(|m| {
                    let id = m.id.0.clone();
                    row(format!("mic-{}", m.id.0), m.name.clone(), p.mic.as_deref() == Some(&m.id.0))
                        .on_click(cx.listener(move |this, _, w, cx| this.set_mic(Some(id.clone()), w, cx)))
                }))
                .into_any_element(),
            Panel::Timer => list
                .child(div().h(px(ROW_H)).flex().items_center().gap_2().children(COUNTDOWNS.map(|n| {
                    ui::chip(SharedString::from(format!("timer-{n}")), if n == 0 { "Off".to_string() } else { format!("{n} s") }, p.countdown == n, cx.listener(move |this, _, w, cx| {
                        update_prefs(cx, |p| p.countdown = n);
                        this.panel = None;
                        this.relayout(w, cx);
                        cx.notify();
                    }))
                })))
                .into_any_element(),
            Panel::Script => self.script_view(cx).into_any_element(),
        }
    }

    fn script_view(&self, cx: &mut Context<Self>) -> impl IntoElement {
        let p = prefs(cx);
        let empty = p.script.is_empty();
        let paragraphs: Vec<String> = p.script.split('\n').map(str::to_string).collect();
        let last = paragraphs.len() - 1;
        surface()
            .h(px(SCRIPT_H))
            .p_3()
            .flex()
            .flex_col()
            .gap_2()
            .child(
                div()
                    .flex()
                    .items_center()
                    .justify_between()
                    .child(div().text_sm().font_weight(FontWeight::SEMIBOLD).text_color(rgb(TEXT)).child("Teleprompter"))
                    .child(div().w(px(220.)).child(ui::toggle("prompter-on", "Show while recording", p.teleprompter, |_, _, cx| {
                        update_prefs(cx, |p| p.teleprompter = !p.teleprompter);
                    }))),
            )
            .child(
                div()
                    .id("script")
                    .h(px(150.))
                    .p_2()
                    .flex()
                    .flex_col()
                    .justify_end()
                    .overflow_hidden()
                    .rounded(px(ui::RADIUS_CONTROL))
                    .bg(rgb(0x101012))
                    .border_1()
                    .border_color(ui::accent())
                    .text_sm()
                    .text_color(rgb(TEXT))
                    .when(empty, |d| d.justify_start().child(div().text_color(rgb(MUTED)).child("Type or paste your script (Ctrl+V)")))
                    .children(paragraphs.into_iter().enumerate().map(move |(i, line)| {
                        div()
                            .min_h(px(18.))
                            .child(line)
                            .when(i == last, |d| d.child(div().ml(px(1.)).w(px(1.5)).h(px(15.)).bg(ui::accent())))
                    })),
            )
            .child(
                div()
                    .flex()
                    .items_center()
                    .gap_2()
                    .text_xs()
                    .text_color(rgb(MUTED))
                    .child("Lines")
                    .children((1..=5u32).map(|n| {
                        ui::chip(SharedString::from(format!("lines-{n}")), n.to_string(), p.prompter_lines == n, move |_, _, cx| update_prefs(cx, |p| p.prompter_lines = n))
                    }))
                    .child(div().flex_1().child("Follows your voice when a microphone is on"))
                    .child(ui::button("script-clear", "Clear", None, false, |_, _, cx| update_prefs(cx, |p| p.script.clear()))),
            )
    }

    fn set_mic(&mut self, id: Option<String>, window: &mut Window, cx: &mut Context<Self>) {
        update_prefs(cx, |p| p.mic = id);
        super::restart_monitor(cx);
        self.panel = None;
        self.relayout(window, cx);
        cx.notify();
    }

    fn set_camera(&mut self, id: Option<String>, window: &mut Window, cx: &mut Context<Self>) {
        update_prefs(cx, |p| p.camera = id);
        cx.defer(super::camera_bubble);
        self.panel = None;
        self.relayout(window, cx);
        cx.notify();
    }
}

impl Render for Picker {
    fn render(&mut self, window: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        // Clicking a control must not leave Esc and typing without a target.
        if !self.focus.is_focused(window) {
            window.focus(&self.focus);
        }
        let p = prefs(cx);
        let counting = super::countdown_left(cx).is_some();
        let (kind, level) = {
            let r = cx.try_global::<Rec>();
            let kind = match r.and_then(|r| r.target.as_ref()) {
                Some(Target::Display(_)) => RecordTarget::Display,
                Some(Target::Window { .. }) => RecordTarget::Window,
                Some(Target::Area { .. }) => RecordTarget::Area,
                None => p.target,
            };
            (kind, r.and_then(|r| r.monitor.as_ref()).map(|m| m.level()))
        };
        let has_target = cx.try_global::<Rec>().is_some_and(|r| r.target.is_some() && matches!(r.stage, Stage::Picking | Stage::Countdown { .. }));

        let source = |id: &'static str, k: RecordTarget, icon: &'static str, label: &'static str| {
            chip(id, kind == k, !counting)
                .child(ui::icon(icon, 16., rgb(TEXT)))
                .child(label)
                .when(!counting, |d| d.on_click(later(move |cx| super::choose(k, cx))))
        };
        let camera_name = p.camera.as_ref().map(|id| {
            self.cameras.iter().find(|c| &c.id.0 == id).map(|c| c.name.clone()).unwrap_or_else(|| "Camera".into())
        });
        let mic_on = p.mic.is_some();

        let bar = surface()
            .h(px(BAR_H))
            .w_full()
            .flex_none()
            .px_2()
            .flex()
            .items_center()
            .gap_1()
            .child(source("src-display", RecordTarget::Display, "monitor", "Display"))
            .child(source("src-window", RecordTarget::Window, "window", "Window"))
            .child(source("src-area", RecordTarget::Area, "area", "Area"))
            .child(separator())
            .child(
                chip("camera", self.panel == Some(Panel::Camera), true)
                    .max_w(px(150.))
                    .child(ui::icon(if camera_name.is_some() { "camera" } else { "camera-off" }, 16., rgb(TEXT)))
                    .child(div().overflow_hidden().whitespace_nowrap().text_ellipsis().child(camera_name.unwrap_or_else(|| "No camera".into())))
                    .tooltip(ui::tooltip("Camera"))
                    .on_click(cx.listener(|this, _, w, cx| this.toggle_panel(Panel::Camera, w, cx))),
            )
            .child(
                chip("mic", self.panel == Some(Panel::Mic), true)
                    .child(ui::icon(if mic_on { "mic" } else { "mic-off" }, 16., rgb(TEXT)))
                    .child(
                        div().w(px(48.)).h(px(6.)).rounded_full().bg(rgba(0xFFFFFF1F)).overflow_hidden().child(
                            div().h_full().rounded_full().bg(rgb(0x34C759)).w(px(48. * level.map(meter_fraction).unwrap_or(0.))),
                        ),
                    )
                    .tooltip(ui::tooltip(if mic_on { "Microphone" } else { "No microphone" }))
                    .on_click(cx.listener(|this, _, w, cx| this.toggle_panel(Panel::Mic, w, cx))),
            )
            .child(
                chip("system-audio", p.system_audio, true)
                    .child(ui::icon(if p.system_audio { "speaker" } else { "speaker-off" }, 16., rgb(TEXT)))
                    .tooltip(ui::tooltip(if p.system_audio { "Recording system audio" } else { "System audio off" }))
                    .on_click(|_, _, cx| update_prefs(cx, |p| p.system_audio = !p.system_audio)),
            )
            .child(
                chip("timer", self.panel == Some(Panel::Timer), true)
                    .child(ui::icon("timer", 16., rgb(TEXT)))
                    .child(if p.countdown == 0 { "Off".to_string() } else { format!("{} s", p.countdown) })
                    .tooltip(ui::tooltip("Start timer"))
                    .on_click(cx.listener(|this, _, w, cx| this.toggle_panel(Panel::Timer, w, cx))),
            )
            .child(
                chip("prompter", p.teleprompter || self.panel == Some(Panel::Script), true)
                    .child(ui::icon("prompter", 16., rgb(TEXT)))
                    .tooltip(ui::tooltip("Teleprompter"))
                    .on_click(cx.listener(|this, _, w, cx| this.toggle_panel(Panel::Script, w, cx))),
            )
            .child(div().flex_1())
            .child(if counting {
                ui::button("cancel", "Cancel", None, false, later(super::cancel_countdown)).h(px(34.)).into_any_element()
            } else {
                div()
                    .id("record")
                    .h(px(34.))
                    .px_3()
                    .flex()
                    .items_center()
                    .gap_1p5()
                    .rounded(px(ui::RADIUS_CONTROL))
                    .cursor_pointer()
                    .bg(rgb(0xC42B1C))
                    .hover(|s| s.bg(rgb(0xD83B2C)))
                    .text_sm()
                    .font_weight(FontWeight::SEMIBOLD)
                    .text_color(rgb(0xFFFFFF))
                    .child(ui::icon("record", 15., rgb(0xFFFFFF)))
                    .child(if has_target { "Record" } else { "Choose…" })
                    .on_click(later(super::record))
                    .into_any_element()
            })
            .child(
                div()
                    .id("close")
                    .size(px(32.))
                    .flex()
                    .items_center()
                    .justify_center()
                    .rounded(px(ui::RADIUS_CONTROL))
                    .cursor_pointer()
                    .hover(|s| s.bg(rgba(0xFFFFFF1A)))
                    .child(ui::icon("close", 15., rgb(TEXT)))
                    .tooltip(ui::tooltip("Close (Esc)"))
                    .on_click(later(super::close_picker)),
            );

        div()
            .id("picker")
            .track_focus(&self.focus)
            .on_key_down(cx.listener(Self::on_key))
            .size_full()
            .flex()
            .flex_col()
            .justify_end()
            .gap(px(GAP))
            .when_some(self.panel, |d, panel| d.child(self.panel_view(panel, cx)))
            .child(bar)
    }
}

/// A round live preview of the chosen camera at the bottom right of the display, so exposure
/// and framing settle before recording starts.
pub fn open_camera(id: DeviceId, display: &DisplayInfo, cx: &mut App) -> Option<AnyWindowHandle> {
    let latest: Arc<Mutex<Option<RgbaImage>>> = Arc::default();
    let sink = latest.clone();
    let preview = match CameraPreview::start(&id, move |frame| *sink.lock().unwrap() = Some(frame)) {
        Ok(p) => p,
        Err(e) => {
            toast::show(format!("Camera preview unavailable: {e:#}"), cx);
            return None;
        }
    };
    let side = 200.;
    let s = display.scale;
    let area = work_area(display);
    let ps = (side * s).round() as i32;
    let margin = (MARGIN * s) as i32;
    let rect = [area[0] + area[2] - ps - margin, area[1] + area[3] - ps - (BAR_H * s) as i32 - margin * 2, ps, ps];
    let placement = Placement { rect, scale: s, kind: Kind::Passive };
    let handle = chrome::open(placement, cx, move |_, _| CameraBubble { latest, image: None, _preview: preview })?;
    chrome::tick(handle.into(), 33, cx);
    Some(handle.into())
}

struct CameraBubble {
    latest: Arc<Mutex<Option<RgbaImage>>>,
    image: Option<Arc<RenderImage>>,
    _preview: CameraPreview,
}

impl Render for CameraBubble {
    fn render(&mut self, window: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        if let Some(frame) = self.latest.lock().unwrap().take() {
            if let Some(old) = self.image.replace(win::render_image(&frame)) {
                let _ = window.drop_image(old);
            }
        }
        div()
            .size_full()
            .rounded_full()
            .overflow_hidden()
            .bg(rgb(0x101012))
            .border_2()
            .border_color(rgba(0xFFFFFF40))
            .when_some(self.image.clone(), |d, image| d.child(img(ImageSource::Render(image)).size_full().object_fit(ObjectFit::Cover)))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn meter_maps_rms_to_a_decibel_scale() {
        assert_eq!(meter_fraction(0.0), 0.0);
        assert_eq!(meter_fraction(1.0), 1.0);
        assert!((meter_fraction(0.01) - 1. / 3.).abs() < 1e-4, "-40 dB is a third of the way");
    }
}
