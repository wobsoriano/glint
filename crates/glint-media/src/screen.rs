use crate::clock::MediaClock;
use crate::d3d::D3dDevice;
use crate::devices::{PhysicalPixels, Rect};
use crate::encoder::{Mp4Sink, SampleAllocator, VideoCodec, VideoParams};
use anyhow::{bail, Context};
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread::JoinHandle;
use std::time::{Duration, Instant};
use windows::core::{IInspectable, Interface};
use windows::Foundation::TypedEventHandler;
use windows::Graphics::Capture::{Direct3D11CaptureFramePool, GraphicsCaptureItem, GraphicsCaptureSession};
use windows::Graphics::DirectX::DirectXPixelFormat;
use windows::Graphics::SizeInt32;
use windows::Win32::Foundation::{HWND, RECT};
use windows::Win32::Graphics::Direct3D11::{
    ID3D11Texture2D, D3D11_BIND_RENDER_TARGET, D3D11_BIND_SHADER_RESOURCE, D3D11_BOX, D3D11_CPU_ACCESS_FLAG, D3D11_USAGE_DEFAULT,
};
use windows::Win32::Graphics::Dwm::{DwmGetWindowAttribute, DWMWA_EXTENDED_FRAME_BOUNDS};
use windows::Win32::Graphics::Gdi::HMONITOR;
use windows::Win32::System::WinRT::Direct3D11::IDirect3DDxgiInterfaceAccess;
use windows::Win32::System::WinRT::Graphics::Capture::IGraphicsCaptureItemInterop;
use windows::Win32::UI::WindowsAndMessaging::GetWindowRect;

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum ScreenSource {
    Monitor { hmonitor: isize, crop: Option<Rect> },
    Window { hwnd: isize },
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum WindowBounds {
    Dwm,
    /// `GetWindowRect`: includes invisible resize borders.
    WindowRect,
}

pub(crate) fn window_capture_rect(hwnd: isize, bounds: WindowBounds) -> Option<Rect> {
    let _physical = PhysicalPixels::enter();
    let hwnd = HWND(hwnd as *mut _);
    let mut r = RECT::default();
    let ok = unsafe {
        match bounds {
            WindowBounds::Dwm => {
                DwmGetWindowAttribute(hwnd, DWMWA_EXTENDED_FRAME_BOUNDS, &mut r as *mut _ as *mut _, std::mem::size_of::<RECT>() as u32).is_ok()
            }
            WindowBounds::WindowRect => GetWindowRect(hwnd, &mut r).is_ok(),
        }
    };
    ok.then(|| Rect::from_win32(r))
}

fn media_100ns(clock: &MediaClock) -> i64 {
    clock.now().as_nanos() as i64 / 100
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct ScreenGeometry {
    pub width: u32,
    pub height: u32,
    pub window_bounds: Option<WindowBounds>,
}

#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct ScreenStats {
    pub frames: u64,
    pub bytes: u64,
    pub hardware: bool,
    pub source_closed: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
enum CaptureEnd {
    Stopped,
    SourceClosed,
}

pub(crate) struct ScreenTrack {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<anyhow::Result<ScreenStats>>>,
    pub path: PathBuf,
    pub warnings: Vec<String>,
}

impl ScreenTrack {
    pub fn start(source: ScreenSource, fps: u32, path: &Path, clock: Arc<MediaClock>) -> anyhow::Result<(Self, ScreenGeometry)> {
        let stop = Arc::new(AtomicBool::new(false));
        let (tx, rx) = std::sync::mpsc::channel::<anyhow::Result<(ScreenGeometry, Vec<String>)>>();
        let out = path.to_path_buf();
        let thread = {
            let stop = stop.clone();
            std::thread::Builder::new().name("glint-screen".into()).spawn(move || {
                let mut capture = match Capture::open(source, fps, &out) {
                    Ok(c) => c,
                    Err(e) => {
                        let _ = tx.send(Err(e));
                        return Ok(ScreenStats { frames: 0, bytes: 0, hardware: false, source_closed: false });
                    }
                };
                if let Err(e) = capture.wait_first_frame(Duration::from_secs(3)) {
                    let _ = tx.send(Err(e));
                    return Ok(ScreenStats { frames: 0, bytes: 0, hardware: false, source_closed: false });
                }
                let _ = tx.send(Ok((capture.geometry, std::mem::take(&mut capture.warnings))));
                capture.run(&clock, &stop)
            })?
        };
        match rx.recv() {
            Ok(Ok((geometry, warnings))) => Ok((Self { stop, thread: Some(thread), path: path.to_path_buf(), warnings }, geometry)),
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

    pub fn stop(mut self) -> anyhow::Result<ScreenStats> {
        self.stop.store(true, Ordering::Relaxed);
        let thread = self.thread.take().unwrap();
        thread.join().map_err(|_| anyhow::anyhow!("screen thread panicked"))?
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

struct Capture {
    d3d: D3dDevice,
    item: GraphicsCaptureItem,
    pool: Direct3D11CaptureFramePool,
    session: GraphicsCaptureSession,
    pool_size: SizeInt32,
    latest: ID3D11Texture2D,
    have_frame: bool,
    crop_origin: (u32, u32),
    sink: Mp4Sink,
    samples: SampleAllocator,
    geometry: ScreenGeometry,
    closed: Arc<AtomicBool>,
    warnings: Vec<String>,
    path: PathBuf,
    fail_after: Option<u64>,
}

fn injected_failure() -> Option<u64> {
    std::env::var("GLINT_FAIL_SCREEN_AFTER").ok()?.parse().ok()
}

impl Capture {
    fn open(source: ScreenSource, fps: u32, path: &Path) -> anyhow::Result<Self> {
        crate::mf::init()?;
        let d3d = D3dDevice::new()?;
        let interop = windows::core::factory::<GraphicsCaptureItem, IGraphicsCaptureItemInterop>().context("GraphicsCaptureItem factory")?;
        let item: GraphicsCaptureItem = unsafe {
            match source {
                ScreenSource::Monitor { hmonitor, .. } => interop.CreateForMonitor(HMONITOR(hmonitor as *mut _)).context("CreateForMonitor")?,
                ScreenSource::Window { hwnd } => interop.CreateForWindow(HWND(hwnd as *mut _)).context("CreateForWindow")?,
            }
        };
        let item_size = item.Size()?;
        let (item_w, item_h) = (item_size.Width.max(0) as u32, item_size.Height.max(0) as u32);

        let (crop_origin, width, height, window_bounds) = match source {
            ScreenSource::Monitor { crop: Some(r), .. } => {
                let x = r.x.clamp(0, item_w as i32) as u32;
                let y = r.y.clamp(0, item_h as i32) as u32;
                ((x, y), r.width.min(item_w - x), r.height.min(item_h - y), None)
            }
            ScreenSource::Monitor { crop: None, .. } => ((0, 0), item_w, item_h, None),
            ScreenSource::Window { hwnd } => {
                let matches = |b| window_capture_rect(hwnd, b).map(|r| (r.width, r.height)) == Some((item_w, item_h));
                let bounds = if matches(WindowBounds::Dwm) { WindowBounds::Dwm } else { WindowBounds::WindowRect };
                ((0, 0), item_w, item_h, Some(bounds))
            }
        };
        let (width, height) = (width & !1, height & !1);
        if width < 2 || height < 2 {
            bail!("capture area is empty ({width}x{height})");
        }

        let device = d3d.winrt_device()?;
        let pool = Direct3D11CaptureFramePool::CreateFreeThreaded(&device, DirectXPixelFormat::B8G8R8A8UIntNormalized, 2, item_size)
            .context("Direct3D11CaptureFramePool")?;
        let session = pool.CreateCaptureSession(&item).context("CreateCaptureSession")?;
        let mut warnings = Vec::new();
        if session.SetIsCursorCaptureEnabled(false).is_err() {
            warnings.push("could not disable cursor capture; the cursor is burned into screen.mp4".into());
        }
        if session.SetIsBorderRequired(false).is_err() {
            warnings.push("could not disable the capture border".into());
        }
        let closed = Arc::new(AtomicBool::new(false));
        {
            let closed = closed.clone();
            item.Closed(&TypedEventHandler::<GraphicsCaptureItem, IInspectable>::new(move |_, _| {
                closed.store(true, Ordering::Relaxed);
                Ok(())
            }))?;
        }

        let latest = d3d.texture(width, height, D3D11_USAGE_DEFAULT, D3D11_BIND_RENDER_TARGET | D3D11_BIND_SHADER_RESOURCE, D3D11_CPU_ACCESS_FLAG(0))?;
        let params = VideoParams { width, height, fps, bitrate: VideoParams::recording_bitrate(width, height, fps), codec: VideoCodec::H264 };
        let sink = Mp4Sink::create(path, params, None, Some(&d3d))?;
        let samples = SampleAllocator::new(&d3d, width, height, fps)?;
        session.StartCapture().context("StartCapture")?;
        Ok(Self {
            d3d,
            item,
            pool,
            session,
            pool_size: item_size,
            latest,
            have_frame: false,
            crop_origin,
            sink,
            samples,
            geometry: ScreenGeometry { width, height, window_bounds },
            closed,
            warnings,
            path: path.to_path_buf(),
            fail_after: injected_failure(),
        })
    }

    fn wait_first_frame(&mut self, timeout: Duration) -> anyhow::Result<()> {
        let deadline = Instant::now() + timeout;
        while !self.have_frame {
            if Instant::now() > deadline {
                bail!("no frame from Windows.Graphics.Capture within {timeout:?}");
            }
            if self.closed.load(Ordering::Relaxed) {
                bail!("capture source closed before the first frame");
            }
            self.drain()?;
            if !self.have_frame {
                std::thread::sleep(Duration::from_millis(2));
            }
        }
        Ok(())
    }

    fn drain(&mut self) -> anyhow::Result<()> {
        while let Ok(frame) = self.pool.TryGetNextFrame() {
            let content = frame.ContentSize()?;
            if content != self.pool_size && content.Width > 0 && content.Height > 0 {
                self.pool.Recreate(&self.d3d.winrt_device()?, DirectXPixelFormat::B8G8R8A8UIntNormalized, 2, content)?;
                self.pool_size = content;
                let g = self.geometry;
                self.latest = self.d3d.texture(g.width, g.height, D3D11_USAGE_DEFAULT, D3D11_BIND_RENDER_TARGET | D3D11_BIND_SHADER_RESOURCE, D3D11_CPU_ACCESS_FLAG(0))?;
                frame.Close()?;
                continue;
            }
            let access: IDirect3DDxgiInterfaceAccess = frame.Surface()?.cast()?;
            let texture: ID3D11Texture2D = unsafe { access.GetInterface()? };
            let (ox, oy) = self.crop_origin;
            let right = (ox + self.geometry.width).min(content.Width.max(0) as u32);
            let bottom = (oy + self.geometry.height).min(content.Height.max(0) as u32);
            if right > ox && bottom > oy {
                let src = D3D11_BOX { left: ox, top: oy, front: 0, right, bottom, back: 1 };
                unsafe { self.d3d.context.CopySubresourceRegion(&self.latest, 0, 0, 0, 0, &texture, 0, Some(&src)) };
                self.have_frame = true;
            }
            frame.Close()?;
        }
        Ok(())
    }

    fn catch_up(&mut self, clock: &MediaClock, frame_dur: i64, next_index: &mut i64) -> anyhow::Result<()> {
        let target_index = media_100ns(clock) / frame_dur;
        while *next_index <= target_index {
            let Some((sample, texture)) = self.samples.take()? else { break };
            unsafe { self.d3d.context.CopyResource(&texture, &self.latest) };
            if self.fail_after == Some(self.sink.frames()) {
                bail!("GLINT_FAIL_SCREEN_AFTER: injected failure after {} frames", self.sink.frames());
            }
            self.sink.write_sample(&sample, *next_index * frame_dur)?;
            *next_index += 1;
        }
        Ok(())
    }

    fn run(mut self, clock: &MediaClock, stop: &AtomicBool) -> anyhow::Result<ScreenStats> {
        let captured = self.capture(clock, stop);
        let _ = self.session.Close();
        let _ = self.pool.Close();
        drop(self.item);
        let frames = self.sink.frames();
        let hardware = self.sink.hardware();
        let finished = self.sink.finish();
        let source_closed = captured? == CaptureEnd::SourceClosed;
        finished?;
        let bytes = std::fs::metadata(&self.path)?.len();
        Ok(ScreenStats { frames, bytes, hardware, source_closed })
    }

    fn capture(&mut self, clock: &MediaClock, stop: &AtomicBool) -> anyhow::Result<CaptureEnd> {
        let frame_dur = self.sink.params().frame_duration();
        let mut next_index: i64 = 0;
        while !stop.load(Ordering::Relaxed) {
            if self.closed.load(Ordering::Relaxed) {
                return Ok(CaptureEnd::SourceClosed);
            }
            self.drain()?;
            if clock.is_paused() {
                std::thread::sleep(Duration::from_millis(4));
                continue;
            }
            self.catch_up(clock, frame_dur, &mut next_index)?;
            let next_due = next_index * frame_dur;
            let wait = (next_due - media_100ns(clock)).clamp(5_000, frame_dur);
            std::thread::sleep(Duration::from_nanos(wait as u64 * 100));
        }
        self.drain()?;
        self.catch_up(clock, frame_dur, &mut next_index)?;
        Ok(CaptureEnd::Stopped)
    }
}
