//! The Settings window: General, Screenshots, Recordings, Overlay, Shortcuts and Privacy.

use crate::cards::{CardAction, CardLayout, Corner, Zone};
use crate::hotkeys::{self, Hotkeys};
use crate::library::Library;
use crate::settings::{ExportFormat, HotkeyAction, RedactRules, RedactStyle, Settings, Side};
use crate::ui::{self, FONT};
use crate::{shell, stack, win};
use gpui::prelude::*;
use gpui::{
    div, px, rgb, rgba, size, App, Bounds, Context, FocusHandle, Global, KeyDownEvent, PathPromptOptions, SharedString,
    TitlebarOptions, Window, WindowBounds, WindowHandle, WindowOptions,
};
use std::rc::Rc;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum Pane {
    General,
    Screenshots,
    Recordings,
    Overlay,
    Shortcuts,
    Privacy,
}

impl Pane {
    const ALL: [Pane; 6] = [Pane::General, Pane::Screenshots, Pane::Recordings, Pane::Overlay, Pane::Shortcuts, Pane::Privacy];

    fn label(self) -> &'static str {
        match self {
            Pane::General => "General",
            Pane::Screenshots => "Screenshots",
            Pane::Recordings => "Recordings",
            Pane::Overlay => "Overlay",
            Pane::Shortcuts => "Shortcuts",
            Pane::Privacy => "Privacy",
        }
    }

    fn icon(self) -> &'static str {
        match self {
            Pane::General => "settings",
            Pane::Screenshots => "camera",
            Pane::Recordings => "film",
            Pane::Overlay => "stack",
            Pane::Shortcuts => "keyboard",
            Pane::Privacy => "shield",
        }
    }
}

#[derive(Default)]
struct PrefsState(Option<WindowHandle<Prefs>>);

impl Global for PrefsState {}

pub struct Prefs {
    pane: Pane,
    recording: Option<HotkeyAction>,
    shortcut_note: Option<(HotkeyAction, Result<String, String>)>,
    focus: FocusHandle,
}

/// The dragged payload in the card editor.
#[derive(Clone, Copy)]
struct DragAction(CardAction);

struct DragGhost(CardAction);

impl Render for DragGhost {
    fn render(&mut self, _: &mut Window, _: &mut Context<Self>) -> impl IntoElement {
        action_chip(self.0).bg(ui::accent()).font_family(FONT).shadow_lg()
    }
}

pub fn show(cx: &mut App) {
    open(None, cx);
}

fn open(pane: Option<Pane>, cx: &mut App) {
    if let Some(h) = cx.default_global::<PrefsState>().0 {
        let ok = h.update(cx, |this, window, cx| {
            if let Some(p) = pane {
                this.pane = p;
            }
            window.activate_window();
            win::bring_to_front(window);
            cx.notify();
        });
        if ok.is_ok() {
            return;
        }
    }
    let bounds = Bounds::centered(None, size(px(860.), px(640.)), cx);
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions { title: Some("Glint Settings".into()), appears_transparent: false, traffic_light_position: None }),
            window_min_size: Some(size(px(720.), px(520.))),
            focus: true,
            window_background: gpui::WindowBackgroundAppearance::Blurred,
            ..Default::default()
        },
        move |window, cx| {
            shell::mica(window);
            let view = cx.new(|cx| {
                // Closing the window mid-capture must give back the shortcuts Change suspended.
                cx.on_release(|this: &mut Prefs, cx| {
                    if this.recording.is_some() {
                        resume_hotkeys(cx);
                    }
                })
                .detach();
                Prefs { pane: pane.unwrap_or(Pane::General), recording: None, shortcut_note: None, focus: cx.focus_handle() }
            });
            let f = view.read(cx).focus.clone();
            window.focus(&f);
            win::bring_to_front(window);
            view
        },
    );
    cx.default_global::<PrefsState>().0 = handle.ok();
}

fn resume_hotkeys(cx: &mut App) {
    let s = cx.global::<Settings>().clone();
    cx.global_mut::<Hotkeys>().register_all(&s);
}

fn update(cx: &mut App, f: impl FnOnce(&mut Settings)) {
    let s = cx.global_mut::<Settings>();
    f(s);
    s.save();
    cx.refresh_windows();
}

fn toggle(id: &'static str, label: &'static str, on: bool, f: fn(&mut Settings)) -> impl IntoElement {
    ui::toggle(id, label, on, move |_, _, cx| update(cx, f))
}

fn group(title: &str) -> gpui::Div {
    div()
        .flex()
        .flex_col()
        .gap_3()
        .p_4()
        .rounded(px(ui::RADIUS_WINDOW))
        .bg(ui::theme().layer)
        .border_1()
        .border_color(ui::theme().border)
        .child(div().text_xs().font_weight(gpui::FontWeight::SEMIBOLD).text_color(ui::theme().muted).child(title.to_uppercase()))
}

fn note(text: impl Into<SharedString>) -> impl IntoElement {
    div().text_xs().text_color(ui::theme().muted).child(text.into())
}

fn labeled(label: &'static str, control: impl IntoElement) -> impl IntoElement {
    div().flex().items_center().justify_between().gap_3().child(div().text_sm().child(label)).child(control)
}

fn action_chip(a: CardAction) -> gpui::Div {
    div()
        .h(px(26.))
        .px_2()
        .flex()
        .items_center()
        .gap_1p5()
        .rounded_full()
        .bg(ui::theme().control)
        .border_1()
        .border_color(ui::theme().border)
        .text_xs()
        .text_color(ui::theme().text)
        .child(ui::icon(a.icon(), 13., ui::theme().text))
        .child(a.label())
}

impl Prefs {
    fn place(&mut self, a: CardAction, zone: Zone, index: Option<usize>, cx: &mut Context<Self>) {
        update(cx, |s| s.overlay.layout.place(a, zone, index));
        stack::refresh(cx);
    }

    fn draggable(&self, a: CardAction, key: &str, cx: &mut Context<Self>) -> impl IntoElement {
        action_chip(a)
            .id(SharedString::from(format!("drag-{key}-{a:?}")))
            .cursor_grab()
            .hover(|s| s.border_color(ui::accent()))
            .tooltip(ui::tooltip(a.help()))
            .on_drag(DragAction(a), |d, _, _, cx| cx.new(|_| DragGhost(d.0)))
            .drag_over::<DragAction>(|s, _, _, _| s.border_color(ui::accent()))
            .when(key == "center", |d| {
                d.on_drop(cx.listener(move |this, dragged: &DragAction, _, cx| {
                    let layout = &cx.global::<Settings>().overlay.layout;
                    let i = layout.center.iter().position(|&x| x == a);
                    this.place(dragged.0, Zone::Center, i, cx);
                }))
            })
    }

    fn corner_slot(&self, layout: &CardLayout, corner: Corner, cx: &mut Context<Self>) -> impl IntoElement {
        let slot = div()
            .id(SharedString::from(format!("slot-{corner:?}")))
            .absolute()
            .min_w(px(44.))
            .h(px(34.))
            .p_1()
            .flex()
            .items_center()
            .justify_center()
            .rounded_full()
            .border_1()
            .border_dashed()
            .border_color(rgba(0xFFFFFF55))
            .bg(rgba(0x00000066))
            .drag_over::<DragAction>(|s, _, _, _| s.bg(ui::accent_alpha(0x66)).border_color(ui::accent()))
            .on_drop(cx.listener(move |this, d: &DragAction, _, cx| this.place(d.0, Zone::Corner(corner), None, cx)));
        let slot = match corner {
            Corner::TopLeft => slot.top(px(10.)).left(px(10.)),
            Corner::TopRight => slot.top(px(10.)).right(px(10.)),
            Corner::BottomLeft => slot.bottom(px(10.)).left(px(10.)),
            Corner::BottomRight => slot.bottom(px(10.)).right(px(10.)),
        };
        match layout.corner(corner) {
            Some(a) => slot.child(self.draggable(a, "corner", cx)),
            None => slot.child(div().text_xs().text_color(ui::theme().muted).child("Empty")),
        }
    }

    fn overlay_pane(&mut self, s: &Settings, cx: &mut Context<Self>) -> gpui::Div {
        let o = &s.overlay;
        let layout = o.layout.clone();
        let close_chip = |secs: u32, label: &'static str| {
            ui::chip(SharedString::from(format!("ac-{secs}")), label, o.auto_close_seconds == secs, move |_, _, cx| {
                update(cx, |s| s.overlay.auto_close_seconds = secs)
            })
        };
        let card = div()
            .relative()
            .w(px(380.))
            .h(px(250.))
            .rounded(px(ui::RADIUS_WINDOW))
            .border_1()
            .border_color(ui::theme().border)
            .bg(gpui::linear_gradient(135., gpui::linear_color_stop(rgb(0x3B3F7A), 0.), gpui::linear_color_stop(rgb(0x7A3B66), 1.)))
            .child(
                div()
                    .id("center-zone")
                    .absolute()
                    .top(px(62.))
                    .bottom(px(62.))
                    .left(px(96.))
                    .right(px(96.))
                    .flex()
                    .flex_col()
                    .items_center()
                    .justify_center()
                    .gap_1p5()
                    .rounded(px(ui::RADIUS_WINDOW))
                    .border_1()
                    .border_dashed()
                    .border_color(rgba(0xFFFFFF55))
                    .bg(rgba(0x00000040))
                    .drag_over::<DragAction>(|s, _, _, _| s.bg(ui::accent_alpha(0x55)).border_color(ui::accent()))
                    .on_drop(cx.listener(|this, d: &DragAction, _, cx| this.place(d.0, Zone::Center, None, cx)))
                    .children(layout.center.iter().map(|&a| self.draggable(a, "center", cx).into_any_element()))
                    .when(layout.center.is_empty(), |d| d.child(note("Drop up to three buttons here"))),
            )
            .children(Corner::ALL.map(|c| self.corner_slot(&layout, c, cx).into_any_element()));
        let tray = div()
            .id("tray")
            .min_h(px(48.))
            .p_2()
            .flex()
            .flex_wrap()
            .gap_2()
            .rounded(px(ui::RADIUS_WINDOW))
            .border_1()
            .border_dashed()
            .border_color(ui::theme().border)
            .bg(ui::theme().layer)
            .drag_over::<DragAction>(|s, _, _, _| s.bg(ui::accent_alpha(0x33)).border_color(ui::accent()))
            .on_drop(cx.listener(|this, d: &DragAction, _, cx| this.place(d.0, Zone::Tray, None, cx)))
            .children(layout.hidden.iter().map(|&a| self.draggable(a, "tray", cx).into_any_element()))
            .when(layout.hidden.is_empty(), |d| d.child(note("Drag actions here to hide them")));

        div()
            .flex()
            .flex_col()
            .gap_4()
            .child(
                group("Preview stack")
                    .child(labeled(
                        "Position",
                        div()
                            .flex()
                            .gap_2()
                            .child(ui::chip("side-l", "Left", o.side == Side::Left, |_, _, cx| {
                                update(cx, |s| s.overlay.side = Side::Left);
                                stack::refresh(cx);
                            }))
                            .child(ui::chip("side-r", "Right", o.side == Side::Right, |_, _, cx| {
                                update(cx, |s| s.overlay.side = Side::Right);
                                stack::refresh(cx);
                            })),
                    ))
                    .child(labeled(
                        "Close automatically",
                        div()
                            .flex()
                            .gap_2()
                            .child(close_chip(0, "Never"))
                            .child(close_chip(5, "5 s"))
                            .child(close_chip(10, "10 s"))
                            .child(close_chip(30, "30 s"))
                            .child(close_chip(60, "1 min")),
                    ))
                    .child(toggle("dismiss-drag", "Dismiss a card after dragging it out", o.dismiss_after_drag, |s| {
                        s.overlay.dismiss_after_drag = !s.overlay.dismiss_after_drag
                    })),
            )
            .child(
                group("Card actions")
                    .child(note("Drag actions between the four corners, the center buttons and the hidden tray."))
                    .child(div().flex().justify_center().py_2().child(card))
                    .child(div().text_xs().text_color(ui::theme().muted).child("Hidden"))
                    .child(tray)
                    .child(div().flex().child(ui::button("layout-reset", "Reset Layout", None, false, |_, _, cx| {
                        update(cx, |s| s.overlay.layout = CardLayout::default());
                        stack::refresh(cx);
                    }))),
            )
    }

    fn shortcuts_pane(&mut self, s: &Settings, cx: &mut Context<Self>) -> gpui::Div {
        let failures = cx.global::<Hotkeys>().failures.clone();
        let rows = HotkeyAction::ALL.map(|action| {
            let recording = self.recording == Some(action);
            let accel = s.hotkeys.get(action).clone();
            let failure = failures.iter().find(|(a, _)| *a == action).map(|(_, e)| e.clone());
            let note_line = self.shortcut_note.as_ref().filter(|(a, _)| *a == action).map(|(_, r)| r.clone());
            div()
                .flex()
                .flex_col()
                .gap_1()
                .py_1p5()
                .child(
                    div()
                        .flex()
                        .items_center()
                        .gap_3()
                        .child(div().flex_1().text_sm().child(action.label()))
                        .child(if recording {
                            div()
                                .h(px(26.))
                                .px_2()
                                .flex()
                                .items_center()
                                .rounded(px(ui::RADIUS_CONTROL))
                                .border_1()
                                .border_color(ui::accent())
                                .text_xs()
                                .text_color(ui::accent())
                                .child("Press a shortcut… (Esc to cancel)")
                                .into_any_element()
                        } else {
                            ui::kbd(&hotkeys::display(&accel)).into_any_element()
                        })
                        .child(
                            ui::button(SharedString::from(format!("rec-{action:?}")), if recording { "Cancel" } else { "Change" }, None, false, cx.listener(move |this, _, window, cx| {
                                if this.recording == Some(action) {
                                    this.stop_recording(cx);
                                } else {
                                    this.recording = Some(action);
                                    this.shortcut_note = None;
                                    cx.global_mut::<Hotkeys>().suspend();
                                    window.focus(&this.focus);
                                }
                                cx.notify();
                            })),
                        ),
                )
                .when_some(failure.filter(|_| note_line.is_none()), |d, e| d.child(div().text_xs().text_color(rgb(0xFF6B61)).child(e)))
                .when_some(note_line, |d, r| {
                    d.child(match r {
                        Ok(m) => div().text_xs().text_color(rgb(0x34C759)).child(m),
                        Err(e) => div().text_xs().text_color(rgb(0xFF6B61)).child(e),
                    })
                })
        });
        div().flex().flex_col().gap_4().child(
            group("Global shortcuts")
                .child(note("Click Change, then press the new combination. If Windows refuses it, the previous shortcut keeps working."))
                .children(rows)
                .child(div().flex().child(ui::button("hk-reset", "Restore Defaults", None, false, cx.listener(|this, _, _, cx| {
                    this.shortcut_note = None;
                    let defaults = crate::settings::Hotkeys::default();
                    let mut errors = Vec::new();
                    for a in HotkeyAction::ALL {
                        if let Err(e) = hotkeys::rebind(a, defaults.get(a), cx) {
                            errors.push((a, e));
                        }
                    }
                    if let Some((a, e)) = errors.into_iter().next() {
                        this.shortcut_note = Some((a, Err(e)));
                    }
                    cx.notify();
                })))),
        )
    }

    fn stop_recording(&mut self, cx: &mut Context<Self>) {
        self.recording = None;
        resume_hotkeys(cx);
    }

    fn on_key(&mut self, e: &KeyDownEvent, _: &mut Window, cx: &mut Context<Self>) {
        let Some(action) = self.recording else { return };
        cx.stop_propagation();
        if e.keystroke.key == "escape" && !e.keystroke.modifiers.modified() {
            self.stop_recording(cx);
            cx.notify();
            return;
        }
        match hotkeys::accelerator(&e.keystroke) {
            Ok(None) => return,
            Ok(Some(accel)) => {
                self.stop_recording(cx);
                let result = hotkeys::rebind(action, &accel, cx).map(|()| format!("Now {}", hotkeys::display(&accel)));
                self.shortcut_note = Some((action, result));
            }
            Err(msg) => self.shortcut_note = Some((action, Err(msg))),
        }
        cx.notify();
    }
}

fn folder_row(id: &'static str, label: &'static str, path: std::path::PathBuf, change: Option<fn(std::path::PathBuf, &mut Settings)>) -> impl IntoElement {
    let open_path = path.clone();
    div()
        .flex()
        .items_center()
        .justify_between()
        .gap_3()
        .child(
            div()
                .flex_1()
                .flex()
                .flex_col()
                .min_w_0()
                .child(div().text_sm().child(label))
                .child(div().text_xs().text_color(ui::theme().muted).truncate().child(path.display().to_string())),
        )
        .child(
            div()
                .flex()
                .gap_2()
                .child(ui::button(SharedString::from(format!("{id}-open")), "Open", Some("folder"), false, move |_, _, cx| {
                    let _ = std::fs::create_dir_all(&open_path);
                    cx.open_with_system(&open_path);
                }))
                .when_some(change, |d, f| {
                    d.child(ui::button(SharedString::from(format!("{id}-pick")), "Change…", None, false, move |_, _, cx| {
                        let rx = cx.prompt_for_paths(PathPromptOptions { files: false, directories: true, multiple: false, prompt: Some("Choose Folder".into()) });
                        cx.spawn(async move |cx| {
                            if let Ok(Ok(Some(paths))) = rx.await {
                                if let Some(p) = paths.into_iter().next() {
                                    let _ = cx.update(|cx| update(cx, |s| f(p, s)));
                                }
                            }
                        })
                        .detach();
                    }))
                }),
        )
}

impl Render for Prefs {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let s = cx.global::<Settings>().clone();
        let nav = Pane::ALL.map(|p| {
            let active = self.pane == p;
            div()
                .id(SharedString::from(format!("pane-{p:?}")))
                .h(px(32.))
                .px_2()
                .flex()
                .items_center()
                .gap_2()
                .rounded(px(ui::RADIUS_CONTROL))
                .cursor_pointer()
                .text_sm()
                .when(active, |d| d.bg(ui::accent_alpha(0x33)).text_color(ui::theme().text))
                .when(!active, |d| d.text_color(ui::theme().text).hover(|s| s.bg(ui::theme().control_hover)))
                .child(ui::icon(p.icon(), 15., if active { ui::accent() } else { ui::theme().muted.into() }))
                .child(p.label())
                .on_click(cx.listener(move |this, _, _, cx| {
                    if this.recording.is_some() {
                        this.stop_recording(cx);
                    }
                    this.pane = p;
                    cx.notify();
                }))
        });

        let body = match self.pane {
            Pane::General => div()
                .flex()
                .flex_col()
                .gap_4()
                .child(
                    group("Startup")
                        .child(ui::toggle("login", "Launch at login", s.launch_at_login, |_, _, cx| {
                            update(cx, |s| {
                                s.launch_at_login = !s.launch_at_login;
                                win::set_launch_at_login(s.launch_at_login);
                            })
                        }))
                        .child(note("At login Glint starts quietly in the tray. Opening Glint yourself shows the Library.")),
                )
                .child(
                    group("Captures")
                        .child(ui::toggle("own", "Include Glint windows in screenshots", s.include_own_windows, |_, _, cx| {
                            update(cx, |s| s.include_own_windows = !s.include_own_windows);
                            shell::exclude_own_windows_from_capture(cx.global::<Settings>().include_own_windows);
                        }))
                        .child(note("Off by default: previews, pins and editors stay out of your captures.")),
                )
                .child(group("Library").child(folder_row("lib", "Library location", Library::default_root(), None))),
            Pane::Screenshots => {
                let timer = |secs: u32| {
                    ui::chip(SharedString::from(format!("timer-{secs}")), format!("{secs} s"), s.timer_seconds == secs, move |_, _, cx| {
                        update(cx, |s| s.timer_seconds = secs)
                    })
                };
                let quality = s.jpeg_quality;
                let a = &s.after_shot;
                div()
                    .flex()
                    .flex_col()
                    .gap_4()
                    .child(
                        group("After capture")
                            .child(note("Each screenshot is added to the Library first, then any of these run."))
                            .child(toggle("a-preview", "Show preview", a.preview, |s| s.after_shot.preview = !s.after_shot.preview))
                            .child(toggle("a-copy", "Copy to clipboard", a.copy, |s| s.after_shot.copy = !s.after_shot.copy))
                            .child(toggle("a-save", "Save to the export folder", a.save, |s| s.after_shot.save = !s.after_shot.save))
                            .child(toggle("a-edit", "Open the editor", a.edit, |s| s.after_shot.edit = !s.after_shot.edit))
                            .child(toggle("a-pin", "Pin above other windows", a.pin, |s| s.after_shot.pin = !s.after_shot.pin)),
                    )
                    .child(
                        group("Saving")
                            .child(labeled(
                                "Format",
                                div()
                                    .flex()
                                    .gap_2()
                                    .child(ui::chip("fmt-png", "PNG", s.format == ExportFormat::Png, |_, _, cx| update(cx, |s| s.format = ExportFormat::Png)))
                                    .child(ui::chip("fmt-jpg", "JPEG", s.format == ExportFormat::Jpeg, |_, _, cx| update(cx, |s| s.format = ExportFormat::Jpeg))),
                            ))
                            .child(ui::slider(
                                "jpeg-q",
                                "JPEG quality (also used by Compress)",
                                quality as f32,
                                30.,
                                100.,
                                format!("{quality}%"),
                                Rc::new(|v, _, cx| update(cx, |s| s.jpeg_quality = v.round() as u8)),
                            ))
                            .child(folder_row("exp", "Export folder", s.save_dir.clone(), Some(|p, s| s.save_dir = p)))
                            .child(toggle("nodialog", "Save without asking where", s.save_without_dialog, |s| s.save_without_dialog = !s.save_without_dialog)),
                    )
                    .child(
                        group("Capture")
                            .child(labeled("Self-timer", div().flex().gap_2().child(timer(3)).child(timer(5)).child(timer(10))))
                            .child(toggle("shadow", "Add a shadow to window captures", s.window_shadow, |s| s.window_shadow = !s.window_shadow))
                            .child(toggle("loupe", "Show magnifier while selecting", s.show_loupe, |s| s.show_loupe = !s.show_loupe)),
                    )
            }
            Pane::Recordings => {
                let r = &s.after_recording;
                div().flex().flex_col().gap_4().child(
                    group("After recording")
                        .child(note("Each recording is added to the Library first, then any of these run."))
                        .child(toggle("r-preview", "Show preview", r.preview, |s| s.after_recording.preview = !s.after_recording.preview))
                        .child(toggle("r-copy", "Copy the video file", r.copy, |s| s.after_recording.copy = !s.after_recording.copy))
                        .child(toggle("r-save", "Save to the export folder", r.save, |s| s.after_recording.save = !s.after_recording.save))
                        .child(toggle("r-studio", "Open in Studio", r.studio, |s| s.after_recording.studio = !s.after_recording.studio)),
                )
            }
            Pane::Overlay => self.overlay_pane(&s, cx),
            Pane::Shortcuts => self.shortcuts_pane(&s, cx),
            Pane::Privacy => {
                let rules = s.redact_rules.clone();
                let rule = |id: &'static str, label: &'static str, on: bool, f: fn(&mut RedactRules)| {
                    ui::toggle(id, label, on, move |_, _, cx| update(cx, |s| f(&mut s.redact_rules)))
                };
                div().flex().flex_col().gap_4().child(
                    group("Hide sensitive info")
                        .child(toggle("t-redact", "Automatically hide sensitive info on capture", s.auto_redact, |s| s.auto_redact = !s.auto_redact))
                        .child(note("Uses on-device Windows OCR, so nothing leaves your PC. The original stays in the Library; you can undo a redaction in the editor."))
                        .child(labeled(
                            "Style",
                            div().flex().gap_2().children(RedactStyle::ALL.map(|k| {
                                ui::chip(SharedString::from(format!("rs-{}", k.label())), k.label(), s.redact_style == k, move |_, _, cx| update(cx, |s| s.redact_style = k))
                            })),
                        ))
                        .child(
                            div()
                                .grid()
                                .grid_cols(2)
                                .gap_x_6()
                                .gap_y_2()
                                .child(rule("r-email", "Email addresses", rules.emails, |r| r.emails = !r.emails))
                                .child(rule("r-phone", "Phone numbers", rules.phones, |r| r.phones = !r.phones))
                                .child(rule("r-card", "Credit cards", rules.credit_cards, |r| r.credit_cards = !r.credit_cards))
                                .child(rule("r-secret", "API keys, tokens, passwords", rules.secrets, |r| r.secrets = !r.secrets))
                                .child(rule("r-ip", "IP & MAC addresses", rules.ip_addresses, |r| r.ip_addresses = !r.ip_addresses))
                                .child(rule("r-id", "SSN & IBAN numbers", rules.ids, |r| r.ids = !r.ids))
                                .child(rule("r-url", "URLs with tokens", rules.urls_with_tokens, |r| r.urls_with_tokens = !r.urls_with_tokens)),
                        ),
                )
            }
        };

        div()
            .id("prefs")
            .track_focus(&self.focus)
            .size_full()
            .flex()
            
            .font_family(FONT)
            .text_color(ui::theme().text)
            .on_key_down(cx.listener(Self::on_key))
            .child(
                div()
                    .w(px(190.))
                    .flex_none()
                    .h_full()
                    .flex()
                    .flex_col()
                    .gap_1()
                    .p_3()
                    .bg(ui::theme().layer)
                    .border_r_1()
                    .border_color(ui::theme().border)
                    .children(nav),
            )
            .child(
                div()
                    .id("pane")
                    .bg(ui::theme().base)
                    .flex_1()
                    .min_w_0()
                    .h_full()
                    .overflow_y_scroll()
                    .child(
                        div()
                            .flex()
                            .flex_col()
                            .gap_4()
                            .p_6()
                            .child(div().text_xl().font_weight(gpui::FontWeight::BOLD).child(self.pane.label()))
                            .child(body),
                    ),
            )
    }
}
