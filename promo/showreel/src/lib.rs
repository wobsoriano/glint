//! Glint showreel: 15 s at 1920x1080, cut to a 120 BPM bed (a beat every 0.5 s).
//!
//! One piece of geometry is carried through every cut: the region selected in the capture
//! overlay becomes the preview card, the card opens into the editor canvas, the editor's red
//! record dot opens the recording, the recorded screen shrinks into the Studio preview, and the
//! Studio collapses into the glint star of the logo.
use fframes::{
    AudioMap, AudioTimestamp::*, AudioTrack, Color, Duration, FFramesContext, FontQuery, Frame, Scene, Scenes, Svgr, Video,
    include_media_dir,
};

include_media_dir!(pub struct ShowreelMedia, "media", stereo);

const W: f32 = 1920.0;
const H: f32 = 1080.0;

const CAPTURE: f32 = 2.5;
const CARDS: f32 = 2.0;
const EDITOR: f32 = 3.5;
const RECORD: f32 = 3.0;
const STUDIO: f32 = 2.0;
const LOGO: f32 = 2.0;

const T_CARDS: f32 = CAPTURE;
const T_EDITOR: f32 = T_CARDS + CARDS;
const T_RECORD: f32 = T_EDITOR + EDITOR;
const T_STUDIO: f32 = T_RECORD + RECORD;
const T_LOGO: f32 = T_STUDIO + STUDIO;

const BG: &str = "#07080C";
const INK: &str = "#F5F6F8";
const MUTED: &str = "#9AA0AE";
const ACCENT: &str = "#5B8CFF";
const VIOLET: &str = "#A78BFA";
const HOT: &str = "#FF4D5E";
const AMBER: &str = "#FFC145";
const REC: &str = "#FF3B30";
const UI: &str = "Segoe UI";
const MONO: &str = "Cascadia Mono";

/// The captured region on the 1920x1080 desktop, and the size of the mock app inside it.
const SHOT_X: f32 = 420.0;
const SHOT_Y: f32 = 250.0;
const SHOT_W: f32 = 1080.0;
const SHOT_H: f32 = 620.0;
/// The newest card of the preview stack (bottom-left), same aspect as the shot.
const CARD: (f32, f32, f32, f32) = (40.0, 830.0, 320.0, 184.0);
/// The image on the editor canvas.
const CANVAS_K: f32 = 0.82;
const CANVAS_CX: f32 = 780.0;
const CANVAS_CY: f32 = 566.0;
/// The Studio preview: the recorded screen scaled into a card inside a padded frame.
const PREVIEW: (f32, f32, f32, f32) = (358.0, 117.0, 864.0, 486.0);
/// Two click targets in shot coordinates: a peak of the chart and the growth chip.
const PEAK: (f32, f32) = (564.0, 322.0);
const CHIP: (f32, f32) = (514.0, 120.0);
const CHART: [(f32, f32); 9] =
    [(228.0, 520.0), (284.0, 498.0), (340.0, 468.0), (396.0, 482.0), (452.0, 418.0), (508.0, 400.0), (564.0, 322.0), (620.0, 348.0), (676.0, 286.0)];

pub struct ShowreelVideo<'a> {
    pub media: &'a ShowreelMedia,
}

impl std::fmt::Debug for ShowreelVideo<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ShowreelVideo").finish()
    }
}

impl Video for ShowreelVideo<'_> {
    const FPS: usize = 60;
    const WIDTH: usize = 1920;
    const HEIGHT: usize = 1080;
    const BACKGROUND_COLOR: Color = Color::BLACK;

    fn duration(&self) -> Duration<'_> {
        Duration::Auto
    }

    fn audio(&self) -> AudioMap<'_> {
        let at = |name: &'static str, t: f32, db: f32| AudioTrack::new(name, Second(t)..Eof).gain_db(db);
        AudioMap::from([
            AudioTrack::new("music.wav", Second(0.)..Eof).gain_db(-2.3),
            at("riser.wav", 0.55, -9.5),
            at("snap.wav", 1.5, -5.0),
            at("whoosh.wav", T_CARDS - 0.1, -5.5),
            at("pop.wav", T_CARDS + 0.45, -8.5),
            at("click.wav", T_CARDS + 1.1, -4.5),
            at("pop_hi.wav", T_CARDS + 1.14, -10.5),
            at("click.wav", T_CARDS + 1.7, -4.5),
            at("whoosh.wav", T_EDITOR - 0.1, -6.5),
            at("whoosh_short.wav", T_EDITOR + 1.0, -9.5),
            at("whoosh_short.wav", T_EDITOR + 1.35, -10.5),
            at("pop.wav", T_EDITOR + 1.8, -7.5),
            at("pop.wav", T_EDITOR + 2.0, -7.5),
            at("pop_hi.wav", T_EDITOR + 2.25, -7.5),
            at("glitch.wav", T_EDITOR + 2.3, -8.5),
            at("click.wav", T_EDITOR + 3.0, -4.5),
            at("rec.wav", T_RECORD + 0.05, -5.5),
            at("click.wav", T_RECORD + 1.0, -4.5),
            at("whoosh_short.wav", T_RECORD + 0.95, -11.5),
            at("click.wav", T_RECORD + 1.9, -4.5),
            at("tick.wav", T_RECORD + 2.0, -5.5),
            at("tick.wav", T_RECORD + 2.08, -5.5),
            at("whoosh.wav", T_RECORD + 2.6, -8.5),
            at("tick.wav", T_STUDIO + 0.6, -11.5),
            at("tick.wav", T_STUDIO + 1.1, -11.5),
            at("impact.wav", T_LOGO, 0.0),
            at("shimmer.wav", T_LOGO + 0.95, -4.5),
        ])
    }

    fn define_scenes(&self) -> Scenes<'_> {
        Scenes::from(vec![&Capture as &dyn Scene, &Cards, &Editor, &Record, &Studio, &Logo])
    }

    fn render_frame<'a>(&'a self, frame: Frame, ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        fframes::svgr!(
            <svg xmlns="http://www.w3.org/2000/svg" width={W} height={H} viewBox="0 0 1920 1080">
                <rect width="1920" height="1080" fill={BG} />
                {ctx.render_scenes(&frame)}
                <defs>
                    <radialGradient id="vignette" cx="0.5" cy="0.5" r="0.75">
                        <stop offset="0.6" stop-color="#000" stop-opacity="0" />
                        <stop offset="1" stop-color="#000" stop-opacity="0.45" />
                    </radialGradient>
                </defs>
                <rect width="1920" height="1080" fill="url(#vignette)" />
            </svg>
        )
    }
}

// ---------------------------------------------------------------- motion helpers

fn c01(x: f32) -> f32 {
    x.clamp(0.0, 1.0)
}

/// 0..1 over `d` seconds from `a`.
fn seg(t: f32, a: f32, d: f32) -> f32 {
    c01((t - a) / d)
}

fn lerp(a: f32, b: f32, p: f32) -> f32 {
    a + (b - a) * p
}

fn out_expo(x: f32) -> f32 {
    if x >= 1.0 { 1.0 } else { 1.0 - 2f32.powf(-10.0 * x) }
}

fn out_cubic(x: f32) -> f32 {
    1.0 - (1.0 - x).powi(3)
}

fn in_cubic(x: f32) -> f32 {
    x * x * x
}

fn in_out(x: f32) -> f32 {
    if x < 0.5 { 4.0 * x * x * x } else { 1.0 - (-2.0 * x + 2.0).powi(3) / 2.0 }
}

fn out_back(x: f32) -> f32 {
    let c1 = 1.70158;
    let c3 = c1 + 1.0;
    1.0 + c3 * (x - 1.0).powi(3) + c1 * (x - 1.0).powi(2)
}

/// Damped spring from 0 to 1, `t` seconds after it starts (mass 1).
fn spring(t: f32, stiffness: f32, damping: f32) -> f32 {
    if t <= 0.0 {
        return 0.0;
    }
    let w0 = stiffness.sqrt();
    let z = damping / (2.0 * w0);
    if z < 1.0 {
        let wd = w0 * (1.0 - z * z).sqrt();
        1.0 - (-z * w0 * t).exp() * ((wd * t).cos() + z * w0 / wd * (wd * t).sin())
    } else {
        1.0 - (-w0 * t).exp() * (1.0 + w0 * t)
    }
}

fn hash(i: u32) -> f32 {
    let mut x = i.wrapping_mul(2_654_435_761) ^ 0x9e37_79b9;
    x ^= x >> 15;
    x = x.wrapping_mul(2_246_822_519);
    x ^= x >> 13;
    (x & 0xffff) as f32 / 65535.0
}

fn tf(x: f32, y: f32, s: f32) -> String {
    format!("translate({x:.2} {y:.2}) scale({s:.4})")
}

fn px(x: f32) -> f32 {
    x.max(0.5)
}

// ---------------------------------------------------------------- shared pieces

/// The four-pointed glint star, centred on 0,0.
fn star_path(r: f32) -> String {
    let a = r * 0.14;
    format!("M0 {} Q{a} {} {r} 0 Q{a} {a} 0 {r} Q{} {a} {} 0 Q{} {} 0 {} Z", -r, -a, -a, -r, -a, -a, -r)
}

fn cursor<'a>(x: f32, y: f32, opacity: f32) -> Svgr<'a> {
    fframes::svgr!(<g transform={tf(x, y, 1.5)} opacity={opacity}>
        <path d="M0 0 L0 23 L6.2 17.6 L10.4 27 L14.6 25.2 L10.6 16 L18 16 Z" fill="#FFFFFF" stroke="#0A0A0A" stroke-width="1.4" stroke-linejoin="round" />
    </g>)
}

/// A click: a ring that grows and fades from the cursor tip.
fn ripple<'a>(x: f32, y: f32, t: f32, at: f32, color: &'a str) -> Svgr<'a> {
    let p = seg(t, at, 0.45);
    if p <= 0.0 || p >= 1.0 {
        return Svgr::empty();
    }
    let r = 8.0 + 46.0 * out_cubic(p);
    fframes::svgr!(<circle cx={x} cy={y} r={r} fill="none" stroke={color} stroke-width={4.0 * (1.0 - p) + 0.5} opacity={1.0 - p} />)
}

/// The scene number and name, top-left. Slides in on entry and out before the cut.
fn chapter<'a>(t: f32, len: f32, number: &'a str, name: &'a str) -> Svgr<'a> {
    let enter = out_expo(seg(t, 0.15, 0.5));
    let exit = in_cubic(seg(t, len - 0.3, 0.25));
    let x = lerp(-40.0, 0.0, enter) - 30.0 * exit;
    let o = enter * (1.0 - exit);
    fframes::svgr!(<g transform={format!("translate({x:.1} 0)")} opacity={o}>
        <rect x="56" y="46" width="268" height="52" rx="26" fill="#000000" fill-opacity="0.45" stroke="#FFFFFF" stroke-opacity="0.12" />
        <text x="82" y="81" font-family={MONO} font-size="28" fill={ACCENT}>{number}</text>
        <text x="128" y="82" font-family={UI} font-weight="600" font-size="30" fill={INK}>{name}</text>
    </g>)
}

/// The mock app being captured, drawn in its own 1080x620 coordinates.
/// `redact` sweeps pixel blocks over the email and the API key from left to right.
fn shot<'a>(id: &str, redact: f32) -> Svgr<'a> {
    let fill_id = format!("{id}-fill");
    let fill_url = format!("url(#{fill_id})");
    let line: String = CHART.iter().enumerate().map(|(i, (x, y))| format!("{}{x} {y} ", if i == 0 { "M" } else { "L" })).collect();
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
    fframes::svgr!(<g>
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

/// The desktop: wallpaper, the app window around the shot, and the taskbar.
fn desktop<'a>(id: &str, redact: f32) -> Svgr<'a> {
    let shot = shot(id, redact);
    fframes::svgr!(<g>
        <defs>
            <linearGradient id="wall" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#0E1636" />
                <stop offset="0.55" stop-color="#241552" />
                <stop offset="1" stop-color="#0A0E1E" />
            </linearGradient>
            <filter id="wall-blur" x="-50%" y="-50%" width="200%" height="200%">
                <feGaussianBlur stdDeviation="90" />
            </filter>
            <filter id="win-shadow" x="-20%" y="-20%" width="140%" height="160%">
                <feGaussianBlur stdDeviation="28" />
            </filter>
        </defs>
        <rect width="1920" height="1080" fill="url(#wall)" />
        <g filter="url(#wall-blur)" opacity="0.8">
            <circle cx="1560" cy="220" r="260" fill="#5B3FD9" />
            <circle cx="300" cy="860" r="300" fill="#1F6FEB" />
            <circle cx="1300" cy="980" r="220" fill="#C2417A" />
        </g>
        <rect x="410" y="240" width="1100" height="680" rx="12" fill="#000000" opacity="0.55" filter="url(#win-shadow)" />
        <rect x="400" y="210" width="1120" height="680" rx="10" fill="#F6F7F9" />
        <rect x="400" y="210" width="1120" height="40" rx="10" fill="#EEF0F3" />
        <rect x="400" y="236" width="1120" height="14" fill="#EEF0F3" />
        <text x="420" y="236" font-family={UI} font-size="15" fill="#4B5563">"Northwind Analytics"</text>
        <path d="M1406 230 H1418 M1446 224 H1458 V236 H1446 Z M1488 224 L1500 236 M1500 224 L1488 236" stroke="#4B5563" stroke-width="1.4" fill="none" />
        <g transform="translate(420 250)">{shot}</g>
        <rect x="0" y="1032" width="1920" height="48" fill="#0D0F16" opacity="0.88" />
        <rect x="852" y="1040" width="32" height="32" rx="7" fill="#2B6CF6" />
        <rect x="896" y="1040" width="32" height="32" rx="7" fill="#F2B530" />
        <rect x="940" y="1040" width="32" height="32" rx="7" fill="#7C3AED" />
        <rect x="984" y="1040" width="32" height="32" rx="7" fill="#1F2937" stroke="#FFFFFF" stroke-opacity="0.25" />
        <path d="M1000 1045 Q1001.5 1054.5 1011 1056 Q1001.5 1057.5 1000 1067 Q998.5 1057.5 989 1056 Q998.5 1054.5 1000 1045 Z" fill="#FFFFFF" />
        <rect x="1028" y="1040" width="32" height="32" rx="7" fill="#10B981" />
    </g>)
}

/// A rounded-corner clip of `content` drawn into the rect, with `content` sized `cw`x`ch`.
fn framed<'a>(id: &str, x: f32, y: f32, w: f32, h: f32, rx: f32, cw: f32, content: Svgr<'a>) -> Svgr<'a> {
    let clip = format!("{id}-clip");
    let clip_url = format!("url(#{clip})");
    let k = w / cw;
    let _ = h;
    fframes::svgr!(<g>
        <defs><clipPath id={clip}><rect x={x} y={y} width={px(w)} height={px(h)} rx={rx} /></clipPath></defs>
        <g clip-path={clip_url}><g transform={tf(x, y, k)}>{content}</g></g>
    </g>)
}

// ---------------------------------------------------------------- 1. capture

#[derive(Debug)]
struct Capture;

impl Scene for Capture {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(CAPTURE)
    }

    fn render_frame<'a>(&'a self, frame: Frame, _ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let intro = out_expo(seg(t, 0.0, 0.9));
        let scene_scale = lerp(1.05, 1.0, intro);
        let fade_in = lerp(0.35, 1.0, seg(t, 0.0, 0.2));

        // Cursor glides to the window's top-left, then drags to the bottom-right.
        let reach = in_out(seg(t, 0.2, 0.42));
        let drag = in_out(seg(t, 0.62, 0.78));
        let (ax, ay) = (SHOT_X, SHOT_Y);
        let (bx, by) = (SHOT_X + SHOT_W, SHOT_Y + SHOT_H);
        let (cx, cy) = if t < 0.62 { (lerp(780.0, ax, reach), lerp(600.0, ay, reach)) } else { (lerp(ax, bx, drag), lerp(ay, by, drag)) };
        let selecting = t >= 0.62;
        let (sw, sh) = (px(cx - ax), px(cy - ay));

        let dim = 0.5 * seg(t, 0.15, 0.25) + 0.35 * out_cubic(seg(t, 1.5, 0.45));
        let hole = if selecting { format!(" M{ax} {ay} h{sw} v{sh} h{} Z", -sw) } else { String::new() };
        let overlay = format!("M0 0 H1920 V1080 H0 Z{hole}");

        let snapped = t >= 1.5;
        let flash = if snapped { (1.0 - seg(t, 1.5, 0.22)).powi(2) * 0.55 } else { 0.0 };
        let handles = out_back(seg(t, 1.5, 0.3));
        let tool_alpha = seg(t, 0.15, 0.2) * (1.0 - seg(t, 1.42, 0.12));

        // The magnifier shows the pixels under the cursor at 5x with a pixel grid.
        let mag_r = 74.0;
        let (mx, my) = (cx + 92.0, cy + 92.0);
        let loupe = if tool_alpha > 0.0 {
            let inner = shot("loupe", 0.0);
            let lx = mx - cx * 5.0;
            let ly = my - cy * 5.0;
            let grid: String = (-7..=7).map(|i| format!("M{} {} v{} M{} {} h{} ", mx + i as f32 * 10.0 - 5.0, my - mag_r, mag_r * 2.0, mx - mag_r, my + i as f32 * 10.0 - 5.0, mag_r * 2.0)).collect();
            fframes::svgr!(<g opacity={tool_alpha}>
                <defs><clipPath id="loupe-clip"><circle cx={mx} cy={my} r={mag_r} /></clipPath></defs>
                <circle cx={mx} cy={my + 6.0} r={mag_r + 4.0} fill="#000000" opacity="0.35" />
                <g clip-path="url(#loupe-clip)">
                    <rect x={mx - mag_r} y={my - mag_r} width={mag_r * 2.0} height={mag_r * 2.0} fill="#22174A" />
                    <g transform={tf(lx, ly, 5.0)}>
                        <rect x="400" y="210" width="1120" height="680" fill="#F6F7F9" />
                        <g transform="translate(420 250)">{inner}</g>
                    </g>
                    <path d={grid} stroke="#000000" stroke-opacity="0.12" stroke-width="1" />
                    <rect x={mx - 5.0} y={my - 5.0} width="10" height="10" fill="none" stroke={ACCENT} stroke-width="2" />
                </g>
                <circle cx={mx} cy={my} r={mag_r} fill="none" stroke="#FFFFFF" stroke-width="3" />
            </g>)
        } else {
            Svgr::empty()
        };

        let crosshair = if tool_alpha > 0.0 {
            fframes::svgr!(<g opacity={tool_alpha} stroke-linecap="round">
                <path d={format!("M{cx} 0 V1080 M0 {cy} H1920")} stroke="#FFFFFF" stroke-opacity="0.18" stroke-width="1" />
                <path d={format!("M{} {cy} h36 M{} {cy} h-36 M{cx} {} v36 M{cx} {} v-36", cx - 46.0, cx + 46.0, cy - 46.0, cy + 46.0)} stroke="#000000" stroke-width="5" />
                <path d={format!("M{} {cy} h36 M{} {cy} h-36 M{cx} {} v36 M{cx} {} v-36", cx - 46.0, cx + 46.0, cy - 46.0, cy + 46.0)} stroke="#FFFFFF" stroke-width="2.5" />
            </g>)
        } else {
            Svgr::empty()
        };

        let label = if selecting && tool_alpha > 0.0 {
            let text = format!("{} × {}", (sw * 2.0).round() as i32, (sh * 2.0).round() as i32);
            fframes::svgr!(<g opacity={tool_alpha} transform={format!("translate({:.1} {:.1})", cx - 236.0, cy + 18.0)}>
                <rect width="212" height="44" rx="22" fill="#000000" fill-opacity="0.72" />
                <text x="106" y="31" text-anchor="middle" font-family={MONO} font-size="24" fill="#FFFFFF">{text}</text>
            </g>)
        } else {
            Svgr::empty()
        };

        let border = if selecting {
            let color = if snapped { ACCENT } else { "#FFFFFF" };
            let width = if snapped { 3.0 } else { 1.5 };
            let corners: Vec<Svgr> = [(ax, ay), (ax + sw, ay), (ax, ay + sh), (ax + sw, ay + sh), (ax + sw / 2.0, ay), (ax + sw / 2.0, ay + sh), (ax, ay + sh / 2.0), (ax + sw, ay + sh / 2.0)]
                .iter()
                .enumerate()
                .map(|(i, (x, y))| {
                    let s = out_back(seg(t, 1.5 + i as f32 * 0.025, 0.28)) * 7.0;
                    fframes::svgr!(<rect x={x - s} y={y - s} width={px(2.0 * s)} height={px(2.0 * s)} rx="2" fill="#FFFFFF" stroke={ACCENT} stroke-width="2" />)
                })
                .collect();
            let lift = 1.0 - seg(t, 1.9, 0.3);
            fframes::svgr!(<g opacity={lift}>
                <rect x={ax} y={ay} width={sw} height={sh} fill="none" stroke={color} stroke-width={width} />
                <g opacity={handles.min(1.0)}>{corners}</g>
            </g>)
        } else {
            Svgr::empty()
        };

        // After the snap the selection lifts off the desktop: it is the card in the next scene.
        let lift = out_cubic(seg(t, 1.85, 0.65));
        let lifted = if t >= 1.85 {
            let s = lerp(1.0, 1.035, lift);
            let (ccx, ccy) = (SHOT_X + SHOT_W / 2.0, SHOT_Y + SHOT_H / 2.0);
            let inner = shot("lift", 0.0);
            fframes::svgr!(<g>
                <defs>
                    <filter id="lift-shadow" x="-20%" y="-20%" width="140%" height="160%"><feGaussianBlur stdDeviation="30" /></filter>
                    <clipPath id="lift-clip"><rect width="1080" height="620" rx={10.0 * lift} /></clipPath>
                </defs>
                <g transform={format!("translate({ccx} {ccy}) scale({s:.4}) translate({} {})", -SHOT_W / 2.0, -SHOT_H / 2.0)}>
                    <rect x="10" y="30" width="1060" height="610" fill="#000000" opacity={0.6 * lift} filter="url(#lift-shadow)" />
                    <g clip-path="url(#lift-clip)">{inner}</g>
                </g>
            </g>)
        } else {
            Svgr::empty()
        };

        let scene = desktop("cap", 0.0);
        fframes::svgr!(<g>
            <g opacity={fade_in} transform={format!("translate(960 540) scale({scene_scale:.4}) translate(-960 -540)")}>
                {scene}
                <path d={overlay} fill="#05060A" fill-opacity={dim} fill-rule="evenodd" />
            </g>
            {lifted}
            {border}
            <rect width="1920" height="1080" fill="#FFFFFF" opacity={flash} />
            {crosshair}
            {label}
            {loupe}
            {chapter(t, CAPTURE, "01", "Capture")}
        </g>)
    }
}

// ---------------------------------------------------------------- 2. preview cards

#[derive(Debug)]
struct Cards;

// The thumbnails carry a dynamic `id`: fully static groups are cached across frames by the
// encoder together with an earlier frame's transform, so they would not scale with the card.
fn thumb_code<'a>(id: String) -> Svgr<'a> {
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

fn thumb_photo<'a>(id: String) -> Svgr<'a> {
    fframes::svgr!(<g id={id}>
        <defs>
            <linearGradient id="sky" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stop-color="#FF9A62" /><stop offset="0.6" stop-color="#C2417A" /><stop offset="1" stop-color="#3B1D5C" />
            </linearGradient>
        </defs>
        <rect width="1080" height="620" fill="url(#sky)" />
        <circle cx="720" cy="300" r="110" fill="#FFE2A8" opacity="0.9" />
        <path d="M0 470 L180 330 L330 440 L520 280 L760 470 L900 380 L1080 470 V620 H0 Z" fill="#2A1640" />
        <path d="M0 540 L220 430 L420 520 L640 410 L860 540 L1080 470 V620 H0 Z" fill="#1A0E2C" />
    </g>)
}

/// One card of the preview stack with a hover state.
fn card<'a>(id: &str, x: f32, y: f32, content: Svgr<'a>, opacity: f32) -> Svgr<'a> {
    let (_, _, w, h) = CARD;
    let inner = framed(id, x, y, w, h, 12.0, SHOT_W, content);
    fframes::svgr!(<g opacity={opacity}>
        <rect x={x + 4.0} y={y + 14.0} width={w - 8.0} height={h - 6.0} rx="12" fill="#000000" opacity="0.45" />
        {inner}
        <rect x={x} y={y} width={w} height={h} rx="12" fill="none" stroke="#FFFFFF" stroke-opacity="0.16" />
    </g>)
}

impl Scene for Cards {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(CARDS)
    }

    fn render_frame<'a>(&'a self, frame: Frame, _ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let (kx, ky, kw, kh) = CARD;

        // The lifted capture flies along an arc into the bottom-left card slot.
        let fly_x = in_out(seg(t, 0.0, 0.62));
        let fly_y = out_expo(seg(t, 0.0, 0.62));
        let start_s = 1.035;
        let (sx, sy) = (SHOT_X + SHOT_W / 2.0 - SHOT_W * start_s / 2.0, SHOT_Y + SHOT_H / 2.0 - SHOT_H * start_s / 2.0);
        let fx = lerp(sx, kx, fly_x);
        let fy = lerp(sy, ky, fly_y);
        let fw = lerp(SHOT_W * start_s, kw, fly_y);
        let fh = fw * SHOT_H / SHOT_W;
        let tilt = -5.0 * (std::f32::consts::PI * fly_x).sin();
        let landed = seg(t, 0.55, 0.1);

        // Older captures slide in from the left, then spring up as the new card lands.
        let push = spring(t - 0.42, 260.0, 20.0) * (kh + 12.0);
        let older: Vec<Svgr> = [(1.0, 0.0), (2.0, 0.06)]
            .iter()
            .map(|&(slot, delay): &(f32, f32)| {
                let enter = out_expo(seg(t, 0.05 + delay, 0.5));
                let x = lerp(-kw - 40.0, kx, enter);
                let y = ky - (slot - 1.0) * (kh + 12.0) - push;
                let content = if slot < 1.5 { thumb_photo(format!("photo{slot}")) } else { thumb_code(format!("code{slot}")) };
                card(&format!("old{slot}"), x, y, content, enter)
            })
            .collect();

        // Hover, Copy, then the pencil.
        let cur_in = in_out(seg(t, 0.55, 0.42));
        let to_copy = in_out(seg(t, 0.98, 0.1));
        let to_edit = in_out(seg(t, 1.38, 0.3));
        let (copy_x, copy_y) = (kx + kw / 2.0, ky + 70.0);
        let (edit_x, edit_y) = (kx + kw - 22.0, ky + 22.0);
        let mut cx = lerp(980.0, kx + kw / 2.0 + 30.0, cur_in);
        let mut cy = lerp(760.0, ky + kh / 2.0 + 20.0, cur_in);
        cx = lerp(cx, copy_x, to_copy);
        cy = lerp(cy, copy_y, to_copy);
        cx = lerp(cx, edit_x, to_edit);
        cy = lerp(cy, edit_y, to_edit);
        let hover = seg(t, 0.85, 0.15);
        let pill = |label: &'a str, y: f32, at: f32, pressed: bool| -> Svgr<'a> {
            let s = out_back(seg(t, at, 0.3));
            let fill = if pressed { ACCENT } else { "#FFFFFF" };
            let text = if pressed { "#FFFFFF" } else { "#111827" };
            fframes::svgr!(<g transform={format!("translate({:.1} {:.1}) scale({s:.3})", kx + kw / 2.0, y)} opacity={c01(s)}>
                <rect x="-62" y="-20" width="124" height="40" rx="20" fill={fill} fill-opacity="0.94" />
                <text y="8" text-anchor="middle" font-family={UI} font-weight="600" font-size="20" fill={text}>{label}</text>
            </g>)
        };
        let copied = t >= 1.1 && t < 1.32;
        let corner = |x: f32, y: f32, glyph: &'a str, at: f32, lit: bool| -> Svgr<'a> {
            let s = out_back(seg(t, at, 0.25));
            let fill = if lit { ACCENT } else { "#111827" };
            fframes::svgr!(<g transform={format!("translate({x:.1} {y:.1}) scale({s:.3})")} opacity={c01(s)}>
                <circle r="15" fill={fill} fill-opacity="0.85" />
                <path d={glyph} stroke="#FFFFFF" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round" />
            </g>)
        };
        let editing = t >= 1.68;
        let hover_ui = if hover > 0.0 {
            fframes::svgr!(<g>
                <rect x={kx} y={ky} width={kw} height={kh} rx="12" fill="#000000" opacity={0.42 * hover} />
                {pill("Copy", ky + 70.0, 0.88, copied)}
                {pill("Save", ky + 118.0, 0.93, false)}
                {corner(kx + 22.0, ky + 22.0, "M-5 -5 L5 5 M5 -5 L-5 5", 0.95, false)}
                {corner(kx + kw - 22.0, ky + 22.0, "M-6 6 L-6 2 L3 -7 L7 -3 L-2 6 Z", 0.99, editing)}
                {corner(kx + 22.0, ky + kh - 22.0, "M0 -7 V3 M-5 3 H5 M0 3 V8", 1.03, false)}
                {corner(kx + kw - 22.0, ky + kh - 22.0, "M-6 0 h0.1 M0 0 h0.1 M6 0 h0.1", 1.07, false)}
            </g>)
        } else {
            Svgr::empty()
        };

        let toast_in = out_expo(seg(t, 1.12, 0.4));
        let toast_out = in_cubic(seg(t, 1.75, 0.2));
        let toast = fframes::svgr!(<g transform={format!("translate(960 {:.1})", lerp(-60.0, 70.0, toast_in) - 40.0 * toast_out)} opacity={toast_in * (1.0 - toast_out)}>
            <rect x="-170" y="-28" width="340" height="56" rx="28" fill="#202020" stroke="#FFFFFF" stroke-opacity="0.14" />
            <circle cx="-136" cy="0" r="14" fill="#22C55E" />
            <path d="M-143 0 L-138 5 L-129 -5" stroke="#FFFFFF" stroke-width="2.6" fill="none" stroke-linecap="round" stroke-linejoin="round" />
            <text x="-108" y="9" font-family={UI} font-weight="600" font-size="24" fill={INK}>"Copied to clipboard"</text>
        </g>);

        // The editor click: the card swells and the desktop sinks into the dark editor canvas.
        let open = in_cubic(seg(t, 1.72, 0.28));
        let card_scale = 1.0 + 0.04 * out_cubic(seg(t, 1.72, 0.2));
        let dim = 0.85 * (1.0 - out_cubic(seg(t, 0.0, 0.35))) + 0.92 * open;

        let flying = shot("fly", 0.0);
        let rx = lerp(10.0, 12.0, landed);
        let fly_card = fframes::svgr!(<g transform={format!("translate({:.1} {:.1}) rotate({tilt:.2}) scale({card_scale:.4}) translate({:.1} {:.1})", fx + fw / 2.0, fy + fh / 2.0, -fw / 2.0, -fh / 2.0)}>
            <rect x="6" y={14.0 + 10.0 * (1.0 - landed)} width={px(fw - 12.0)} height={px(fh - 8.0)} rx="12" fill="#000000" opacity="0.5" />
            {framed("flycard", 0.0, 0.0, fw, fh, rx, SHOT_W, flying)}
            <rect width={px(fw)} height={px(fh)} rx={rx} fill="none" stroke="#FFFFFF" stroke-opacity={0.16 * landed} />
        </g>);

        let scene = desktop("cards", 0.0);
        fframes::svgr!(<g>
            {scene}
            <rect width="1920" height="1080" fill="#05060A" opacity={dim} />
            {older}
            {fly_card}
            <g opacity={1.0 - open}>{hover_ui}</g>
            {toast}
            {ripple(copy_x, copy_y, t, 1.1, ACCENT)}
            {ripple(edit_x, edit_y, t, 1.7, ACCENT)}
            {cursor(cx, cy, seg(t, 0.55, 0.1) * (1.0 - open))}
            {chapter(t, CARDS, "02", "Quick access")}
        </g>)
    }
}

// ---------------------------------------------------------------- 3. editor

#[derive(Debug)]
struct Editor;

fn quad(p0: (f32, f32), c: (f32, f32), p1: (f32, f32), s: f32) -> (f32, f32) {
    let u = 1.0 - s;
    (u * u * p0.0 + 2.0 * u * s * c.0 + s * s * p1.0, u * u * p0.1 + 2.0 * u * s * c.1 + s * s * p1.1)
}

fn quad_len(p0: (f32, f32), c: (f32, f32), p1: (f32, f32)) -> f32 {
    (1..=32)
        .map(|i| {
            let (a, b) = (quad(p0, c, p1, (i - 1) as f32 / 32.0), quad(p0, c, p1, i as f32 / 32.0));
            ((b.0 - a.0).powi(2) + (b.1 - a.1).powi(2)).sqrt()
        })
        .sum()
}

const TOOLS: [&str; 8] = [
    "M-6 -9 L-6 7 L-2 3 L1 10 L4 9 L1 2 L6 2 Z",
    "M-9 -7 H9 V7 H-9 Z",
    "M-8 8 L8 -8 M1 -8 H8 V-1",
    "M-8 -8 H8 M0 -8 V9",
    "M0 -9 A9 9 0 1 1 -0.1 -9 M-2 -3 L1 -5 V5",
    "M-9 -9 h6 v6 h-6 Z M3 -9 h6 v6 h-6 Z M-9 3 h6 v6 h-6 Z M3 3 h6 v6 h-6 Z",
    "M-9 -4 V-9 H-4 M4 -9 H9 V-4 M9 4 V9 H4 M-4 9 H-9 V4",
    "M-8 4 L4 -8 L8 -4 L-4 8 Z M-8 8 L-4 8",
];

impl Scene for Editor {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(EDITOR)
    }

    fn render_frame<'a>(&'a self, frame: Frame, _ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let (kx, ky, kw, _) = CARD;

        // The card opens into the canvas image.
        let open = in_out(seg(t, 0.0, 0.55));
        let iw = SHOT_W * CANVAS_K;
        let ih = SHOT_H * CANVAS_K;
        let (ix0, iy0) = (CANVAS_CX - iw / 2.0, CANVAS_CY - ih / 2.0);
        let img_x = lerp(kx, ix0, open);
        let img_y = lerp(ky, iy0, open);
        let img_w = lerp(kw * 1.04, iw, open);
        let k = img_w / SHOT_W;
        let img_h = SHOT_H * k;

        // Padding, radius and shadow grow in, and the sliders move with them.
        let style = out_cubic(seg(t, 0.5, 0.6));
        let pad = 72.0 * style;
        let radius = 4.0 + 16.0 * style;
        let chrome = seg(t, 0.05, 0.35);
        let sidebar_x = 360.0 * (1.0 - out_expo(seg(t, 0.1, 0.55)));

        // Annotation timings.
        let arrow_p = out_cubic(seg(t, 1.0, 0.38));
        let rect_p = in_out(seg(t, 1.36, 0.3));
        let marks = [(1.8, (446.0, 78.0)), (2.0, (512.0, 284.0)), (2.25, (712.0, 366.0))];
        let redact = in_out(seg(t, 2.32, 0.5));

        let tool = if t >= 2.3 { 5 } else if t >= 1.72 { 4 } else if t >= 1.36 { 1 } else { 2 };
        let tool_from = if t >= 2.3 { 4 } else if t >= 1.72 { 1 } else if t >= 1.36 { 2 } else { 0 };
        let switch_at = [0.92, 1.36, 1.72, 2.3].into_iter().filter(|s| t >= *s).last().unwrap_or(0.92);
        let ind = out_expo(seg(t, switch_at, 0.25));
        let tool_x = |i: usize| 780.0 - 3.5 * 56.0 + i as f32 * 56.0;
        let ind_x = if t < 0.92 { tool_x(0) } else { lerp(tool_x(tool_from), tool_x(tool), ind) };

        let tools: Vec<Svgr> = TOOLS
            .iter()
            .enumerate()
            .map(|(i, d)| {
                let drop = out_expo(seg(t, 0.12 + i as f32 * 0.035, 0.4));
                let lit = (i == tool && t >= 0.92) || (i == 0 && t < 0.92);
                let stroke = if lit { "#FFFFFF" } else { "#C9CCD4" };
                fframes::svgr!(<g transform={format!("translate({:.1} {:.1})", tool_x(i), 72.0 - 18.0 * (1.0 - drop))} opacity={drop}>
                    <path d={*d} stroke={stroke} stroke-width="1.8" fill="none" stroke-linecap="round" stroke-linejoin="round" />
                </g>)
            })
            .collect();

        // Arrow drawn along a quadratic curve, with its head following the tip.
        let (a0, ac, a1) = ((905.0, 64.0), (760.0, 6.0), (586.0, 108.0));
        let alen = quad_len(a0, ac, a1);
        let tip = quad(a0, ac, a1, arrow_p);
        let before = quad(a0, ac, a1, (arrow_p - 0.04).max(0.0));
        let ang = (tip.1 - before.1).atan2(tip.0 - before.0).to_degrees();
        let arrow = if arrow_p > 0.0 {
            fframes::svgr!(<g>
                <path d={format!("M{} {} Q{} {} {} {}", a0.0, a0.1, ac.0, ac.1, a1.0, a1.1)} fill="none" stroke={HOT} stroke-width="8" stroke-linecap="round"
                    stroke-dasharray={format!("{alen:.1} {alen:.1}")} stroke-dashoffset={alen * (1.0 - arrow_p)} />
                <g transform={format!("translate({:.1} {:.1}) rotate({ang:.1}) scale({:.3})", tip.0, tip.1, out_back(seg(t, 1.05, 0.3)))}>
                    <path d="M8 0 L-16 -13 L-11 0 L-16 13 Z" fill={HOT} stroke={HOT} stroke-width="3" stroke-linejoin="round" />
                </g>
            </g>)
        } else {
            Svgr::empty()
        };
        let (rx0, ry0, rw, rh) = (524.0, 288.0, 84.0, 66.0);
        let rlen = 2.0 * (rw + rh);
        let rect = if rect_p > 0.0 {
            fframes::svgr!(<rect x={rx0} y={ry0} width={rw} height={rh} rx="10" fill={AMBER} fill-opacity={0.14 * rect_p} stroke={AMBER} stroke-width="6"
                stroke-dasharray={format!("{rlen:.1} {rlen:.1}")} stroke-dashoffset={rlen * (1.0 - rect_p)} />)
        } else {
            Svgr::empty()
        };
        let markers: Vec<Svgr> = marks
            .iter()
            .enumerate()
            .map(|(i, (at, (x, y)))| {
                let s = spring(t - at, 320.0, 14.0);
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

        // The cursor draws: along the arrow, around the rectangle, onto each marker, across the secrets.
        let rect_pt = {
            let d = rect_p * rlen;
            if d < rw {
                (rx0 + d, ry0)
            } else if d < rw + rh {
                (rx0 + rw, ry0 + d - rw)
            } else if d < 2.0 * rw + rh {
                (rx0 + rw - (d - rw - rh), ry0 + rh)
            } else {
                (rx0, ry0 + rh - (d - 2.0 * rw - rh))
            }
        };
        let local = if t < 1.0 {
            let p = in_out(seg(t, 0.55, 0.42));
            (lerp(980.0, a0.0, p), lerp(420.0, a0.1, p))
        } else if t < 1.38 {
            tip
        } else if t < 1.66 {
            rect_pt
        } else if t < 2.32 {
            let i = marks.iter().rposition(|(at, _)| t >= *at - 0.2).unwrap_or(0);
            let from = if i == 0 { (rx0, ry0) } else { marks[i - 1].1 };
            let start = if i == 0 { 1.66 } else { marks[i].0 - 0.2 };
            let p = in_out(seg(t, start, marks[i].0 - 0.02 - start));
            (lerp(from.0, marks[i].1.0, p), lerp(from.1, marks[i].1.1, p))
        } else {
            let p = redact;
            let sweep = (744.0 + 290.0 * p, lerp(450.0, 530.0, (p * 3.0).sin().abs()));
            let from = marks[2].1;
            let blend = in_out(seg(t, 2.32, 0.14));
            (lerp(from.0, sweep.0, blend), lerp(from.1, sweep.1, blend))
        };
        let cursor_on_canvas = (img_x + local.0 * k, img_y + local.1 * k);
        // Then to Done.
        let (done_x, done_y) = (1478.0, 1054.0);
        let to_done = in_out(seg(t, 2.68, 0.3));
        let cur = (lerp(cursor_on_canvas.0, done_x, to_done), lerp(cursor_on_canvas.1, done_y, to_done));

        // Done: the editor drops away and leaves the red record dot.
        let close = in_cubic(seg(t, 3.08, 0.3));
        let win_scale = 1.0 - 0.08 * close;
        let rec_dot = spring(t - 3.22, 240.0, 16.0);

        let frame_x = img_x - pad;
        let frame_y = img_y - pad;
        let frame_w = img_w + 2.0 * pad;
        let frame_h = img_h + 2.0 * pad;
        let content = shot("edit", redact);
        let ann_k = k;
        let annotations = fframes::svgr!(<g transform={tf(img_x, img_y, ann_k)}>{arrow}{rect}{markers}</g>);

        let knob = |y: f32, v: f32| -> Svgr<'a> {
            let x = 1590.0 + 300.0 * v;
            fframes::svgr!(<g>
                <rect x="1590" y={y - 2.0} width="300" height="4" rx="2" fill="#FFFFFF" fill-opacity="0.16" />
                <rect x="1590" y={y - 2.0} width={px(300.0 * v)} height="4" rx="2" fill={ACCENT} />
                <circle cx={x} cy={y} r="10" fill="#FFFFFF" stroke={ACCENT} stroke-width="3" />
            </g>)
        };
        let toggle_on = seg(t, 2.32, 0.15);
        let hidden = out_back(seg(t, 2.85, 0.3));
        let sidebar = fframes::svgr!(<g transform={format!("translate({sidebar_x:.1} 0)")}>
            <rect x="1560" y="44" width="360" height="1036" fill="#1C1C1C" />
            <rect x="1560" y="44" width="1" height="1036" fill="#FFFFFF" fill-opacity="0.08" />
            <text x="1590" y="150" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Background"</text>
            <circle cx="1612" cy="200" r="20" fill="url(#bg-grad)" />
            <circle cx="1612" cy="200" r="26" fill="none" stroke="#FFFFFF" stroke-width="2.5" opacity={style} />
            <circle cx="1668" cy="200" r="20" fill="#0F172A" />
            <circle cx="1724" cy="200" r="20" fill="#F2B530" />
            <circle cx="1780" cy="200" r="20" fill="#10B981" />
            <circle cx="1836" cy="200" r="20" fill="#E5E7EB" />
            <text x="1590" y="290" font-family={UI} font-size="28" fill={MUTED}>"Padding"</text>
            {knob(326.0, 0.08 + 0.5 * style)}
            <text x="1590" y="402" font-family={UI} font-size="28" fill={MUTED}>"Corners"</text>
            {knob(438.0, 0.1 + 0.45 * style)}
            <text x="1590" y="514" font-family={UI} font-size="28" fill={MUTED}>"Shadow"</text>
            {knob(550.0, 0.05 + 0.6 * style)}
            <rect x="1576" y="610" width="328" height="128" rx="12" fill="#FFFFFF" fill-opacity={0.04 + 0.06 * toggle_on} stroke={ACCENT} stroke-opacity={0.8 * toggle_on} />
            <text x="1600" y="660" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Smart redaction"</text>
            <rect x="1824" y="636" width="56" height="30" rx="15" fill={if toggle_on > 0.5 { ACCENT } else { "#3A3A3A" }} />
            <circle cx={1840.0 + 24.0 * toggle_on} cy="651" r="10" fill="#FFFFFF" />
            <g transform={format!("translate(1600 712) scale({hidden:.3})")} opacity={c01(hidden)}>
                <text font-family={UI} font-size="28" fill={ACCENT}>"2 secrets hidden"</text>
            </g>
        </g>);

        let window = fframes::svgr!(<g transform={format!("translate(960 540) scale({win_scale:.4}) translate(-960 -540)")} opacity={1.0 - close}>
            <defs>
                <linearGradient id="bg-grad" x1="0" y1="0" x2="1" y2="1">
                    <stop offset="0" stop-color="#5B8CFF" />
                    <stop offset="0.5" stop-color="#A78BFA" />
                    <stop offset="1" stop-color="#F472B6" />
                </linearGradient>
                <filter id="frame-shadow" x="-20%" y="-20%" width="140%" height="160%"><feGaussianBlur stdDeviation="24" /></filter>
                <clipPath id="frame-clip"><rect x={frame_x} y={frame_y} width={px(frame_w)} height={px(frame_h)} rx={14.0 * style} /></clipPath>
                <clipPath id="img-clip"><rect x={img_x} y={img_y} width={img_w} height={img_h} rx={radius} /></clipPath>
            </defs>
            <rect width="1920" height="1080" fill="#141414" opacity={chrome} />
            <g opacity={chrome}>
                <rect width="1920" height="44" fill="#202020" />
                <path d={star_path(9.0)} transform="translate(26 22)" fill={ACCENT} />
                <text x="46" y="29" font-family={UI} font-size="15" fill={MUTED}>"Glint"</text>
                <path d="M1786 22 H1798 M1832 16 H1844 V28 H1832 Z M1878 16 L1890 28 M1890 16 L1878 28" stroke="#C9CCD4" stroke-width="1.4" fill="none" />
                <rect y="44" width="1560" height="56" fill="#1A1A1A" />
                <rect x={ind_x - 22.0} y="50" width="44" height="44" rx="6" fill={ACCENT} fill-opacity="0.28" />
                <rect x={ind_x - 10.0} y="92" width="20" height="3" rx="1.5" fill={ACCENT} />
                {tools}
                <rect y="1028" width="1560" height="52" fill="#1A1A1A" />
                <text x="36" y="1063" font-family={MONO} font-size="28" fill={MUTED}>"100%"</text>
                <rect x="1260" y="1036" width="88" height="36" rx="6" fill="#FFFFFF" fill-opacity="0.08" />
                <text x="1304" y="1062" text-anchor="middle" font-family={UI} font-size="20" fill={INK}>"Copy"</text>
                <rect x="1358" y="1036" width="88" height="36" rx="6" fill="#FFFFFF" fill-opacity="0.08" />
                <text x="1402" y="1062" text-anchor="middle" font-family={UI} font-size="20" fill={INK}>"Save"</text>
                <rect x="1456" y="1036" width="88" height="36" rx="6" fill={ACCENT} />
                <text x="1500" y="1062" text-anchor="middle" font-family={UI} font-weight="600" font-size="20" fill="#FFFFFF">"Done"</text>
            </g>
            <rect x={frame_x + 10.0} y={frame_y + 26.0} width={px(frame_w - 20.0)} height={px(frame_h - 10.0)} rx="18" fill="#000000" opacity={0.55 * style} filter="url(#frame-shadow)" />
            <g clip-path="url(#frame-clip)">
                <rect x={frame_x} y={frame_y} width={px(frame_w)} height={px(frame_h)} fill="url(#bg-grad)" opacity={style} />
            </g>
            <rect x={img_x + 6.0} y={img_y + 16.0} width={img_w - 12.0} height={img_h - 8.0} rx={radius} fill="#000000" opacity={0.35 * style} />
            <g clip-path="url(#img-clip)"><g transform={tf(img_x, img_y, k)}>{content}</g></g>
            {annotations}
            {sidebar}
            {ripple(done_x, done_y, t, 3.0, "#FFFFFF")}
            {cursor(cur.0, cur.1, seg(t, 0.55, 0.1))}
            {chapter(t, EDITOR, "03", "Annotate")}
        </g>);
        fframes::svgr!(<g>
            {window}
            <g transform={format!("translate(960 540) scale({:.3})", rec_dot * 2.6)} opacity={c01(rec_dot)}>
                <circle r="16" fill={REC} />
            </g>
        </g>)
    }
}

// ---------------------------------------------------------------- 4. record

#[derive(Debug)]
struct Record;

/// Where the camera looks at time `t`, and how far it is zoomed.
fn record_camera(t: f32) -> (f32, f32, f32) {
    let a = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
    let b = (SHOT_X + CHIP.0, SHOT_Y + CHIP.1);
    let zin = in_out(seg(t, 1.02, 0.45));
    let zout = in_out(seg(t, 2.25, 0.45));
    let z = 1.0 + 0.75 * zin * (1.0 - zout);
    let pan = in_out(seg(t, 1.55, 0.4));
    (lerp(a.0, b.0, pan), lerp(a.1, b.1, pan), z)
}

fn camera_transform(fx: f32, fy: f32, z: f32) -> String {
    let k = (z - 1.0) / 0.75 * 0.65;
    let sx = lerp(fx, 960.0, k);
    let sy = lerp(fy, 520.0, k);
    format!("translate({sx:.2} {sy:.2}) scale({z:.4}) translate({:.2} {:.2})", -fx, -fy)
}

impl Scene for Record {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(RECORD)
    }

    fn render_frame<'a>(&'a self, frame: Frame, _ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let (fx, fy, z) = record_camera(t);

        // The red dot from the editor drops into the control bar.
        let dock = out_expo(seg(t, 0.0, 0.5));
        let (bar_x, bar_y) = (960.0, 1000.0);
        let dot_x = lerp(960.0, bar_x - 112.0, dock);
        let dot_y = lerp(540.0, bar_y, dock);
        let dot_s = lerp(2.6, 0.5, dock);
        let reveal = out_cubic(seg(t, 0.08, 0.4));
        let bar_in = out_expo(seg(t, 0.2, 0.45));
        let pulse = 0.65 + 0.35 * (t * 7.0).cos().abs();

        let secs = ((t - 0.3).max(0.0) * 1.0) as i32;
        let timer = format!("00:{secs:02}");

        let a = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
        let b = (SHOT_X + CHIP.0, SHOT_Y + CHIP.1);
        let reach = in_out(seg(t, 0.42, 0.55));
        let hop = in_out(seg(t, 1.5, 0.38));
        let world_cursor = if t < 1.5 { (lerp(1240.0, a.0, reach), lerp(780.0, a.1, reach)) } else { (lerp(a.0, b.0, hop), lerp(a.1, b.1, hop)) };
        let k = (z - 1.0) / 0.75 * 0.65;
        let to_screen = |p: (f32, f32)| (lerp(fx, 960.0, k) + (p.0 - fx) * z, lerp(fy, 520.0, k) + (p.1 - fy) * z);
        let cur = to_screen(world_cursor);
        let click_a = to_screen(a);
        let click_b = to_screen(b);

        // Out: the recorded screen shrinks into the Studio preview.
        let out = in_out(seg(t, 2.62, 0.38));
        let (px0, py0, pw, _) = PREVIEW;
        let out_s = lerp(1.0, pw / W, out);
        let out_x = lerp(0.0, px0, out);
        let out_y = lerp(0.0, py0, out);
        let ui = 1.0 - seg(t, 2.55, 0.15);

        let key = |label: &'a str, x: f32, at: f32, w: f32| -> Svgr<'a> {
            let s = out_back(seg(t, at, 0.28)) * (1.0 - seg(t, 2.45, 0.15));
            fframes::svgr!(<g transform={format!("translate({x} 912) scale({s:.3})")} opacity={c01(s)}>
                <rect x={-w / 2.0} y="-30" width={w} height="60" rx="12" fill="#111318" fill-opacity="0.9" stroke="#FFFFFF" stroke-opacity="0.25" />
                <rect x={-w / 2.0} y="24" width={w} height="6" rx="3" fill="#000000" opacity="0.4" />
                <text y="11" text-anchor="middle" font-family={UI} font-weight="600" font-size="30" fill={INK}>{label}</text>
            </g>)
        };

        let scene = desktop("rec", 0.0);
        let ants = format!("{:.1}", -t * 80.0);
        fframes::svgr!(<g>
            <g transform={format!("translate({out_x:.2} {out_y:.2}) scale({out_s:.4})")}>
                <defs><clipPath id="rec-clip"><rect width="1920" height="1080" rx={24.0 * out / out_s.max(0.1)} /></clipPath></defs>
                <g clip-path="url(#rec-clip)" opacity={reveal}>
                    <g transform={camera_transform(fx, fy, z)}>
                        {scene}
                        <path d="M-2000 -2000 H4000 V4000 H-2000 Z M400 210 V890 H1520 V210 Z" fill="#05060A" fill-opacity={0.38 * ui} fill-rule="evenodd" />
                        <rect x="400" y="210" width="1120" height="680" fill="none" stroke="#FFFFFF" stroke-width={2.0 / z} stroke-dasharray="12 9" stroke-dashoffset={ants} opacity={ui} />
                    </g>
                    {ripple(click_a.0, click_a.1, t, 1.0, REC)}
                    {ripple(click_b.0, click_b.1, t, 1.9, REC)}
                    {cursor(cur.0, cur.1, seg(t, 0.4, 0.1))}
                </g>
            </g>
            <g opacity={ui}>
                {key("Ctrl", 920.0, 2.0, 104.0)}
                {key("C", 1010.0, 2.08, 64.0)}
                <g transform={format!("translate(0 {:.1})", 90.0 * (1.0 - bar_in))} opacity={bar_in}>
                    <rect x="800" y="972" width="320" height="56" rx="28" fill="#1C1C1E" stroke="#FFFFFF" stroke-opacity="0.12" />
                    <text x="870" y="1010" font-family={MONO} font-size="28" fill={INK}>{timer}</text>
                    <rect x="976" y="986" width="1" height="28" fill="#FFFFFF" fill-opacity="0.2" />
                    <path d="M996 990 V1010 M1004 990 V1010" stroke="#FFFFFF" stroke-width="3" stroke-linecap="round" />
                    <path d="M1036 992 A9 9 0 1 0 1045 1000 M1045 990 V1000 H1035" stroke="#FFFFFF" stroke-width="2.4" fill="none" stroke-linecap="round" />
                    <rect x="1066" y="991" width="18" height="18" rx="3" fill="#FFFFFF" />
                </g>
                <circle cx={dot_x} cy={dot_y} r={16.0 * dot_s} fill={REC} opacity={if dock < 1.0 { 1.0 } else { pulse }} />
            </g>
            {chapter(t, RECORD, "04", "Record")}
        </g>)
    }
}

// ---------------------------------------------------------------- 5. studio

#[derive(Debug)]
struct Studio;

const WORDS: [&str; 9] = ["Click", "the", "chart", "and", "it", "zooms", "in", "by", "itself."];

impl Scene for Studio {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(STUDIO)
    }

    fn render_frame<'a>(&'a self, mut frame: Frame, ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let (px0, py0, pw, ph) = PREVIEW;
        let chrome = seg(t, 0.0, 0.3);
        let play = c01((t - 0.25) / 1.5);
        let (tl_x, tl_w) = (100.0, 1380.0);
        let head = tl_x + tl_w * play;

        // The preview replays the recording: zoom segments on the timeline drive its camera.
        let zoom_a = (460.0, 760.0);
        let zoom_b = (900.0, 1160.0);
        let inside = |(a, b): (f32, f32)| in_out(seg(head, a - 40.0, 200.0)) * (1.0 - in_out(seg(head, b - 160.0, 200.0)));
        let za = inside(zoom_a);
        let zb = inside(zoom_b);
        let peak = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
        let chip = (SHOT_X + CHIP.0, SHOT_Y + CHIP.1);
        let (fx, fy, z) = if zb > 0.0 { (chip.0, chip.1, 1.0 + 0.5 * zb) } else { (peak.0, peak.1, 1.0 + 0.75 * za) };
        let scene = desktop("studio", 0.0);
        let recorded = fframes::svgr!(<g transform={camera_transform(fx, fy, z)}>{scene}</g>);

        // Karaoke captions follow the playhead.
        let font = FontQuery { family: UI, size: 34, weight: 600, ..Default::default() };
        let space = 11.0;
        let widths: Vec<f32> = WORDS.iter().map(|w| frame.text_width(ctx, font, w).unwrap_or(60) as f32).collect();
        let total: f32 = widths.iter().sum::<f32>() + space * (WORDS.len() - 1) as f32;
        let mut x = px0 + pw / 2.0 - total / 2.0;
        let cap_y = py0 + ph - 34.0;
        let words: Vec<Svgr> = WORDS
            .iter()
            .zip(&widths)
            .enumerate()
            .map(|(i, (word, w))| {
                let on = 0.3 + i as f32 * 0.15;
                let lit = seg(t, on, 0.08);
                let current = t >= on && t < on + 0.15;
                let wx = x;
                x += w + space;
                let bg = if current {
                    fframes::svgr!(<rect x={wx - 8.0} y={cap_y - 34.0} width={w + 16.0} height="46" rx="8" fill={ACCENT} />)
                } else {
                    Svgr::empty()
                };
                fframes::svgr!(<g>{bg}<text x={wx} y={cap_y} font-family={UI} font-weight="600" font-size="34" fill="#FFFFFF" opacity={0.45 + 0.55 * lit}>{*word}</text></g>)
            })
            .collect();
        let caption_in = out_expo(seg(t, 0.2, 0.4));

        let track = |i: usize| out_expo(seg(t, 0.08 + i as f32 * 0.07, 0.45));
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
                let grow = out_cubic(seg(t, 0.15 + i as f32 * 0.002, 0.3));
                let env = 0.35 + 0.65 * ((i as f32 * 0.21).sin() * 0.5 + 0.5) * hash(i as u32 + 40);
                let hgt = px(54.0 * env * grow);
                let fill = if x < head { "#34D399" } else { "#2F6F5A" };
                fframes::svgr!(<rect x={x} y={951.0 - hgt / 2.0} width="4" height={hgt} rx="2" fill={fill} />)
            })
            .collect();
        let zoom_pill = |(a, b): (f32, f32), label: &'a str, i: usize| -> Svgr<'a> {
            let lit = 0.55 + 0.45 * seg(head, a - 30.0, 60.0) * (1.0 - seg(head, b - 30.0, 60.0));
            fframes::svgr!(<g opacity={track(i) * lit}>
                <rect x={a} y="852" width={b - a} height="52" rx="10" fill={VIOLET} fill-opacity="0.35" stroke={VIOLET} />
                <text x={a + 18.0} y="887" font-family={UI} font-weight="600" font-size="28" fill="#FFFFFF">{label}</text>
            </g>)
        };
        let ticks: String = (0..=27).map(|i| format!("M{} 728 v{} ", tl_x + i as f32 * 51.1, if i % 5 == 0 { 14 } else { 7 })).collect();
        let time = format!("0:{:02}", (play * 12.0) as i32);

        let out = in_cubic(seg(t, 1.68, 0.32));
        let s = 1.0 - 0.06 * out;
        fframes::svgr!(<g transform={format!("translate(960 540) scale({s:.4}) translate(-960 -540)")} opacity={1.0 - out}>
            <defs>
                <linearGradient id="studio-bg" x1="0" y1="0" x2="1" y2="1">
                    <stop offset="0" stop-color="#5B8CFF" /><stop offset="0.5" stop-color="#A78BFA" /><stop offset="1" stop-color="#F472B6" />
                </linearGradient>
                <clipPath id="preview-clip"><rect x={px0} y={py0} width={pw} height={ph} rx="12" /></clipPath>
            </defs>
            <g opacity={chrome}>
                <rect width="1920" height="1080" fill="#141414" />
                <rect width="1920" height="44" fill="#202020" />
                <path d={star_path(9.0)} transform="translate(26 22)" fill={ACCENT} />
                <text x="46" y="29" font-family={UI} font-size="15" fill={MUTED}>"Glint Studio"</text>
                <rect x="310" y="90" width="960" height="540" rx="14" fill="url(#studio-bg)" />
            </g>
            <rect x={px0 + 4.0} y={py0 + 12.0} width={pw - 8.0} height={ph - 4.0} rx="12" fill="#000000" opacity={0.35 * chrome} />
            <g clip-path="url(#preview-clip)"><g transform={tf(px0, py0, pw / W)}>{recorded}</g></g>
            <g opacity={caption_in}>
                <rect x={px0 + pw / 2.0 - total / 2.0 - 22.0} y={cap_y - 46.0} width={total + 44.0} height="66" rx="14" fill="#000000" fill-opacity="0.62" />
                {words}
            </g>
            <g transform={format!("translate({:.1} 0)", 360.0 * (1.0 - out_expo(seg(t, 0.05, 0.5))))}>
                <rect x="1560" y="44" width="360" height="1036" fill="#1C1C1C" />
                <text x="1590" y="150" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Auto zoom"</text>
                <rect x="1824" y="126" width="56" height="30" rx="15" fill={ACCENT} />
                <circle cx="1864" cy="141" r="10" fill="#FFFFFF" />
                <text x="1590" y="230" font-family={UI} font-weight="600" font-size="28" fill={INK}>"Captions"</text>
                <rect x="1824" y="206" width="56" height="30" rx="15" fill={ACCENT} />
                <circle cx="1864" cy="221" r="10" fill="#FFFFFF" />
                <text x="1590" y="310" font-family={UI} font-size="28" fill={MUTED}>"Cursor size"</text>
                <rect x="1590" y="338" width="300" height="4" rx="2" fill={ACCENT} />
                <circle cx="1770" cy="340" r="10" fill="#FFFFFF" stroke={ACCENT} stroke-width="3" />
            </g>
            <g transform={format!("translate(0 {:.1})", 120.0 * (1.0 - track(0)))} opacity={track(0)}>
                <rect y="680" width="1560" height="400" fill="#181818" />
                <path d={ticks} stroke="#FFFFFF" stroke-opacity="0.3" stroke-width="2" />
            </g>
            <g opacity={track(1)} transform={format!("translate(0 {:.1})", 40.0 * (1.0 - track(1)))}>
                <rect x={tl_x} y="760" width={tl_w} height="72" rx="8" fill="#2A1C55" />
                {tiles}
                <rect x={tl_x} y="760" width={tl_w} height="72" rx="8" fill="none" stroke={ACCENT} stroke-width="2" />
            </g>
            {zoom_pill(zoom_a, "Zoom 1.75×", 2)}
            {zoom_pill(zoom_b, "Zoom 1.5×", 3)}
            <g opacity={track(4)}>{bars}</g>
            <g opacity={seg(t, 0.2, 0.2)}>
                <rect x={head - 1.5} y="716" width="3" height="270" fill={REC} />
                <path d={format!("M{} 706 h20 v12 l-10 10 l-10 -10 Z", head - 10.0)} fill={REC} />
                <text x={head} y="700" text-anchor="middle" font-family={MONO} font-size="28" fill={INK}>{time}</text>
            </g>
            {chapter(t, STUDIO, "05", "Studio")}
        </g>)
    }
}

// ---------------------------------------------------------------- 6. logo

#[derive(Debug)]
struct Logo;

const WORD: &str = "Glint";

impl Scene for Logo {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(LOGO)
    }

    fn render_frame<'a>(&'a self, mut frame: Frame, ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let size = 200;
        let font = FontQuery { family: UI, size, weight: 600, ..Default::default() };
        let word_w = frame.text_width(ctx, font, WORD).unwrap_or(460) as f32;
        let r_final = 80.0;
        let gap = 44.0;
        let total = 2.0 * r_final + gap + word_w;
        let x0 = 960.0 - total / 2.0;
        let base = 600.0;
        let star_final = (x0 + r_final, base - size as f32 * 0.36);
        let text_x = x0 + 2.0 * r_final + gap;

        // The star bursts at the centre, then slides left to make room for the word.
        let burst = spring(t, 170.0, 11.0);
        let slide = out_expo(seg(t, 0.42, 0.6));
        let sx = lerp(960.0, star_final.0, slide);
        let sy = lerp(520.0, star_final.1, slide);
        let r = lerp(150.0, r_final, slide) * burst;
        let rot = 90.0 * (1.0 - out_expo(seg(t, 0.0, 0.8))) + 8.0 * (t * 3.0).sin() * seg(t, 1.2, 0.6);
        let twinkle = 1.0 + 0.12 * (out_back(seg(t, 1.0, 0.25)) - in_cubic(seg(t, 1.25, 0.3))).max(0.0);

        let rays: Vec<Svgr> = (0..16)
            .map(|i| {
                let a = i as f32 / 16.0 * std::f32::consts::TAU + 0.2;
                let p = out_expo(seg(t, 0.02, 0.7));
                let len = if i % 2 == 0 { 520.0 } else { 300.0 };
                let (r0, r1) = (40.0 + len * 0.35 * p, 40.0 + len * p);
                fframes::svgr!(<path d={format!("M{:.1} {:.1} L{:.1} {:.1}", 960.0 + a.cos() * r0, 520.0 + a.sin() * r0, 960.0 + a.cos() * r1, 520.0 + a.sin() * r1)}
                    stroke={if i % 3 == 0 { VIOLET } else { ACCENT }} stroke-width="3" stroke-linecap="round" opacity={(1.0 - p).powf(1.4)} />)
            })
            .collect();

        // Letters rise out of a mask below the baseline, 45 ms apart.
        let letters: Vec<Svgr> = WORD
            .char_indices()
            .map(|(i, ch)| {
                let prefix = &WORD[..i];
                let lx = text_x + frame.text_width(ctx, font, prefix).unwrap_or(0) as f32 + if i > 0 { 0.0 } else { 0.0 };
                let rise = spring(t - 0.55 - i as f32 * 0.045, 200.0, 19.0);
                let y = base + 230.0 * (1.0 - rise);
                let s = ch.to_string();
                fframes::svgr!(<text x={lx} y={y} font-family={UI} font-weight="600" font-size={size} fill={INK}>{s}</text>)
            })
            .collect();
        let sweep_x = lerp(text_x - 300.0, text_x + word_w + 300.0, in_out(seg(t, 0.95, 0.65)));
        let sheen: Vec<Svgr> = WORD
            .char_indices()
            .map(|(i, ch)| {
                let lx = text_x + frame.text_width(ctx, font, &WORD[..i]).unwrap_or(0) as f32;
                let s = ch.to_string();
                fframes::svgr!(<text x={lx} y={base} font-family={UI} font-weight="600" font-size={size} fill="url(#sheen)">{s}</text>)
            })
            .collect();
        let tag = out_expo(seg(t, 1.05, 0.5));
        let glow = burst.min(1.2);
        let sparkles: Vec<Svgr> = [(1.0, 70.0, -64.0, 16.0), (1.1, -58.0, 70.0, 11.0), (1.2, 96.0, 48.0, 9.0)]
            .iter()
            .map(|&(at, dx, dy, rr): &(f32, f32, f32, f32)| {
                let p = seg(t, at, 0.5);
                let s = (std::f32::consts::PI * p).sin();
                fframes::svgr!(<path d={star_path(rr)} transform={format!("translate({:.1} {:.1}) scale({s:.3})", sx + dx, sy + dy)} fill="#FFFFFF" opacity={s} />)
            })
            .collect();

        fframes::svgr!(<g>
            <defs>
                <radialGradient id="logo-glow" cx="0.5" cy="0.5" r="0.5">
                    <stop offset="0" stop-color={ACCENT} stop-opacity="0.55" />
                    <stop offset="0.5" stop-color={VIOLET} stop-opacity="0.18" />
                    <stop offset="1" stop-color={VIOLET} stop-opacity="0" />
                </radialGradient>
                <linearGradient id="star-fill" x1="0" y1="0" x2="1" y2="1">
                    <stop offset="0" stop-color="#FFFFFF" /><stop offset="0.6" stop-color="#DCE6FF" /><stop offset="1" stop-color={VIOLET} />
                </linearGradient>
                <linearGradient id="sheen" gradientUnits="userSpaceOnUse" x1={sweep_x - 160.0} y1="0" x2={sweep_x + 160.0} y2="0">
                    <stop offset="0" stop-color="#FFFFFF" stop-opacity="0" />
                    <stop offset="0.5" stop-color="#C9D8FF" stop-opacity="0.95" />
                    <stop offset="1" stop-color="#FFFFFF" stop-opacity="0" />
                </linearGradient>
                <clipPath id="word-mask"><rect x="0" y="0" width="1920" height={base + 54.0} /></clipPath>
                <filter id="star-blur" x="-100%" y="-100%" width="300%" height="300%"><feGaussianBlur stdDeviation="22" /></filter>
            </defs>
            <circle cx={sx} cy={sy} r={520.0 * glow.max(0.01)} fill="url(#logo-glow)" opacity={0.9 * glow.min(1.0)} />
            {rays}
            <g transform={format!("translate({sx:.1} {sy:.1}) rotate({rot:.2}) scale({twinkle:.3})")}>
                <path d={star_path(px(r) * 1.25)} fill={ACCENT} opacity="0.85" filter="url(#star-blur)" />
                <path d={star_path(px(r))} fill="url(#star-fill)" />
            </g>
            {sparkles}
            <g clip-path="url(#word-mask)">{letters}</g>
            <g clip-path="url(#word-mask)" opacity={seg(t, 0.9, 0.1)}>{sheen}</g>
            <g transform={format!("translate(960 {:.1})", 700.0 + 30.0 * (1.0 - tag))} opacity={tag}>
                <text text-anchor="middle" font-family={UI} font-size="44" fill={MUTED}>"Screenshots and screen recordings for Windows"</text>
            </g>
        </g>)
    }
}
