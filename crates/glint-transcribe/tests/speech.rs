use std::path::{Path, PathBuf};
use std::process::Command;
use std::sync::atomic::AtomicBool;
use std::sync::Arc;
use std::time::{Duration, Instant};

use glint_transcribe::*;

const SCRIPT: &str = "Hello everyone, um, today we are recording a short demo of the new screen recorder.";

fn data_dir() -> PathBuf {
    std::env::var_os("GLINT_DATA_DIR")
        .map(PathBuf::from)
        .unwrap_or_else(|| PathBuf::from(env!("CARGO_TARGET_TMPDIR")).join("glint-transcribe-data"))
}

fn synthesize(text: &str, path: &Path) {
    let script = format!(
        "Add-Type -AssemblyName System.Speech; \
         $s = New-Object System.Speech.Synthesis.SpeechSynthesizer; \
         $f = New-Object System.Speech.AudioFormat.SpeechAudioFormatInfo 48000, ([System.Speech.AudioFormat.AudioBitsPerSample]::Sixteen), ([System.Speech.AudioFormat.AudioChannel]::Mono); \
         $s.SetOutputToWaveFile('{}', $f); $s.Speak('{}'); $s.Dispose()",
        path.display(),
        text.replace('\'', "''")
    );
    let status = Command::new("powershell").args(["-NoProfile", "-Command", &script]).status().unwrap();
    assert!(status.success(), "SAPI synthesis failed");
}

fn read_wav(path: &Path) -> (Vec<f32>, u32) {
    let mut reader = hound::WavReader::open(path).unwrap();
    let spec = reader.spec();
    assert_eq!((spec.channels, spec.bits_per_sample), (1, 16));
    let samples = reader.samples::<i16>().map(|s| s.unwrap() as f32 / 32768.0).collect();
    (samples, spec.sample_rate)
}

fn ensure(model: Model) -> PathBuf {
    let dir = data_dir();
    if !is_installed(&dir, model) {
        let started = Instant::now();
        let mut last_tenth = 0;
        let path = download_model(
            &dir,
            model,
            |done, total| {
                let tenth = done * 10 / total;
                if tenth != last_tenth {
                    last_tenth = tenth;
                    println!("  download {:?}: {}%", model, tenth * 10);
                }
            },
            &AtomicBool::new(false),
        )
        .unwrap();
        let secs = started.elapsed().as_secs_f64();
        let mb = model.size_bytes() as f64 / 1e6;
        println!("downloaded {:?}: {mb:.1} MB in {secs:.1} s ({:.1} MB/s)", model, mb / secs);
        assert_eq!(path, model_path(&dir, model));
    }
    assert!(installed_models(&dir).contains(&model));
    model_path(&dir, model)
}

fn wav() -> (Vec<f32>, u32) {
    let path = PathBuf::from(env!("CARGO_TARGET_TMPDIR")).join("glint-speech.wav");
    if !path.exists() {
        synthesize(SCRIPT, &path);
    }
    read_wav(&path)
}

#[test]
#[ignore = "downloads models and synthesizes speech"]
fn batch_transcription_finds_words_in_order_and_the_filler() {
    let base = ensure(Model::BaseEn);
    let (samples, rate) = wav();
    let pcm = Pcm::mono(&samples, rate);
    println!("audio: {} ms at {} Hz", pcm.duration_ms(), rate);

    let started = Instant::now();
    let transcript = transcribe(&base, pcm, |_| {}, Arc::new(AtomicBool::new(false))).unwrap();
    println!("base.en transcribed in {:.2} s, language {}", started.elapsed().as_secs_f64(), transcript.language);
    for w in &transcript.words {
        println!("  {:>5}-{:>5} ms  {:.2}  {}", w.start_ms, w.end_ms, w.confidence, w.text);
    }

    let spoken: Vec<String> = transcript.words.iter().map(|w| normalize_word(&w.text)).collect();
    let expected = ["hello", "everyone", "today", "recording", "short", "demo", "new", "screen", "recorder"];
    let mut cursor = 0;
    let mut times = Vec::new();
    for word in expected {
        let found = spoken[cursor..].iter().position(|s| s == word).map(|i| i + cursor);
        let i = found.unwrap_or_else(|| panic!("{word:?} missing or out of order in {spoken:?}"));
        times.push(transcript.words[i].start_ms);
        cursor = i + 1;
    }
    assert!(times.windows(2).all(|t| t[0] < t[1]), "start times not increasing: {times:?}");

    let fillers = filler_ranges(&transcript.words);
    let filler_text: Vec<_> = fillers.iter().map(|r| &transcript.words[r.clone()]).collect();
    println!("fillers: {filler_text:?}");
    assert_eq!(fillers.len(), 1);
    assert_eq!(normalize_word(&transcript.words[fillers[0].start].text), "um");
    println!("filler cut: {:?}", word_cut_ranges(&transcript.words, &fillers, pcm.duration_ms()));
    println!("captions: {:?}", caption_lines(&transcript.words, &CaptionOptions::default()));

    let out = PathBuf::from(env!("CARGO_TARGET_TMPDIR")).join(TRANSCRIPT_FILE);
    transcript.save(&out).unwrap();
    assert_eq!(Transcript::load(&out).unwrap(), transcript);

    let cancelled = transcribe(&base, pcm, |_| {}, Arc::new(AtomicBool::new(true))).unwrap_err();
    assert!(cancelled.is::<Cancelled>(), "{cancelled:#}");
}

#[test]
#[ignore = "downloads models and synthesizes speech"]
fn live_transcription_tracks_the_script() {
    let tiny = ensure(Model::TinyEn);
    let (samples, rate) = wav();
    let chunk = rate as usize / 10;

    let live = LiveTranscriber::start(&tiny, rate).unwrap();
    let mut tracker = ScriptTracker::new(SCRIPT);
    let started = Instant::now();
    for (i, piece) in samples.chunks(chunk).enumerate() {
        live.push(piece);
        let words = live.poll();
        if !words.is_empty() {
            let texts: Vec<&str> = words.iter().map(|w| w.text.as_str()).collect();
            let index = tracker.feed(&texts);
            println!("t={:>5} ms  heard {:?} -> script index {:?}", i * 100, texts, index);
        }
        let due = Duration::from_millis((i as u64 + 1) * 100);
        if let Some(wait) = due.checked_sub(started.elapsed()) {
            std::thread::sleep(wait);
        }
    }
    let rest = live.finish();
    let texts: Vec<&str> = rest.iter().map(|w| w.text.as_str()).collect();
    let index = tracker.feed(&texts);
    println!("final pass heard {texts:?} -> script index {index:?} of {}", tracker.display_len() - 1);
    assert!(index.unwrap() + 2 >= tracker.display_len() - 1, "tracker stalled at {index:?}");
}
