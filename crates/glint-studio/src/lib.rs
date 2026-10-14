//! Recording Studio: the project model, timeline math, auto zoom, motion and the frame
//! compositor. Decoding and encoding live in `glint-media`; this crate takes decoded frames.

pub mod compose;
pub mod motion;
pub mod overlays;
pub mod preset;
pub mod project;
pub mod timeline;
pub mod zoom;

pub use compose::{compose_frame, compose_frame_motion_blur, Assets, CursorImage, FrameInputs};
pub use overlays::CaptionWord;
pub use preset::{Preset, PresetStore};
pub use project::*;
pub use timeline::History;
pub use zoom::auto_zooms;
