//! Drawings shared by several shots: the mock app, the desktop, cards, the editor and Studio.
use crate::motion::*;
use crate::{ACCENT, AMBER, HOT, INK, MONO, MUTED, REC, UI, VIOLET, W};
use fframes::Svgr;

/// The captured region on the 1920x1080 desktop, and the size of the mock app inside it.
pub const SHOT_X: f32 = 420.0;
pub const SHOT_Y: f32 = 250.0;
pub const SHOT_W: f32 = 1080.0;
pub const SHOT_H: f32 = 620.0;
/// The app window around the shot.
pub const WIN: (f32, f32, f32, f32) = (400.0, 210.0, 1120.0, 680.0);
/// The click target in shot coordinates: a peak of the chart.
pub const PEAK: (f32, f32) = (564.0, 322.0);
const CHART: [(f32, f32); 9] = [
    (228.0, 520.0),
    (284.0, 498.0),
    (340.0, 468.0),
    (396.0, 482.0),
    (452.0, 418.0),
    (508.0, 400.0),
    (564.0, 322.0),
    (620.0, 348.0),
    (676.0, 286.0),
];

pub fn tf(x: f32, y: f32, s: f32) -> String {
    format!("translate({x:.2} {y:.2}) scale({s:.4})")
}

/// Scales by `s` around the point `cx, cy`.
pub fn about(cx: f32, cy: f32, s: f32) -> String {
    format!(
        "translate({cx:.2} {cy:.2}) scale({s:.4}) translate({:.2} {:.2})",
        -cx, -cy
    )
}

pub fn px(x: f32) -> f32 {
    x.max(0.5)
}

/// The four-pointed glint star, centred on 0,0.
pub fn star_path(r: f32) -> String {
    let a = r * 0.14;
    format!(
        "M0 {} Q{a} {} {r} 0 Q{a} {a} 0 {r} Q{} {a} {} 0 Q{} {} 0 {} Z",
        -r, -a, -a, -r, -a, -a, -r
    )
}

pub fn cursor<'a>(x: f32, y: f32, opacity: f32) -> Svgr<'a> {
    fframes::svgr!(<g transform={tf(x, y, 1.5)} opacity={opacity}>
        <path d="M0 0 L0 23 L6.2 17.6 L10.4 27 L14.6 25.2 L10.6 16 L18 16 Z" fill="#FFFFFF" stroke="#0A0A0A" stroke-width="1.4" stroke-linejoin="round" />
    </g>)
}

/// A click: a ring that grows and fades from the cursor tip.
pub fn ripple<'a>(x: f32, y: f32, t: f32, at: f32, color: &'a str) -> Svgr<'a> {
    let p = seg(t, at, 0.45);
    if p <= 0.0 || p >= 1.0 {
        return Svgr::empty();
    }
    let r = 8.0 + 46.0 * out_cubic(p);
    fframes::svgr!(<circle cx={x} cy={y} r={r} fill="none" stroke={color} stroke-width={4.0 * (1.0 - p) + 0.5} opacity={1.0 - p} />)
}

/// The capture flash: white, gone after two frames.
pub fn flash<'a>(t: f32, at: f32) -> Svgr<'a> {
    let p = seg(t, at, 0.04);
    if t < at || p >= 1.0 {
        return Svgr::empty();
    }
    fframes::svgr!(<rect width="1920" height="1080" fill="#FFFFFF" opacity={0.8 * (1.0 - p)} />)
}

/// A Windows-style toast: dark pill, green check, one line of text, dropping in from above.
pub fn toast<'a>(cx: f32, y: f32, text: &'a str, width: f32, p: f32) -> Svgr<'a> {
    if p <= 0.0 {
        return Svgr::empty();
    }
    let e = out_expo(p);
    let x0 = -width / 2.0;
    fframes::svgr!(<g transform={format!("translate({cx:.1} {:.1}) scale({:.4})", lerp(y - 70.0, y, e), lerp(0.9, 1.0, e))} opacity={c01(p * 3.0)}>
        <rect x={x0 + 4.0} y="-20" width={width - 8.0} height="64" rx="32" fill="#000000" opacity="0.45" />
        <rect x={x0} y="-32" width={width} height="64" rx="32" fill="#202020" stroke="#FFFFFF" stroke-opacity="0.14" />
        <circle cx={x0 + 36.0} cy="0" r="15" fill="#22C55E" />
        <path d={format!("M{} 0 l5 5 l9 -10", x0 + 29.0)} stroke="#FFFFFF" stroke-width="2.8" fill="none" stroke-linecap="round" stroke-linejoin="round" />
        <text x={x0 + 64.0} y="10" font-family={UI} font-weight="600" font-size="28" fill={INK}>{text}</text>
    </g>)
}

/// A keycap chip, scaled by `s` around its centre.
pub fn keycap<'a>(label: &'a str, x: f32, y: f32, w: f32, s: f32) -> Svgr<'a> {
    if s <= 0.0 {
        return Svgr::empty();
    }
    fframes::svgr!(<g transform={format!("translate({x:.1} {y:.1}) scale({s:.3})")} opacity={c01(s)}>
        <rect x={-w / 2.0} y="-34" width={w} height="68" rx="14" fill="#111318" fill-opacity="0.92" stroke="#FFFFFF" stroke-opacity="0.25" />
        <rect x={-w / 2.0} y="28" width={w} height="6" rx="3" fill="#000000" opacity="0.4" />
        <text y="12" text-anchor="middle" font-family={UI} font-weight="600" font-size="32" fill={INK}>{label}</text>
    </g>)
}

/// A Windows 11 toggle switch, `on` from 0 to 1.
pub fn toggle<'a>(x: f32, y: f32, on: f32) -> Svgr<'a> {
    let fill = if on > 0.5 { ACCENT } else { "#3A3A3A" };
    fframes::svgr!(<g>
        <rect x={x} y={y} width="60" height="32" rx="16" fill={fill} stroke="#FFFFFF" stroke-opacity={0.3 * (1.0 - on)} />
        <circle cx={x + 16.0 + 28.0 * out_back(on).min(1.08)} cy={y + 16.0} r={lerp(8.0, 10.0, on)} fill="#FFFFFF" />
    </g>)
}

// ---------------------------------------------------------------- the mock app

/// The mock app being captured, drawn in its own 1080x620 coordinates.
/// `redact` sweeps pixel blocks over the email and the API key from left to right.
pub fn shot<'a>(id: &str, redact: f32) -> Svgr<'a> {
    let fill_id = format!("{id}-fill");
    let fill_url = format!("url(#{fill_id})");
    let line: String = CHART
        .iter()
        .enumerate()
        .map(|(i, (x, y))| format!("{}{x} {y} ", if i == 0 { "M" } else { "L" }))
        .collect();
    let area = format!("{line}L676 566 L228 566 Z");
    let dots: Vec<Svgr> = CHART
        .iter()
        .skip(1)
        .step_by(2)
        .map(|(x, y)| fframes::svgr!(<circle cx={*x} cy={*y} r="5" fill="#FFFFFF" stroke={ACCENT} stroke-width="3" />))
        .collect();
    let blocks = |x0: f32, y0: f32, w: f32, seed: u32| -> Vec<Svgr<'a>> {
        let cols = (w / 10.0) as u32;
        let shown = (cols as f32 * c01(redact)).ceil() as u32;
        (0..shown)
            .flat_map(|c| (0..3u32).map(move |r| (c, r)))
            .map(|(c, r)| {
                let g = 0x86 + (hash(seed + c * 7 + r * 131) * 70.0) as u32;
                let fill = format!("#{g:02X}{:02X}{:02X}", g + 4, g + 14);
                fframes::svgr!(<rect x={x0 + c as f32 * 10.0} y={y0 + r as f32 * 10.0} width="10" height="10" fill={fill} />)
            })
            .collect()
    };
    let email_blocks = blocks(744.0, 434.0, 250.0, 11);
    let key_blocks = blocks(744.0, 514.0, 290.0, 977);
    let edge = if redact > 0.0 && redact < 1.0 {
        let x = 744.0 + 290.0 * redact;
        fframes::svgr!(<rect x={x} y="420" width="3" height="134" fill={ACCENT} opacity="0.9" />)
    } else {
        Svgr::empty()
    };
    fframes::svgr!(<g id={id.to_string()}>
        <defs>
            <linearGradient id={fill_id} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stop-color={ACCENT} stop-opacity="0.35" />
                <stop offset="1" stop-color={ACCENT} stop-opacity="0" />
            </linearGradient>
        </defs>
        <rect width="1080" height="620" fill="#F6F7F9" />
        <rect width="180" height="620" fill="#FFFFFF" />
        <rect x="180" width="1" height="620" fill="#E6E8EC" />
        <circle cx="34" cy="38" r="12" fill="#111827" />
        <text x="56" y="45" font-family={UI} font-weight="600" font-size="19" fill="#111827">"Northwind"</text>
        <rect x="16" y="88" width="148" height="38" rx="8" fill="#EAF0FF" />
        <rect x="34" y="104" width="76" height="8" rx="4" fill={ACCENT} />
        <rect x="34" y="154" width="92" height="8" rx="4" fill="#C9CED8" />
        <rect x="34" y="194" width="64" height="8" rx="4" fill="#C9CED8" />
        <rect x="34" y="234" width="84" height="8" rx="4" fill="#C9CED8" />
        <rect x="34" y="274" width="58" height="8" rx="4" fill="#C9CED8" />
        <text x="212" y="64" font-family={UI} font-weight="600" font-size="26" fill="#111827">"Revenue"</text>
        <text x="212" y="140" font-family={UI} font-weight="700" font-size="58" fill="#0B1220">"$84,920"</text>
        <rect x="468" y="104" width="92" height="32" rx="16" fill="#DCFCE7" />
        <text x="514" y="127" text-anchor="middle" font-family={UI} font-weight="600" font-size="18" fill="#15803D">"+12.4%"</text>
        <rect x="204" y="170" width="496" height="420" rx="16" fill="#FFFFFF" stroke="#E6E8EC" />
        <text x="228" y="214" font-family={UI} font-weight="600" font-size="18" fill="#374151">"Last 30 days"</text>
        <path d="M228 330 H676 M228 410 H676 M228 490 H676" stroke="#EEF0F3" stroke-width="2" />
        <path d={area} fill={fill_url} />
        <path d={line} fill="none" stroke={ACCENT} stroke-width="4" stroke-linejoin="round" stroke-linecap="round" />
        {dots}
        <rect x="720" y="170" width="160" height="160" rx="16" fill="#FFFFFF" stroke="#E6E8EC" />
        <text x="740" y="210" font-family={UI} font-size="17" fill="#6B7280">"Orders"</text>
        <text x="740" y="262" font-family={UI} font-weight="700" font-size="36" fill="#0B1220">"1,284"</text>
        <rect x="896" y="170" width="160" height="160" rx="16" fill="#FFFFFF" stroke="#E6E8EC" />
        <text x="916" y="210" font-family={UI} font-size="17" fill="#6B7280">"Refunds"</text>
        <text x="916" y="262" font-family={UI} font-weight="700" font-size="36" fill="#0B1220">"0.8%"</text>
        <rect x="720" y="350" width="336" height="240" rx="16" fill="#FFFFFF" stroke="#E6E8EC" />
        <text x="744" y="392" font-family={UI} font-weight="600" font-size="18" fill="#374151">"Account"</text>
        <text x="744" y="424" font-family={UI} font-size="15" fill="#6B7280">"Owner"</text>
        <text x="744" y="458" font-family={UI} font-size="21" fill="#111827">"rob@northwind.dev"</text>
        <text x="744" y="504" font-family={UI} font-size="15" fill="#6B7280">"API key"</text>
        <text x="744" y="538" font-family={MONO} font-size="19" fill="#111827">"sk_live_51Hx2...9fQa"</text>
        {email_blocks}
        {key_blocks}
        {edge}
    </g>)
}

pub fn wallpaper<'a>(id: &str) -> Svgr<'a> {
    fframes::svgr!(<g id={id.to_string()}>
        <defs>
            <linearGradient id="wall" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#0E1636" />
                <stop offset="0.55" stop-color="#241552" />
                <stop offset="1" stop-color="#0A0E1E" />
            </linearGradient>
            <filter id="wall-blur" x="-50%" y="-50%" width="200%" height="200%">
                <feGaussianBlur stdDeviation="90" />
            </filter>
        </defs>
        <rect width="1920" height="1080" fill="url(#wall)" />
        <g filter="url(#wall-blur)" opacity="0.8">
            <circle cx="1560" cy="220" r="260" fill="#5B3FD9" />
            <circle cx="300" cy="860" r="300" fill="#1F6FEB" />
            <circle cx="1300" cy="980" r="220" fill="#C2417A" />
        </g>
    </g>)
}

/// The app window around the shot, with its drop shadow.
pub fn app_window<'a>(id: &str, redact: f32) -> Svgr<'a> {
    let shot = shot(id, redact);
    fframes::svgr!(<g id={format!("{id}-win")}>
        <defs>
            <filter id="win-shadow" x="-20%" y="-20%" width="140%" height="160%">
                <feGaussianBlur stdDeviation="28" />
            </filter>
        </defs>
        <rect x="410" y="240" width="1100" height="680" rx="12" fill="#000000" opacity="0.55" filter="url(#win-shadow)" />
        <rect x="400" y="210" width="1120" height="680" rx="10" fill="#F6F7F9" />
        <rect x="400" y="210" width="1120" height="40" rx="10" fill="#EEF0F3" />
        <rect x="400" y="236" width="1120" height="14" fill="#EEF0F3" />
        <text x="420" y="236" font-family={UI} font-size="15" fill="#4B5563">"Northwind Analytics"</text>
        <path d="M1406 230 H1418 M1446 224 H1458 V236 H1446 Z M1488 224 L1500 236 M1500 224 L1488 236" stroke="#4B5563" stroke-width="1.4" fill="none" />
        <g transform="translate(420 250)">{shot}</g>
    </g>)
}

pub fn taskbar<'a>(id: &str) -> Svgr<'a> {
    fframes::svgr!(<g id={id.to_string()}>
        <rect x="0" y="1032" width="1920" height="48" fill="#0D0F16" opacity="0.88" />
        <rect x="852" y="1040" width="32" height="32" rx="7" fill="#2B6CF6" />
        <rect x="896" y="1040" width="32" height="32" rx="7" fill="#F2B530" />
        <rect x="940" y="1040" width="32" height="32" rx="7" fill="#7C3AED" />
        <rect x="984" y="1040" width="32" height="32" rx="7" fill="#1F2937" stroke="#FFFFFF" stroke-opacity="0.25" />
        <path d="M1000 1045 Q1001.5 1054.5 1011 1056 Q1001.5 1057.5 1000 1067 Q998.5 1057.5 989 1056 Q998.5 1054.5 1000 1045 Z" fill="#FFFFFF" />
        <rect x="1028" y="1040" width="32" height="32" rx="7" fill="#10B981" />
    </g>)
}

/// The desktop: wallpaper, the app window around the shot, and the taskbar.
pub fn desktop<'a>(id: &str, redact: f32) -> Svgr<'a> {
    let wall = wallpaper(&format!("{id}-wall"));
    let win = app_window(id, redact);
    let bar = taskbar(&format!("{id}-bar"));
    fframes::svgr!(<g>{wall}{win}{bar}</g>)
}

/// A rounded-corner clip of `content` drawn into the rect, with `content` sized `cw` wide.
pub fn framed<'a>(
    id: &str,
    x: f32,
    y: f32,
    w: f32,
    h: f32,
    rx: f32,
    cw: f32,
    content: Svgr<'a>,
) -> Svgr<'a> {
    let clip = format!("{id}-clip");
    let clip_url = format!("url(#{clip})");
    let k = w / cw;
    fframes::svgr!(<g>
        <defs><clipPath id={clip}><rect x={x} y={y} width={px(w)} height={px(h)} rx={rx} /></clipPath></defs>
        <g clip-path={clip_url}><g transform={tf(x, y, k)}>{content}</g></g>
    </g>)
}

pub fn thumb_code<'a>(id: String) -> Svgr<'a> {
    fframes::svgr!(<g id={id}>
        <rect width="1080" height="620" fill="#141821" />
        <rect width="1080" height="56" fill="#1B2030" />
        <circle cx="34" cy="28" r="9" fill="#FF5F57" /><circle cx="62" cy="28" r="9" fill="#FEBC2E" /><circle cx="90" cy="28" r="9" fill="#28C840" />
        <rect x="60" y="110" width="320" height="22" rx="11" fill="#C678DD" />
        <rect x="400" y="110" width="220" height="22" rx="11" fill="#61AFEF" />
        <rect x="100" y="170" width="520" height="22" rx="11" fill="#98C379" />
        <rect x="100" y="230" width="380" height="22" rx="11" fill="#E5C07B" />
        <rect x="140" y="290" width="600" height="22" rx="11" fill="#56B6C2" />
        <rect x="140" y="350" width="260" height="22" rx="11" fill="#E06C75" />
        <rect x="100" y="410" width="440" height="22" rx="11" fill="#61AFEF" />
        <rect x="60" y="470" width="180" height="22" rx="11" fill="#C678DD" />
    </g>)
}

pub fn thumb_photo<'a>(id: String) -> Svgr<'a> {
    let sky = format!("{id}-sky");
    let sky_url = format!("url(#{sky})");
    fframes::svgr!(<g id={id}>
        <defs>
            <linearGradient id={sky} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stop-color="#FF9A62" /><stop offset="0.6" stop-color="#C2417A" /><stop offset="1" stop-color="#3B1D5C" />
            </linearGradient>
        </defs>
        <rect width="1080" height="620" fill={sky_url} />
        <circle cx="720" cy="300" r="110" fill="#FFE2A8" opacity="0.9" />
        <path d="M0 470 L180 330 L330 440 L520 280 L760 470 L900 380 L1080 470 V620 H0 Z" fill="#2A1640" />
        <path d="M0 540 L220 430 L420 520 L640 410 L860 540 L1080 470 V620 H0 Z" fill="#1A0E2C" />
    </g>)
}

/// One preview card `w` wide (same aspect as the shot), with its shadow and hairline border.
pub fn card<'a>(id: &str, x: f32, y: f32, w: f32, content: Svgr<'a>, opacity: f32) -> Svgr<'a> {
    let h = w * SHOT_H / SHOT_W;
    let inner = framed(id, x, y, w, h, 14.0, SHOT_W, content);
    fframes::svgr!(<g opacity={opacity}>
        <rect x={x + 6.0} y={y + 18.0} width={w - 12.0} height={h - 8.0} rx="14" fill="#000000" opacity="0.5" />
        {inner}
        <rect x={x} y={y} width={w} height={h} rx="14" fill="none" stroke="#FFFFFF" stroke-opacity="0.18" />
    </g>)
}

// ---------------------------------------------------------------- the editor

pub const TOOLS: [&str; 8] = [
    "M-6 -9 L-6 7 L-2 3 L1 10 L4 9 L1 2 L6 2 Z",
    "M-9 -7 H9 V7 H-9 Z",
    "M-8 8 L8 -8 M1 -8 H8 V-1",
    "M-8 -8 H8 M0 -8 V9",
    "M0 -9 A9 9 0 1 1 -0.1 -9 M-2 -3 L1 -5 V5",
    "M-9 -9 h6 v6 h-6 Z M3 -9 h6 v6 h-6 Z M-9 3 h6 v6 h-6 Z M3 3 h6 v6 h-6 Z",
    "M-9 -4 V-9 H-4 M4 -9 H9 V-4 M9 4 V9 H4 M-4 9 H-9 V4",
    "M-8 4 L4 -8 L8 -4 L-4 8 Z M-8 8 L-4 8",
];
pub const T_ARROW: usize = 2;
pub const T_RECT: usize = 1;
pub const T_MARKER: usize = 4;
pub const T_PIXEL: usize = 5;
/// Scale of the shot on the editor canvas, and the canvas centre.
pub const CANVAS_K: f32 = 0.82;
pub const CANVAS_C: (f32, f32) = (780.0, 566.0);

pub fn tool_x(i: f32) -> f32 {
    780.0 - 3.5 * 56.0 + i * 56.0
}

#[derive(Clone, Copy, PartialEq)]
pub enum Bg {
    Gradient,
    Solid,
    Wallpaper,
}

pub const SOLID: &str = "#F2B530";

/// A background fill for the rect `w`x`h` at the origin.
pub fn bg_fill<'a>(id: &str, bg: Bg, w: f32, h: f32) -> Svgr<'a> {
    match bg {
        Bg::Gradient => fframes::svgr!(<rect width={px(w)} height={px(h)} fill="url(#bg-grad)" />),
        Bg::Solid => fframes::svgr!(<rect width={px(w)} height={px(h)} fill={SOLID} />),
        Bg::Wallpaper => {
            let photo = thumb_photo(format!("{id}-photo"));
            fframes::svgr!(<g transform={format!("scale({:.4} {:.4})", w / SHOT_W, h / SHOT_H)}>{photo}</g>)
        }
    }
}

pub fn bg_defs<'a>() -> Svgr<'a> {
    fframes::svgr!(<defs>
        <linearGradient id="bg-grad" x1="0" y1="0" x2="1" y2="1">
            <stop offset="0" stop-color="#5B8CFF" />
            <stop offset="0.5" stop-color="#A78BFA" />
            <stop offset="1" stop-color="#F472B6" />
        </linearGradient>
        <filter id="frame-shadow" x="-20%" y="-20%" width="140%" height="160%"><feGaussianBlur stdDeviation="24" /></filter>
    </defs>)
}

/// Annotation progress, in shot coordinates.
#[derive(Clone, Copy)]
pub struct Marks {
    pub arrow: f32,
    pub rect: f32,
    /// Spring value of each numbered marker.
    pub markers: [f32; 3],
}

impl Marks {
    pub const DONE: Marks = Marks {
        arrow: 1.0,
        rect: 1.0,
        markers: [1.0; 3],
    };
}

pub const ARROW: ((f32, f32), (f32, f32), (f32, f32)) =
    ((905.0, 64.0), (760.0, 6.0), (586.0, 108.0));
pub const RECT: (f32, f32, f32, f32) = (524.0, 288.0, 84.0, 66.0);
pub const MARKERS: [(f32, f32); 3] = [(446.0, 78.0), (512.0, 284.0), (712.0, 366.0)];

pub fn quad(p0: (f32, f32), c: (f32, f32), p1: (f32, f32), s: f32) -> (f32, f32) {
    let u = 1.0 - s;
    (
        u * u * p0.0 + 2.0 * u * s * c.0 + s * s * p1.0,
        u * u * p0.1 + 2.0 * u * s * c.1 + s * s * p1.1,
    )
}

fn quad_len(p0: (f32, f32), c: (f32, f32), p1: (f32, f32)) -> f32 {
    (1..=32)
        .map(|i| {
            let (a, b) = (
                quad(p0, c, p1, (i - 1) as f32 / 32.0),
                quad(p0, c, p1, i as f32 / 32.0),
            );
            ((b.0 - a.0).powi(2) + (b.1 - a.1).powi(2)).sqrt()
        })
        .sum()
}

/// The point the rectangle's stroke has reached at progress `p`.
pub fn rect_point(p: f32) -> (f32, f32) {
    let (x, y, w, h) = RECT;
    let d = p * 2.0 * (w + h);
    if d < w {
        (x + d, y)
    } else if d < w + h {
        (x + w, y + d - w)
    } else if d < 2.0 * w + h {
        (x + w - (d - w - h), y + h)
    } else {
        (x, y + h - (d - 2.0 * w - h))
    }
}

pub fn annotations<'a>(m: Marks) -> Svgr<'a> {
    let (a0, ac, a1) = ARROW;
    let alen = quad_len(a0, ac, a1);
    let tip = quad(a0, ac, a1, m.arrow);
    let before = quad(a0, ac, a1, (m.arrow - 0.04).max(0.0));
    let ang = (tip.1 - before.1).atan2(tip.0 - before.0).to_degrees();
    let head = out_back(c01(m.arrow * 3.0));
    let arrow = if m.arrow > 0.0 {
        fframes::svgr!(<g>
            <path d={format!("M{} {} Q{} {} {} {}", a0.0, a0.1, ac.0, ac.1, a1.0, a1.1)} fill="none" stroke={HOT} stroke-width="8" stroke-linecap="round"
                stroke-dasharray={format!("{alen:.1} {alen:.1}")} stroke-dashoffset={alen * (1.0 - m.arrow)} />
            <g transform={format!("translate({:.1} {:.1}) rotate({ang:.1}) scale({head:.3})", tip.0, tip.1)}>
                <path d="M8 0 L-16 -13 L-11 0 L-16 13 Z" fill={HOT} stroke={HOT} stroke-width="3" stroke-linejoin="round" />
            </g>
        </g>)
    } else {
        Svgr::empty()
    };
    let (rx0, ry0, rw, rh) = RECT;
    let rlen = 2.0 * (rw + rh);
    let rect = if m.rect > 0.0 {
        fframes::svgr!(<rect x={rx0} y={ry0} width={rw} height={rh} rx="10" fill={AMBER} fill-opacity={0.14 * m.rect} stroke={AMBER} stroke-width="6"
            stroke-dasharray={format!("{rlen:.1} {rlen:.1}")} stroke-dashoffset={rlen * (1.0 - m.rect)} />)
    } else {
        Svgr::empty()
    };
    let markers: Vec<Svgr> = MARKERS
        .iter()
        .zip(m.markers)
        .enumerate()
        .map(|(i, ((x, y), s))| {
            if s <= 0.0 {
                return Svgr::empty();
            }
            let n = (i + 1).to_string();
            fframes::svgr!(<g transform={format!("translate({x} {y}) scale({s:.3})")}>
                <circle r="22" fill={HOT} stroke="#FFFFFF" stroke-width="4" />
                <text y="9" text-anchor="middle" font-family={UI} font-weight="700" font-size="26" fill="#FFFFFF">{n}</text>
            </g>)
        })
        .collect();
    fframes::svgr!(<g>{arrow}{rect}{markers}</g>)
}

/// The styled image: background with padding, shadow, the shot with rounded corners and its
/// annotations. Drawn with the frame's top-left at the origin; returns it with the frame size.
pub fn composition<'a>(
    id: &str,
    k: f32,
    pad: f32,
    bg: Bg,
    next: Option<(Bg, f32)>,
    redact: f32,
    marks: Marks,
) -> (Svgr<'a>, f32, f32) {
    let (iw, ih) = (SHOT_W * k, SHOT_H * k);
    let (fw, fh) = (iw + 2.0 * pad, ih + 2.0 * pad);
    let frame_clip = format!("{id}-frame");
    let img_clip = format!("{id}-img");
    let base = bg_fill(&format!("{id}-a"), bg, fw, fh);
    // The next background wipes in from the left edge.
    let wipe = match next {
        Some((b, p)) if p > 0.0 => {
            let fill = bg_fill(&format!("{id}-b"), b, fw, fh);
            let wipe_clip = format!("{id}-wipe");
            fframes::svgr!(<g>
                <defs><clipPath id={wipe_clip.clone()}><rect width={px(fw * in_out(p))} height={fh} /></clipPath></defs>
                <g clip-path={format!("url(#{wipe_clip})")}>{fill}</g>
            </g>)
        }
        _ => Svgr::empty(),
    };
    let content = shot(&format!("{id}-shot"), redact);
    let ann = annotations(marks);
    let svg = fframes::svgr!(<g id={id.to_string()}>
        <defs>
            <clipPath id={frame_clip.clone()}><rect width={px(fw)} height={px(fh)} rx="14" /></clipPath>
            <clipPath id={img_clip.clone()}><rect x={pad} y={pad} width={iw} height={ih} rx="16" /></clipPath>
        </defs>
        <rect x="10" y="26" width={px(fw - 20.0)} height={px(fh - 10.0)} rx="18" fill="#000000" opacity="0.55" filter="url(#frame-shadow)" />
        <g clip-path={format!("url(#{frame_clip})")}>{base}{wipe}</g>
        <rect x={pad + 6.0} y={pad + 16.0} width={iw - 12.0} height={ih - 8.0} rx="16" fill="#000000" opacity="0.35" />
        <g clip-path={format!("url(#{img_clip})")}><g transform={tf(pad, pad, k)}>{content}</g></g>
        <g transform={tf(pad, pad, k)}>{ann}</g>
    </g>);
    (svg, fw, fh)
}

/// Everything the editor window shows at one moment.
pub struct EditorView {
    /// Toolbar indicator position, in tool slots (fractional while it hops).
    pub tool: f32,
    pub lit: usize,
    pub marks: Marks,
    pub redact: f32,
    pub pad: f32,
    pub bg: Bg,
    pub next: Option<(Bg, f32)>,
    /// Selection ring position among the background swatches.
    pub swatch: f32,
    pub redaction_on: f32,
}

impl EditorView {
    /// Screen position of a point in shot coordinates.
    pub fn to_screen(&self, p: (f32, f32)) -> (f32, f32) {
        let (fw, fh) = (
            SHOT_W * CANVAS_K + 2.0 * self.pad,
            SHOT_H * CANVAS_K + 2.0 * self.pad,
        );
        (
            CANVAS_C.0 - fw / 2.0 + self.pad + p.0 * CANVAS_K,
            CANVAS_C.1 - fh / 2.0 + self.pad + p.1 * CANVAS_K,
        )
    }

    pub fn render<'a>(&self, id: &str) -> Svgr<'a> {
        let (comp, fw, fh) = composition(
            &format!("{id}-comp"),
            CANVAS_K,
            self.pad,
            self.bg,
            self.next,
            self.redact,
            self.marks,
        );
        let ind_x = tool_x(self.tool);
        let tools: Vec<Svgr> = TOOLS
            .iter()
            .enumerate()
            .map(|(i, d)| {
                let stroke = if i == self.lit { "#FFFFFF" } else { "#C9CCD4" };
                fframes::svgr!(<path d={*d} transform={format!("translate({:.1} 72)", tool_x(i as f32))} stroke={stroke} stroke-width="1.8" fill="none" stroke-linecap="round" stroke-linejoin="round" />)
            })
            .collect();
        let knob = |y: f32, v: f32| -> Svgr<'a> {
            let x = 1590.0 + 300.0 * v;
            fframes::svgr!(<g>
                <rect x="1590" y={y - 2.0} width="300" height="4" rx="2" fill="#FFFFFF" fill-opacity="0.16" />
                <rect x="1590" y={y - 2.0} width={px(300.0 * v)} height="4" rx="2" fill={ACCENT} />
                <circle cx={x} cy={y} r="10" fill="#FFFFFF" stroke={ACCENT} stroke-width="3" />
            </g>)
        };
        let ring_x = 1612.0 + 56.0 * self.swatch;
        fframes::svgr!(<g>
            {bg_defs()}
            <rect width="1920" height="1080" fill="#141414" />
            <rect width="1920" height="44" fill="#202020" />
            <path d={star_path(9.0)} transform="translate(26 22)" fill={ACCENT} />
            <text x="46" y="29" font-family={UI} font-size="15" fill={MUTED}>"Glint"</text>
            <path d="M1786 22 H1798 M1832 16 H1844 V28 H1832 Z M1878 16 L1890 28 M1890 16 L1878 28" stroke="#C9CCD4" stroke-width="1.4" fill="none" />
            <rect y="44" width="1560" height="56" fill="#1A1A1A" />
            <rect x={ind_x - 22.0} y="50" width="44" height="44" rx="6" fill={ACCENT} fill-opacity="0.28" />
            <rect x={ind_x - 10.0} y="92" width="20" height="3" rx="1.5" fill={ACCENT} />
            {tools}
            <rect y="1028" width="1560" height="52" fill="#1A1A1A" />
            <rect x="1358" y="1036" width="88" height="36" rx="6" fill="#FFFFFF" fill-opacity="0.08" />
            <text x="1402" y="1062" text-anchor="middle" font-family={UI} font-size="20" fill={INK}>"Save"</text>
            <rect x="1456" y="1036" width="88" height="36" rx="6" fill={ACCENT} />
            <text x="1500" y="1062" text-anchor="middle" font-family={UI} font-weight="600" font-size="20" fill="#FFFFFF">"Done"</text>
            <g transform={format!("translate({:.1} {:.1})", CANVAS_C.0 - fw / 2.0, CANVAS_C.1 - fh / 2.0)}>{comp}</g>
            <rect x="1560" y="44" width="360" height="1036" fill="#1C1C1C" />
            <rect x="1560" y="44" width="1" height="1036" fill="#FFFFFF" fill-opacity="0.08" />
            <text x="1590" y="150" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Background"</text>
            <circle cx="1612" cy="200" r="20" fill="url(#bg-grad)" />
            <circle cx="1668" cy="200" r="20" fill={SOLID} />
            <circle cx="1724" cy="200" r="20" fill="#C2417A" />
            <circle cx="1724" cy="206" r="9" fill="#FFE2A8" />
            <circle cx="1780" cy="200" r="20" fill="#10B981" />
            <circle cx="1836" cy="200" r="20" fill="#E5E7EB" />
            <circle cx={ring_x} cy="200" r="26" fill="none" stroke="#FFFFFF" stroke-width="2.5" />
            <text x="1590" y="290" font-family={UI} font-size="28" fill={MUTED}>"Padding"</text>
            {knob(326.0, self.pad / 140.0)}
            <text x="1590" y="402" font-family={UI} font-size="28" fill={MUTED}>"Corners"</text>
            {knob(438.0, 0.55)}
            <text x="1590" y="514" font-family={UI} font-size="28" fill={MUTED}>"Shadow"</text>
            {knob(550.0, 0.65)}
            <rect x="1576" y="610" width="328" height="128" rx="12" fill="#FFFFFF" fill-opacity={0.04 + 0.06 * self.redaction_on} stroke={ACCENT} stroke-opacity={0.8 * self.redaction_on} />
            <text x="1600" y="664" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Smart redaction"</text>
            {toggle(1600.0, 690.0, self.redaction_on)}
        </g>)
    }
}

// ---------------------------------------------------------------- Studio

/// Where the camera looks (`fx, fy`) and how far it is zoomed; `z` 1.75 centres the focus.
pub fn camera_transform(fx: f32, fy: f32, z: f32) -> String {
    let k = (z - 1.0) / 0.75 * 0.65;
    let sx = lerp(fx, 960.0, k);
    let sy = lerp(fy, 520.0, k);
    format!(
        "translate({sx:.2} {sy:.2}) scale({z:.4}) translate({:.2} {:.2})",
        -fx, -fy
    )
}

pub fn camera_point(fx: f32, fy: f32, z: f32, p: (f32, f32)) -> (f32, f32) {
    let k = (z - 1.0) / 0.75 * 0.65;
    (
        lerp(fx, 960.0, k) + (p.0 - fx) * z,
        lerp(fy, 520.0, k) + (p.1 - fy) * z,
    )
}

/// The Studio preview: the recorded screen scaled into a card inside a padded frame.
pub const PREVIEW: (f32, f32, f32, f32) = (358.0, 117.0, 864.0, 486.0);

/// Studio's window with the recording in the preview, `head` the playhead from 0 to 1.
pub fn studio<'a>(id: &str, head: f32, recorded: Svgr<'a>, overlay: Svgr<'a>) -> Svgr<'a> {
    let (px0, py0, pw, ph) = PREVIEW;
    let (tl_x, tl_w) = (100.0, 1380.0);
    let hx = tl_x + tl_w * head;
    let clip = format!("{id}-preview");
    let tiles: Vec<Svgr> = (0..10)
        .map(|i| {
            let x = tl_x + i as f32 * 138.0;
            fframes::svgr!(<g>
                <rect x={x + 2.0} y="764" width="134" height="64" rx="4" fill="#2A1C55" />
                <rect x={x + 34.0} y="776" width="70" height="40" rx="3" fill="#F6F7F9" />
                <path d={format!("M{} 806 L{} 798 L{} 801 L{} 788", x + 40.0, x + 60.0, x + 76.0, x + 98.0)} stroke={ACCENT} stroke-width="2.5" fill="none" />
            </g>)
        })
        .collect();
    let bars: Vec<Svgr> = (0..172)
        .map(|i| {
            let x = tl_x + i as f32 * 8.0;
            let env = 0.35 + 0.65 * ((i as f32 * 0.21).sin() * 0.5 + 0.5) * hash(i as u32 + 40);
            let hgt = px(54.0 * env);
            let fill = if x < hx { "#34D399" } else { "#2F6F5A" };
            fframes::svgr!(<rect x={x} y={951.0 - hgt / 2.0} width="4" height={hgt} rx="2" fill={fill} />)
        })
        .collect();
    let ticks: String = (0..=27)
        .map(|i| {
            format!(
                "M{} 728 v{} ",
                tl_x + i as f32 * 51.1,
                if i % 5 == 0 { 14 } else { 7 }
            )
        })
        .collect();
    let time = format!("0:{:02}", (head * 12.0) as i32);
    fframes::svgr!(<g>
        <defs>
            <linearGradient id="studio-bg" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#5B8CFF" /><stop offset="0.5" stop-color="#A78BFA" /><stop offset="1" stop-color="#F472B6" />
            </linearGradient>
            <clipPath id={clip.clone()}><rect x={px0} y={py0} width={pw} height={ph} rx="12" /></clipPath>
        </defs>
        <rect width="1920" height="1080" fill="#141414" />
        <rect width="1920" height="44" fill="#202020" />
        <path d={star_path(9.0)} transform="translate(26 22)" fill={ACCENT} />
        <text x="46" y="29" font-family={UI} font-size="15" fill={MUTED}>"Glint Studio"</text>
        <rect x="310" y="90" width="960" height="540" rx="14" fill="url(#studio-bg)" />
        <rect x={px0 + 4.0} y={py0 + 12.0} width={pw - 8.0} height={ph - 4.0} rx="12" fill="#000000" opacity="0.35" />
        <g clip-path={format!("url(#{clip})")}><g transform={tf(px0, py0, pw / W)}>{recorded}</g></g>
        {overlay}
        <rect x="1560" y="44" width="360" height="1036" fill="#1C1C1C" />
        <text x="1590" y="150" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Auto zoom"</text>
        {toggle(1824.0, 126.0, 1.0)}
        <text x="1590" y="230" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Captions"</text>
        {toggle(1824.0, 206.0, 1.0)}
        <text x="1590" y="310" font-family={UI} font-size="28" fill={MUTED}>"Cursor size"</text>
        <rect x="1590" y="338" width="300" height="4" rx="2" fill={ACCENT} />
        <circle cx="1770" cy="340" r="10" fill="#FFFFFF" stroke={ACCENT} stroke-width="3" />
        <rect y="680" width="1560" height="400" fill="#181818" />
        <path d={ticks} stroke="#FFFFFF" stroke-opacity="0.3" stroke-width="2" />
        <rect x={tl_x} y="760" width={tl_w} height="72" rx="8" fill="#2A1C55" />
        {tiles}
        <rect x={tl_x} y="760" width={tl_w} height="72" rx="8" fill="none" stroke={ACCENT} stroke-width="2" />
        <rect x="460" y="852" width="300" height="52" rx="10" fill={VIOLET} fill-opacity="0.35" stroke={VIOLET} />
        <text x="478" y="887" font-family={UI} font-weight="600" font-size="28" fill="#FFFFFF">"Zoom 1.75×"</text>
        <rect x="900" y="852" width="260" height="52" rx="10" fill={VIOLET} fill-opacity="0.35" stroke={VIOLET} />
        <text x="918" y="887" font-family={UI} font-weight="600" font-size="28" fill="#FFFFFF">"Zoom 1.5×"</text>
        {bars}
        <rect x={hx - 1.5} y="716" width="3" height="270" fill={REC} />
        <path d={format!("M{} 706 h20 v12 l-10 10 l-10 -10 Z", hx - 10.0)} fill={REC} />
        <text x={hx} y="700" text-anchor="middle" font-family={MONO} font-size="28" fill={INK}>{time}</text>
    </g>)
}
