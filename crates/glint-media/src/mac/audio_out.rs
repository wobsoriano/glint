//! Playback through the default output `AudioUnit`. The render callback pulls 48 kHz stereo
//! f32 from `fill`; the unit converts to whatever the device runs at and follows changes of
//! the default output device.
//!
//! The clock counts frames the device has played: each callback says at which host time its
//! first frame reaches the speaker (the callback's output time plus the unit, device and
//! stream latency), and `played_ms` runs forward from the latest such anchor, never past the
//! frames rendered so far.

use anyhow::bail;
use objc2_audio_toolbox::{
    kAudioOutputUnitProperty_CurrentDevice, kAudioUnitManufacturer_Apple, kAudioUnitProperty_Latency,
    kAudioUnitProperty_SetRenderCallback, kAudioUnitProperty_StreamFormat, kAudioUnitScope_Global, kAudioUnitScope_Input,
    kAudioUnitSubType_DefaultOutput, kAudioUnitType_Output, AURenderCallbackStruct, AudioComponentDescription,
    AudioComponentFindNext, AudioComponentInstanceDispose, AudioComponentInstanceNew, AudioOutputUnitStart, AudioOutputUnitStop,
    AudioUnit, AudioUnitGetProperty, AudioUnitInitialize, AudioUnitRenderActionFlags, AudioUnitSetProperty, AudioUnitUninitialize,
};
use objc2_core_audio::{
    kAudioDevicePropertyLatency, kAudioDevicePropertyNominalSampleRate, kAudioDevicePropertyStreams, kAudioObjectPropertyElementMain,
    kAudioObjectPropertyScopeGlobal, kAudioObjectPropertyScopeOutput, kAudioStreamPropertyLatency, AudioConvertHostTimeToNanos,
    AudioGetCurrentHostTime, AudioObjectGetPropertyData, AudioObjectID, AudioObjectPropertyAddress,
};
use objc2_core_audio_types::{
    kAudioFormatFlagIsFloat, kAudioFormatFlagIsPacked, kAudioFormatLinearPCM, AudioBufferList, AudioStreamBasicDescription,
    AudioTimeStamp, AudioTimeStampFlags,
};
use std::ffi::c_void;
use std::panic::AssertUnwindSafe;
use std::ptr::{self, NonNull};
use std::sync::{Arc, Mutex, TryLockError};

type FillFn = Box<dyn FnMut(&mut [f32]) + Send>;

/// How long the device may go without asking for samples before playback counts as failed.
const STALL_NS: u64 = 1_000_000_000;

/// A running output stream. Dropping it stops playback at once.
pub struct AudioOutput {
    unit: Unit,
    render: *mut Render,
    clock: AudioClock,
}

// The unit is only started, stopped and disposed of through `AudioOutput`, which Core Audio
// allows from any thread. `render` belongs to the render callback until the unit is gone.
unsafe impl Send for AudioOutput {}

#[derive(Clone)]
pub struct AudioClock(Arc<Mutex<ClockState>>);

impl AudioOutput {
    pub const SAMPLE_RATE: u32 = 48_000;
    pub const CHANNELS: u16 = 2;

    pub fn start(fill: impl FnMut(&mut [f32]) + Send + 'static) -> anyhow::Result<Self> {
        let clock = AudioClock(Arc::new(Mutex::new(ClockState::new(now_ns()))));
        let unit = Unit::default_output()?;
        unsafe {
            let format = AudioStreamBasicDescription {
                mSampleRate: Self::SAMPLE_RATE as f64,
                mFormatID: kAudioFormatLinearPCM,
                mFormatFlags: kAudioFormatFlagIsFloat | kAudioFormatFlagIsPacked,
                mBytesPerPacket: 4 * Self::CHANNELS as u32,
                mFramesPerPacket: 1,
                mBytesPerFrame: 4 * Self::CHANNELS as u32,
                mChannelsPerFrame: Self::CHANNELS as u32,
                mBitsPerChannel: 32,
                mReserved: 0,
            };
            unit.set(kAudioUnitProperty_StreamFormat, kAudioUnitScope_Input, &format, "48 kHz stereo float")?;
            check(AudioUnitInitialize(unit.0), "AudioUnitInitialize")?;
        }
        let render = Box::into_raw(Box::new(Render {
            fill: Box::new(fill),
            clock: clock.0.clone(),
            rendered: 0,
            latency_ns: unsafe { output_latency_ns(&unit) },
        }));
        let output = Self { unit, render, clock };
        unsafe {
            let callback = AURenderCallbackStruct { inputProc: Some(render_callback), inputProcRefCon: output.render.cast() };
            output.unit.set(kAudioUnitProperty_SetRenderCallback, kAudioUnitScope_Input, &callback, "render callback")?;
            check(AudioOutputUnitStart(output.unit.0), "AudioOutputUnitStart")?;
        }
        Ok(output)
    }

    pub fn clock(&self) -> AudioClock {
        self.clock.clone()
    }
}

impl Drop for AudioOutput {
    fn drop(&mut self) {
        let _ = unsafe { AudioOutputUnitStop(self.unit.0) };
        lock(&self.clock.0).freeze(now_ns());
        drop(std::mem::replace(&mut self.unit, Unit(ptr::null_mut())));
        drop(unsafe { Box::from_raw(self.render) });
    }
}

impl AudioClock {
    /// Milliseconds of audio the device has played. Stays at 0 until the first samples
    /// reach the device, and stops once the output is dropped. If the device stops asking
    /// for samples, it keeps running on the wall clock so video does not freeze.
    pub fn played_ms(&self) -> f64 {
        lock(&self.0).played_ms(now_ns())
    }

    pub fn error(&self) -> Option<String> {
        let mut c = lock(&self.0);
        c.check(now_ns());
        c.error.clone()
    }
}

/// An output unit instance, uninitialized and disposed of on drop.
struct Unit(AudioUnit);

impl Unit {
    fn default_output() -> anyhow::Result<Self> {
        let desc = AudioComponentDescription {
            componentType: kAudioUnitType_Output,
            componentSubType: kAudioUnitSubType_DefaultOutput,
            componentManufacturer: kAudioUnitManufacturer_Apple,
            componentFlags: 0,
            componentFlagsMask: 0,
        };
        let component = unsafe { AudioComponentFindNext(ptr::null_mut(), NonNull::from(&desc)) };
        if component.is_null() {
            bail!("no default output audio unit");
        }
        let mut unit: AudioUnit = ptr::null_mut();
        check(unsafe { AudioComponentInstanceNew(component, NonNull::from(&mut unit)) }, "AudioComponentInstanceNew")?;
        Ok(Self(unit))
    }

    unsafe fn set<T>(&self, id: u32, scope: u32, value: &T, what: &str) -> anyhow::Result<()> {
        let size = size_of::<T>() as u32;
        check(unsafe { AudioUnitSetProperty(self.0, id, scope, 0, (value as *const T).cast(), size) }, what)
    }

    unsafe fn get<T: Copy>(&self, id: u32, scope: u32, mut value: T) -> Option<T> {
        let mut size = size_of::<T>() as u32;
        let status = unsafe { AudioUnitGetProperty(self.0, id, scope, 0, NonNull::from(&mut value).cast(), NonNull::from(&mut size)) };
        (status == 0).then_some(value)
    }
}

impl Drop for Unit {
    fn drop(&mut self) {
        if !self.0.is_null() {
            unsafe {
                AudioUnitUninitialize(self.0);
                AudioComponentInstanceDispose(self.0);
            }
        }
    }
}

fn check(status: i32, what: &str) -> anyhow::Result<()> {
    if status != 0 {
        bail!("{what} failed: OSStatus {status}");
    }
    Ok(())
}

fn lock(m: &Mutex<ClockState>) -> std::sync::MutexGuard<'_, ClockState> {
    m.lock().unwrap_or_else(|e| e.into_inner())
}

fn now_ns() -> u64 {
    unsafe { AudioConvertHostTimeToNanos(AudioGetCurrentHostTime()) }
}

/// Time from a callback's output time to the speaker: the unit's own latency plus the
/// current device's and its first output stream's. Missing properties count as zero.
unsafe fn output_latency_ns(unit: &Unit) -> u64 {
    let unit_s = unsafe { unit.get(kAudioUnitProperty_Latency, kAudioUnitScope_Global, 0.0f64) }.unwrap_or(0.0);
    let device_s = unsafe { unit.get(kAudioOutputUnitProperty_CurrentDevice, kAudioUnitScope_Global, 0 as AudioObjectID) }
        .map(|device| unsafe { device_latency_s(device) })
        .unwrap_or(0.0);
    ((unit_s + device_s).max(0.0) * 1e9) as u64
}

unsafe fn device_latency_s(device: AudioObjectID) -> f64 {
    let output = kAudioObjectPropertyScopeOutput;
    let Some(rate) = (unsafe { object_property(device, kAudioDevicePropertyNominalSampleRate, output, 0.0f64) }) else { return 0.0 };
    if rate <= 0.0 {
        return 0.0;
    }
    let device_frames = unsafe { object_property(device, kAudioDevicePropertyLatency, output, 0u32) }.unwrap_or(0);
    let stream_frames = unsafe { object_property(device, kAudioDevicePropertyStreams, output, [0 as AudioObjectID; 8]) }
        .map(|streams| streams[0])
        .filter(|&s| s != 0)
        .and_then(|s| unsafe { object_property(s, kAudioStreamPropertyLatency, kAudioObjectPropertyScopeGlobal, 0u32) })
        .unwrap_or(0);
    (device_frames + stream_frames) as f64 / rate
}

unsafe fn object_property<T: Copy>(id: AudioObjectID, selector: u32, scope: u32, mut value: T) -> Option<T> {
    let address = AudioObjectPropertyAddress { mSelector: selector, mScope: scope, mElement: kAudioObjectPropertyElementMain };
    let mut size = size_of::<T>() as u32;
    let status = unsafe {
        AudioObjectGetPropertyData(id, NonNull::from(&address), 0, ptr::null(), NonNull::from(&mut size), NonNull::from(&mut value).cast())
    };
    (status == 0).then_some(value)
}

struct Render {
    fill: FillFn,
    clock: Arc<Mutex<ClockState>>,
    rendered: u64,
    latency_ns: u64,
}

unsafe extern "C-unwind" fn render_callback(
    refcon: NonNull<c_void>,
    _flags: NonNull<AudioUnitRenderActionFlags>,
    timestamp: NonNull<AudioTimeStamp>,
    _bus: u32,
    frames: u32,
    data: *mut AudioBufferList,
) -> i32 {
    let r = unsafe { &mut *refcon.as_ptr().cast::<Render>() };
    let Some(list) = (unsafe { data.as_mut() }) else { return 0 };
    let buffer = &mut list.mBuffers[0];
    let len = (frames as usize * AudioOutput::CHANNELS as usize).min(buffer.mDataByteSize as usize / 4);
    let samples = unsafe { std::slice::from_raw_parts_mut(buffer.mData.cast::<f32>(), len) };
    let filled = std::panic::catch_unwind(AssertUnwindSafe(|| (r.fill)(samples)));
    if filled.is_err() {
        samples.fill(0.0);
    }
    let ts = unsafe { timestamp.as_ref() };
    let plays_at = ts
        .mFlags
        .contains(AudioTimeStampFlags::HostTimeValid)
        .then(|| unsafe { AudioConvertHostTimeToNanos(ts.mHostTime) } + r.latency_ns);
    let mut clock = match r.clock.try_lock() {
        Ok(c) => Some(c),
        Err(TryLockError::Poisoned(p)) => Some(p.into_inner()),
        Err(TryLockError::WouldBlock) => None,
    };
    if let Some(c) = clock.as_mut() {
        c.rendered(r.rendered, frames, plays_at, now_ns());
        if filled.is_err() {
            c.fail(now_ns(), "the audio source panicked");
        }
    }
    r.rendered += frames as u64;
    0
}

/// Where playback stands, fed by the render callback and read by [`AudioClock`]. Times are
/// host nanoseconds.
struct ClockState {
    /// A frame and the host time it reaches the speaker.
    anchor: Option<(u64, u64)>,
    rendered: u64,
    last_callback_ns: u64,
    floor_ms: f64,
    frozen: Option<f64>,
    error: Option<String>,
}

impl ClockState {
    fn new(now_ns: u64) -> Self {
        Self { anchor: None, rendered: 0, last_callback_ns: now_ns, floor_ms: 0.0, frozen: None, error: None }
    }

    fn rendered(&mut self, first_frame: u64, frames: u32, plays_at_ns: Option<u64>, now_ns: u64) {
        if let Some(at) = plays_at_ns {
            self.anchor = Some((first_frame, at));
        }
        self.rendered = first_frame + frames as u64;
        self.last_callback_ns = now_ns;
    }

    /// Marks playback as failed. From here on the clock runs on the wall clock.
    fn fail(&mut self, now_ns: u64, why: &str) {
        if self.error.is_none() {
            let at = self.estimate(now_ns);
            self.anchor = Some((ms_to_frames(at), now_ns));
            self.error = Some(why.to_string());
        }
    }

    fn check(&mut self, now_ns: u64) {
        if self.frozen.is_none() && now_ns.saturating_sub(self.last_callback_ns) > STALL_NS {
            self.fail(now_ns, "the output device stopped asking for samples");
        }
    }

    fn played_ms(&mut self, now_ns: u64) -> f64 {
        if let Some(ms) = self.frozen {
            return ms;
        }
        self.check(now_ns);
        self.floor_ms = self.estimate(now_ns);
        self.floor_ms
    }

    /// Runs forward from the anchor, capped at the frames rendered while the device works,
    /// and never below what was already reported.
    fn estimate(&self, now_ns: u64) -> f64 {
        let Some((frame, at)) = self.anchor else { return self.floor_ms };
        let mut ms = frames_to_ms(frame) + (now_ns as f64 - at as f64) / 1e6;
        if self.error.is_none() {
            ms = ms.min(frames_to_ms(self.rendered));
        }
        self.floor_ms.max(ms)
    }

    fn freeze(&mut self, now_ns: u64) {
        if self.frozen.is_none() {
            let ms = self.played_ms(now_ns);
            self.frozen = Some(ms);
        }
    }
}

fn frames_to_ms(frames: u64) -> f64 {
    frames as f64 * 1000.0 / AudioOutput::SAMPLE_RATE as f64
}

fn ms_to_frames(ms: f64) -> u64 {
    (ms * AudioOutput::SAMPLE_RATE as f64 / 1000.0) as u64
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::{Duration, Instant};

    const MS: u64 = 1_000_000;

    #[test]
    fn clock_waits_for_the_first_frame_to_reach_the_speaker() {
        let mut c = ClockState::new(0);
        assert_eq!(c.played_ms(5 * MS), 0.0, "nothing rendered yet");
        c.rendered(0, 480, Some(30 * MS), 10 * MS);
        assert_eq!(c.played_ms(20 * MS), 0.0, "the first frame plays at 30 ms");
        assert_eq!(c.played_ms(35 * MS), 5.0);
        assert_eq!(c.played_ms(60 * MS), 10.0, "never past the 480 frames rendered");
    }

    #[test]
    fn clock_follows_new_anchors_and_never_runs_backwards() {
        let mut c = ClockState::new(0);
        c.rendered(0, 480, Some(10 * MS), 0);
        c.rendered(480, 480, Some(20 * MS), 10 * MS);
        assert_eq!(c.played_ms(25 * MS), 15.0);
        c.rendered(960, 480, Some(32 * MS), 20 * MS);
        assert_eq!(c.played_ms(26 * MS), 15.0, "a later anchor does not take time back");
        assert_eq!(c.played_ms(35 * MS), 23.0);
        assert!(c.error.is_none());
    }

    #[test]
    fn clock_stops_when_frozen() {
        let mut c = ClockState::new(0);
        c.rendered(0, 48_000, Some(0), 0);
        c.freeze(100 * MS);
        assert_eq!(c.played_ms(500 * MS), 100.0);
        assert!(c.error.is_none(), "a stopped output is not a stalled one");
        c.check(5_000 * MS);
        assert!(c.error.is_none());
    }

    #[test]
    fn a_stalled_device_reports_an_error_and_falls_back_to_the_wall_clock() {
        let mut c = ClockState::new(0);
        c.rendered(0, 480, Some(0), 0);
        assert_eq!(c.played_ms(500 * MS), 10.0);
        assert!(c.error.is_none());
        assert_eq!(c.played_ms(1_100 * MS), 10.0, "the stall is noticed here");
        assert!(c.error.is_some());
        assert_eq!(c.played_ms(1_300 * MS), 210.0, "then time keeps going");
    }

    #[test]
    #[ignore = "needs an audio output device"]
    fn output_clock_advances_with_played_silence() {
        let out = AudioOutput::start(|buf: &mut [f32]| buf.fill(0.0)).unwrap();
        let clock = out.clock();
        let started = Instant::now();
        while clock.played_ms() == 0.0 {
            assert!(started.elapsed() < Duration::from_secs(2), "the device never started playing");
            std::thread::sleep(Duration::from_millis(2));
        }
        let first = clock.played_ms();
        let t0 = Instant::now();
        std::thread::sleep(Duration::from_millis(500));
        let played = clock.played_ms() - first;
        let wall = t0.elapsed().as_secs_f64() * 1000.0;
        assert!((played - wall).abs() <= 30.0, "played {played:.1} ms in {wall:.1} ms");
        assert!(clock.error().is_none());
        drop(out);
        let stopped = clock.played_ms();
        std::thread::sleep(Duration::from_millis(100));
        assert_eq!(clock.played_ms(), stopped, "the clock stops with the output");
    }
}
