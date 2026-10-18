use crate::ocr::{Line, Word};
use crate::annotate::R;
use anyhow::{anyhow, Result};
use image::RgbaImage;
use windows::Graphics::Imaging::{BitmapPixelFormat, SoftwareBitmap};
use windows::Media::Ocr::OcrEngine;
use windows::Storage::Streams::DataWriter;

/// Recognizes text in `img`. Blocking; call from a background thread.
pub fn recognize(img: &RgbaImage) -> Result<Vec<Line>> {
    let engine = OcrEngine::TryCreateFromUserProfileLanguages()
        .map_err(|e| anyhow!("No OCR language pack installed: {e}"))?;
    let max_dim = OcrEngine::MaxImageDimension().unwrap_or(2600).max(1000);

    // Small captures are upscaled — Windows OCR struggles with tiny UI text.
    let (w, h) = img.dimensions();
    let k = if w.max(h) * 2 <= max_dim && w * h < 1_500_000 { 2.0 } else { 1.0 };
    let work = if k > 1.0 {
        image::imageops::resize(img, w * 2, h * 2, image::imageops::FilterType::CatmullRom)
    } else {
        img.clone()
    };
    let (ww, wh) = work.dimensions();

    let overlap = 160u32;
    let step = max_dim - overlap;
    let mut lines: Vec<Line> = Vec::new();
    let mut tile_of: Vec<usize> = Vec::new();
    let mut tile_idx = 0usize;
    let mut ty = 0;
    loop {
        let th = (wh - ty).min(max_dim);
        let mut tx = 0;
        loop {
            let tw = (ww - tx).min(max_dim);
            let tile = image::imageops::crop_imm(&work, tx, ty, tw, th).to_image();
            tile_idx += 1;
            for line in recognize_tile(&engine, &tile)? {
                let map = |r: R| R {
                    x: (r.x + tx as f32) / k,
                    y: (r.y + ty as f32) / k,
                    w: r.w / k,
                    h: r.h / k,
                };
                let line = Line {
                    rect: map(line.rect),
                    words: line
                        .words
                        .into_iter()
                        .map(|w| Word {
                            text: w.text,
                            rect: map(w.rect),
                        })
                        .collect(),
                };
                // Skip duplicates from overlapping neighbor tiles.
                let dup = lines.iter().zip(&tile_of).any(|(l, &t)| {
                    t != tile_idx
                        && l.rect.intersect(&line.rect).is_some_and(|i| {
                            i.area() > 0.5 * l.rect.area().min(line.rect.area())
                        })
                });
                if !dup {
                    lines.push(line);
                    tile_of.push(tile_idx);
                }
            }
            if tx + tw >= ww {
                break;
            }
            tx += step;
        }
        if ty + th >= wh {
            break;
        }
        ty += step;
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

fn recognize_tile(engine: &OcrEngine, img: &RgbaImage) -> Result<Vec<Line>> {
    let mut bgra = img.as_raw().clone();
    for px in bgra.chunks_exact_mut(4) {
        px.swap(0, 2);
        px[3] = 255;
    }
    let writer = DataWriter::new()?;
    writer.WriteBytes(&bgra)?;
    let buffer = writer.DetachBuffer()?;
    let bitmap = SoftwareBitmap::CreateCopyFromBuffer(
        &buffer,
        BitmapPixelFormat::Bgra8,
        img.width() as i32,
        img.height() as i32,
    )?;
    let result = engine.RecognizeAsync(&bitmap)?.join()?;
    let mut out = Vec::new();
    for line in result.Lines()? {
        let mut words = Vec::new();
        for word in line.Words()? {
            let r = word.BoundingRect()?;
            words.push(Word {
                text: word.Text()?.to_string(),
                rect: R {
                    x: r.X,
                    y: r.Y,
                    w: r.Width,
                    h: r.Height,
                },
            });
        }
        if words.is_empty() {
            continue;
        }
        let rect = words[1..].iter().fold(words[0].rect, |acc, w| acc.union(&w.rect));
        out.push(Line { words, rect });
    }
    Ok(out)
}
