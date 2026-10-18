//! Glint's screen space on macOS, and the one place that converts to and from it.
//!
//! macOS describes displays in points, top-left origin, with a backing scale per display.
//! Glint works in pixels, like the physical-pixel virtual desktop on Windows: every rectangle
//! the capture, overlay and recording code passes around is in this space.
//!
//! A display's pixel origin is its point origin times the largest scale of any display, and
//! its pixel size is its point size times its own scale. Displays keep their arrangement and
//! never overlap, and with a single display a pixel is simply a point times the scale. Mixed
//! scales leave gaps between displays, which no pointer or window can be in.

use crate::devices::{DisplayInfo, Rect};

/// One display as CoreGraphics reports it.
#[derive(Clone, Debug, PartialEq)]
pub(crate) struct RawDisplay {
    pub id: u32,
    /// Origin and size in global points, top-left origin.
    pub bounds: [f64; 4],
    pub scale: f32,
    pub primary: bool,
    pub builtin: bool,
}

/// Places `raw` in pixel space, primary first.
pub(crate) fn layout(mut raw: Vec<RawDisplay>) -> Vec<DisplayInfo> {
    raw.sort_by_key(|d| !d.primary);
    let global = raw.iter().map(|d| d.scale).fold(1.0, f32::max) as f64;
    let mut others = 0;
    raw.into_iter()
        .enumerate()
        .map(|(index, d)| {
            let [x, y, w, h] = d.bounds;
            let s = d.scale as f64;
            let name = if d.builtin {
                "Built-in Display".to_string()
            } else {
                others += 1;
                format!("Display {others}")
            };
            DisplayInfo {
                index,
                name,
                rect: Rect {
                    x: (x * global).round() as i32,
                    y: (y * global).round() as i32,
                    width: (w * s).round() as u32,
                    height: (h * s).round() as u32,
                },
                scale: d.scale,
                handle: d.id as isize,
                primary: d.primary,
            }
        })
        .collect()
}

fn global_scale(displays: &[DisplayInfo]) -> f64 {
    displays.iter().map(|d| d.scale).fold(1.0, f32::max) as f64
}

/// The display's origin and size in points.
pub fn point_bounds(displays: &[DisplayInfo], d: &DisplayInfo) -> [f64; 4] {
    let g = global_scale(displays);
    let s = d.scale as f64;
    [d.rect.x as f64 / g, d.rect.y as f64 / g, d.rect.width as f64 / s, d.rect.height as f64 / s]
}

fn nearest<'a>(displays: &'a [DisplayInfo], dist: impl Fn(&DisplayInfo) -> f64) -> Option<&'a DisplayInfo> {
    displays.iter().min_by(|a, b| dist(a).total_cmp(&dist(b)))
}

fn outside(p: f64, start: f64, len: f64) -> f64 {
    (start - p).max(p - (start + len)).max(0.0)
}

/// Converts a global point to Glint pixels, through the display that holds it or else the
/// nearest one.
pub fn to_pixels(displays: &[DisplayInfo], x: f64, y: f64) -> (i32, i32) {
    let Some(d) = nearest(displays, |d| {
        let [bx, by, bw, bh] = point_bounds(displays, d);
        outside(x, bx, bw) + outside(y, by, bh)
    }) else {
        return (x.round() as i32, y.round() as i32);
    };
    let [bx, by, _, _] = point_bounds(displays, d);
    let s = d.scale as f64;
    ((d.rect.x as f64 + (x - bx) * s).round() as i32, (d.rect.y as f64 + (y - by) * s).round() as i32)
}

/// Converts Glint pixels to a global point, through the display that holds them or else the
/// nearest one.
pub fn to_points(displays: &[DisplayInfo], x: i32, y: i32) -> (f64, f64) {
    let (x, y) = (x as f64, y as f64);
    let Some(d) = nearest(displays, |d| {
        outside(x, d.rect.x as f64, d.rect.width as f64) + outside(y, d.rect.y as f64, d.rect.height as f64)
    }) else {
        return (x, y);
    };
    let [bx, by, _, _] = point_bounds(displays, d);
    let s = d.scale as f64;
    (bx + (x - d.rect.x as f64) / s, by + (y - d.rect.y as f64) / s)
}

/// Converts a pixel rectangle to points through the display that holds its center, so a rect
/// on one display keeps that display's scale.
pub fn rect_to_points(displays: &[DisplayInfo], rect: [i32; 4]) -> [f64; 4] {
    let [x, y, w, h] = rect;
    let (cx, cy) = to_points(displays, x + w / 2, y + h / 2);
    let s = displays
        .iter()
        .find(|d| {
            let (px, py) = (x + w / 2, y + h / 2);
            px >= d.rect.x && py >= d.rect.y && px < d.rect.x + d.rect.width as i32 && py < d.rect.y + d.rect.height as i32
        })
        .map_or(1.0, |d| d.scale as f64);
    let (pw, ph) = (w as f64 / s, h as f64 / s);
    [cx - pw / 2.0, cy - ph / 2.0, pw, ph]
}

/// Converts a point rectangle to pixels through the display that holds its center.
pub fn rect_to_pixels(displays: &[DisplayInfo], rect: [f64; 4]) -> [i32; 4] {
    let [x, y, w, h] = rect;
    let (cx, cy) = to_pixels(displays, x + w / 2.0, y + h / 2.0);
    let s = displays
        .iter()
        .find(|d| {
            let [bx, by, bw, bh] = point_bounds(displays, d);
            outside(x + w / 2.0, bx, bw) + outside(y + h / 2.0, by, bh) == 0.0
        })
        .map_or(1.0, |d| d.scale as f64);
    let (pw, ph) = ((w * s).round() as i32, (h * s).round() as i32);
    [cx - pw / 2, cy - ph / 2, pw, ph]
}

#[cfg(test)]
mod tests {
    use super::*;

    fn raw(id: u32, bounds: [f64; 4], scale: f32, primary: bool) -> RawDisplay {
        RawDisplay { id, bounds, scale, primary, builtin: id == 1 }
    }

    #[test]
    fn single_retina_display_is_points_times_scale() {
        let list = layout(vec![raw(1, [0.0, 0.0, 1512.0, 982.0], 2.0, true)]);
        assert_eq!(list[0].rect, Rect { x: 0, y: 0, width: 3024, height: 1964 });
        assert_eq!(list[0].name, "Built-in Display");
        assert_eq!(to_pixels(&list, 100.0, 50.5), (200, 101));
        assert_eq!(to_points(&list, 200, 101), (100.0, 50.5));
    }

    #[test]
    fn mixed_scales_never_overlap_and_round_trip() {
        // A 1x display to the left of and above a 2x laptop, which is primary.
        let list = layout(vec![
            raw(2, [-1920.0, -1080.0, 1920.0, 1080.0], 1.0, false),
            raw(1, [0.0, 0.0, 1512.0, 982.0], 2.0, true),
        ]);
        assert_eq!(list[0].handle, 1, "primary first");
        assert_eq!(list[1].rect, Rect { x: -3840, y: -2160, width: 1920, height: 1080 });
        assert_eq!(list[1].name, "Display 1");
        let ends = |d: &DisplayInfo| (d.rect.x + d.rect.width as i32, d.rect.y + d.rect.height as i32);
        assert!(ends(&list[1]).0 <= list[0].rect.x && ends(&list[1]).1 <= list[0].rect.y);

        for (x, y) in [(-1919.0, -1079.0), (-1.0, -1.0), (0.0, 0.0), (1511.0, 981.0), (700.5, 20.0)] {
            let (px, py) = to_pixels(&list, x, y);
            assert_eq!(to_points(&list, px, py), (x, y), "({x}, {y}) via ({px}, {py})");
        }
        assert_eq!(to_pixels(&list, -1.0, -1.0), (-1921, -1081), "1x pixels on the 1x display");
    }

    #[test]
    fn rects_convert_through_the_display_at_their_center() {
        let list = layout(vec![
            raw(1, [0.0, 0.0, 1512.0, 982.0], 2.0, true),
            raw(2, [1512.0, 0.0, 1920.0, 1080.0], 1.0, false),
        ]);
        assert_eq!(list[1].rect.x, 3024);
        assert_eq!(rect_to_points(&list, [200, 100, 400, 300]), [100.0, 50.0, 200.0, 150.0]);
        assert_eq!(rect_to_points(&list, [3024 + 10, 20, 100, 50]), [1522.0, 20.0, 100.0, 50.0]);
        assert_eq!(rect_to_pixels(&list, [100.0, 50.0, 200.0, 150.0]), [200, 100, 400, 300]);
        assert_eq!(rect_to_pixels(&list, [1522.0, 20.0, 100.0, 50.0]), [3034, 20, 100, 50]);
    }
}
