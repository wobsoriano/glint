//! Text recognition with the OCR engine built into the system: Windows.Media.Ocr on Windows,
//! Vision on macOS.

use crate::annotate::R;

pub use crate::sys::ocr::recognize;

#[derive(Clone, Debug)]
pub struct Word {
    pub text: String,
    pub rect: R,
}

#[derive(Clone, Debug)]
pub struct Line {
    pub words: Vec<Word>,
    pub rect: R,
}

impl Line {
    pub fn text(&self) -> String {
        self.words
            .iter()
            .map(|w| w.text.as_str())
            .collect::<Vec<_>>()
            .join(" ")
    }
}

/// Plain text in reading order.
pub fn to_text(lines: &[Line]) -> String {
    let mut out = String::new();
    let mut prev: Option<&Line> = None;
    for l in lines {
        if let Some(p) = prev {
            let same_row = (l.rect.y - p.rect.y).abs() < p.rect.h.min(l.rect.h) * 0.5;
            out.push_str(if same_row { "  " } else { "\n" });
        }
        out.push_str(&l.text());
        prev = Some(l);
    }
    out
}
