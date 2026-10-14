//! Rendering the current cursor shape into an RGBA sprite.

use windows::Win32::Graphics::Gdi::{
    CreateCompatibleDC, CreateDIBSection, DeleteDC, DeleteObject, GetObjectW, SelectObject, BITMAP, BITMAPINFO, BITMAPINFOHEADER, BI_RGB,
    DIB_RGB_COLORS, HDC, HGDIOBJ,
};
use windows::Win32::UI::WindowsAndMessaging::{DrawIconEx, GetIconInfo, HCURSOR, HICON, ICONINFO, DI_NORMAL};

/// A cursor shape as straight-alpha RGBA pixels.
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) struct CursorImage {
    pub width: u32,
    pub height: u32,
    pub hotspot: [u32; 2],
    pub rgba: Vec<u8>,
}

impl CursorImage {
    /// Content hash used to recognise a shape seen before.
    pub fn hash(&self) -> u64 {
        let mut h: u64 = 0xcbf29ce484222325;
        for b in self.rgba.iter().chain(self.hotspot.iter().flat_map(|v| v.to_le_bytes()).collect::<Vec<_>>().iter()) {
            h ^= *b as u64;
            h = h.wrapping_mul(0x100000001b3);
        }
        h ^ ((self.width as u64) << 32 | self.height as u64)
    }
}

/// Draws the cursor on black and on white and recovers colour and alpha from the pair.
/// Inverting (XOR) cursors come out white and opaque, which keeps them visible.
pub(crate) fn render(cursor: HCURSOR) -> Option<CursorImage> {
    unsafe {
        let icon = HICON(cursor.0);
        let mut info = ICONINFO::default();
        GetIconInfo(icon, &mut info).ok()?;
        let (width, height) = bitmap_size(&info);
        let result = (width > 0 && height > 0).then(|| {
            let on_black = draw(icon, width, height, 0x00);
            let on_white = draw(icon, width, height, 0xFF);
            on_black.zip(on_white).map(|(b, w)| CursorImage { width, height, hotspot: [info.xHotspot, info.yHotspot], rgba: merge(&b, &w) })
        });
        if !info.hbmColor.is_invalid() {
            let _ = DeleteObject(HGDIOBJ(info.hbmColor.0));
        }
        if !info.hbmMask.is_invalid() {
            let _ = DeleteObject(HGDIOBJ(info.hbmMask.0));
        }
        result.flatten()
    }
}

unsafe fn bitmap_size(info: &ICONINFO) -> (u32, u32) {
    let mut bm = BITMAP::default();
    let (handle, halve) = if info.hbmColor.is_invalid() { (info.hbmMask, true) } else { (info.hbmColor, false) };
    if unsafe { GetObjectW(HGDIOBJ(handle.0), std::mem::size_of::<BITMAP>() as i32, Some(&mut bm as *mut _ as *mut _)) } == 0 {
        return (0, 0);
    }
    let h = if halve { bm.bmHeight / 2 } else { bm.bmHeight };
    (bm.bmWidth.max(0) as u32, h.max(0) as u32)
}

/// Renders the icon over a solid grey level and returns BGRA rows, top-down.
unsafe fn draw(icon: HICON, width: u32, height: u32, fill: u8) -> Option<Vec<u8>> {
    let dc: HDC = unsafe { CreateCompatibleDC(None) };
    if dc.is_invalid() {
        return None;
    }
    let info = BITMAPINFO {
        bmiHeader: BITMAPINFOHEADER {
            biSize: std::mem::size_of::<BITMAPINFOHEADER>() as u32,
            biWidth: width as i32,
            biHeight: -(height as i32),
            biPlanes: 1,
            biBitCount: 32,
            biCompression: BI_RGB.0,
            ..Default::default()
        },
        ..Default::default()
    };
    let mut bits = std::ptr::null_mut();
    let dib = unsafe { CreateDIBSection(Some(dc), &info, DIB_RGB_COLORS, &mut bits, None, 0) }.ok();
    let result = dib.and_then(|dib| {
        let len = width as usize * height as usize * 4;
        unsafe {
            std::ptr::write_bytes(bits as *mut u8, fill, len);
            let old = SelectObject(dc, HGDIOBJ(dib.0));
            let ok = DrawIconEx(dc, 0, 0, icon, width as i32, height as i32, 0, None, DI_NORMAL).is_ok();
            SelectObject(dc, old);
            let pixels = ok.then(|| std::slice::from_raw_parts(bits as *const u8, len).to_vec());
            let _ = DeleteObject(HGDIOBJ(dib.0));
            pixels
        }
    });
    let _ = unsafe { DeleteDC(dc) };
    result
}

fn merge(on_black: &[u8], on_white: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(on_black.len());
    for (b, w) in on_black.chunks_exact(4).zip(on_white.chunks_exact(4)) {
        // Over black a pixel shows colour * alpha; over white it adds (255 - alpha).
        let alpha = (255 - (w[0] as i32 - b[0] as i32).clamp(0, 255)) as u8;
        let unpremul = |c: u8| if alpha == 0 { 0 } else { ((c as u32 * 255) / alpha as u32).min(255) as u8 };
        out.extend([unpremul(b[2]), unpremul(b[1]), unpremul(b[0]), alpha]);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn merge_recovers_alpha_and_colour() {
        let black = [0, 0, 128, 0, 0, 0, 0, 0, 0, 0, 255, 0];
        let white = [127, 127, 255, 0, 255, 255, 255, 0, 0, 0, 255, 0];
        let rgba = merge(&black, &white);
        assert_eq!(&rgba[0..4], &[255, 0, 0, 128], "half-transparent red");
        assert_eq!(&rgba[4..8], &[0, 0, 0, 0], "fully transparent");
        assert_eq!(&rgba[8..12], &[255, 0, 0, 255], "opaque red");
    }

    #[test]
    #[ignore = "needs an interactive desktop session"]
    fn renders_the_arrow_cursor() {
        use windows::Win32::UI::WindowsAndMessaging::{LoadCursorW, IDC_ARROW};
        let arrow = unsafe { LoadCursorW(None, IDC_ARROW) }.unwrap();
        let img = render(arrow).unwrap();
        assert!(img.width >= 16 && img.height >= 16, "{}x{}", img.width, img.height);
        let opaque = img.rgba.chunks_exact(4).filter(|p| p[3] == 255).count();
        let clear = img.rgba.chunks_exact(4).filter(|p| p[3] == 0).count();
        assert!(opaque > 50, "an arrow has a solid body, got {opaque} opaque pixels");
        assert!(clear > opaque, "an arrow is mostly empty space, got {clear} clear vs {opaque} opaque");
        assert_eq!(img.hotspot, [0, 0], "the arrow's tip is its hotspot");
        assert_eq!(img.hash(), render(arrow).unwrap().hash());
    }
}
