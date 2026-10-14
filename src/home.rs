//! Main window: capture buttons, shortcuts and preferences.

use crate::settings::{AfterCapture, RedactRules, RedactStyle, Settings};
use crate::ui::{self, ACCENT, BG, BORDER, ELEV, FONT, MUTED, PANEL, TEXT};
use crate::{run_command, win, Command, Shortcuts};
use gpui::prelude::*;
use gpui::{
    div, px, rgb, size, App, Bounds, Context, Global, PathPromptOptions, TitlebarOptions, Window,
    WindowBounds, WindowHandle, WindowOptions,
};

#[derive(Default)]
struct HomeState(Option<WindowHandle<Home>>);
impl Global for HomeState {}

pub struct Home;

pub fn show(cx: &mut App) {
    if let Some(h) = cx.default_global::<HomeState>().0 {
        if h.update(cx, |_, window, _| window.activate_window()).is_ok() {
            return;
        }
    }
    let bounds = Bounds::centered(None, size(px(760.), px(720.)), cx);
    let handle = cx.open_window(
        WindowOptions {
            window_bounds: Some(WindowBounds::Windowed(bounds)),
            titlebar: Some(TitlebarOptions {
                title: Some("Glint".into()),
                appears_transparent: false,
                traffic_light_position: None,
            }),
            window_min_size: Some(size(px(560.), px(480.))),
            ..Default::default()
        },
        |_, cx| cx.new(|_| Home),
    );
    cx.default_global::<HomeState>().0 = handle.ok();
}

fn update(cx: &mut App, f: impl FnOnce(&mut Settings)) {
    let s = cx.global_mut::<Settings>();
    f(s);
    s.save();
    cx.refresh_windows();
}

fn capture_card(id: &'static str, icon: &'static str, title: &'static str, accel: String, cmd: Command) -> impl IntoElement {
    div()
        .id(id)
        .flex_1()
        .min_w(px(130.))
        .flex()
        .flex_col()
        .gap_2()
        .p_3()
        .rounded_lg()
        .bg(rgb(ELEV))
        .border_1()
        .border_color(rgb(BORDER))
        .cursor_pointer()
        .hover(|s| s.border_color(rgb(ACCENT)))
        .child(
            div()
                .size(px(32.))
                .rounded_md()
                .flex()
                .items_center()
                .justify_center()
                .bg(gpui::rgba(0x7C8CFF26))
                .child(ui::icon(icon, 17., rgb(ACCENT))),
        )
        .child(div().text_sm().text_color(rgb(TEXT)).child(title))
        .child(ui::kbd(&accel))
        .on_click(move |_, _, cx| {
            // Let the click finish before the screen is frozen.
            cx.defer(move |cx| run_command(cmd, cx));
        })
}

fn card(title: &str) -> gpui::Div {
    div()
        .flex()
        .flex_col()
        .gap_3()
        .p_4()
        .rounded_xl()
        .bg(rgb(PANEL))
        .border_1()
        .border_color(rgb(BORDER))
        .child(
            div()
                .text_xs()
                .font_weight(gpui::FontWeight::SEMIBOLD)
                .text_color(rgb(MUTED))
                .child(title.to_uppercase()),
        )
}

impl Render for Home {
    fn render(&mut self, _: &mut Window, cx: &mut Context<Self>) -> impl IntoElement {
        let s = cx.global::<Settings>().clone();
        let status = cx.global::<Shortcuts>().status.clone();
        let failed: Vec<String> = status
            .iter()
            .filter_map(|(_, accel, err)| err.as_ref().map(|e| format!("{accel}: {e}")))
            .collect();
        let rules = s.redact_rules.clone();

        let rule_toggle = |id: &'static str, label: &'static str, on: bool, f: fn(&mut RedactRules)| {
            ui::toggle(id, label, on, move |_, _, cx| update(cx, |s| f(&mut s.redact_rules)))
        };

        div()
            .id("home")
            .size_full()
            .bg(rgb(BG))
            .font_family(FONT)
            .text_color(rgb(TEXT))
            .overflow_y_scroll()
            .child(
                div()
                    .flex()
                    .flex_col()
                    .gap_4()
                    .p_6()
                    .child(
                        div()
                            .flex()
                            .items_center()
                            .gap_3()
                            .child(
                                div()
                                    .size(px(40.))
                                    .rounded_lg()
                                    .flex()
                                    .items_center()
                                    .justify_center()
                                    .bg(gpui::linear_gradient(
                                        135.,
                                        gpui::linear_color_stop(rgb(0x6366F1), 0.),
                                        gpui::linear_color_stop(rgb(0xEC4899), 1.),
                                    ))
                                    .child(ui::icon("area", 22., rgb(0xFFFFFF))),
                            )
                            .child(
                                div()
                                    .flex()
                                    .flex_col()
                                    .child(div().text_xl().font_weight(gpui::FontWeight::BOLD).child("Glint"))
                                    .child(
                                        div()
                                            .text_sm()
                                            .text_color(rgb(MUTED))
                                            .child("Beautiful, private screenshots. Lives in your system tray."),
                                    ),
                            ),
                    )
                    .child(
                        div()
                            .flex()
                            .flex_wrap()
                            .gap_3()
                            .child(capture_card("c-area", "area", "Capture Area", s.hotkeys.area.clone(), Command::CaptureArea))
                            .child(capture_card("c-full", "monitor", "Fullscreen", s.hotkeys.fullscreen.clone(), Command::CaptureFullscreen))
                            .child(capture_card("c-win", "window", "Active Window", s.hotkeys.window.clone(), Command::CaptureWindow))
                            .child(capture_card("c-prev", "repeat", "Previous Area", s.hotkeys.previous_area.clone(), Command::CapturePrevious))
                            .child(capture_card("c-text", "scan", "Capture Text", s.hotkeys.text.clone(), Command::CaptureText)),
                    )
                    .when(!failed.is_empty(), |d| {
                        d.child(
                            div()
                                .p_3()
                                .rounded_lg()
                                .bg(gpui::rgba(0xFF3B3022))
                                .border_1()
                                .border_color(gpui::rgba(0xFF3B3066))
                                .text_sm()
                                .child(format!(
                                    "Some shortcuts are taken by another app: {}. Edit them in settings.json.",
                                    failed.join(", ")
                                )),
                        )
                    })
                    .child(
                        card("After capture")
                            .child(
                                div()
                                    .flex()
                                    .gap_2()
                                    .child(ui::chip("ac-q", "Show quick access", s.after_capture == AfterCapture::QuickAccess, |_, _, cx| update(cx, |s| s.after_capture = AfterCapture::QuickAccess)))
                                    .child(ui::chip("ac-e", "Open editor", s.after_capture == AfterCapture::OpenEditor, |_, _, cx| update(cx, |s| s.after_capture = AfterCapture::OpenEditor)))
                                    .child(ui::chip("ac-n", "Do nothing", s.after_capture == AfterCapture::Nothing, |_, _, cx| update(cx, |s| s.after_capture = AfterCapture::Nothing))),
                            )
                            .child(ui::toggle("t-copy", "Copy to clipboard", s.copy_on_capture, |_, _, cx| update(cx, |s| s.copy_on_capture = !s.copy_on_capture)))
                            .child(ui::toggle("t-save", "Save to folder", s.save_on_capture, |_, _, cx| update(cx, |s| s.save_on_capture = !s.save_on_capture)))
                            .child(ui::toggle("t-loupe", "Show magnifier while selecting", s.show_loupe, |_, _, cx| update(cx, |s| s.show_loupe = !s.show_loupe)))
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .justify_between()
                                    .gap_3()
                                    .child(
                                        div()
                                            .flex()
                                            .flex_col()
                                            .min_w_0()
                                            .child(div().text_sm().child("Save folder"))
                                            .child(div().text_xs().text_color(rgb(MUTED)).truncate().child(s.save_dir.display().to_string())),
                                    )
                                    .child(
                                        div()
                                            .flex()
                                            .gap_2()
                                            .child(ui::button("open-dir", "Open", Some("folder"), false, {
                                                let dir = s.save_dir.clone();
                                                move |_, _, cx| {
                                                    let _ = std::fs::create_dir_all(&dir);
                                                    cx.open_with_system(&dir);
                                                }
                                            }))
                                            .child(ui::button("pick-dir", "Change…", None, false, |_, _, cx| {
                                                let rx = cx.prompt_for_paths(PathPromptOptions {
                                                    files: false,
                                                    directories: true,
                                                    multiple: false,
                                                    prompt: Some("Choose folder".into()),
                                                });
                                                cx.spawn(async move |cx| {
                                                    if let Ok(Ok(Some(paths))) = rx.await {
                                                        if let Some(p) = paths.into_iter().next() {
                                                            let _ = cx.update(|cx| update(cx, |s| s.save_dir = p));
                                                        }
                                                    }
                                                })
                                                .detach();
                                            })),
                                    ),
                            ),
                    )
                    .child(
                        card("Privacy")
                            .child(ui::toggle("t-redact", "Automatically hide sensitive info on capture", s.auto_redact, |_, _, cx| update(cx, |s| s.auto_redact = !s.auto_redact)))
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(rgb(MUTED))
                                    .child("Uses on-device Windows OCR — nothing leaves your PC. You can always undo a redaction in the editor."),
                            )
                            .child(
                                div()
                                    .flex()
                                    .items_center()
                                    .gap_2()
                                    .child(div().text_sm().mr_2().child("Style"))
                                    .children(RedactStyle::ALL.map(|k| {
                                        ui::chip(SharedId::of("rs", k.label()), k.label(), s.redact_style == k, move |_, _, cx| update(cx, |s| s.redact_style = k))
                                    })),
                            )
                            .child(
                                div()
                                    .grid()
                                    .grid_cols(2)
                                    .gap_x_6()
                                    .gap_y_2()
                                    .child(rule_toggle("r-email", "Email addresses", rules.emails, |r| r.emails = !r.emails))
                                    .child(rule_toggle("r-phone", "Phone numbers", rules.phones, |r| r.phones = !r.phones))
                                    .child(rule_toggle("r-card", "Credit cards", rules.credit_cards, |r| r.credit_cards = !r.credit_cards))
                                    .child(rule_toggle("r-secret", "API keys, tokens, passwords", rules.secrets, |r| r.secrets = !r.secrets))
                                    .child(rule_toggle("r-ip", "IP & MAC addresses", rules.ip_addresses, |r| r.ip_addresses = !r.ip_addresses))
                                    .child(rule_toggle("r-id", "SSN & IBAN numbers", rules.ids, |r| r.ids = !r.ids))
                                    .child(rule_toggle("r-url", "URLs with tokens", rules.urls_with_tokens, |r| r.urls_with_tokens = !r.urls_with_tokens)),
                            ),
                    )
                    .child(
                        card("General")
                            .child(ui::toggle("t-login", "Launch at login", s.launch_at_login, |_, _, cx| {
                                update(cx, |s| {
                                    s.launch_at_login = !s.launch_at_login;
                                    win::set_launch_at_login(s.launch_at_login);
                                })
                            }))
                            .child(
                                div()
                                    .flex()
                                    .gap_2()
                                    .child(ui::button("last", "Open last capture", Some("image"), false, |_, _, cx| run_command(Command::OpenLast, cx)))
                                    .child(ui::button("cfg", "Edit shortcuts (settings.json)", Some("keyboard"), false, |_, _, cx| {
                                        let p = dirs::config_dir().unwrap_or_default().join("Glint").join("settings.json");
                                        cx.global::<Settings>().save();
                                        cx.open_with_system(&p);
                                    }))
                                    .child(ui::button("reload", "Reload shortcuts", None, false, |_, _, cx| {
                                        let fresh = Settings::load();
                                        cx.update_global::<Shortcuts, _>(|sc, _| sc.register(&fresh));
                                        cx.set_global(fresh);
                                        cx.refresh_windows();
                                    })),
                            )
                            .child(
                                div()
                                    .text_xs()
                                    .text_color(rgb(MUTED))
                                    .child("Closing this window keeps Glint running in the tray. Double-click the tray icon to capture an area."),
                            ),
                    ),
            )
    }
}

/// Builds element ids from a prefix and a label.
struct SharedId;
impl SharedId {
    fn of(prefix: &str, label: &str) -> gpui::ElementId {
        gpui::ElementId::Name(format!("{prefix}-{label}").into())
    }
}
