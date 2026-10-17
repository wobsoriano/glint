//! Glint showreel: 15 s at 1920x1080 and 60 fps, cut to a 120 BPM bed (a beat every 0.5 s).
//!
//! The first 13 s are a montage of 19 shots, one feature each, and the last 2 s are the logo.
//! [`SHOTS`] is the single source of truth: each row gives a shot's start, length, feature
//! group, on-screen label, the cut that opens it, its sound cues and the function that draws
//! it. The table drives the scenes, the label overlay, the group progress rail and the audio
//! map, so adding or retiming a shot is one row edit. A compile-time check keeps the rows
//! tiled on the beat grid and ending at [`T_LOGO`].
use fframes::{
    AudioMap, AudioTimestamp::*, AudioTrack, Color, Duration, FFramesContext, FontQuery, Frame,
    Scene, Scenes, Svgr, Video, include_media_dir,
};
use kit::{about, px};
use motion::*;

mod kit;
mod logo;
mod motion;
mod shots;

include_media_dir!(pub struct ShowreelMedia, "media", stereo);

const W: f32 = 1920.0;
const H: f32 = 1080.0;
const FPS: f32 = 60.0;

/// The logo lands on the music bed's closing chord.
const T_LOGO: f32 = 13.0;

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

#[derive(Debug, Clone, Copy, PartialEq)]
enum Group {
    Capture,
    QuickAccess,
    Editor,
    Record,
    Studio,
    Library,
}

impl Group {
    fn tag(self) -> &'static str {
        match self {
            Group::Capture => "CAPTURE",
            Group::QuickAccess => "QUICK ACCESS",
            Group::Editor => "EDITOR",
            Group::Record => "RECORD",
            Group::Studio => "STUDIO",
            Group::Library => "LIBRARY",
        }
    }
}

/// How a shot opens. A whip carries the direction the picture travels in.
#[derive(Debug, Clone, Copy)]
enum Cut {
    /// The first shot: no transition.
    Open,
    /// A hard cut into a shot whose own motion continues the previous one.
    Match,
    Whip(f32, f32),
    Punch,
    Flash,
}

impl Cut {
    /// The sound at the cut: file, gain and how far before the cut it starts.
    fn sound(self) -> Option<(&'static str, f32, f32)> {
        match self {
            Cut::Open => None,
            Cut::Match => Some(("tick.wav", -14.0, 0.0)),
            Cut::Whip(..) => Some(("whoosh_short.wav", -10.0, 0.08)),
            Cut::Punch => Some(("tick.wav", -11.0, 0.0)),
            Cut::Flash => Some(("snap.wav", -7.0, 0.0)),
        }
    }
}

/// A sound inside a shot: seconds from the shot's start, file, gain in dB.
type Cue = (f32, &'static str, f32);
type Draw = for<'a, 'b> fn(&mut Frame, &FFramesContext<'a, 'b>, f32) -> Svgr<'a>;

struct Shot {
    at: f32,
    len: f32,
    group: Group,
    label: &'static str,
    cut: Cut,
    cues: &'static [Cue],
    /// Draws the shot from its local time in seconds.
    draw: Draw,
}

use Cut::*;
use Group::*;

#[rustfmt::skip]
const SHOTS: [Shot; 19] = [
    Shot { at: 0.0,  len: 1.0, group: Capture,     label: "Area",             cut: Open,             cues: shots::AREA_CUES,       draw: shots::area },
    Shot { at: 1.0,  len: 0.5, group: Capture,     label: "Window",           cut: Whip(-1.0, 0.0),  cues: shots::WINDOW_CUES,     draw: shots::window },
    Shot { at: 1.5,  len: 0.5, group: Capture,     label: "Full screen",      cut: Flash,            cues: &[],                    draw: shots::full_screen },
    Shot { at: 2.0,  len: 0.5, group: Capture,     label: "Self-timer",       cut: Punch,            cues: shots::TIMER_CUES,      draw: shots::self_timer },
    Shot { at: 2.5,  len: 1.0, group: Capture,     label: "Capture Text",     cut: Whip(0.0, -1.0),  cues: shots::TEXT_CUES,       draw: shots::capture_text },
    Shot { at: 3.5,  len: 1.0, group: QuickAccess, label: "Preview cards",    cut: Whip(-1.0, 0.0),  cues: shots::CARDS_CUES,      draw: shots::preview_cards },
    Shot { at: 4.5,  len: 0.5, group: QuickAccess, label: "Drag and drop",    cut: Match,            cues: shots::DRAG_CUES,       draw: shots::drag_and_drop },
    Shot { at: 5.0,  len: 0.5, group: QuickAccess, label: "Pin",              cut: Whip(0.0, 1.0),   cues: shots::PIN_CUES,        draw: shots::pin },
    Shot { at: 5.5,  len: 1.0, group: Editor,      label: "Annotate",         cut: Punch,            cues: shots::ANNOTATE_CUES,   draw: shots::annotate },
    Shot { at: 6.5,  len: 0.5, group: Editor,      label: "Smart redaction",  cut: Match,            cues: shots::REDACT_CUES,     draw: shots::smart_redaction },
    Shot { at: 7.0,  len: 0.5, group: Editor,      label: "Backgrounds",      cut: Whip(1.0, 0.0),   cues: shots::BACKGROUND_CUES, draw: shots::backgrounds },
    Shot { at: 7.5,  len: 0.5, group: Editor,      label: "Mockups",          cut: Match,            cues: &[],                    draw: shots::mockups },
    Shot { at: 8.0,  len: 1.0, group: Record,      label: "Record",           cut: Whip(0.0, -1.0),  cues: shots::RECORD_CUES,     draw: shots::record },
    Shot { at: 9.0,  len: 0.5, group: Record,      label: "Teleprompter",     cut: Punch,            cues: &[],                    draw: shots::teleprompter },
    Shot { at: 9.5,  len: 1.0, group: Studio,      label: "Auto zoom",        cut: Whip(1.0, 0.0),   cues: shots::ZOOM_CUES,       draw: shots::auto_zoom },
    Shot { at: 10.5, len: 0.5, group: Studio,      label: "Captions",         cut: Punch,            cues: &[],                    draw: shots::captions },
    Shot { at: 11.0, len: 0.5, group: Studio,      label: "Transcript edits", cut: Whip(0.0, -1.0),  cues: shots::TRANSCRIPT_CUES, draw: shots::transcript },
    Shot { at: 11.5, len: 0.5, group: Studio,      label: "Export",           cut: Punch,            cues: shots::EXPORT_CUES,     draw: shots::export },
    Shot { at: 12.0, len: 1.0, group: Library,     label: "Library",          cut: Whip(-1.0, 0.0),  cues: shots::LIBRARY_CUES,    draw: shots::library },
];

const _: () = {
    assert!(SHOTS[0].at == 0.0, "the montage starts at 0");
    let mut i = 1;
    while i < SHOTS.len() {
        let prev = &SHOTS[i - 1];
        assert!(
            SHOTS[i].at == prev.at + prev.len,
            "each shot starts where the previous one ends"
        );
        assert!(
            SHOTS[i].at * 2.0 == (SHOTS[i].at * 2.0) as i32 as f32,
            "cuts sit on the 0.5 s beat grid"
        );
        i += 1;
    }
    let last = &SHOTS[SHOTS.len() - 1];
    assert!(
        last.at + last.len == T_LOGO,
        "the montage ends where the logo starts"
    );
};

impl std::fmt::Debug for Shot {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(self.label)
    }
}

impl Shot {
    fn next(&self) -> Option<&'static Shot> {
        SHOTS.iter().find(|s| s.at == self.at + self.len)
    }
}

impl Scene for Shot {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(self.len)
    }

    fn name(&self) -> &'static str {
        self.label
    }

    fn render_frame<'a>(&'a self, mut frame: Frame, ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let body = (self.draw)(&mut frame, ctx, t);

        // A whip enters already travelling and leaves by speeding up the same way, with a
        // directional blur while the picture moves fast.
        let (mut dx, mut dy, mut blur_x, mut blur_y) = (0.0, 0.0, 0.0, 0.0);
        if let Whip(ux, uy) = self.cut {
            let off = 1.0 - out_expo(seg(t, 0.0, 0.34));
            let streak = 46.0 * (1.0 - seg(t, 0.0, 0.1));
            dx -= ux * 300.0 * off;
            dy -= uy * 300.0 * off;
            blur_x += ux.abs() * streak;
            blur_y += uy.abs() * streak;
        }
        if let Some(Whip(ux, uy)) = self.next().map(|n| n.cut) {
            let q = in_cubic(seg(t, self.len - 0.1, 0.1));
            dx += ux * 260.0 * q;
            dy += uy * 260.0 * q;
            blur_x += ux.abs() * 46.0 * q;
            blur_y += uy.abs() * 46.0 * q;
        }
        let s = if let Punch = self.cut {
            lerp(1.08, 1.0, out_expo(seg(t, 0.0, 0.32)))
        } else {
            1.0
        };
        let transform = format!("translate({dx:.1} {dy:.1}) {}", about(960.0, 540.0, s));
        let moved = if blur_x + blur_y > 0.5 {
            let id = format!("whip-{}", frame.index);
            fframes::svgr!(<g>
                <defs><filter id={id.clone()} x="-5%" y="-5%" width="110%" height="110%"><feGaussianBlur stdDeviation={format!("{blur_x:.1} {blur_y:.1}")} /></filter></defs>
                <g transform={transform} filter={format!("url(#{id})")}>{body}</g>
            </g>)
        } else {
            fframes::svgr!(<g transform={transform}>{body}</g>)
        };
        let flash = if let Flash = self.cut {
            kit::flash(t, 0.0)
        } else {
            Svgr::empty()
        };
        fframes::svgr!(<g>{moved}{flash}</g>)
    }
}

// ---------------------------------------------------------------- label overlay

const LABEL_X: f32 = 154.0;
const LABEL_Y: f32 = 976.0;
const LABEL_SIZE: usize = 68;
const TAG_Y: f32 = 904.0;
const RAIL_Y: f32 = 852.0;
const RAIL_PX_PER_S: f32 = 24.0;

/// One label's letters rising into place (`entering`) or leaving upward, `since` its cut.
fn label_letters<'a>(
    frame: &mut Frame,
    ctx: &FFramesContext<'a, '_>,
    label: &'static str,
    since: f32,
    entering: bool,
) -> Vec<Svgr<'a>> {
    let font = FontQuery {
        family: UI,
        size: LABEL_SIZE,
        weight: 600,
        ..Default::default()
    };
    label
        .char_indices()
        .map(|(i, ch)| {
            let x = LABEL_X + frame.text_width(ctx, font, &label[..i]).unwrap_or(0) as f32;
            // Long labels stagger tighter so every label lands within the same beat.
            let step = (0.12 / label.len() as f32).min(0.016);
            let n = label[..i].chars().count() as f32;
            let y = if entering {
                LABEL_Y + 90.0 * (1.0 - spring(since - 0.05 - n * step, 420.0, 30.0))
            } else {
                LABEL_Y - 90.0 * in_cubic(seg(since, n * step * 0.5, 0.1))
            };
            fframes::svgr!(<text x={x} y={y} font-family={UI} font-weight="600" font-size={LABEL_SIZE} fill={INK} letter-spacing="-1">{ch.to_string()}</text>)
        })
        .collect()
}

/// The feature label, its group tag and the group progress rail, bottom-left. Lives above
/// the scenes so it carries across cuts; letters roll through a mask at every cut.
fn overlay<'a>(frame: &mut Frame, ctx: &FFramesContext<'a, '_>, g: f32) -> Svgr<'a> {
    let i = SHOTS.iter().rposition(|s| g >= s.at).unwrap_or(0);
    let (current, previous, cut_at) = if g >= T_LOGO {
        (None, Some(&SHOTS[SHOTS.len() - 1]), T_LOGO)
    } else {
        (
            Some(&SHOTS[i]),
            i.checked_sub(1).map(|p| &SHOTS[p]),
            SHOTS[i].at,
        )
    };
    let since = g - cut_at;

    let incoming = current
        .map(|s| label_letters(frame, ctx, s.label, since, true))
        .unwrap_or_default();
    let outgoing = match previous {
        Some(p) if since < 0.2 => label_letters(frame, ctx, p.label, since, false),
        _ => Vec::new(),
    };

    // The tag changes only when the group does.
    let group_start = |shot: &Shot| {
        SHOTS
            .iter()
            .find(|s| s.group == shot.group)
            .map_or(0.0, |s| s.at)
    };
    let tag_cut = current.map_or(T_LOGO, group_start);
    let tag_since = g - tag_cut;
    let tag_prev = if g >= T_LOGO {
        previous
    } else {
        SHOTS.iter().rev().find(|s| s.at < tag_cut)
    };
    let tag_text = |text: &'static str, y: f32, o: f32| -> Svgr<'a> {
        fframes::svgr!(<text x={LABEL_X + 2.0} y={y} font-family={UI} font-weight="700" font-size="28" letter-spacing="6" fill={ACCENT} opacity={o}>{text}</text>)
    };
    let tag_in = current.map_or(Svgr::empty(), |s| {
        let p = out_expo(seg(tag_since, 0.04, 0.4));
        tag_text(s.group.tag(), TAG_Y + 44.0 * (1.0 - p), 1.0)
    });
    let tag_out = match tag_prev {
        Some(p) if tag_since < 0.2 => tag_text(
            p.group.tag(),
            TAG_Y - 44.0 * in_cubic(seg(tag_since, 0.0, 0.14)),
            1.0,
        ),
        _ => Svgr::empty(),
    };

    // One rail segment per group, filled with the playhead.
    let mut spans: Vec<(Group, f32, f32)> = Vec::new();
    for s in &SHOTS {
        match spans.last_mut() {
            Some((grp, _, end)) if *grp == s.group => *end = s.at + s.len,
            _ => spans.push((s.group, s.at, s.at + s.len)),
        }
    }
    let rail: Vec<Svgr> = spans
        .iter()
        .map(|&(grp, a, b)| {
            let x = LABEL_X + 2.0 + a * RAIL_PX_PER_S;
            let w = (b - a) * RAIL_PX_PER_S - 6.0;
            let fill = px(w * c01((g - a) / (b - a)));
            let lit = current.is_some_and(|s| s.group == grp);
            fframes::svgr!(<g>
                <rect x={x} y={RAIL_Y} width={w} height="4" rx="2" fill="#FFFFFF" fill-opacity={if lit { 0.32 } else { 0.16 }} />
                <rect x={x} y={RAIL_Y} width={fill} height="4" rx="2" fill={ACCENT} opacity={if g > a { 1.0 } else { 0.0 }} />
            </g>)
        })
        .collect();

    let fade = 1.0 - seg(g, T_LOGO, 0.3);
    let enter = out_expo(seg(g, 0.0, 0.5));
    fframes::svgr!(<g>
        <defs>
            <radialGradient id="scrim" cx="0" cy="1" r="1">
                <stop offset="0" stop-color="#000000" stop-opacity="0.62" />
                <stop offset="0.55" stop-color="#000000" stop-opacity="0.3" />
                <stop offset="1" stop-color="#000000" stop-opacity="0" />
            </radialGradient>
            <clipPath id="label-mask"><rect x={LABEL_X - 20.0} y={LABEL_Y - 64.0} width="1200" height="84" /></clipPath>
            <clipPath id="tag-mask"><rect x={LABEL_X - 20.0} y={TAG_Y - 30.0} width="900" height="40" /></clipPath>
        </defs>
        <rect y="620" width="1200" height="460" fill="url(#scrim)" opacity={fade} />
        <g clip-path="url(#tag-mask)">{tag_out}{tag_in}</g>
        <g clip-path="url(#label-mask)">{outgoing}{incoming}</g>
        <g opacity={fade} transform={format!("translate({:.1} 0)", -40.0 * (1.0 - enter))}>{rail}</g>
    </g>)
}

// ---------------------------------------------------------------- video

pub struct ShowreelVideo<'a> {
    pub media: &'a ShowreelMedia,
}

impl std::fmt::Debug for ShowreelVideo<'_> {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("ShowreelVideo").finish()
    }
}

impl Video for ShowreelVideo<'_> {
    const FPS: usize = FPS as usize;
    const WIDTH: usize = W as usize;
    const HEIGHT: usize = H as usize;
    const BACKGROUND_COLOR: Color = Color::BLACK;

    fn duration(&self) -> Duration<'_> {
        Duration::Auto
    }

    fn audio(&self) -> AudioMap<'_> {
        let at = |name: &'static str, t: f32, db: f32| {
            AudioTrack::new(name, Second(t.max(0.0))..Eof).gain_db(db)
        };
        let mut tracks = vec![AudioTrack::new("music.wav", Second(0.)..Eof).gain_db(-2.6)];
        for shot in &SHOTS {
            if let Some((file, db, lead)) = shot.cut.sound() {
                tracks.push(at(file, shot.at - lead, db));
            }
            tracks.extend(
                shot.cues
                    .iter()
                    .map(|&(dt, file, db)| at(file, shot.at + dt, db)),
            );
        }
        tracks.push(at("impact.wav", T_LOGO, 0.0));
        tracks.push(at("shimmer.wav", T_LOGO + 0.95, -4.5));
        AudioMap::from(tracks)
    }

    fn define_scenes(&self) -> Scenes<'_> {
        Scenes::from(
            SHOTS
                .iter()
                .map(|s| s as &dyn Scene)
                .chain([&logo::Logo as &dyn Scene])
                .collect::<Vec<_>>(),
        )
    }

    fn render_frame<'a>(&'a self, mut frame: Frame, ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let g = frame.index as f32 / FPS;
        let labels = overlay(&mut frame, ctx, g);
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
                {labels}
            </svg>
        )
    }
}
