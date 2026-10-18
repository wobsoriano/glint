use crate::audio::{AudioStats, ClockedWav, Converter, LevelMeter, SAMPLE_RATE};
use crate::clock::MediaClock;
use crate::devices::DeviceId;
use super::mf::{self, pcwstr, wide};
use anyhow::{bail, Context};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::SyncSender;
use std::sync::Arc;
use std::thread::JoinHandle;
use std::time::Duration;
use windows::Win32::Media::Audio::{
    eCapture, eConsole, eRender, IAudioCaptureClient, IAudioClient, IMMDeviceEnumerator, MMDeviceEnumerator,
    AUDCLNT_BUFFERFLAGS_SILENT, AUDCLNT_SHAREMODE_SHARED, AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM, AUDCLNT_STREAMFLAGS_LOOPBACK,
    AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY, WAVEFORMATEX, WAVEFORMATEXTENSIBLE, WAVEFORMATEXTENSIBLE_0, WAVE_FORMAT_PCM,
};
use windows::Win32::Media::KernelStreaming::{KSDATAFORMAT_SUBTYPE_PCM, WAVE_FORMAT_EXTENSIBLE};
use windows::Win32::Media::Multimedia::{KSDATAFORMAT_SUBTYPE_IEEE_FLOAT, WAVE_FORMAT_IEEE_FLOAT};
use windows::Win32::System::Com::{CoCreateInstance, CoTaskMemFree, CLSCTX_ALL};

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) enum AudioSource {
    Loopback,
    Microphone(Option<DeviceId>),
}

pub(crate) struct AudioTrack {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<anyhow::Result<AudioStats>>>,
    level: Arc<LevelMeter>,
    pub path: PathBuf,
}

impl AudioTrack {
    pub fn start(
        source: AudioSource,
        path: &Path,
        channels: u16,
        clock: Arc<MediaClock>,
        tap: Option<SyncSender<Vec<f32>>>,
    ) -> anyhow::Result<Self> {
        let stop = Arc::new(AtomicBool::new(false));
        let level = Arc::new(LevelMeter::default());
        let writer_path = path.to_path_buf();
        let thread = spawn_capture(format!("glint-audio-{source:?}"), source, channels, stop.clone(), level.clone(), tap, move || {
            ClockedWav::create(&writer_path, channels, clock).map(Some)
        })?;
        Ok(Self { stop, thread: Some(thread), level, path: path.to_path_buf() })
    }

    pub fn level(&self) -> &Arc<LevelMeter> {
        &self.level
    }

    pub fn stop(mut self) -> anyhow::Result<AudioStats> {
        self.stop.store(true, Ordering::Relaxed);
        let thread = self.thread.take().unwrap();
        thread.join().map_err(|_| anyhow::anyhow!("audio thread panicked"))?
    }
}

impl Drop for AudioTrack {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

pub struct MicMonitor {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<anyhow::Result<AudioStats>>>,
    level: Arc<LevelMeter>,
}

impl MicMonitor {
    /// Starts metering `mic`, or the default microphone when `None`.
    pub fn start(mic: Option<DeviceId>) -> anyhow::Result<Self> {
        let stop = Arc::new(AtomicBool::new(false));
        let level = Arc::new(LevelMeter::default());
        let thread = spawn_capture("glint-mic-monitor".into(), AudioSource::Microphone(mic), 1, stop.clone(), level.clone(), None, || Ok(None))?;
        Ok(Self { stop, thread: Some(thread), level })
    }

    pub fn level(&self) -> f32 {
        self.level.rms()
    }
}

impl Drop for MicMonitor {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

type Sink = Option<ClockedWav>;

/// Starts the capture thread. WASAPI objects are not `Send`, so the device is opened on that
/// thread and the outcome comes back over a channel before this returns.
fn spawn_capture(
    name: String,
    source: AudioSource,
    channels: u16,
    stop: Arc<AtomicBool>,
    level: Arc<LevelMeter>,
    tap: Option<SyncSender<Vec<f32>>>,
    make_sink: impl FnOnce() -> anyhow::Result<Sink> + Send + 'static,
) -> anyhow::Result<JoinHandle<anyhow::Result<AudioStats>>> {
    let (tx, rx) = std::sync::mpsc::channel::<anyhow::Result<()>>();
    let thread = std::thread::Builder::new().name(name).spawn(move || {
        let opened = Capture::open(&source, channels).and_then(|capture| Ok((capture, make_sink()?)));
        let (capture, sink) = match opened {
            Ok(v) => v,
            Err(e) => {
                let _ = tx.send(Err(e));
                return Ok(AudioStats { frames: 0, bytes: 0 });
            }
        };
        let _ = tx.send(Ok(()));
        capture.run(sink, &stop, &level, tap)
    })?;
    match rx.recv() {
        Ok(Ok(())) => Ok(thread),
        Ok(Err(e)) => {
            let _ = thread.join();
            Err(e)
        }
        Err(_) => {
            let _ = thread.join();
            bail!("audio thread exited before opening the device")
        }
    }
}

#[derive(Clone, Copy, Debug)]
struct DeviceFormat {
    rate: u32,
    channels: u16,
    float: bool,
    bits: u16,
}

struct Capture {
    client: IAudioClient,
    capture: IAudioCaptureClient,
    format: DeviceFormat,
    converter: Converter,
    decoded: Vec<f32>,
}

impl Capture {
    fn open(source: &AudioSource, out_channels: u16) -> anyhow::Result<Self> {
        mf::init()?;
        unsafe {
            let enumerator: IMMDeviceEnumerator = CoCreateInstance(&MMDeviceEnumerator, None, CLSCTX_ALL).context("MMDeviceEnumerator")?;
            let device = match source {
                AudioSource::Loopback => enumerator.GetDefaultAudioEndpoint(eRender, eConsole).context("no default output device")?,
                AudioSource::Microphone(None) => enumerator.GetDefaultAudioEndpoint(eCapture, eConsole).context("no default microphone")?,
                AudioSource::Microphone(Some(id)) => {
                    let w = wide(&id.0);
                    enumerator.GetDevice(pcwstr(&w)).with_context(|| format!("microphone {id} not found"))?
                }
            };
            let client: IAudioClient = device.Activate(CLSCTX_ALL, None).context("IAudioClient")?;
            let flags = AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM
                | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY
                | if *source == AudioSource::Loopback { AUDCLNT_STREAMFLAGS_LOOPBACK } else { 0 };
            let wanted = float_format(SAMPLE_RATE, out_channels);
            let buffer_100ns = 2_000_000;
            let format = match client.Initialize(AUDCLNT_SHAREMODE_SHARED, flags, buffer_100ns, 0, &wanted.Format, None) {
                Ok(()) => DeviceFormat { rate: SAMPLE_RATE, channels: out_channels, float: true, bits: 32 },
                Err(_) => {
                    // The engine would not convert for us: take the mix format and convert here.
                    let client: IAudioClient = device.Activate(CLSCTX_ALL, None)?;
                    let mix = client.GetMixFormat().context("GetMixFormat")?;
                    let format = parse_format(mix)?;
                    let flags = flags & !(AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY);
                    let r = client.Initialize(AUDCLNT_SHAREMODE_SHARED, flags, buffer_100ns, 0, mix, None);
                    CoTaskMemFree(Some(mix as *const _));
                    r.context("IAudioClient::Initialize with mix format")?;
                    return Self::started(client, format, out_channels);
                }
            };
            Self::started(client, format, out_channels)
        }
    }

    unsafe fn started(client: IAudioClient, format: DeviceFormat, out_channels: u16) -> anyhow::Result<Self> {
        let capture: IAudioCaptureClient = unsafe { client.GetService() }.context("IAudioCaptureClient")?;
        unsafe { client.Start() }.context("IAudioClient::Start")?;
        Ok(Self { client, capture, format, converter: Converter::new(format.rate, format.channels, out_channels), decoded: Vec::new() })
    }

    fn run(mut self, mut sink: Sink, stop: &AtomicBool, level: &LevelMeter, tap: Option<SyncSender<Vec<f32>>>) -> anyhow::Result<AudioStats> {
        let mut out = Vec::new();
        while !stop.load(Ordering::Relaxed) {
            let mut got_any = false;
            loop {
                let packet = unsafe { self.capture.GetNextPacketSize()? };
                if packet == 0 {
                    break;
                }
                got_any = true;
                let mut data = std::ptr::null_mut();
                let mut frames = 0u32;
                let mut flags = 0u32;
                unsafe { self.capture.GetBuffer(&mut data, &mut frames, &mut flags, None, None)? };
                out.clear();
                let silent = flags & AUDCLNT_BUFFERFLAGS_SILENT.0 as u32 != 0;
                let bytes = frames as usize * self.format.channels as usize * (self.format.bits / 8) as usize;
                let raw = unsafe { std::slice::from_raw_parts(data, bytes) };
                self.convert(raw, frames as usize, silent, &mut out);
                unsafe { self.capture.ReleaseBuffer(frames)? };
                level.update(&out);
                if let Some(tap) = &tap {
                    let _ = tap.try_send(out.clone());
                }
                if let Some(wav) = sink.as_mut() {
                    wav.write(&out)?;
                }
            }
            if !got_any {
                std::thread::sleep(Duration::from_millis(5));
            }
        }
        unsafe { self.client.Stop()? };
        match sink {
            Some(wav) => wav.finish(),
            None => Ok(AudioStats { frames: 0, bytes: 0 }),
        }
    }

    /// Decodes the device's samples to f32 and converts them to the track's format.
    fn convert(&mut self, raw: &[u8], frames: usize, silent: bool, out: &mut Vec<f32>) {
        let f = self.format;
        let samples = frames * f.channels as usize;
        self.decoded.clear();
        if silent {
            self.decoded.resize(samples, 0.0);
        } else {
            let width = (f.bits / 8) as usize;
            self.decoded.extend(raw[..samples * width].chunks_exact(width).map(|b| match (f.float, f.bits) {
                (true, 32) => f32::from_le_bytes(b.try_into().unwrap()),
                (false, 16) => i16::from_le_bytes(b.try_into().unwrap()) as f32 / 32768.0,
                (false, 32) => i32::from_le_bytes(b.try_into().unwrap()) as f32 / 2_147_483_648.0,
                (false, 24) => (i32::from_le_bytes([0, b[0], b[1], b[2]]) >> 8) as f32 / 8_388_608.0,
                _ => 0.0,
            }));
        }
        self.converter.push(&self.decoded, out);
    }
}

fn float_format(rate: u32, channels: u16) -> WAVEFORMATEXTENSIBLE {
    WAVEFORMATEXTENSIBLE {
        Format: WAVEFORMATEX {
            wFormatTag: WAVE_FORMAT_EXTENSIBLE as u16,
            nChannels: channels,
            nSamplesPerSec: rate,
            nAvgBytesPerSec: rate * channels as u32 * 4,
            nBlockAlign: channels * 4,
            wBitsPerSample: 32,
            cbSize: 22,
        },
        Samples: WAVEFORMATEXTENSIBLE_0 { wValidBitsPerSample: 32 },
        dwChannelMask: if channels == 1 { 0x4 } else { 0x3 },
        SubFormat: KSDATAFORMAT_SUBTYPE_IEEE_FLOAT,
    }
}

unsafe fn parse_format(p: *const WAVEFORMATEX) -> anyhow::Result<DeviceFormat> {
    let f = unsafe { *p };
    let tag = f.wFormatTag as u32;
    let float = if tag == WAVE_FORMAT_EXTENSIBLE {
        let ext = unsafe { *(p as *const WAVEFORMATEXTENSIBLE) };
        let sub = ext.SubFormat;
        if sub == KSDATAFORMAT_SUBTYPE_IEEE_FLOAT {
            true
        } else if sub == KSDATAFORMAT_SUBTYPE_PCM {
            false
        } else {
            bail!("unsupported mix format subtype {sub:?}");
        }
    } else if tag == WAVE_FORMAT_IEEE_FLOAT {
        true
    } else if tag == WAVE_FORMAT_PCM {
        false
    } else {
        bail!("unsupported mix format tag {tag}");
    };
    Ok(DeviceFormat { rate: f.nSamplesPerSec, channels: f.nChannels, float, bits: f.wBitsPerSample })
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::wav::read_wav;

    #[test]
    fn level_meter_reports_rms_of_last_buffer() {
        let m = LevelMeter::default();
        m.update(&[0.5, -0.5, 0.5, -0.5]);
        assert_eq!(m.rms(), 0.5);
        m.update(&[0.0; 8]);
        assert_eq!(m.rms(), 0.0);
        m.update(&[]);
        assert_eq!(m.rms(), 0.0, "an empty buffer keeps the last level");
    }

    #[test]
    #[ignore = "needs an audio output device"]
    fn loopback_track_matches_media_time_and_format() {
        let dir = std::env::temp_dir().join("glint-media-audio-test");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("system.wav");
        let clock = Arc::new(MediaClock::paused());
        let track = AudioTrack::start(AudioSource::Loopback, &path, 2, clock.clone(), None).unwrap();
        clock.resume();
        std::thread::sleep(Duration::from_millis(700));
        clock.pause();
        std::thread::sleep(Duration::from_millis(300));
        clock.resume();
        std::thread::sleep(Duration::from_millis(300));
        clock.pause();
        let media_ms = clock.now_ms();
        let stats = track.stop().unwrap();
        let wav = read_wav(&path).unwrap();
        assert_eq!((wav.sample_rate, wav.channels), (48000, 2));
        assert_eq!(wav.frames() as u64, stats.frames);
        let diff = wav.duration_ms() as i64 - media_ms as i64;
        assert!(diff.abs() <= 70, "wav is {} ms, media time {} ms", wav.duration_ms(), media_ms);
        assert!(wav.duration_ms() < 1200, "the 300 ms pause leaked into the file: {} ms", wav.duration_ms());
    }

    #[test]
    #[ignore = "needs a microphone"]
    fn tap_receives_converted_buffers_without_blocking_capture() {
        let dir = std::env::temp_dir().join("glint-media-tap-test");
        std::fs::create_dir_all(&dir).unwrap();
        let clock = Arc::new(MediaClock::paused());
        let (tx, rx) = std::sync::mpsc::sync_channel(1);
        let track = AudioTrack::start(AudioSource::Microphone(None), &dir.join("mic.wav"), 1, clock.clone(), Some(tx)).unwrap();
        clock.resume();
        std::thread::sleep(Duration::from_millis(600));
        clock.pause();
        let media_ms = clock.now_ms();
        let stats = track.stop().unwrap();
        let buffers: Vec<Vec<f32>> = rx.try_iter().collect();
        assert_eq!(buffers.len(), 1, "a one-slot tap that nobody reads holds one buffer and drops the rest");
        assert!(!buffers[0].is_empty());
        let wav_ms = stats.frames * 1000 / SAMPLE_RATE as u64;
        assert!((wav_ms as i64 - media_ms as i64).abs() <= 70, "the full tap stalled the file: {wav_ms} ms of {media_ms} ms");
    }
}
