//! Media engine: screen, camera, audio and input recording, MP4 encode and decode.
//!
//! Everything here is blocking and meant for background threads. Nothing depends on a UI
//! framework. See `docs/architecture.md` for the on-disk session contract.
//!
//! The modules at this level are shared by every platform. Each platform backend (`win`,
//! `mac`) supplies the same set of primitives: device enumeration, the recorder and its
//! tracks, MP4 readers and the writer, and audio playback.

pub mod audio;
pub mod clock;
pub mod devices;
pub mod frames;
pub mod input;
pub mod reader;
pub mod recorder;
pub mod wav;
pub mod writer;

#[cfg(windows)]
mod win;
#[cfg(windows)]
use win as backend;

#[cfg(target_os = "macos")]
mod mac;
#[cfg(target_os = "macos")]
use mac as backend;
#[cfg(target_os = "macos")]
pub use mac::{sck, space};

pub use audio::{LevelMeter, MicMonitor};
pub use backend::audio_out::{AudioClock, AudioOutput};
pub use backend::camera::CameraPreview;
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
