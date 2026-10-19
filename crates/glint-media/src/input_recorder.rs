//! Builds the input timeline from pointer, button, key and cursor events. Platform backends
//! feed it positions already mapped to screen-master pixels.

use crate::clock::MediaClock;
use crate::input::{Click, CursorSprite, InputTimeline, KeyPress, MouseButton, PointerSample};
use std::path::PathBuf;
use std::sync::Arc;

const POINTER_INTERVAL_MS: u64 = 8;

/// A cursor shape with its hotspot, in straight RGBA.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct CursorImage {
    pub width: u32,
    pub height: u32,
    pub hotspot: [u32; 2],
    pub rgba: Vec<u8>,
}

impl CursorImage {
    pub fn hash(&self) -> u64 {
        let mut h: u64 = 0xcbf29ce484222325;
        for b in &self.rgba {
            h ^= *b as u64;
            h = h.wrapping_mul(0x100000001b3);
        }
        h ^ ((self.width as u64) << 48 | (self.height as u64) << 32 | (self.hotspot[0] as u64) << 16 | self.hotspot[1] as u64)
    }
}

pub(crate) struct InputRecorder {
    pub clock: Arc<MediaClock>,
    scale: f32,
    cursors_dir: PathBuf,
    pub timeline: InputTimeline,
    known_cursors: Vec<(u64, u32)>,
    current_cursor: u32,
    last_pos: (f32, f32),
    last_pointer_ms: Option<u64>,
    pending: [Option<usize>; 3],
    ignored_keys: Vec<String>,
}

impl InputRecorder {
    /// `scale` is the display scale the cursor sprites are drawn at; sprites go to
    /// `cursors_dir` as `<id>.png`.
    pub fn new(size: [u32; 2], scale: f32, cursors_dir: PathBuf, clock: Arc<MediaClock>) -> Self {
        Self {
            clock,
            scale,
            cursors_dir,
            timeline: InputTimeline { version: 1, size, ..Default::default() },
            known_cursors: Vec::new(),
            current_cursor: 0,
            last_pos: (0.0, 0.0),
            last_pointer_ms: None,
            pending: [None; 3],
            ignored_keys: Vec::new(),
        }
    }

    /// Leaves these shortcut labels out of the timeline, such as the app's own hotkeys.
    pub fn ignoring(mut self, labels: Vec<String>) -> Self {
        self.ignored_keys = labels;
        self
    }

    /// Media time, or `None` while paused, when nothing is recorded.
    fn now(&self) -> Option<u64> {
        (!self.clock.is_paused()).then(|| self.clock.now_ms())
    }

    /// A pointer move from an input event. Moves closer than 8 ms apart are dropped.
    pub fn moved(&mut self, pos: (f32, f32)) {
        let Some(t) = self.now() else { return };
        self.last_pos = pos;
        if self.last_pointer_ms.is_some_and(|last| t < last + POINTER_INTERVAL_MS) {
            return;
        }
        self.push_pointer(t, pos);
    }

    pub fn pressed(&mut self, button: MouseButton, pos: (f32, f32)) {
        let Some(t) = self.now() else { return };
        self.last_pos = pos;
        self.push_pointer(t, pos);
        self.timeline.clicks.push(Click { t, up: t, x: pos.0, y: pos.1, button });
        self.pending[slot(button)] = Some(self.timeline.clicks.len() - 1);
    }

    pub fn released(&mut self, button: MouseButton, pos: (f32, f32)) {
        let Some(t) = self.now() else { return };
        self.last_pos = pos;
        if let Some(i) = self.pending[slot(button)].take() {
            self.timeline.clicks[i].up = t;
        }
    }

    pub fn key(&mut self, label: String) {
        if self.ignored_keys.contains(&label) {
            return;
        }
        if let Some(t) = self.now() {
            self.timeline.keys.push(KeyPress { t, label });
        }
    }

    /// The pointer position from a poll, which catches moves no input event reported (a
    /// program warping the cursor).
    pub fn polled(&mut self, pos: (f32, f32)) {
        if pos == self.last_pos {
            return;
        }
        if let Some(t) = self.now() {
            self.last_pos = pos;
            self.push_pointer(t, pos);
        }
    }

    /// The cursor took a new shape. Each distinct shape is saved once.
    pub fn cursor_changed(&mut self, image: CursorImage, pos: (f32, f32)) {
        let id = self.sprite_id(image);
        if id != self.current_cursor {
            self.current_cursor = id;
            if let Some(t) = self.now() {
                self.push_pointer(t, pos);
            }
        }
    }

    fn push_pointer(&mut self, t: u64, pos: (f32, f32)) {
        self.timeline.pointer.push(PointerSample(t, pos.0, pos.1, self.current_cursor));
        self.last_pointer_ms = Some(t);
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

    /// Closes clicks whose button is still down at the end.
    pub fn finish(mut self) -> InputTimeline {
        let end = self.clock.now_ms();
        for i in self.pending.into_iter().flatten() {
            self.timeline.clicks[i].up = end;
        }
        self.timeline
    }
}

fn slot(button: MouseButton) -> usize {
    match button {
        MouseButton::Left => 0,
        MouseButton::Right => 1,
        MouseButton::Middle => 2,
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn recorder() -> InputRecorder {
        let clock = Arc::new(MediaClock::paused());
        clock.resume();
        InputRecorder::new([1920, 1080], 1.5, std::env::temp_dir(), clock)
    }

    #[test]
    fn pointer_moves_are_rate_limited() {
        let mut r = recorder();
        r.moved((10.0, 20.0));
        r.moved((11.0, 21.0));
        assert_eq!(r.timeline.pointer.len(), 1, "a second move within 8 ms is dropped");
        let p = r.timeline.pointer[0];
        assert_eq!((p.pos(), p.cursor()), ((10.0, 20.0), 0));
    }

    #[test]
    fn clicks_pair_down_with_up_and_close_at_finish() {
        let mut r = recorder();
        r.pressed(MouseButton::Left, (5.0, 6.0));
        r.released(MouseButton::Left, (5.0, 6.0));
        r.pressed(MouseButton::Right, (7.0, 8.0));
        let tl = r.finish();
        assert_eq!(tl.clicks.len(), 2);
        assert_eq!((tl.clicks[0].button, tl.clicks[0].x, tl.clicks[0].y), (MouseButton::Left, 5.0, 6.0));
        assert!(tl.clicks[0].up >= tl.clicks[0].t);
        assert_eq!(tl.clicks[1].button, MouseButton::Right);
        assert!(tl.clicks[1].up >= tl.clicks[1].t, "an unreleased button closes at the end");
    }

    #[test]
    fn a_polled_position_is_recorded_only_when_it_moved() {
        let mut r = recorder();
        r.moved((1.0, 1.0));
        r.polled((1.0, 1.0));
        r.polled((40.0, 2.0));
        assert_eq!(r.timeline.pointer.iter().map(|p| p.pos()).collect::<Vec<_>>(), vec![(1.0, 1.0), (40.0, 2.0)]);
    }

    #[test]
    fn each_cursor_shape_is_saved_once() {
        let dir = std::env::temp_dir().join("glint-input-recorder-cursors");
        std::fs::create_dir_all(&dir).unwrap();
        let clock = Arc::new(MediaClock::paused());
        clock.resume();
        let mut r = InputRecorder::new([100, 100], 2.0, dir, clock);
        let arrow = CursorImage { width: 2, height: 2, hotspot: [0, 0], rgba: vec![255; 16] };
        let beam = CursorImage { width: 2, height: 2, hotspot: [1, 1], rgba: vec![0; 16] };
        r.cursor_changed(arrow.clone(), (0.0, 0.0));
        r.cursor_changed(beam, (0.0, 0.0));
        r.cursor_changed(arrow, (0.0, 0.0));
        let ids: Vec<u32> = r.timeline.cursors.iter().map(|c| c.id).collect();
        assert_eq!(ids, vec![0, 1]);
        assert_eq!(r.timeline.cursors[1].scale, 2.0);
        assert_eq!(r.timeline.pointer.last().unwrap().cursor(), 0, "back on the first shape");
    }

    #[test]
    fn ignored_shortcuts_are_left_out() {
        let mut r = recorder().ignoring(vec!["Ctrl+Shift+7".into()]);
        r.key("Ctrl+Shift+7".into());
        r.key("Ctrl+S".into());
        assert_eq!(r.timeline.keys.iter().map(|k| k.label.as_str()).collect::<Vec<_>>(), vec!["Ctrl+S"]);
    }

    #[test]
    fn nothing_is_recorded_while_paused() {
        let mut r = recorder();
        r.clock.pause();
        r.moved((1.0, 1.0));
        r.pressed(MouseButton::Left, (1.0, 1.0));
        r.key("Enter".into());
        assert_eq!(r.timeline, InputTimeline { version: 1, size: [1920, 1080], ..Default::default() });
    }
}
