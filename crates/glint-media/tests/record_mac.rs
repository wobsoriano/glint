#![cfg(target_os = "macos")]

use glint_media::{read_wav, CaptureSource, Fps, RecordOptions, Recorder, VideoReader};
use std::time::Duration;

/// Plays a system sound during the recording so `system.wav` has something to hold.
#[test]
#[ignore = "needs the Screen Recording permission; Input Monitoring is optional"]
fn records_two_seconds_of_display_0_with_input_and_system_audio() {
    let dir = std::env::temp_dir().join("glint-media-record-mac");
    let _ = std::fs::remove_dir_all(&dir);
    let rec = Recorder::start(RecordOptions {
        source: CaptureSource::Display(0),
        session_dir: dir.clone(),
        fps: Fps::F30,
        system_audio: true,
        mic: None,
        camera: None,
        record_input: true,
        mic_tap: None,
        ignored_keys: Vec::new(),
    })
    .unwrap();
    let sound = std::process::Command::new("afplay").arg("/System/Library/Sounds/Submarine.aiff").spawn();
    std::thread::sleep(Duration::from_secs(2));
    let summary = rec.stop();
    if let Ok(mut sound) = sound {
        let _ = sound.wait();
    }
    eprintln!("{summary:#?}");

    assert_eq!(summary.files[0], dir.join("screen.mp4"));
    let mut reader = VideoReader::open(&dir.join("screen.mp4")).unwrap();
    let mut frames = 0u64;
    while reader.next_frame().unwrap().is_some() {
        frames += 1;
    }
    eprintln!("screen.mp4: {frames} frames, {} ms", reader.duration_ms());
    assert!((58..=62).contains(&frames), "{frames} frames in 2 s at 30 fps");
    assert_eq!(reader.duration_ms(), frames * 1000 / 30);
    assert!(summary.duration_ms.abs_diff(reader.duration_ms()) <= 34, "media {} ms", summary.duration_ms);

    let wav = read_wav(&dir.join("system.wav")).unwrap();
    let wav_ms = wav.frames() as u64 * 1000 / 48_000;
    let peak = wav.samples.iter().fold(0f32, |m, s| m.max(s.abs()));
    eprintln!("system.wav: {wav_ms} ms, {} channels, peak {peak}", wav.channels);
    assert!(wav_ms.abs_diff(summary.duration_ms) <= 70, "system.wav {wav_ms} ms, media {} ms", summary.duration_ms);
    assert!(summary.files.contains(&dir.join("system.wav")));

    match summary.files.contains(&dir.join("input.json")) {
        true => eprintln!("input.json recorded"),
        false => eprintln!("input.json not recorded: {:?}", summary.warnings),
    }
}

/// Set `GLINT_TEST_WINDOW` to an on-screen window's `CGWindowID`.
#[test]
#[ignore = "needs the Screen Recording permission and GLINT_TEST_WINDOW"]
fn records_a_window() {
    let id: isize = std::env::var("GLINT_TEST_WINDOW").expect("GLINT_TEST_WINDOW").parse().unwrap();
    let dir = std::env::temp_dir().join("glint-media-record-mac-window");
    let _ = std::fs::remove_dir_all(&dir);
    let rec = Recorder::start(RecordOptions {
        source: CaptureSource::Window(id),
        session_dir: dir.clone(),
        fps: Fps::F30,
        system_audio: false,
        mic: None,
        camera: None,
        record_input: true,
        mic_tap: None,
        ignored_keys: Vec::new(),
    })
    .unwrap();
    std::thread::sleep(Duration::from_secs(1));
    let summary = rec.stop();
    eprintln!("{summary:#?}");
    let reader = VideoReader::open(&dir.join("screen.mp4")).unwrap();
    assert_eq!(reader.size(), (summary.width, summary.height));
    assert!(summary.warnings.is_empty(), "{:?}", summary.warnings);
}

#[test]
#[ignore = "needs the Screen Recording permission"]
fn records_an_area_at_its_even_pixel_size_without_the_pause() {
    let dir = std::env::temp_dir().join("glint-media-record-mac-area");
    let _ = std::fs::remove_dir_all(&dir);
    let rect = glint_media::Rect { x: 100, y: 50, width: 801, height: 601 };
    let rec = Recorder::start(RecordOptions {
        source: CaptureSource::Area { display: 0, rect },
        session_dir: dir.clone(),
        fps: Fps::F60,
        system_audio: false,
        mic: None,
        camera: None,
        record_input: false,
        mic_tap: None,
        ignored_keys: Vec::new(),
    })
    .unwrap();
    std::thread::sleep(Duration::from_millis(500));
    rec.pause();
    std::thread::sleep(Duration::from_millis(500));
    rec.resume();
    std::thread::sleep(Duration::from_millis(500));
    let summary = rec.stop();
    assert_eq!((summary.width, summary.height), (800, 600));
    assert_eq!(summary.files, vec![dir.join("screen.mp4")]);
    let reader = VideoReader::open(&dir.join("screen.mp4")).unwrap();
    assert_eq!(reader.size(), (800, 600));
    assert!(reader.duration_ms().abs_diff(1000) <= 40, "{} ms", reader.duration_ms());
}
