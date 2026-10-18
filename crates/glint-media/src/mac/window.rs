use objc2_core_foundation::{CFDictionary, CGRect};
use objc2_core_graphics::{kCGWindowBounds, CGRectMakeWithDictionaryRepresentation, CGWindowListCopyWindowInfo, CGWindowListOption};
use std::ffi::c_void;

/// Window `id`'s frame in global points, top-left origin, or `None` once it is gone.
pub(crate) fn bounds(id: u32) -> Option<[f64; 4]> {
    let list = CGWindowListCopyWindowInfo(CGWindowListOption::OptionIncludingWindow, id)?;
    if list.count() == 0 {
        return None;
    }
    let info = unsafe { &*(list.value_at_index(0) as *const CFDictionary) };
    let dict = unsafe { info.value(kCGWindowBounds as *const _ as *const c_void) } as *const CFDictionary;
    let mut r = CGRect::default();
    if dict.is_null() || !unsafe { CGRectMakeWithDictionaryRepresentation(Some(&*dict), &mut r) } {
        return None;
    }
    Some([r.origin.x, r.origin.y, r.size.width, r.size.height])
}
