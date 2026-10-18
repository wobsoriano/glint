#![cfg_attr(not(debug_assertions), windows_subsystem = "windows")]

mod actions;
mod assets;
mod capture;
mod cards;
mod clip;
mod desktop;
mod editor;
mod flow;
mod hotkeys;
mod ipc;
mod library;
mod library_window;
mod ocr;
mod overlay;
mod pin;
mod prefs;
mod quicklook;
mod record;
mod redact;
mod settings;
mod shell;
mod stack;
mod store;
mod studio;
mod sys;
mod toast;
mod ui;

pub use glint_render::{annotate, render, style};

use global_hotkey::{GlobalHotKeyEvent, HotKeyState};
use gpui::prelude::*;
use gpui::{div, px, size, App, Application, Bounds, Global, WindowBounds, WindowKind, WindowOptions};
use ipc::{CaptureCmd, Request};
use settings::{HotkeyAction, Settings};
use std::time::Duration;
use tray_icon::menu::{Menu, MenuEvent, MenuId, MenuItem, PredefinedMenuItem};
use tray_icon::{TrayIcon, TrayIconBuilder, TrayIconEvent};

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Command {
    Capture(CaptureCmd),
    CaptureTimer,
    Recording,
    TrimVideo,
    Library,
    Settings,
    OpenLast,
    Quit,
}

impl From<HotkeyAction> for Command {
    fn from(a: HotkeyAction) -> Self {
        match a {
            HotkeyAction::Area => Command::Capture(CaptureCmd::Area),
            HotkeyAction::Fullscreen => Command::Capture(CaptureCmd::Screen),
            HotkeyAction::Window => Command::Capture(CaptureCmd::Window),
            HotkeyAction::PreviousArea => Command::Capture(CaptureCmd::Previous),
            HotkeyAction::Text => Command::Capture(CaptureCmd::Text),
            HotkeyAction::Recording => Command::Recording,
            HotkeyAction::Library => Command::Library,
        }
    }
}

pub fn run_command(cmd: Command, cx: &mut App) {
    match cmd {
        Command::Capture(CaptureCmd::Area) => flow::start_area(overlay::Mode::Image, cx),
        Command::Capture(CaptureCmd::Text) => flow::start_area(overlay::Mode::Text, cx),
        Command::Capture(CaptureCmd::Screen) => flow::capture_fullscreen(cx),
        Command::Capture(CaptureCmd::Window) => flow::capture_window(cx),
        Command::Capture(CaptureCmd::Previous) => flow::capture_previous(cx),
        Command::CaptureTimer => flow::capture_after_timer(cx),
        Command::Recording => record::toggle(cx),
        Command::TrimVideo => clip::pick(cx),
        Command::Library => library_window::show(cx),
        Command::Settings => prefs::show(cx),
        Command::OpenLast => flow::open_last(cx),
        Command::Quit => record::quit(cx),
    }
}

fn handle_requests(requests: Vec<Request>, cx: &mut App) {
    let mut images = Vec::new();
    for r in requests {
        match r {
            Request::Capture(c) => run_command(Command::Capture(c), cx),
            Request::Library => library_window::show(cx),
            Request::Record => record::toggle(cx),
            Request::Open(path) => images.push(path),
            Request::Trim(path) => clip::open(path, cx),
            Request::Studio(dir) => studio::open(cx, dir),
        }
    }
    if !images.is_empty() {
        flow::open_images(images, cx);
    }
}

struct Tray {
    icon: Option<TrayIcon>,
    ids: Vec<(MenuId, Command)>,
}

impl Global for Tray {}

fn tray_menu(s: &Settings) -> (Menu, Vec<(MenuId, Command)>) {
    let menu = Menu::new();
    let mut ids = Vec::new();
    let h = &s.hotkeys;
    let display = |a: HotkeyAction| Some(hotkeys::display(h.get(a)));
    let items = [
        ("Capture Area", display(HotkeyAction::Area), Command::Capture(CaptureCmd::Area)),
        ("Capture Window", display(HotkeyAction::Window), Command::Capture(CaptureCmd::Window)),
        ("Capture Full Screen", display(HotkeyAction::Fullscreen), Command::Capture(CaptureCmd::Screen)),
        ("Capture Previous Area", display(HotkeyAction::PreviousArea), Command::Capture(CaptureCmd::Previous)),
        ("Capture Text", display(HotkeyAction::Text), Command::Capture(CaptureCmd::Text)),
        ("Self-Timer", Some(format!("{} s", s.timer_seconds)), Command::CaptureTimer),
        ("Record Screen", display(HotkeyAction::Recording), Command::Recording),
    ];
    let mut add = |label: String, cmd: Command| {
        let item = MenuItem::new(label, true, None);
        ids.push((item.id().clone(), cmd));
        let _ = menu.append(&item);
    };
    for (label, accel, cmd) in items {
        add(accel.map(|a| format!("{label}\t{a}")).unwrap_or(label.to_string()), cmd);
    }
    // Win32 menus read a single & as a mnemonic marker.
    add("Trim && Compress Video…".into(), Command::TrimVideo);
    let _ = menu.append(&PredefinedMenuItem::separator());
    add(format!("Open Library\t{}", hotkeys::display(&h.library)), Command::Library);
    add("Edit Last Capture".into(), Command::OpenLast);
    add("Settings…".into(), Command::Settings);
    let _ = menu.append(&PredefinedMenuItem::separator());
    add("Quit Glint".into(), Command::Quit);
    (menu, ids)
}

pub fn refresh_tray(cx: &mut App) {
    let (menu, ids) = tray_menu(cx.global::<Settings>());
    let tray = cx.global_mut::<Tray>();
    if let Some(icon) = &tray.icon {
        icon.set_menu(Some(Box::new(menu)));
    }
    tray.ids = ids;
}

fn build_tray(s: &Settings) -> Tray {
    let (menu, ids) = tray_menu(s);
    let icon = TrayIconBuilder::new()
        .with_tooltip("Glint")
        .with_icon(tray_icon_image())
        .with_menu(Box::new(menu))
        .build()
        .ok();
    Tray { icon, ids }
}

/// GPUI quits when the last window closes; this invisible window keeps us running in the tray.
struct KeepAlive;

impl Render for KeepAlive {
    fn render(&mut self, _: &mut gpui::Window, _: &mut gpui::Context<Self>) -> impl IntoElement {
        div()
    }
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let cwd = std::env::current_dir().unwrap_or_default();
    let launch = match ipc::parse(&args, &cwd) {
        Ok(l) => l,
        Err(e) => {
            eprintln!("{e}");
            std::process::exit(2);
        }
    };
    if !desktop::single_instance() {
        if launch.requests.is_empty() && launch.background {
            return;
        }
        let requests = if launch.requests.is_empty() { vec![Request::Library] } else { launch.requests };
        if let Err(e) = ipc::forward(&requests) {
            eprintln!("{e:#}");
            std::process::exit(1);
        }
        return;
    }
    ipc::listen();
    if let Err(e) = shell::register_open_with() {
        eprintln!("Open with Glint is not registered: {e:#}");
    }

    Application::new().with_assets(assets::Assets).run(move |cx: &mut App| {
        let settings = Settings::load();
        cx.set_global(flow::AppState::default());
        store::init(cx);
        let mut keys = hotkeys::Hotkeys::new();
        keys.register_all(&settings);
        cx.set_global(keys);
        cx.set_global(build_tray(&settings));
        shell::exclude_own_windows_from_capture(settings.include_own_windows);
        cx.set_global(settings);

        let _ = cx.open_window(
            WindowOptions {
                window_bounds: Some(WindowBounds::Windowed(Bounds { origin: gpui::point(px(-10000.), px(-10000.)), size: size(px(1.), px(1.)) })),
                show: false,
                focus: false,
                kind: WindowKind::PopUp,
                titlebar: None,
                ..Default::default()
            },
            |window, cx| {
                cx.new(|cx| {
                    cx.observe_window_appearance(window, |_, _, cx| {
                        if ui::refresh_theme() {
                            for w in cx.windows() {
                                let _ = w.update(cx, |_, window, _| shell::caption_text(window));
                            }
                            cx.refresh_windows();
                        }
                    })
                    .detach();
                    KeepAlive
                })
            },
        );

        if launch.requests.is_empty() && !launch.background {
            library_window::show(cx);
        }
        handle_requests(launch.requests, cx);

        cx.spawn(async move |cx| loop {
            gpui::Timer::after(Duration::from_millis(30)).await;
            let mut keys = Vec::new();
            while let Ok(ev) = GlobalHotKeyEvent::receiver().try_recv() {
                if ev.state() == HotKeyState::Pressed {
                    keys.push(ev.id());
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
            let forwarded = ipc::drain();
            if keys.is_empty() && menu.is_empty() && !tray_clicked && forwarded.is_empty() {
                continue;
            }
            let ok = cx.update(|cx| {
                let hk = cx.global::<hotkeys::Hotkeys>();
                let mut todo: Vec<Command> = keys.iter().filter_map(|id| hk.action_for(*id)).map(Command::from).collect();
                let tray = cx.global::<Tray>();
                todo.extend(menu.iter().filter_map(|id| tray.ids.iter().find(|(m, _)| m == id).map(|(_, c)| *c)));
                if tray_clicked {
                    todo.push(Command::Capture(CaptureCmd::Area));
                }
                for c in todo {
                    run_command(c, cx);
                }
                handle_requests(forwarded, cx);
            });
            if ok.is_err() {
                break;
            }
        })
        .detach();
    });
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


#[cfg(test)]
mod pipeline_tests {
    use crate::annotate::{pt, Annotation, Shape, TextStyle, R};
    use crate::style::{Chrome, Color};

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
        anns.push(mk(Shape::Text { pos: pt(w * 0.1, h * 0.1), text: "Hello Glint".into(), size: 48.0, style: TextStyle::Pill, font: Default::default() }));
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
        let anns: Vec<Annotation> = lines.iter().enumerate().map(|(i, t)| Annotation { id: 0, shape: Shape::Text { pos: pt(30.0, 30.0 + i as f32 * 100.0), text: t.to_string(), size: 40.0, style: TextStyle::Plain, font: Default::default() }, color: Color::BLACK, width: 1.0 }).collect();
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
