//! The tour's vignettes, one per row of `SHOTS`. Each draws from its local time `t` in
//! seconds and plays its interaction start to finish: the cursor moves, clicks, and the result
//! holds. Event times are local constants shared by the drawing and the shot's cues, so a
//! vignette keeps its sound in sync when the table moves it. Shots joined by a match cut share
//! a camera constant, so the incoming shot starts on the outgoing shot's last frame.
use crate::kit::*;
use crate::motion::*;
use crate::{ACCENT, BG, Cue, HOT, INK, MONO, MUTED, REC, UI, VIOLET};
use fframes::{FFramesContext, FontQuery, Frame, Svgr};

type Ctx<'a, 'b> = FFramesContext<'a, 'b>;

fn dim<'a>(o: f32) -> Svgr<'a> {
    fframes::svgr!(<rect width="1920" height="1080" fill="#05060A" opacity={o} />)
}

fn view<'a>(cam: Cam, body: Svgr<'a>) -> Svgr<'a> {
    fframes::svgr!(<g transform={cam.transform()}>{body}</g>)
}

/// The cursor gliding through `(time, point)` keys, eased between them.
fn glide(t: f32, keys: &[(f32, (f32, f32))]) -> (f32, f32) {
    let mut p = keys[0].1;
    for w in keys.windows(2) {
        let ((a, from), (b, to)) = (w[0], w[1]);
        if t > a {
            let k = in_out(seg(t, a, b - a));
            p = (lerp(from.0, to.0, k), lerp(from.1, to.1, k));
        }
    }
    p
}

/// A marching-ants frame around `r`, with everything outside it dimmed.
fn guide<'a>(r: (f32, f32, f32, f32), t: f32, dim_o: f32, o: f32) -> Svgr<'a> {
    let (x, y, w, h) = r;
    fframes::svgr!(<g opacity={o}>
        <path d={format!("M-100 -100 H2020 V1180 H-100 Z M{x} {y} V{} H{} V{y} Z", y + h, x + w)} fill="#05060A" fill-opacity={dim_o} fill-rule="evenodd" />
        <rect x={x} y={y} width={px(w)} height={px(h)} fill="none" stroke="#FFFFFF" stroke-width="2.5" stroke-dasharray="12 9" stroke-dashoffset={format!("{:.1}", -t * 60.0)} />
    </g>)
}

// ---------------------------------------------------------------- capture

const AREA_MODE: f32 = 0.15;
const AREA_PRESS: f32 = 0.55;
const AREA_SNAP: f32 = 1.25;
pub const AREA_CUES: &[Cue] = &[
    (AREA_PRESS, "click.wav", -14.0),
    (AREA_SNAP, "snap.wav", -9.0),
];

pub fn area<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (ax, ay) = (SHOT_X, SHOT_Y);
    let mode = seg(t, AREA_MODE, 0.2);
    let drag = in_out(seg(t, AREA_PRESS, AREA_SNAP - AREA_PRESS - 0.05));
    let dragging = t >= AREA_PRESS;
    let (cx, cy) = if dragging {
        (lerp(ax, ax + SHOT_W, drag), lerp(ay, ay + SHOT_H, drag))
    } else {
        glide(t, &[(0.0, (1420.0, 880.0)), (AREA_PRESS - 0.05, (ax, ay))])
    };
    let (sw, sh) = (px(cx - ax), px(cy - ay));
    let snapped = t >= AREA_SNAP;
    let tool = mode * (1.0 - seg(t, AREA_SNAP, 0.1));
    let dim_o = 0.45 * mode + 0.2 * out_cubic(seg(t, AREA_SNAP, 0.3));
    let overlay = if dragging {
        format!("M0 0 H1920 V1080 H0 Z M{ax} {ay} h{sw} v{sh} h{} Z", -sw)
    } else {
        "M0 0 H1920 V1080 H0 Z".to_string()
    };

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
    let size_o = tool * seg(t, AREA_PRESS, 0.08);
    let handles: Vec<Svgr> = [(ax, ay), (ax + sw, ay), (ax, ay + sh), (ax + sw, ay + sh), (ax + sw / 2.0, ay), (ax + sw / 2.0, ay + sh), (ax, ay + sh / 2.0), (ax + sw, ay + sh / 2.0)]
        .iter()
        .enumerate()
        .map(|(i, (x, y))| {
            let s = out_back(seg(t, AREA_SNAP + i as f32 * 0.03, 0.3)) * 7.0;
            if s <= 0.0 {
                return Svgr::empty();
            }
            fframes::svgr!(<rect x={x - s} y={y - s} width={px(2.0 * s)} height={px(2.0 * s)} rx="2" fill="#FFFFFF" stroke={ACCENT} stroke-width="2" />)
        })
        .collect();
    let selection = if dragging {
        fframes::svgr!(<rect x={ax} y={ay} width={sw} height={sh} fill="none" stroke={if snapped { ACCENT } else { "#FFFFFF" }} stroke-width={if snapped { 3.0 } else { 1.5 }} />)
    } else {
        Svgr::empty()
    };
    let scene = desktop("area", 0.0);
    let cam = cam_path(
        t,
        &[(0.0, CAM0), (AREA_SNAP, CAM0), (2.0, Cam::at(960.0, 560.0, 1.05))],
    );
    let body = fframes::svgr!(<g>
        {scene}
        <path d={overlay} fill="#05060A" fill-opacity={dim_o} fill-rule="evenodd" />
        {selection}
        {handles}
        <g opacity={tool} stroke-linecap="round">
            <path d={format!("M{cx} 0 V1080 M0 {cy} H1920")} stroke="#FFFFFF" stroke-opacity="0.18" stroke-width="1" />
            <path d={cross.clone()} stroke="#000000" stroke-width="5" />
            <path d={cross} stroke="#FFFFFF" stroke-width="2.5" />
        </g>
        <g opacity={size_o} transform={format!("translate({:.1} {:.1})", cx - 250.0, cy + 18.0)}>
            <rect width="226" height="48" rx="24" fill="#000000" fill-opacity="0.72" />
            <text x="113" y="34" text-anchor="middle" font-family={MONO} font-size="28" fill="#FFFFFF">{size}</text>
        </g>
        {loupe}
        {cursor(cx, cy, 1.0 - mode)}
    </g>);
    fframes::svgr!(<g>{view(cam, body)}{flash(t, AREA_SNAP)}</g>)
}

const WIN_CLICK: f32 = 0.7;
const FULL_SNAP: f32 = 1.3;
pub const WINDOW_CUES: &[Cue] = &[
    (WIN_CLICK, "click.wav", -10.0),
    (WIN_CLICK + 0.02, "snap.wav", -11.0),
    (FULL_SNAP, "snap.wav", -11.0),
];
const NOTES: (f32, f32, f32, f32) = (96.0, 150.0, 270.0, 300.0);

pub fn window_or_full<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    // Window mode: the highlight follows the cursor from the notes window to the app.
    let hop = in_out(seg(t, 0.3, 0.32));
    let (nx, ny, nw, nh) = NOTES;
    let (wx, wy, ww, wh) = WIN;
    let (hx, hy, hw, hh) = (
        lerp(nx, wx, hop),
        lerp(ny, wy, hop),
        lerp(nw, ww, hop),
        lerp(nh, wh, hop),
    );
    let (cx, cy) = glide(
        t,
        &[(0.0, (300.0, 360.0)), (0.3, (300.0, 360.0)), (0.64, (900.0, 560.0))],
    );
    let lift = out_expo(seg(t, WIN_CLICK + 0.02, 0.3)) * (1.0 - in_out(seg(t, 1.0, 0.2)));
    let s = lerp(1.0, 0.9, lift);
    let bars: Vec<Svgr> = (0..6)
        .map(|i| {
            let w = 120.0 + 100.0 * hash(i + 3);
            fframes::svgr!(<rect x={nx + 24.0} y={ny + 70.0 + i as f32 * 34.0} width={w} height="10" rx="5" fill="#FFFFFF" fill-opacity="0.22" />)
        })
        .collect();
    let highlight_o = seg(t, -0.1, 0.2) * (1.0 - seg(t, WIN_CLICK, 0.08));
    let wall = wallpaper("win-wall");
    let bar = taskbar("win-bar");
    let app = app_window("win", 0.0);
    let scene = fframes::svgr!(<g>
        <defs><filter id="lift-shadow" x="-20%" y="-20%" width="140%" height="160%"><feGaussianBlur stdDeviation="34" /></filter></defs>
        {wall}
        <rect x={nx} y={ny} width={nw} height={nh} rx="10" fill="#1F2026" stroke="#FFFFFF" stroke-opacity="0.1" />
        <rect x={nx} y={ny} width={nw} height="40" rx="10" fill="#26272E" />
        <rect x={nx} y={ny + 30.0} width={nw} height="10" fill="#26272E" />
        <circle cx={nx + 24.0} cy={ny + 20.0} r="6" fill={crate::AMBER} />
        {bars}
        {bar}
        {dim(0.75 * lift)}
        <g transform={about(960.0, 550.0, s)}>
            <rect x="420" y="250" width="1080" height="680" rx="14" fill="#000000" opacity={0.7 * lift} filter="url(#lift-shadow)" />
            {app}
        </g>
        <rect x={hx} y={hy} width={hw} height={hh} rx="10" fill={ACCENT} fill-opacity="0.16" stroke={ACCENT} stroke-width="4" opacity={highlight_o} />
        {ripple(cx, cy, t, WIN_CLICK, ACCENT)}
        {cursor(cx, cy, 1.0 - seg(t, WIN_CLICK + 0.1, 0.12))}
    </g>);

    // Full screen: the whole desktop is outlined, then shrinks into the captured card.
    let edge = seg(t, 1.1, 0.12) * (1.0 - seg(t, FULL_SNAP, 0.12));
    let p = out_expo(seg(t, FULL_SNAP, 0.5));
    let sc = lerp(1.0, 0.6, p);
    let (fx, fy) = (lerp(960.0, 1040.0, p), lerp(540.0, 440.0, p));
    let r = 28.0 * p / sc;
    fframes::svgr!(<g>
        <defs>
            <clipPath id="full-clip"><rect width="1920" height="1080" rx={r} /></clipPath>
            <filter id="full-shadow" x="-20%" y="-20%" width="140%" height="160%"><feGaussianBlur stdDeviation="40" /></filter>
            <radialGradient id="full-glow" cx="0.5" cy="0.5" r="0.5">
                <stop offset="0" stop-color={ACCENT} stop-opacity="0.28" />
                <stop offset="1" stop-color={ACCENT} stop-opacity="0" />
            </radialGradient>
        </defs>
        <rect width="1920" height="1080" fill={BG} />
        <circle cx="1040" cy="440" r="900" fill="url(#full-glow)" opacity={p} />
        <g transform={format!("translate({:.2} {:.2}) scale({sc:.4})", fx - 960.0 * sc, fy - 540.0 * sc)}>
            <rect x="40" y="90" width="1840" height="1020" rx={r} fill="#000000" opacity={0.75 * p} filter="url(#full-shadow)" />
            <g clip-path="url(#full-clip)">{scene}</g>
            <rect width="1920" height="1080" rx={r} fill="none" stroke="#FFFFFF" stroke-opacity={0.25 * p} stroke-width={2.0 / sc} />
        </g>
        <rect x="5" y="5" width="1910" height="1070" rx="8" fill={ACCENT} fill-opacity={0.08 * edge} stroke={ACCENT} stroke-width="10" opacity={edge} />
        {flash(t, FULL_SNAP)}
    </g>)
}

const TICK_AT: [f32; 3] = [0.1, 0.45, 0.8];
const TIMER_FIRE: f32 = 1.15;
pub const TIMER_CUES: &[Cue] = &[
    (TICK_AT[1], "tick.wav", -12.0),
    (TICK_AT[2], "tick.wav", -12.0),
    (TIMER_FIRE, "snap.wav", -10.0),
];

pub fn self_timer<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (cx, cy) = (960.0, 480.0);
    let r = 92.0;
    let circ = std::f32::consts::TAU * r;
    let progress = seg(t, TICK_AT[0], TIMER_FIRE - TICK_AT[0]);
    let n = TICK_AT.iter().rposition(|&a| t >= a).unwrap_or(0);
    let local = t - TICK_AT[n];
    let digit = |d: usize, y: f32| -> Svgr<'a> {
        fframes::svgr!(<text x={cx} y={y} text-anchor="middle" font-family={UI} font-weight="600" font-size="128" fill={INK}>{(3 - d).to_string()}</text>)
    };
    let rise = out_expo(seg(local, 0.0, 0.2));
    let base = cy + 46.0;
    let incoming = digit(n, base + 120.0 * (1.0 - rise));
    let outgoing = if n > 0 {
        digit(n - 1, base - 120.0 * in_cubic(seg(local, 0.0, 0.12)))
    } else {
        Svgr::empty()
    };
    let chip_in = out_back(seg(t, -0.15, 0.35));
    let fire = in_cubic(seg(t, TIMER_FIRE - 0.04, 0.14));
    let chip_s = lerp(0.9, 1.0, chip_in) * lerp(1.0, 0.85, fire);
    let chip_o = c01(chip_in) * (1.0 - fire);
    let captured = spring(t - TIMER_FIRE - 0.08, 200.0, 22.0);
    let landed = card(
        "timer-card",
        lerp(1980.0, STACK_X, captured),
        STACK_Y[0],
        STACK_W,
        shot("timer-card-c", 0.0),
        1.0,
    );
    let scene = desktop("timer", 0.0);
    let cam = cam_path(t, &[(0.0, CAM0), (1.5, Cam::at(960.0, 520.0, 1.03))]);
    let body = fframes::svgr!(<g>
        {scene}
        {dim(0.5 * (1.0 - seg(t, TIMER_FIRE, 0.25)))}
        <g transform={about(cx, cy, chip_s)} opacity={chip_o}>
            <defs><clipPath id="digit-mask"><circle cx={cx} cy={cy} r={r - 10.0} /></clipPath></defs>
            <rect x={cx - 140.0} y={cy - 124.0} width="280" height="280" rx="60" fill="#000000" opacity="0.45" />
            <rect x={cx - 140.0} y={cy - 140.0} width="280" height="280" rx="60" fill="#1E1E22" fill-opacity="0.96" stroke="#FFFFFF" stroke-opacity="0.12" />
            <circle cx={cx} cy={cy} r={r} fill="none" stroke="#FFFFFF" stroke-opacity="0.1" stroke-width="10" />
            <circle cx={cx} cy={cy} r={r} fill="none" stroke={ACCENT} stroke-width="10" stroke-linecap="round" transform={format!("rotate(-90 {cx} {cy})")}
                stroke-dasharray={format!("{circ:.1} {circ:.1}")} stroke-dashoffset={circ * progress} />
            <g clip-path="url(#digit-mask)">{outgoing}{incoming}</g>
        </g>
        {landed}
    </g>);
    fframes::svgr!(<g>{view(cam, body)}{flash(t, TIMER_FIRE)}</g>)
}

const TEXT_PRESS: f32 = 0.2;
const TEXT_RELEASE: f32 = 0.75;
const TEXT_LIFT: f32 = 1.2;
const TEXT_TOAST: f32 = 1.45;
pub const TEXT_CUES: &[Cue] = &[
    (TEXT_LIFT, "whoosh_short.wav", -17.0),
    (TEXT_TOAST, "pop.wav", -11.0),
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
    let (sx, sy, sw, sh) = (476.0, 248.0, 760.0, 306.0);
    let drag = in_out(seg(t, TEXT_PRESS, TEXT_RELEASE - TEXT_PRESS - 0.05));
    let sel = (sx, sy, sw * drag, sh * drag);
    let (cx, cy) = if t < TEXT_PRESS {
        glide(t, &[(-0.2, (1300.0, 760.0)), (TEXT_PRESS - 0.02, (sx, sy))])
    } else {
        (sx + sel.2, sy + sel.3)
    };
    let scan = in_out(seg(t, TEXT_RELEASE, 0.4));
    let scan_y = sy + sh * scan;
    let scan_o = seg(t, TEXT_RELEASE, 0.05) * (1.0 - seg(t, TEXT_RELEASE + 0.4, 0.08));
    let size = |w: usize| if w == 700 { 54 } else { 40 };
    let lines: Vec<Svgr> = DOC_LINES
        .iter()
        .map(|&(text, y, weight)| {
            let hit = seg(scan_y, y - 30.0, 30.0) * (1.0 - seg(t, TEXT_LIFT + 0.4, 0.3));
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
            let p = in_out(seg(t, TEXT_LIFT + j as f32 * 0.06, 0.32));
            if p <= 0.0 || p >= 1.0 {
                return Svgr::empty();
            }
            let (x, yy) = (lerp(500.0, 860.0, p), lerp(y, 128.0, p) - 90.0 * (std::f32::consts::PI * p).sin());
            fframes::svgr!(<text transform={tf(x, yy, lerp(1.0, 0.3, p))} font-family={UI} font-weight={weight} font-size={size(weight)} fill={ACCENT} opacity={1.0 - p * p}>{text}</text>)
        })
        .collect();
    let wall = wallpaper("text-wall");
    let guide_o = seg(t, TEXT_PRESS, 0.04) * (1.0 - seg(t, TEXT_LIFT, 0.25));
    let cam = cam_path(
        t,
        &[(0.0, CAM0), (0.8, CAM0), (2.0, Cam::at(960.0, 430.0, 1.05))],
    );
    let body = fframes::svgr!(<g>
        {wall}
        <defs>
            <linearGradient id="scan-glow" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0" stop-color={ACCENT} stop-opacity="0" />
                <stop offset="1" stop-color={ACCENT} stop-opacity="0.35" />
            </linearGradient>
            <clipPath id="scan-clip"><rect x={sx} y={sy} width={sw} height={sh} /></clipPath>
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
        {guide(sel, t, 0.4, guide_o)}
        <g clip-path="url(#scan-clip)" opacity={scan_o}>
            <rect x={sx} y={scan_y - 90.0} width={sw} height="90" fill="url(#scan-glow)" />
            <rect x={sx} y={scan_y - 2.0} width={sw} height="4" fill={ACCENT} />
        </g>
        {lifted}
        {cursor(cx, cy, 1.0 - seg(t, TEXT_RELEASE, 0.12))}
        {toast(960.0, 128.0, "Text copied", 260.0, seg(t, TEXT_TOAST - 0.04, 0.4))}
    </g>);
    view(cam, body)
}

// ---------------------------------------------------------------- quick access

const STACK_X: f32 = 1326.0;
const STACK_W: f32 = 440.0;
const STACK_Y: [f32; 3] = [690.0, 424.0, 158.0];
/// Where Preview cards ends and Drag and drop begins: pushed in on the stack.
const CARDS_CAM: Cam = Cam::at(1300.0, 600.0, 1.08);

fn stack_content<'a>(slot: usize, id: &str) -> Svgr<'a> {
    match slot {
        0 => shot(id, 0.0),
        1 => thumb_photo(id.to_string()),
        _ => thumb_code(id.to_string()),
    }
}

const CARDS_HOVER: f32 = 0.95;
const CARDS_COPY: f32 = 1.35;
pub const CARDS_CUES: &[Cue] = &[
    (0.06, "pop.wav", -17.0),
    (0.22, "pop.wav", -18.0),
    (CARDS_COPY, "click.wav", -9.0),
    (CARDS_COPY + 0.04, "pop_hi.wav", -14.0),
];

pub fn preview_cards<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let ch = STACK_W * SHOT_H / SHOT_W;
    let cards: Vec<Svgr> = (0..3)
        .rev()
        .map(|j| {
            let p = spring(t - (2 - j) as f32 * 0.08, 200.0, 22.0);
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
    let (cx, cy) = glide(
        t,
        &[
            (0.0, (900.0, 760.0)),
            (0.4, (900.0, 760.0)),
            (0.95, (mid + 40.0, ky + 190.0)),
            (1.05, (mid + 40.0, ky + 190.0)),
            (1.3, (mid + 10.0, copy_y + 4.0)),
        ],
    );
    let hover = seg(t, CARDS_HOVER, 0.12);
    let copied = t >= CARDS_COPY;
    let pill = |label: &'a str, y: f32, at: f32, pressed: bool| -> Svgr<'a> {
        let s = out_back(seg(t, at, 0.28));
        fframes::svgr!(<g transform={format!("translate({mid:.1} {y:.1}) scale({s:.3})")} opacity={c01(s)}>
            <rect x="-84" y="-25" width="168" height="50" rx="25" fill={if pressed { ACCENT } else { "#FFFFFF" }} fill-opacity="0.95" />
            <text y="10" text-anchor="middle" font-family={UI} font-weight="600" font-size="28" fill={if pressed { "#FFFFFF" } else { "#111827" }}>{label}</text>
        </g>)
    };
    let corner = |x: f32, y: f32, glyph: &'a str, at: f32| -> Svgr<'a> {
        let s = out_back(seg(t, at, 0.26));
        fframes::svgr!(<g transform={format!("translate({x:.1} {y:.1}) scale({s:.3})")} opacity={c01(s)}>
            <circle r="18" fill="#111827" fill-opacity="0.85" />
            <path d={glyph} stroke="#FFFFFF" stroke-width="2.2" fill="none" stroke-linecap="round" stroke-linejoin="round" />
        </g>)
    };
    let hover_ui = if hover > 0.0 {
        fframes::svgr!(<g>
            <rect x={kx} y={ky} width={STACK_W} height={ch} rx="14" fill="#000000" opacity={0.45 * hover} />
            {pill("Copy", copy_y, CARDS_HOVER + 0.02, copied)}
            {pill("Save", save_y, CARDS_HOVER + 0.07, false)}
            {corner(kx + 26.0, ky + 26.0, "M-5 -5 L5 5 M5 -5 L-5 5", CARDS_HOVER + 0.05)}
            {corner(kx + STACK_W - 26.0, ky + 26.0, PIN_GLYPH, CARDS_HOVER + 0.09)}
            {corner(kx + 26.0, ky + ch - 26.0, "M0 -7 V3 M-5 3 H5 M0 3 V8", CARDS_HOVER + 0.13)}
            {corner(kx + STACK_W - 26.0, ky + ch - 26.0, "M-6 0 h0.1 M0 0 h0.1 M6 0 h0.1", CARDS_HOVER + 0.17)}
        </g>)
    } else {
        Svgr::empty()
    };
    let scene = desktop("cards", 0.0);
    let cam = cam_path(t, &[(0.0, CAM0), (0.6, CAM0), (1.7, CARDS_CAM)]);
    let body = fframes::svgr!(<g>
        {scene}
        {dim(0.3)}
        {cards}
        {hover_ui}
        {ripple(mid, copy_y, t, CARDS_COPY, ACCENT)}
        {cursor(cx, cy, 1.0)}
        {toast(1060.0, 150.0, "Copied to clipboard", 360.0, seg(t, CARDS_COPY + 0.02, 0.4))}
    </g>);
    view(cam, body)
}

const PIN_GLYPH: &str = "M-6 6 L-6 2 L3 -7 L7 -3 L-2 6 Z";

const GRAB: f32 = 0.15;
const DROP: f32 = 0.95;
pub const DRAG_CUES: &[Cue] = &[(GRAB, "click.wav", -13.0), (DROP, "pop.wav", -10.0)];
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
    let card_h = STACK_W * SHOT_H / SHOT_W;
    let target = folder_slot(5);
    let grab_at = (STACK_X + STACK_W / 2.0, STACK_Y[0] + card_h / 2.0);
    let drop_at = (target.0 + tw / 2.0, target.1 + th / 2.0);
    let (cx, cy) = glide(t, &[(0.0, grab_at), (GRAB + 0.05, grab_at), (DROP - 0.05, drop_at)]);
    let lift = out_cubic(seg(t, GRAB, 0.2));
    let land = out_expo(seg(t, DROP, 0.2));
    let over = seg(t, 0.55, 0.1) * (1.0 - seg(t, DROP + 0.04, 0.12));
    let held_w = lerp(STACK_W, 300.0, lift);
    let dw = lerp(held_w, tw, land);
    let held = (cx - held_w / 2.0, cy - held_w * SHOT_H / SHOT_W / 2.0);
    let (gx, gy) = (lerp(held.0, target.0, land), lerp(held.1, target.1, land));
    let tilt = 5.0 * lift * (1.0 - land);
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
    let cam = cam_path(t, &[(0.0, CARDS_CAM), (0.15, CARDS_CAM), (0.75, CAM0)]);
    let body = fframes::svgr!(<g>
        {scene}
        {dim(0.3)}
        {older}
        <rect x={fx + 10.0} y={fy + 26.0} width={fw - 20.0} height={fh} rx="12" fill="#000000" opacity="0.5" />
        <rect x={fx} y={fy} width={fw} height={fh} rx="12" fill="#202020" stroke={ACCENT} stroke-opacity={over} stroke-width="3" />
        <rect x={fx} y={fy} width={fw} height="44" rx="12" fill="#2B2B2B" />
        <rect x={fx} y={fy + 30.0} width={fw} height="14" fill="#2B2B2B" />
        <path d={format!("M{} {} h14 l4 4 h12 v14 h-30 Z", fx + 20.0, fy + 14.0)} fill="#F2B530" />
        <text x={fx + 60.0} y={fy + 30.0} font-family={UI} font-size="18" fill={INK}>"Screenshots"</text>
        <rect x={fx + 20.0} y={fy + 76.0} width="150" height="10" rx="5" fill="#FFFFFF" fill-opacity="0.18" />
        <rect x={fx + 20.0} y={fy + 112.0} width="120" height="10" rx="5" fill="#FFFFFF" fill-opacity="0.12" />
        <rect x={fx + 20.0} y={fy + 148.0} width="132" height="10" rx="5" fill="#FFFFFF" fill-opacity="0.12" />
        {tiles}
        <rect x={target.0} y={target.1} width={tw} height={th} rx="8" fill={ACCENT} fill-opacity={0.18 * over} stroke={ACCENT} stroke-opacity={over} stroke-width="2" stroke-dasharray="8 6" />
        <g transform={format!("translate({gx:.1} {gy:.1}) rotate({tilt:.2})")}>{ghost}</g>
        <g transform={format!("translate({:.1} {:.1}) scale({:.3})", cx + 34.0, cy + 40.0, out_back(seg(t, 0.55, 0.22)) * (1.0 - land))}>
            <circle r="16" fill={ACCENT} />
            <path d="M-7 0 H7 M0 -7 V7" stroke="#FFFFFF" stroke-width="2.6" stroke-linecap="round" />
        </g>
        {cursor(cx, cy, 1.0)}
    </g>);
    view(cam, body)
}

const PIN_CLICK: f32 = 0.35;
const PIN_BADGE: f32 = 0.85;
pub const PIN_CUES: &[Cue] = &[(PIN_CLICK, "click.wav", -11.0), (PIN_BADGE, "pop_hi.wav", -13.0)];

pub fn pin<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (ex, ey, ew, eh) = (150.0, 70.0, 1620.0, 760.0);
    let scroll = 140.0 + (t + 0.4) * 160.0;
    let colors = [
        "#C678DD", "#61AFEF", "#98C379", "#E5C07B", "#56B6C2", "#E06C75",
    ];
    let code: Vec<Svgr> = (0..22u32)
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

    // The card lifts out of the stack and floats over the editor as a pinned window.
    let sp = spring(t - PIN_CLICK - 0.02, 150.0, 18.0);
    let (w0, w1) = (STACK_W, 560.0);
    let pw = lerp(w0, w1, sp);
    let ph = pw * SHOT_H / SHOT_W;
    let (x0, y0) = (STACK_X + w0 / 2.0, STACK_Y[0] + w0 * SHOT_H / SHOT_W / 2.0);
    let (x1, y1) = (1180.0 + w1 / 2.0, 130.0 + w1 * SHOT_H / SHOT_W / 2.0);
    let (pcx, pcy) = (lerp(x0, x1, sp), lerp(y0, y1, sp));
    let rot = 4.0 * (std::f32::consts::PI * c01(sp)).sin();
    let float = sp.min(1.0);
    let hover = seg(t, -0.3, 0.2) * (1.0 - seg(t, PIN_CLICK + 0.02, 0.15));
    let badge = out_back(seg(t, PIN_BADGE, 0.26));
    let pin_corner = (STACK_X + STACK_W - 26.0, STACK_Y[0] + 26.0);
    let (cx, cy) = glide(
        t,
        &[
            (-0.4, (1500.0, 900.0)),
            (0.25, (pin_corner.0 - 4.0, pin_corner.1 + 6.0)),
            (0.6, (pin_corner.0 - 4.0, pin_corner.1 + 6.0)),
            (1.1, (1560.0, 900.0)),
        ],
    );
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
    let bar = taskbar("pin-bar");
    let cam = cam_path(t, &[(0.0, CAM0), (1.5, Cam::at(1100.0, 480.0, 1.04))]);
    let body = fframes::svgr!(<g>
        {wall}
        {bar}
        <defs>
            <clipPath id="code-clip"><rect x={ex} y={ey + 44.0} width={ew} height={eh - 44.0} /></clipPath>
            <filter id="pin-shadow" x="-30%" y="-30%" width="160%" height="180%"><feGaussianBlur stdDeviation="26" /></filter>
        </defs>
        <rect x={ex + 10.0} y={ey + 26.0} width={ew - 20.0} height={eh} rx="12" fill="#000000" opacity="0.45" />
        <rect x={ex} y={ey} width={ew} height={eh} rx="12" fill="#1B1D23" />
        <rect x={ex} y={ey} width={ew} height="44" rx="12" fill="#22252C" />
        <rect x={ex} y={ey + 30.0} width={ew} height="14" fill="#22252C" />
        <text x={ex + 22.0} y={ey + 29.0} font-family={UI} font-size="16" fill={MUTED}>"report.ts"</text>
        <g clip-path="url(#code-clip)">{code}</g>
        {dim(0.2 * float)}
        <g transform={format!("translate({pcx:.1} {pcy:.1}) rotate({rot:.2}) translate({:.1} {:.1})", -pw / 2.0, -ph / 2.0)}>
            <rect x="8" y="28" width={pw - 16.0} height={ph - 8.0} rx="14" fill="#000000" opacity={0.4 + 0.3 * float} filter="url(#pin-shadow)" />
            {pinned}
            <rect width={pw} height={ph} rx="12" fill="none" stroke={ACCENT} stroke-width="3" opacity={float} />
            <rect width={pw} height={ph} rx="12" fill="#000000" opacity={0.45 * hover} />
            <g transform={format!("translate({:.1} 26)", pw - 26.0)} opacity={hover}>
                <circle r="18" fill="#111827" fill-opacity="0.85" />
                <path d={PIN_GLYPH} stroke="#FFFFFF" stroke-width="2.2" fill="none" stroke-linecap="round" stroke-linejoin="round" />
            </g>
            <g transform={format!("scale({badge:.3})")} opacity={c01(badge)}>
                <circle r="24" fill={ACCENT} stroke="#FFFFFF" stroke-width="3" />
                <path d="M-4 -10 h8 l-1 7 l5 4 h-16 l5 -4 Z M0 1 V11" fill="#FFFFFF" stroke="#FFFFFF" stroke-width="2" stroke-linejoin="round" stroke-linecap="round" />
            </g>
        </g>
        {ripple(pin_corner.0, pin_corner.1, t, PIN_CLICK, ACCENT)}
        {cursor(cx, cy, 1.0)}
    </g>);
    view(cam, body)
}

// ---------------------------------------------------------------- editor

/// Toolbar indicator sliding between tools at the given switch times.
fn tool_at(t: f32, hops: &[(f32, usize)]) -> (f32, usize) {
    let mut pos = hops[0].1 as f32;
    let mut lit = hops[0].1;
    for w in hops.windows(2) {
        let (at, to) = w[1];
        pos = lerp(pos, to as f32, out_expo(seg(t, at, 0.2)));
        if t >= at {
            lit = to;
        }
    }
    (pos, lit)
}

/// The editor's settled state once the annotations are drawn.
const EDITED: EditorView = EditorView {
    tool: T_MARKER as f32,
    lit: T_MARKER,
    marks: Marks::DONE,
    redact: 0.0,
    pad: 72.0,
    bg: Bg::Gradient,
    next: None,
    swatch: 0.0,
    redaction_on: 0.0,
    tilt: 0.0,
};

/// A point in shot coordinates, as it lands on the canvas with the editor on the desktop.
fn ed_pt(view: &EditorView, p: (f32, f32)) -> (f32, f32) {
    win_pt(view.to_screen(p))
}

/// Pushed in on the editor canvas: where Annotate ends and Smart redaction begins.
fn canvas_cam() -> Cam {
    let (x, y) = win_pt(CANVAS_C);
    Cam::at(x, y, 1.12)
}

/// Close on the account card: where Smart redaction ends and the next shot begins.
fn account_cam() -> Cam {
    let (x, y) = ed_pt(&EDITED, (888.0, 470.0));
    Cam::at(x, y, 1.42)
}

const ARROW_AT: f32 = 0.25;
const RECT_AT: f32 = 0.9;
const MARK_AT: [f32; 3] = [1.4, 1.55, 1.7];
pub const ANNOTATE_CUES: &[Cue] = &[
    (MARK_AT[0], "pop.wav", -12.0),
    (MARK_AT[1], "pop.wav", -12.0),
    (MARK_AT[2], "pop_hi.wav", -12.0),
];

pub fn annotate<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let marks = Marks {
        arrow: in_out(seg(t, ARROW_AT, 0.5)),
        rect: in_out(seg(t, RECT_AT, 0.36)),
        markers: MARK_AT.map(|at| spring(t - at, 320.0, 16.0)),
    };
    let (tool, lit) = tool_at(t, &[(0.0, T_ARROW), (0.8, T_RECT), (1.3, T_MARKER)]);
    let view = EditorView {
        tool,
        lit,
        marks,
        ..EDITED
    };
    let (a0, ac, a1) = ARROW;
    let pen = if t < ARROW_AT {
        a0
    } else if t < 0.78 {
        quad(a0, ac, a1, marks.arrow)
    } else if t < RECT_AT {
        let p = in_out(seg(t, 0.78, 0.1));
        (lerp(a1.0, RECT.0, p), lerp(a1.1, RECT.1, p))
    } else if t < MARK_AT[0] - 0.12 {
        rect_point(marks.rect)
    } else {
        let i = MARK_AT.iter().rposition(|at| t >= at - 0.12).unwrap_or(0);
        let from = if i == 0 {
            (RECT.0, RECT.1)
        } else {
            MARKERS[i - 1]
        };
        let p = in_out(seg(t, MARK_AT[i] - 0.12, 0.1));
        (lerp(from.0, MARKERS[i].0, p), lerp(from.1, MARKERS[i].1, p))
    };
    let (px0, py0) = ed_pt(&view, pen);
    let (cx, cy) = if t < ARROW_AT {
        let p = in_out(seg(t, -0.1, ARROW_AT + 0.08));
        (lerp(1380.0, px0, p), lerp(900.0, py0, p))
    } else {
        (px0, py0)
    };
    let cam = cam_path(t, &[(0.0, CAM0), (0.3, CAM0), (1.9, canvas_cam())]);
    let window = on_desktop("ann", view.render("ann"));
    view_cam(cam, window, cursor(cx, cy, 1.0))
}

fn view_cam<'a>(cam: Cam, scene: Svgr<'a>, top: Svgr<'a>) -> Svgr<'a> {
    view(cam, fframes::svgr!(<g>{scene}{top}</g>))
}

const REDACT_CLICK: f32 = 0.45;
const REDACT_SWEEP: f32 = 0.55;
pub const REDACT_CUES: &[Cue] = &[
    (REDACT_CLICK, "click.wav", -11.0),
    (REDACT_SWEEP, "glitch.wav", -13.0),
];
/// The Smart redaction toggle in the sidebar, in window coordinates.
const REDACT_TOGGLE: (f32, f32) = (1630.0, 706.0);

fn sweep_point(redact: f32) -> (f32, f32) {
    (
        744.0 + 290.0 * redact,
        lerp(450.0, 530.0, (redact * 3.0).sin().abs()),
    )
}

pub fn smart_redaction<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let redact = in_out(seg(t, REDACT_SWEEP, 0.5));
    let view = EditorView {
        redact,
        redaction_on: seg(t, REDACT_CLICK, 0.1),
        ..EDITED
    };
    let toggle_pt = win_pt(REDACT_TOGGLE);
    let start = ed_pt(&view, MARKERS[2]);
    let sweep = ed_pt(&view, sweep_point(redact));
    let (cx, cy) = if t < REDACT_SWEEP {
        glide(t, &[(0.0, start), (0.4, toggle_pt)])
    } else {
        let p = in_out(seg(t, REDACT_SWEEP, 0.2));
        (lerp(toggle_pt.0, sweep.0, p), lerp(toggle_pt.1, sweep.1, p))
    };
    let cam = cam_path(
        t,
        &[(0.0, canvas_cam()), (0.5, canvas_cam()), (1.4, account_cam())],
    );
    let window = on_desktop("red", view.render("red"));
    let top = fframes::svgr!(<g>{ripple(toggle_pt.0, toggle_pt.1, t, REDACT_CLICK, ACCENT)}{cursor(cx, cy, 1.0)}</g>);
    view_cam(cam, window, top)
}

const BG_CLICK: [f32; 2] = [0.4, 0.75];
const TILT_AT: f32 = 1.1;
pub const BACKGROUND_CUES: &[Cue] = &[
    (BG_CLICK[0], "tick.wav", -11.0),
    (BG_CLICK[1], "tick.wav", -11.0),
    (TILT_AT, "click.wav", -14.0),
];

pub fn backgrounds_and_mockups<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (bg, next) = if t < BG_CLICK[1] {
        (Bg::Gradient, Some((Bg::Solid, seg(t, BG_CLICK[0], 0.2))))
    } else {
        (Bg::Solid, Some((Bg::Wallpaper, seg(t, BG_CLICK[1], 0.2))))
    };
    let pad = lerp(
        lerp(72.0, 44.0, spring(t - BG_CLICK[0], 260.0, 22.0)),
        104.0,
        spring(t - BG_CLICK[1], 260.0, 22.0),
    );
    let swatch = out_expo(seg(t, BG_CLICK[0], 0.18)) + out_expo(seg(t, BG_CLICK[1], 0.18));
    let tilt = in_out(seg(t, TILT_AT + 0.05, 0.5));
    let view = EditorView {
        redact: 1.0,
        redaction_on: 1.0,
        pad,
        bg,
        next,
        swatch,
        tilt,
        ..EDITED
    };
    let solid = win_pt((1668.0, 206.0));
    let wall = win_pt((1724.0, 206.0));
    let knob = |v: f32| win_pt((1590.0 + 300.0 * (0.12 + 0.6 * v), 842.0));
    let start = ed_pt(&view, sweep_point(1.0));
    let (cx, cy) = if t < TILT_AT {
        glide(
            t,
            &[
                (0.0, start),
                (0.35, solid),
                (0.5, solid),
                (0.7, wall),
                (0.82, wall),
                (1.05, knob(0.0)),
            ],
        )
    } else {
        knob(tilt)
    };
    let side_cam = Cam::at(1010.0, 480.0, 1.04);
    let cam = cam_path(
        t,
        &[
            (0.0, account_cam()),
            (0.4, side_cam),
            (1.1, side_cam),
            (1.9, Cam::at(980.0, 470.0, 1.1)),
        ],
    );
    let window = on_desktop("bgs", view.render("bgs"));
    let top = fframes::svgr!(<g>
        {ripple(solid.0, solid.1, t, BG_CLICK[0], "#FFFFFF")}
        {ripple(wall.0, wall.1, t, BG_CLICK[1], "#FFFFFF")}
        {cursor(cx, cy, 1.0)}
    </g>);
    view_cam(cam, window, top)
}

// ---------------------------------------------------------------- record

const REC_AREA: f32 = 0.6;
const REC_MIC: f32 = 0.9;
const REC_TELE: f32 = 1.2;
const REC_CLICK: f32 = 1.5;
const REC_COUNT: f32 = 1.62;
const REC_START: f32 = 2.17;
pub const RECORD_CUES: &[Cue] = &[
    (REC_AREA, "click.wav", -11.0),
    (REC_MIC, "tick.wav", -13.0),
    (REC_TELE, "tick.wav", -13.0),
    (REC_CLICK, "click.wav", -9.0),
    (REC_COUNT, "tick.wav", -15.0),
    (REC_COUNT + 0.183, "tick.wav", -15.0),
    (REC_COUNT + 0.367, "tick.wav", -15.0),
    (REC_START, "rec.wav", -9.0),
];
/// Where Record ends and Teleprompter begins.
const REC_CAM: Cam = Cam::at(960.0, 560.0, 1.03);

fn control_bar<'a>(t: f32, timer: String, enter: f32) -> Svgr<'a> {
    let pulse = 0.65 + 0.35 * (t * 5.0).cos().abs();
    fframes::svgr!(<g transform={format!("translate(0 {:.1})", 110.0 * (1.0 - enter))} opacity={c01(enter * 2.0)}>
        <rect x="790" y="930" width="340" height="60" rx="30" fill="#1C1C1E" stroke="#FFFFFF" stroke-opacity="0.12" />
        <circle cx="830" cy="960" r="10" fill={REC} opacity={pulse} />
        <text x="856" y="970" font-family={MONO} font-size="28" fill={INK}>{timer}</text>
        <rect x="976" y="946" width="1" height="28" fill="#FFFFFF" fill-opacity="0.2" />
        <path d="M996 950 V970 M1004 950 V970" stroke="#FFFFFF" stroke-width="3" stroke-linecap="round" />
        <path d="M1036 952 A9 9 0 1 0 1045 960 M1045 950 V960 H1035" stroke="#FFFFFF" stroke-width="2.4" fill="none" stroke-linecap="round" />
        <rect x="1070" y="951" width="18" height="18" rx="3" fill="#FFFFFF" />
    </g>)
}

pub fn record<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (px0, py0, pw, ph) = (460.0, 330.0, 1000.0, 280.0);
    let mode = lerp(0.0, 2.0, out_expo(seg(t, REC_AREA, 0.3)));
    let collapse = in_cubic(seg(t, REC_CLICK + 0.02, 0.12));
    let picker_in = out_expo(seg(t, 0.0, 0.35));
    let picker_s = lerp(0.94, 1.0, picker_in) * lerp(1.0, 0.85, collapse);
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
    let rec_btn = (px0 + 880.0, py0 + 86.0);
    let toggles = [
        (px0 + 40.0, "Microphone", out_expo(seg(t, REC_MIC, 0.14))),
        (px0 + 340.0, "System audio", 1.0),
        (px0 + 660.0, "Teleprompter", out_expo(seg(t, REC_TELE, 0.14))),
    ];
    let toggle_ui: Vec<Svgr> = toggles
        .iter()
        .map(|&(x, label, on)| {
            fframes::svgr!(<g>
                {toggle(x, py0 + 200.0, on)}
                <text x={x + 78.0} y={py0 + 226.0} font-family={UI} font-size="28" fill={INK}>{label}</text>
            </g>)
        })
        .collect();
    let (cx, cy) = glide(
        t,
        &[
            (0.0, (1300.0, 800.0)),
            (0.2, (1300.0, 800.0)),
            (REC_AREA - 0.04, (px0 + 40.0 + 2.0 * 196.0 + 90.0, py0 + 80.0)),
            (REC_MIC - 0.04, (toggles[0].0 + 30.0, py0 + 216.0)),
            (REC_TELE - 0.04, (toggles[2].0 + 30.0, py0 + 216.0)),
            (REC_CLICK - 0.04, rec_btn),
        ],
    );
    let picker = fframes::svgr!(<g transform={about(960.0, 470.0, picker_s)} opacity={picker_in * (1.0 - collapse)}>
        <rect x={px0 + 10.0} y={py0 + 26.0} width={pw - 20.0} height={ph} rx="18" fill="#000000" opacity="0.5" />
        <rect x={px0} y={py0} width={pw} height={ph} rx="18" fill="#202020" fill-opacity="0.97" stroke="#FFFFFF" stroke-opacity="0.12" />
        <rect x={sel_x} y={py0 + 20.0} width="180" height="132" rx="12" fill={ACCENT} fill-opacity="0.26" stroke={ACCENT} stroke-width="2" />
        {buttons}
        <rect x={px0 + 30.0} y={py0 + 172.0} width={pw - 60.0} height="1" fill="#FFFFFF" fill-opacity="0.1" />
        {toggle_ui}
        <circle cx={rec_btn.0} cy={rec_btn.1} r="46" fill="none" stroke="#FFFFFF" stroke-opacity="0.5" stroke-width="3" />
        <circle cx={rec_btn.0} cy={rec_btn.1} r={36.0 - 4.0 * seg(t, REC_CLICK, 0.04) + 4.0 * seg(t, REC_CLICK + 0.04, 0.06)} fill={REC} />
    </g>);

    // Countdown: a ring closes around 3, 2, 1, then the control bar rises.
    let count = seg(t, REC_COUNT, REC_START - REC_COUNT);
    let n = ((count * 3.0) as usize).min(2);
    let ring_o = seg(t, REC_COUNT - 0.04, 0.06) * (1.0 - seg(t, REC_START, 0.08));
    let r = 76.0;
    let circ = std::f32::consts::TAU * r;
    let step = (REC_START - REC_COUNT) / 3.0;
    let digit_local = t - REC_COUNT - n as f32 * step;
    let digit_s = lerp(1.3, 1.0, out_expo(seg(digit_local, 0.0, 0.1)));
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
    let cam = cam_path(t, &[(0.0, CAM0), (2.5, REC_CAM)]);
    let body = fframes::svgr!(<g>
        {scene}
        {dim(0.35)}
        {guide(WIN, t, 0.3, seg(t, REC_AREA, 0.1))}
        {picker}
        {countdown}
        {control_bar(t, "00:00".to_string(), bar_in)}
        {ripple(cx, cy, t, REC_AREA, ACCENT)}
        {ripple(cx, cy, t, REC_MIC, ACCENT)}
        {ripple(cx, cy, t, REC_TELE, ACCENT)}
        {ripple(cx, cy, t, REC_CLICK, REC)}
        {cursor(cx, cy, 1.0 - seg(t, REC_CLICK + 0.04, 0.1))}
    </g>);
    view(cam, body)
}

const SCRIPT: [&str; 5] = [
    "Here's last month's revenue.",
    "Click any day to see its orders.",
    "Then export the report.",
    "That's it.",
    "",
];
const TELE_IN: f32 = 0.05;
pub const TELE_CUES: &[Cue] = &[(TELE_IN, "pop.wav", -17.0)];

pub fn teleprompter<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (px0, py0, pw, ph) = (480.0, 96.0, 960.0, 170.0);
    let drop = out_expo(seg(t, TELE_IN, 0.4));
    let scroll = 1.2 * in_out(seg(t, 0.4, 1.1));
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
            let h = 10.0 + 34.0 * (0.5 + 0.5 * (t * 17.0 + i as f32 * 1.7).sin()) * hash(i + (t * 20.0).max(0.0) as u32 * 5);
            fframes::svgr!(<rect x={px0 + 44.0 + i as f32 * 12.0} y={py0 + ph / 2.0 - h / 2.0} width="6" height={px(h)} rx="3" fill="#34D399" />)
        })
        .collect();
    let secs = format!("00:0{}", if t >= 1.0 { 1 } else { 0 });
    let scene = desktop("tele", 0.0);
    let cam = cam_path(t, &[(0.0, REC_CAM), (0.3, REC_CAM), (1.5, Cam::at(960.0, 430.0, 1.08))]);
    let body = fframes::svgr!(<g>
        {scene}
        {dim(0.35)}
        {guide(WIN, t, 0.3, 1.0)}
        <defs><clipPath id="script-clip"><rect x={px0 + 110.0} y={py0 + 44.0} width={pw - 130.0} height={ph - 54.0} /></clipPath></defs>
        <g transform={format!("translate(0 {:.1})", -60.0 * (1.0 - drop))} opacity={drop}>
            <rect x={px0 + 8.0} y={py0 + 22.0} width={pw - 16.0} height={ph} rx="30" fill="#000000" opacity="0.5" />
            <rect x={px0} y={py0} width={pw} height={ph} rx="30" fill="#111214" fill-opacity="0.95" stroke="#FFFFFF" stroke-opacity="0.12" />
            {levels}
            <rect x={px0 + 110.0} y={py0 + 30.0} width="1" height={ph - 60.0} fill="#FFFFFF" fill-opacity="0.15" />
            <g clip-path="url(#script-clip)">{lines}</g>
        </g>
        {control_bar(t + 3.0, secs, 1.0)}
    </g>);
    view(cam, body)
}

// ---------------------------------------------------------------- studio

/// The playhead runs in real time through a 12 s recording across Studio's shots.
fn studio_head(shot_start: f32, t: f32) -> f32 {
    0.215 + (shot_start + t) / 12.0
}

/// The Studio preview centre on the canvas, and the camera that pushes in on it.
fn preview_cam() -> Cam {
    let (px0, py0, pw, ph) = PREVIEW;
    let (x, y) = win_pt((px0 + pw / 2.0, py0 + ph / 2.0 + 120.0));
    Cam::at(x, y, 1.18)
}

/// Where Captions and transcript ends: the preview and the inspector both in view.
const CAPTIONS_CAM: Cam = Cam::at(1010.0, 420.0, 1.08);

/// The recorded screen inside the preview, zoomed by `z` on the chart peak, with Studio's
/// enlarged cursor at `world`.
fn recording<'a>(id: &str, z: f32, world: (f32, f32), t: f32, click: f32) -> Svgr<'a> {
    let peak = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
    let scene = desktop(id, 0.0);
    let p = seg(t, click, 0.5);
    let ring = if p > 0.0 && p < 1.0 {
        fframes::svgr!(<circle cx={peak.0} cy={peak.1} r={10.0 + 60.0 * out_cubic(p)} fill="none" stroke={ACCENT} stroke-width={8.0 * (1.0 - p) + 1.0} opacity={1.0 - p} />)
    } else {
        Svgr::empty()
    };
    fframes::svgr!(<g transform={camera_transform(peak.0, peak.1, z)}>
        {scene}
        {ring}
        {cursor_at(world.0, world.1, 1.0, 3.2)}
    </g>)
}

const ZOOM_CLICK: f32 = 0.55;
pub const ZOOM_CUES: &[Cue] = &[
    (ZOOM_CLICK, "click.wav", -11.0),
    (ZOOM_CLICK + 0.05, "whoosh_short.wav", -19.0),
];

pub fn auto_zoom<'a>(f: &mut Frame, c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let peak = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
    let z = 1.0 + 0.75 * in_out(seg(t, ZOOM_CLICK + 0.05, 0.6));
    let world = glide(t, &[(0.0, (1240.0, 800.0)), (ZOOM_CLICK - 0.03, peak)]);
    let recorded = recording("zoom", z, world, t, ZOOM_CLICK);
    let block = out_cubic(seg(t, ZOOM_CLICK + 0.05, 0.45));
    let window = studio(
        "zoom-studio",
        studio_head(0.0, t),
        block,
        recorded,
        Svgr::empty(),
        transcript_panel(f, c, 1.0, 0.0, 0.0),
    );
    let cam = cam_path(t, &[(0.0, CAM0), (0.7, CAM0), (1.9, preview_cam())]);
    view(cam, on_desktop("zoom", window))
}

const CUT_CLICK: f32 = 0.5;
const CUT_AT: f32 = 0.72;
const KARAOKE_AT: f32 = 1.1;
const WORD_STEP: f32 = 0.2;
pub const CAPTION_CUES: &[Cue] = &[
    (CUT_CLICK, "click.wav", -10.0),
    (CUT_AT, "pop_hi.wav", -14.0),
];
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
/// The Remove fillers button in the inspector, in window coordinates.
const FILLER_BTN: (f32, f32, f32, f32) = (1590.0, 560.0, 240.0, 52.0);

/// Lays the words out from `x0` on one line, fillers shrinking away with `keep`.
fn word_row(widths: &[f32], words: &[(&str, bool)], x0: f32, space: f32, keep: f32) -> Vec<f32> {
    let mut x = x0;
    words
        .iter()
        .zip(widths)
        .map(|((_, filler), w)| {
            let here = x;
            x += (w + space) * if *filler { keep } else { 1.0 };
            here
        })
        .collect()
}

/// The transcript in Studio's inspector: fillers marked, struck through (`strike`) and
/// shrunk away (`keep` from 1 to 0), with the button that removes them.
fn transcript_panel<'a>(f: &mut Frame, c: &Ctx<'a, '_>, keep: f32, strike: f32, pressed: f32) -> Svgr<'a> {
    let tsize = 30;
    let font = FontQuery {
        family: UI,
        size: tsize,
        weight: 600,
        ..Default::default()
    };
    let twidths: Vec<f32> = TRANSCRIPT
        .iter()
        .map(|(w, _)| f.text_width(c, font, w).unwrap_or(60) as f32)
        .collect();
    let tspace = 10.0;
    let line_of = |i: usize| if i < 5 { 0 } else { 1 };
    let mut transcript: Vec<Svgr> = Vec::new();
    for line in 0..2 {
        let idx: Vec<usize> = (0..TRANSCRIPT.len()).filter(|&i| line_of(i) == line).collect();
        let words: Vec<(&str, bool)> = idx.iter().map(|&i| TRANSCRIPT[i]).collect();
        let ws: Vec<f32> = idx.iter().map(|&i| twidths[i]).collect();
        let lx = word_row(&ws, &words, 1590.0, tspace, keep);
        let y = 450.0 + line as f32 * 50.0;
        for (j, &(w, filler)) in words.iter().enumerate() {
            let x = lx[j];
            if filler {
                if keep <= 0.0 {
                    continue;
                }
                transcript.push(fframes::svgr!(<g transform={format!("translate({x:.1} 0) scale({:.4} 1)", px(keep).min(1.0))} opacity={keep}>
                    <rect x="-6" y={y - 30.0} width={ws[j] + 12.0} height="42" rx="8" fill={HOT} fill-opacity="0.22" />
                    <text y={y} font-family={UI} font-weight="600" font-size={tsize} fill={HOT}>{w}</text>
                    <rect x="-3" y={y - 11.0} width={px((ws[j] + 6.0) * strike)} height="4" rx="2" fill={HOT} />
                </g>));
            } else {
                transcript.push(fframes::svgr!(<text x={x} y={y} font-family={UI} font-weight="600" font-size={tsize} fill={INK}>{w}</text>));
            }
        }
    }
    let (bx, by, bw, bh) = FILLER_BTN;
    fframes::svgr!(<g>
        <text x="1590" y="400" font-family={UI} font-size="28" fill={MUTED}>"Transcript"</text>
        {transcript}
        <rect x={bx} y={by} width={bw} height={bh} rx="10" fill={ACCENT} fill-opacity={0.2 + 0.6 * pressed} stroke={ACCENT} stroke-width="2" />
        <text x={bx + bw / 2.0} y={by + 35.0} text-anchor="middle" font-family={UI} font-weight="600" font-size="26" fill={INK}>"Remove fillers"</text>
    </g>)
}

pub fn captions<'a>(f: &mut Frame, c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let keep = 1.0 - in_out(seg(t, CUT_AT, 0.3));
    let strike = out_cubic(seg(t, CUT_CLICK + 0.04, 0.14));
    let measure = |f: &mut Frame, size: usize| -> Vec<f32> {
        let font = FontQuery {
            family: UI,
            size,
            weight: 600,
            ..Default::default()
        };
        TRANSCRIPT
            .iter()
            .map(|(w, _)| f.text_width(c, font, w).unwrap_or(60) as f32)
            .collect()
    };

    // The caption bar in the preview, with a karaoke highlight once the fillers are gone.
    let (px0, py0, pw, ph) = PREVIEW;
    let size = 36;
    let widths = measure(f, size);
    let space = 12.0;
    let total: f32 = TRANSCRIPT
        .iter()
        .zip(&widths)
        .map(|((_, filler), w)| (w + space) * if *filler { keep } else { 1.0 })
        .sum::<f32>()
        - space;
    let x0 = px0 + pw / 2.0 - total / 2.0;
    let xs = word_row(&widths, &TRANSCRIPT, x0, space, keep);
    let cap_y = py0 + ph - 40.0;
    let kept: Vec<usize> = (0..TRANSCRIPT.len()).filter(|&i| !TRANSCRIPT[i].1).collect();
    let beat = (t - KARAOKE_AT) / WORD_STEP;
    let lit = if beat >= 0.0 {
        Some(kept[(beat as usize).min(kept.len() - 1)])
    } else {
        None
    };
    let highlight = lit.map_or(Svgr::empty(), |i| {
        let next = kept.iter().position(|&k| k == i).and_then(|k| kept.get(k + 1)).copied().unwrap_or(i);
        let g = if next == i { 0.0 } else { out_cubic(c01((beat.fract() - 0.6) * 2.5)) };
        let hx = lerp(xs[i], xs[next], g);
        let hw = lerp(widths[i], widths[next], g);
        fframes::svgr!(<rect x={hx - 8.0} y={cap_y - 37.0} width={hw + 16.0} height="50" rx="10" fill={ACCENT} opacity={seg(t, KARAOKE_AT, 0.08)} />)
    });
    let words: Vec<Svgr> = TRANSCRIPT
        .iter()
        .enumerate()
        .map(|(i, (w, filler))| {
            if *filler {
                let k = keep;
                if k <= 0.0 {
                    return Svgr::empty();
                }
                fframes::svgr!(<text transform={format!("translate({:.1} {cap_y:.1}) scale({:.4} 1)", xs[i], px(k).min(1.0))} font-family={UI} font-weight="600" font-size={size} fill={HOT} opacity={k}>{*w}</text>)
            } else {
                let spoken = beat >= kept.iter().position(|&k| k == i).unwrap_or(0) as f32;
                fframes::svgr!(<text x={xs[i]} y={cap_y} font-family={UI} font-weight="600" font-size={size} fill="#FFFFFF" opacity={if spoken { 1.0 } else { 0.62 }}>{*w}</text>)
            }
        })
        .collect();
    let caption = fframes::svgr!(<g opacity={out_cubic(seg(t, 0.05, 0.3))}>
        <rect x={x0 - 22.0} y={cap_y - 48.0} width={total + 44.0} height="68" rx="16" fill="#000000" fill-opacity="0.66" />
        {highlight}
        {words}
    </g>);

    let pressed = seg(t, CUT_CLICK, 0.06) * (1.0 - seg(t, CUT_CLICK + 0.15, 0.15));
    let panel = transcript_panel(f, c, keep, strike, pressed);
    let (bx, by, bw, bh) = FILLER_BTN;

    let peak = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
    let recorded = recording("cap", 1.75, peak, t, -10.0);
    let window = studio(
        "cap-studio",
        studio_head(2.0, t),
        1.0,
        recorded,
        caption,
        panel,
    );
    let btn = win_pt((bx + bw / 2.0, by + bh / 2.0));
    let (cx, cy) = glide(
        t,
        &[
            (0.0, (1500.0, 760.0)),
            (0.1, (1500.0, 760.0)),
            (CUT_CLICK - 0.04, (btn.0 + 10.0, btn.1 + 6.0)),
            (1.0, (btn.0 + 10.0, btn.1 + 6.0)),
            (1.5, (1560.0, 820.0)),
        ],
    );
    let cursor_o = seg(t, 0.0, 0.15) * (1.0 - seg(t, 1.2, 0.3));
    let cam = cam_path(t, &[(0.0, preview_cam()), (0.15, preview_cam()), (0.9, CAPTIONS_CAM)]);
    let top = fframes::svgr!(<g>{ripple(btn.0, btn.1, t, CUT_CLICK, ACCENT)}{cursor(cx, cy, cursor_o)}</g>);
    view_cam(cam, on_desktop("cap", window), top)
}

const EXPORT_CLICK: f32 = 0.25;
const EXPORT_DONE: f32 = 1.05;
pub const EXPORT_CUES: &[Cue] = &[
    (EXPORT_CLICK, "click.wav", -10.0),
    (EXPORT_DONE, "pop.wav", -10.0),
];

pub fn export<'a>(_f: &mut Frame, _c: &Ctx<'a, '_>, t: f32) -> Svgr<'a> {
    let (dx, dy, dw, dh) = (500.0, 300.0, 920.0, 340.0);
    let enter = out_expo(seg(t, -0.35, 0.45));
    let p = in_out(seg(t, EXPORT_CLICK + 0.05, EXPORT_DONE - EXPORT_CLICK - 0.05));
    let done = t >= EXPORT_DONE;
    let check = out_back(seg(t, EXPORT_DONE, 0.26));
    let chip = |label: &'a str, x: f32, w: f32| -> Svgr<'a> {
        fframes::svgr!(<g transform={format!("translate({x:.1} {:.1})", dy + 98.0)}>
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
    let btn = (dx + 420.0, dy + 248.0, 170.0, 56.0);
    let pressed = seg(t, EXPORT_CLICK, 0.05) * (1.0 - seg(t, EXPORT_CLICK + 0.12, 0.15));
    let (cx, cy) = glide(
        t,
        &[
            (-0.3, (1240.0, 860.0)),
            (EXPORT_CLICK - 0.03, (btn.0 + btn.2 / 2.0 + 12.0, btn.1 + btn.3 / 2.0 + 8.0)),
            (0.6, (btn.0 + btn.2 / 2.0 + 12.0, btn.1 + btn.3 / 2.0 + 8.0)),
            (1.1, (1300.0, 840.0)),
        ],
    );
    let peak = (SHOT_X + PEAK.0, SHOT_Y + PEAK.1);
    let recorded = recording("exp-rec", 1.75, peak, t, -10.0);
    let window = studio(
        "exp-studio",
        studio_head(4.5, 0.0),
        1.0,
        recorded,
        Svgr::empty(),
        Svgr::empty(),
    );
    let dialog = fframes::svgr!(<g transform={about(960.0, 470.0, lerp(0.94, 1.0, enter))} opacity={enter}>
        <defs>
            <linearGradient id="exp-bg" x1="0" y1="0" x2="1" y2="1">
                <stop offset="0" stop-color="#5B8CFF" /><stop offset="0.5" stop-color="#A78BFA" /><stop offset="1" stop-color="#F472B6" />
            </linearGradient>
        </defs>
        <rect x={dx + 12.0} y={dy + 30.0} width={dw - 24.0} height={dh} rx="20" fill="#000000" opacity="0.55" />
        <rect x={dx} y={dy} width={dw} height={dh} rx="20" fill="#202020" stroke="#FFFFFF" stroke-opacity="0.12" />
        <rect x={dx + 40.0} y={dy + 40.0} width="340" height="220" rx="14" fill="url(#exp-bg)" />
        <g transform={format!("translate({} {})", dx + 60.0, dy + 66.0)}>{thumb}</g>
        <text x={dx + 40.0} y={dy + 304.0} font-family={UI} font-size="24" fill={MUTED}>"Revenue walkthrough.mp4"</text>
        {chip("4K", dx + 420.0, 90.0)}
        {chip("60 fps", dx + 526.0, 132.0)}
        {chip("MP4", dx + 674.0, 104.0)}
        <rect x={bx} y={by - 6.0} width={bw} height="12" rx="6" fill="#FFFFFF" fill-opacity="0.12" />
        <rect x={bx} y={by - 6.0} width={px(bw * p)} height="12" rx="6" fill={bar_color} />
        <rect x={btn.0} y={btn.1} width={btn.2} height={btn.3} rx="10" fill={ACCENT} fill-opacity={1.0 - 0.3 * pressed} />
        <text x={btn.0 + btn.2 / 2.0} y={btn.1 + 37.0} text-anchor="middle" font-family={UI} font-weight="600" font-size="28" fill="#FFFFFF">"Export"</text>
        <text x={bx + bw} y={btn.1 + 40.0} text-anchor="end" font-family={MONO} font-size="32" fill={INK}>{percent}</text>
        <g transform={format!("translate({} {}) scale({check:.3})", bx + bw - 120.0, btn.1 + 28.0)} opacity={c01(check)}>
            <circle r="18" fill="#22C55E" />
            <path d="M-8 0 l5 5 l10 -10" stroke="#FFFFFF" stroke-width="3" fill="none" stroke-linecap="round" stroke-linejoin="round" />
        </g>
    </g>);
    let body = fframes::svgr!(<g>
        {on_desktop("exp", window)}
        {dim(0.5 * enter)}
    </g>);
    let top = fframes::svgr!(<g>{dialog}{cursor(cx, cy, 1.0 - seg(t, 1.0, 0.2))}</g>);
    fframes::svgr!(<g>{view(CAPTIONS_CAM, body)}{top}</g>)
}

// ---------------------------------------------------------------- library

const QUERY: &str = "chart";
const SEARCH_CLICK: f32 = 0.55;
const TYPE_AT: f32 = 0.7;
const TYPE_STEP: f32 = 0.08;
const FILTER_AT: f32 = 1.12;
const RUSH_AT: f32 = 1.75;
pub const LIBRARY_CUES: &[Cue] = &[
    (SEARCH_CLICK, "click.wav", -12.0),
    (TYPE_AT, "click.wav", -19.0),
    (TYPE_AT + TYPE_STEP, "click.wav", -19.0),
    (TYPE_AT + 2.0 * TYPE_STEP, "click.wav", -19.0),
    (TYPE_AT + 3.0 * TYPE_STEP, "click.wav", -19.0),
    (TYPE_AT + 4.0 * TYPE_STEP, "click.wav", -19.0),
    (RUSH_AT - 0.75, "riser.wav", -12.0),
    (RUSH_AT - 0.1, "whoosh.wav", -11.0),
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
    let filter = out_cubic(seg(t, FILTER_AT, 0.2));
    let hover = seg(t, 1.45, 0.1);
    let tiles: Vec<Svgr> = TILES
        .iter()
        .enumerate()
        .map(|(i, kind)| {
            let (col, row) = (i % 4, i / 4);
            let (x, y) = (400.0 + col as f32 * 344.0, 176.0 + row as f32 * 212.0);
            let p = out_expo(seg(t, (col + row) as f32 * 0.04 + 0.02, 0.45));
            let id = format!("lib{i}");
            let content = match kind {
                Tile::Chart | Tile::Recording => shot(&id, 0.0),
                Tile::Photo => thumb_photo(id.clone()),
                Tile::Code => thumb_code(id.clone()),
            };
            let matched = matches!(kind, Tile::Chart | Tile::Recording);
            let lift = if i == 0 { 1.0 + 0.04 * hover } else { 1.0 };
            let s = lerp(0.8, 1.0, p) * lift * if matched { 1.0 } else { lerp(1.0, 0.94, filter) };
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
    let focus = seg(t, SEARCH_CLICK, 0.06);
    let caret = if (t * 4.0) as i32 % 2 == 0 || typed < QUERY.len() {
        focus
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
    let window = fframes::svgr!(<g>
        <rect width="1920" height="1080" fill="#161616" />
        <rect width="1920" height="44" fill="#202020" />
        <path d={star_path(9.0)} transform="translate(26 22)" fill={ACCENT} />
        <text x="46" y="29" font-family={UI} font-size="15" fill={MUTED}>"Glint Library"</text>
        <rect y="44" width="340" height="1036" fill="#1C1C1C" />
        {side("All", 150.0, true)}
        {side("Screenshots", 210.0, false)}
        {side("Recordings", 270.0, false)}
        <rect x="400" y="72" width="560" height="60" rx="30" fill="#262626" stroke={ACCENT} stroke-opacity={focus} stroke-width="2" />
        <circle cx="438" cy="100" r="10" fill="none" stroke={MUTED} stroke-width="2.5" />
        <path d="M445 107 L452 114" stroke={MUTED} stroke-width="2.5" stroke-linecap="round" />
        <text x="470" y="112" font-family={UI} font-size="28" fill={MUTED} opacity={1.0 - focus}>"Search"</text>
        <text x="470" y="112" font-family={UI} font-size="28" fill={INK}>{&QUERY[..typed]}</text>
        <rect x={472.0 + query_w} y="84" width="2" height="34" fill={INK} opacity={caret} />
        {tiles}
    </g>);
    let search = win_pt((620.0, 104.0));
    let tile0 = win_pt((400.0 + 220.0, 176.0 + 120.0));
    let (cx, cy) = glide(
        t,
        &[
            (0.0, (1350.0, 820.0)),
            (0.15, (1350.0, 820.0)),
            (SEARCH_CLICK - 0.04, search),
            (1.2, search),
            (1.5, tile0),
        ],
    );
    let cam = cam_path(t, &[(0.0, CAM0), (1.0, CAM0), (RUSH_AT, Cam::at(900.0, 440.0, 1.07))]);
    let body = fframes::svgr!(<g>
        {on_desktop("lib", window)}
        {ripple(search.0, search.1, t, SEARCH_CLICK, ACCENT)}
        {cursor(cx, cy, 1.0 - rush)}
    </g>);
    fframes::svgr!(<g>
        <defs>
            <radialGradient id="lib-glow" cx="0.5" cy="0.5" r="0.5">
                <stop offset="0" stop-color="#FFFFFF" stop-opacity="0.9" />
                <stop offset="0.3" stop-color={ACCENT} stop-opacity="0.5" />
                <stop offset="1" stop-color={VIOLET} stop-opacity="0" />
            </radialGradient>
        </defs>
        <g transform={about(960.0, 540.0, lerp(1.0, 1.6, rush))} opacity={1.0 - rush}>{view(cam, body)}</g>
        <rect width="1920" height="1080" fill={BG} opacity={rush * 0.6} />
        <circle cx="960" cy="520" r={px(700.0 * rush)} fill="url(#lib-glow)" opacity={rush} />
    </g>)
}
