//! The screen master: a ScreenCaptureKit stream written to `screen.mp4` at a constant frame
//! rate, and optionally the system audio from the same stream to `system.wav`.
//!
//! ScreenCaptureKit sends a frame only when the content changes. The writer thread follows the
//! media clock instead: frame n has timestamp n/fps, and when frame n is due and no new image
//! arrived, the last one is written again.

use super::sck::shareable_content;
use super::writer::PixelWriter;
use crate::audio::{AudioStats, ClockedWav, Converter};
use crate::clock::MediaClock;
use crate::writer::{VideoCodec, VideoParams};
use anyhow::{anyhow, bail, Context};
use block2::RcBlock;
use dispatch2::DispatchQueue;
use objc2::rc::Retained;
use objc2::runtime::{AnyObject, NSObject, NSObjectProtocol, ProtocolObject};
use objc2::{define_class, msg_send, AllocAnyThread, DefinedClass};
use objc2_core_audio_types::{kAudioFormatFlagIsNonInterleaved, AudioBuffer, AudioBufferList};
use objc2_core_foundation::{CFArray, CFRetained, CGPoint, CGRect, CGSize};
use objc2_core_graphics::kCGColorSpaceSRGB;
use objc2_core_media::{CMAudioFormatDescriptionGetStreamBasicDescription, CMBlockBuffer, CMSampleBuffer, CMTime};
use objc2_core_video::{kCVPixelFormatType_32BGRA, CVPixelBuffer};
use objc2_foundation::{NSArray, NSDictionary, NSError, NSNumber, NSString};
use objc2_screen_capture_kit::{
    SCContentFilter, SCFrameStatus, SCStream, SCStreamConfiguration, SCStreamDelegate, SCStreamFrameInfoStatus, SCStreamOutput,
    SCStreamOutputType, SCWindow,
};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc;
use std::sync::{Arc, Mutex};
use std::thread::JoinHandle;
use std::time::{Duration, Instant};

/// Frames ScreenCaptureKit may have in flight. The writer holds one and the encoder a few.
const QUEUE_DEPTH: isize = 6;
const TIMEOUT: Duration = Duration::from_secs(5);
const AUDIO_RATE: isize = 48_000;

#[derive(Clone, Copy, Debug, PartialEq)]
pub(crate) enum ScreenSource {
    /// Display `id` (a `CGDirectDisplayID`), or the `crop` of it in points from its top-left.
    Display { id: u32, crop: Option<[f64; 4]> },
    /// Window `id` (a `CGWindowID`) on its own, wherever it moves.
    Window { id: u32 },
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct ScreenStats {
    pub frames: u64,
    pub bytes: u64,
    pub source_closed: bool,
}

/// How the screen track ended: the video, and the system audio when it was recorded.
pub(crate) struct ScreenEnd {
    pub screen: anyhow::Result<ScreenStats>,
    pub system: Option<anyhow::Result<AudioStats>>,
}

pub(crate) struct ScreenTrack {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<ScreenEnd>>,
    pub path: PathBuf,
    /// `system.wav`, when system audio is recording.
    pub system_path: Option<PathBuf>,
    pub warnings: Vec<String>,
}

impl ScreenTrack {
    /// Starts capturing `source` at `size` pixels, and the system audio to `system`
    /// when given. A system audio failure becomes a warning; the screen still records.
    pub fn start(
        source: ScreenSource,
        size: [u32; 2],
        fps: u32,
        path: &Path,
        system: Option<&Path>,
        clock: Arc<MediaClock>,
    ) -> anyhow::Result<Self> {
        let stop = Arc::new(AtomicBool::new(false));
        let (tx, rx) = mpsc::channel::<anyhow::Result<(Option<PathBuf>, Vec<String>)>>();
        let (out, system) = (path.to_path_buf(), system.map(Path::to_path_buf));
        let thread = {
            let stop = stop.clone();
            std::thread::Builder::new().name("glint-screen".into()).spawn(move || {
                let failed = |e| {
                    if let Some(wav) = &system {
                        let _ = std::fs::remove_file(wav);
                    }
                    let _ = tx.send(Err(e));
                    ScreenEnd { screen: Err(anyhow!("did not start")), system: None }
                };
                let mut capture = match Capture::open(source, size, fps, &out, system.clone(), &clock) {
                    Ok(c) => c,
                    Err(e) => return failed(e),
                };
                if let Err(e) = capture.wait_first_frame(Duration::from_secs(3)) {
                    capture.close();
                    drop(capture);
                    return failed(e);
                }
                let system_path = capture.shared.system.lock().unwrap().as_ref().map(|s| s.path.clone());
                let _ = tx.send(Ok((system_path, std::mem::take(&mut capture.warnings))));
                capture.run(&clock, &stop)
            })?
        };
        match rx.recv() {
            Ok(Ok((system_path, warnings))) => Ok(Self { stop, thread: Some(thread), path: path.to_path_buf(), system_path, warnings }),
            Ok(Err(e)) => {
                let _ = thread.join();
                Err(e)
            }
            Err(_) => {
                let _ = thread.join();
                bail!("screen thread exited before capture started")
            }
        }
    }

    pub fn is_live(&self) -> bool {
        self.thread.as_ref().is_some_and(|t| !t.is_finished())
    }

    pub fn stop(mut self) -> ScreenEnd {
        self.stop.store(true, Ordering::Relaxed);
        let thread = self.thread.take().unwrap();
        thread.join().unwrap_or_else(|_| ScreenEnd { screen: Err(anyhow!("screen thread panicked")), system: None })
    }
}

impl Drop for ScreenTrack {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

/// A pixel buffer handed from ScreenCaptureKit's queue to the writer thread. Neither side
/// writes to it.
struct Frame(CFRetained<CVPixelBuffer>);
unsafe impl Send for Frame {}

struct SystemAudio {
    path: PathBuf,
    wav: ClockedWav,
    /// Built from the first buffer's format.
    converter: Option<Converter>,
    pcm: Vec<f32>,
    out: Vec<f32>,
    error: Option<anyhow::Error>,
}

/// What the stream's queues share with the writer thread.
#[derive(Default)]
struct Shared {
    latest: Mutex<Option<Frame>>,
    closed: AtomicBool,
    system: Mutex<Option<SystemAudio>>,
}

impl Shared {
    fn on_screen(&self, sample: &CMSampleBuffer) {
        if frame_status(sample) != Some(SCFrameStatus::Complete) {
            return;
        }
        if let Some(pixels) = unsafe { sample.image_buffer() } {
            *self.latest.lock().unwrap() = Some(Frame(pixels));
        }
    }

    fn on_audio(&self, sample: &CMSampleBuffer) {
        let mut system = self.system.lock().unwrap();
        let Some(system) = system.as_mut().filter(|s| s.error.is_none()) else { return };
        if let Err(e) = system.write(sample) {
            system.error = Some(e);
        }
    }
}

impl SystemAudio {
    fn write(&mut self, sample: &CMSampleBuffer) -> anyhow::Result<()> {
        let format = unsafe { sample.format_description() }.context("audio buffer without a format")?;
        let asbd = unsafe { CMAudioFormatDescriptionGetStreamBasicDescription(&format).as_ref() }.context("audio format")?;
        let channels = asbd.mChannelsPerFrame.max(1) as usize;
        let planar = asbd.mFormatFlags & kAudioFormatFlagIsNonInterleaved != 0;
        let converter = self.converter.get_or_insert_with(|| Converter::new(asbd.mSampleRate as u32, channels as u16, 2));

        let mut needed = 0usize;
        let status = unsafe {
            sample.audio_buffer_list_with_retained_block_buffer(&mut needed, std::ptr::null_mut(), 0, None, None, 0, std::ptr::null_mut())
        };
        if status != 0 {
            bail!("size system audio buffers: OSStatus {status}");
        }
        let mut storage = vec![0u64; needed.div_ceil(8)];
        let list = storage.as_mut_ptr() as *mut AudioBufferList;
        let mut block: *mut CMBlockBuffer = std::ptr::null_mut();
        let status = unsafe { sample.audio_buffer_list_with_retained_block_buffer(std::ptr::null_mut(), list, needed, None, None, 0, &mut block) };
        if status != 0 {
            bail!("read system audio: OSStatus {status}");
        }
        let _block = std::ptr::NonNull::new(block).map(|b| unsafe { CFRetained::from_raw(b) });
        let count = unsafe { (*list).mNumberBuffers } as usize;
        let first = unsafe { std::ptr::addr_of!((*list).mBuffers) as *const AudioBuffer };
        let buffers: Vec<&[f32]> = (0..count)
            .map(|i| unsafe { &*first.add(i) })
            .map(|b| unsafe { std::slice::from_raw_parts(b.mData as *const f32, b.mDataByteSize as usize / 4) })
            .collect();
        self.pcm.clear();
        if planar {
            if buffers.len() < channels {
                bail!("system audio has {} planes for {channels} channels", buffers.len());
            }
            let frames = buffers.iter().map(|b| b.len()).min().unwrap_or(0);
            for i in 0..frames {
                self.pcm.extend(buffers[..channels].iter().map(|b| b[i]));
            }
        } else if let Some(b) = buffers.first() {
            self.pcm.extend_from_slice(b);
        }
        self.out.clear();
        converter.push(&self.pcm, &mut self.out);
        self.wav.write(&self.out)
    }

    fn finish(self) -> anyhow::Result<AudioStats> {
        let stats = self.wav.finish()?;
        match self.error {
            Some(e) => Err(e.context("system audio stopped")),
            None => Ok(stats),
        }
    }
}

fn frame_status(sample: &CMSampleBuffer) -> Option<SCFrameStatus> {
    let attachments: CFRetained<CFArray> = unsafe { sample.sample_attachments_array(false) }?;
    let attachments = unsafe { &*(&*attachments as *const CFArray as *const NSArray<NSDictionary<NSString, AnyObject>>) };
    let info = attachments.firstObject()?;
    let status = info.objectForKey(unsafe { SCStreamFrameInfoStatus })?;
    let status = status.downcast::<NSNumber>().ok()?;
    Some(SCFrameStatus(status.integerValue()))
}

define_class!(
    /// Receives ScreenCaptureKit's frames, audio and stop notice.
    #[unsafe(super(NSObject))]
    #[name = "GlintScreenOutput"]
    #[ivars = Arc<Shared>]
    struct StreamOutput;

    unsafe impl NSObjectProtocol for StreamOutput {}

    unsafe impl SCStreamOutput for StreamOutput {
        #[unsafe(method(stream:didOutputSampleBuffer:ofType:))]
        fn did_output(&self, _stream: &SCStream, sample: &CMSampleBuffer, kind: SCStreamOutputType) {
            match kind {
                SCStreamOutputType::Screen => self.ivars().on_screen(sample),
                SCStreamOutputType::Audio => self.ivars().on_audio(sample),
                _ => {}
            }
        }
    }

    unsafe impl SCStreamDelegate for StreamOutput {
        #[unsafe(method(stream:didStopWithError:))]
        fn did_stop(&self, _stream: &SCStream, _error: &NSError) {
            self.ivars().closed.store(true, Ordering::Relaxed);
        }
    }
);

impl StreamOutput {
    fn new(shared: Arc<Shared>) -> Retained<Self> {
        let this = Self::alloc().set_ivars(shared);
        unsafe { msg_send![super(this), init] }
    }
}

fn completion() -> (RcBlock<dyn Fn(*mut NSError)>, mpsc::Receiver<Option<String>>) {
    let (tx, rx) = mpsc::channel();
    let block = RcBlock::new(move |err: *mut NSError| {
        let _ = tx.send(unsafe { err.as_ref() }.map(|e| e.localizedDescription().to_string()));
    });
    (block, rx)
}

fn wait(rx: mpsc::Receiver<Option<String>>, what: &str) -> anyhow::Result<()> {
    match rx.recv_timeout(TIMEOUT) {
        Ok(None) => Ok(()),
        Ok(Some(e)) => bail!("{what}: {e}"),
        Err(_) => bail!("{what}: ScreenCaptureKit did not answer"),
    }
}

struct Capture {
    stream: Retained<SCStream>,
    _output: Retained<StreamOutput>,
    shared: Arc<Shared>,
    writer: PixelWriter,
    fps: u32,
    warnings: Vec<String>,
    fail_after: Option<u64>,
}

fn injected_failure() -> Option<u64> {
    std::env::var("GLINT_FAIL_SCREEN_AFTER").ok()?.parse().ok()
}

impl Capture {
    fn open(
        source: ScreenSource,
        [width, height]: [u32; 2],
        fps: u32,
        path: &Path,
        system: Option<PathBuf>,
        clock: &Arc<MediaClock>,
    ) -> anyhow::Result<Self> {
        let (width, height) = (width & !1, height & !1);
        if width < 2 || height < 2 {
            bail!("capture area is empty ({width}x{height})");
        }
        let content = shareable_content()?;
        let filter = match source {
            ScreenSource::Display { id, .. } => {
                let display = unsafe { content.displays() }
                    .into_iter()
                    .find(|d| unsafe { d.displayID() } == id)
                    .with_context(|| format!("display {id} is not shareable"))?;
                let own = std::process::id() as i32;
                let excluded: Vec<_> = if std::env::var_os("GLINT_CAPTURE_CHROME").is_some() {
                    Vec::new()
                } else {
                    unsafe { content.applications() }.into_iter().filter(|a| unsafe { a.processID() } == own).collect()
                };
                unsafe {
                    SCContentFilter::initWithDisplay_excludingApplications_exceptingWindows(
                        SCContentFilter::alloc(),
                        &display,
                        &NSArray::from_retained_slice(&excluded),
                        &NSArray::<SCWindow>::new(),
                    )
                }
            }
            ScreenSource::Window { id } => {
                let window = unsafe { content.windows() }
                    .into_iter()
                    .find(|w| unsafe { w.windowID() } == id)
                    .with_context(|| format!("window {id} is not shareable"))?;
                unsafe { SCContentFilter::initWithDesktopIndependentWindow(SCContentFilter::alloc(), &window) }
            }
        };

        let config = unsafe { SCStreamConfiguration::new() };
        unsafe {
            config.setWidth(width as usize);
            config.setHeight(height as usize);
            config.setShowsCursor(false);
            config.setPixelFormat(kCVPixelFormatType_32BGRA);
            config.setColorSpaceName(kCGColorSpaceSRGB);
            config.setMinimumFrameInterval(CMTime::new(1, fps as i32));
            config.setQueueDepth(QUEUE_DEPTH);
            if let ScreenSource::Display { crop: Some([x, y, w, h]), .. } = source {
                config.setSourceRect(CGRect::new(CGPoint::new(x, y), CGSize::new(w, h)));
            }
        }

        let mut warnings = Vec::new();
        let shared = Arc::new(Shared::default());
        if let Some(path) = system {
            match ClockedWav::create(&path, 2, clock.clone()) {
                Ok(wav) => {
                    unsafe {
                        config.setCapturesAudio(true);
                        config.setExcludesCurrentProcessAudio(true);
                        config.setSampleRate(AUDIO_RATE);
                        config.setChannelCount(2);
                    }
                    *shared.system.lock().unwrap() =
                        Some(SystemAudio { path, wav, converter: None, pcm: Vec::new(), out: Vec::new(), error: None });
                }
                Err(e) => warnings.push(format!("system audio not recorded: {e:#}")),
            }
        }

        let output = StreamOutput::new(shared.clone());
        let stream = unsafe {
            SCStream::initWithFilter_configuration_delegate(
                SCStream::alloc(),
                &filter,
                &config,
                Some(ProtocolObject::from_ref(&*output)),
            )
        };
        let as_output = ProtocolObject::from_ref(&*output);
        let screen_queue = DispatchQueue::new("glint.screen.frames", None);
        unsafe { stream.addStreamOutput_type_sampleHandlerQueue_error(as_output, SCStreamOutputType::Screen, Some(&screen_queue)) }
            .map_err(|e| anyhow!("add the screen output: {}", e.localizedDescription()))?;
        let has_audio = shared.system.lock().unwrap().is_some();
        if has_audio {
            let audio_queue = DispatchQueue::new("glint.screen.audio", None);
            if let Err(e) =
                unsafe { stream.addStreamOutput_type_sampleHandlerQueue_error(as_output, SCStreamOutputType::Audio, Some(&audio_queue)) }
            {
                warnings.push(format!("system audio not recorded: {}", e.localizedDescription()));
                discard(shared.system.lock().unwrap().take());
            }
        }

        let params = VideoParams { width, height, fps, bitrate: VideoParams::recording_bitrate(width, height, fps), codec: VideoCodec::H264 };
        let writer = PixelWriter::create(path, params)?;
        let (block, rx) = completion();
        unsafe { stream.startCaptureWithCompletionHandler(Some(&block)) };
        wait(rx, "start the screen capture")?;
        Ok(Self { stream, _output: output, shared, writer, fps, warnings, fail_after: injected_failure() })
    }

    fn wait_first_frame(&mut self, timeout: Duration) -> anyhow::Result<()> {
        let deadline = Instant::now() + timeout;
        while self.shared.latest.lock().unwrap().is_none() {
            if Instant::now() > deadline {
                bail!("no frame from ScreenCaptureKit within {timeout:?}");
            }
            if self.shared.closed.load(Ordering::Relaxed) {
                bail!("capture source closed before the first frame");
            }
            std::thread::sleep(Duration::from_millis(2));
        }
        Ok(())
    }

    /// Stops the stream. Its queues deliver nothing afterwards.
    fn close(&self) {
        let (block, rx) = completion();
        unsafe { self.stream.stopCaptureWithCompletionHandler(Some(&block)) };
        let _ = rx.recv_timeout(TIMEOUT);
    }

    fn frame_time(&self, index: u64) -> Duration {
        Duration::from_nanos(index * 1_000_000_000 / self.fps as u64)
    }

    fn catch_up(&mut self, clock: &MediaClock, next_index: &mut u64) -> anyhow::Result<()> {
        while self.frame_time(*next_index) <= clock.now() {
            if self.fail_after == Some(self.writer.frames()) {
                bail!("GLINT_FAIL_SCREEN_AFTER: injected failure after {} frames", self.writer.frames());
            }
            let Some(Frame(pixels)) = self.shared.latest.lock().unwrap().as_ref().map(|f| Frame(f.0.clone())) else { break };
            self.writer.append(&pixels, unsafe { CMTime::new(*next_index as i64, self.fps as i32) })?;
            *next_index += 1;
        }
        Ok(())
    }

    fn run(mut self, clock: &MediaClock, stop: &AtomicBool) -> ScreenEnd {
        let captured = self.capture(clock, stop);
        self.close();
        let frames = self.writer.frames();
        if !stop.load(Ordering::Relaxed) {
            clock.end_at(self.frame_time(frames));
        }
        let system = self.shared.system.lock().unwrap().take().map(SystemAudio::finish);
        let finished = self.writer.finish(unsafe { CMTime::new(frames as i64, self.fps as i32) });
        let screen = captured.and_then(|end| {
            let bytes = finished?;
            Ok(ScreenStats { frames, bytes, source_closed: end == CaptureEnd::SourceClosed })
        });
        ScreenEnd { screen, system }
    }

    fn capture(&mut self, clock: &MediaClock, stop: &AtomicBool) -> anyhow::Result<CaptureEnd> {
        let mut next_index = 0u64;
        while !stop.load(Ordering::Relaxed) {
            if self.shared.closed.load(Ordering::Relaxed) {
                return Ok(CaptureEnd::SourceClosed);
            }
            if clock.is_paused() {
                std::thread::sleep(Duration::from_millis(4));
                continue;
            }
            self.catch_up(clock, &mut next_index)?;
            let wait = self.frame_time(next_index).saturating_sub(clock.now());
            std::thread::sleep(wait.clamp(Duration::from_micros(500), self.frame_time(1)));
        }
        self.catch_up(clock, &mut next_index)?;
        Ok(CaptureEnd::Stopped)
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum CaptureEnd {
    Stopped,
    SourceClosed,
}

/// Drops a system audio track that will not record, with the empty `system.wav` it created.
fn discard(system: Option<SystemAudio>) {
    if let Some(s) = system {
        let path = s.path.clone();
        drop(s);
        let _ = std::fs::remove_file(path);
    }
}

