# Feature parity checklist

Glint targets Screendrop's feature set on Windows, minus cloud sharing, Apple Shortcuts, Siri
and Sparkle. A line is checked only after it was driven in the packaged Go build on a real
desktop, or proved by a test that calls the real code. Each line names the scripts that check
it and what they showed.

The evidence comes from the verification pass of 2026-10-23 and 2026-10-24, which
`docs/go-port/verification.md` writes up item by item. That pass ran the packaged `Glint.exe`
0.2.0 on Windows 11, on one 1920x1080 display at 100% scale. Its screenshots and logs stayed on
that machine, because they show the desktop. Every check is a script, so rerun the script
instead of looking for the files. Run the desktop scripts inside the desktop lock:

```powershell
$env:GLINT_BIN = '<folder with glint.exe (the packaged Glint.exe), glintcheck.exe and record.exe>'
$env:GLINT_OUT = '<evidence folder>'
go run ./tools/desklock -- powershell -NoProfile -File internal/app/cmd/glintcheck/scenarios/area.ps1
```

A bare script name such as `area.ps1` is a `glintcheck` scenario in
`internal/app/cmd/glintcheck/scenarios/`. Scripts under `tools/verify/` are the verification
pass's own checks. A checked line that says "Not driven" had its main path driven and the named
parts not run.

## Capture

- [x] **Capture display, window, area as screenshot.** `area.ps1` compares the area capture with the screen read before the shortcut, and 0 of 150500 pixels differ. `captures.ps1` takes a window by Space and a click, then Ctrl+Shift+3 and Ctrl+Shift+5.
- [x] **Global hotkeys for full screen, window, area, recording picker, capture text. Rebinding in Settings, with a failure message and a fallback.** `prefs.ps1` moves the area shortcut to Ctrl+Alt+K, which then captures, and the old Ctrl+Shift+4 does nothing. `tools/verify/settings.ps1` shows Ctrl+Shift+3 refused as used by full screen and F12 refused as not registrable, and Ctrl+Shift+4 still captures.
- [x] **Self-timer 3, 5 and 10 s.** `tray.ps1` gets captures 2907, 4922 and 9963 ms after the menu item.
- [x] **PNG or JPEG with quality, export folder, option to save without the dialog.** `automation.ps1` saves JPEG and PNG into the export folder, and the PNG equals `image.png`. With the option off, `tools/verify/savedialog.ps1` sees Save ask and write to the chosen folder.
- [x] **Optional window shadow on window captures.** `captures.ps1` gets 1142x695 with transparent corners, against 1086x633 opaque without the shadow.
- [x] **Glint windows excluded from captures by default, opt-in to include them.** `automation.ps1` finds the Library absent from a full-screen capture by default and present with the option on.
- [x] **Capture Text puts OCR text on the clipboard in reading order, shows a toast and saves nothing.** `captures.ps1` reads the three stage lines back from the clipboard in order, and the Library count is unchanged.
- [x] **Every capture is imported into the Library before automation runs.** Every after-capture step reads the Library item. With the Library folder denied, `tools/verify/adversarial2.ps1` sees nothing run and a toast say why. `go test ./internal/store` covers the import itself.

## After capture and preview stack

- [x] **Screenshot automation, in any combination of show preview, copy, save, open editor and pin.** `automation.ps1` covers preview, copy and save, `editor.ps1` the editor, and `tools/verify/cards.ps1` Pin.
- [x] **Recording automation, in any combination of show preview, copy, save and open Studio.** `record.ps1` finds Studio's render on the clipboard after Copy. `studio.ps1` opens Studio after a recording. `tools/verify/rustrender.ps1` saves a 2866 ms MP4 into the export folder.
- [x] **Floating preview stack with several cards, on the left or right, an auto-close delay, and dismiss after drag.** `cards.ps1` and `tools/verify/cards.ps1` show three cards on each side, auto close at 3 s, and dismiss after a drag.
- [x] **Card actions: copy, drag out to other apps, save, compress to JPEG, pin, Quick Look, edit, delete, dismiss, copy text.** `cards.ps1` covers copy, save, Quick Look, pin, delete and a drag into Explorer. `tools/verify/cards.ps1` covers Copy Text, Compress (a JPEG on the clipboard as a file) and Dismiss. `editor.ps1` opens the editor from a card. Not driven: Save As.
- [x] **Rearrangeable card actions in four corner slots, center buttons and a hidden tray (Settings > Overlay).** `tools/verify/cards.ps1` sets a layout of Pin, Delete, Dismiss and More with Edit and Copy in the center, and the card honors it with the rest in More. Not driven: dragging in the Settings layout editor, which renders.
- [x] **A collapsed stack becomes a peek tab.** `tools/verify/cards.ps1` tucks the stack into the edge, and a click on the tab brings the cards back.
- [x] **Save failure dialog with retry or another location.** `automation.ps1` and `tools/verify/adversarial2.ps1` show it for a missing folder, a write-denied folder and an over-long file name.

## Library

- [x] **Window with a sidebar filter for All, Screenshots and Recordings.** `tools/verify/library.ps1`.
- [x] **Grid and list views, search, sort by date, name or modified. Layout, sort and inspector remembered.** `tools/verify/library.ps1` reopens the Library, and `settings.json` keeps the view, the sort and the inspector.
- [x] **Inspector with dimensions, dates, duration, disk usage, editing state and location.** `tools/verify/library.ps1` shows each for a recording and an edited screenshot.
- [x] **Quick Look on double-click or Space. Edit reopens the editor or Studio with saved edits.** `library.ps1` opens Quick Look by double-click, and `tools/verify/library.ps1` opens and closes it with Space. `editor.ps1` and `studio.ps1` reopen from the Library's Edit.
- [x] **Multi-select (Ctrl-click, Shift-click, Ctrl+A). Copy, export, reveal in Explorer, move to the Recycle Bin.** `tools/verify/library.ps1` and `library2.ps1` pass Ctrl-click, Shift-click, Ctrl+A, Copy of two files, Export of two PNGs and the Recycle Bin. `internal/app/cmd/glintcheck/scenarios/fixapp.ps1` selects two captures and clicks Show in Explorer; Explorer reports both selected.
- [x] **Rename with Enter, with the title stored in `meta.json`.** `library.ps1` reads the new title from `meta.json`.
- [x] **Opens from the tray and Ctrl+Shift+L. A plain launch opens the Library, and autostart stays in the tray.** `tray.ps1` and `library.ps1` open it. `tools/verify/realdata.ps1` sees a plain launch open the Library. `--background` shows no window.
- [x] **Open images from Explorer (Open with Glint), import a copy, and open the editor for each image.** `tools/packagecheck/install.ps1` checks that the installer registers `com.glint.app.<ext>` and that Explorer lists one Glint. `editor.ps1` runs `glint.exe <image>`, which imports a copy and opens the editor. Not driven: the Explorer menu itself.

## Annotation editor

- [x] **Non-destructive. The base is kept, edits go to the `edit.json` sidecar, and reopening restores everything.** In `editor.ps1`, `glintcheck edited` finds `image.png` equal to RenderDoc of `base.png` and `edit.json`. `tools/verify/editor3.ps1` reopens a capture with the saved look, and it closes without a prompt.
- [x] **Tools: rectangle, filled rectangle, circle, line, arrow, freehand, numbered markers, text (font, weight, alignment, size), highlight-dim, pixelate, blur.** `tools/verify/editor.ps1` draws one of each by its key, and `edit.json` holds each kind.
- [x] **Select, move, resize, multi-select, delete, undo, redo.** `tools/verify/editor.ps1` measures pixel diffs. Undo removes exactly the 90x81 blur box (7290 px) and redo restores it. Deleting a marquee pair removes 14580 px, and undo restores them. Not driven: resizing by a handle.
- [x] **Smart redaction with editable strength.** `tools/verify/misc.ps1` redacts an email, a phone number and an IP address shown by `tools/verify/secrets.ps1`. The strength slider shows on a redaction. Windows OCR returns no line for "4111 1111 1111 1111", so that card number stays visible. `go run ./tools/verify/redactprobe <png>` finds the 4242 and 5555 cards.
- [x] **Crop presets freeform, original, 1:1, 16:9, 9:16, 4:3 and 3:2. Crop remaps annotations and is undoable.** A 1:1 crop of a 500x301 capture is `{x:100,w:301,h:301}`, the arrow moves by -100, and undo restores both (`tools/verify/editor.ps1`, `editor3.ps1`).
- [x] **Backgrounds: solid, gradient, custom image, wallpapers.** `tools/verify/editor3.ps1` drives solid and gradient. Not driven: custom image and wallpaper.
- [x] **Presets: save, import and export of versioned `.glintpreset` JSON.** `tools/verify/presets.ps1` sees Save look write `presets.json`, Export write a `glint-presets` version 1 file, and Import read it back. A Rust-written `presets.json` lists Plain, Dusk and Photo.
- [x] **Padding, corner radius, shadow, aspect ratio, nine-point alignment.** `tools/verify/realdata.ps1` restores padding 64, radius 12 and shadow 40 from a Rust-written `edit.json`. Not driven: aspect ratio and the alignment grid.
- [x] **Border.** `tools/verify/editor3.ps1` saves and renders `effects.border.enabled`.
- [x] **Text watermark with placement and typography.** `tools/verify/editor3.ps1` turns it on with the default text, and it renders.
- [x] **Perspective, rotation, zoom and pan mockup controls.** `tools/verify/editor3.ps1` applies the Hero preset, which saves rot_x 14, rot_y -20 and rot_z 4. Not driven: zoom and pan.
- [x] **Progressive blur, radial or directional, clipped or bleeding.** `tools/verify/editor3.ps1` saves radial with bleed. Not driven: directional and clipped.
- [x] **Canvas zoom: fit (Ctrl+1), actual size (Ctrl+0), in and out to 1000%, Ctrl+scroll around the pointer, scroll to pan.** `tools/verify/editor.ps1` drives Ctrl+0, Ctrl+1 and Ctrl+scroll. Not driven: scroll to pan.
- [x] **Save, Done, copy, pin. A failed save keeps the editor open.** `editor.ps1` covers Save, Done, Copy and Pin. With the capture folder denied writes, `tools/verify/adversarial3.ps1` sees Done keep the editor open and `image.png` unchanged, then succeed once writes are allowed.

## Screen recording

- [x] **Picker with display, window and area, camera, microphone, a system audio toggle, a 1, 3 or 5 s timer, and a teleprompter script.** `tools/verify/record.ps1` and `record2.ps1` record a display, a window chosen by a click and Enter, and an 800x500 area, and open the microphone list. The camera chip shows "No camera".
- [x] **Countdown cancellable with Cancel or Esc.** `tools/verify/record.ps1` cancels a 3 s countdown both ways, and nothing is recorded.
- [x] **Missing devices warn and continue.** `tools/verify/record.ps1` remembers a camera and a microphone that are absent. The picker and the result both warn, and a 2668 ms recording lands.
- [ ] **Live camera preview before recording.** Not verifiable, because the test machine has no camera.
- [x] **Area guide with the region highlighted, the rest dimmed, click-through, and left out of the recording.** `tools/verify/record.ps1` shows the dashed outline and dimmed surroundings during an area recording. Not proven: click-through and the guide's absence from the recording.
- [x] **Control bar with elapsed time, pause and resume, restart, stop, delete.** `tools/verify/record.ps1` and `record2.ps1` pause for 2 s, and 3705 ms of about 5.5 s is recorded. Restart gives 1516 ms. Delete asks, then leaves nothing.
- [x] **Separate masters: screen without the cursor, microphone, system audio, input timeline.** `record.ps1` and `tools/verify/misc.ps1` write `screen.mp4` with no audio track and no pointer in the first frame, `system.wav`, a 550858-byte `mic.wav`, and `input.json`.
- [ ] **Separate masters: camera.** Not verifiable, because the test machine has no camera.

## Teleprompter

- [x] **Script with a visible line count, in a floating pill at the top of the screen.** `tools/verify/record.ps1` shows it during an area recording. Line breaks in the script become spaces.
- [x] **Follows narration with on-device speech recognition when a microphone is on. Static otherwise. Never breaks the recording.** `tools/verify/record.ps1` shows the static pill with "Scroll to move through the script", and the recording completes. Not driven: following live speech, which needs someone speaking into the microphone.

## Recording Studio

- [x] **Session package. The project reopens from the Library.** `tools/verify/studio.ps1` closes and reopens Studio, and `project.json` keeps the edits.
- [x] **Timeline with thumbnails, scrub, split at the playhead, trim, delete, per-clip speed, undo and redo.** `tools/verify/studio.ps1`, `studio2.ps1` and `studio3.ps1` scrub with Shift+Right, split, delete, undo, redo, set 1.25x and 2x, and trim by dragging a clip's end.
- [x] **Background, padding, radius, shadow. Aspect original, 16:9, 9:16, 1:1 or 4:5. Fill (follows the pointer) or Fit.** `tools/verify/studio2.ps1` sets a background and 16:9, and the export is 1920x1080. Not driven: the sliders and Fill or Fit.
- [ ] **Crop the screen card with presets.** Not run. `tools/verify/studio4.ps1` opened Crop video and applied nothing.
- [x] **Studio presets: save named, set default.** `tools/verify/studio3.ps1` saves a named preset to `StudioPresets/verify-preset.json`, the folder the Rust build used. Not driven: set default.
- [x] **Auto zoom from clicks. Add, remove, enable, resize and retime zooms. Follow the pointer or a fixed focus.** Zooms from clicks appear in the project, and Z adds one at the playhead (`tools/verify/studio.ps1`). Not driven: remove, enable, resize, retime and fixed focus.
- [x] **Reconstructed cursor with an adjustable size, click highlights, keystroke captions in six positions.** `tools/verify/studio4.ps1` saves cursor size 2.6x, highlights off and captions at Top left.
- [x] **Camera track: show and hide, drag, size, roundness.** `tools/verify/studio4.ps1` adds a synthetic `camera.mp4`, and the bubble and its pane show. Not driven: changing the bubble.
- [x] **On-device transcription. Captions editor with seek, position, size and karaoke highlight.** `tools/verify/transcribe.ps1` downloads the model and the runtime, then transcribes in 9 s, and captions show over the video. The word times are wrong. See the next line.
- [x] **Transcript editing: click to seek, shift-click to select, cut. Remove filler words. Trim silences.** `go run ./internal/transcribe/cmd/wordtimecheck` synthesizes narrations with SAPI and checks every word edge within 150 ms of the spoken word, plus phrase and filler cuts; on the default model the worst edge is 110 ms. In the packaged app, selecting "this narration into" and cutting leaves clips 0-1168 and 2400-7033 ms. Silence trims read audio energy and remove no word.
- [x] **Export with quality, resolution, format, 30 or 60 fps, motion blur and audio. Progress, cancel, cached renders.** `tools/verify/studio2.ps1` exports in 4.7 s. The same export again takes 1.2 s and reuses the render file. Esc cancels and removes the partial file. `go run ./internal/studio/cmd/exportcheck` checks the renders. Not driven: GIF and HEVC.
- [x] **Trim & Compress editor for regular video files.** `media.ps1` and `tools/verify/misc.ps1` export 1 s to 3 s of a 4 s MP4 as 2112 ms with sound. `glint.exe --trim clip.mov` opens it (`tools/verify/cli.ps1`).

## macOS

The Go build compiles for macOS and `go tool mygo build -platform darwin/universal` makes
`Glint.app`, but the app has not run on a Mac. Most platform code there is a stub that returns
`errors.ErrUnsupported`, and `docs/mac-port.md` lists them package by package. Check a line
below only after it ran in the macOS app or in a test that calls the real macOS code.

- [ ] Capture full screen, window and area, previous area, and Capture Text through Vision
- [ ] Global hotkeys and the menu bar menu
- [ ] Glint windows excluded from captures
- [ ] Preview stack in the work area, a card dragged into Finder, dismiss after drag
- [ ] Library multi-select, Copy as files, Move to Trash
- [ ] Annotation editor with the system font and appearance
- [ ] Single instance with command-line forwarding, launch at login
- [ ] MP4 encode, decode, transcode and trim
- [ ] Screen recording with system audio, microphone, camera and input
- [ ] Studio playback with audio, export, captions and transcription
- [ ] Trim & Compress, Quick Look, pins, self-timer, teleprompter
- [ ] Permission prompts from a signed `Glint.app` (Screen Recording, Input Monitoring, Microphone, Camera)
- [ ] Mixed-scale multi-display setups
