# Glint architecture

Glint is a Windows screenshot, annotation and screen-recording app written in Rust with GPUI.
It is a cargo workspace. The UI lives in the root crate, and everything that can run without a
window lives in a library crate so it can be tested headlessly.

| Crate | Owns | Depends on |
| --- | --- | --- |
| `glint` (root, `src/`) | GPUI windows, tray, hotkeys, capture flow, Library UI, editors, Studio UI | all below |
| `crates/glint-render` | tiny-skia compositor: styles, backgrounds, annotations, screenshot render | none of ours |
| `crates/glint-media` | Windows media engine: screen/camera/audio/input recording, MP4 encode, MP4/WAV decode | none of ours |
| `crates/glint-studio` | Recording project model, timeline math, auto zoom, frame compositor, export | `glint-render`, `glint-media` |
| `crates/glint-transcribe` | On-device speech-to-text (whisper.cpp), transcript model, filler and silence detection | none of ours |

Library crates never depend on `gpui`. They expose plain Rust types, blocking functions meant for
background threads, and `Arc<AtomicBool>` cancel flags plus progress callbacks for long jobs.

## Library on disk

The Library root is `%APPDATA%\Glint\Library`. Every capture is one directory, so moving,
renaming or trashing a capture is one filesystem operation.

```
Library/
  <id>/                       screenshot
    meta.json                 CaptureMeta
    base.png                  untouched original pixels, never rewritten
    image.png                 current deliverable (base + edits flattened); equals base when unedited
    edit.json                 EditDoc sidecar; absent when never edited
    thumb.png                 <= 512px thumbnail of image.png
  <id>.glintrec/              recording session package
    meta.json                 CaptureMeta
    screen.mp4                screen master, H.264, cursor NOT burned in
    camera.mp4                optional camera master, H.264
    mic.wav                   optional, 48 kHz float32 mono
    system.wav                optional, 48 kHz float32 stereo
    input.json                InputTimeline (pointer, clicks, keys, cursor sprites)
    cursors/<n>.png           cursor sprites referenced by input.json
    project.json              Studio project; absent means defaults
    transcript.json           optional Transcript
    thumb.png
    render/                   export cache, safe to delete
```

`<id>` is `YYYY-MM-DD_HH-MM-SS_xxxx` (local time plus 4 random hex chars), so directory order is
capture order. Titles live in `meta.json`, never in the directory name.

```rust
struct CaptureMeta {
    version: u32,            // 1
    kind: CaptureKind,       // "screenshot" | "recording"
    title: String,           // user-editable, defaults to "Screenshot 2026-10-14 at 14.03.22"
    created: DateTime<Local>,
    modified: DateTime<Local>,
    width: u32, height: u32, // pixels of base / screen master
    duration_ms: Option<u64>,
    source: Option<String>,  // window title or "Display 1"
    imported_from: Option<PathBuf>,
}
```

## Recording session contract (`glint-media` writes, `glint-studio` reads)

All times are milliseconds of **media time**: 0 is the first recorded frame and paused spans
are removed. All positions are pixels in the screen master's coordinate space.

`input.json`:

```json
{
  "version": 1,
  "size": [2560, 1440],
  "pointer": [[0, 100.5, 200.0, 0], [16, 104.0, 203.5, 0]],
  "clicks": [{ "t": 1200, "up": 1310, "x": 640.0, "y": 360.0, "button": "left" }],
  "keys": [{ "t": 3000, "label": "Ctrl+S" }],
  "cursors": [{ "id": 0, "file": "cursors/0.png", "hotspot": [1, 1], "scale": 1.5 }]
}
```

`pointer` rows are `[t, x, y, cursor_id]`, sampled whenever the pointer moves (at most every
8 ms) and when the cursor shape changes. Positions may fall outside the frame. `keys` holds
only shortcut-like presses (modifier combos, Enter, Esc, Tab, arrows, function keys) plus plain
typing grouped by the recorder, never raw text from password fields.

## Studio project (`project.json`)

Owned by `glint-studio`. Versioned with a `version` field. Clip times are in source media time.
Output time is derived by walking clips in order and dividing each clip's duration by its speed.

## Shared rendering

`glint-render::style::Background` and the gradient presets are the single source of truth for
backgrounds in both the screenshot editor and Studio. Studio composes recordings with the same
shadow, corner and background code as screenshots so presets look identical in both.

## Threads

GPUI owns the main thread. Capture, OCR, recording, decode, export and transcription run on
background threads and report back through `cx.spawn` tasks or channels polled by GPUI.
Glint's own windows call `SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)` unless the user
opts in to including them.
