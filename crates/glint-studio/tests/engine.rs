//! Session loading and export against a small generated recording: a 3 s screen master whose
//! frames get redder over time, plus a 440 Hz system-audio tone.

use glint_media::{read_mp4_audio, VideoCodec, VideoParams, VideoReader, VideoWriter};
use glint_studio::{export, Clip, Format, Fps, Project, Session};
use std::path::{Path, PathBuf};
use std::sync::atomic::AtomicBool;
use std::time::Instant;

fn make_session(name: &str) -> PathBuf {
    let dir = std::env::temp_dir().join(format!("glint-studio-engine-{name}-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&dir);
    std::fs::create_dir_all(&dir).unwrap();
    let params = VideoParams { width: 320, height: 180, fps: 30, bitrate: 4_000_000, codec: VideoCodec::H264 };
    let mut w = VideoWriter::create(&dir.join("screen.mp4"), params, None).unwrap();
    for i in 0..90u32 {
        w.write_frame(&image::RgbaImage::from_pixel(320, 180, image::Rgba([(i * 2) as u8, 128, 64, 255]))).unwrap();
    }
    w.finish().unwrap();
    write_tone_wav(&dir.join("system.wav"), 3.0);
    dir
}

fn write_tone_wav(path: &Path, seconds: f32) {
    let frames = (48_000.0 * seconds) as usize;
    let mut data = Vec::with_capacity(frames * 8);
    for i in 0..frames {
        let s = (i as f32 * 440.0 * std::f32::consts::TAU / 48_000.0).sin() * 0.5;
        data.extend_from_slice(&s.to_le_bytes());
        data.extend_from_slice(&s.to_le_bytes());
    }
    let mut b = Vec::new();
    b.extend_from_slice(b"RIFF");
    b.extend_from_slice(&(36 + data.len() as u32).to_le_bytes());
    b.extend_from_slice(b"WAVEfmt ");
    b.extend_from_slice(&16u32.to_le_bytes());
    b.extend_from_slice(&3u16.to_le_bytes());
    b.extend_from_slice(&2u16.to_le_bytes());
    b.extend_from_slice(&48_000u32.to_le_bytes());
    b.extend_from_slice(&(48_000u32 * 8).to_le_bytes());
    b.extend_from_slice(&8u16.to_le_bytes());
    b.extend_from_slice(&32u16.to_le_bytes());
    b.extend_from_slice(b"data");
    b.extend_from_slice(&(data.len() as u32).to_le_bytes());
    b.extend_from_slice(&data);
    std::fs::write(path, b).unwrap();
}

/// Red channel at the canvas centre of each frame, which sits inside the screen card.
fn centre_reds(path: &Path) -> Vec<u8> {
    let mut r = VideoReader::open(path).unwrap();
    let mut out = Vec::new();
    while let Some(f) = r.next_frame().unwrap() {
        out.push(f.image.get_pixel(f.image.width() / 2, f.image.height() / 2)[0]);
    }
    out
}

#[test]
fn opens_a_session_without_project_or_input() {
    let dir = make_session("open");
    let s = Session::open(&dir).unwrap();
    assert_eq!(s.source_size, (320, 180));
    assert!((s.source_duration_ms - 3000.0).abs() <= 34.0, "{}", s.source_duration_ms);
    assert_eq!(s.project.clips, vec![Clip { start_ms: 0.0, end_ms: s.source_duration_ms, speed: 1.0 }]);
    assert!(s.project.zooms.is_empty(), "no clicks, no auto zooms");
    assert_eq!((s.tracks.camera.is_some(), s.tracks.mic.is_some(), s.tracks.system.is_some()), (false, false, true));
    assert!(s.transcript.is_none());
    std::fs::remove_dir_all(&dir).unwrap();
}

#[test]
fn export_follows_the_clip_map_and_caches() {
    let dir = make_session("export");
    let mut s = Session::open(&dir).unwrap();
    s.project.clips = vec![
        Clip { start_ms: 0.0, end_ms: 1000.0, speed: 1.0 },
        Clip { start_ms: 2000.0, end_ms: 3000.0, speed: 2.0 },
    ];
    s.project.design.padding = 0.1;
    let mut opts = s.project.export;
    opts.fps = Fps::Fps30;
    opts.motion_blur = false;
    let mut last = 0.0;
    let path = export(&s, &opts, |p| last = p, &AtomicBool::new(false)).unwrap();
    assert_eq!(last, 1.0);

    let reds = centre_reds(&path);
    let source = centre_reds(&dir.join("screen.mp4"));
    assert_eq!(reds.len(), 45, "1000 ms + 1000 ms at 2x = 1.5 s at 30 fps");
    for (out_frame, source_frame) in [(0, 0), (15, 15), (29, 29), (30, 60), (36, 72), (44, 88)] {
        let (got, want) = (reds[out_frame] as i32, source[source_frame] as i32);
        assert!((got - want).abs() <= 4, "output frame {out_frame} shows red {got}, source frame {source_frame} has {want}");
    }
    let audio = read_mp4_audio(&path).unwrap().expect("system audio is mixed in");
    assert_eq!((audio.sample_rate, audio.channels), (48_000, 2));
    assert!((audio.duration_ms() as i64 - 1500).abs() < 60, "audio lasts {} ms", audio.duration_ms());
    let body = &audio.samples[48_000..96_000];
    let rms = (body.iter().map(|s| s * s).sum::<f32>() / body.len() as f32).sqrt();
    assert!((rms - 0.35).abs() < 0.05, "the 0.5 tone survives the 2x stretch at full level: rms {rms}");

    let started = Instant::now();
    assert_eq!(export(&s, &opts, |_| {}, &AtomicBool::new(false)).unwrap(), path);
    assert!(started.elapsed().as_millis() < 50, "unchanged export is a cache hit");

    opts.format = Format::Gif;
    let err = export(&s, &opts, |_| {}, &AtomicBool::new(true)).unwrap_err();
    assert!(err.to_string().contains("cancelled"), "{err:#}");
    let names: Vec<String> = std::fs::read_dir(s.render_dir()).unwrap().map(|e| e.unwrap().file_name().to_string_lossy().into_owned()).collect();
    assert_eq!(names, vec![path.file_name().unwrap().to_string_lossy().into_owned()], "the cancelled render left nothing behind");

    opts.format = Format::Gif;
    let gif = export(&s, &opts, |_| {}, &AtomicBool::new(false)).unwrap();
    let bytes = std::fs::read(&gif).unwrap();
    assert_eq!(&bytes[..6], b"GIF89a");
    std::fs::remove_dir_all(&dir).unwrap();
}

/// Writes `bytes` as `file` in a fresh session that already holds an older `<file>.broken`,
/// opens it, and returns the session, its directory and what `<file>.broken` holds afterwards.
fn open_with_broken(name: &str, file: &str, bytes: &[u8]) -> (Session, PathBuf, Vec<u8>) {
    let dir = make_session(name);
    let broken = dir.join(format!("{file}.broken"));
    std::fs::write(&broken, b"an older broken copy").unwrap();
    std::fs::write(dir.join(file), bytes).unwrap();
    let s = Session::open(&dir).unwrap();
    assert!(!dir.join(file).exists(), "the bad {file} was moved aside");
    let kept = std::fs::read(&broken).unwrap();
    (s, dir, kept)
}

#[test]
fn unparsable_project_is_set_aside() {
    let (s, dir, kept) = open_with_broken("bad-project", "project.json", b"{ not json");
    assert_eq!(kept, b"{ not json");
    assert_eq!(s.notices, vec!["project.json could not be read (key must be a string at line 1 column 3), so it was renamed project.json.broken and defaults are used"]);
    assert_eq!(s.project, Project::new(s.source_duration_ms));
    std::fs::remove_dir_all(&dir).unwrap();
}

#[test]
fn project_from_a_newer_version_is_set_aside() {
    let json = br#"{ "version": 99, "clips": [{ "start_ms": 0, "end_ms": 1000 }] }"#;
    let (s, dir, kept) = open_with_broken("new-project", "project.json", json);
    assert_eq!(kept, json);
    assert_eq!(s.notices, vec!["project.json could not be read (project version 99 is newer than supported 1), so it was renamed project.json.broken and defaults are used"]);
    assert_eq!(s.project, Project::new(s.source_duration_ms));
    std::fs::remove_dir_all(&dir).unwrap();
}

#[test]
fn unparsable_transcript_is_set_aside() {
    let (s, dir, kept) = open_with_broken("bad-transcript", "transcript.json", b"{ not json");
    assert_eq!(kept, b"{ not json");
    assert_eq!(s.notices, vec!["transcript.json could not be read (key must be a string at line 1 column 3), so it was renamed transcript.json.broken and defaults are used"]);
    assert!(s.transcript.is_none());
    std::fs::remove_dir_all(&dir).unwrap();
}

#[test]
fn transcript_from_a_newer_version_is_set_aside() {
    let json = br#"{ "version": 99, "language": "en", "words": [] }"#;
    let (s, dir, kept) = open_with_broken("new-transcript", "transcript.json", json);
    assert_eq!(kept, json);
    assert_eq!(s.notices, vec!["transcript.json could not be read (transcript version 99 is newer than supported 1), so it was renamed transcript.json.broken and defaults are used"]);
    assert!(s.transcript.is_none());
    std::fs::remove_dir_all(&dir).unwrap();
}
