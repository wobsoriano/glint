#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod annotate;
mod assets;
mod capture;
mod editor;
mod flow;
mod home;
mod ocr;
mod overlay;
mod pin;
mod quick;
mod redact;
mod render;
mod settings;
mod style;
mod ui;
mod win;

use global_hotkey::hotkey::HotKey;
use global_hotkey::{GlobalHotKeyEvent, GlobalHotKeyManager, HotKeyState};
use gpui::prelude::*;
use gpui::{div, px, size, App, Application, Bounds, Global, WindowBounds, WindowKind, WindowOptions};
use settings::Settings;
use std::time::Duration;
use tray_icon::menu::{Menu, MenuEvent, MenuItem, PredefinedMenuItem};
use tray_icon::{TrayIcon, TrayIconBuilder, TrayIconEvent};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Command {
    CaptureArea,
    CaptureFullscreen,
    CaptureWindow,
    CapturePrevious,
    CaptureText,
    CaptureTimer,
    OpenLast,
    ShowHome,
    Quit,
}

/// Registered global shortcuts and the tray icon; kept alive for the whole session.
pub struct Shortcuts {
    manager: Option<GlobalHotKeyManager>,
    bindings: Vec<(HotKey, Command)>,
    pub status: Vec<(Command, String, Option<String>)>,
    _tray: Option<TrayIcon>,
    menu_ids: Vec<(tray_icon::menu::MenuId, Command)>,
}

impl Global for Shortcuts {}

impl Shortcuts {
    pub fn register(&mut self, settings: &Settings) {
        let Some(manager) = &self.manager else { return };
        for (hk, _) in self.bindings.drain(..) {
            let _ = manager.unregister(hk);
        }
        self.status.clear();
        let h = &settings.hotkeys;
        for (cmd, accel) in [
            (Command::CaptureArea, &h.area),
            (Command::CaptureFullscreen, &h.fullscreen),
            (Command::CaptureWindow, &h.window),
            (Command::CapturePrevious, &h.previous_area),
            (Command::CaptureText, &h.text),
        ] {
            let result = accel
                .parse::<HotKey>()
                .map_err(|e| e.to_string())
                .and_then(|hk| {
                    manager.register(hk).map_err(|e| e.to_string())?;
                    Ok(hk)
                });
            match result {
                Ok(hk) => {
                    self.bindings.push((hk, cmd));
                    self.status.push((cmd, accel.clone(), None));
                }
                Err(e) => self.status.push((cmd, accel.clone(), Some(e))),
            }
        }
    }
}

fn tray_icon_image() -> tray_icon::Icon {
    let n = 32u32;
    let mut pm = tiny_skia::Pixmap::new(n, n).unwrap();
    let grad = tiny_skia::LinearGradient::new(
        tiny_skia::Point::from_xy(0.0, 0.0),
        tiny_skia::Point::from_xy(n as f32, n as f32),
        vec![
            tiny_skia::GradientStop::new(0.0, tiny_skia::Color::from_rgba8(0x63, 0x66, 0xF1, 255)),
            tiny_skia::GradientStop::new(1.0, tiny_skia::Color::from_rgba8(0xEC, 0x48, 0x99, 255)),
        ],
        tiny_skia::SpreadMode::Pad,
        tiny_skia::Transform::identity(),
    )
    .unwrap();
    let paint = tiny_skia::Paint {
        shader: grad,
        anti_alias: true,
        ..Default::default()
    };
    let card = annotate::R { x: 1.0, y: 1.0, w: 30.0, h: 30.0 };
    pm.fill_path(
        &render::rounded_rect(&card, 8.0).unwrap(),
        &paint,
        tiny_skia::FillRule::Winding,
        tiny_skia::Transform::identity(),
        None,
    );
    let mut white = tiny_skia::Paint::default();
    white.set_color_rgba8(255, 255, 255, 255);
    white.anti_alias = true;
    let stroke = tiny_skia::Stroke {
        width: 2.6,
        line_cap: tiny_skia::LineCap::Round,
        ..Default::default()
    };
    let mut pb = tiny_skia::PathBuilder::new();
    // Four corner brackets, like a selection frame.
    for [a, b, c] in [
        [(8.0, 13.0), (8.0, 8.0), (13.0, 8.0)],
        [(19.0, 8.0), (24.0, 8.0), (24.0, 13.0)],
        [(24.0, 19.0), (24.0, 24.0), (19.0, 24.0)],
        [(13.0, 24.0), (8.0, 24.0), (8.0, 19.0)],
    ] {
        pb.move_to(a.0, a.1);
        pb.line_to(b.0, b.1);
        pb.line_to(c.0, c.1);
    }
    if let Some(p) = pb.finish() {
        pm.stroke_path(&p, &white, &stroke, tiny_skia::Transform::identity(), None);
    }
    let rgba = render::rgba_from_pixmap(&pm);
    tray_icon::Icon::from_rgba(rgba.into_raw(), n, n).unwrap()
}

fn build_tray() -> (Option<TrayIcon>, Vec<(tray_icon::menu::MenuId, Command)>) {
    let menu = Menu::new();
    let mut ids = Vec::new();
    let s = Settings::load();
    let items = [
        ("Capture Area", Some(s.hotkeys.area.clone()), Command::CaptureArea),
        ("Capture Fullscreen", Some(s.hotkeys.fullscreen.clone()), Command::CaptureFullscreen),
        ("Capture Window", Some(s.hotkeys.window.clone()), Command::CaptureWindow),
        ("Capture Previous Area", Some(s.hotkeys.previous_area.clone()), Command::CapturePrevious),
        ("Capture Text (OCR)", Some(s.hotkeys.text.clone()), Command::CaptureText),
        ("Fullscreen in 3 Seconds", None, Command::CaptureTimer),
    ];
    for (label, accel, cmd) in items {
        let text = match accel {
            Some(a) => format!("{label}\t{a}"),
            None => label.to_string(),
        };
        let item = MenuItem::new(text, true, None);
        ids.push((item.id().clone(), cmd));
        let _ = menu.append(&item);
    }
    let _ = menu.append(&PredefinedMenuItem::separator());
    for (label, cmd) in [
        ("Open Last Capture", Command::OpenLast),
        ("Settings…", Command::ShowHome),
    ] {
        let item = MenuItem::new(label, true, None);
        ids.push((item.id().clone(), cmd));
        let _ = menu.append(&item);
    }
    let _ = menu.append(&PredefinedMenuItem::separator());
    let quit = MenuItem::new("Quit PrettyShot", true, None);
    ids.push((quit.id().clone(), Command::Quit));
    let _ = menu.append(&quit);

    let tray = TrayIconBuilder::new()
        .with_tooltip("PrettyShot")
        .with_icon(tray_icon_image())
        .with_menu(Box::new(menu))
        .build()
        .ok();
    (tray, ids)
}

/// GPUI quits when the last window closes; this invisible window keeps us running in the tray.
struct KeepAlive;

impl Render for KeepAlive {
    fn render(&mut self, _: &mut gpui::Window, _: &mut gpui::Context<Self>) -> impl IntoElement {
        div()
    }
}

pub fn run_command(cmd: Command, cx: &mut App) {
    match cmd {
        Command::CaptureArea => flow::start_area(overlay::Mode::Image, cx),
        Command::CaptureText => flow::start_area(overlay::Mode::Text, cx),
        Command::CaptureFullscreen => flow::capture_fullscreen(cx),
        Command::CaptureWindow => flow::capture_window(cx),
        Command::CapturePrevious => flow::capture_previous(cx),
        Command::CaptureTimer => {
            quick::show_toast("Capturing in 3 seconds…", cx);
            cx.spawn(async move |cx| {
                gpui::Timer::after(Duration::from_secs(3)).await;
                let _ = cx.update(flow::capture_fullscreen);
            })
            .detach();
        }
        Command::OpenLast => flow::open_last(cx),
        Command::ShowHome => home::show(cx),
        Command::Quit => cx.quit(),
    }
}

fn main() {
    if !win::single_instance() {
        return;
    }
    let background = std::env::args().any(|a| a == "--background");

    Application::new()
        .with_assets(assets::Assets)
        .run(move |cx: &mut App| {
            let settings = Settings::load();
            cx.set_global(flow::AppState::default());

            let (tray, menu_ids) = build_tray();
            let mut shortcuts = Shortcuts {
                manager: GlobalHotKeyManager::new().ok(),
                bindings: Vec::new(),
                status: Vec::new(),
                _tray: tray,
                menu_ids,
            };
            shortcuts.register(&settings);
            cx.set_global(shortcuts);
            cx.set_global(settings);

            let _ = cx.open_window(
                WindowOptions {
                    window_bounds: Some(WindowBounds::Windowed(Bounds {
                        origin: gpui::point(px(-10000.), px(-10000.)),
                        size: size(px(1.), px(1.)),
                    })),
                    show: false,
                    focus: false,
                    kind: WindowKind::PopUp,
                    titlebar: None,
                    ..Default::default()
                },
                |_, cx| cx.new(|_| KeepAlive),
            );

            if !background {
                home::show(cx);
            }

            // Poll global hotkeys and tray events from the main thread.
            cx.spawn(async move |cx| loop {
                gpui::Timer::after(Duration::from_millis(30)).await;
                let mut cmds = Vec::new();
                while let Ok(ev) = GlobalHotKeyEvent::receiver().try_recv() {
                    if ev.state() == HotKeyState::Pressed {
                        cmds.push(ev.id());
                    }
                }
                let mut menu = Vec::new();
                while let Ok(ev) = MenuEvent::receiver().try_recv() {
                    menu.push(ev.id().clone());
                }
                let mut tray_clicked = false;
                while let Ok(ev) = TrayIconEvent::receiver().try_recv() {
                    if let TrayIconEvent::DoubleClick { .. } = ev {
                        tray_clicked = true;
                    }
                }
                if cmds.is_empty() && menu.is_empty() && !tray_clicked {
                    continue;
                }
                let ok = cx.update(|cx| {
                    let s = cx.global::<Shortcuts>();
                    let mut todo: Vec<Command> = cmds
                        .iter()
                        .filter_map(|id| s.bindings.iter().find(|(b, _)| b.id() == *id).map(|(_, c)| *c))
                        .collect();
                    todo.extend(
                        menu.iter()
                            .filter_map(|id| s.menu_ids.iter().find(|(m, _)| m == id).map(|(_, c)| *c)),
                    );
                    if tray_clicked {
                        todo.push(Command::CaptureArea);
                    }
                    for c in todo {
                        run_command(c, cx);
                    }
                });
                if ok.is_err() {
                    break;
                }
            })
            .detach();
        });
}

#[cfg(test)]
mod pipeline_tests {
    use crate::annotate::{pt, Annotation, Shape, TextStyle, R};
    use crate::style::{Chrome, Color};

    /// Captures the primary monitor, runs OCR + redaction and renders a framed export.
    /// Run with: cargo test pipeline -- --ignored --nocapture
    #[test]
    #[ignore]
    fn pipeline() {
        let out = std::env::var("PIPELINE_OUT").unwrap_or_else(|_| ".".into());
        let shots = crate::capture::capture_monitors().unwrap();
        let m = &shots[0];
        println!("monitors: {}, first {}x{} scale {}", shots.len(), m.width, m.height, m.scale);
        let cap = crate::capture::Capture::new((*m.image).clone(), m.scale, Some("Test Window".into()));

        let t = std::time::Instant::now();
        let lines = crate::ocr::recognize(&cap.image).unwrap();
        println!("ocr: {} lines in {:?}", lines.len(), t.elapsed());
        println!("{}", crate::ocr::to_text(&lines).chars().take(400).collect::<String>());
        let findings = crate::redact::find(&lines, &Default::default(), 2.0);
        println!("findings: {:?}", findings.iter().map(|f| f.kind).collect::<Vec<_>>());

        let mut anns = crate::editor::redactions_from(&findings, crate::settings::RedactStyle::Blur);
        let (w, h) = (cap.image.width() as f32, cap.image.height() as f32);
        let ink = Color::rgb(0xFF3B30);
        let mk = |shape| Annotation { id: 0, shape, color: ink, width: 6.0 };
        anns.push(mk(Shape::Arrow { from: pt(w * 0.2, h * 0.7), to: pt(w * 0.45, h * 0.45) }));
        anns.push(mk(Shape::Rect { rect: R { x: w * 0.5, y: h * 0.2, w: w * 0.2, h: h * 0.15 }, filled: false }));
        anns.push(mk(Shape::Text { pos: pt(w * 0.1, h * 0.1), text: "Hello PrettyShot".into(), size: 48.0, style: TextStyle::Pill }));
        anns.push(mk(Shape::Counter { center: pt(w * 0.8, h * 0.7), n: 1, size: 22.0 }));
        anns.push(Annotation { color: Color::rgb(0xFFE14D), ..mk(Shape::Highlight { rect: R { x: w * 0.1, y: h * 0.3, w: w * 0.2, h: 40.0 } }) });

        let mut style = crate::style::Style::default();
        style.chrome = Chrome::MacDark;
        let t = std::time::Instant::now();
        let full = R { x: 0.0, y: 0.0, w, h };
        let l = crate::render::layout(&style, full, cap.dpi, 1.0);
        let image = crate::render::prepare_image(&cap.image, full, l.image.w as u32, l.image.h as u32);
        let inputs = crate::render::Inputs { style: &style, layout: l, image: &image, blur_bg: None, bg_image: None, title: cap.title.as_deref() };
        let mut pm = crate::render::render_base(&inputs, &anns);
        crate::render::render_annotations(&mut pm, &l, &anns);
        println!("render {}x{} in {:?}", pm.width(), pm.height(), t.elapsed());
        crate::render::rgba_from_pixmap(&pm).save(format!("{out}/export.png")).unwrap();
    }

    #[test]
    #[ignore]
    fn redacts_rendered_secrets() {
        let mut pm = tiny_skia::Pixmap::new(1400, 360).unwrap();
        pm.fill(tiny_skia::Color::WHITE);
        let style = crate::style::Style { background: crate::style::Background::None, padding: 0.0, shadow: 0.0, radius: 0.0, ..Default::default() };
        let full = R { x: 0.0, y: 0.0, w: 1400.0, h: 360.0 };
        let l = crate::render::layout(&style, full, 1.0, 1.0);
        let lines = ["Contact: jane.doe@example.com", "Key: sk_live_51HxYzAbCdEfGhIjKlMnOp", "Card 4242 4242 4242 4242 exp 12/29"];
        let anns: Vec<Annotation> = lines.iter().enumerate().map(|(i, t)| Annotation { id: 0, shape: Shape::Text { pos: pt(30.0, 30.0 + i as f32 * 100.0), text: t.to_string(), size: 40.0, style: TextStyle::Plain }, color: Color::BLACK, width: 1.0 }).collect();
        crate::render::render_annotations(&mut pm, &l, &anns);
        let img = crate::render::rgba_from_pixmap(&pm);
        if let Ok(out) = std::env::var("PIPELINE_OUT") {
            img.save(format!("{out}/secrets.png")).unwrap();
        }
        let ocr = crate::ocr::recognize(&img).unwrap();
        println!("{}", crate::ocr::to_text(&ocr));
        let kinds: Vec<_> = crate::redact::find(&ocr, &Default::default(), 2.0).iter().map(|f| f.kind).collect();
        println!("{kinds:?}");
        use crate::redact::Kind;
        for k in [Kind::Email, Kind::Secret, Kind::Card] {
            assert!(kinds.contains(&k), "missing {k:?}");
        }
    }
}
