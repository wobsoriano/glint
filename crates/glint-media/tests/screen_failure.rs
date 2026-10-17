use glint_media::{microphones, read_wav, CaptureSource, Fps, RecordOptions, Recorder, VideoReader};
use std::path::PathBuf;
use std::time::{Duration, Instant};

const FRAMES: u64 = 20;

fn session(name: &str) -> PathBuf {
    std::env::set_var("GLINT_FAIL_SCREEN_AFTER", FRAMES.to_string());
    let dir = std::env::temp_dir().join("glint-media-screen-failure").join(name);
    let _ = std::fs::remove_dir_all(&dir);
    dir
}

fn options(dir: &PathBuf) -> RecordOptions {
    RecordOptions {
        source: CaptureSource::Display(0),
        session_dir: dir.clone(),
        fps: Fps::F30,
        system_audio: false,
        mic: None,
        camera: None,
        record_input: false,
        mic_tap: None,
    }
}

fn start(dir: &PathBuf) -> Recorder {
    Recorder::start(options(dir)).unwrap()
}

fn let_it_die() {
    std::thread::sleep(Duration::from_secs(2));
}

#[test]
fn stop_after_the_screen_thread_died_keeps_the_frames_written_so_far() {
    let dir = session("stop");
    let rec = start(&dir);
    let_it_die();
    let summary = rec.stop();

    assert_eq!(summary.files, vec![dir.join("screen.mp4")]);
    assert_eq!(summary.warnings, vec![format!("the screen capture failed; the recording ends there: GLINT_FAIL_SCREEN_AFTER: injected failure after {FRAMES} frames")]);

    let mut reader = VideoReader::open(&dir.join("screen.mp4")).unwrap();
    let mut frames = 0;
    while reader.next_frame().unwrap().is_some() {
        frames += 1;
    }
    assert_eq!(frames, FRAMES);
    assert_eq!(reader.duration_ms(), FRAMES * 1000 / 30);
}

#[test]
fn cancel_after_the_screen_thread_died_deletes_the_session_dir() {
    let dir = session("cancel");
    let rec = start(&dir);
    let_it_die();
    rec.cancel();
    assert!(!dir.exists(), "{} still exists", dir.display());
}

#[test]
fn the_recorder_reports_live_until_the_screen_thread_dies() {
    let dir = session("live");
    let rec = start(&dir);
    assert!(rec.is_live());
    let started = Instant::now();
    while rec.is_live() {
        assert!(started.elapsed() < Duration::from_secs(5), "still live 5 s after the injected failure point");
        std::thread::sleep(Duration::from_millis(20));
    }
    let lived = started.elapsed();
    assert!((Duration::from_millis(500)..Duration::from_secs(2)).contains(&lived), "died after {lived:?}, expected about 0.67 s");
    assert_eq!(rec.stop().files, vec![dir.join("screen.mp4")]);
}

#[test]
fn the_audio_tracks_end_where_the_screen_ended() {
    let dir = session("tracks");
    let mic = microphones().unwrap_or_default().into_iter().next().map(|m| m.id);
    let rec = Recorder::start(RecordOptions { system_audio: true, mic: mic.clone(), ..options(&dir) }).unwrap();
    let_it_die();
    let summary = rec.stop();

    assert_eq!(summary.warnings, vec![format!("the screen capture failed; the recording ends there: GLINT_FAIL_SCREEN_AFTER: injected failure after {FRAMES} frames")]);
    assert_eq!(summary.duration_ms, FRAMES * 1000 / 30);
    let wavs: Vec<_> = summary.files.iter().filter(|f| f.extension().is_some_and(|e| e == "wav")).collect();
    assert_eq!(wavs.len(), 1 + mic.is_some() as usize);
    for wav in wavs {
        let data = read_wav(wav).unwrap();
        assert_eq!(data.frames() as u64, FRAMES * 48000 / 30, "{}", wav.display());
    }
}
