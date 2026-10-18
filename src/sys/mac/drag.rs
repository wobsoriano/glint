//! Dragging a capture into other apps: an AppKit dragging session from the window under the
//! pointer, carrying the file URL and, for apps that take images, its pixels as PNG.

use objc2::rc::Retained;
use objc2::runtime::{NSObject, NSObjectProtocol, ProtocolObject};
use objc2::{define_class, msg_send, AllocAnyThread, DefinedClass, MainThreadMarker, MainThreadOnly};
use objc2_app_kit::{
    NSApplication, NSDragOperation, NSDraggingContext, NSDraggingItem, NSDraggingSession, NSDraggingSource, NSEvent, NSEventModifierFlags,
    NSEventType, NSImage, NSPasteboardItem, NSPasteboardTypeFileURL, NSPasteboardTypePNG, NSWindow,
};
use objc2_foundation::{NSArray, NSData, NSPoint, NSRect, NSSize, NSString, NSURL};
use anyhow::Context as _;
use std::cell::{Cell, RefCell};
use std::path::Path;

type Done = Box<dyn FnOnce(anyhow::Result<bool>)>;

pub struct SourceIvars {
    done: Cell<Option<Done>>,
}

define_class!(
    /// Reports the end of a drag to the callback it holds.
    #[unsafe(super(NSObject))]
    #[thread_kind = MainThreadOnly]
    #[name = "GlintDragSource"]
    #[ivars = SourceIvars]
    pub struct DragSource;

    unsafe impl NSObjectProtocol for DragSource {}

    unsafe impl NSDraggingSource for DragSource {
        #[unsafe(method(draggingSession:sourceOperationMaskForDraggingContext:))]
        fn operation_mask(&self, _session: &NSDraggingSession, _context: NSDraggingContext) -> NSDragOperation {
            NSDragOperation::Copy
        }

        #[unsafe(method(draggingSession:endedAtPoint:operation:))]
        fn ended(&self, _session: &NSDraggingSession, _point: NSPoint, operation: NSDragOperation) {
            if let Some(done) = self.ivars().done.take() {
                done(Ok(operation != NSDragOperation::None));
            }
            ACTIVE.with(|a| a.borrow_mut().take());
        }
    }
);

thread_local! {
    /// The source of the running drag. AppKit does not keep it alive.
    static ACTIVE: RefCell<Option<Retained<DragSource>>> = const { RefCell::new(None) };
}

/// Starts dragging `path` from the Glint window under the pointer. `done` runs when the drag
/// ends, with whether something took the drop, or with the error that kept it from starting.
pub fn start(path: &Path, image: Option<&image::RgbaImage>, thumb: &gpui::RenderImage, done: Done) {
    let mut done = Some(done);
    if let Err(e) = try_start(path, image, thumb, &mut done) {
        if let Some(done) = done.take() {
            done(Err(e));
        }
    }
}

/// Takes `done` only once the session owns it.
fn try_start(path: &Path, image: Option<&image::RgbaImage>, thumb: &gpui::RenderImage, done: &mut Option<Done>) -> anyhow::Result<()> {
    let mtm = MainThreadMarker::new().context("drags start on the main thread")?;
    let app = NSApplication::sharedApplication(mtm);
    let mouse = NSEvent::mouseLocation();
    let number = NSWindow::windowNumberAtPoint_belowWindowWithWindowNumber(mouse, 0, mtm);
    let window = app.windowWithWindowNumber(number).context("no Glint window under the pointer")?;
    let view = window.contentView().context("the window has no view")?;

    let item = NSPasteboardItem::new();
    let url = NSURL::fileURLWithPath(&NSString::from_str(&path.to_string_lossy()));
    if let Some(s) = url.absoluteString() {
        item.setString_forType(&s, unsafe { NSPasteboardTypeFileURL });
    }
    if let Some(img) = image {
        let png = crate::shell::encode(img, crate::settings::ExportFormat::Png, 100)?;
        item.setData_forType(&NSData::with_bytes(&png), unsafe { NSPasteboardTypePNG });
    }
    let dragging = NSDraggingItem::initWithPasteboardWriter(NSDraggingItem::alloc(), &ProtocolObject::from_retained(item));
    let at = window.convertPointFromScreen(mouse);
    let picture = ns_image(thumb);
    let size = picture.as_ref().map_or(NSSize::new(64.0, 64.0), |p| p.size());
    let frame = NSRect::new(NSPoint::new(at.x - size.width / 2.0, at.y - size.height / 2.0), size);
    unsafe { dragging.setDraggingFrame_contents(frame, picture.as_deref().map(|p| p as &objc2::runtime::AnyObject)) };

    let event = app.currentEvent().filter(|e| matches!(e.r#type(), NSEventType::LeftMouseDragged | NSEventType::LeftMouseDown));
    let event = match event {
        Some(e) => e,
        None => NSEvent::mouseEventWithType_location_modifierFlags_timestamp_windowNumber_context_eventNumber_clickCount_pressure(
            NSEventType::LeftMouseDragged,
            at,
            NSEventModifierFlags::empty(),
            0.0,
            window.windowNumber(),
            None,
            0,
            1,
            1.0,
        )
        .ok_or_else(|| anyhow::anyhow!("could not make a drag event"))?,
    };

    let source = DragSource::alloc(mtm).set_ivars(SourceIvars { done: Cell::new(done.take()) });
    let source: Retained<DragSource> = unsafe { msg_send![super(source), init] };
    view.beginDraggingSessionWithItems_event_source(&NSArray::from_retained_slice(&[dragging]), &event, ProtocolObject::from_ref(&*source));
    ACTIVE.with(|a| *a.borrow_mut() = Some(source));
    Ok(())
}

/// The card thumbnail as an NSImage at half its pixel size, so it shows at Retina scale.
fn ns_image(thumb: &gpui::RenderImage) -> Option<Retained<NSImage>> {
    let size = thumb.size(0);
    let (w, h) = (u32::from(size.width) as u32, u32::from(size.height) as u32);
    let mut rgba = thumb.as_bytes(0)?.to_vec();
    for px in rgba.chunks_exact_mut(4) {
        px.swap(0, 2);
    }
    let img = image::RgbaImage::from_raw(w, h, rgba)?;
    let png = crate::shell::encode(&img, crate::settings::ExportFormat::Png, 100).ok()?;
    let picture = NSImage::initWithData(NSImage::alloc(), &NSData::with_bytes(&png))?;
    picture.setSize(NSSize::new(w as f64 / 2.0, h as f64 / 2.0));
    Some(picture)
}
