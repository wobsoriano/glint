//! Cameras through `AVCaptureSession`: `camera.mp4` on the media clock, and live preview frames.

use super::av::{cf_key, constant, number, settings};
use super::writer::PixelWriter;
use crate::clock::MediaClock;
use crate::devices::DeviceId;
use crate::writer::{VideoCodec, VideoParams};
use anyhow::{anyhow, bail, Context};
use block2::RcBlock;
use dispatch2::{DispatchQueue, DispatchRetained};
use image::RgbaImage;
use objc2::rc::Retained;
use objc2::runtime::{Bool, NSObject, NSObjectProtocol, ProtocolObject};
use objc2::{define_class, msg_send, AllocAnyThread, DefinedClass};
use objc2_av_foundation::{
    AVAuthorizationStatus, AVCaptureConnection, AVCaptureDevice, AVCaptureDeviceInput, AVCaptureInput, AVCaptureOutput, AVCaptureSession,
    AVCaptureSessionPreset1280x720, AVCaptureSessionPreset1920x1080, AVCaptureSessionPreset640x480, AVCaptureVideoDataOutput,
    AVCaptureVideoDataOutputSampleBufferDelegate, AVMediaTypeVideo,
};
use objc2_core_media::{CMSampleBuffer, CMTime};
use objc2_core_video::{
    kCVPixelBufferPixelFormatTypeKey, kCVPixelFormatType_32BGRA, CVPixelBuffer, CVPixelBufferGetBaseAddress, CVPixelBufferGetBytesPerRow,
    CVPixelBufferGetHeight, CVPixelBufferGetWidth, CVPixelBufferLockBaseAddress, CVPixelBufferLockFlags, CVPixelBufferUnlockBaseAddress,
};
use objc2_foundation::NSString;
use std::path::{Path, PathBuf};
use std::sync::mpsc;
use std::sync::{Arc, Condvar, Mutex};
use std::time::Duration;

/// How long to wait for the user to answer the camera permission prompt.
const PERMISSION_TIMEOUT: Duration = Duration::from_secs(60);
const FIRST_FRAME_TIMEOUT: Duration = Duration::from_secs(5);
/// Timestamps in microseconds of media time.
const TIMESCALE: i32 = 1_000_000;
/// Nominal rate for the encoder's bitrate; frames carry their own timestamps.
const NOMINAL_FPS: u32 = 30;

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct CameraStats {
    pub frames: u64,
    pub bytes: u64,
    pub width: u32,
    pub height: u32,
}

pub(crate) struct CameraTrack {
    capture: Capture,
    pub path: PathBuf,
}

impl CameraTrack {
    pub fn start(id: &DeviceId, path: &Path, clock: Arc<MediaClock>) -> anyhow::Result<Self> {
        let sink = Sink::Encode { writer: None, clock, path: path.to_path_buf(), last: None };
        let capture = Capture::start(id, sink)?;
        Ok(Self { capture, path: path.to_path_buf() })
    }

    pub fn stop(self) -> anyhow::Result<CameraStats> {
        let (width, height) = self.capture.size;
        let state = self.capture.stop();
        let frames = state.frames;
        let bytes = match state.sink {
            Sink::Encode { writer: Some(writer), clock, last, .. } => {
                if let Some(e) = state.error {
                    return Err(e);
                }
                let now = (clock.now().as_micros() as i64).max(last.map_or(0, |l| l + 1));
                writer.finish(unsafe { CMTime::new(now, TIMESCALE) })?
            }
            _ => match state.error {
                Some(e) => return Err(e),
                None => bail!("the camera sent no frames"),
            },
        };
        Ok(CameraStats { frames, bytes, width, height })
    }
}

/// Live RGBA frames from a camera, for the picker's preview tile. Dropping it stops the camera.
pub struct CameraPreview {
    _capture: Capture,
    width: u32,
    height: u32,
}

impl CameraPreview {
    pub fn start(id: &DeviceId, on_frame: impl FnMut(RgbaImage) + Send + 'static) -> anyhow::Result<Self> {
        let capture = Capture::start(id, Sink::Preview(Box::new(on_frame)))?;
        let (width, height) = capture.size;
        Ok(Self { _capture: capture, width, height })
    }

    pub fn size(&self) -> (u32, u32) {
        (self.width, self.height)
    }
}

enum Sink {
    Encode { writer: Option<PixelWriter>, clock: Arc<MediaClock>, path: PathBuf, last: Option<i64> },
    Preview(Box<dyn FnMut(RgbaImage) + Send>),
}

struct State {
    sink: Sink,
    size: Option<(u32, u32)>,
    frames: u64,
    error: Option<anyhow::Error>,
}

struct Shared {
    state: Mutex<State>,
    first: Condvar,
}

impl Shared {
    fn on_frame(&self, sample: &CMSampleBuffer) {
        let Some(pixels) = (unsafe { sample.image_buffer() }) else { return };
        let mut state = self.state.lock().unwrap();
        if state.error.is_some() {
            return;
        }
        let size = (CVPixelBufferGetWidth(&pixels) as u32, CVPixelBufferGetHeight(&pixels) as u32);
        if state.size.is_none() {
            state.size = Some(size);
            self.first.notify_all();
        }
        match state.write(&pixels, size) {
            Ok(true) => state.frames += 1,
            Ok(false) => {}
            Err(e) => state.error = Some(e),
        }
    }
}

impl State {
    /// Encodes or shows one frame. False when it was skipped because media time is paused.
    fn write(&mut self, pixels: &CVPixelBuffer, (width, height): (u32, u32)) -> anyhow::Result<bool> {
        match &mut self.sink {
            Sink::Encode { writer, clock, path, last } => {
                if clock.is_paused() {
                    return Ok(false);
                }
                let writer = match writer {
                    Some(w) => w,
                    None => {
                        let params = VideoParams {
                            width,
                            height,
                            fps: NOMINAL_FPS,
                            bitrate: VideoParams::recording_bitrate(width, height, NOMINAL_FPS),
                            codec: VideoCodec::H264,
                        };
                        writer.insert(PixelWriter::create(path, params)?)
                    }
                };
                let at = clock.now().as_micros() as i64;
                if last.is_some_and(|l| at <= l) {
                    return Ok(false);
                }
                writer.append(pixels, unsafe { CMTime::new(at, TIMESCALE) })?;
                *last = Some(at);
                Ok(true)
            }
            Sink::Preview(on_frame) => {
                on_frame(to_rgba(pixels, width, height)?);
                Ok(true)
            }
        }
    }
}

fn to_rgba(pixels: &CVPixelBuffer, width: u32, height: u32) -> anyhow::Result<RgbaImage> {
    let status = unsafe { CVPixelBufferLockBaseAddress(pixels, CVPixelBufferLockFlags::ReadOnly) };
    if status != 0 {
        bail!("lock camera frame: CVReturn {status}");
    }
    let pitch = CVPixelBufferGetBytesPerRow(pixels);
    let base = CVPixelBufferGetBaseAddress(pixels) as *const u8;
    let row = width as usize * 4;
    let mut rgba = Vec::with_capacity(row * height as usize);
    for y in 0..height as usize {
        let src = unsafe { std::slice::from_raw_parts(base.add(y * pitch), row) };
        rgba.extend(src.chunks_exact(4).flat_map(|p| [p[2], p[1], p[0], 255]));
    }
    unsafe { CVPixelBufferUnlockBaseAddress(pixels, CVPixelBufferLockFlags::ReadOnly) };
    RgbaImage::from_raw(width, height, rgba).context("camera frame size")
}

define_class!(
    /// Receives the camera's frames on its dispatch queue.
    #[unsafe(super(NSObject))]
    #[name = "GlintCameraOutput"]
    #[ivars = Arc<Shared>]
    struct CameraOutput;

    unsafe impl NSObjectProtocol for CameraOutput {}

    unsafe impl AVCaptureVideoDataOutputSampleBufferDelegate for CameraOutput {
        #[unsafe(method(captureOutput:didOutputSampleBuffer:fromConnection:))]
        fn did_output(&self, _output: &AVCaptureOutput, sample: &CMSampleBuffer, _connection: &AVCaptureConnection) {
            self.ivars().on_frame(sample);
        }
    }
);

impl CameraOutput {
    fn new(shared: Arc<Shared>) -> Retained<Self> {
        let this = Self::alloc().set_ivars(shared);
        unsafe { msg_send![super(this), init] }
    }
}

/// A running capture session. AVFoundation lets any thread start and stop a session, and the
/// frames arrive on `queue`.
struct Capture {
    session: Retained<AVCaptureSession>,
    _output: Retained<CameraOutput>,
    queue: DispatchRetained<DispatchQueue>,
    shared: Arc<Shared>,
    size: (u32, u32),
}

unsafe impl Send for Capture {}

impl Capture {
    fn start(id: &DeviceId, sink: Sink) -> anyhow::Result<Self> {
        let device = unsafe { AVCaptureDevice::deviceWithUniqueID(&NSString::from_str(&id.0)) }
            .with_context(|| format!("no camera with id {id}"))?;
        ensure_access()?;
        let input = unsafe { AVCaptureDeviceInput::deviceInputWithDevice_error(&device) }
            .map_err(|e| anyhow!("open camera {id}: {}", e.localizedDescription()))?;
        let session = unsafe { AVCaptureSession::new() };
        unsafe { session.beginConfiguration() };
        let input: &AVCaptureInput = &input;
        if !unsafe { session.canAddInput(input) } {
            bail!("camera {id} cannot be added to a capture session");
        }
        unsafe { session.addInput(input) };
        let preset = [
            unsafe { AVCaptureSessionPreset1920x1080 },
            unsafe { AVCaptureSessionPreset1280x720 },
            unsafe { AVCaptureSessionPreset640x480 },
        ]
        .into_iter()
        .find(|p| unsafe { session.canSetSessionPreset(p) })
        .context("camera offers no mode up to 1080p")?;
        unsafe { session.setSessionPreset(preset) };

        let output = unsafe { AVCaptureVideoDataOutput::new() };
        let bgra = number(kCVPixelFormatType_32BGRA as i64);
        unsafe {
            output.setVideoSettings(Some(&settings(&[(cf_key(kCVPixelBufferPixelFormatTypeKey), &bgra)])));
            output.setAlwaysDiscardsLateVideoFrames(true);
        }
        let shared = Arc::new(Shared {
            state: Mutex::new(State { sink, size: None, frames: 0, error: None }),
            first: Condvar::new(),
        });
        let delegate = CameraOutput::new(shared.clone());
        let queue = DispatchQueue::new("glint.camera", None);
        unsafe { output.setSampleBufferDelegate_queue(Some(ProtocolObject::from_ref(&*delegate)), Some(&queue)) };
        let as_output: &AVCaptureOutput = &output;
        if !unsafe { session.canAddOutput(as_output) } {
            bail!("camera {id} cannot produce BGRA frames");
        }
        unsafe { session.addOutput(as_output) };
        unsafe { session.commitConfiguration() };
        unsafe { session.startRunning() };

        let mut capture = Self { session, _output: delegate, queue, shared, size: (0, 0) };
        let state = capture.shared.state.lock().unwrap();
        let (state, timeout) = capture.shared.first.wait_timeout_while(state, FIRST_FRAME_TIMEOUT, |s| s.size.is_none() && s.error.is_none()).unwrap();
        let size = state.size;
        let failed = state.error.as_ref().map(|e| anyhow!("{e:#}"));
        drop(state);
        match (size, failed) {
            (_, Some(e)) => {
                capture.stop();
                Err(e)
            }
            (Some(size), None) => {
                capture.size = size;
                Ok(capture)
            }
            (None, None) => {
                let _ = timeout;
                capture.stop();
                bail!("camera {id} sent no frame within {FIRST_FRAME_TIMEOUT:?}")
            }
        }
    }

    /// Stops the session and waits for the frame in flight, then hands back the sink.
    fn stop(self) -> State {
        let shared = self.shared.clone();
        drop(self);
        let mut state = shared.state.lock().unwrap();
        std::mem::replace(&mut *state, State { sink: Sink::Preview(Box::new(|_| {})), size: None, frames: 0, error: None })
    }
}

impl Drop for Capture {
    fn drop(&mut self) {
        unsafe { self.session.stopRunning() };
        self.queue.exec_sync(|| {});
    }
}

/// Asks for camera access when the user has not decided yet, and fails when it is denied.
fn ensure_access() -> anyhow::Result<()> {
    let video = constant(unsafe { AVMediaTypeVideo });
    let denied = || anyhow!("Glint needs the Camera permission; turn it on in System Settings > Privacy & Security > Camera");
    match unsafe { AVCaptureDevice::authorizationStatusForMediaType(video) } {
        AVAuthorizationStatus::Authorized => Ok(()),
        AVAuthorizationStatus::NotDetermined => {
            let (tx, rx) = mpsc::channel();
            let block = RcBlock::new(move |granted: Bool| {
                let _ = tx.send(granted.as_bool());
            });
            unsafe { AVCaptureDevice::requestAccessForMediaType_completionHandler(video, &block) };
            match rx.recv_timeout(PERMISSION_TIMEOUT) {
                Ok(true) => Ok(()),
                Ok(false) => Err(denied()),
                Err(_) => bail!("the camera permission prompt was not answered within {PERMISSION_TIMEOUT:?}"),
            }
        }
        _ => Err(denied()),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn a_bogus_camera_id_fails_to_open_without_a_file() {
        let dir = std::env::temp_dir().join("glint-media-camera-test");
        std::fs::create_dir_all(&dir).unwrap();
        let path = dir.join("camera.mp4");
        let _ = std::fs::remove_file(&path);
        let clock = Arc::new(MediaClock::paused());
        let err = CameraTrack::start(&DeviceId("no-such-camera".into()), &path, clock).err().expect("must fail");
        assert!(err.to_string().contains("no camera with id"), "{err:#}");
        assert!(!path.exists(), "no camera.mp4 should be created for a missing camera");
    }

    #[test]
    #[ignore = "needs a camera and the Camera permission"]
    fn records_and_previews_the_first_camera() {
        let camera = crate::devices::cameras().unwrap().into_iter().next().expect("a camera");
        eprintln!("camera {camera:?}");
        let frames = Arc::new(std::sync::atomic::AtomicU32::new(0));
        let preview = {
            let frames = frames.clone();
            CameraPreview::start(&camera.id, move |_| {
                frames.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
            })
            .unwrap()
        };
        std::thread::sleep(Duration::from_secs(1));
        eprintln!("preview {:?}, {} frames", preview.size(), frames.load(std::sync::atomic::Ordering::Relaxed));
        drop(preview);
        assert!(frames.load(std::sync::atomic::Ordering::Relaxed) > 5);

        let path = std::env::temp_dir().join("glint-media-camera-record.mp4");
        let clock = Arc::new(MediaClock::paused());
        let track = CameraTrack::start(&camera.id, &path, clock.clone()).unwrap();
        clock.resume();
        std::thread::sleep(Duration::from_secs(2));
        clock.pause();
        let stats = track.stop().unwrap();
        eprintln!("{stats:?}");
        let reader = crate::reader::VideoReader::open(&path).unwrap();
        assert_eq!(reader.size(), (stats.width, stats.height));
        assert!(reader.duration_ms().abs_diff(clock.now_ms()) <= 50, "{} ms vs {} ms", reader.duration_ms(), clock.now_ms());
        assert!(stats.width * stats.height <= 1920 * 1080);
    }
}
