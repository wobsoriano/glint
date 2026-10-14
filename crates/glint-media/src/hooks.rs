//! The input track: low-level mouse and keyboard hooks on their own message-loop thread,
//! building an [`InputTimeline`] in screen-master pixel space on media time.

use crate::clock::MediaClock;
use crate::cursor::{self, CursorImage};
use crate::devices::PhysicalPixels;
use crate::input::{Click, CursorSprite, InputTimeline, KeyPress, MouseButton, PointerSample};
use crate::keys::{shortcut_label, Mods};
use crate::screen::{window_capture_rect, WindowBounds};
use anyhow::{bail, Context};
use std::cell::RefCell;
use std::collections::HashSet;
use std::path::{Path, PathBuf};
use std::sync::Arc;
use std::thread::JoinHandle;
use windows::Win32::Foundation::{LPARAM, LRESULT, WPARAM};
use windows::Win32::System::LibraryLoader::GetModuleHandleW;
use windows::Win32::System::Threading::GetCurrentThreadId;
use windows::Win32::UI::WindowsAndMessaging::*;

/// How screen coordinates map into the screen master's pixel space.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum PointerMap {
    /// Display and area sources: subtract a fixed virtual-desktop origin.
    Fixed { x: i32, y: i32 },
    /// Window sources: subtract the window's current capture rectangle.
    Window { hwnd: isize, bounds: WindowBounds },
}

impl PointerMap {
    fn origin(&self, last: &mut (i32, i32)) -> (i32, i32) {
        match *self {
            PointerMap::Fixed { x, y } => (x, y),
            PointerMap::Window { hwnd, bounds } => {
                if let Some(r) = window_capture_rect(hwnd, bounds) {
                    *last = (r.x, r.y);
                }
                *last
            }
        }
    }
}

/// The input track while recording. Stop it to get the timeline.
pub(crate) struct InputTrack {
    thread_id: u32,
    thread: Option<JoinHandle<anyhow::Result<InputTimeline>>>,
}

impl InputTrack {
    /// Installs the hooks. `scale` is the source display's DPI scale, stored on each sprite.
    pub fn start(map: PointerMap, size: [u32; 2], scale: f32, session_dir: &Path, clock: Arc<MediaClock>) -> anyhow::Result<Self> {
        let cursors_dir = session_dir.join("cursors");
        std::fs::create_dir_all(&cursors_dir).with_context(|| format!("create {}", cursors_dir.display()))?;
        let (tx, rx) = std::sync::mpsc::channel::<anyhow::Result<u32>>();
        let thread = std::thread::Builder::new().name("glint-input".into()).spawn(move || {
            let _physical = PhysicalPixels::enter();
            let state = HookState::new(map, size, scale, cursors_dir, clock);
            let hooks = unsafe {
                let module = GetModuleHandleW(None)?;
                let mouse = SetWindowsHookExW(WH_MOUSE_LL, Some(mouse_proc), Some(module.into()), 0).context("WH_MOUSE_LL");
                let keyboard = SetWindowsHookExW(WH_KEYBOARD_LL, Some(keyboard_proc), Some(module.into()), 0).context("WH_KEYBOARD_LL");
                match (mouse, keyboard) {
                    (Ok(m), Ok(k)) => (m, k),
                    (m, k) => {
                        let err = m.and(k).unwrap_err();
                        let _ = tx.send(Err(err));
                        return Ok(InputTimeline::default());
                    }
                }
            };
            STATE.with(|s| *s.borrow_mut() = Some(state));
            STATE.with(|s| s.borrow_mut().as_mut().unwrap().poll_cursor());
            let timer = unsafe { SetTimer(None, 0, CURSOR_POLL_MS, None) };
            let _ = tx.send(Ok(unsafe { GetCurrentThreadId() }));

            let mut msg = MSG::default();
            while unsafe { GetMessageW(&mut msg, None, 0, 0) }.as_bool() {
                if msg.message == WM_TIMER {
                    STATE.with(|s| s.borrow_mut().as_mut().unwrap().poll_cursor());
                }
                unsafe { DispatchMessageW(&msg) };
            }
            unsafe {
                let _ = KillTimer(None, timer);
                let _ = UnhookWindowsHookEx(hooks.0);
                let _ = UnhookWindowsHookEx(hooks.1);
            }
            let state = STATE.with(|s| s.borrow_mut().take()).unwrap();
            Ok(state.finish())
        })?;
        match rx.recv() {
            Ok(Ok(thread_id)) => Ok(Self { thread_id, thread: Some(thread) }),
            Ok(Err(e)) => {
                let _ = thread.join();
                Err(e)
            }
            Err(_) => {
                let _ = thread.join();
                bail!("input thread exited before installing hooks")
            }
        }
    }

    /// Removes the hooks and returns the timeline. Cursor PNGs are already on disk.
    pub fn stop(mut self) -> anyhow::Result<InputTimeline> {
        unsafe { PostThreadMessageW(self.thread_id, WM_QUIT, WPARAM(0), LPARAM(0))? };
        let thread = self.thread.take().unwrap();
        thread.join().map_err(|_| anyhow::anyhow!("input thread panicked"))?
    }
}

impl Drop for InputTrack {
    fn drop(&mut self) {
        if let Some(t) = self.thread.take() {
            unsafe { let _ = PostThreadMessageW(self.thread_id, WM_QUIT, WPARAM(0), LPARAM(0)); }
            let _ = t.join();
        }
    }
}

const CURSOR_POLL_MS: u32 = 32;
/// Minimum spacing between pointer samples, per the session contract.
const POINTER_INTERVAL_MS: u64 = 8;

thread_local! {
    static STATE: RefCell<Option<HookState>> = const { RefCell::new(None) };
}

struct HookState {
    clock: Arc<MediaClock>,
    map: PointerMap,
    last_origin: (i32, i32),
    scale: f32,
    cursors_dir: PathBuf,
    timeline: InputTimeline,
    /// `(content hash, sprite id)` of every sprite saved so far.
    known_cursors: Vec<(u64, u32)>,
    current_handle: isize,
    current_cursor: u32,
    last_pos: (f32, f32),
    last_pointer_ms: Option<u64>,
    /// Index into `timeline.clicks` of the press still waiting for its release, per button.
    pending: [Option<usize>; 3],
    mods: Mods,
    down: HashSet<u32>,
}

impl HookState {
    fn new(map: PointerMap, size: [u32; 2], scale: f32, cursors_dir: PathBuf, clock: Arc<MediaClock>) -> Self {
        Self {
            clock,
            map,
            last_origin: (0, 0),
            scale,
            cursors_dir,
            timeline: InputTimeline { version: 1, size, ..Default::default() },
            known_cursors: Vec::new(),
            current_handle: 0,
            current_cursor: 0,
            last_pos: (0.0, 0.0),
            last_pointer_ms: None,
            pending: [None; 3],
            mods: Mods::default(),
            down: HashSet::new(),
        }
    }

    fn now(&self) -> Option<u64> {
        (!self.clock.is_paused()).then(|| self.clock.now_ms())
    }

    fn map(&mut self, x: i32, y: i32) -> (f32, f32) {
        let (ox, oy) = self.map.origin(&mut self.last_origin);
        ((x - ox) as f32, (y - oy) as f32)
    }

    fn on_mouse(&mut self, msg: u32, x: i32, y: i32) {
        let Some(t) = self.now() else { return };
        let pos = self.map(x, y);
        self.last_pos = pos;
        match msg {
            WM_MOUSEMOVE => {
                if self.last_pointer_ms.is_some_and(|last| t < last + POINTER_INTERVAL_MS) {
                    return;
                }
                self.push_pointer(t, pos);
            }
            WM_LBUTTONDOWN | WM_RBUTTONDOWN | WM_MBUTTONDOWN => {
                let (button, slot) = button_of(msg);
                self.push_pointer(t, pos);
                self.timeline.clicks.push(Click { t, up: t, x: pos.0, y: pos.1, button });
                self.pending[slot] = Some(self.timeline.clicks.len() - 1);
            }
            WM_LBUTTONUP | WM_RBUTTONUP | WM_MBUTTONUP => {
                let (_, slot) = button_of(msg);
                if let Some(i) = self.pending[slot].take() {
                    self.timeline.clicks[i].up = t;
                }
            }
            _ => {}
        }
    }

    fn on_key(&mut self, msg: u32, vk: u32) {
        let down = matches!(msg, WM_KEYDOWN | WM_SYSKEYDOWN);
        if self.mods.apply(vk, down) {
            return;
        }
        if !down {
            self.down.remove(&vk);
            return;
        }
        if !self.down.insert(vk) {
            return;
        }
        let Some(t) = self.now() else { return };
        if let Some(label) = shortcut_label(vk, self.mods) {
            self.timeline.keys.push(KeyPress { t, label });
        }
    }

    fn push_pointer(&mut self, t: u64, pos: (f32, f32)) {
        self.timeline.pointer.push(PointerSample(t, pos.0, pos.1, self.current_cursor));
        self.last_pointer_ms = Some(t);
    }

    /// Notices cursor shape changes, saving each new shape once, and position changes the
    /// hooks did not see (`SetCursorPos` moves the cursor without input events).
    fn poll_cursor(&mut self) {
        let mut info = CURSORINFO { cbSize: std::mem::size_of::<CURSORINFO>() as u32, ..Default::default() };
        if unsafe { GetCursorInfo(&mut info) }.is_err() || info.flags.0 & CURSOR_SHOWING.0 == 0 {
            return;
        }
        let pos = self.map(info.ptScreenPos.x, info.ptScreenPos.y);
        if pos != self.last_pos {
            if let Some(t) = self.now() {
                self.last_pos = pos;
                self.push_pointer(t, pos);
            }
        }
        let handle = info.hCursor.0 as isize;
        if handle == self.current_handle {
            return;
        }
        let Some(image) = cursor::render(info.hCursor) else { return };
        self.current_handle = handle;
        let id = self.sprite_id(image);
        if id != self.current_cursor {
            self.current_cursor = id;
            if let Some(t) = self.now() {
                let pos = self.map(info.ptScreenPos.x, info.ptScreenPos.y);
                self.push_pointer(t, pos);
            }
        }
    }

    fn sprite_id(&mut self, image: CursorImage) -> u32 {
        let hash = image.hash();
        if let Some((_, id)) = self.known_cursors.iter().find(|(h, _)| *h == hash) {
            return *id;
        }
        let id = self.known_cursors.len() as u32;
        let file = format!("cursors/{id}.png");
        let path = self.cursors_dir.join(format!("{id}.png"));
        if image::save_buffer(&path, &image.rgba, image.width, image.height, image::ColorType::Rgba8).is_ok() {
            self.known_cursors.push((hash, id));
            self.timeline.cursors.push(CursorSprite { id, file, hotspot: image.hotspot, scale: self.scale });
        }
        id
    }

    fn finish(mut self) -> InputTimeline {
        let end = self.clock.now_ms();
        for i in self.pending.into_iter().flatten() {
            self.timeline.clicks[i].up = end;
        }
        self.timeline
    }
}

fn button_of(msg: u32) -> (MouseButton, usize) {
    match msg {
        WM_LBUTTONDOWN | WM_LBUTTONUP => (MouseButton::Left, 0),
        WM_RBUTTONDOWN | WM_RBUTTONUP => (MouseButton::Right, 1),
        _ => (MouseButton::Middle, 2),
    }
}

unsafe extern "system" fn mouse_proc(code: i32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if code == HC_ACTION as i32 {
        let data = unsafe { &*(lparam.0 as *const MSLLHOOKSTRUCT) };
        let (x, y) = (data.pt.x, data.pt.y);
        STATE.with(|s| {
            if let Some(state) = s.borrow_mut().as_mut() {
                state.on_mouse(wparam.0 as u32, x, y);
            }
        });
    }
    unsafe { CallNextHookEx(None, code, wparam, lparam) }
}

unsafe extern "system" fn keyboard_proc(code: i32, wparam: WPARAM, lparam: LPARAM) -> LRESULT {
    if code == HC_ACTION as i32 {
        let data = unsafe { &*(lparam.0 as *const KBDLLHOOKSTRUCT) };
        let vk = data.vkCode;
        STATE.with(|s| {
            if let Some(state) = s.borrow_mut().as_mut() {
                state.on_key(wparam.0 as u32, vk);
            }
        });
    }
    unsafe { CallNextHookEx(None, code, wparam, lparam) }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn state(map: PointerMap) -> HookState {
        let clock = Arc::new(MediaClock::paused());
        clock.resume();
        HookState::new(map, [1920, 1080], 1.5, std::env::temp_dir(), clock)
    }

    #[test]
    fn pointer_moves_are_mapped_and_rate_limited() {
        let mut s = state(PointerMap::Fixed { x: 100, y: 50 });
        s.on_mouse(WM_MOUSEMOVE, 110, 70);
        s.on_mouse(WM_MOUSEMOVE, 111, 71);
        assert_eq!(s.timeline.pointer.len(), 1, "a second move within 8 ms is dropped");
        let p = s.timeline.pointer[0];
        assert_eq!((p.pos(), p.cursor()), ((10.0, 20.0), 0));
    }

    #[test]
    fn clicks_pair_down_with_up_and_close_at_finish() {
        let mut s = state(PointerMap::Fixed { x: 0, y: 0 });
        s.on_mouse(WM_LBUTTONDOWN, 5, 6);
        s.on_mouse(WM_LBUTTONUP, 5, 6);
        s.on_mouse(WM_RBUTTONDOWN, 7, 8);
        let tl = s.finish();
        assert_eq!(tl.clicks.len(), 2);
        assert_eq!((tl.clicks[0].button, tl.clicks[0].x, tl.clicks[0].y), (MouseButton::Left, 5.0, 6.0));
        assert!(tl.clicks[0].up >= tl.clicks[0].t);
        assert_eq!(tl.clicks[1].button, MouseButton::Right);
        assert!(tl.clicks[1].up >= tl.clicks[1].t, "an unreleased button closes at the end");
    }

    #[test]
    fn keys_record_shortcuts_once_per_press() {
        use windows::Win32::UI::Input::KeyboardAndMouse::{VK_LCONTROL, VK_RETURN};
        let mut s = state(PointerMap::Fixed { x: 0, y: 0 });
        s.on_key(WM_KEYDOWN, VK_LCONTROL.0 as u32);
        s.on_key(WM_KEYDOWN, b'S' as u32);
        s.on_key(WM_KEYDOWN, b'S' as u32);
        s.on_key(WM_KEYUP, b'S' as u32);
        s.on_key(WM_KEYUP, VK_LCONTROL.0 as u32);
        s.on_key(WM_KEYDOWN, b'A' as u32);
        s.on_key(WM_KEYDOWN, VK_RETURN.0 as u32);
        let labels: Vec<&str> = s.timeline.keys.iter().map(|k| k.label.as_str()).collect();
        assert_eq!(labels, vec!["Ctrl+S", "Enter"]);
    }

    #[test]
    fn nothing_is_recorded_while_paused() {
        let mut s = state(PointerMap::Fixed { x: 0, y: 0 });
        s.clock.pause();
        s.on_mouse(WM_MOUSEMOVE, 1, 1);
        s.on_mouse(WM_LBUTTONDOWN, 1, 1);
        s.on_key(WM_KEYDOWN, windows::Win32::UI::Input::KeyboardAndMouse::VK_RETURN.0 as u32);
        assert_eq!(s.timeline, InputTimeline { version: 1, size: [1920, 1080], ..Default::default() });
    }
}
