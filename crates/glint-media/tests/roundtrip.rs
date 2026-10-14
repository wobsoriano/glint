//! Writer → reader round trip with synthetic frames and an audio tone.

use glint_media::{
    available_codecs, decodable_codecs, read_mp4_audio, transcode, AudioParams, TranscodeOptions, VideoCodec, VideoParams,
    VideoReader, VideoWriter,
};
use image::{Rgba, RgbaImage};
use std::path::PathBuf;
use std::sync::atomic::AtomicBool;

const W: u32 = 320;
const H: u32 = 240;
const FPS: u32 = 30;
const FRAMES: u32 = 60;
const RATE: u32 = 48000;

fn temp(name: &str) -> PathBuf {
    let dir = std::env::temp_dir().join("glint-media-roundtrip");
    std::fs::create_dir_all(&dir).unwrap();
    dir.join(name)
}

fn frame(i: u32) -> RgbaImage {
    let (top, bottom) = if i < FRAMES / 2 { ([255, 0, 0], [0, 0, 255]) } else { ([0, 255, 0], [255, 255, 0]) };
    RgbaImage::from_fn(W, H, |_, y| {
        let c = if y < H / 2 { top } else { bottom };
        Rgba([c[0], c[1], c[2], 255])
    })
}

fn tone(seconds: f32) -> Vec<f32> {
    (0..(RATE as f32 * seconds) as usize)
        .map(|i| (i as f32 / RATE as f32 * 440.0 * std::f32::consts::TAU).sin() * 0.5)
        .collect()
}

fn assert_close(actual: Rgba<u8>, expected: [u8; 3], what: &str) {
    for c in 0..3 {
        let d = (actual[c] as i32 - expected[c] as i32).abs();
        assert!(d <= 24, "{what}: channel {c} is {} but expected {} (frame {actual:?})", actual[c], expected[c]);
    }
}

fn zero_crossings_per_second(samples: &[f32], rate: u32) -> f32 {
    let crossings = samples.windows(2).filter(|w| (w[0] < 0.0) != (w[1] < 0.0)).count();
    crossings as f32 * rate as f32 / samples.len() as f32
}

fn write_sample_file(path: &PathBuf, codec: VideoCodec) {
    let video = VideoParams { width: W, height: H, fps: FPS, bitrate: 2_000_000, codec };
    let mut w = VideoWriter::create(path, video, Some(AudioParams { sample_rate: RATE, channels: 1 })).unwrap();
    w.write_audio(&tone(2.0)).unwrap();
    for i in 0..FRAMES {
        w.write_frame(&frame(i)).unwrap();
    }
    let summary = w.finish().unwrap();
    assert_eq!(summary.frames, FRAMES as u64);
    assert_eq!(summary.duration_ms, 2000);
    assert!(summary.bytes > 10_000, "file is only {} bytes", summary.bytes);
}

#[test]
fn h264_frames_and_tone_survive() {
    let path = temp("rt_h264.mp4");
    write_sample_file(&path, VideoCodec::H264);

    let mut r = VideoReader::open(&path).unwrap();
    assert_eq!(r.size(), (W, H));
    assert!((r.fps() - 30.0).abs() < 0.01, "fps {}", r.fps());
    assert!((1950..=2050).contains(&r.duration_ms()), "duration {}", r.duration_ms());

    let first = r.next_frame().unwrap().unwrap();
    assert_eq!(first.t_ms, 0);
    assert_close(*first.image.get_pixel(10, 10), [255, 0, 0], "frame 0 top");
    assert_close(*first.image.get_pixel(300, 230), [0, 0, 255], "frame 0 bottom");

    let mut count = 1;
    while r.next_frame().unwrap().is_some() {
        count += 1;
    }
    assert_eq!(count, FRAMES);

    r.seek(1500).unwrap();
    let late = r.next_frame().unwrap().unwrap();
    assert!((1450..=1550).contains(&late.t_ms), "seek landed at {}", late.t_ms);
    assert_close(*late.image.get_pixel(10, 10), [0, 255, 0], "frame at 1.5s top");
    assert_close(*late.image.get_pixel(300, 230), [255, 255, 0], "frame at 1.5s bottom");

    let thumbs = r.thumbnails(4).unwrap();
    let times: Vec<u64> = thumbs.iter().map(|t| t.t_ms).collect();
    assert_eq!(times, vec![0, 500, 1000, 1500]);
    assert_close(*thumbs[1].image.get_pixel(10, 10), [255, 0, 0], "thumb 1");
    assert_close(*thumbs[3].image.get_pixel(10, 10), [0, 255, 0], "thumb 3");

    let audio = read_mp4_audio(&path).unwrap().expect("audio track");
    assert_eq!((audio.sample_rate, audio.channels), (RATE, 1));
    assert!((1900..=2100).contains(&audio.duration_ms()), "audio duration {}", audio.duration_ms());
    let body = &audio.samples[RATE as usize / 4..RATE as usize * 7 / 4];
    let rms = (body.iter().map(|s| s * s).sum::<f32>() / body.len() as f32).sqrt();
    assert!((0.25..=0.45).contains(&rms), "tone rms {rms} (expected ~0.35)");
    let hz = zero_crossings_per_second(body, RATE) / 2.0;
    assert!((430.0..=450.0).contains(&hz), "tone frequency {hz}");
}

#[test]
fn hevc_round_trip_when_an_encoder_exists() {
    let codecs = available_codecs();
    assert!(codecs.contains(&VideoCodec::H264), "H.264 must always be available: {codecs:?}");
    if !codecs.contains(&VideoCodec::Hevc) {
        eprintln!("no HEVC encoder on this machine, skipping");
        return;
    }
    let path = temp("rt_hevc.mp4");
    write_sample_file(&path, VideoCodec::Hevc);
    if !decodable_codecs().contains(&VideoCodec::Hevc) {
        eprintln!("HEVC encoded ({} bytes) but this machine has no HEVC decoder, skipping read-back", std::fs::metadata(&path).unwrap().len());
        return;
    }
    let mut r = VideoReader::open(&path).unwrap();
    assert_eq!(r.size(), (W, H));
    let first = r.next_frame().unwrap().unwrap();
    assert_close(*first.image.get_pixel(10, 10), [255, 0, 0], "hevc frame 0 top");
    assert_close(*first.image.get_pixel(300, 230), [0, 0, 255], "hevc frame 0 bottom");
}

#[test]
fn transcode_trims_and_resizes() {
    let src = temp("tc_src.mp4");
    write_sample_file(&src, VideoCodec::H264);
    let dst = temp("tc_dst.mp4");
    let opts = TranscodeOptions { size: Some((160, 120)), bitrate: Some(500_000), codec: None, trim: Some((1000, 2000)) };
    let mut last = 0.0;
    let summary = transcode(&src, &dst, &opts, &AtomicBool::new(false), |p| last = p).unwrap();
    assert_eq!(last, 1.0);
    assert_eq!(summary.frames, 30);
    let mut r = VideoReader::open(&dst).unwrap();
    assert_eq!(r.size(), (160, 120));
    assert!((950..=1050).contains(&r.duration_ms()), "trimmed duration {}", r.duration_ms());
    let first = r.next_frame().unwrap().unwrap();
    assert_close(*first.image.get_pixel(5, 5), [0, 255, 0], "trimmed first frame is the green half");
    assert_close(*first.image.get_pixel(150, 110), [255, 255, 0], "trimmed first frame bottom");
    let audio = read_mp4_audio(&dst).unwrap().expect("audio carried over");
    assert!((900..=1100).contains(&audio.duration_ms()), "trimmed audio {}", audio.duration_ms());
}
