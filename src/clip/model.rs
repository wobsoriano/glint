use glint_media::{TranscodeOptions, VideoCodec};
use std::path::{Path, PathBuf};

pub const VIDEO_EXTENSIONS: [&str; 5] = ["mp4", "mov", "m4v", "wmv", "avi"];

pub fn is_video(path: &Path) -> bool {
    path.extension()
        .and_then(|e| e.to_str())
        .is_some_and(|e| VIDEO_EXTENSIONS.iter().any(|v| v.eq_ignore_ascii_case(e)))
}

#[derive(Clone, Debug, PartialEq)]
pub struct Source {
    pub path: PathBuf,
    pub size: (u32, u32),
    pub fps: u32,
    pub duration_ms: u64,
    pub bytes: u64,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Trim {
    pub start_ms: u64,
    pub end_ms: u64,
}

impl Trim {
    pub const MIN_MS: u64 = 250;

    pub fn full(duration_ms: u64) -> Self {
        Self { start_ms: 0, end_ms: duration_ms }
    }

    pub fn duration_ms(self) -> u64 {
        self.end_ms - self.start_ms
    }

    pub fn with_start(self, ms: u64) -> Self {
        Self { start_ms: ms.min(self.end_ms.saturating_sub(Self::MIN_MS)), ..self }
    }

    pub fn with_end(self, ms: u64, duration_ms: u64) -> Self {
        Self { end_ms: ms.clamp(self.start_ms + Self::MIN_MS, duration_ms.max(self.start_ms + Self::MIN_MS)), ..self }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Resolution {
    Original,
    P1080,
    P720,
    P480,
}

impl Resolution {
    pub const ALL: [Resolution; 4] = [Resolution::Original, Resolution::P1080, Resolution::P720, Resolution::P480];

    pub fn label(self) -> &'static str {
        match self {
            Resolution::Original => "Original",
            Resolution::P1080 => "1080p",
            Resolution::P720 => "720p",
            Resolution::P480 => "480p",
        }
    }

    fn short_side(self) -> Option<u32> {
        match self {
            Resolution::Original => None,
            Resolution::P1080 => Some(1080),
            Resolution::P720 => Some(720),
            Resolution::P480 => Some(480),
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Quality {
    High,
    Medium,
    Small,
}

impl Quality {
    pub const ALL: [Quality; 3] = [Quality::High, Quality::Medium, Quality::Small];

    pub fn label(self) -> &'static str {
        match self {
            Quality::High => "High",
            Quality::Medium => "Medium",
            Quality::Small => "Small",
        }
    }

    fn studio(self) -> glint_studio::Quality {
        match self {
            Quality::High => glint_studio::Quality::High,
            Quality::Medium => glint_studio::Quality::Medium,
            Quality::Small => glint_studio::Quality::Low,
        }
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Options {
    pub resolution: Resolution,
    pub quality: Quality,
    pub codec: VideoCodec,
    pub keep_audio: bool,
}

impl Default for Options {
    fn default() -> Self {
        Self { resolution: Resolution::Original, quality: Quality::Medium, codec: VideoCodec::H264, keep_audio: true }
    }
}

pub fn output_size((w, h): (u32, u32), resolution: Resolution) -> (u32, u32) {
    match resolution.short_side() {
        Some(s) if s < w.min(h) => {
            let k = s as f64 / w.min(h) as f64;
            (((w as f64 * k).round() as u32) & !1, ((h as f64 * k).round() as u32) & !1)
        }
        _ => (w & !1, h & !1),
    }
}

fn video_bitrate(source: &Source, opts: &Options) -> u32 {
    let (w, h) = output_size(source.size, opts.resolution);
    glint_studio::bitrate(w, h, source.fps, opts.quality.studio())
}

pub fn estimate_bytes(source: &Source, trim: Trim, opts: &Options, has_audio: bool) -> u64 {
    let secs = trim.duration_ms() as f64 / 1000.0;
    let video = video_bitrate(source, opts) as f64 / 8.0 * secs;
    let audio = if has_audio && opts.keep_audio { glint_media::AAC_BYTES_PER_SEC as f64 * secs } else { 0.0 };
    (video + audio) as u64
}

pub fn transcode_options(source: &Source, trim: Trim, opts: &Options) -> TranscodeOptions {
    TranscodeOptions {
        size: Some(output_size(source.size, opts.resolution)),
        bitrate: Some(video_bitrate(source, opts)),
        codec: Some(opts.codec),
        trim: Some((trim.start_ms, trim.end_ms)),
        drop_audio: !opts.keep_audio,
    }
}

pub fn output_stem(source: &Path) -> String {
    let stem = source.file_stem().map(|s| s.to_string_lossy().into_owned()).unwrap_or_else(|| "Video".into());
    format!("{stem} (trimmed)")
}

pub fn clock(ms: u64) -> String {
    let s = ms / 1000;
    if s >= 3600 {
        format!("{}:{:02}:{:02}", s / 3600, (s / 60) % 60, s % 60)
    } else {
        format!("{}:{:02}.{}", s / 60, s % 60, (ms % 1000) / 100)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn source(w: u32, h: u32) -> Source {
        Source { path: PathBuf::from(r"C:\v\clip.mp4"), size: (w, h), fps: 30, duration_ms: 10_000, bytes: 50_000_000 }
    }

    #[test]
    fn recognizes_video_extensions_case_insensitively() {
        assert!(is_video(Path::new(r"C:\a\b.MP4")));
        assert!(is_video(Path::new("b.mov")));
        assert!(is_video(Path::new("b.wmv")));
        assert!(!is_video(Path::new("b.png")));
        assert!(!is_video(Path::new("mp4")));
    }

    #[test]
    fn output_size_shrinks_the_short_side_and_never_upscales() {
        assert_eq!(output_size((3840, 2160), Resolution::P720), (1280, 720));
        assert_eq!(output_size((1080, 1920), Resolution::P720), (720, 1280));
        assert_eq!(output_size((2560, 1080), Resolution::P480), (1138, 480));
        assert_eq!(output_size((1280, 720), Resolution::P1080), (1280, 720));
        assert_eq!(output_size((1281, 721), Resolution::Original), (1280, 720));
    }

    #[test]
    fn trim_handles_keep_a_minimum_span_inside_the_source() {
        let t = Trim::full(10_000);
        assert_eq!(t.with_start(9_900), Trim { start_ms: 9_750, end_ms: 10_000 });
        assert_eq!(t.with_end(100, 10_000), Trim { start_ms: 0, end_ms: 250 });
        assert_eq!(t.with_end(20_000, 10_000), Trim { start_ms: 0, end_ms: 10_000 });
        let mid = t.with_start(4_000).with_end(6_000, 10_000);
        assert_eq!((mid, mid.duration_ms()), (Trim { start_ms: 4_000, end_ms: 6_000 }, 2_000));
    }

    #[test]
    fn transcode_options_follow_the_settings() {
        let s = source(3840, 2160);
        let trim = Trim { start_ms: 3_000, end_ms: 5_000 };
        let opts = Options { resolution: Resolution::P720, quality: Quality::Small, codec: VideoCodec::Hevc, keep_audio: false };
        let t = transcode_options(&s, trim, &opts);
        assert_eq!(t.size, Some((1280, 720)));
        assert_eq!(t.bitrate, Some(1_500_000));
        assert_eq!(t.codec, Some(VideoCodec::Hevc));
        assert_eq!(t.trim, Some((3_000, 5_000)));
        assert!(t.drop_audio);
    }

    #[test]
    fn estimate_scales_with_quality_span_and_audio() {
        let s = source(1920, 1080);
        let two = Trim { start_ms: 0, end_ms: 2_000 };
        let high = Options { quality: Quality::High, ..Default::default() };
        assert_eq!(estimate_bytes(&s, two, &high, true), 1_555_200 + 48_000);
        assert_eq!(estimate_bytes(&s, two, &Options { keep_audio: false, ..high }, true), 1_555_200);
        assert_eq!(estimate_bytes(&s, two, &high, false), 1_555_200);
        let small = Options { quality: Quality::Small, ..high };
        assert!(estimate_bytes(&s, two, &small, false) < estimate_bytes(&s, two, &high, false));
    }

    #[test]
    fn names_and_clock() {
        assert_eq!(output_stem(Path::new(r"C:\v\My clip.mov")), "My clip (trimmed)");
        assert_eq!(clock(0), "0:00.0");
        assert_eq!(clock(65_432), "1:05.4");
        assert_eq!(clock(3_725_000), "1:02:05");
    }
}
