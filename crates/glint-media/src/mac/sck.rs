//! ScreenCaptureKit: shareable content, and display screenshots that can leave Glint out.

use anyhow::{anyhow, Context as _};
use block2::RcBlock;
use image::RgbaImage;
use objc2::rc::Retained;
use objc2::AnyThread;
use objc2_core_foundation::{CGPoint, CGRect, CGSize, CFRetained};
use objc2_core_graphics::{
    kCGColorSpaceSRGB, CGBitmapContextCreate, CGColorSpace, CGContext, CGImage, CGImageAlphaInfo,
};
use objc2_foundation::{NSArray, NSError};
use objc2_screen_capture_kit::{SCContentFilter, SCScreenshotManager, SCShareableContent, SCStreamConfiguration, SCWindow};
use std::sync::mpsc;
use std::time::Duration;

const TIMEOUT: Duration = Duration::from_secs(5);

fn error(e: *mut NSError) -> anyhow::Error {
    match unsafe { e.as_ref() } {
        Some(e) => anyhow!("{}", e.localizedDescription()),
        None => anyhow!("ScreenCaptureKit returned nothing"),
    }
}

/// What can be captured right now, on-screen windows only. Blocks until ScreenCaptureKit
/// answers; its completion handler runs on its own queue, so the main thread may wait.
pub(crate) fn shareable_content() -> anyhow::Result<Retained<SCShareableContent>> {
    let (tx, rx) = mpsc::channel();
    let block = RcBlock::new(move |content: *mut SCShareableContent, err: *mut NSError| {
        let result = unsafe { Retained::retain(content) }.ok_or_else(|| error(err));
        let _ = tx.send(result.map(SendBox));
    });
    unsafe { SCShareableContent::getShareableContentExcludingDesktopWindows_onScreenWindowsOnly_completionHandler(false, true, &block) };
    Ok(rx.recv_timeout(TIMEOUT).context("ScreenCaptureKit did not answer")??.0)
}

/// Carries an Objective-C object out of a completion handler. It is only read after the
/// handler is done with it.
struct SendBox<T>(T);
unsafe impl<T> Send for SendBox<T> {}

/// Captures display `display_id` at `width` x `height` pixels in sRGB, without the cursor.
/// Glint's own windows are left out unless `include_own`.
pub fn screenshot(display_id: u32, width: u32, height: u32, include_own: bool) -> anyhow::Result<RgbaImage> {
    let content = shareable_content()?;
    let display = unsafe { content.displays() }
        .into_iter()
        .find(|d| unsafe { d.displayID() } == display_id)
        .with_context(|| format!("display {display_id} is not shareable"))?;
    let own = std::process::id() as i32;
    let excluded: Vec<_> = if include_own {
        Vec::new()
    } else {
        unsafe { content.applications() }.into_iter().filter(|a| unsafe { a.processID() } == own).collect()
    };
    let filter = unsafe {
        SCContentFilter::initWithDisplay_excludingApplications_exceptingWindows(
            SCContentFilter::alloc(),
            &display,
            &NSArray::from_retained_slice(&excluded),
            &NSArray::<SCWindow>::new(),
        )
    };
    let config = unsafe { SCStreamConfiguration::new() };
    unsafe {
        config.setWidth(width as usize);
        config.setHeight(height as usize);
        config.setShowsCursor(false);
        config.setColorSpaceName(kCGColorSpaceSRGB);
    }

    let (tx, rx) = mpsc::channel();
    let block = RcBlock::new(move |image: *mut CGImage, err: *mut NSError| {
        let result = unsafe { image.as_ref() }.map(|i| unsafe { CFRetained::retain(i.into()) }).ok_or_else(|| error(err));
        let _ = tx.send(result.map(SendBox));
    });
    unsafe { SCScreenshotManager::captureImageWithFilter_configuration_completionHandler(&filter, &config, Some(&block)) };
    let image = rx.recv_timeout(TIMEOUT).context("the screenshot did not arrive")??.0;
    to_rgba(&image)
}

/// Draws `image` into an sRGB RGBA buffer. Screen pixels are opaque, so premultiplied and
/// straight alpha are the same.
fn to_rgba(image: &CGImage) -> anyhow::Result<RgbaImage> {
    let (w, h) = (CGImage::width(Some(image)), CGImage::height(Some(image)));
    let mut buf = vec![0u8; w * h * 4];
    let space = CGColorSpace::with_name(Some(unsafe { kCGColorSpaceSRGB })).context("sRGB color space")?;
    let ctx: CFRetained<CGContext> = unsafe {
        CGBitmapContextCreate(buf.as_mut_ptr().cast(), w, h, 8, w * 4, Some(&space), CGImageAlphaInfo::PremultipliedLast.0)
    }
    .context("CGBitmapContextCreate")?;
    let rect = CGRect::new(CGPoint::new(0.0, 0.0), CGSize::new(w as f64, h as f64));
    CGContext::draw_image(Some(&ctx), rect, Some(image));
    drop(ctx);
    RgbaImage::from_raw(w as u32, h as u32, buf).context("screenshot buffer size")
}
