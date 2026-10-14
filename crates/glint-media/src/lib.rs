//! Windows media engine: screen, camera, audio and input recording, MP4 encode and decode.
//!
//! Everything here is blocking and meant for background threads. Nothing depends on a UI
//! framework. See `docs/architecture.md` for the on-disk session contract.

pub mod audio;
pub mod clock;
mod d3d;
pub mod devices;
mod encoder;
pub mod input;
mod mf;
pub mod reader;
pub mod wav;
pub mod writer;

pub use audio::{LevelMeter, MicMonitor};
pub use clock::MediaClock;
pub use devices::{cameras, displays, microphones, AudioDevice, CameraDevice, DeviceId, DisplayInfo, Rect};
pub use reader::{read_mp4_audio, VideoFrame, VideoReader};
pub use wav::{read_wav, WavData};
pub use writer::{
    available_codecs, decodable_codecs, transcode, AudioParams, TranscodeOptions, VideoCodec, VideoParams, VideoWriter,
    WriteSummary,
};
