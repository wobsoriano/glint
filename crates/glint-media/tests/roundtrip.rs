use glint_media::{
    available_codecs, decodable_codecs, read_mp4_audio, transcode, AudioParams, FrameReader, TranscodeOptions, VideoCodec,
    VideoParams, VideoReader, VideoWriter,
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
    let opts = TranscodeOptions { size: Some((160, 120)), bitrate: Some(500_000), codec: None, trim: Some((1000, 2000)), drop_audio: false };
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

#[test]
fn transcode_can_drop_audio() {
    let src = temp("tc_mute_src.mp4");
    write_sample_file(&src, VideoCodec::H264);
    let dst = temp("tc_mute_dst.mp4");
    let opts = TranscodeOptions { drop_audio: true, ..Default::default() };
    transcode(&src, &dst, &opts, &AtomicBool::new(false), |_| {}).unwrap();
    assert!(read_mp4_audio(&src).unwrap().is_some(), "source has audio");
    assert!(read_mp4_audio(&dst).unwrap().is_none(), "audio dropped");
    assert_eq!(VideoReader::open(&dst).unwrap().size(), (W, H));
}

fn stepped_tone_44k(seconds: f32) -> Vec<f32> {
    (0..(44_100.0 * seconds) as usize)
        .flat_map(|i| {
            let amp = if i < 44_100 { 0.5 } else { 0.1 };
            let v = (i as f32 / 44_100.0 * 440.0 * std::f32::consts::TAU).sin() * amp;
            [v, v]
        })
        .collect()
}

fn envelope(samples: &[f32], channels: usize, above: f32, below: f32) -> (Option<usize>, Option<usize>) {
    let rms = |w: &[f32]| (w.iter().map(|s| s * s).sum::<f32>() / w.len() as f32).sqrt();
    let windows: Vec<f32> = samples.chunks_exact(64 * channels).map(rms).collect();
    let start = windows.iter().position(|&r| r > above);
    let step = start.and_then(|s| windows[s..].iter().position(|&r| r < below).map(|i| s + i));
    (start.map(|i| i * 64), step.map(|i| i * 64))
}

fn write_stepped_file(path: &PathBuf) {
    let video = VideoParams { width: W, height: H, fps: FPS, bitrate: 2_000_000, codec: VideoCodec::H264 };
    let mut w = VideoWriter::create(path, video, Some(AudioParams { sample_rate: 44_100, channels: 2 })).unwrap();
    w.write_audio(&stepped_tone_44k(2.0)).unwrap();
    for i in 0..FRAMES {
        w.write_frame(&frame(i)).unwrap();
    }
    w.finish().unwrap();
}

/// Silence after `samples`, so a tone that runs to the very end of a decode still shows where
/// it stops. Decoders differ there: Media Foundation pads AAC to whole 1024-frame blocks,
/// AVFoundation trims the track to the written frames.
fn with_tail(samples: &[f32], channels: usize) -> Vec<f32> {
    let mut v = samples.to_vec();
    v.resize(v.len() + 1024 * channels, 0.0);
    v
}

/// The source's step from loud to quiet sits at frame 44100, and a 1000..2000 ms trim at
/// 44.1 kHz keeps exactly the quiet second, so its content must land on the source's frames
/// shifted by 44100: it starts where the source's tone starts (decoders may fade in the first
/// block) and stops where the source steps.
#[test]
fn transcode_keeps_audio_aligned_at_44100_hz() {
    let src = temp("align_src.mp4");
    write_stepped_file(&src);
    let whole = read_mp4_audio(&src).unwrap().unwrap();
    assert_eq!((whole.sample_rate, whole.channels), (44_100, 2));
    assert!((88_200..88_200 + 1024).contains(&whole.frames()), "two seconds within one AAC block: {}", whole.frames());
    let (start, step) = envelope(&with_tail(&whole.samples, 2), 2, 0.3, 0.2);
    let (start, step) = (start.expect("source tone"), step.expect("source step"));
    assert!(start < 1024, "source tone starts in the first AAC block, at {start}");
    assert_eq!(step, 44_096, "source steps in the 64-frame window that holds frame 44100");

    let dst = temp("align_dst.mp4");
    let opts = TranscodeOptions { trim: Some((1000, 2000)), ..Default::default() };
    transcode(&src, &dst, &opts, &AtomicBool::new(false), |_| {}).unwrap();
    let out = read_mp4_audio(&dst).unwrap().unwrap();
    assert!((44_100..44_100 + 1024).contains(&out.frames()), "one second within one AAC block: {}", out.frames());
    assert_eq!(envelope(&out.samples, 2, 0.2, 0.0).0, None, "no loud audio from before the trim start");
    assert_eq!(
        envelope(&with_tail(&out.samples, 2), 2, 0.05, 0.03),
        (Some(start), Some(step)),
        "quiet tone starts and ends on the source's frames"
    );
}

/// AVFoundation's AAC decode can differ by an ulp between two decodes of the same file, so
/// samples compare within 1e-6. A one-frame slip of the quiet tone is off by about 6e-3.
#[test]
fn audio_reader_seeks_to_the_exact_frame() {
    let src = temp("seek_src.mp4");
    write_stepped_file(&src);
    let whole = read_mp4_audio(&src).unwrap().unwrap();
    let mut r = glint_media::AudioReader::open(&src).unwrap().unwrap();
    assert_eq!((r.sample_rate(), r.channels()), (44_100, 2));
    r.seek(1000).unwrap();
    let mut chunks = Vec::new();
    assert!(r.next_chunk(&mut chunks).unwrap());
    let first = chunks.len();
    assert!(first > 0 && first % 2 == 0, "first chunk holds whole frames: {first} samples");
    assert!(r.next_chunk(&mut chunks).unwrap());
    assert!(chunks.len() > first, "second chunk is not empty");
    let off = chunks.iter().zip(&whole.samples[88_200..88_200 + chunks.len()]).map(|(a, b)| (a - b).abs()).fold(0.0, f32::max);
    assert!(off <= 1e-6, "seeked chunks equal the continuous decode from frame 44100, off by {off}");
}

const BLOCKS: [[u8; 3]; 8] =
    [[255, 0, 0], [0, 255, 0], [0, 0, 255], [255, 255, 255], [128, 128, 128], [255, 255, 0], [224, 172, 105], [30, 60, 90]];

fn colour_blocks(w: u32, h: u32) -> RgbaImage {
    RgbaImage::from_fn(w, h, |x, y| {
        let c = BLOCKS[(x * 4 / w) as usize + if y < h / 2 { 0 } else { 4 }];
        Rgba([c[0], c[1], c[2], 255])
    })
}

fn assert_blocks_survive(img: &RgbaImage, what: &str) {
    let (w, h) = img.dimensions();
    for (i, c) in BLOCKS.iter().enumerate() {
        let (cx, cy) = ((i % 4) as u32 * w / 4 + w / 8, if i < 4 { h / 4 } else { 3 * h / 4 });
        let mut mean = [0i32; 3];
        for y in cy - 4..cy + 4 {
            for x in cx - 4..cx + 4 {
                let p = img.get_pixel(x, y);
                for k in 0..3 {
                    mean[k] += p[k] as i32;
                }
            }
        }
        let mean = mean.map(|v| v / 64);
        for k in 0..3 {
            let d = (mean[k] - c[k] as i32).abs();
            assert!(d <= 8, "{what}: block {i} {c:?} came back as {mean:?}, channel {k} off by {d}");
        }
    }
}

#[test]
fn colours_survive_the_round_trip_at_sd_and_hd() {
    for (w, h) in [(640, 480), (1920, 1080)] {
        let path = temp(&format!("colour_{w}x{h}.mp4"));
        let video = VideoParams { width: w, height: h, fps: 30, bitrate: 20_000_000, codec: VideoCodec::H264 };
        let mut writer = VideoWriter::create(&path, video, None).unwrap();
        let frame = colour_blocks(w, h);
        for _ in 0..10 {
            writer.write_frame(&frame).unwrap();
        }
        writer.finish().unwrap();
        let decoded = VideoReader::open(&path).unwrap().next_frame().unwrap().unwrap().image;
        assert_blocks_survive(&decoded, &format!("{w}x{h} through VideoReader"));
        let streamed = FrameReader::open(&path).unwrap().next_frame().unwrap().unwrap().to_rgba(Vec::new());
        assert_blocks_survive(&streamed, &format!("{w}x{h} through FrameReader"));
    }
}

fn text_page(w: u32, h: u32) -> RgbaImage {
    let mut seed = 0x9e37_79b9_7f4a_7c15u64;
    let mut next = move || {
        seed ^= seed << 13;
        seed ^= seed >> 7;
        seed ^= seed << 17;
        seed
    };
    RgbaImage::from_fn(w, h * 2, |x, y| {
        let ink = y % 24 < 12 && x % 3 != 2 && next() % 5 < 2;
        let v = if ink { (next() % 90) as u8 } else { 255 };
        Rgba([v, v, v, 255])
    })
}

#[test]
fn sixty_fps_lands_on_the_requested_bitrate() {
    const BITRATE: u32 = 4_000_000;
    let (w, h) = (1280u32, 720u32);
    let page = text_page(w, h);
    for fps in [30u32, 60] {
        let path = temp(&format!("bitrate_{fps}.mp4"));
        let video = VideoParams { width: w, height: h, fps, bitrate: BITRATE, codec: VideoCodec::H264 };
        let mut writer = VideoWriter::create(&path, video, None).unwrap();
        let mut frame = RgbaImage::new(w, h);
        let row = (w * 4) as usize;
        for i in 0..fps * 3 {
            let off = i * 180 / fps % h;
            for y in 0..h as usize {
                let src = (y + off as usize) % (2 * h as usize);
                frame.as_mut()[y * row..(y + 1) * row].copy_from_slice(&page.as_raw()[src * row..(src + 1) * row]);
            }
            writer.write_frame(&frame).unwrap();
        }
        let s = writer.finish().unwrap();
        let measured = s.bytes as f64 * 8.0 / (s.duration_ms as f64 / 1000.0);
        let ratio = measured / BITRATE as f64;
        assert!(
            (0.8..=1.4).contains(&ratio),
            "{fps} fps: {:.2} Mbps for {:.2} requested ({ratio:.2}x), hardware {}",
            measured / 1e6,
            BITRATE as f64 / 1e6,
            s.hardware
        );
    }
}
