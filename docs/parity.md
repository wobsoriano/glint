# Feature parity checklist

Glint targets Screendrop's feature set on Windows, minus cloud sharing, Apple Shortcuts/Siri and
Sparkle. An item is checked only after it was exercised in the running app or by a test that
calls the real code. The evidence column points at that proof.

## Capture

- [ ] Capture display, window, area as screenshot
- [ ] Global hotkeys for full screen, window, area, recording picker, capture text; rebinding in Settings with failure message and fallback
- [ ] Self-timer 3/5/10 s
- [ ] PNG or JPEG with quality; export folder; Save bypasses dialog option
- [ ] Optional window shadow on window captures
- [ ] Glint windows excluded from captures by default, opt-in to include
- [ ] Capture Text: OCR to clipboard in reading order, toast, nothing saved
- [ ] Every capture is imported into the Library before automation runs

## After capture and preview stack

- [ ] Screenshot automation: show preview, copy, save, open editor, pin (any combination)
- [ ] Recording automation: show preview, copy, save, open Studio
- [ ] Floating preview stack, multiple cards, left or right side, auto close delay, dismiss after drag
- [ ] Card actions: copy, drag out to other apps, save, compress to JPEG, pin, quick look, annotate/edit, delete, dismiss, copy text (OCR)
- [ ] Rearrangeable card actions: four corner slots, center buttons, hidden tray (Settings > Overlay)
- [ ] Collapsed stack becomes a peek tab
- [ ] Save failure dialog with retry or choose another location

## Library

- [ ] Window with sidebar filter: All, Screenshots, Recordings
- [ ] Grid and list views, search, sort by date/name/modified; layout, sort, inspector remembered
- [ ] Inspector: dimensions, dates, duration, disk usage, editable state, location
- [ ] Quick look on double-click or Space; Edit reopens editor or Studio with saved edits
- [ ] Multi-select (Ctrl-click, Shift-click, Ctrl+A); copy, export, reveal in Explorer, move to Recycle Bin
- [ ] Rename with Enter (title stored in metadata)
- [ ] Opens from tray and Ctrl+Shift+L; normal launch opens Library, autostart stays in tray
- [ ] Open images from Explorer (Open with Glint), imports a copy, opens editor per image

## Annotation editor

- [ ] Non-destructive: base preserved, edit.json sidecar, reopen restores everything
- [ ] Tools: rectangle, filled rectangle, circle, line, arrow, freehand, numbered markers, text (font, weight, alignment, size), highlight-dim, pixelate, blur
- [ ] Select, move, resize, multi-select, delete, undo, redo
- [ ] Smart redaction with editable strength
- [ ] Crop presets: freeform, original, 1:1, 16:9, 9:16, 4:3, 3:2; remaps annotations; undoable
- [ ] Backgrounds: solid, gradient, custom image, wallpapers
- [ ] Presets: save, import, export versioned `.glintpreset` JSON
- [ ] Padding, corner radius, shadow, aspect ratio, nine-point alignment
- [ ] Border
- [ ] Text watermark with placement and typography
- [ ] Perspective, rotation, zoom, pan mockup controls
- [ ] Progressive blur, radial or directional, clipped or bleeding
- [ ] Canvas zoom: fit (Ctrl+1), actual (Ctrl+0), in/out to 1000%, Ctrl+scroll around pointer, scroll pans
- [ ] Save, Done, copy, pin; failed save keeps editor open

## Screen recording

- [ ] Picker: display, window, area; camera; microphone; system audio toggle; 1/3/5 s timer; teleprompter script
- [ ] Countdown cancellable with Cancel or Esc
- [ ] Live camera preview before recording; missing devices warn and continue
- [ ] Area guide: highlighted region, dimmed rest, click-through, excluded from recording
- [ ] Control bar: elapsed time, pause/resume, restart, stop, delete
- [ ] Separate masters: screen without cursor, camera, mic, system audio, input timeline

## Teleprompter

- [ ] Script with visible line count, floating pill at the top of the screen
- [ ] Follows narration with on-device speech recognition when a mic is enabled; static otherwise; never breaks recording

## Recording Studio

- [ ] Session package; project reopens from Library
- [ ] Timeline with thumbnails, scrub, split at playhead, trim, delete, per-clip speed, undo/redo
- [ ] Background, padding, radius, shadow; aspect original/16:9/9:16/1:1/4:5; Fill (pointer-following) or Fit
- [ ] Crop the screen card with presets
- [ ] Studio presets: save named, set default
- [ ] Auto zoom from clicks; add, remove, enable, resize, retime zooms; follow pointer or fixed focus
- [ ] Reconstructed cursor with adjustable size; click highlights; keystroke captions in six positions
- [ ] Camera track: show/hide, drag, size, roundness
- [ ] On-device transcription; captions editor with seek, position, size, karaoke highlight
- [ ] Transcript editing: click to seek, shift-click select, cut; remove filler words; trim silences
- [ ] Export: quality, resolution, format, 30/60 fps, motion blur, audio; progress; cancel; cached renders
- [x] Trim-and-compress editor for regular video files
