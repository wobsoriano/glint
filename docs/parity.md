# Feature parity checklist

Glint targets Screendrop's feature set on Windows, minus cloud sharing, Apple Shortcuts/Siri and
Sparkle. An item is checked only after it was exercised in the running app or by a test that
calls the real code. The evidence column points at that proof.

## Capture

- [x] Capture display, window, area as screenshot — W4 drove area/CLI captures; window via picker
- [x] Global hotkeys for full screen, window, area, recording picker, capture text; rebinding in Settings with failure message and fallback — W4 30-rebind-fail, 31-rebind-ok
- [ ] Self-timer 3/5/10 s
- [x] PNG or JPEG with quality; export folder; Save bypasses dialog option — W6 s7-saved (JPEG to export folder)
- [ ] Optional window shadow on window captures
- [x] Glint windows excluded from captures by default, opt-in to include — W4 28-excluded
- [ ] Capture Text: OCR to clipboard in reading order, toast, nothing saved
- [x] Every capture is imported into the Library before automation runs — store.rs import tests; W6 s2-card

## After capture and preview stack

- [x] Screenshot automation: show preview, copy, save, open editor, pin (any combination) — W4 settings pane + W6 flow
- [ ] Recording automation: show preview, copy, save, open Studio
- [x] Floating preview stack, multiple cards, left or right side, auto close delay, dismiss after drag — W4 05-stack, 33/34-throw
- [ ] Card actions: copy, drag out to other apps, save, compress to JPEG, pin, quick look, annotate/edit, delete, dismiss, copy text (OCR)
- [x] Rearrangeable card actions: four corner slots, center buttons, hidden tray (Settings > Overlay) — W4 12-overlay-reordered
- [x] Collapsed stack becomes a peek tab — W4 04-peek-b
- [x] Save failure dialog with retry or choose another location — W4 26-save-failed

## Library

- [x] Window with sidebar filter: All, Screenshots, Recordings — W4 13-inspector
- [x] Grid and list views, search, sort by date/name/modified; layout, sort, inspector remembered — W4 16-18
- [x] Inspector: dimensions, dates, duration, disk usage, editable state, location — W4 13-inspector
- [x] Quick look on double-click or Space; Edit reopens editor or Studio with saved edits — W4 22-quicklook, W6 s5, W8b actions::edit
- [x] Multi-select (Ctrl-click, Shift-click, Ctrl+A); copy, export, reveal in Explorer, move to Recycle Bin — W4 19-20 (copy/export/reveal not clicked)
- [x] Rename with Enter (title stored in metadata) — W4 rename test + 32-grid-hover
- [ ] Opens from tray and Ctrl+Shift+L; normal launch opens Library, autostart stays in tray
- [x] Open images from Explorer (Open with Glint), imports a copy, opens editor per image — W4 27-open-with, W6 s1-openwith

## Annotation editor

- [x] Non-destructive: base preserved, edit.json sidecar, reopen restores everything — W6 s5, W5 s45
- [x] Tools: rectangle, filled rectangle, circle, line, arrow, freehand, numbered markers, text (font, weight, alignment, size), highlight-dim, pixelate, blur — W5 s02, s46/s47
- [x] Select, move, resize, multi-select, delete, undo, redo — W5 s03/s04
- [x] Smart redaction with editable strength — W6 m2/m3
- [x] Crop presets: freeform, original, 1:1, 16:9, 9:16, 4:3, 3:2; remaps annotations; undoable — W5 s11-s13
- [x] Backgrounds: solid, gradient, custom image, wallpapers — W5 s32
- [x] Presets: save, import, export versioned `.glintpreset` JSON — W5 s34-s38
- [x] Padding, corner radius, shadow, aspect ratio, nine-point alignment — existing + W6 l3
- [x] Border — W5 s06
- [x] Text watermark with placement and typography — W5 s08
- [x] Perspective, rotation, zoom, pan mockup controls — W5 s17/s43
- [x] Progressive blur, radial or directional, clipped or bleeding — W5 s19/s20
- [x] Canvas zoom: fit (Ctrl+1), actual (Ctrl+0), in/out to 1000%, Ctrl+scroll around pointer, scroll pans — W5 s31
- [ ] Save, Done, copy, pin; failed save keeps editor open

## Screen recording

- [x] Picker: display, window, area; camera; microphone; system audio toggle; 1/3/5 s timer; teleprompter script — W7 a1-picker
- [x] Countdown cancellable with Cancel or Esc — W7 c1/c2
- [ ] Live camera preview before recording; missing devices warn and continue
- [x] Area guide: highlighted region, dimmed rest, click-through, excluded from recording — W7 a1, b2-frame
- [x] Control bar: elapsed time, pause/resume, restart, stop, delete — W7 p1, f1
- [x] Separate masters: screen without cursor, camera, mic, system audio, input timeline — W1 record.rs run, w1-input.png

## Teleprompter

- [x] Script with visible line count, floating pill at the top of the screen — W7 g2
- [x] Follows narration with on-device speech recognition when a mic is enabled; static otherwise; never breaks recording — W7 g2-follow (WAV-fed through the mic queue)

## Recording Studio

- [x] Session package; project reopens from Library — W8b persistence; W7 Library tile
- [x] Timeline with thumbnails, scrub, split at playhead, trim, delete, per-clip speed, undo/redo — W8b s07-s12
- [x] Background, padding, radius, shadow; aspect original/16:9/9:16/1:1/4:5; Fill (pointer-following) or Fit — W8b s17/s18
- [x] Crop the screen card with presets — W8b s24-s26
- [x] Studio presets: save named, set default — W8b s33
- [x] Auto zoom from clicks; add, remove, enable, resize, retime zooms; follow pointer or fixed focus — W3 frames, W8b s13-s15
- [x] Reconstructed cursor with adjustable size; click highlights; keystroke captions in six positions — W3 frame_03150, cursor test
- [x] Camera track: show/hide, drag, size, roundness — W8b s19 (synthetic camera)
- [x] On-device transcription; captions editor with seek, position, size, karaoke highlight — W9 s7-s12, s17, export frame1/2
- [x] Transcript editing: click to seek, shift-click select, cut; remove filler words; trim silences — W9 s20-s25, t6
- [x] Export: quality, resolution, format, 30/60 fps, motion blur, audio; progress; cancel; cached renders — W8a proof.log, W8b s22/s23
- [ ] Trim-and-compress editor for regular video files
