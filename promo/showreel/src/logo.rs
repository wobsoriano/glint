//! The closing card: the glint star bursts, slides left, and the wordmark rises beside it.
use crate::kit::{px, star_path};
use crate::motion::*;
use crate::{ACCENT, INK, MUTED, UI, VIOLET};
use fframes::{Duration, FFramesContext, FontQuery, Frame, Scene, Svgr};

#[derive(Debug)]
pub struct Logo;

const WORD: &str = "Glint";

impl Scene for Logo {
    fn duration(&self) -> Duration<'_> {
        Duration::Seconds(2.0)
    }

    fn render_frame<'a>(&'a self, mut frame: Frame, ctx: &FFramesContext<'a, '_>) -> Svgr<'a> {
        let t = frame.seconds();
        let size = 200;
        let font = FontQuery {
            family: UI,
            size,
            weight: 600,
            ..Default::default()
        };
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
        let rot =
            90.0 * (1.0 - out_expo(seg(t, 0.0, 0.8))) + 8.0 * (t * 3.0).sin() * seg(t, 1.2, 0.6);
        let twinkle =
            1.0 + 0.12 * (out_back(seg(t, 1.0, 0.25)) - in_cubic(seg(t, 1.25, 0.3))).max(0.0);

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
        let sweep_x = lerp(
            text_x - 300.0,
            text_x + word_w + 300.0,
            in_out(seg(t, 0.95, 0.65)),
        );
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
