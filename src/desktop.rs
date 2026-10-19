pub use crate::sys::desktop::{bring_to_front, desktop_wallpaper, init_app, make_topmost, set_launch_at_login, single_instance, start_window_move};

pub fn copy_image(img: &image::RgbaImage) -> anyhow::Result<()> {
    let mut cb = arboard::Clipboard::new()?;
    cb.set_image(arboard::ImageData {
        width: img.width() as usize,
        height: img.height() as usize,
        bytes: std::borrow::Cow::Borrowed(img.as_raw()),
    })?;
    Ok(())
}

pub fn copy_text(text: &str) -> anyhow::Result<()> {
    arboard::Clipboard::new()?.set_text(text.to_string())?;
    Ok(())
}

/// Converts straight RGBA into the BGRA frame GPUI expects.
pub fn render_image(img: &image::RgbaImage) -> std::sync::Arc<gpui::RenderImage> {
    let mut bgra = img.clone();
    for px in bgra.chunks_exact_mut(4) {
        px.swap(0, 2);
    }
    std::sync::Arc::new(gpui::RenderImage::new(smallvec::smallvec![image::Frame::new(bgra)]))
}
