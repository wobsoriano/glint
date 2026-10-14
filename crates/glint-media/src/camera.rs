//! Camera capture through a Media Foundation source reader: an H.264 track while recording,
//! or RGBA frames to a callback for the pre-record preview.

use crate::clock::MediaClock;
use crate::devices::{camera_activate, DeviceId};
use crate::encoder::{Mp4Sink, VideoCodec, VideoParams};
use crate::mf::{self, pack2, unpack2};
use anyhow::{bail, Context};
use image::RgbaImage;
use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread::JoinHandle;
use windows::core::Interface;
use windows::Win32::Media::MediaFoundation::*;

/// Largest camera mode the engine picks; bigger native modes are skipped.
const MAX_PIXELS: u32 = 1920 * 1080;

/// Size and frame count of a finished camera track.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct CameraStats {
    pub frames: u64,
    pub bytes: u64,
    pub width: u32,
    pub height: u32,
}

/// `camera.mp4` being recorded on its own thread, frames stamped with media time.
pub(crate) struct CameraTrack {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<anyhow::Result<CameraStats>>>,
    pub path: PathBuf,
}

impl CameraTrack {
    pub fn start(id: &DeviceId, path: &Path, clock: Arc<MediaClock>) -> anyhow::Result<Self> {
        let stop = Arc::new(AtomicBool::new(false));
        let (thread, _) = spawn_reader(id.clone(), stop.clone(), {
            let path = path.to_path_buf();
            move |width, height, fps| {
                let params = VideoParams { width, height, fps, bitrate: VideoParams::recording_bitrate(width, height, fps), codec: VideoCodec::H264 };
                Mp4Sink::create(&path, params, None, None).map(|sink| FrameSink::Encode { sink, clock, path })
            }
        })?;
        Ok(Self { stop, thread: Some(thread), path: path.to_path_buf() })
    }

    pub fn stop(mut self) -> anyhow::Result<CameraStats> {
        self.stop.store(true, Ordering::Relaxed);
        let thread = self.thread.take().unwrap();
        thread.join().map_err(|_| anyhow::anyhow!("camera thread panicked"))?
    }
}

impl Drop for CameraTrack {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

/// Live RGBA frames from a camera, for the picker's preview tile. Dropping it stops the camera.
pub struct CameraPreview {
    stop: Arc<AtomicBool>,
    thread: Option<JoinHandle<anyhow::Result<CameraStats>>>,
    width: u32,
    height: u32,
}

impl CameraPreview {
    /// Opens the camera and calls `on_frame` from a background thread for every frame.
    pub fn start(id: &DeviceId, on_frame: impl FnMut(RgbaImage) + Send + 'static) -> anyhow::Result<Self> {
        let stop = Arc::new(AtomicBool::new(false));
        let (thread, (width, height)) = spawn_reader(id.clone(), stop.clone(), move |_, _, _| Ok(FrameSink::Preview(Box::new(on_frame))))?;
        Ok(Self { stop, thread: Some(thread), width, height })
    }

    /// Frame size the camera delivers.
    pub fn size(&self) -> (u32, u32) {
        (self.width, self.height)
    }
}

impl Drop for CameraPreview {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(t) = self.thread.take() {
            let _ = t.join();
        }
    }
}

enum FrameSink {
    Encode { sink: Mp4Sink, clock: Arc<MediaClock>, path: PathBuf },
    Preview(Box<dyn FnMut(RgbaImage) + Send>),
}

/// Opens the camera on a new thread and waits for the open to succeed or fail. `make_sink`
/// runs on that thread once the frame size and rate are known.
fn spawn_reader(
    id: DeviceId,
    stop: Arc<AtomicBool>,
    make_sink: impl FnOnce(u32, u32, u32) -> anyhow::Result<FrameSink> + Send + 'static,
) -> anyhow::Result<(JoinHandle<anyhow::Result<CameraStats>>, (u32, u32))> {
    let (tx, rx) = std::sync::mpsc::channel::<anyhow::Result<(u32, u32)>>();
    let thread = std::thread::Builder::new().name("glint-camera".into()).spawn(move || {
        let (reader, width, height, fps) = match open_reader(&id) {
            Ok(r) => r,
            Err(e) => {
                let _ = tx.send(Err(e));
                return Ok(CameraStats { frames: 0, bytes: 0, width: 0, height: 0 });
            }
        };
        let sink = match make_sink(width, height, fps) {
            Ok(s) => s,
            Err(e) => {
                let _ = tx.send(Err(e));
                return Ok(CameraStats { frames: 0, bytes: 0, width: 0, height: 0 });
            }
        };
        let _ = tx.send(Ok((width, height)));
        pump(reader, width, height, sink, &stop)
    })?;
    match rx.recv() {
        Ok(Ok(size)) => Ok((thread, size)),
        Ok(Err(e)) => {
            let _ = thread.join();
            Err(e)
        }
        Err(_) => {
            let _ = thread.join();
            bail!("camera thread exited before opening the device")
        }
    }
}

/// Picks the largest native mode up to 1080p at 24 fps or more, and asks for RGB32 output.
fn open_reader(id: &DeviceId) -> anyhow::Result<(IMFSourceReader, u32, u32, u32)> {
    mf::init()?;
    let activate = camera_activate(id)?;
    unsafe {
        let source: IMFMediaSource = activate.ActivateObject().with_context(|| format!("open camera {id}"))?;
        let mut attrs = None;
        MFCreateAttributes(&mut attrs, 1)?;
        let attrs = attrs.unwrap();
        attrs.SetUINT32(&MF_SOURCE_READER_ENABLE_ADVANCED_VIDEO_PROCESSING, 1)?;
        let reader = MFCreateSourceReaderFromMediaSource(&source, &attrs).context("MFCreateSourceReaderFromMediaSource")?;
        let stream = MF_SOURCE_READER_FIRST_VIDEO_STREAM.0 as u32;

        let mut best: Option<(u32, u32, u32, IMFMediaType)> = None;
        for i in 0.. {
            let Ok(t) = reader.GetNativeMediaType(stream, i) else { break };
            let (w, h) = unpack2(t.GetUINT64(&MF_MT_FRAME_SIZE).unwrap_or(0));
            let (n, d) = unpack2(t.GetUINT64(&MF_MT_FRAME_RATE).unwrap_or(pack2(30, 1)));
            let fps = n / d.max(1);
            if w * h > MAX_PIXELS || fps < 24 || w == 0 {
                continue;
            }
            if best.as_ref().is_none_or(|(bw, bh, bf, _)| (w * h, fps) > (bw * bh, *bf)) {
                best = Some((w, h, fps, t));
            }
        }
        let (width, height, fps, native) = best.context("camera offers no mode up to 1080p at 24 fps or more")?;
        reader.SetCurrentMediaType(stream, None, &native).context("select camera mode")?;
        let out = MFCreateMediaType()?;
        out.SetGUID(&MF_MT_MAJOR_TYPE, &MFMediaType_Video)?;
        out.SetGUID(&MF_MT_SUBTYPE, &MFVideoFormat_RGB32)?;
        out.SetUINT64(&MF_MT_FRAME_SIZE, pack2(width, height))?;
        reader.SetCurrentMediaType(stream, None, &out).context("camera RGB32 output")?;
        Ok((reader, width & !1, height & !1, fps))
    }
}

fn pump(reader: IMFSourceReader, width: u32, height: u32, mut sink: FrameSink, stop: &AtomicBool) -> anyhow::Result<CameraStats> {
    let stream = MF_SOURCE_READER_FIRST_VIDEO_STREAM.0 as u32;
    let mut frames = 0u64;
    let mut bgra = vec![0u8; width as usize * height as usize * 4];
    while !stop.load(Ordering::Relaxed) {
        let mut flags = 0u32;
        let mut sample = None;
        unsafe { reader.ReadSample(stream, 0, None, Some(&mut flags), None, Some(&mut sample)).context("camera ReadSample")? };
        if flags & MF_SOURCE_READERF_ENDOFSTREAM.0 as u32 != 0 {
            break;
        }
        let Some(sample) = sample else { continue };
        copy_rows(&sample, width, height, &mut bgra)?;
        match &mut sink {
            FrameSink::Encode { sink, clock, .. } => {
                if clock.is_paused() {
                    continue;
                }
                sink.write_bgra(&bgra, clock.now().as_nanos() as i64 / 100)?;
            }
            FrameSink::Preview(cb) => {
                let mut rgba = bgra.clone();
                for px in rgba.chunks_exact_mut(4) {
                    px.swap(0, 2);
                    px[3] = 255;
                }
                cb(RgbaImage::from_raw(width, height, rgba).context("camera frame size")?);
            }
        }
        frames += 1;
    }
    let bytes = match sink {
        FrameSink::Encode { sink, path, .. } => {
            sink.finish()?;
            std::fs::metadata(&path)?.len()
        }
        FrameSink::Preview(_) => 0,
    };
    Ok(CameraStats { frames, bytes, width, height })
}

/// Copies the sample's rows into a packed top-down BGRA buffer, whatever its stride.
fn copy_rows(sample: &IMFSample, width: u32, height: u32, out: &mut [u8]) -> anyhow::Result<()> {
    let row_bytes = width as usize * 4;
    unsafe {
        let buffer = sample.ConvertToContiguousBuffer()?;
        let mut scan0 = std::ptr::null_mut();
        let mut pitch = 0i32;
        let b2d = buffer.cast::<IMF2DBuffer>().ok();
        let base = match &b2d {
            Some(b) => {
                b.Lock2D(&mut scan0, &mut pitch)?;
                scan0
            }
            None => {
                buffer.Lock(&mut scan0, None, None)?;
                pitch = row_bytes as i32;
                scan0
            }
        };
        for y in 0..height as usize {
            let src = std::slice::from_raw_parts(base.offset(y as isize * pitch as isize), row_bytes);
            out[y * row_bytes..(y + 1) * row_bytes].copy_from_slice(src);
        }
        match &b2d {
            Some(b) => b.Unlock2D()?,
            None => buffer.Unlock()?,
        }
    }
    Ok(())
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
        let err = CameraTrack::start(&DeviceId("\\\\?\\no-such-camera".into()), &path, clock).err().expect("must fail");
        assert!(err.to_string().contains("no camera with id"), "{err:#}");
        assert!(!path.exists(), "no camera.mp4 should be created for a missing camera");
    }
}
