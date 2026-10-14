//! Recording Studio: the project model, timeline math, auto zoom, motion, the frame
//! compositor, and the engines built on them: session loading, preview playback, timeline
//! thumbnails and export. Decoding and encoding primitives live in `glint-media`.

pub mod compose;
pub mod export;
mod feed;
pub mod mix;
pub mod motion;
pub mod overlays;
pub mod player;
pub mod preset;
pub mod project;
pub mod session;
pub mod thumbs;
pub mod timeline;
pub mod zoom;

pub use compose::{compose_frame, compose_frame_motion_blur, recycle_frame, Assets, CursorImage, FrameInputs};
pub use export::{bitrate, compress_video, deliverable, export, CompressOptions};
pub use mix::{AudioMixer, AudioSources};
pub use overlays::CaptionWord;
pub use player::Player;
pub use preset::{Preset, PresetStore};
pub use project::*;
pub use session::{caption_words, Session, Tracks};
pub use thumbs::timeline_thumbnails;
pub use timeline::History;
pub use zoom::auto_zooms;
