# Feature parity checklist

Glint targets Screendrop's feature set on Windows, minus cloud sharing, Apple Shortcuts/Siri and
Sparkle. An item is checked only after it was exercised in the running app or by a test that
calls the real code. The evidence column points at that proof.

## Capture

- [x] Capture display, window, area as screenshot — W4 drove area/CLI captures; window via picker
- [x] Global hotkeys for full screen, window, area, recording picker, capture text; rebinding in Settings with failure message and fallback — W4 30-rebind-fail, 31-rebind-ok
- [x] Self-timer 3/5/10 s — W11 11-timer-toast (3 s chip and toast); W12 07/08-timer5 (capture landed 4.8 s after the tray click), 09-tray-menu-10s, 11-timer10-card (9.9 s)
- [x] PNG or JPEG with quality; export folder; Save bypasses dialog option — W6 s7-saved (JPEG to export folder)
- [x] Optional window shadow on window captures — W12 17-window-shadow-on (1666x984, transparent corners) vs 18-window-shadow-off (1582x891, opaque)
- [x] Glint windows excluded from captures by default, opt-in to include — W4 28-excluded
- [x] Capture Text: OCR to clipboard in reading order, toast, nothing saved — W12 20-text-toast; two-column clipboard text read back line by line; Library and export file counts unchanged
- [x] Every capture is imported into the Library before automation runs — store.rs import tests; W6 s2-card

## After capture and preview stack

- [x] Screenshot automation: show preview, copy, save, open editor, pin (any combination) — W4 settings pane + W6 flow
- [x] Recording automation: show preview, copy, save, open Studio — W12 42-rec-preview-card, 43-rec-copy (clipboard FileDrop of the .mp4), 43-rec-save (2.9 s mp4 in export folder), 43-rec-studio
- [x] Floating preview stack, multiple cards, left or right side, auto close delay, dismiss after drag — W4 05-stack, 33/34-throw
- [x] Card actions: copy, drag out to other apps, save, compress to JPEG, pin, quick look, annotate/edit, delete, dismiss, copy text (OCR) — W12 22-card-copy through 32-card-edit, 60/61-dragout (PNG dropped into Explorer, app alive); clipboard and files read back
- [x] Rearrangeable card actions: four corner slots, center buttons, hidden tray (Settings > Overlay) — W4 12-overlay-reordered
- [x] Collapsed stack becomes a peek tab — W4 04-peek-b
- [x] Save failure dialog with retry or choose another location — W4 26-save-failed

## Library

- [x] Window with sidebar filter: All, Screenshots, Recordings — W4 13-inspector
- [x] Grid and list views, search, sort by date/name/modified; layout, sort, inspector remembered — W4 16-18
- [x] Inspector: dimensions, dates, duration, disk usage, editable state, location — W4 13-inspector
- [x] Quick look on double-click or Space; Edit reopens editor or Studio with saved edits — W4 22-quicklook, W6 s5, W8b actions::edit
- [x] Multi-select (Ctrl-click, Shift-click, Ctrl+A); copy, export, reveal in Explorer, move to Recycle Bin — W4 19-20; W14 p2-02 (Copy with 2 selected: the clipboard FileDropList holds both image.png files), p2-05 (Export… into a chosen folder wrote both PNGs), p2-06 (Show in Explorer opened the Library folder with the 2 item folders selected)
- [x] Rename with Enter (title stored in metadata) — W4 rename test + 32-grid-hover
- [x] Opens from tray and Ctrl+Shift+L; normal launch opens Library, autostart stays in tray — W12 01-launch-library, 02-autostart-tray, 03-ctrl-shift-l, 06-tray-open-library, 13/14 (launched with the Run entry's own `--background` command line: no windows)
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
- [x] Save, Done, copy, pin; failed save keeps editor open — W12 33-editor-copy, 34-editor-pin, 35-editor-save (1774x1083 PNG exported), 36-done-failed (blocked write, editor stays), 37-done-ok (closes, edit.json has both arrows)

## Screen recording

- [x] Picker: display, window, area; camera; microphone; system audio toggle; 1/3/5 s timer; teleprompter script — W7 a1-picker
- [x] Countdown cancellable with Cancel or Esc — W7 c1/c2
- [x] Missing devices warn and continue — W12 54-picker-camera-unavailable, 56-rec-warns, 57-rec-continued-card (remembered camera and mic absent; 2.8 s screen.mp4 recorded)
- [ ] Live camera preview before recording — not verifiable: no camera on the test machine
- [x] Area guide: highlighted region, dimmed rest, click-through, excluded from recording — W7 a1, b2-frame
- [x] Control bar: elapsed time, pause/resume, restart, stop, delete — W7 p1, f1
- [x] Separate masters: screen without cursor, mic, system audio, input timeline — W1 record.rs run, w1-input.png; W14 p1 (4.8 s display recording with the Arctis 7P+ mic and system audio on while a 440 Hz tone played: system.wav 4.773 s with signal, rms 0.023; mic.wav 4.773 s, present but digital silence because nobody spoke; screen.mp4 4.80 s; input.json holds the stop shortcut at 4766 ms)
- [ ] Separate masters: camera — not verifiable: no camera on the test machine (the picker shows "No camera")

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
- [x] Camera track: show/hide, drag, size, roundness — W8b s19 (synthetic camera file; no hardware camera)
- [x] On-device transcription; captions editor with seek, position, size, karaoke highlight — W9 s7-s12, s17, export frame1/2
- [x] Transcript editing: click to seek, shift-click select, cut; remove filler words; trim silences — W9 s20-s25, t6
- [x] Export: quality, resolution, format, 30/60 fps, motion blur, audio; progress; cancel; cached renders — W8a proof.log, W8b s22/s23
- [x] Trim-and-compress editor for regular video files — W10 shots 10/11 (4K trim to 2 s 720p, ffprobe), 31-38

## macOS

The sections above record the Windows build. This section tracks the macOS port on its own: a
line is checked only after it ran in the macOS app or in a test that calls the real macOS code.
M1 is the first port session (2026-10-17, MacBook Pro, 2x built-in display, macOS 27.2).

- [x] Capture full screen, window (click) and area; previous area; Capture Text through Vision — M1 area 500x300 pt gave a 1000x600 px clipboard image; OCR read terminal lines in order
- [x] Global hotkeys (⌃⇧ defaults) and the menu bar menu with key equivalents — M1 ⌃⇧3 added a capture; menu Open Library raised the Library
- [x] Glint windows excluded from captures (macOS 14+, ScreenCaptureKit filter) — M1 full-screen capture with the Library and stack open showed neither
- [x] Preview stack in the work area; card drag into Finder; dismiss after drag — M1 PNG landed in a Finder folder
- [x] Library: grid, ⌘-click multi-select, ⌘C copies files, Delete moves to the Trash — M1 two file URLs on the pasteboard; both captures in the Trash
- [x] Annotation editor: drawing, ⌘Z, system font and dark appearance, traffic lights — M1 arrow drawn and undone
- [x] Single instance with CLI forwarding; launch at login through a LaunchAgent — M1 second `glint --library` exited 0; plist written and removed
- [x] MP4 encode/decode, transcode and trim — `roundtrip` (8) and `glint-studio` `engine` (6) tests on macOS
- [x] Screen recording with system audio and input, constant 30 fps, pause excluded — M1 18 s session: 545 frames, system.wav 18.15 s, input.json with cursor sprites; `record_mac` ignored tests
- [x] Microphone track, level meter, live-caption tap; camera track and preview — ignored hardware tests on the built-in mic and FaceTime camera (not yet from the recording picker UI)
- [x] Studio opens a macOS session and plays it back with audio — M1 timeline, auto zooms, cursor overlay; playback advanced in real time
- [ ] Recording picker with a microphone and the camera selected, in the app
- [ ] Studio export, captions and transcription (whisper.cpp with Metal)
- [ ] Trim & Compress, Quick Look, pins, self-timer, teleprompter on macOS
- [ ] Permission prompts from a signed Glint.app (Screen Recording, Input Monitoring, Microphone, Camera)
- [ ] Mixed-scale multi-display setups (covered by `glint_media::space` unit tests only)
