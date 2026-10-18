//! Microphone capture through `AVCaptureSession`. The audio data output asks AVFoundation for
//! 48 kHz mono f32, and whatever format actually arrives is decoded from the sample buffer's
//! stream description and converted with [`Converter`].

use super::av::{constant, ns_error, number, settings};
use crate::audio::{AudioStats, ClockedWav, Converter, LevelMeter, SAMPLE_RATE};
use crate::clock::MediaClock;
use crate::devices::{AudioDevice, DeviceId};
use anyhow::{bail, Context};
use block2::RcBlock;
use dispatch2::{DispatchQueue, DispatchRetained};
use objc2::rc::Retained;
use objc2::runtime::{Bool, NSObject, NSObjectProtocol, ProtocolObject};
use objc2::{define_class, msg_send, AllocAnyThread, DefinedClass};
use objc2_av_foundation::{
    AVAuthorizationStatus, AVCaptureAudioDataOutput, AVCaptureAudioDataOutputSampleBufferDelegate, AVCaptureConnection,
    AVCaptureDevice, AVCaptureDeviceDiscoverySession, AVCaptureDeviceInput, AVCaptureDevicePosition, AVCaptureDeviceType,
    AVCaptureOutput, AVCaptureSession, AVMediaTypeAudio,
};
use objc2_avf_audio::{
    AVFormatIDKey, AVLinearPCMBitDepthKey, AVLinearPCMIsBigEndianKey, AVLinearPCMIsFloatKey, AVLinearPCMIsNonInterleaved,
    AVNumberOfChannelsKey, AVSampleRateKey,
};
use objc2_core_audio_types::{
    kAudioFormatFlagIsBigEndian, kAudioFormatFlagIsFloat, kAudioFormatFlagIsNonInterleaved, kAudioFormatLinearPCM,
    AudioStreamBasicDescription,
};
use objc2_core_media::{CMAudioFormatDescriptionGetStreamBasicDescription, CMSampleBuffer};
use objc2_foundation::{ns_string, NSArray, NSNumber, NSOperatingSystemVersion, NSProcessInfo, NSString};
use std::path::{Path, PathBuf};
use std::ptr::NonNull;
use std::sync::mpsc::{self, SyncSender};
use std::sync::{Arc, Mutex};
use std::time::Duration;

/// How long to wait for the user to answer the microphone permission prompt.
const PERMISSION_TIMEOUT: Duration = Duration::from_secs(60);

/// Connected microphones, the system default input first.
pub(crate) fn microphones() -> anyhow::Result<Vec<AudioDevice>> {
    let audio = constant(unsafe { AVMediaTypeAudio });
    let discovery = unsafe {
        AVCaptureDeviceDiscoverySession::discoverySessionWithDeviceTypes_mediaType_position(
            &microphone_types(),
            Some(audio),
            AVCaptureDevicePosition::Unspecified,
        )
    };
    let describe = |d: &AVCaptureDevice| AudioDevice {
        id: DeviceId(unsafe { d.uniqueID() }.to_string()),
        name: unsafe { d.localizedName() }.to_string(),
    };
    let mut list: Vec<AudioDevice> = unsafe { discovery.devices() }.iter().map(|d| describe(&d)).collect();
    if let Some(default) = unsafe { AVCaptureDevice::defaultDeviceWithMediaType(audio) } {
        let default = describe(&default);
        list.retain(|d| d.id != default.id);
        list.insert(0, default);
    }
    Ok(list)
}

/// `AVCaptureDeviceTypeMicrophone` only exists from macOS 14, and its symbol would keep the
/// binary from loading on 13, so it is named by its value. Older systems use the types it
/// replaced.
#[allow(deprecated)]
fn microphone_types() -> Retained<NSArray<AVCaptureDeviceType>> {
    let sonoma = NSOperatingSystemVersion { majorVersion: 14, minorVersion: 0, patchVersion: 0 };
    if NSProcessInfo::processInfo().isOperatingSystemAtLeastVersion(sonoma) {
        NSArray::from_slice(&[ns_string!("AVCaptureDeviceTypeMicrophone")])
    } else {
        use objc2_av_foundation::{AVCaptureDeviceTypeBuiltInMicrophone, AVCaptureDeviceTypeExternalUnknown};
        NSArray::from_slice(&[unsafe { AVCaptureDeviceTypeBuiltInMicrophone }, unsafe { AVCaptureDeviceTypeExternalUnknown }])
    }
}

/// A microphone recorded to a mono WAV at [`SAMPLE_RATE`] that follows the media clock.
pub(crate) struct AudioTrack {
    capture: Capture,
    level: Arc<LevelMeter>,
    pub path: PathBuf,
}

impl AudioTrack {
    pub fn start_mic(id: &DeviceId, path: &Path, clock: Arc<MediaClock>, tap: Option<SyncSender<Vec<f32>>>) -> anyhow::Result<Self> {
        let level = Arc::new(LevelMeter::default());
        let capture = Capture::start(Some(id), level.clone(), tap, || ClockedWav::create(path, 1, clock).map(Some))?;
        Ok(Self { capture, level, path: path.to_path_buf() })
    }

    pub fn level(&self) -> &Arc<LevelMeter> {
        &self.level
    }

    pub fn stop(mut self) -> anyhow::Result<AudioStats> {
        self.capture.stop()
    }
}

pub struct MicMonitor {
    _capture: Capture,
    level: Arc<LevelMeter>,
}

impl MicMonitor {
    /// Starts metering `mic`, or the default microphone when `None`.
    pub fn start(mic: Option<DeviceId>) -> anyhow::Result<Self> {
        let level = Arc::new(LevelMeter::default());
        let capture = Capture::start(mic.as_ref(), level.clone(), None, || Ok(None))?;
        Ok(Self { _capture: capture, level })
    }

    pub fn level(&self) -> f32 {
        self.level.rms()
    }
}

/// A running capture session. Dropping it stops the session and finishes the WAV.
struct Capture {
    session: Retained<AVCaptureSession>,
    delegate: Retained<MicDelegate>,
    queue: DispatchRetained<DispatchQueue>,
    running: bool,
}

// AVCaptureSession may be started and stopped from any thread, the dispatch queue is
// thread-safe, and the delegate's state sits behind a mutex.
unsafe impl Send for Capture {}
unsafe impl Sync for Capture {}

impl Capture {
    fn start(
        mic: Option<&DeviceId>,
        level: Arc<LevelMeter>,
        tap: Option<SyncSender<Vec<f32>>>,
        make_sink: impl FnOnce() -> anyhow::Result<Option<ClockedWav>>,
    ) -> anyhow::Result<Self> {
        authorize()?;
        let audio = constant(unsafe { AVMediaTypeAudio });
        let device = match mic {
            None => unsafe { AVCaptureDevice::defaultDeviceWithMediaType(audio) }.context("no microphone is connected")?,
            Some(id) => unsafe { AVCaptureDevice::deviceWithUniqueID(&NSString::from_str(&id.0)) }
                .with_context(|| format!("microphone {id} is not connected"))?,
        };
        let name = unsafe { device.localizedName() };
        let input = unsafe { AVCaptureDeviceInput::deviceInputWithDevice_error(&device) }
            .map_err(|e| ns_error(Some(e), &format!("open microphone {name}")))?;

        let output = unsafe { AVCaptureAudioDataOutput::new() };
        unsafe { output.setAudioSettings(Some(&mono_float_settings())) };
        let delegate = MicDelegate::new(Pipeline::new(make_sink()?, level, tap));
        let queue = DispatchQueue::new("glint.mic", None);
        unsafe { output.setSampleBufferDelegate_queue(Some(ProtocolObject::from_ref(&*delegate)), Some(&queue)) };

        let session = unsafe { AVCaptureSession::new() };
        unsafe {
            session.beginConfiguration();
            if !session.canAddInput(&input) {
                session.commitConfiguration();
                bail!("the capture session cannot use microphone {name}");
            }
            session.addInput(&input);
            if !session.canAddOutput(&output) {
                session.commitConfiguration();
                bail!("the capture session cannot deliver audio from {name}");
            }
            session.addOutput(&output);
            session.commitConfiguration();
            session.startRunning();
        }
        if !unsafe { session.isRunning() } {
            bail!("microphone {name} did not start");
        }
        Ok(Self { session, delegate, queue, running: true })
    }

    /// Stops the session, waits for the last buffer, and finishes the WAV. Later calls
    /// return empty stats.
    fn stop(&mut self) -> anyhow::Result<AudioStats> {
        if self.running {
            unsafe { self.session.stopRunning() };
            self.queue.exec_sync(|| {});
            self.running = false;
        }
        self.delegate.ivars().lock().unwrap_or_else(|e| e.into_inner()).finish()
    }
}

impl Drop for Capture {
    fn drop(&mut self) {
        let _ = self.stop();
    }
}

/// Fails unless the user has allowed microphone access, asking first when they have not
/// answered yet.
fn authorize() -> anyhow::Result<()> {
    let audio = constant(unsafe { AVMediaTypeAudio });
    match unsafe { AVCaptureDevice::authorizationStatusForMediaType(audio) } {
        AVAuthorizationStatus::Authorized => Ok(()),
        AVAuthorizationStatus::NotDetermined => {
            let (tx, rx) = mpsc::channel();
            let block = RcBlock::new(move |granted: Bool| {
                let _ = tx.send(granted.as_bool());
            });
            unsafe { AVCaptureDevice::requestAccessForMediaType_completionHandler(audio, &block) };
            match rx.recv_timeout(PERMISSION_TIMEOUT) {
                Ok(true) => Ok(()),
                Ok(false) => bail!("microphone access was denied"),
                Err(_) => bail!("the microphone permission prompt was not answered"),
            }
        }
        AVAuthorizationStatus::Restricted => bail!("microphone access is restricted on this Mac"),
        _ => bail!("microphone access is denied; allow it in System Settings > Privacy & Security > Microphone"),
    }
}

fn mono_float_settings() -> Retained<super::av::Settings> {
    let (lpcm, rate, one, bits, yes, no) = (
        number(kAudioFormatLinearPCM as i64),
        number(SAMPLE_RATE as i64),
        number(1),
        number(32),
        NSNumber::new_bool(true),
        NSNumber::new_bool(false),
    );
    settings(&[
        (constant(unsafe { AVFormatIDKey }), &lpcm),
        (constant(unsafe { AVSampleRateKey }), &rate),
        (constant(unsafe { AVNumberOfChannelsKey }), &one),
        (constant(unsafe { AVLinearPCMBitDepthKey }), &bits),
        (constant(unsafe { AVLinearPCMIsFloatKey }), &yes),
        (constant(unsafe { AVLinearPCMIsNonInterleaved }), &no),
        (constant(unsafe { AVLinearPCMIsBigEndianKey }), &no),
    ])
}

define_class!(
    #[unsafe(super(NSObject))]
    #[name = "GlintMicDelegate"]
    #[ivars = Mutex<Pipeline>]
    struct MicDelegate;

    unsafe impl NSObjectProtocol for MicDelegate {}

    unsafe impl AVCaptureAudioDataOutputSampleBufferDelegate for MicDelegate {
        #[unsafe(method(captureOutput:didOutputSampleBuffer:fromConnection:))]
        fn did_output(&self, _output: &AVCaptureOutput, sample: &CMSampleBuffer, _connection: &AVCaptureConnection) {
            self.ivars().lock().unwrap_or_else(|e| e.into_inner()).ingest(sample);
        }
    }
);

impl MicDelegate {
    fn new(pipeline: Pipeline) -> Retained<Self> {
        let this = Self::alloc().set_ivars(Mutex::new(pipeline));
        unsafe { msg_send![super(this), init] }
    }
}

/// What happens to every captured buffer: decode, convert to 48 kHz mono, meter, offer to
/// the tap, write. The first failure stops the pipeline and `finish` returns it.
struct Pipeline {
    sink: Option<ClockedWav>,
    level: Arc<LevelMeter>,
    tap: Option<SyncSender<Vec<f32>>>,
    converter: Option<(PcmFormat, Converter)>,
    raw: Vec<u8>,
    decoded: Vec<f32>,
    out: Vec<f32>,
    failed: Option<anyhow::Error>,
}

impl Pipeline {
    fn new(sink: Option<ClockedWav>, level: Arc<LevelMeter>, tap: Option<SyncSender<Vec<f32>>>) -> Self {
        Self { sink, level, tap, converter: None, raw: Vec::new(), decoded: Vec::new(), out: Vec::new(), failed: None }
    }

    fn ingest(&mut self, sample: &CMSampleBuffer) {
        let mut raw = std::mem::take(&mut self.raw);
        match read_sample(sample, &mut raw) {
            Ok(format) => self.push(format, &raw),
            Err(e) => self.fail(e),
        }
        self.raw = raw;
    }

    fn push(&mut self, format: PcmFormat, raw: &[u8]) {
        if self.failed.is_some() {
            return;
        }
        if let Err(e) = self.convert_and_write(format, raw) {
            self.fail(e);
        }
    }

    fn convert_and_write(&mut self, format: PcmFormat, raw: &[u8]) -> anyhow::Result<()> {
        let converter = match &mut self.converter {
            Some((f, c)) if *f == format => c,
            slot => &mut slot.insert((format, Converter::new(format.rate, format.channels, 1))).1,
        };
        self.decoded.clear();
        format.decode(raw, &mut self.decoded);
        self.out.clear();
        converter.push(&self.decoded, &mut self.out);
        self.level.update(&self.out);
        if let Some(tap) = &self.tap {
            let _ = tap.try_send(self.out.clone());
        }
        match &mut self.sink {
            Some(wav) => wav.write(&self.out),
            None => Ok(()),
        }
    }

    fn fail(&mut self, e: anyhow::Error) {
        self.failed.get_or_insert(e);
    }

    fn finish(&mut self) -> anyhow::Result<AudioStats> {
        if let Some(e) = self.failed.take() {
            self.sink = None;
            return Err(e);
        }
        match self.sink.take() {
            Some(wav) => wav.finish(),
            None => Ok(AudioStats { frames: 0, bytes: 0 }),
        }
    }
}

/// Copies the sample buffer's bytes into `raw` and returns their format.
fn read_sample(sample: &CMSampleBuffer, raw: &mut Vec<u8>) -> anyhow::Result<PcmFormat> {
    let desc = unsafe { sample.format_description() }.context("microphone buffer has no format")?;
    let asbd = unsafe { CMAudioFormatDescriptionGetStreamBasicDescription(&desc).as_ref() }.context("microphone buffer is not audio")?;
    let format = PcmFormat::parse(asbd)?;
    let data = unsafe { sample.data_buffer() }.context("microphone buffer has no data")?;
    let len = unsafe { data.data_length() };
    raw.resize(len, 0);
    if len > 0 {
        let status = unsafe { data.copy_data_bytes(0, len, NonNull::new_unchecked(raw.as_mut_ptr().cast())) };
        if status != 0 {
            bail!("copy microphone samples: OSStatus {status}");
        }
    }
    Ok(format)
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum SampleKind {
    F32,
    I16,
    I24,
    I32,
}

impl SampleKind {
    fn width(self) -> usize {
        match self {
            SampleKind::I16 => 2,
            SampleKind::I24 => 3,
            SampleKind::F32 | SampleKind::I32 => 4,
        }
    }
}

/// Linear PCM as a capture device delivers it: little-endian, packed, interleaved or one
/// plane per channel.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct PcmFormat {
    rate: u32,
    channels: u16,
    kind: SampleKind,
    interleaved: bool,
}

impl PcmFormat {
    fn parse(d: &AudioStreamBasicDescription) -> anyhow::Result<Self> {
        if d.mFormatID != kAudioFormatLinearPCM {
            bail!("microphone delivers format {:#x}, not linear PCM", d.mFormatID);
        }
        let flags = d.mFormatFlags;
        if flags & kAudioFormatFlagIsBigEndian != 0 {
            bail!("microphone delivers big-endian samples");
        }
        let kind = match (flags & kAudioFormatFlagIsFloat != 0, d.mBitsPerChannel) {
            (true, 32) => SampleKind::F32,
            (false, 16) => SampleKind::I16,
            (false, 24) => SampleKind::I24,
            (false, 32) => SampleKind::I32,
            (float, bits) => bail!("microphone delivers unsupported {bits}-bit {} samples", if float { "float" } else { "integer" }),
        };
        let interleaved = flags & kAudioFormatFlagIsNonInterleaved == 0;
        let channels = d.mChannelsPerFrame;
        let per_frame = if interleaved { kind.width() * channels as usize } else { kind.width() };
        if channels == 0 || channels > u16::MAX as u32 || d.mBytesPerFrame as usize != per_frame || d.mSampleRate < 1.0 {
            bail!("microphone delivers an unsupported layout: {} channels, {} bytes per frame, {} Hz", channels, d.mBytesPerFrame, d.mSampleRate);
        }
        Ok(Self { rate: d.mSampleRate.round() as u32, channels: channels as u16, kind, interleaved })
    }

    /// Appends `raw` to `out` as interleaved f32.
    fn decode(&self, raw: &[u8], out: &mut Vec<f32>) {
        let (width, channels) = (self.kind.width(), self.channels as usize);
        let frames = raw.len() / (width * channels);
        let sample = |b: &[u8]| match self.kind {
            SampleKind::F32 => f32::from_le_bytes([b[0], b[1], b[2], b[3]]),
            SampleKind::I16 => i16::from_le_bytes([b[0], b[1]]) as f32 / 32_768.0,
            SampleKind::I24 => (i32::from_le_bytes([0, b[0], b[1], b[2]]) >> 8) as f32 / 8_388_608.0,
            SampleKind::I32 => i32::from_le_bytes([b[0], b[1], b[2], b[3]]) as f32 / 2_147_483_648.0,
        };
        if self.interleaved {
            out.extend(raw[..frames * channels * width].chunks_exact(width).map(sample));
        } else {
            let plane = frames * width;
            for f in 0..frames {
                for c in 0..channels {
                    let at = c * plane + f * width;
                    out.push(sample(&raw[at..at + width]));
                }
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::wav::read_wav;
    use objc2_core_audio_types::kAudioFormatFlagIsPacked;

    fn asbd(rate: f64, channels: u32, bits: u32, flags: u32) -> AudioStreamBasicDescription {
        let width = bits / 8;
        let per_frame = if flags & kAudioFormatFlagIsNonInterleaved != 0 { width } else { width * channels };
        AudioStreamBasicDescription {
            mSampleRate: rate,
            mFormatID: kAudioFormatLinearPCM,
            mFormatFlags: flags | kAudioFormatFlagIsPacked,
            mBytesPerPacket: per_frame,
            mFramesPerPacket: 1,
            mBytesPerFrame: per_frame,
            mChannelsPerFrame: channels,
            mBitsPerChannel: bits,
            mReserved: 0,
        }
    }

    fn bytes_f32(samples: &[f32]) -> Vec<u8> {
        samples.iter().flat_map(|s| s.to_le_bytes()).collect()
    }

    #[test]
    fn parses_and_decodes_interleaved_integers() {
        let f = PcmFormat::parse(&asbd(44_100.0, 2, 16, 0)).unwrap();
        assert_eq!(f, PcmFormat { rate: 44_100, channels: 2, kind: SampleKind::I16, interleaved: true });
        let raw: Vec<u8> = [16_384i16, -32_768, 0, 32_767].iter().flat_map(|s| s.to_le_bytes()).collect();
        let mut out = Vec::new();
        f.decode(&raw, &mut out);
        assert_eq!(out, vec![0.5, -1.0, 0.0, 32_767.0 / 32_768.0]);

        let f = PcmFormat::parse(&asbd(48_000.0, 1, 24, 0)).unwrap();
        let mut out = Vec::new();
        f.decode(&[0x00, 0x00, 0xc0], &mut out);
        assert_eq!(out, vec![-0.5], "24-bit samples are sign-extended");
    }

    #[test]
    fn interleaves_planar_float() {
        let f = PcmFormat::parse(&asbd(48_000.0, 2, 32, kAudioFormatFlagIsFloat | kAudioFormatFlagIsNonInterleaved)).unwrap();
        assert!(!f.interleaved);
        let mut out = Vec::new();
        f.decode(&bytes_f32(&[0.1, 0.2, 0.3, -0.1, -0.2, -0.3]), &mut out);
        assert_eq!(out, vec![0.1, -0.1, 0.2, -0.2, 0.3, -0.3]);
    }

    #[test]
    fn rejects_formats_it_cannot_decode() {
        assert!(PcmFormat::parse(&asbd(48_000.0, 1, 32, kAudioFormatFlagIsFloat | kAudioFormatFlagIsBigEndian)).is_err());
        assert!(PcmFormat::parse(&asbd(48_000.0, 1, 8, 0)).is_err());
        assert!(PcmFormat::parse(&asbd(48_000.0, 0, 16, 0)).is_err());
        let mut aac = asbd(48_000.0, 1, 16, 0);
        aac.mFormatID = objc2_core_audio_types::kAudioFormatMPEG4AAC;
        assert!(PcmFormat::parse(&aac).is_err());
    }

    #[test]
    fn pipeline_converts_meters_and_never_waits_on_the_tap() {
        let level = Arc::new(LevelMeter::default());
        let (tx, rx) = mpsc::sync_channel(1);
        let mut p = Pipeline::new(None, level.clone(), Some(tx));
        let stereo_44k = PcmFormat { rate: 44_100, channels: 2, kind: SampleKind::F32, interleaved: true };
        let tenth = bytes_f32(&[0.5; 4_410 * 2]);
        for _ in 0..10 {
            p.push(stereo_44k, &tenth);
        }
        assert!((level.rms() - 0.5).abs() < 1e-6);
        let buffers: Vec<Vec<f32>> = rx.try_iter().collect();
        assert_eq!(buffers.len(), 1, "a full one-slot tap drops buffers instead of blocking");
        assert!((buffers[0].len() as i64 - 4_800).abs() <= 2, "the tap gets 48 kHz mono, got {} samples", buffers[0].len());
        assert_eq!(p.finish().unwrap(), AudioStats { frames: 0, bytes: 0 });
    }

    #[test]
    fn microphone_type_name_matches_the_sdk() {
        assert_eq!(unsafe { objc2_av_foundation::AVCaptureDeviceTypeMicrophone }.to_string(), "AVCaptureDeviceTypeMicrophone");
    }

    fn default_mic() -> DeviceId {
        microphones().unwrap().into_iter().next().expect("a microphone").id
    }

    #[test]
    #[ignore = "needs a microphone and microphone permission"]
    fn lists_the_default_microphone_first() {
        let mics = microphones().unwrap();
        assert!(!mics.is_empty());
        let audio = constant(unsafe { AVMediaTypeAudio });
        let default = unsafe { AVCaptureDevice::defaultDeviceWithMediaType(audio) }.unwrap();
        assert_eq!(mics[0].id.0, unsafe { default.uniqueID() }.to_string());
    }

    #[test]
    #[ignore = "needs a microphone and microphone permission"]
    fn mic_track_matches_media_time_across_a_pause() {
        let dir = std::env::temp_dir().join("glint-media-mac-mic-test");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("mic.wav");
        let clock = Arc::new(MediaClock::paused());
        let track = AudioTrack::start_mic(&default_mic(), &path, clock.clone(), None).unwrap();
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
        assert_eq!((wav.sample_rate, wav.channels), (48_000, 1));
        assert_eq!(wav.frames() as u64, stats.frames);
        let diff = wav.duration_ms() as i64 - media_ms as i64;
        assert!(diff.abs() <= 70, "wav is {} ms, media time {} ms", wav.duration_ms(), media_ms);
        assert!(wav.duration_ms() < 1200, "the 300 ms pause leaked into the file: {} ms", wav.duration_ms());
    }

    #[test]
    #[ignore = "needs a microphone and microphone permission"]
    fn tap_receives_converted_buffers_without_blocking_capture() {
        let dir = std::env::temp_dir().join("glint-media-mac-tap-test");
        std::fs::create_dir_all(&dir).unwrap();
        let clock = Arc::new(MediaClock::paused());
        let (tx, rx) = mpsc::sync_channel(1);
        let track = AudioTrack::start_mic(&default_mic(), &dir.join("mic.wav"), clock.clone(), Some(tx)).unwrap();
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

    #[test]
    #[ignore = "needs a microphone and microphone permission"]
    fn monitor_meters_the_default_microphone() {
        let monitor = MicMonitor::start(None).unwrap();
        std::thread::sleep(Duration::from_millis(500));
        let level = monitor.level();
        assert!(level.is_finite() && level >= 0.0);
        assert!(MicMonitor::start(Some(DeviceId("no-such-mic".into()))).is_err());
    }
}
