use crate::settings::ExportFormat;
use anyhow::Context as _;
use std::path::{Path, PathBuf};

pub use crate::sys::shell::{
    caption_text, copy_files, drag_out, exclude_own_windows_from_capture, mica, place, refuse_drops, register_open_with, reveal,
    save_failed_dialog, work_area_at_cursor,
};

pub fn encode(img: &image::RgbaImage, format: ExportFormat, quality: u8) -> anyhow::Result<Vec<u8>> {
    let mut out = std::io::Cursor::new(Vec::new());
    match format {
        ExportFormat::Png => img.write_to(&mut out, image::ImageFormat::Png)?,
        ExportFormat::Jpeg => {
            let rgb = image::DynamicImage::ImageRgba8(img.clone()).to_rgb8();
            image::codecs::jpeg::JpegEncoder::new_with_quality(&mut out, quality.clamp(1, 100)).encode_image(&rgb)?;
        }
    }
    Ok(out.into_inner())
}

pub fn export(img: &image::RgbaImage, dir: &Path, stem: &str, format: ExportFormat, quality: u8) -> anyhow::Result<PathBuf> {
    let bytes = encode(img, format, quality)?;
    std::fs::create_dir_all(dir).with_context(|| format!("creating {}", dir.display()))?;
    let path = unique_path(dir, &sanitize(stem), format.extension());
    std::fs::write(&path, bytes).with_context(|| format!("writing {}", path.display()))?;
    Ok(path)
}

pub fn unique_path(dir: &Path, stem: &str, ext: &str) -> PathBuf {
    let mut p = dir.join(format!("{stem}.{ext}"));
    let mut n = 2;
    while p.exists() {
        p = dir.join(format!("{stem} ({n}).{ext}"));
        n += 1;
    }
    p
}

pub fn sanitize(name: &str) -> String {
    let s: String = name
        .chars()
        .map(|c| if c.is_control() || r#"<>:"/\|?*"#.contains(c) { '_' } else { c })
        .collect();
    let s = s.trim().trim_end_matches('.').to_string();
    if s.is_empty() {
        "Capture".into()
    } else {
        s
    }
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum SaveChoice {
    Retry,
    ChooseFolder,
    Cancel,
}

pub const OPEN_WITH_TYPES: [&str; 6] = [".png", ".jpg", ".jpeg", ".webp", ".bmp", ".gif"];
pub const OPEN_WITH_VIDEO_TYPES: [&str; 3] = [".mp4", ".mov", ".m4v"];