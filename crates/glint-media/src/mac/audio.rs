//! TEMPORARY STUB from the mac-record branch. The microphone branch replaces this file; drop
//! this version when merging.

use super::unsupported;
use crate::audio::{AudioStats, LevelMeter};
use crate::clock::MediaClock;
use crate::devices::DeviceId;
use std::convert::Infallible;
use std::path::{Path, PathBuf};
use std::sync::mpsc::SyncSender;
use std::sync::Arc;

pub struct MicMonitor(Infallible);

impl MicMonitor {
    pub fn start(_mic: Option<DeviceId>) -> anyhow::Result<Self> {
        unsupported("the microphone level meter")
    }

    pub fn level(&self) -> f32 {
        match self.0 {}
    }
}

pub(crate) struct AudioTrack {
    pub path: PathBuf,
    never: Infallible,
}

impl AudioTrack {
    pub fn start_mic(_id: &DeviceId, _path: &Path, _clock: Arc<MediaClock>, _tap: Option<SyncSender<Vec<f32>>>) -> anyhow::Result<Self> {
        unsupported("microphone recording")
    }

    pub fn level(&self) -> &Arc<LevelMeter> {
        match self.never {}
    }

    pub fn stop(self) -> anyhow::Result<AudioStats> {
        match self.never {}
    }
}
