//! Windows backend: Windows.Graphics.Capture, Media Foundation, WASAPI and low-level hooks.

pub(crate) mod audio;
pub(crate) mod audio_out;
pub(crate) mod camera;
mod cursor;
mod d3d;
pub(crate) mod devices;
pub(crate) mod encoder;
pub(crate) mod frames;
mod hooks;
mod keys;
mod mf;
pub(crate) mod reader;
pub(crate) mod recorder;
mod screen;
pub(crate) mod writer;
