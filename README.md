# Glint

Glint is a screenshot, annotation and screen-recording app for Windows 10 and 11. It is written in Rust with [GPUI](https://crates.io/crates/gpui), the GPU-rendered UI framework from the Zed editor. Everything runs on your PC: text recognition uses the Windows OCR engine, and transcription runs whisper.cpp locally. Glint has no accounts and no cloud upload.

Glint runs in the notification area. Every screenshot and recording goes into the Library, a folder of captures that you can search, reopen and edit again later.

## Features

### Capture

- Capture the full screen, a window, or an area. The area overlay shows a magnifier and the selection size. Space switches between area and window mode, and Esc cancels.
- Capture the previous area again.
- Self-timer of 3, 5 or 10 seconds, started from the tray menu.
- Capture Text copies the text in an area to the clipboard in reading order and saves nothing.
- Save as PNG, or as JPEG with a quality setting. **Save** can write straight to the export folder or ask where to save.
- Window captures can get a drop shadow.
- Glint's own windows stay out of screenshots unless you turn on **Include Glint windows in screenshots**.
- Optional automatic redaction finds emails, phone numbers, card numbers, API keys, IP addresses and tokens in URLs, and hides them as editable annotations.

### After a capture

- Choose what happens after each screenshot: show a preview card, copy, save, open the editor, pin. Any combination works.
- Choose what happens after each recording: show a preview card, copy the video, save, open Studio.
- Preview cards stack on the left or right edge of the screen. They close after a delay you set, or stay until you dismiss them. A collapsed stack becomes a small tab.
- Card actions are copy, save, compress to JPEG, pin, Quick Look, edit, copy text, delete and dismiss. Drag a card into another app to drop the file there. You can move the actions between the four corners, the center and the overflow menu in **Settings > Overlay**.
- When a save fails, a dialog offers **Retry** or another folder.

### Library

- Filter by all captures, screenshots or recordings. Switch between grid and list, search by title, and sort by date captured, name or date modified.
- The inspector shows size, dates, duration, disk usage and location.
- Quick Look opens on double-click or Space.
- Select several items with Ctrl-click, Shift-click or Ctrl+A, then copy, export, show them in Explorer, or move them to the Recycle Bin.
- Press Enter to rename a capture.
- Glint registers **Open with Glint** for images and videos. An image opens in the editor as an imported copy. A video opens in Trim & Compress.

### Annotation editor

- Edits are non-destructive. The original pixels stay in the capture, the edit is stored beside them, and reopening the capture restores every annotation.
- Tools are select, rectangle, filled rectangle, ellipse, line, arrow, freehand, numbered markers, text, highlight, pixelate and blur.
- Crop presets are freeform, original, 1:1, 16:9, 9:16, 4:3 and 3:2. Cropping moves the annotations with the image and can be undone.
- Backgrounds can be a solid color, a gradient, an image or the desktop wallpaper. You can also set padding, corner radius, shadow, border, aspect ratio, alignment, a text watermark, perspective and rotation, and a progressive blur.
- Save styles as presets, and import or export them as `.glintpreset` files.
- **Copy**, **Save** and **Done** each save the edit into the capture. **Save** also exports a copy, and **Done** closes the editor. If the edit cannot be saved, the editor stays open with your changes.
- Pin any image as an always-on-top window.

### Screen recording

- The recording picker chooses a display, a window or an area, plus a camera, a microphone and system audio. It also sets a start timer of 1, 3 or 5 seconds and an optional teleprompter script.
- A missing camera or microphone produces a warning, and the recording continues without it.
- A control bar shows the elapsed time and has pause, restart, stop and delete.
- Glint records the screen without the cursor, plus separate camera, microphone, system audio and input tracks. Studio redraws the cursor from the input track.
- The teleprompter scrolls with your voice when a microphone is on.

### Recording Studio

- Cut the recording on a timeline: split, trim, delete, and set the speed per clip, with undo and redo.
- Style the frame with a background, padding, corner radius and shadow. Pick an aspect ratio, and crop the screen.
- Auto zoom follows clicks. You can add, remove, resize and retime zooms.
- The cursor can be resized and can highlight clicks. Keystroke captions can sit in six positions.
- Show, move, resize and round the camera bubble.
- Transcribe on the device, edit captions, and cut the video by deleting words from the transcript. Filler words and silences can be removed in one step.
- Export as MP4 at 30 or 60 fps with a quality and resolution setting, optional motion blur, a progress bar and cancel.
- Trim & Compress trims and shrinks any MP4, MOV or M4V file.

## Default shortcuts

These are global shortcuts. Change them in **Settings > Shortcuts**.

| Shortcut | Action |
| --- | --- |
| `Ctrl+Shift+4` | Capture area |
| `Ctrl+Shift+3` | Capture full screen |
| `Ctrl+Shift+5` | Capture window |
| `Ctrl+Shift+6` | Capture previous area |
| `Ctrl+Shift+2` | Capture text |
| `Ctrl+Shift+7` | Record screen, or stop the recording in progress |
| `Ctrl+Shift+L` | Open Library |

Double-click the tray icon to capture an area.

In the editor, `Ctrl+C` copies, `Ctrl+S` saves, `Ctrl+Shift+S` saves through a dialog, `Ctrl+Enter` is **Done**, and `Ctrl+P` pins. `Ctrl+Z` undoes and `Ctrl+Y` redoes. Single letters pick a tool: `V` select, `R` rectangle, `F` filled rectangle, `O` ellipse, `L` line, `A` arrow, `P` freehand, `N` marker, `T` text, `H` highlight, `X` pixelate, `B` blur, `C` crop. `Ctrl+1` fits the image and `Ctrl+0` shows it at actual size.

## Command line

```
glint.exe [--background] [--library] [--record] [--capture area|window|screen|text|previous]
          [--studio <session dir>] [--trim <video>] [image, video or Library capture...]
```

| Argument | Effect |
| --- | --- |
| no arguments | Opens the Library. |
| `--background` | Starts in the tray without opening a window. **Launch at login** uses this flag. |
| `--library` | Opens the Library. |
| `--record` | Opens the recording picker, or stops the recording in progress. |
| `--capture <kind>` | Starts a capture. `<kind>` is `area`, `window`, `screen`, `text` or `previous`. |
| `--studio <dir>` | Opens a recording session folder in Studio. |
| `--trim <video>` | Opens a video in Trim & Compress. |
| a file path | Opens an image in the editor, or a video in Trim & Compress. |

Only one Glint runs at a time. A second `glint.exe` sends its arguments to the running instance and exits.

## Build

You need:

- Rust with the MSVC toolchain (`x86_64-pc-windows-msvc`).
- Visual Studio 2022 Build Tools with the C++ workload.
- The Windows 10 or 11 SDK.
- CMake and `libclang.dll`. The `glint-transcribe` crate builds whisper.cpp with CMake and generates its bindings with libclang.

`docs/building.md` lists the exact versions and tells you how to point the build at CMake and libclang. Then build and run:

```
cargo build --release
target\release\glint.exe
```

Glint does not need ffmpeg.

## Data locations

| Path | Contents |
| --- | --- |
| `%APPDATA%\Glint\settings.json` | Settings |
| `%APPDATA%\Glint\Library\` | One folder per capture. See `docs/architecture.md` for the format. |
| `%APPDATA%\Glint\presets.json` | Editor presets |
| `%APPDATA%\Glint\models\` | Downloaded whisper models |
| `%USERPROFILE%\Pictures\Glint\` | Default export folder |

Set `GLINT_DATA_DIR` to use another folder in place of `%APPDATA%\Glint`.

## Crate layout

| Crate | Contents |
| --- | --- |
| `glint` (`src/`) | GPUI windows, tray, global shortcuts, capture flow, preview cards, Library, editor, recording UI, Studio UI, Trim & Compress |
| `crates/glint-render` | CPU compositor for styles, backgrounds and annotations, shared by the editor and Studio |
| `crates/glint-media` | Screen, camera, audio and input recording, MP4 encoding and decoding |
| `crates/glint-studio` | Studio project model, timeline, auto zoom, frame compositor and export |
| `crates/glint-transcribe` | whisper.cpp transcription, captions, filler words and silence detection |

The library crates do not depend on GPUI, so their tests run without a window. `docs/architecture.md` describes the on-disk formats and the contracts between crates. `docs/parity.md` is the feature checklist with evidence for each item.

## Tests

```
cargo test --workspace
```

Some tests need hardware or a desktop session, or download models. They are marked `#[ignore]`. To run them:

```
cargo test --workspace --release -- --ignored
```

## Inspired by

The feature set is inspired by [Screendrop](https://github.com/fayazara/Screendrop) by Fayaz Ahmed, which is released under CC0. The interaction patterns are inspired by [CleanShot X](https://cleanshot.com). Glint shares no code or assets with either app.
