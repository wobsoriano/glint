use glint_media::input::InputTimeline;
use glint_media::{read_wav, CaptureSource, DeviceId, Fps, RecordOptions, Recorder, Rect, VideoReader};
use std::path::PathBuf;
use std::time::{Duration, Instant};

fn main() -> anyhow::Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let get = |flag: &str| args.iter().position(|a| a == flag).and_then(|i| args.get(i + 1)).cloned();
    let has = |flag: &str| args.iter().any(|a| a == flag);

    let source = match get("--source").unwrap_or_else(|| "display:0".into()).split_once(':') {
        Some(("display", i)) => CaptureSource::Display(i.parse()?),
        Some(("window", h)) => CaptureSource::Window(h.parse()?),
        Some(("area", spec)) => {
            let v: Vec<i32> = spec.split(',').map(|s| s.parse()).collect::<Result<_, _>>()?;
            CaptureSource::Area { display: v[0] as usize, rect: Rect { x: v[1], y: v[2], width: v[3] as u32, height: v[4] as u32 } }
        }
        _ => anyhow::bail!("bad --source"),
    };
    let seconds: f64 = get("--seconds").map(|s| s.parse()).transpose()?.unwrap_or(3.0);
    let fps = if get("--fps").as_deref() == Some("60") { Fps::F60 } else { Fps::F30 };
    let pause_ms: u64 = get("--pause-ms").map(|s| s.parse()).transpose()?.unwrap_or(0);
    let session_dir = get("--out").map(PathBuf::from).unwrap_or_else(|| std::env::temp_dir().join("glint-record-example"));
    let _ = std::fs::remove_dir_all(&session_dir);
    let mic = get("--mic").map(|m| if m == "default" { glint_media::microphones().unwrap()[0].id.clone() } else { DeviceId(m) });

    let opts = RecordOptions {
        source,
        session_dir: session_dir.clone(),
        fps,
        system_audio: !has("--no-system-audio"),
        mic,
        camera: get("--camera").map(DeviceId),
        record_input: !has("--no-input"),
        mic_tap: None,
    };
    println!("start {:?} -> {}", opts.source, session_dir.display());
    let started = Instant::now();
    let rec = Recorder::start(opts)?;
    println!("started in {:?}", started.elapsed());

    let half = Duration::from_secs_f64(seconds / 2.0);
    std::thread::sleep(half);
    if pause_ms > 0 {
        rec.pause();
        println!("paused at media {:?}", rec.elapsed());
        std::thread::sleep(Duration::from_millis(pause_ms));
        println!("still at media {:?} after {pause_ms} ms wall", rec.elapsed());
        rec.resume();
    }
    std::thread::sleep(half);
    if let Some(level) = rec.mic_level() {
        println!("mic level rms {level:.4}");
    }
    let wall = started.elapsed();
    let summary = rec.stop();
    println!("stopped after {wall:?} wall; summary: {summary:#?}");

    let mut video = VideoReader::open(&session_dir.join("screen.mp4"))?;
    let mut frames = 0u64;
    let mut first_size = None;
    let mut last_t = 0;
    while let Some(f) = video.next_frame()? {
        first_size.get_or_insert((f.image.width(), f.image.height()));
        frames += 1;
        last_t = f.t_ms;
    }
    println!(
        "screen.mp4: {} frames decoded, size {:?}, fps {}, duration {} ms, last frame t={} ms (expected ~{} frames)",
        frames,
        first_size,
        video.fps(),
        video.duration_ms(),
        last_t,
        summary.duration_ms * fps.value() as u64 / 1000
    );
    for name in ["system.wav", "mic.wav"] {
        let p = session_dir.join(name);
        if p.exists() {
            let w = read_wav(&p)?;
            let peak = w.samples.iter().fold(0f32, |m, s| m.max(s.abs()));
            println!("{name}: {} Hz, {} ch, {} frames = {} ms, peak {peak:.3}", w.sample_rate, w.channels, w.frames(), w.duration_ms());
        }
    }
    if session_dir.join(InputTimeline::FILE).exists() {
        let tl = InputTimeline::load(&session_dir)?;
        println!(
            "input.json: size {:?}, {} pointer samples, {} clicks {:?}, {} keys {:?}, {} cursors {:?}",
            tl.size,
            tl.pointer.len(),
            tl.clicks.len(),
            tl.clicks,
            tl.keys.len(),
            tl.keys.iter().map(|k| k.label.as_str()).collect::<Vec<_>>(),
            tl.cursors.len(),
            tl.cursors
        );
        if let (Some(first), Some(last)) = (tl.pointer.first(), tl.pointer.last()) {
            println!("pointer from {:?} to {:?}", first, last);
        }
        for c in &tl.cursors {
            let png = image::open(session_dir.join(&c.file))?;
            println!("{} is {}x{}", c.file, png.width(), png.height());
        }
    }
    Ok(())
}
