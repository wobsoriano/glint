//! Records pointer, clicks, shortcuts and cursor shapes into `input.json` through a
//! listen-only event tap. The tap needs the Input Monitoring permission; without it the track
//! fails to start and the recording continues without input.

use super::keys::{shortcut_label, typed_char, Mods};
use super::space;
use crate::clock::MediaClock;
use crate::devices::DisplayInfo;
use crate::input::{InputTimeline, MouseButton};
use crate::input_recorder::{CursorImage, InputRecorder};
use anyhow::{bail, Context};
use dispatch2::DispatchQueue;
use objc2_app_kit::NSCursor;
use objc2_core_foundation::{
    kCFRunLoopCommonModes, CFAbsoluteTimeGetCurrent, CFMachPort, CFRetained, CFRunLoop, CFRunLoopTimer, CFRunLoopTimerContext,
};
use objc2_core_graphics::{
    CGEvent, CGEventField, CGEventTapLocation, CGEventTapOptions, CGEventTapPlacement, CGEventTapProxy, CGEventType,
};
use std::cell::RefCell;
use std::ffi::c_void;
use std::path::Path;
use std::ptr::NonNull;
use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::Arc;
use std::thread::JoinHandle;

const CURSOR_POLL_SECS: f64 = 0.032;

/// Where pointer coordinates are measured from.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum PointerMap {
    /// A fixed origin in Glint pixels (a display or an area).
    Fixed { x: i32, y: i32 },
    /// The top-left corner of window `id`, which may move while recording.
    Window { id: u32 },
}

struct RunLoop(CFRetained<CFRunLoop>);
unsafe impl Send for RunLoop {}

pub(crate) struct InputTrack {
    run_loop: RunLoop,
    thread: Option<JoinHandle<InputTimeline>>,
}

impl InputTrack {
    pub fn start(
        map: PointerMap,
        size: [u32; 2],
        scale: f32,
        session_dir: &Path,
        clock: Arc<MediaClock>,
        ignored_keys: Vec<String>,
    ) -> anyhow::Result<Self> {
        let cursors_dir = session_dir.join("cursors");
        std::fs::create_dir_all(&cursors_dir).with_context(|| format!("create {}", cursors_dir.display()))?;
        let displays = crate::devices::displays()?;
        let (tx, rx) = mpsc::channel::<anyhow::Result<RunLoop>>();
        let thread = std::thread::Builder::new().name("glint-input".into()).spawn(move || {
            let (cursor_tx, cursor_rx) = mpsc::channel();
            let state = RefCell::new(TapState {
                rec: InputRecorder::new(size, scale, cursors_dir, clock).ignoring(ignored_keys),
                map,
                last_origin: (0, 0),
                displays,
                cursor_tx,
                cursor_rx,
                cursor_pending: false,
                tap: None,
            });
            let info = &state as *const RefCell<TapState> as *mut c_void;
            let mask = [
                CGEventType::MouseMoved,
                CGEventType::LeftMouseDragged,
                CGEventType::RightMouseDragged,
                CGEventType::OtherMouseDragged,
                CGEventType::LeftMouseDown,
                CGEventType::LeftMouseUp,
                CGEventType::RightMouseDown,
                CGEventType::RightMouseUp,
                CGEventType::OtherMouseDown,
                CGEventType::OtherMouseUp,
                CGEventType::KeyDown,
            ]
            .iter()
            .fold(0u64, |m, t| m | 1 << t.0);
            let tap = unsafe {
                CGEvent::tap_create(
                    CGEventTapLocation::SessionEventTap,
                    CGEventTapPlacement::HeadInsertEventTap,
                    CGEventTapOptions::ListenOnly,
                    mask,
                    Some(on_event),
                    info,
                )
            };
            let Some(tap) = tap else {
                let _ = tx.send(Err(anyhow::anyhow!("Glint needs the Input Monitoring permission to record clicks and keys")));
                return InputTimeline::default();
            };
            let (Some(run_loop), Some(source)) = (CFRunLoop::current(), CFMachPort::new_run_loop_source(None, Some(&tap), 0)) else {
                let _ = tx.send(Err(anyhow::anyhow!("could not attach the event tap")));
                return InputTimeline::default();
            };
            let mut context = CFRunLoopTimerContext { version: 0, info, retain: None, release: None, copyDescription: None };
            let timer = unsafe {
                CFRunLoopTimer::new(None, CFAbsoluteTimeGetCurrent() + CURSOR_POLL_SECS, CURSOR_POLL_SECS, 0, 0, Some(on_timer), &mut context)
            };
            let modes = unsafe { kCFRunLoopCommonModes };
            run_loop.add_source(Some(&source), modes);
            run_loop.add_timer(timer.as_deref(), modes);
            CGEvent::tap_enable(&tap, true);
            state.borrow_mut().tap = Some(tap.clone());
            state.borrow_mut().poll();
            let _ = tx.send(Ok(RunLoop(run_loop.clone())));
            CFRunLoop::run();
            CGEvent::tap_enable(&tap, false);
            if let Some(timer) = &timer {
                timer.invalidate();
            }
            state.into_inner().rec.finish()
        })?;
        match rx.recv() {
            Ok(Ok(run_loop)) => Ok(Self { run_loop, thread: Some(thread) }),
            Ok(Err(e)) => {
                let _ = thread.join();
                Err(e)
            }
            Err(_) => {
                let _ = thread.join();
                bail!("input thread exited before installing the event tap")
            }
        }
    }

    pub fn stop(mut self) -> anyhow::Result<InputTimeline> {
        self.run_loop.0.stop();
        let thread = self.thread.take().unwrap();
        thread.join().map_err(|_| anyhow::anyhow!("input thread panicked"))
    }
}

impl Drop for InputTrack {
    fn drop(&mut self) {
        if let Some(t) = self.thread.take() {
            self.run_loop.0.stop();
            let _ = t.join();
        }
    }
}

struct TapState {
    rec: InputRecorder,
    map: PointerMap,
    last_origin: (i32, i32),
    displays: Vec<DisplayInfo>,
    /// Cursor shapes read on the main thread, which AppKit requires.
    cursor_tx: Sender<CursorImage>,
    cursor_rx: Receiver<CursorImage>,
    cursor_pending: bool,
    /// The system disables a tap that runs slow; it is re-enabled from here.
    tap: Option<CFRetained<CFMachPort>>,
}

impl TapState {
    /// Maps a global point to screen-master pixels.
    fn map(&mut self, x: f64, y: f64) -> (f32, f32) {
        let (px, py) = space::to_pixels(&self.displays, x, y);
        let (ox, oy) = match self.map {
            PointerMap::Fixed { x, y } => (x, y),
            PointerMap::Window { id } => {
                if let Some(b) = super::window::bounds(id) {
                    let r = space::rect_to_pixels(&self.displays, b);
                    self.last_origin = (r[0], r[1]);
                }
                self.last_origin
            }
        };
        ((px - ox) as f32, (py - oy) as f32)
    }

    fn on_event(&mut self, kind: CGEventType, event: &CGEvent) {
        if matches!(kind, CGEventType::TapDisabledByTimeout | CGEventType::TapDisabledByUserInput) {
            if let Some(tap) = &self.tap {
                CGEvent::tap_enable(tap, true);
            }
            return;
        }
        if self.rec.clock.is_paused() {
            return;
        }
        let p = CGEvent::location(Some(event));
        let button = |e: &CGEvent| match CGEvent::integer_value_field(Some(e), CGEventField::MouseEventButtonNumber) {
            1 => MouseButton::Right,
            _ => MouseButton::Middle,
        };
        match kind {
            CGEventType::MouseMoved | CGEventType::LeftMouseDragged | CGEventType::RightMouseDragged | CGEventType::OtherMouseDragged => {
                let pos = self.map(p.x, p.y);
                self.rec.moved(pos);
            }
            CGEventType::LeftMouseDown => {
                let pos = self.map(p.x, p.y);
                self.rec.pressed(MouseButton::Left, pos);
            }
            CGEventType::LeftMouseUp => {
                let pos = self.map(p.x, p.y);
                self.rec.released(MouseButton::Left, pos);
            }
            CGEventType::RightMouseDown | CGEventType::OtherMouseDown => {
                let pos = self.map(p.x, p.y);
                self.rec.pressed(button(event), pos);
            }
            CGEventType::RightMouseUp | CGEventType::OtherMouseUp => {
                let pos = self.map(p.x, p.y);
                self.rec.released(button(event), pos);
            }
            CGEventType::KeyDown => {
                if CGEvent::integer_value_field(Some(event), CGEventField::KeyboardEventAutorepeat) != 0 {
                    return;
                }
                let keycode = CGEvent::integer_value_field(Some(event), CGEventField::KeyboardEventKeycode) as u16;
                if let Some(label) = shortcut_label(keycode, Mods::from_flags(CGEvent::flags(Some(event))), typed_char(keycode)) {
                    self.rec.key(label);
                }
            }
            _ => {}
        }
    }

    /// Picks up the pointer position no event reported, the cursor shape the main thread read
    /// last, and asks for the next one.
    fn poll(&mut self) {
        let p = CGEvent::location(CGEvent::new(None).as_deref());
        let pos = self.map(p.x, p.y);
        self.rec.polled(pos);
        while let Ok(image) = self.cursor_rx.try_recv() {
            self.cursor_pending = false;
            self.rec.cursor_changed(image, pos);
        }
        if !self.cursor_pending {
            self.cursor_pending = true;
            let tx = self.cursor_tx.clone();
            DispatchQueue::main().exec_async(move || {
                if let Some(image) = current_cursor() {
                    let _ = tx.send(image);
                }
            });
        }
    }
}

/// The cursor the system shows now, at its backing resolution. Main thread only.
fn current_cursor() -> Option<CursorImage> {
    let cursor = NSCursor::currentSystemCursor()?;
    let image = cursor.image();
    let size = image.size();
    let cg = unsafe { image.CGImageForProposedRect_context_hints(std::ptr::null_mut(), None, None) }?;
    let rgba = super::sck::to_rgba(&cg).ok()?;
    let k = rgba.width() as f64 / size.width.max(1.0);
    let hot = cursor.hotSpot();
    Some(CursorImage {
        width: rgba.width(),
        height: rgba.height(),
        hotspot: [(hot.x * k).round() as u32, (hot.y * k).round() as u32],
        rgba: rgba.into_raw(),
    })
}

unsafe extern "C-unwind" fn on_event(_: CGEventTapProxy, kind: CGEventType, event: NonNull<CGEvent>, info: *mut c_void) -> *mut CGEvent {
    let state = unsafe { &*(info as *const RefCell<TapState>) };
    if let Ok(mut state) = state.try_borrow_mut() {
        state.on_event(kind, unsafe { event.as_ref() });
    }
    event.as_ptr()
}

unsafe extern "C-unwind" fn on_timer(_: *mut CFRunLoopTimer, info: *mut c_void) {
    let state = unsafe { &*(info as *const RefCell<TapState>) };
    if let Ok(mut state) = state.try_borrow_mut() {
        state.poll();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Move and click the mouse while it runs, for example with a CGEvent script. Cursor shapes
    /// come from the main queue, which a test binary never runs, so they are not checked here.
    #[test]
    #[ignore = "needs the Input Monitoring permission and real input"]
    fn records_pointer_and_clicks_while_running() {
        let dir = std::env::temp_dir().join("glint-input-track");
        std::fs::create_dir_all(&dir).unwrap();
        let clock = Arc::new(MediaClock::paused());
        clock.resume();
        let track = InputTrack::start(PointerMap::Fixed { x: 0, y: 0 }, [3024, 1964], 2.0, &dir, clock, Vec::new()).unwrap();
        std::thread::sleep(std::time::Duration::from_secs(3));
        let tl = track.stop().unwrap();
        eprintln!("pointer {} clicks {:?} keys {:?} cursors {:?}", tl.pointer.len(), tl.clicks, tl.keys, tl.cursors);
        assert!(tl.pointer.len() > 5, "pointer samples");
        assert!(!tl.clicks.is_empty(), "the click");
    }
}
