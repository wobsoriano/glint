//! Windows media engine: screen, camera, audio and input recording, MP4 encode and decode.
//!
//! Everything here is blocking and meant for background threads. Nothing depends on a UI
//! framework. See `docs/architecture.md` for the on-disk session contract.

pub mod audio;
pub mod audio_out;
pub mod camera;
pub mod clock;
mod cursor;
mod d3d;
pub mod devices;
mod encoder;
pub mod frames;
mod hooks;
pub mod input;
mod keys;
mod mf;
pub mod reader;
pub mod recorder;
mod screen;
pub mod wav;
pub mod writer;

pub use audio::{LevelMeter, MicMonitor};
pub use audio_out::{AudioClock, AudioOutput};
pub use camera::CameraPreview;
pub use clock::MediaClock;
pub use devices::{cameras, displays, microphones, AudioDevice, CameraDevice, DeviceId, DisplayInfo, Rect};
pub use frames::FrameReader;
pub use recorder::{CaptureSource, Fps, RecordOptions, RecordSummary, Recorder};
pub use reader::{read_mp4_audio, AudioReader, VideoFrame, VideoReader};
pub use wav::{read_wav, WavData};
pub use writer::{
    available_codecs, decodable_codecs, AAC_BYTES_PER_SEC, shrink, transcode, AudioParams, TranscodeOptions, VideoCodec, VideoParams, VideoWriter,
    WriteSummary,
};
