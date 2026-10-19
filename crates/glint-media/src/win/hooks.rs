use crate::clock::MediaClock;
use super::cursor;
use crate::input_recorder::InputRecorder;
use super::devices::PhysicalPixels;
use crate::input::{InputTimeline, MouseButton};
use super::keys::{shortcut_label, Mods};
use super::screen::{window_capture_rect, WindowBounds};
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

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum PointerMap {
    Fixed { x: i32, y: i32 },
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

pub(crate) struct InputTrack {
    thread_id: u32,
    thread: Option<JoinHandle<anyhow::Result<InputTimeline>>>,
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
        let (tx, rx) = std::sync::mpsc::channel::<anyhow::Result<u32>>();
        let thread = std::thread::Builder::new().name("glint-input".into()).spawn(move || {
            let _physical = PhysicalPixels::enter();
            let mut state = HookState::new(map, size, scale, cursors_dir, clock);
            state.rec = state.rec.ignoring(ignored_keys);
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

    pub fn stop(mut self) -> anyhow::Result<InputTimeline> {
        unsafe { PostThreadMessageW(self.thread_id, WM_QUIT, WPARAM(0), LPARAM(0))? };
        let thread = self.thread.take().unwrap();
        thread.join().map_err(|_| anyhow::anyhow!("input thread panicked"))?
    }
}

impl Drop for InputTrack {
    fn drop(&mut self) {
        if let Some(t) = self.thread.take() {
            let _ = unsafe { PostThreadMessageW(self.thread_id, WM_QUIT, WPARAM(0), LPARAM(0)) };
            let _ = t.join();
        }
    }
}

const CURSOR_POLL_MS: u32 = 32;

thread_local! {
    static STATE: RefCell<Option<HookState>> = const { RefCell::new(None) };
}

/// Turns hook messages into recorder calls: maps screen positions to the capture, tracks
/// modifiers for shortcut labels, and renders new cursor shapes.
struct HookState {
    rec: InputRecorder,
    map: PointerMap,
    last_origin: (i32, i32),
    current_handle: isize,
    mods: Mods,
    down: HashSet<u32>,
}

impl HookState {
    fn new(map: PointerMap, size: [u32; 2], scale: f32, cursors_dir: PathBuf, clock: Arc<MediaClock>) -> Self {
        Self {
            rec: InputRecorder::new(size, scale, cursors_dir, clock),
            map,
            last_origin: (0, 0),
            current_handle: 0,
            mods: Mods::default(),
            down: HashSet::new(),
        }
    }

    fn map(&mut self, x: i32, y: i32) -> (f32, f32) {
        let (ox, oy) = self.map.origin(&mut self.last_origin);
        ((x - ox) as f32, (y - oy) as f32)
    }

    fn on_mouse(&mut self, msg: u32, x: i32, y: i32) {
        if self.rec.clock.is_paused() {
            return;
        }
        let pos = self.map(x, y);
        match msg {
            WM_MOUSEMOVE => self.rec.moved(pos),
            WM_LBUTTONDOWN | WM_RBUTTONDOWN | WM_MBUTTONDOWN => self.rec.pressed(button_of(msg), pos),
            WM_LBUTTONUP | WM_RBUTTONUP | WM_MBUTTONUP => self.rec.released(button_of(msg), pos),
            _ => {}
        }
    }

    fn on_key(&mut self, msg: u32, vk: u32, scan: u32) {
        let down = matches!(msg, WM_KEYDOWN | WM_SYSKEYDOWN);
        if self.mods.apply(vk, scan, down) {
            return;
        }
        if !down {
            self.down.remove(&vk);
            return;
        }
        if !self.down.insert(vk) {
            return;
        }
        if let Some(label) = shortcut_label(vk, self.mods) {
            self.rec.key(label);
        }
    }

    /// Notices cursor shape changes and position changes the hooks did not see
    /// (`SetCursorPos` moves the cursor without input events).
    fn poll_cursor(&mut self) {
        let mut info = CURSORINFO { cbSize: std::mem::size_of::<CURSORINFO>() as u32, ..Default::default() };
        if unsafe { GetCursorInfo(&mut info) }.is_err() || info.flags.0 & CURSOR_SHOWING.0 == 0 {
            return;
        }
        let pos = self.map(info.ptScreenPos.x, info.ptScreenPos.y);
        self.rec.polled(pos);
        let handle = info.hCursor.0 as isize;
        if handle == self.current_handle {
            return;
        }
        let Some(image) = cursor::render(info.hCursor) else { return };
        self.current_handle = handle;
        self.rec.cursor_changed(image, pos);
    }

    fn finish(self) -> InputTimeline {
        self.rec.finish()
    }
}

fn button_of(msg: u32) -> MouseButton {
    match msg {
        WM_LBUTTONDOWN | WM_LBUTTONUP => MouseButton::Left,
        WM_RBUTTONDOWN | WM_RBUTTONUP => MouseButton::Right,
        _ => MouseButton::Middle,
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
        let (vk, scan) = (data.vkCode, data.scanCode);
        STATE.with(|s| {
            if let Some(state) = s.borrow_mut().as_mut() {
                state.on_key(wparam.0 as u32, vk, scan);
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
        assert_eq!(s.rec.timeline.pointer.len(), 1, "a second move within 8 ms is dropped");
        let p = s.rec.timeline.pointer[0];
        assert_eq!((p.pos(), p.cursor()), ((10.0, 20.0), 0));
    }

    const SC_Q: u32 = 0x10;
    const SC_LCONTROL: u32 = 0x1D;
    const SC_RETURN: u32 = 0x1C;
    const SC_ALT: u32 = 0x38;
    const SC_ALTGR_LCONTROL: u32 = 0x21D;

    fn labels(s: &HookState) -> Vec<&str> {
        s.rec.timeline.keys.iter().map(|k| k.label.as_str()).collect()
    }

    fn altgr_chord(s: &mut HookState, vk: u32, scan: u32) {
        use windows::Win32::UI::Input::KeyboardAndMouse::{VK_LCONTROL, VK_RMENU};
        s.on_key(WM_SYSKEYDOWN, VK_LCONTROL.0 as u32, SC_ALTGR_LCONTROL);
        s.on_key(WM_SYSKEYDOWN, VK_RMENU.0 as u32, SC_ALT);
        s.on_key(WM_SYSKEYDOWN, vk, scan);
        s.on_key(WM_SYSKEYUP, vk, scan);
        s.on_key(WM_KEYUP, VK_LCONTROL.0 as u32, SC_ALTGR_LCONTROL);
        s.on_key(WM_KEYUP, VK_RMENU.0 as u32, SC_ALT);
    }

    #[test]
    fn keys_record_shortcuts_once_per_press() {
        use windows::Win32::UI::Input::KeyboardAndMouse::{VK_LCONTROL, VK_RETURN};
        let mut s = state(PointerMap::Fixed { x: 0, y: 0 });
        s.on_key(WM_KEYDOWN, VK_LCONTROL.0 as u32, SC_LCONTROL);
        s.on_key(WM_KEYDOWN, b'S' as u32, 0x1F);
        s.on_key(WM_KEYDOWN, b'S' as u32, 0x1F);
        s.on_key(WM_KEYUP, b'S' as u32, 0x1F);
        s.on_key(WM_KEYUP, VK_LCONTROL.0 as u32, SC_LCONTROL);
        s.on_key(WM_KEYDOWN, b'A' as u32, 0x1E);
        s.on_key(WM_KEYDOWN, VK_RETURN.0 as u32, SC_RETURN);
        assert_eq!(labels(&s), vec!["Ctrl+S", "Enter"]);
    }

    #[test]
    fn altgr_typing_is_not_a_shortcut() {
        use windows::Win32::UI::Input::KeyboardAndMouse::VK_RETURN;
        let mut s = state(PointerMap::Fixed { x: 0, y: 0 });
        altgr_chord(&mut s, b'Q' as u32, SC_Q);
        assert_eq!(labels(&s), Vec::<&str>::new(), "AltGr+Q types @ on a German layout");
        altgr_chord(&mut s, VK_RETURN.0 as u32, SC_RETURN);
        assert_eq!(labels(&s), vec!["Enter"], "AltGr+Enter is still the Enter key");
        s.on_key(WM_KEYDOWN, b'Q' as u32, SC_Q);
        assert_eq!(labels(&s), vec!["Enter"], "AltGr is fully released afterwards");
    }

    #[test]
    fn left_ctrl_left_alt_is_still_a_shortcut() {
        use windows::Win32::UI::Input::KeyboardAndMouse::{VK_LCONTROL, VK_LMENU};
        let mut s = state(PointerMap::Fixed { x: 0, y: 0 });
        s.on_key(WM_KEYDOWN, VK_LCONTROL.0 as u32, SC_LCONTROL);
        s.on_key(WM_SYSKEYDOWN, VK_LMENU.0 as u32, SC_ALT);
        s.on_key(WM_SYSKEYDOWN, b'Q' as u32, SC_Q);
        s.on_key(WM_SYSKEYUP, b'Q' as u32, SC_Q);
        s.on_key(WM_KEYUP, VK_LMENU.0 as u32, SC_ALT);
        s.on_key(WM_KEYUP, VK_LCONTROL.0 as u32, SC_LCONTROL);
        assert_eq!(labels(&s), vec!["Ctrl+Alt+Q"]);
    }

    #[test]
    fn nothing_is_recorded_while_paused() {
        let mut s = state(PointerMap::Fixed { x: 0, y: 0 });
        s.rec.clock.pause();
        s.on_mouse(WM_MOUSEMOVE, 1, 1);
        s.on_mouse(WM_LBUTTONDOWN, 1, 1);
        s.on_key(WM_KEYDOWN, windows::Win32::UI::Input::KeyboardAndMouse::VK_RETURN.0 as u32, SC_RETURN);
        assert_eq!(s.rec.timeline, InputTimeline { version: 1, size: [1920, 1080], ..Default::default() });
    }
}
