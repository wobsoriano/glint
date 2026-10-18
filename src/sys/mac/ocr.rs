//! Text recognition with Vision's `VNRecognizeTextRequest`, which runs on-device and takes the
//! whole image at once.

use crate::annotate::R;
use crate::ocr::{Line, Word};
use anyhow::{anyhow, Result};
use image::RgbaImage;
use objc2::rc::Retained;
use objc2::{msg_send, AnyThread};
use objc2_core_foundation::CGRect;
use objc2_foundation::{NSArray, NSData, NSDictionary, NSError, NSRange};
use objc2_vision::{
    VNImageRequestHandler, VNRecognizeTextRequest, VNRecognizedText, VNRectangleObservation, VNRequest, VNRequestTextRecognitionLevel,
};

/// Recognizes text in `img`. Blocking; call from a background thread.
pub fn recognize(img: &RgbaImage) -> Result<Vec<Line>> {
    let png = crate::shell::encode(img, crate::settings::ExportFormat::Png, 100)?;
    let (w, h) = (img.width() as f64, img.height() as f64);
    // Vision boxes are normalized with a bottom-left origin.
    let to_px = |b: CGRect| R {
        x: (b.origin.x * w) as f32,
        y: ((1.0 - b.origin.y - b.size.height) * h) as f32,
        w: (b.size.width * w) as f32,
        h: (b.size.height * h) as f32,
    };

    let request = VNRecognizeTextRequest::new();
    request.setRecognitionLevel(VNRequestTextRecognitionLevel::Accurate);
    request.setUsesLanguageCorrection(true);
    let handler = VNImageRequestHandler::initWithData_options(VNImageRequestHandler::alloc(), &NSData::with_bytes(&png), &NSDictionary::new());
    let as_request: Retained<VNRequest> = Retained::into_super(Retained::into_super(request.clone()));
    let requests = NSArray::from_retained_slice(&[as_request]);
    handler.performRequests_error(&requests).map_err(|e| anyhow!("text recognition failed: {}", e.localizedDescription()))?;

    let mut lines = Vec::new();
    for obs in request.results().into_iter().flatten() {
        let Some(top) = obs.topCandidates(1).firstObject() else { continue };
        let text = top.string().to_string();
        let words: Vec<Word> = word_ranges(&text)
            .filter_map(|(word, range)| Some(Word { text: word.to_string(), rect: to_px(word_box(&top, range)?) }))
            .collect();
        if words.is_empty() {
            continue;
        }
        let rect = to_px(unsafe { obs.boundingBox() });
        lines.push(Line { words, rect });
    }
    lines.sort_by(|a, b| {
        let same_row = (a.rect.y - b.rect.y).abs() < a.rect.h.min(b.rect.h) * 0.5;
        if same_row {
            a.rect.x.total_cmp(&b.rect.x)
        } else {
            a.rect.y.total_cmp(&b.rect.y)
        }
    });
    Ok(lines)
}

/// The whitespace-separated words of `text` with their UTF-16 ranges, which is how NSString
/// counts.
fn word_ranges(text: &str) -> impl Iterator<Item = (&str, NSRange)> {
    let mut at = 0usize;
    let mut out = Vec::new();
    for (i, part) in text.split(char::is_whitespace).enumerate() {
        if i > 0 {
            at += 1;
        }
        let len = part.encode_utf16().count();
        if len > 0 {
            out.push((part, NSRange::new(at, len)));
        }
        at += len;
    }
    out.into_iter()
}

/// The normalized box of `range` in a candidate. The generated bindings lack this method.
fn word_box(text: &VNRecognizedText, range: NSRange) -> Option<CGRect> {
    let mut error: *mut NSError = std::ptr::null_mut();
    let obs: Option<Retained<VNRectangleObservation>> = unsafe { msg_send![text, boundingBoxForRange: range, error: &mut error] };
    obs.map(|o| unsafe { o.boundingBox() })
}
