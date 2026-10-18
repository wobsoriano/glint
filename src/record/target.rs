use glint_media::{CaptureSource, DisplayInfo, Rect};
use windows::Win32::Foundation::HWND;
use windows::Win32::Graphics::Gdi::{GetMonitorInfoW, MonitorFromWindow, HMONITOR, MONITORINFO, MONITOR_DEFAULTTONEAREST};

#[derive(Clone, Debug, PartialEq)]
pub enum Target {
    Display(DisplayInfo),
    Window { hwnd: isize, title: String, display: DisplayInfo },
    Area { rect: [i32; 4], display: DisplayInfo },
}

impl Target {
    pub fn display_at_cursor() -> anyhow::Result<Self> {
        let list = glint_media::displays()?;
        let (x, y) = crate::capture::cursor_pos();
        let d = display_containing(&list, x, y).unwrap_or(&list[0]).clone();
        Ok(Target::Display(d))
    }

    pub fn window(hwnd: isize, title: String) -> anyhow::Result<Self> {
        let list = glint_media::displays()?;
        let monitor = unsafe { MonitorFromWindow(HWND(hwnd as *mut _), MONITOR_DEFAULTTONEAREST) }.0 as isize;
        let display = list.iter().find(|d| d.handle == monitor).unwrap_or(&list[0]).clone();
        Ok(Target::Window { hwnd, title, display })
    }

    /// An area, clipped to the display holding its center. Sizes are rounded down to even
    /// pixels for the H.264 encoder. `None` when nothing usable is left.
    pub fn area(rect: [i32; 4]) -> anyhow::Result<Option<Self>> {
        let list = glint_media::displays()?;
        let (cx, cy) = (rect[0] + rect[2] / 2, rect[1] + rect[3] / 2);
        let Some(d) = display_containing(&list, cx, cy) else { return Ok(None) };
        Ok(clip_area(rect, &d.rect).map(|rect| Target::Area { rect, display: d.clone() }))
    }

    pub fn from_selection(rect: [i32; 4]) -> anyhow::Result<Option<Self>> {
        let list = glint_media::displays()?;
        if let Some(d) = list.iter().find(|d| display_rect(d) == rect) {
            return Ok(Some(Target::Display(d.clone())));
        }
        Self::area(rect)
    }

    pub fn display(&self) -> &DisplayInfo {
        match self {
            Target::Display(d) | Target::Window { display: d, .. } | Target::Area { display: d, .. } => d,
        }
    }

    pub fn source(&self) -> CaptureSource {
        match self {
            Target::Display(d) => CaptureSource::Display(d.index),
            Target::Window { hwnd, .. } => CaptureSource::Window(*hwnd),
            Target::Area { rect, display } => CaptureSource::Area {
                display: display.index,
                rect: Rect { x: rect[0] - display.rect.x, y: rect[1] - display.rect.y, width: rect[2] as u32, height: rect[3] as u32 },
            },
        }
    }

    pub fn label(&self) -> String {
        match self {
            Target::Display(d) => d.name.clone(),
            Target::Window { title, .. } => title.clone(),
            Target::Area { rect, display } => format!("{} × {} area of {}", rect[2], rect[3], display.name),
        }
    }

    pub fn frame(&self) -> [i32; 4] {
        match self {
            Target::Display(d) => display_rect(d),
            Target::Window { hwnd, display, .. } => {
                crate::capture::window_rect(HWND(*hwnd as *mut _)).unwrap_or_else(|| display_rect(display))
            }
            Target::Area { rect, .. } => *rect,
        }
    }
}

pub fn display_rect(d: &DisplayInfo) -> [i32; 4] {
    [d.rect.x, d.rect.y, d.rect.width as i32, d.rect.height as i32]
}

pub fn work_area(d: &DisplayInfo) -> [i32; 4] {
    let mut info = MONITORINFO { cbSize: std::mem::size_of::<MONITORINFO>() as u32, ..Default::default() };
    if unsafe { GetMonitorInfoW(HMONITOR(d.handle as *mut _), &mut info) }.as_bool() {
        let r = info.rcWork;
        [r.left, r.top, r.right - r.left, r.bottom - r.top]
    } else {
        display_rect(d)
    }
}

fn display_containing(list: &[DisplayInfo], x: i32, y: i32) -> Option<&DisplayInfo> {
    list.iter().find(|d| x >= d.rect.x && y >= d.rect.y && x < d.rect.x + d.rect.width as i32 && y < d.rect.y + d.rect.height as i32)
}

fn clip_area(rect: [i32; 4], d: &Rect) -> Option<[i32; 4]> {
    let x = rect[0].max(d.x);
    let y = rect[1].max(d.y);
    let r = (rect[0] + rect[2]).min(d.x + d.width as i32);
    let b = (rect[1] + rect[3]).min(d.y + d.height as i32);
    let (w, h) = ((r - x) & !1, (b - y) & !1);
    (w >= 16 && h >= 16).then_some([x, y, w, h])
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn areas_are_clipped_to_the_display_and_made_even() {
        let d = Rect { x: -1920, y: 0, width: 1920, height: 1080 };
        assert_eq!(clip_area([-2000, 100, 501, 301], &d), Some([-1920, 100, 420, 300]));
        assert_eq!(clip_area([-100, 1070, 400, 300], &d), None, "10 px tall after clipping");
        assert_eq!(clip_area([0, 0, 50, 50], &d), None, "outside the display");
    }
}
