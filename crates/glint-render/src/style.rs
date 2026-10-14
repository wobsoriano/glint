//! The visual "frame" applied around a screenshot: background, padding, shadow, chrome...

use serde::{Deserialize, Serialize};
use std::path::PathBuf;

/// RGBA color packed as 0xRRGGBBAA.
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Serialize, Deserialize)]
pub struct Color(pub u32);

impl Color {
    pub const WHITE: Color = Color(0xFFFFFFFF);
    pub const BLACK: Color = Color(0x000000FF);
    pub const TRANSPARENT: Color = Color(0);

    pub const fn rgb(hex: u32) -> Self {
        Color((hex << 8) | 0xFF)
    }
    pub fn r(self) -> u8 {
        (self.0 >> 24) as u8
    }
    pub fn g(self) -> u8 {
        (self.0 >> 16) as u8
    }
    pub fn b(self) -> u8 {
        (self.0 >> 8) as u8
    }
    pub fn a(self) -> u8 {
        self.0 as u8
    }
    pub fn from_rgba(r: u8, g: u8, b: u8, a: u8) -> Self {
        Color(((r as u32) << 24) | ((g as u32) << 16) | ((b as u32) << 8) | a as u32)
    }
    pub fn with_alpha(self, a: u8) -> Self {
        Color((self.0 & 0xFFFFFF00) | a as u32)
    }
    /// Perceived luminance in 0..1.
    pub fn luma(self) -> f32 {
        (0.299 * self.r() as f32 + 0.587 * self.g() as f32 + 0.114 * self.b() as f32) / 255.0
    }
    pub fn to_skia(self) -> tiny_skia::Color {
        tiny_skia::Color::from_rgba8(self.r(), self.g(), self.b(), self.a())
    }
    pub fn hex(self) -> String {
        format!("#{:02X}{:02X}{:02X}", self.r(), self.g(), self.b())
    }
    /// h in 0..360, s/l in 0..1
    pub fn from_hsl(h: f32, s: f32, l: f32) -> Self {
        let c = (1.0 - (2.0 * l - 1.0).abs()) * s;
        let hp = (h.rem_euclid(360.0)) / 60.0;
        let x = c * (1.0 - (hp % 2.0 - 1.0).abs());
        let (r, g, b) = match hp as u32 {
            0 => (c, x, 0.0),
            1 => (x, c, 0.0),
            2 => (0.0, c, x),
            3 => (0.0, x, c),
            4 => (x, 0.0, c),
            _ => (c, 0.0, x),
        };
        let m = l - c / 2.0;
        let f = |v: f32| ((v + m).clamp(0.0, 1.0) * 255.0).round() as u8;
        Color::from_rgba(f(r), f(g), f(b), 255)
    }
    pub fn to_hsl(self) -> (f32, f32, f32) {
        let r = self.r() as f32 / 255.0;
        let g = self.g() as f32 / 255.0;
        let b = self.b() as f32 / 255.0;
        let max = r.max(g).max(b);
        let min = r.min(g).min(b);
        let l = (max + min) / 2.0;
        if (max - min).abs() < f32::EPSILON {
            return (0.0, 0.0, l);
        }
        let d = max - min;
        let s = if l > 0.5 { d / (2.0 - max - min) } else { d / (max + min) };
        let h = if max == r {
            (g - b) / d + if g < b { 6.0 } else { 0.0 }
        } else if max == g {
            (b - r) / d + 2.0
        } else {
            (r - g) / d + 4.0
        };
        (h * 60.0, s, l)
    }
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub struct Gradient {
    pub stops: Vec<Color>,
    /// Degrees, 0 = left→right, 90 = top→bottom.
    pub angle: f32,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
pub enum Background {
    None,
    Solid(Color),
    Gradient(Gradient),
    /// A heavily blurred copy of the screenshot itself.
    Blurred,
    /// An image file, scaled to cover.
    Image(PathBuf),
}

#[derive(Clone, Copy, Debug, PartialEq, Eq, Serialize, Deserialize)]
pub enum Chrome {
    None,
    MacLight,
    MacDark,
    WinLight,
    WinDark,
}

impl Chrome {
    pub const ALL: [Chrome; 5] = [
        Chrome::None,
        Chrome::MacLight,
        Chrome::MacDark,
        Chrome::WinLight,
        Chrome::WinDark,
    ];
    pub fn label(self) -> &'static str {
        match self {
            Chrome::None => "None",
            Chrome::MacLight => "macOS",
            Chrome::MacDark => "macOS Dark",
            Chrome::WinLight => "Windows",
            Chrome::WinDark => "Win Dark",
        }
    }
    pub fn is_dark(self) -> bool {
        matches!(self, Chrome::MacDark | Chrome::WinDark)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Serialize, Deserialize)]
pub struct Aspect {
    pub w: f32,
    pub h: f32,
}

pub const ASPECTS: [(&str, Option<Aspect>); 8] = [
    ("Auto", None),
    ("1:1", Some(Aspect { w: 1.0, h: 1.0 })),
    ("4:3", Some(Aspect { w: 4.0, h: 3.0 })),
    ("3:2", Some(Aspect { w: 3.0, h: 2.0 })),
    ("16:9", Some(Aspect { w: 16.0, h: 9.0 })),
    ("16:10", Some(Aspect { w: 16.0, h: 10.0 })),
    ("4:5", Some(Aspect { w: 4.0, h: 5.0 })),
    ("9:16", Some(Aspect { w: 9.0, h: 16.0 })),
];

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct Style {
    pub background: Background,
    /// All sizes below are in points and get multiplied by the capture's DPI scale.
    pub padding: f32,
    pub inset: f32,
    /// Fill the inset with a color sampled from the screenshot edges.
    pub auto_balance: bool,
    pub radius: f32,
    pub shadow: f32,
    pub shadow_opacity: f32,
    pub aspect: Option<Aspect>,
    /// 0..=8, row-major 3x3 grid (4 = centered).
    pub align: u8,
    pub chrome: Chrome,
    pub chrome_title: bool,
}

impl Default for Style {
    fn default() -> Self {
        Self {
            background: Background::Gradient(GRADIENTS[0].1.clone()),
            padding: 64.0,
            inset: 0.0,
            auto_balance: true,
            radius: 12.0,
            shadow: 40.0,
            shadow_opacity: 0.45,
            aspect: None,
            align: 4,
            chrome: Chrome::None,
            chrome_title: true,
        }
    }
}

macro_rules! grad {
    ($angle:expr; $($c:expr),+) => {
        Gradient { stops: vec![$(Color::rgb($c)),+], angle: $angle }
    };
}

pub static GRADIENTS: std::sync::LazyLock<Vec<(&'static str, Gradient)>> =
    std::sync::LazyLock::new(|| {
        vec![
            ("Aurora", grad!(135.0; 0x6366F1, 0xA855F7, 0xEC4899)),
            ("Sunset", grad!(135.0; 0xFF7E5F, 0xFEB47B)),
            ("Ocean", grad!(135.0; 0x2193B0, 0x6DD5ED)),
            ("Mint", grad!(135.0; 0x11998E, 0x38EF7D)),
            ("Peach", grad!(135.0; 0xFFD1B3, 0xFF9A8B, 0xFF6A88)),
            ("Lavender", grad!(135.0; 0xC3B1E1, 0x9F86C0, 0x5E548E)),
            ("Midnight", grad!(135.0; 0x0F2027, 0x203A43, 0x2C5364)),
            ("Candy", grad!(135.0; 0xF9A8D4, 0xC4B5FD, 0x93C5FD)),
            ("Citrus", grad!(135.0; 0xF7971E, 0xFFD200)),
            ("Forest", grad!(135.0; 0x134E5E, 0x71B280)),
            ("Rose", grad!(135.0; 0xF43F5E, 0xFB7185, 0xFDA4AF)),
            ("Sky", grad!(180.0; 0x89F7FE, 0x66A6FF)),
            ("Ember", grad!(135.0; 0x200122, 0x6F0000)),
            ("Cosmic", grad!(135.0; 0x1A2A6C, 0xB21F1F, 0xFDBB2D)),
            ("Slate", grad!(135.0; 0x334155, 0x0F172A)),
            ("Paper", grad!(135.0; 0xF5F7FA, 0xC3CFE2)),
            ("Lime", grad!(135.0; 0xD4FC79, 0x96E6A1)),
            ("Grape", grad!(135.0; 0x8E2DE2, 0x4A00E0)),
        ]
    });

pub const SOLIDS: [u32; 12] = [
    0xFFFFFF, 0xF4F4F5, 0xD4D4D8, 0x71717A, 0x27272A, 0x000000, 0xFDE68A, 0xFCA5A5, 0x93C5FD,
    0x86EFAC, 0xC4B5FD, 0xF9A8D4,
];

/// Annotation palette.
pub const INK: [u32; 10] = [
    0xFF3B30, 0xFF9500, 0xFFCC00, 0x34C759, 0x00C7BE, 0x007AFF, 0x5856D6, 0xAF52DE, 0xFFFFFF,
    0x1C1C1E,
];
