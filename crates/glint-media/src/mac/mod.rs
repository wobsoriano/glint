//! macOS backend: ScreenCaptureKit for screens and system audio, AVFoundation for MP4, the
//! microphone and the camera, CoreAudio for playback, and an event tap for input.

mod av;
pub(crate) mod audio;
pub(crate) mod audio_out;
pub(crate) mod camera;
pub(crate) mod devices;
pub(crate) mod encoder;
pub(crate) mod frames;
pub(crate) mod hooks;
mod keys;
pub(crate) mod reader;
pub(crate) mod recorder;
pub mod sck;
mod screen;
pub mod space;
mod window;
pub(crate) mod writer;
