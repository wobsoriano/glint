//! Shared AVFoundation plumbing for the MP4 readers and the writer.

use super::sck::SendBox;
use anyhow::{anyhow, bail, Context};
use block2::RcBlock;
use objc2::rc::Retained;
use objc2::runtime::AnyObject;
use objc2_av_foundation::{
    AVAssetReader, AVAssetReaderOutput, AVAssetReaderStatus, AVAssetReaderTrackOutput, AVAssetTrack, AVMediaType, AVURLAsset,
    AVURLAssetPreferPreciseDurationAndTimingKey,
};
use objc2_core_foundation::CFString;
use objc2_core_media::{kCMTimePositiveInfinity, CMSampleBuffer, CMTime, CMTimeRange};
use objc2_foundation::{NSArray, NSDictionary, NSError, NSNumber, NSString, NSURL};
use std::path::Path;
use std::sync::mpsc;
use std::time::Duration;

pub(super) type Settings = NSDictionary<NSString, AnyObject>;

/// An AVFoundation string constant. They are only missing when the framework is not linked.
pub(super) fn constant<T: ?Sized>(c: Option<&'static T>) -> &'static T {
    c.expect("AVFoundation constant is linked")
}

/// A Core Foundation string key used where Foundation expects an `NSString`. The two are
/// toll-free bridged.
pub(super) fn cf_key(s: &'static CFString) -> &'static NSString {
    unsafe { &*(s as *const CFString as *const NSString) }
}

pub(super) fn settings(pairs: &[(&NSString, &AnyObject)]) -> Retained<Settings> {
    let (keys, values): (Vec<&NSString>, Vec<&AnyObject>) = pairs.iter().copied().unzip();
    NSDictionary::from_slices(&keys, &values)
}

pub(super) fn number(n: i64) -> Retained<NSNumber> {
    NSNumber::new_i64(n)
}

pub(super) fn ns_error(e: Option<Retained<NSError>>, what: &str) -> anyhow::Error {
    match e {
        Some(e) => anyhow!("{what}: {}", e.localizedDescription()),
        None => anyhow!("{what}"),
    }
}

pub(super) fn time_ms(t: CMTime) -> u64 {
    if t.timescale <= 0 || t.value <= 0 {
        return 0;
    }
    (t.value as i128 * 1000 / t.timescale as i128) as u64
}

/// The sample at time `t` at `rate` samples per second, rounded to the nearest.
pub(super) fn time_frames(t: CMTime, rate: u32) -> u64 {
    if t.timescale <= 0 || t.value <= 0 {
        return 0;
    }
    ((t.value as i128 * rate as i128 + t.timescale as i128 / 2) / t.timescale as i128) as u64
}

pub(super) fn open_asset(path: &Path) -> anyhow::Result<Retained<AVURLAsset>> {
    if !path.exists() {
        bail!("{} does not exist", path.display());
    }
    let url = NSURL::from_file_path(path).with_context(|| format!("file URL for {}", path.display()))?;
    let yes = NSNumber::new_bool(true);
    let options = settings(&[(unsafe { AVURLAssetPreferPreciseDurationAndTimingKey }, &yes)]);
    Ok(unsafe { AVURLAsset::URLAssetWithURL_options(&url, Some(&options)) })
}

/// The first track of `kind` in `asset`, or `None` when it has none. Blocks until the asset
/// has loaded its track list.
pub(super) fn first_track(asset: &AVURLAsset, kind: &AVMediaType) -> anyhow::Result<Option<Retained<AVAssetTrack>>> {
    let (tx, rx) = mpsc::channel();
    let block = RcBlock::new(move |tracks: *mut NSArray<AVAssetTrack>, err: *mut NSError| {
        let result = match unsafe { Retained::retain(tracks) } {
            Some(tracks) => Ok(tracks.firstObject()),
            None => Err(unsafe { Retained::retain(err) }),
        };
        let _ = tx.send(SendBox(result));
    });
    unsafe { asset.loadTracksWithMediaType_completionHandler(kind, &block) };
    let SendBox(result) = rx.recv_timeout(Duration::from_secs(30)).context("AVFoundation did not load the tracks")?;
    result.map_err(|e| ns_error(e, "load tracks"))
}

/// Decoded samples of one track. An `AVAssetReader` cannot seek once it has started, so
/// `restart` replaces it with a new one that reads from the requested time.
pub(super) struct TrackReader {
    asset: Retained<AVURLAsset>,
    track: Retained<AVAssetTrack>,
    settings: Retained<Settings>,
    output: Retained<AVAssetReaderTrackOutput>,
    reader: Retained<AVAssetReader>,
}

impl TrackReader {
    pub fn new(asset: Retained<AVURLAsset>, track: Retained<AVAssetTrack>, settings: Retained<Settings>) -> anyhow::Result<Self> {
        let (reader, output) = start(&asset, &track, &settings, 0)?;
        Ok(Self { asset, track, settings, output, reader })
    }

    /// Reads from `ms` on. The first sample may start a little earlier.
    pub fn restart(&mut self, ms: u64) -> anyhow::Result<()> {
        unsafe { self.reader.cancelReading() };
        (self.reader, self.output) = start(&self.asset, &self.track, &self.settings, ms)?;
        Ok(())
    }

    /// The next decoded sample buffer, or `None` at the end of the track.
    pub fn next(&mut self) -> anyhow::Result<Option<Retained<CMSampleBuffer>>> {
        if let Some(sample) = unsafe { self.output.copyNextSampleBuffer() } {
            return Ok(Some(sample));
        }
        match unsafe { self.reader.status() } {
            AVAssetReaderStatus::Failed => Err(ns_error(unsafe { self.reader.error() }, "decode")),
            _ => Ok(None),
        }
    }
}

impl Drop for TrackReader {
    fn drop(&mut self) {
        unsafe { self.reader.cancelReading() };
    }
}

fn start(
    asset: &AVURLAsset,
    track: &AVAssetTrack,
    settings: &Settings,
    ms: u64,
) -> anyhow::Result<(Retained<AVAssetReader>, Retained<AVAssetReaderTrackOutput>)> {
    let reader = unsafe { AVAssetReader::assetReaderWithAsset_error(asset) }.map_err(|e| ns_error(Some(e), "create AVAssetReader"))?;
    let output = unsafe { AVAssetReaderTrackOutput::assetReaderTrackOutputWithTrack_outputSettings(track, Some(settings)) };
    unsafe { output.setAlwaysCopiesSampleData(false) };
    let as_output: &AVAssetReaderOutput = &output;
    if !unsafe { reader.canAddOutput(as_output) } {
        bail!("AVAssetReader cannot produce the requested format");
    }
    unsafe { reader.addOutput(as_output) };
    if ms > 0 {
        let range = unsafe { CMTimeRange::new(CMTime::new(ms as i64, 1000), kCMTimePositiveInfinity) };
        unsafe { reader.setTimeRange(range) };
    }
    if !unsafe { reader.startReading() } {
        return Err(ns_error(unsafe { reader.error() }, "start reading"));
    }
    Ok((reader, output))
}
