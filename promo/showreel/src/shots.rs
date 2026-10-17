//! The montage vignettes, one per row of `SHOTS`. Each draws from its local time `t` in
//! seconds. Event times are local constants shared by the drawing and the shot's cues, so a
//! vignette keeps its sound in sync when the table moves it.
use crate::kit::*;
use crate::motion::*;
use crate::{ACCENT, BG, Cue, HOT, INK, MONO, MUTED, REC, UI, VIOLET};
use fframes::{FFramesContext, FontQuery, Frame, Svgr};

type Ctx<'a, 'b> = FFramesContext<'a, 'b>;

fn dim<'a>(o: f32) -> Svgr<'a> {
    fframes::svgr!(<rect width="1920" height="1080" fill="#05060A" opacity={o} />)
}

/// A marching-ants frame around `r`, with everything outside it dimmed.
fn guide<'a>(r: (f32, f32, f32, f32), t: f32, dim_o: f32, o: f32) -> Svgr<'a> {
    let (x, y, w, h) = r;
    fframes::svgr!(<g opacity={o}>
        <path d={format!("M-100 -100 H2020 V1180 H-100 Z M{x} {y} V{} H{} V{y} Z", y + h, x + w)} fill="#05060A" fill-opacity={dim_o} fill-rule="evenodd" />
        <rect x={x} y={y} width={w} height={h} fill="none" stroke="#FFFFFF" stroke-width="2.5" stroke-dasharray="12 9" stroke-dashoffset={format!("{:.1}", -t * 90.0)} />
    </g>)
}

// ---------------------------------------------------------------- capture

const AREA_SNAP: f32 = 0.5;
pub const AREA_CUES: &[Cue] = &[(AREA_SNAP, "snap.wav", -5.0)];

pub fn area<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    // The drag is already under way on the first frame and decelerates into the snap.
    let drag = lerp(0.12, 1.0, out_cubic(seg(t, 0.0, AREA_SNAP)));
    let (ax, ay) = (SHOT_X, SHOT_Y);
    let (cx, cy) = (lerp(ax, ax + SHOT_W, drag), lerp(ay, ay + SHOT_H, drag));
    let (sw, sh) = (px(cx - ax), px(cy - ay));
    let snapped = t >= AREA_SNAP;
    let tool = 1.0 - seg(t, AREA_SNAP, 0.06);
    let dim_o = 0.5 + 0.25 * out_cubic(seg(t, AREA_SNAP, 0.3));
    let push = 1.0 + 0.045 * out_cubic(seg(t, AREA_SNAP, 0.5));
    let overlay = format!("M0 0 H1920 V1080 H0 Z M{ax} {ay} h{sw} v{sh} h{} Z", -sw);

    // The magnifier shows the pixels under the cursor at 5x with a pixel grid.
    let mag_r = 74.0;
    let (mx, my) = (cx + 92.0, cy + 92.0);
    let loupe = if tool > 0.0 {
        let inner = shot("loupe", 0.0);
        let grid: String = (-7..=7)
            .map(|i| {
                format!(
                    "M{} {} v{} M{} {} h{} ",
                    mx + i as f32 * 10.0 - 5.0,
                    my - mag_r,
                    mag_r * 2.0,
                    mx - mag_r,
                    my + i as f32 * 10.0 - 5.0,
                    mag_r * 2.0
                )
            })
            .collect();
        fframes::svgr!(<g opacity={tool}>
            <defs><clipPath id="loupe-clip"><circle cx={mx} cy={my} r={mag_r} /></clipPath></defs>
            <circle cx={mx} cy={my + 6.0} r={mag_r + 4.0} fill="#000000" opacity="0.35" />
            <g clip-path="url(#loupe-clip)">
                <rect x={mx - mag_r} y={my - mag_r} width={mag_r * 2.0} height={mag_r * 2.0} fill="#22174A" />
                <g transform={tf(mx - cx * 5.0, my - cy * 5.0, 5.0)}>
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
    let cross = format!(
        "M{} {cy} h36 M{} {cy} h-36 M{cx} {} v36 M{cx} {} v-36",
        cx - 46.0,
        cx + 46.0,
        cy - 46.0,
        cy + 46.0
    );
    let size = format!(
        "{} × {}",
        (sw * 2.0).round() as i32,
        (sh * 2.0).round() as i32
    );
    let handles: Vec<Svgr> = [(ax, ay), (ax + sw, ay), (ax, ay + sh), (ax + sw, ay + sh), (ax + sw / 2.0, ay), (ax + sw / 2.0, ay + sh), (ax, ay + sh / 2.0), (ax + sw, ay + sh / 2.0)]
        .iter()
        .enumerate()
        .map(|(i, (x, y))| {
            let s = out_back(seg(t, AREA_SNAP + i as f32 * 0.025, 0.28)) * 7.0;
            if s <= 0.0 {
                return Svgr::empty();
            }
            fframes::svgr!(<rect x={x - s} y={y - s} width={px(2.0 * s)} height={px(2.0 * s)} rx="2" fill="#FFFFFF" stroke={ACCENT} stroke-width="2" />)
        })
        .collect();
    let scene = desktop("area", 0.0);
    fframes::svgr!(<g>
        <g transform={about(960.0, 560.0, push)}>
            {scene}
            <path d={overlay} fill="#05060A" fill-opacity={dim_o} fill-rule="evenodd" />
            <rect x={ax} y={ay} width={sw} height={sh} fill="none" stroke={if snapped { ACCENT } else { "#FFFFFF" }} stroke-width={if snapped { 3.0 } else { 1.5 }} />
            {handles}
        </g>
        {flash(t, AREA_SNAP)}
        <g opacity={tool} stroke-linecap="round">
            <path d={format!("M{cx} 0 V1080 M0 {cy} H1920")} stroke="#FFFFFF" stroke-opacity="0.18" stroke-width="1" />
            <path d={cross.clone()} stroke="#000000" stroke-width="5" />
            <path d={cross} stroke="#FFFFFF" stroke-width="2.5" />
        </g>
        <g opacity={tool} transform={format!("translate({:.1} {:.1})", cx - 250.0, cy + 18.0)}>
            <rect width="226" height="48" rx="24" fill="#000000" fill-opacity="0.72" />
            <text x="113" y="34" text-anchor="middle" font-family={MONO} font-size="28" fill="#FFFFFF">{size}</text>
        </g>
        {loupe}
    </g>)
}

const WINDOW_CLICK: f32 = 0.22;
pub const WINDOW_CUES: &[Cue] = &[
    (WINDOW_CLICK, "click.wav", -5.0),
    (WINDOW_CLICK + 0.02, "snap.wav", -7.0),
];
const NOTES: (f32, f32, f32, f32) = (96.0, 150.0, 270.0, 300.0);

pub fn window<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    // The highlight is already sliding from the small window to the app window.
    let hop = lerp(0.3, 1.0, in_out(seg(t, 0.0, 0.16)));
    let (nx, ny, nw, nh) = NOTES;
    let (wx, wy, ww, wh) = WIN;
    let (hx, hy, hw, hh) = (
        lerp(nx, wx, hop),
        lerp(ny, wy, hop),
        lerp(nw, ww, hop),
        lerp(nh, wh, hop),
    );
    let reach = lerp(0.3, 1.0, out_cubic(seg(t, 0.0, 0.2)));
    let (cx, cy) = (lerp(300.0, 900.0, reach), lerp(330.0, 560.0, reach));
    let lift = out_expo(seg(t, WINDOW_CLICK + 0.02, 0.26));
    let s = lerp(1.0, 0.88, lift);
    let bars: Vec<Svgr> = (0..6)
        .map(|i| {
            let w = 120.0 + 100.0 * hash(i + 3);
            fframes::svgr!(<rect x={nx + 24.0} y={ny + 70.0 + i as f32 * 34.0} width={w} height="10" rx="5" fill="#FFFFFF" fill-opacity="0.22" />)
        })
        .collect();
    let wall = wallpaper("win-wall");
    let bar = taskbar("win-bar");
    let app = app_window("win", 0.0);
    fframes::svgr!(<g>
        <defs><filter id="lift-shadow" x="-20%" y="-20%" width="140%" height="160%"><feGaussianBlur stdDeviation="34" /></filter></defs>
        {wall}
        <rect x={nx} y={ny} width={nw} height={nh} rx="10" fill="#1F2026" stroke="#FFFFFF" stroke-opacity="0.1" />
        <rect x={nx} y={ny} width={nw} height="40" rx="10" fill="#26272E" />
        <rect x={nx} y={ny + 30.0} width={nw} height="10" fill="#26272E" />
        <circle cx={nx + 24.0} cy={ny + 20.0} r="6" fill={crate::AMBER} />
        {bars}
        {bar}
        {dim(0.82 * lift)}
        <g transform={about(960.0, 550.0, s)}>
            <rect x="420" y="250" width="1080" height="680" rx="14" fill="#000000" opacity={0.7 * lift} filter="url(#lift-shadow)" />
            {app}
        </g>
        <rect x={hx} y={hy} width={hw} height={hh} rx="10" fill={ACCENT} fill-opacity="0.16" stroke={ACCENT} stroke-width="4" opacity={1.0 - seg(t, WINDOW_CLICK, 0.06)} />
        {ripple(cx, cy, t, WINDOW_CLICK, ACCENT)}
        {cursor(cx, cy, 1.0 - seg(t, WINDOW_CLICK + 0.06, 0.1))}
        {flash(t, WINDOW_CLICK + 0.02)}
    </g>)
}

pub fn full_screen<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let p = out_expo(seg(t, 0.04, 0.42));
    let s = lerp(1.0, 0.56, p);
    let (cx, cy) = (lerp(960.0, 1080.0, p), lerp(540.0, 470.0, p));
    let r = 28.0 * p / s;
    let scene = desktop("full", 0.0);
    fframes::svgr!(<g>
        <defs>
            <clipPath id="full-clip"><rect width="1920" height="1080" rx={r} /></clipPath>
            <filter id="full-shadow" x="-20%" y="-20%" width="140%" height="160%"><feGaussianBlur stdDeviation="40" /></filter>
            <radialGradient id="full-glow" cx="0.5" cy="0.5" r="0.5">
                <stop offset="0" stop-color={ACCENT} stop-opacity="0.28" />
                <stop offset="1" stop-color={ACCENT} stop-opacity="0" />
            </radialGradient>
        </defs>
        <circle cx="1080" cy="470" r="900" fill="url(#full-glow)" opacity={p} />
        <g transform={format!("translate({:.2} {:.2}) scale({s:.4})", cx - 960.0 * s, cy - 540.0 * s)}>
            <rect x="40" y="90" width="1840" height="1020" rx={r} fill="#000000" opacity={0.75 * p} filter="url(#full-shadow)" />
            <g clip-path="url(#full-clip)">{scene}</g>
            <rect width="1920" height="1080" rx={r} fill="none" stroke="#FFFFFF" stroke-opacity={0.25 * p} stroke-width={2.0 / s} />
        </g>
    </g>)
}

const TICK: f32 = 1.0 / 6.0;
const TIMER_FIRE: f32 = 0.46;
pub const TIMER_CUES: &[Cue] = &[
    (TICK, "tick.wav", -8.0),
    (2.0 * TICK, "tick.wav", -8.0),
    (TIMER_FIRE, "snap.wav", -6.0),
];

pub fn self_timer<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (cx, cy) = (960.0, 500.0);
    let r = 92.0;
    let circ = std::f32::consts::TAU * r;
    let progress = c01(t / TIMER_FIRE);
    let n = ((t / TICK) as usize).min(2);
    let local = t - n as f32 * TICK;
    let digit = |d: usize, y: f32, o: f32| -> Svgr<'a> {
        fframes::svgr!(<text x={cx} y={y} text-anchor="middle" font-family={UI} font-weight="600" font-size="128" fill={INK} opacity={o}>{(3 - d).to_string()}</text>)
    };
    let rise = out_expo(seg(local, 0.04, 0.14));
    let base = cy + 46.0;
    let incoming = digit(n, base + 120.0 * (1.0 - rise), 1.0);
    let outgoing = if n > 0 {
        digit(n - 1, base - 120.0 * in_cubic(seg(local, 0.0, 0.08)), 1.0)
    } else {
        Svgr::empty()
    };
    let fire = in_cubic(seg(t, TIMER_FIRE - 0.04, 0.06));
    let chip_s = lerp(1.0, 0.9, fire);
    let scene = desktop("timer", 0.0);
    fframes::svgr!(<g>
        <g transform={about(960.0, 540.0, 1.06)}>{scene}</g>
        {dim(0.55)}
        <g transform={about(cx, cy, chip_s)}>
            <defs><clipPath id="digit-mask"><circle cx={cx} cy={cy} r={r - 10.0} /></clipPath></defs>
            <rect x={cx - 140.0} y={cy - 124.0} width="280" height="280" rx="60" fill="#000000" opacity="0.45" />
            <rect x={cx - 140.0} y={cy - 140.0} width="280" height="280" rx="60" fill="#1E1E22" fill-opacity="0.96" stroke="#FFFFFF" stroke-opacity="0.12" />
            <circle cx={cx} cy={cy} r={r} fill="none" stroke="#FFFFFF" stroke-opacity="0.1" stroke-width="10" />
            <circle cx={cx} cy={cy} r={r} fill="none" stroke={ACCENT} stroke-width="10" stroke-linecap="round" transform={format!("rotate(-90 {cx} {cy})")}
                stroke-dasharray={format!("{circ:.1} {circ:.1}")} stroke-dashoffset={circ * progress} />
            <g clip-path="url(#digit-mask)">{outgoing}{incoming}</g>
        </g>
        {flash(t, TIMER_FIRE)}
    </g>)
}

const TEXT_LIFT: f32 = 0.46;
const TEXT_TOAST: f32 = 0.7;
pub const TEXT_CUES: &[Cue] = &[
    (TEXT_LIFT, "whoosh_short.wav", -13.0),
    (TEXT_TOAST, "pop.wav", -8.0),
];
const DOC: (f32, f32, f32, f32) = (420.0, 150.0, 1200.0, 660.0);
const DOC_LINES: [(&str, f32, usize); 4] = [
    ("March review", 312.0, 700),
    ("Revenue grew 12.4% this month.", 392.0, 400),
    ("Refunds stayed under 1%.", 456.0, 400),
    ("Next, ship the new export panel.", 520.0, 400),
];

pub fn capture_text<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (dx, dy, dw, dh) = DOC;
    let sel = (476.0, 248.0, 760.0, 306.0);
    let scan = lerp(0.08, 1.0, in_out(seg(t, 0.0, 0.42)));
    let scan_y = sel.1 + sel.3 * scan;
    let scan_o = 1.0 - seg(t, 0.4, 0.08);
    let size = |w: usize| if w == 700 { 54 } else { 40 };
    let lines: Vec<Svgr> = DOC_LINES
        .iter()
        .map(|&(text, y, weight)| {
            let hit = seg(scan_y, y - 30.0, 30.0);
            let width = text.len() as f32 * if weight == 700 { 27.0 } else { 19.5 };
            fframes::svgr!(<g>
                <rect x="490" y={y - 40.0} width={width + 20.0} height="54" rx="8" fill={ACCENT} fill-opacity={0.2 * hit} />
                <text x="500" y={y} font-family={UI} font-weight={weight} font-size={size(weight)} fill="#111827">{text}</text>
            </g>)
        })
        .collect();
    let lifted: Vec<Svgr> = DOC_LINES
        .iter()
        .enumerate()
        .map(|(j, &(text, y, weight))| {
            let p = in_out(seg(t, TEXT_LIFT + j as f32 * 0.05, 0.28));
            if p <= 0.0 || p >= 1.0 {
                return Svgr::empty();
            }
            let (x, yy) = (lerp(500.0, 860.0, p), lerp(y, 128.0, p) - 90.0 * (std::f32::consts::PI * p).sin());
            fframes::svgr!(<text transform={tf(x, yy, lerp(1.0, 0.3, p))} font-family={UI} font-weight={weight} font-size={size(weight)} fill={ACCENT} opacity={1.0 - p * p}>{text}</text>)
        })
        .collect();
    let wall = wallpaper("text-wall");
    fframes::svgr!(<g>
        {wall}
        <defs>
            <linearGradient id="scan-glow" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stop-color={ACCENT} stop-opacity="0" />
                <stop offset="1" stop-color={ACCENT} stop-opacity="0.35" />
            </linearGradient>
            <clipPath id="scan-clip"><rect x={sel.0} y={sel.1} width={sel.2} height={sel.3} /></clipPath>
        </defs>
        <rect x={dx + 12.0} y={dy + 30.0} width={dw - 24.0} height={dh} rx="12" fill="#000000" opacity="0.5" />
        <rect x={dx} y={dy} width={dw} height={dh} rx="12" fill="#FFFFFF" />
        <rect x={dx} y={dy} width={dw} height="44" rx="12" fill="#F1F2F5" />
        <rect x={dx} y={dy + 30.0} width={dw} height="14" fill="#F1F2F5" />
        <text x={dx + 22.0} y={dy + 29.0} font-family={UI} font-size="16" fill="#4B5563">"Notes"</text>
        <rect x="500" y="580" width="560" height="12" rx="6" fill="#E5E7EB" />
        <rect x="500" y="618" width="420" height="12" rx="6" fill="#E5E7EB" />
        <rect x="500" y="656" width="500" height="12" rx="6" fill="#E5E7EB" />
        {lines}
        {guide(sel, t, 0.45, 1.0)}
        <g clip-path="url(#scan-clip)" opacity={scan_o}>
            <rect x={sel.0} y={scan_y - 90.0} width={sel.2} height="90" fill="url(#scan-glow)" />
            <rect x={sel.0} y={scan_y - 2.0} width={sel.2} height="4" fill={ACCENT} />
        </g>
        {lifted}
        {toast(960.0, 128.0, "Text copied", 260.0, seg(t, TEXT_TOAST - 0.04, 0.4))}
    </g>)
}

// ---------------------------------------------------------------- quick access

const STACK_X: f32 = 1326.0;
const STACK_W: f32 = 440.0;
const STACK_Y: [f32; 3] = [690.0, 424.0, 158.0];

fn stack_content<'a>(slot: usize, id: &str) -> Svgr<'a> {
    match slot {
        0 => shot(id, 0.0),
        1 => thumb_photo(id.to_string()),
        _ => thumb_code(id.to_string()),
    }
}

const CARDS_HOVER: f32 = 0.36;
const CARDS_COPY: f32 = 0.56;
pub const CARDS_CUES: &[Cue] = &[
    (0.04, "pop.wav", -13.0),
    (0.11, "pop.wav", -14.0),
    (CARDS_COPY, "click.wav", -5.0),
    (CARDS_COPY + 0.04, "pop_hi.wav", -10.0),
];

pub fn preview_cards<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let ch = STACK_W * SHOT_H / SHOT_W;
    let cards: Vec<Svgr> = (0..3)
        .rev()
        .map(|j| {
            let p = spring(t + 0.12 - j as f32 * 0.07, 240.0, 22.0);
            let x = lerp(1980.0, STACK_X, p);
            card(
                &format!("pc{j}"),
                x,
                STACK_Y[j],
                STACK_W,
                stack_content(j, &format!("pc{j}-c")),
                1.0,
            )
        })
        .collect();
    let (kx, ky) = (STACK_X, STACK_Y[0]);
    let mid = kx + STACK_W / 2.0;
    let (copy_y, save_y) = (ky + 96.0, ky + 160.0);
    let reach = lerp(0.2, 1.0, in_out(seg(t, 0.0, 0.36)));
    let to_copy = in_out(seg(t, 0.4, 0.14));
    let cx = lerp(lerp(900.0, mid + 40.0, reach), mid + 10.0, to_copy);
    let cy = lerp(lerp(780.0, ky + 190.0, reach), copy_y + 4.0, to_copy);
    let hover = seg(t, CARDS_HOVER, 0.1);
    let copied = t >= CARDS_COPY;
    let pill = |label: &'a str, y: f32, at: f32, pressed: bool| -> Svgr<'a> {
        let s = out_back(seg(t, at, 0.26));
        fframes::svgr!(<g transform={format!("translate({mid:.1} {y:.1}) scale({s:.3})")} opacity={c01(s)}>
            <rect x="-84" y="-25" width="168" height="50" rx="25" fill={if pressed { ACCENT } else { "#FFFFFF" }} fill-opacity="0.95" />
            <text y="10" text-anchor="middle" font-family={UI} font-weight="600" font-size="28" fill={if pressed { "#FFFFFF" } else { "#111827" }}>{label}</text>
        </g>)
    };
    let corner = |x: f32, y: f32, glyph: &'a str, at: f32| -> Svgr<'a> {
        let s = out_back(seg(t, at, 0.24));
        fframes::svgr!(<g transform={format!("translate({x:.1} {y:.1}) scale({s:.3})")} opacity={c01(s)}>
            <circle r="18" fill="#111827" fill-opacity="0.85" />
            <path d={glyph} stroke="#FFFFFF" stroke-width="2.2" fill="none" stroke-linecap="round" stroke-linejoin="round" />
        </g>)
    };
    let hover_ui = if hover > 0.0 {
        fframes::svgr!(<g>
            <rect x={kx} y={ky} width={STACK_W} height={ch} rx="14" fill="#000000" opacity={0.45 * hover} />
            {pill("Copy", copy_y, CARDS_HOVER + 0.02, copied)}
            {pill("Save", save_y, CARDS_HOVER + 0.06, false)}
            {corner(kx + 26.0, ky + 26.0, "M-5 -5 L5 5 M5 -5 L-5 5", CARDS_HOVER + 0.04)}
            {corner(kx + STACK_W - 26.0, ky + 26.0, "M-6 6 L-6 2 L3 -7 L7 -3 L-2 6 Z", CARDS_HOVER + 0.07)}
            {corner(kx + 26.0, ky + ch - 26.0, "M0 -7 V3 M-5 3 H5 M0 3 V8", CARDS_HOVER + 0.1)}
            {corner(kx + STACK_W - 26.0, ky + ch - 26.0, "M-6 0 h0.1 M0 0 h0.1 M6 0 h0.1", CARDS_HOVER + 0.13)}
        </g>)
    } else {
        Svgr::empty()
    };
    let scene = desktop("cards", 0.0);
    fframes::svgr!(<g>
        {scene}
        {dim(0.45)}
        {cards}
        {hover_ui}
        {ripple(mid, copy_y, t, CARDS_COPY, ACCENT)}
        {cursor(cx, cy, 1.0)}
        {toast(960.0, 128.0, "Copied to clipboard", 360.0, seg(t, CARDS_COPY + 0.02, 0.4))}
    </g>)
}

const DROP: f32 = 0.3;
pub const DRAG_CUES: &[Cue] = &[(DROP, "pop.wav", -7.0)];
const FOLDER: (f32, f32, f32, f32) = (190.0, 160.0, 860.0, 420.0);

fn folder_slot(i: usize) -> (f32, f32) {
    (
        420.0 + (i % 3) as f32 * 204.0,
        236.0 + (i / 3) as f32 * 146.0,
    )
}

pub fn drag_and_drop<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (fx, fy, fw, fh) = FOLDER;
    let (tw, th) = (180.0, 112.0);
    let target = folder_slot(5);
    let drop_at = (target.0 + tw / 2.0, target.1 + th / 2.0);
    let go = lerp(0.18, 1.0, out_cubic(seg(t, 0.0, DROP)));
    let (cx, cy) = (lerp(1520.0, drop_at.0, go), lerp(800.0, drop_at.1, go));
    let land = out_expo(seg(t, DROP, 0.16));
    let hovering = seg(t, 0.12, 0.08) * (1.0 - seg(t, DROP + 0.04, 0.12));
    let dw = lerp(300.0, tw, land);
    let (gx, gy) = (
        lerp(cx - 70.0, target.0, land),
        lerp(cy - 50.0, target.1, land),
    );
    let tilt = 6.0 * (1.0 - land);
    let ghost = card("drag", 0.0, 0.0, dw, shot("drag-c", 0.0), 1.0);
    let tiles: Vec<Svgr> = (0..5)
        .map(|i| {
            let (x, y) = folder_slot(i);
            let c = match i % 3 {
                0 => thumb_photo(format!("ft{i}")),
                1 => thumb_code(format!("ft{i}")),
                _ => shot(&format!("ft{i}"), 0.0),
            };
            framed(&format!("ftf{i}"), x, y, tw, th, 8.0, SHOT_W, c)
        })
        .collect();
    let older: Vec<Svgr> = (1..3)
        .map(|j| {
            card(
                &format!("dd{j}"),
                STACK_X,
                STACK_Y[j],
                STACK_W,
                stack_content(j, &format!("dd{j}-c")),
                1.0,
            )
        })
        .collect();
    let scene = desktop("drag", 0.0);
    fframes::svgr!(<g>
        {scene}
        {dim(0.45)}
        {older}
        <rect x={fx + 10.0} y={fy + 26.0} width={fw - 20.0} height={fh} rx="12" fill="#000000" opacity="0.5" />
        <rect x={fx} y={fy} width={fw} height={fh} rx="12" fill="#202020" stroke={ACCENT} stroke-opacity={hovering} stroke-width="3" />
        <rect x={fx} y={fy} width={fw} height="44" rx="12" fill="#2B2B2B" />
        <rect x={fx} y={fy + 30.0} width={fw} height="14" fill="#2B2B2B" />
        <path d={format!("M{} {} h14 l4 4 h12 v14 h-30 Z", fx + 20.0, fy + 14.0)} fill="#F2B530" />
        <text x={fx + 60.0} y={fy + 30.0} font-family={UI} font-size="18" fill={INK}>"Screenshots"</text>
        <rect x={fx + 20.0} y={fy + 76.0} width="150" height="10" rx="5" fill="#FFFFFF" fill-opacity="0.18" />
        <rect x={fx + 20.0} y={fy + 112.0} width="120" height="10" rx="5" fill="#FFFFFF" fill-opacity="0.12" />
        <rect x={fx + 20.0} y={fy + 148.0} width="132" height="10" rx="5" fill="#FFFFFF" fill-opacity="0.12" />
        {tiles}
        <rect x={target.0} y={target.1} width={tw} height={th} rx="8" fill={ACCENT} fill-opacity={0.18 * hovering} stroke={ACCENT} stroke-opacity={hovering} stroke-width="2" stroke-dasharray="8 6" />
        <g transform={format!("translate({gx:.1} {gy:.1}) rotate({tilt:.2})")}>{ghost}</g>
        <g transform={format!("translate({:.1} {:.1}) scale({:.3})", cx + 34.0, cy + 40.0, out_back(seg(t, 0.12, 0.2)) * (1.0 - land))}>
            <circle r="16" fill={ACCENT} />
            <path d="M-7 0 H7 M0 -7 V7" stroke="#FFFFFF" stroke-width="2.6" stroke-linecap="round" />
        </g>
        {cursor(cx, cy, 1.0)}
    </g>)
}

const PIN_BADGE: f32 = 0.12;
pub const PIN_CUES: &[Cue] = &[(PIN_BADGE, "pop_hi.wav", -9.0)];

pub fn pin<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (ex, ey, ew, eh) = (150.0, 96.0, 1620.0, 880.0);
    let scroll = 140.0 + t * 220.0;
    let colors = [
        "#C678DD", "#61AFEF", "#98C379", "#E5C07B", "#56B6C2", "#E06C75",
    ];
    let code: Vec<Svgr> = (0..30u32)
        .map(|i| {
            let y = ey + 70.0 + i as f32 * 40.0 - scroll % 40.0 - 40.0;
            let row = i + (scroll / 40.0) as u32;
            let indent = (hash(row * 3 + 1) * 4.0).floor() * 36.0;
            let w = 120.0 + 420.0 * hash(row * 7 + 2);
            let w2 = 60.0 + 200.0 * hash(row * 11 + 5);
            fframes::svgr!(<g>
                <text x={ex + 30.0} y={y + 7.0} font-family={MONO} font-size="16" fill="#5C6370">{(row + 1).to_string()}</text>
                <rect x={ex + 90.0 + indent} y={y - 7.0} width={w} height="14" rx="7" fill={colors[(row % 6) as usize]} fill-opacity="0.75" />
                <rect x={ex + 106.0 + indent + w} y={y - 7.0} width={w2} height="14" rx="7" fill={colors[((row + 3) % 6) as usize]} fill-opacity="0.55" />
            </g>)
        })
        .collect();
    let sp = spring(t + 0.08, 300.0, 18.0);
    let (pw, ph) = (560.0, 560.0 * SHOT_H / SHOT_W);
    let (px0, py0) = (1180.0, 150.0);
    let s = lerp(1.22, 1.0, sp);
    let rot = lerp(5.0, 0.0, sp);
    let badge = out_back(seg(t, PIN_BADGE, 0.24));
    let pinned = framed(
        "pinned",
        0.0,
        0.0,
        pw,
        ph,
        12.0,
        SHOT_W,
        shot("pinned-c", 0.0),
    );
    let wall = wallpaper("pin-wall");
    fframes::svgr!(<g>
        {wall}
        <defs>
            <clipPath id="code-clip"><rect x={ex} y={ey + 44.0} width={ew} height={eh - 44.0} /></clipPath>
            <filter id="pin-shadow" x="-30%" y="-30%" width="160%" height="180%"><feGaussianBlur stdDeviation="26" /></filter>
        </defs>
        <rect x={ex} y={ey} width={ew} height={eh} rx="12" fill="#1B1D23" />
        <rect x={ex} y={ey} width={ew} height="44" rx="12" fill="#22252C" />
        <rect x={ex} y={ey + 30.0} width={ew} height="14" fill="#22252C" />
        <text x={ex + 22.0} y={ey + 29.0} font-family={UI} font-size="16" fill={MUTED}>"report.ts"</text>
        <g clip-path="url(#code-clip)">{code}</g>
        {dim(0.2 * sp.min(1.0))}
        <g transform={format!("translate({:.1} {:.1}) rotate({rot:.2}) scale({s:.4}) translate({:.1} {:.1})", px0 + pw / 2.0, py0 + ph / 2.0, -pw / 2.0, -ph / 2.0)}>
            <rect x="8" y="28" width={pw - 16.0} height={ph - 8.0} rx="14" fill="#000000" opacity="0.7" filter="url(#pin-shadow)" />
            {pinned}
            <rect width={pw} height={ph} rx="12" fill="none" stroke={ACCENT} stroke-width="3" />
            <g transform={format!("translate(0 0) scale({badge:.3})")} opacity={c01(badge)}>
                <circle r="24" fill={ACCENT} stroke="#FFFFFF" stroke-width="3" />
                <path d="M-4 -10 h8 l-1 7 l5 4 h-16 l5 -4 Z M0 1 V11" fill="#FFFFFF" stroke="#FFFFFF" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
            </g>
        </g>
    </g>)
}

// ---------------------------------------------------------------- editor

/// Toolbar indicator sliding between tools at the given switch times.
fn tool_at(t: f32, hops: &[(f32, usize)]) -> (f32, usize) {
    let mut pos = hops[0].1 as f32;
    let mut lit = hops[0].1;
    for w in hops.windows(2) {
        let (at, to) = w[1];
        pos = lerp(pos, to as f32, out_expo(seg(t, at, 0.18)));
        if t >= at {
            lit = to;
        }
    }
    (pos, lit)
}

const MARK_AT: [f32; 3] = [0.5, 0.625, 0.75];
pub const ANNOTATE_CUES: &[Cue] = &[
    (0.26, "whoosh_short.wav", -16.0),
    (MARK_AT[0], "pop.wav", -8.0),
    (MARK_AT[1], "pop.wav", -8.0),
    (MARK_AT[2], "pop_hi.wav", -8.0),
];

pub fn annotate<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let marks = Marks {
        arrow: out_cubic(seg(t, -0.05, 0.28)),
        rect: in_out(seg(t, 0.26, 0.2)),
        markers: MARK_AT.map(|at| spring(t - at, 320.0, 14.0)),
    };
    let (tool, lit) = tool_at(t, &[(0.0, T_ARROW), (0.24, T_RECT), (0.46, T_MARKER)]);
    let view = EditorView {
        tool,
        lit,
        marks,
        redact: 0.0,
        pad: 72.0,
        bg: Bg::Gradient,
        next: None,
        swatch: 0.0,
        redaction_on: 0.0,
    };
    let pen = if t < 0.26 {
        let (a0, ac, a1) = ARROW;
        quad(a0, ac, a1, marks.arrow)
    } else if t < 0.47 {
        rect_point(marks.rect)
    } else {
        let i = MARK_AT.iter().rposition(|at| t >= at - 0.1).unwrap_or(0);
        let from = if i == 0 {
            (RECT.0, RECT.1)
        } else {
            MARKERS[i - 1]
        };
        let p = in_out(seg(t, MARK_AT[i] - 0.1, 0.09));
        (lerp(from.0, MARKERS[i].0, p), lerp(from.1, MARKERS[i].1, p))
    };
    let (cx, cy) = view.to_screen(pen);
    fframes::svgr!(<g>{view.render("ann")}{cursor(cx, cy, 1.0)}</g>)
}

const REDACT_AT: f32 = 0.06;
pub const REDACT_CUES: &[Cue] = &[(REDACT_AT, "glitch.wav", -8.0)];

pub fn smart_redaction<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let redact = in_out(seg(t, REDACT_AT, 0.3));
    let view = EditorView {
        tool: T_PIXEL as f32,
        lit: T_PIXEL,
        marks: Marks::DONE,
        redact,
        pad: 72.0,
        bg: Bg::Gradient,
        next: None,
        swatch: 0.0,
        redaction_on: seg(t, 0.02, 0.08),
    };
    let focus = view.to_screen((888.0, 470.0));
    let z = lerp(1.55, 2.2, out_expo(seg(t, 0.0, 0.5)));
    let camera = format!(
        "translate({:.2} {:.2}) scale({z:.4})",
        980.0 - focus.0 * z,
        470.0 - focus.1 * z
    );
    let sweep = view.to_screen((
        744.0 + 290.0 * redact,
        lerp(450.0, 530.0, (redact * 3.0).sin().abs()),
    ));
    fframes::svgr!(<g transform={camera}>{view.render("red")}{cursor(sweep.0, sweep.1, 1.0)}</g>)
}

const BG_SWITCH: [f32; 2] = [0.0, 0.22];
pub const BACKGROUND_CUES: &[Cue] = &[(BG_SWITCH[1], "tick.wav", -7.0)];

pub fn backgrounds<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let w1 = lerp(0.3, 1.0, seg(t, BG_SWITCH[0], 0.08));
    let w2 = seg(t, BG_SWITCH[1], 0.1);
    let (bg, next) = if t < BG_SWITCH[1] {
        (Bg::Gradient, Some((Bg::Solid, w1)))
    } else {
        (Bg::Solid, Some((Bg::Wallpaper, w2)))
    };
    let pad = lerp(
        lerp(72.0, 44.0, spring(t + 0.04, 300.0, 20.0)),
        104.0,
        spring(t - BG_SWITCH[1], 300.0, 20.0),
    );
    let swatch = lerp(0.6, 1.0, out_expo(seg(t, 0.0, 0.12))) + out_expo(seg(t, BG_SWITCH[1], 0.14));
    let view = EditorView {
        tool: 0.0,
        lit: 0,
        marks: Marks::DONE,
        redact: 1.0,
        pad,
        bg,
        next,
        swatch,
        redaction_on: 1.0,
    };
    let hop = in_out(seg(t, 0.06, 0.15));
    let (cx, cy) = (lerp(1668.0, 1724.0, hop), 206.0);
    fframes::svgr!(<g>
        {view.render("bgs")}
        {ripple(1724.0, 200.0, t, BG_SWITCH[1], "#FFFFFF")}
        {cursor(cx, cy, 1.0)}
    </g>)
}

pub fn mockups<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let p = lerp(0.2, 1.0, in_out(seg(t, 0.0, 0.46)));
    let (comp, fw, fh) = composition(
        "mock",
        CANVAS_K,
        104.0,
        Bg::Wallpaper,
        None,
        1.0,
        Marks::DONE,
    );
    let z = lerp(0.9, 1.0, p);
    let transform = format!(
        "translate(960 520) scale({z:.4}) rotate({:.2}) skewX({:.2}) scale({:.4} {:.4}) translate({:.1} {:.1})",
        -9.0 * p,
        14.0 * p,
        lerp(1.0, 1.04, p),
        lerp(1.0, 0.9, p),
        -fw / 2.0,
        -fh / 2.0
    );
    let orbit = 14.0 * (t * 4.0).sin();
    fframes::svgr!(<g>
        {bg_defs()}
        <defs>
            <radialGradient id="mock-glow" cx="0.5" cy="0.5" r="0.5">
                <stop offset="0" stop-color={VIOLET} stop-opacity="0.35" />
                <stop offset="1" stop-color={VIOLET} stop-opacity="0" />
            </radialGradient>
        </defs>
        <rect width="1920" height="1080" fill="#0B0C12" />
        <circle cx={1000.0 + orbit} cy="520" r="760" fill="url(#mock-glow)" />
        <g transform={transform}>{comp}</g>
    </g>)
}

// ---------------------------------------------------------------- record

const REC_AREA: f32 = 0.12;
const REC_MIC: f32 = 0.22;
const REC_SYS: f32 = 0.3;
const REC_CLICK: f32 = 0.44;
const REC_COUNT: f32 = 0.5;
const REC_START: f32 = 0.8;
pub const RECORD_CUES: &[Cue] = &[
    (REC_AREA, "click.wav", -6.0),
    (REC_MIC, "tick.wav", -9.0),
    (REC_SYS, "tick.wav", -9.0),
    (REC_CLICK, "click.wav", -5.0),
    (REC_COUNT, "tick.wav", -11.0),
    (REC_COUNT + 0.1, "tick.wav", -11.0),
    (REC_COUNT + 0.2, "tick.wav", -11.0),
    (REC_START, "rec.wav", -5.0),
];

fn control_bar<'a>(t: f32, timer: String, enter: f32) -> Svgr<'a> {
    let pulse = 0.65 + 0.35 * (t * 7.0).cos().abs();
    fframes::svgr!(<g transform={format!("translate(0 {:.1})", 110.0 * (1.0 - enter))} opacity={c01(enter * 2.0)}>
        <rect x="790" y="964" width="340" height="60" rx="30" fill="#1C1C1E" stroke="#FFFFFF" stroke-opacity="0.12" />
        <circle cx="830" cy="994" r="10" fill={REC} opacity={pulse} />
        <text x="856" y="1004" font-family={MONO} font-size="28" fill={INK}>{timer}</text>
        <rect x="976" y="980" width="1" height="28" fill="#FFFFFF" fill-opacity="0.2" />
        <path d="M996 984 V1004 M1004 984 V1004" stroke="#FFFFFF" stroke-width="3" stroke-linecap="round" />
        <path d="M1036 986 A9 9 0 1 0 1045 994 M1045 984 V994 H1035" stroke="#FFFFFF" stroke-width="2.4" fill="none" stroke-linecap="round" />
        <rect x="1070" y="985" width="18" height="18" rx="3" fill="#FFFFFF" />
    </g>)
}

pub fn record<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (px0, py0, pw, ph) = (540.0, 330.0, 840.0, 280.0);
    let mode = lerp(0.0, 2.0, out_expo(seg(t, REC_AREA - 0.06, 0.2)));
    let collapse = in_cubic(seg(t, REC_CLICK + 0.02, 0.1));
    let picker_in = lerp(0.6, 1.0, out_expo(seg(t, 0.0, 0.25)));
    let picker_s = picker_in * lerp(1.0, 0.85, collapse);
    let modes = ["Display", "Window", "Area"];
    let icons = [
        "M-16 -11 H16 V9 H-16 Z M-6 15 H6",
        "M-16 -11 H16 V11 H-16 Z M-16 -4 H16",
        "M-16 -6 V-11 H-11 M11 -11 H16 V-6 M16 6 V11 H11 M-11 11 H-16 V6",
    ];
    let buttons: Vec<Svgr> = modes
        .iter()
        .zip(icons)
        .enumerate()
        .map(|(i, (label, icon))| {
            let x = px0 + 40.0 + i as f32 * 196.0;
            let on = 1.0 - c01((mode - i as f32).abs());
            fframes::svgr!(<g>
                <path d={icon} transform={format!("translate({} {})", x + 90.0, py0 + 64.0)} stroke="#FFFFFF" stroke-opacity={0.6 + 0.4 * on} stroke-width="2.4" fill="none" stroke-linecap="round" stroke-linejoin="round" />
                <text x={x + 90.0} y={py0 + 124.0} text-anchor="middle" font-family={UI} font-weight="600" font-size="28" fill={INK} opacity={0.6 + 0.4 * on}>{*label}</text>
            </g>)
        })
        .collect();
    let sel_x = px0 + 40.0 + mode * 196.0;
    let targets = [
        (px0 + 40.0 + 2.0 * 196.0 + 90.0, py0 + 80.0),
        (px0 + 70.0, py0 + 216.0),
        (px0 + 390.0, py0 + 216.0),
        (px0 + 750.0, py0 + 100.0),
    ];
    let times = [REC_AREA, REC_MIC, REC_SYS, REC_CLICK];
    let mut cur = (1200.0, 760.0);
    let mut prev_t = -0.1;
    for (target, at) in targets.iter().zip(times) {
        let p = in_out(seg(t, prev_t + 0.02, at - prev_t - 0.04));
        cur = (lerp(cur.0, target.0, p), lerp(cur.1, target.1, p));
        prev_t = at;
    }
    let picker = fframes::svgr!(<g transform={about(960.0, 470.0, picker_s)} opacity={1.0 - collapse}>
        <rect x={px0 + 10.0} y={py0 + 26.0} width={pw - 20.0} height={ph} rx="18" fill="#000000" opacity="0.5" />
        <rect x={px0} y={py0} width={pw} height={ph} rx="18" fill="#202020" fill-opacity="0.97" stroke="#FFFFFF" stroke-opacity="0.12" />
        <rect x={sel_x} y={py0 + 20.0} width="180" height="132" rx="12" fill={ACCENT} fill-opacity="0.26" stroke={ACCENT} stroke-width="2" />
        {buttons}
        <rect x={px0 + 30.0} y={py0 + 172.0} width={pw - 60.0} height="1" fill="#FFFFFF" fill-opacity="0.1" />
        {toggle(px0 + 40.0, py0 + 200.0, out_expo(seg(t, REC_MIC, 0.12)))}
        <text x={px0 + 118.0} y={py0 + 226.0} font-family={UI} font-size="28" fill={INK}>"Microphone"</text>
        {toggle(px0 + 360.0, py0 + 200.0, out_expo(seg(t, REC_SYS, 0.12)))}
        <text x={px0 + 438.0} y={py0 + 226.0} font-family={UI} font-size="28" fill={INK}>"System audio"</text>
        <circle cx={px0 + 750.0} cy={py0 + 86.0} r="46" fill="none" stroke="#FFFFFF" stroke-opacity="0.5" stroke-width="3" />
        <circle cx={px0 + 750.0} cy={py0 + 86.0} r={36.0 - 4.0 * seg(t, REC_CLICK, 0.04) + 4.0 * seg(t, REC_CLICK + 0.04, 0.06)} fill={REC} />
    </g>);

    // Countdown: a ring closes around 3, 2, 1, then the dot drops into the control bar.
    let count = seg(t, REC_COUNT, REC_START - REC_COUNT);
    let n = ((count * 3.0) as usize).min(2);
    let ring_o = seg(t, REC_COUNT - 0.04, 0.06) * (1.0 - seg(t, REC_START, 0.06));
    let r = 76.0;
    let circ = std::f32::consts::TAU * r;
    let digit_local = t - REC_COUNT - n as f32 * 0.1;
    let digit_s = lerp(1.35, 1.0, out_expo(seg(digit_local, 0.0, 0.08)));
    let countdown = if ring_o > 0.0 {
        fframes::svgr!(<g opacity={ring_o}>
            <circle cx="960" cy="520" r="110" fill="#1E1E22" fill-opacity="0.94" />
            <circle cx="960" cy="520" r={r} fill="none" stroke={REC} stroke-width="8" stroke-linecap="round" transform="rotate(-90 960 520)"
                stroke-dasharray={format!("{circ:.1} {circ:.1}")} stroke-dashoffset={circ * count} />
            <text transform={format!("translate(960 556) scale({digit_s:.3})")} text-anchor="middle" font-family={UI} font-weight="600" font-size="104" fill={INK}>{(3 - n).to_string()}</text>
        </g>)
    } else {
        Svgr::empty()
    };
    let bar_in = out_expo(seg(t, REC_START, 0.3));
    let scene = desktop("rec", 0.0);
    fframes::svgr!(<g>
        {scene}
        {dim(0.35)}
        {guide(WIN, t, 0.3, seg(t, REC_AREA, 0.08))}
        {picker}
        {countdown}
        {control_bar(t, "00:00".to_string(), bar_in)}
        {ripple(cur.0, cur.1, t, REC_AREA, ACCENT)}
        {ripple(cur.0, cur.1, t, REC_CLICK, REC)}
        {cursor(cur.0, cur.1, 1.0 - seg(t, REC_CLICK + 0.04, 0.08))}
    </g>)
}

const SCRIPT: [&str; 4] = [
    "Here's last month's revenue.",
    "Click any day to see its orders.",
    "Then export the report.",
    "That's it.",
];

pub fn teleprompter<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (px0, py0, pw, ph) = (480.0, 96.0, 960.0, 170.0);
    let scroll = lerp(0.15, 1.0, in_out(seg(t, 0.0, 0.4)));
    let line_h = 54.0;
    let lines: Vec<Svgr> = SCRIPT
        .iter()
        .enumerate()
        .map(|(i, text)| {
            let y = py0 + 84.0 + (i as f32 - scroll) * line_h;
            let d = (i as f32 - scroll).abs();
            fframes::svgr!(<text x={px0 + 130.0} y={y} font-family={UI} font-weight="600" font-size="36" fill={INK} opacity={lerp(1.0, 0.35, c01(d))}>{*text}</text>)
        })
        .collect();
    let levels: Vec<Svgr> = (0..5)
        .map(|i| {
            let h = 10.0 + 34.0 * (0.5 + 0.5 * (t * 23.0 + i as f32 * 1.7).sin()) * hash(i + (t * 30.0) as u32 * 5);
            fframes::svgr!(<rect x={px0 + 44.0 + i as f32 * 12.0} y={py0 + ph / 2.0 - h / 2.0} width="6" height={px(h)} rx="3" fill="#34D399" />)
        })
        .collect();
    let push = lerp(1.0, 1.035, t / 0.5);
    let secs = format!("00:0{}", 4);
    let scene = desktop("tele", 0.0);
    fframes::svgr!(<g>
        <g transform={about(960.0, 180.0, push)}>
            {scene}
            {guide(WIN, t, 0.38, 1.0)}
        </g>
        <defs><clipPath id="script-clip"><rect x={px0 + 110.0} y={py0 + 44.0} width={pw - 130.0} height={ph - 54.0} /></clipPath></defs>
        <rect x={px0 + 8.0} y={py0 + 22.0} width={pw - 16.0} height={ph} rx="30" fill="#000000" opacity="0.5" />
        <rect x={px0} y={py0} width={pw} height={ph} rx="30" fill="#111214" fill-opacity="0.95" stroke="#FFFFFF" stroke-opacity="0.12" />
        {levels}
        <rect x={px0 + 110.0} y={py0 + 30.0} width="1" height={ph - 60.0} fill="#FFFFFF" fill-opacity="0.15" />
        <g clip-path="url(#script-clip)">{lines}</g>
        {control_bar(t + 9.0, secs, 1.0)}
    </g>)
}

// ---------------------------------------------------------------- studio

const ZOOM_CLICK: f32 = 0.2;
const ZOOM_KEYS: f32 = 0.62;
pub const ZOOM_CUES: &[Cue] = &[
    (ZOOM_CLICK, "click.wav", -5.0),
    (ZOOM_CLICK + 0.04, "whoosh_short.wav", -15.0),
    (ZOOM_KEYS, "tick.wav", -8.0),
    (ZOOM_KEYS + 0.07, "tick.wav", -8.0),
];

pub fn auto_zoom<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (cx0, cy0, cw, chh) = (96.0, 54.0, 1728.0, 972.0);
    let k = cw / 1920.0;
    let peak = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
    let z = 1.0 + 0.75 * in_out(seg(t, ZOOM_CLICK + 0.02, 0.42));
    let reach = lerp(0.3, 1.0, in_out(seg(t, 0.0, ZOOM_CLICK)));
    let world = (lerp(1240.0, peak.0, reach), lerp(780.0, peak.1, reach));
    let local = camera_point(peak.0, peak.1, z, world);
    let screen = |p: (f32, f32)| (cx0 + p.0 * k, cy0 + p.1 * k);
    let cur = screen(local);
    let click = screen(camera_point(peak.0, peak.1, z, peak));
    let scene = desktop("zoom", 0.0);
    fframes::svgr!(<g>
        <defs>
            <linearGradient id="zoom-bg" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#5B8CFF" /><stop offset="0.5" stop-color="#A78BFA" /><stop offset="1" stop-color="#F472B6" />
            </linearGradient>
            <clipPath id="zoom-clip"><rect x={cx0} y={cy0} width={cw} height={chh} rx="20" /></clipPath>
        </defs>
        <rect width="1920" height="1080" fill="url(#zoom-bg)" />
        <rect x={cx0 + 8.0} y={cy0 + 20.0} width={cw - 16.0} height={chh - 8.0} rx="20" fill="#000000" opacity="0.35" />
        <g clip-path="url(#zoom-clip)">
            <g transform={tf(cx0, cy0, k)}><g transform={camera_transform(peak.0, peak.1, z)}>{scene}</g></g>
        </g>
        {ripple(click.0, click.1, t, ZOOM_CLICK, ACCENT)}
        {cursor(cur.0, cur.1, 1.0)}
        {keycap("Ctrl", 1180.0, 880.0, 116.0, out_back(seg(t, ZOOM_KEYS, 0.26)))}
        {keycap("C", 1274.0, 880.0, 68.0, out_back(seg(t, ZOOM_KEYS + 0.07, 0.26)))}
    </g>)
}

const WORDS: [&str; 7] = ["Click", "the", "chart", "and", "it", "zooms", "in."];
const WORD_STEP: f32 = 0.072;

pub fn captions<'a>(f: &mut Frame, c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (px0, py0, pw, ph) = PREVIEW;
    let size = 40;
    let font = FontQuery {
        family: UI,
        size,
        weight: 600,
        ..Default::default()
    };
    let space = 13.0;
    let widths: Vec<f32> = WORDS
        .iter()
        .map(|w| f.text_width(c, font, w).unwrap_or(60) as f32)
        .collect();
    let total: f32 = widths.iter().sum::<f32>() + space * (WORDS.len() - 1) as f32;
    let x0 = px0 + pw / 2.0 - total / 2.0;
    let xs: Vec<f32> = widths
        .iter()
        .scan(x0, |x, w| {
            let here = *x;
            *x += w + space;
            Some(here)
        })
        .collect();
    let cap_y = py0 + ph - 40.0;
    let beat = ((t + 0.04) / WORD_STEP).max(0.0);
    let i0 = (beat as usize).min(WORDS.len() - 1);
    let i1 = (i0 + 1).min(WORDS.len() - 1);
    let glide = out_cubic(c01((beat - i0 as f32) * 2.5));
    let hx = lerp(xs[i0], xs[i1], if i0 == i1 { 0.0 } else { glide });
    let hw = lerp(widths[i0], widths[i1], if i0 == i1 { 0.0 } else { glide });
    let words: Vec<Svgr> = WORDS
        .iter()
        .enumerate()
        .map(|(i, w)| {
            let spoken = beat >= i as f32 + 0.3;
            fframes::svgr!(<text x={xs[i]} y={cap_y} font-family={UI} font-weight="600" font-size={size} fill="#FFFFFF" opacity={if spoken { 1.0 } else { 0.5 }}>{*w}</text>)
        })
        .collect();
    let caption = fframes::svgr!(<g>
        <rect x={x0 - 24.0} y={cap_y - 52.0} width={total + 48.0} height="74" rx="16" fill="#000000" fill-opacity="0.66" />
        <rect x={hx - 9.0} y={cap_y - 41.0} width={hw + 18.0} height="54" rx="10" fill={ACCENT} />
        {words}
    </g>);
    let peak = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
    let scene = desktop("cap", 0.0);
    let recorded =
        fframes::svgr!(<g transform={camera_transform(peak.0, peak.1, 1.75)}>{scene}</g>);
    let s = 1.3;
    let view = studio("cap-studio", lerp(0.4, 0.47, t / 0.5), recorded, caption);
    fframes::svgr!(<g transform={format!("translate({:.1} {:.1}) scale({s})", 1030.0 - 790.0 * s, 470.0 - 360.0 * s)}>{view}</g>)
}

const FILLER_CUT: f32 = 0.2;
pub const TRANSCRIPT_CUES: &[Cue] = &[(0.04, "click.wav", -8.0), (FILLER_CUT, "pop_hi.wav", -11.0)];
const TRANSCRIPT: [(&str, bool); 8] = [
    ("So", false),
    ("um", true),
    ("this", false),
    ("is", false),
    ("the", false),
    ("uh", true),
    ("revenue", false),
    ("chart.", false),
];

pub fn transcript<'a>(f: &mut Frame, c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let size = 56;
    let font = FontQuery {
        family: UI,
        size,
        weight: 600,
        ..Default::default()
    };
    let space = 18.0;
    let keep = 1.0 - in_out(seg(t, FILLER_CUT, 0.2));
    let widths: Vec<f32> = TRANSCRIPT
        .iter()
        .map(|(w, _)| f.text_width(c, font, w).unwrap_or(60) as f32)
        .collect();
    let total: f32 = TRANSCRIPT
        .iter()
        .zip(&widths)
        .map(|((_, filler), w)| (w + space) * if *filler { keep } else { 1.0 })
        .sum::<f32>()
        - space;
    let (bx, by, bw, bh) = (300.0, 300.0, 1320.0, 330.0);
    let mut x = 960.0 - total / 2.0;
    let y = by + 200.0;
    let words: Vec<Svgr> = TRANSCRIPT
        .iter()
        .zip(&widths)
        .enumerate()
        .map(|(i, ((w, filler), width))| {
            let here = x;
            let k = if *filler { keep } else { 1.0 };
            x += (width + space) * k;
            if *filler {
                let strike = out_cubic(seg(t, 0.02 + i as f32 * 0.012, 0.1));
                fframes::svgr!(<g transform={format!("translate({here:.1} 0) scale({:.4} 1)", px(k).min(1.0))} opacity={k}>
                    <rect x="-8" y={y - 46.0} width={width + 16.0} height="62" rx="10" fill={HOT} fill-opacity="0.22" />
                    <text y={y} font-family={UI} font-weight="600" font-size={size} fill={HOT}>{*w}</text>
                    <rect x="-4" y={y - 20.0} width={px((width + 8.0) * strike)} height="5" rx="2.5" fill={HOT} />
                </g>)
            } else {
                fframes::svgr!(<text x={here} y={y} font-family={UI} font-weight="600" font-size={size} fill={INK}>{*w}</text>)
            }
        })
        .collect();
    fframes::svgr!(<g>
        <rect width="1920" height="1080" fill="#141414" />
        <g transform={about(960.0, 480.0, 1.22)}>
        <rect x={bx + 10.0} y={by + 26.0} width={bw - 20.0} height={bh} rx="20" fill="#000000" opacity="0.5" />
        <rect x={bx} y={by} width={bw} height={bh} rx="20" fill="#1C1C1C" stroke="#FFFFFF" stroke-opacity="0.1" />
        <text x={bx + 48.0} y={by + 72.0} font-family={MONO} font-size="28" fill={MUTED}>"0:04"</text>
        <circle cx={bx + 150.0} cy={by + 62.0} r="6" fill={ACCENT} />
        {words}
        <rect x={bx + 48.0} y={by + 260.0} width={bw - 96.0} height="2" fill="#FFFFFF" fill-opacity="0.08" />
        </g>
    </g>)
}

const EXPORT_DONE: f32 = 0.4;
pub const EXPORT_CUES: &[Cue] = &[(EXPORT_DONE, "pop.wav", -7.0)];

pub fn export<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (dx, dy, dw, dh) = (500.0, 330.0, 920.0, 300.0);
    let p = in_out(seg(t, 0.02, EXPORT_DONE - 0.02));
    let done = t >= EXPORT_DONE;
    let check = out_back(seg(t, EXPORT_DONE, 0.24));
    let chip = |label: &'a str, x: f32, w: f32, at: f32| -> Svgr<'a> {
        let s = out_back(seg(t, at, 0.24));
        fframes::svgr!(<g transform={format!("translate({x:.1} {:.1}) scale({s:.3})", dy + 98.0)} opacity={c01(s)}>
            <rect y="-26" width={w} height="52" rx="26" fill={ACCENT} fill-opacity="0.2" stroke={ACCENT} stroke-width="2" />
            <text x={w / 2.0} y="10" text-anchor="middle" font-family={UI} font-weight="700" font-size="28" fill={INK}>{label}</text>
        </g>)
    };
    let (bx, by, bw) = (dx + 420.0, dy + 176.0, 440.0);
    let percent = format!("{}%", (p * 100.0).round() as i32);
    let thumb = framed(
        "exp-thumb",
        0.0,
        0.0,
        300.0,
        168.75,
        10.0,
        1920.0,
        desktop("exp", 0.0),
    );
    let bar_color = if done { "#22C55E" } else { ACCENT };
    fframes::svgr!(<g>
        <rect width="1920" height="1080" fill="#141414" />
        <defs>
            <linearGradient id="exp-bg" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#5B8CFF" /><stop offset="0.5" stop-color="#A78BFA" /><stop offset="1" stop-color="#F472B6" />
            </linearGradient>
            <radialGradient id="exp-glow" cx="0.5" cy="0.5" r="0.5">
                <stop offset="0" stop-color={ACCENT} stop-opacity="0.22" />
                <stop offset="1" stop-color={ACCENT} stop-opacity="0" />
            </radialGradient>
        </defs>
        <circle cx="960" cy="480" r="700" fill="url(#exp-glow)" />
        <g transform={about(960.0, 480.0, 1.3)}>
        <rect x={dx + 12.0} y={dy + 30.0} width={dw - 24.0} height={dh} rx="20" fill="#000000" opacity="0.55" />
        <rect x={dx} y={dy} width={dw} height={dh} rx="20" fill="#202020" stroke="#FFFFFF" stroke-opacity="0.12" />
        <rect x={dx + 40.0} y={dy + 40.0} width="340" height="220" rx="14" fill="url(#exp-bg)" />
        <g transform={format!("translate({} {})", dx + 60.0, dy + 66.0)}>{thumb}</g>
        {chip("4K", dx + 420.0, 90.0, 0.0)}
        {chip("60 fps", dx + 526.0, 132.0, 0.05)}
        {chip("MP4", dx + 674.0, 104.0, 0.1)}
        <rect x={bx} y={by - 6.0} width={bw} height="12" rx="6" fill="#FFFFFF" fill-opacity="0.12" />
        <rect x={bx} y={by - 6.0} width={px(bw * p)} height="12" rx="6" fill={bar_color} />
        <text x={bx + bw} y={by + 62.0} text-anchor="end" font-family={MONO} font-size="32" fill={INK}>{percent}</text>
        <g transform={format!("translate({} {}) scale({check:.3})", bx + 18.0, by + 52.0)} opacity={c01(check)}>
            <circle r="18" fill="#22C55E" />
            <path d="M-8 0 l5 5 l10 -10" stroke="#FFFFFF" stroke-width="3" fill="none" stroke-linecap="round" stroke-linejoin="round" />
        </g>
        </g>
    </g>)
}

// ---------------------------------------------------------------- library

const QUERY: &str = "chart";
const TYPE_AT: f32 = 0.3;
const TYPE_STEP: f32 = 0.06;
const FILTER_AT: f32 = 0.6;
const RUSH_AT: f32 = 0.75;
pub const LIBRARY_CUES: &[Cue] = &[
    (0.1, "riser.wav", -10.0),
    (TYPE_AT, "click.wav", -15.0),
    (TYPE_AT + TYPE_STEP, "click.wav", -15.0),
    (TYPE_AT + 2.0 * TYPE_STEP, "click.wav", -15.0),
    (TYPE_AT + 3.0 * TYPE_STEP, "click.wav", -15.0),
    (TYPE_AT + 4.0 * TYPE_STEP, "click.wav", -15.0),
    (RUSH_AT - 0.1, "whoosh.wav", -8.0),
];

#[derive(Clone, Copy, PartialEq)]
enum Tile {
    Chart,
    Photo,
    Code,
    Recording,
}

const TILES: [Tile; 12] = [
    Tile::Chart,
    Tile::Photo,
    Tile::Code,
    Tile::Recording,
    Tile::Code,
    Tile::Recording,
    Tile::Photo,
    Tile::Chart,
    Tile::Photo,
    Tile::Code,
    Tile::Chart,
    Tile::Photo,
];

pub fn library<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (tw, th) = (320.0, 184.0);
    let filter = out_cubic(seg(t, FILTER_AT, 0.14));
    let tiles: Vec<Svgr> = TILES
        .iter()
        .enumerate()
        .map(|(i, kind)| {
            let (col, row) = (i % 4, i / 4);
            let (x, y) = (400.0 + col as f32 * 344.0, 176.0 + row as f32 * 212.0);
            let p = out_expo(seg(t, (col + row) as f32 * 0.035 - 0.08, 0.4));
            let id = format!("lib{i}");
            let content = match kind {
                Tile::Chart | Tile::Recording => shot(&id, 0.0),
                Tile::Photo => thumb_photo(id.clone()),
                Tile::Code => thumb_code(id.clone()),
            };
            let matched = matches!(kind, Tile::Chart | Tile::Recording);
            let s = lerp(0.8, 1.0, p) * if matched { 1.0 } else { lerp(1.0, 0.94, filter) };
            let o = c01(p * 2.0) * if matched { 1.0 } else { lerp(1.0, 0.22, filter) };
            let badge = if *kind == Tile::Recording {
                fframes::svgr!(<g>
                    <rect x={x + tw - 82.0} y={y + th - 42.0} width="68" height="30" rx="15" fill="#000000" fill-opacity="0.7" />
                    <text x={x + tw - 48.0} y={y + th - 20.0} text-anchor="middle" font-family={MONO} font-size="18" fill="#FFFFFF">"0:12"</text>
                    <circle cx={x + tw / 2.0} cy={y + th / 2.0} r="26" fill="#000000" fill-opacity="0.55" />
                    <path d={format!("M{} {} l18 10 l-18 10 Z", x + tw / 2.0 - 7.0, y + th / 2.0 - 10.0)} fill="#FFFFFF" />
                </g>)
            } else {
                Svgr::empty()
            };
            let ring = if matched && filter > 0.0 {
                fframes::svgr!(<rect x={x - 4.0} y={y - 4.0} width={tw + 8.0} height={th + 8.0} rx="14" fill="none" stroke={ACCENT} stroke-width="3" opacity={filter} />)
            } else {
                Svgr::empty()
            };
            let image = framed(&format!("{id}-f"), x, y, tw, th, 10.0, SHOT_W, content);
            fframes::svgr!(<g transform={format!("translate(0 {:.1}) {}", 60.0 * (1.0 - p), about(x + tw / 2.0, y + th / 2.0, s))} opacity={o}>
                <rect x={x + 4.0} y={y + 12.0} width={tw - 8.0} height={th - 4.0} rx="10" fill="#000000" opacity="0.4" />
                {image}
                {badge}
                {ring}
            </g>)
        })
        .collect();
    let typed = ((t - TYPE_AT) / TYPE_STEP + 1.0).clamp(0.0, QUERY.len() as f32) as usize;
    let caret = if (t * 6.0) as i32 % 2 == 0 || typed < QUERY.len() {
        1.0
    } else {
        0.0
    };
    let query_w = typed as f32 * 15.5;
    let rush = in_cubic(seg(t, RUSH_AT, 0.25));
    let side = |label: &'a str, y: f32, on: bool| -> Svgr<'a> {
        let bg = if on {
            fframes::svgr!(<rect x="20" y={y - 34.0} width="300" height="50" rx="8" fill="#FFFFFF" fill-opacity="0.08" />)
        } else {
            Svgr::empty()
        };
        fframes::svgr!(<g>{bg}<text x="44" y={y} font-family={UI} font-size="28" fill={if on { INK } else { MUTED }}>{label}</text></g>)
    };
    fframes::svgr!(<g>
        <defs>
            <radialGradient id="lib-glow" cx="0.5" cy="0.5" r="0.5">
                <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.9" />
                <stop offset="0.3" stop-color={ACCENT} stop-opacity="0.5" />
                <stop offset="1" stop-color={VIOLET} stop-opacity="0" />
            </radialGradient>
        </defs>
        <g transform={about(960.0, 540.0, lerp(1.0, 1.6, rush))} opacity={1.0 - rush}>
            <rect width="1920" height="1080" fill="#161616" />
            <rect width="1920" height="44" fill="#202020" />
            <path d={star_path(9.0)} transform="translate(26 22)" fill={ACCENT} />
            <text x="46" y="29" font-family={UI} font-size="15" fill={MUTED}>"Glint Library"</text>
            <rect y="44" width="340" height="1036" fill="#1C1C1C" />
            {side("All", 150.0, true)}
            {side("Screenshots", 210.0, false)}
            {side("Recordings", 270.0, false)}
            <rect x="400" y="72" width="560" height="60" rx="30" fill="#262626" stroke={ACCENT} stroke-opacity={seg(t, TYPE_AT - 0.06, 0.06)} stroke-width="2" />
            <circle cx="438" cy="100" r="10" fill="none" stroke={MUTED} stroke-width="2.5" />
            <path d="M445 107 L452 114" stroke={MUTED} stroke-width="2.5" stroke-linecap="round" />
            <text x="470" y="112" font-family={UI} font-size="28" fill={INK}>{&QUERY[..typed]}</text>
            <rect x={472.0 + query_w} y="84" width="2" height="34" fill={INK} opacity={caret * seg(t, TYPE_AT - 0.06, 0.04)} />
            {tiles}
        </g>
        <rect width="1920" height="1080" fill={BG} opacity={rush * 0.6} />
        <circle cx="960" cy="520" r={px(700.0 * rush)} fill="url(#lib-glow)" opacity={rush} />
    </g>)
}
