//! WASAPI shared-mode playback to the default output device, with a clock that reports how
//! much audio the device has actually played so video can follow it.

use crate::mf;
use anyhow::Context;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};
use std::thread::JoinHandle;
use std::time::{Duration, Instant};
use windows::Win32::Media::Audio::{
    eConsole, eRender, IAudioClient, IAudioClock, IAudioRenderClient, IMMDeviceEnumerator, MMDeviceEnumerator,
    AUDCLNT_SHAREMODE_SHARED, AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM, AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY, WAVEFORMATEX,
    WAVEFORMATEXTENSIBLE, WAVEFORMATEXTENSIBLE_0,
};
use windows::Win32::Media::KernelStreaming::WAVE_FORMAT_EXTENSIBLE;
use windows::Win32::Media::Multimedia::KSDATAFORMAT_SUBTYPE_IEEE_FLOAT;
use windows::Win32::System::Com::{CoCreateInstance, CLSCTX_ALL};

/// Fills interleaved stereo f32 at [`AudioOutput::SAMPLE_RATE`].
type FillFn = Box<dyn FnMut(&mut [f32]) + Send>;

/// A running output stream. Dropping it stops playback at once.
pub struct AudioOutput {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<()>>,
    clock: AudioClock,
}

/// How much audio an [`AudioOutput`] has played, readable from any thread.
#[derive(Clone)]
pub struct AudioClock(Arc<Mutex<DeviceClock>>);

/// The device position at the last reading, extrapolated between readings.
#[derive(Clone)]
struct DeviceClock {
    played_ms: f64,
    read_at: Instant,
    /// Why the stream stopped early, e.g. the device was unplugged.
    error: Option<String>,
}

impl AudioOutput {
    pub const SAMPLE_RATE: u32 = 48_000;
    pub const CHANNELS: u16 = 2;

    /// Opens the default output device and starts pulling samples from `fill` on a
    /// dedicated thread. The engine converts to the device's mix format.
    pub fn start(fill: impl FnMut(&mut [f32]) + Send + 'static) -> anyhow::Result<Self> {
        let stop = Arc::new(AtomicBool::new(false));
        let clock = AudioClock(Arc::new(Mutex::new(DeviceClock { played_ms: 0.0, read_at: Instant::now(), error: None })));
        let (tx, rx) = std::sync::mpsc::channel::<anyhow::Result<()>>();
        let (stop2, clock2) = (stop.clone(), clock.clone());
        let mut fill: FillFn = Box::new(fill);
        let thread = std::thread::Builder::new().name("glint-audio-out".into()).spawn(move || {
            let stream = match Stream::open(&mut fill) {
                Ok(s) => s,
                Err(e) => {
                    let _ = tx.send(Err(e));
                    return;
                }
            };
            let _ = tx.send(Ok(()));
            if let Err(e) = stream.run(&mut fill, &stop2, &clock2.0) {
                let mut c = clock2.0.lock().unwrap_or_else(|e| e.into_inner());
                c.read_at = Instant::now();
                c.error = Some(format!("{e:#}"));
            }
        })?;
        match rx.recv() {
            Ok(Ok(())) => Ok(Self { stop, thread: Some(thread), clock }),
            Ok(Err(e)) => {
                let _ = thread.join();
                Err(e)
            }
            Err(_) => {
                let _ = thread.join();
                anyhow::bail!("audio output thread exited before opening the device")
            }
        }
    }

    /// The stream's clock: how much audio the device has played since `start`.
    pub fn clock(&self) -> AudioClock {
        self.clock.clone()
    }
}

impl AudioClock {
    /// Milliseconds of audio the device has played. Stays at 0 until the first samples
    /// reach the device, and stops once the output is dropped. If the stream fails, it
    /// keeps running on the wall clock so video does not freeze.
    pub fn played_ms(&self) -> f64 {
        let c = self.0.lock().unwrap_or_else(|e| e.into_inner());
        let since = c.read_at.elapsed().as_secs_f64() * 1000.0;
        if c.error.is_some() {
            return c.played_ms + since;
        }
        if c.played_ms == 0.0 {
            return 0.0;
        }
        // Readings come every few milliseconds; never extrapolate across a stall.
        c.played_ms + since.min(20.0)
    }

    /// Why the stream stopped early, if it did.
    pub fn error(&self) -> Option<String> {
        self.0.lock().unwrap_or_else(|e| e.into_inner()).error.clone()
    }
}

impl Drop for AudioOutput {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

struct Stream {
    client: IAudioClient,
    render: IAudioRenderClient,
    clock: IAudioClock,
    frequency: u64,
    buffer_frames: u32,
}

impl Stream {
    fn open(fill: &mut FillFn) -> anyhow::Result<Self> {
        mf::init()?;
        unsafe {
            let enumerator: IMMDeviceEnumerator = CoCreateInstance(&MMDeviceEnumerator, None, CLSCTX_ALL).context("MMDeviceEnumerator")?;
            let device = enumerator.GetDefaultAudioEndpoint(eRender, eConsole).context("no default output device")?;
            let client: IAudioClient = device.Activate(CLSCTX_ALL, None).context("IAudioClient")?;
            let format = stereo_float();
            client
                .Initialize(
                    AUDCLNT_SHAREMODE_SHARED,
                    AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY,
                    1_000_000,
                    0,
                    &format.Format,
                    None,
                )
                .context("IAudioClient::Initialize 48 kHz stereo float")?;
            let buffer_frames = client.GetBufferSize()?;
            let render: IAudioRenderClient = client.GetService().context("IAudioRenderClient")?;
            let clock: IAudioClock = client.GetService().context("IAudioClock")?;
            let frequency = clock.GetFrequency()?;
            let stream = Self { client, render, clock, frequency, buffer_frames };
            stream.feed(fill)?;
            stream.client.Start().context("IAudioClient::Start")?;
            Ok(stream)
        }
    }

    fn run(self, fill: &mut FillFn, stop: &AtomicBool, clock: &Mutex<DeviceClock>) -> anyhow::Result<()> {
        while !stop.load(Ordering::Relaxed) {
            self.feed(fill)?;
            let mut pos = 0u64;
            unsafe { self.clock.GetPosition(&mut pos, None)? };
            let played_ms = pos as f64 * 1000.0 / self.frequency.max(1) as f64;
            *clock.lock().unwrap_or_else(|e| e.into_inner()) = DeviceClock { played_ms, read_at: Instant::now(), error: None };
            std::thread::sleep(Duration::from_millis(4));
        }
        unsafe { self.client.Stop()? };
        Ok(())
    }

    /// Tops the device buffer up with fresh samples.
    fn feed(&self, fill: &mut FillFn) -> anyhow::Result<()> {
        unsafe {
            let free = self.buffer_frames - self.client.GetCurrentPadding()?;
            if free == 0 {
                return Ok(());
            }
            let data = self.render.GetBuffer(free)?;
            let samples = std::slice::from_raw_parts_mut(data as *mut f32, free as usize * AudioOutput::CHANNELS as usize);
            fill(samples);
            self.render.ReleaseBuffer(free, 0)?;
        }
        Ok(())
    }
}

fn stereo_float() -> WAVEFORMATEXTENSIBLE {
    let (rate, channels) = (AudioOutput::SAMPLE_RATE, AudioOutput::CHANNELS);
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
        dwChannelMask: 0x3,
        SubFormat: KSDATAFORMAT_SUBTYPE_IEEE_FLOAT,
    }
}
