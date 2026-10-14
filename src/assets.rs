//! Embedded SVG icons (24×24, stroke style). GPUI tints them with the element's text color.

use gpui::{AssetSource, SharedString};
use std::borrow::Cow;

const HEAD: &str = r##"<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#000" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">"##;

fn body(name: &str) -> Option<&'static str> {
    Some(match name {
        "select" => r##"<path d="M5 3l14 7.5-6.2 1.6L10 18.5z" fill="#000"/>"##,
        "arrow" => r##"<path d="M5 19L19 5"/><path d="M9 5h10v10"/>"##,
        "rect" => r##"<rect x="4" y="5" width="16" height="14" rx="2.5"/>"##,
        "ellipse" => r##"<ellipse cx="12" cy="12" rx="9" ry="7"/>"##,
        "line" => r##"<path d="M5 19L19 5"/>"##,
        "pen" => r##"<path d="M3 17c3-1 4-6 7-6s2 6 5 6 3.5-4 6-5"/>"##,
        "text" => r##"<path d="M5 6V4h14v2"/><path d="M12 4v16"/><path d="M9 20h6"/>"##,
        "highlight" => r##"<path d="M9 11l-5 5v3h6l5-5"/><path d="M13 7l4 4"/><path d="M9 11l6-6 4 4-6 6z"/>"##,
        "counter" => r##"<circle cx="12" cy="12" r="9"/><path d="M10.5 9l2-1.5V16"/>"##,
        "redact" => r##"<rect x="3" y="8" width="18" height="8" rx="1.5" fill="#000"/><path d="M3 4h18M3 20h12"/>"##,
        "spotlight" => r##"<circle cx="12" cy="12" r="4"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M4.9 19.1L7 17M17 7l2.1-2.1"/>"##,
        "crop" => r##"<path d="M6 2v14a2 2 0 0 0 2 2h14"/><path d="M18 22V8a2 2 0 0 0-2-2H2"/>"##,
        "undo" => r##"<path d="M9 14L4 9l5-5"/><path d="M4 9h10.5a5.5 5.5 0 0 1 0 11H11"/>"##,
        "redo" => r##"<path d="M15 14l5-5-5-5"/><path d="M20 9H9.5a5.5 5.5 0 0 0 0 11H13"/>"##,
        "copy" => r##"<rect x="8" y="8" width="13" height="13" rx="2"/><path d="M4 16a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h10a2 2 0 0 1 2 2"/>"##,
        "save" => r##"<path d="M12 3v12"/><path d="M7 10l5 5 5-5"/><path d="M5 21h14"/>"##,
        "pin" => r##"<path d="M12 17v5"/><path d="M9 10.8a2 2 0 0 1-1.1 1.8l-1.8.9A2 2 0 0 0 5 15.2V17h14v-1.8a2 2 0 0 0-1.1-1.8l-1.8-.9a2 2 0 0 1-1.1-1.8V7a1 1 0 0 1 1-1 2 2 0 0 0 0-4H8a2 2 0 0 0 0 4 1 1 0 0 1 1 1z"/>"##,
        "trash" => r##"<path d="M3 6h18"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>"##,
        "shield" => r##"<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/><path d="M9 12l2 2 4-4"/>"##,
        "scan" => r##"<path d="M3 7V5a2 2 0 0 1 2-2h2M17 3h2a2 2 0 0 1 2 2v2M21 17v2a2 2 0 0 1-2 2h-2M7 21H5a2 2 0 0 1-2-2v-2"/><path d="M7 8h10M7 12h10M7 16h6"/>"##,
        "edit" => r##"<path d="M12 20h9"/><path d="M16.5 3.5a2.1 2.1 0 0 1 3 3L7 19l-4 1 1-4z"/>"##,
        "close" => r##"<path d="M18 6L6 18M6 6l12 12"/>"##,
        "min" => r##"<path d="M5 12h14"/>"##,
        "max" => r##"<rect x="5" y="5" width="14" height="14" rx="1"/>"##,
        "folder" => r##"<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>"##,
        "image" => r##"<rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="M21 15l-5-5L5 21"/>"##,
        "camera" => r##"<path d="M14.5 4h-5L7 7H4a2 2 0 0 0-2 2v9a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2V9a2 2 0 0 0-2-2h-3z"/><circle cx="12" cy="13" r="3"/>"##,
        "monitor" => r##"<rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>"##,
        "window" => r##"<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M3 9h18"/>"##,
        "area" => r##"<path d="M4 8V4h4M16 4h4v4M20 16v4h-4M8 20H4v-4"/>"##,
        "repeat" => r##"<path d="M17 2l4 4-4 4"/><path d="M3 11v-1a4 4 0 0 1 4-4h14"/><path d="M7 22l-4-4 4-4"/><path d="M21 13v1a4 4 0 0 1-4 4H3"/>"##,
        "timer" => r##"<circle cx="12" cy="13" r="8"/><path d="M12 9v4l2 2M10 2h4"/>"##,
        "sparkles" => r##"<path d="M12 3l1.9 5.1L19 10l-5.1 1.9L12 17l-1.9-5.1L5 10l5.1-1.9z"/><path d="M19 17l.8 2.2L22 20l-2.2.8L19 23l-.8-2.2L16 20l2.2-.8z"/>"##,
        "check" => r##"<path d="M20 6L9 17l-5-5"/>"##,
        "keyboard" => r##"<rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 10h.01M10 10h.01M14 10h.01M18 10h.01M7 14h10"/>"##,
        "settings" => r##"<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 0 0 .3 1.8l.1.1a2 2 0 1 1-2.8 2.8l-.1-.1a1.7 1.7 0 0 0-1.8-.3 1.7 1.7 0 0 0-1 1.5V21a2 2 0 1 1-4 0v-.1a1.7 1.7 0 0 0-1.1-1.5 1.7 1.7 0 0 0-1.8.3l-.1.1a2 2 0 1 1-2.8-2.8l.1-.1a1.7 1.7 0 0 0 .3-1.8 1.7 1.7 0 0 0-1.5-1H3a2 2 0 1 1 0-4h.1a1.7 1.7 0 0 0 1.5-1.1 1.7 1.7 0 0 0-.3-1.8l-.1-.1a2 2 0 1 1 2.8-2.8l.1.1a1.7 1.7 0 0 0 1.8.3H9a1.7 1.7 0 0 0 1-1.5V3a2 2 0 1 1 4 0v.1a1.7 1.7 0 0 0 1 1.5 1.7 1.7 0 0 0 1.8-.3l.1-.1a2 2 0 1 1 2.8 2.8l-.1.1a1.7 1.7 0 0 0-.3 1.8V9a1.7 1.7 0 0 0 1.5 1H21a2 2 0 1 1 0 4h-.1a1.7 1.7 0 0 0-1.5 1z"/>"##,
        "save-as" => r##"<path d="M12 3v9"/><path d="M8 8l4 4 4-4"/><path d="M4 15v4a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-4"/>"##,
        "compress" => r##"<path d="M4 14h6v6"/><path d="M20 10h-6V4"/><path d="M14 10l7-7"/><path d="M3 21l7-7"/>"##,
        "eye" => r##"<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>"##,
        "chevron-left" => r##"<path d="M15 18l-6-6 6-6"/>"##,
        "chevron-right" => r##"<path d="M9 18l6-6-6-6"/>"##,
        "grid" => r##"<rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="3" width="7" height="7" rx="1.5"/><rect x="3" y="14" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/>"##,
        "list" => r##"<path d="M8 6h13M8 12h13M8 18h13M3 6h.01M3 12h.01M3 18h.01"/>"##,
        "sidebar" => r##"<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M15 4v16"/>"##,
        "film" => r##"<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M7 3v18M17 3v18M3 8h4M3 16h4M17 8h4M17 16h4"/>"##,
        "library" => r##"<rect x="3" y="3" width="7" height="9" rx="1.5"/><rect x="14" y="3" width="7" height="5" rx="1.5"/><rect x="14" y="12" width="7" height="9" rx="1.5"/><rect x="3" y="16" width="7" height="5" rx="1.5"/>"##,
        "stack" => r##"<rect x="5" y="9" width="14" height="11" rx="2"/><path d="M7 6h10M9 3h6"/>"##,
        "more" => r##"<circle cx="5" cy="12" r="1.2" fill="#000"/><circle cx="12" cy="12" r="1.2" fill="#000"/><circle cx="19" cy="12" r="1.2" fill="#000"/>"##,
        "play" => r##"<path d="M7 4.5v15l12-7.5z" fill="#000"/>"##,
        "pause" => r##"<rect x="6" y="4.5" width="4" height="15" rx="1" fill="#000"/><rect x="14" y="4.5" width="4" height="15" rx="1" fill="#000"/>"##,
        "scissors" => r##"<circle cx="6" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M20 4L8.1 15.9M14.5 14.5L20 20M8.1 8.1L12 12"/>"##,
        "zoom" => r##"<circle cx="11" cy="11" r="7"/><path d="M21 21l-5-5M11 8v6M8 11h6"/>"##,
        "volume" => r##"<path d="M11 5L6 9H2v6h4l5 4z"/><path d="M15.5 8.5a5 5 0 0 1 0 7M19 5a10 10 0 0 1 0 14"/>"##,
        "plus" => r##"<path d="M12 5v14M5 12h14"/>"##,
        "minus" => r##"<path d="M5 12h14"/>"##,
        _ => return None,
    })
}

pub struct Assets;

impl AssetSource for Assets {
    fn load(&self, path: &str) -> gpui::Result<Option<Cow<'static, [u8]>>> {
        let name = path
            .strip_prefix("icons/")
            .and_then(|p| p.strip_suffix(".svg"));
        Ok(name
            .and_then(body)
            .map(|b| Cow::Owned(format!("{HEAD}{b}</svg>").into_bytes())))
    }

    fn list(&self, _path: &str) -> gpui::Result<Vec<SharedString>> {
        Ok(vec![])
    }
}
