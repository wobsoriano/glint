# Glint architecture

Glint is a screenshot, annotation and screen-recording app for Windows, written in Go on
[MyGo](https://mygo.egoist.dev) v0.6.3. MyGo supplies the windows, native UI drawn on the GPU,
the tray, global shortcuts, the clipboard and dialogs. Glint reaches Windows directly for
everything else: screen and window capture, OCR, Media Foundation, WASAPI and input hooks. All
of it is pure Go with no cgo. Windows APIs are called through `golang.org/x/sys/windows` and
COM or WinRT vtables, and whisper.cpp is loaded from its release DLL.

| Package | Owns | Imports MyGo |
| --- | --- | --- |
| `main.go`, `internal/app` | start-up, single instance, tray, global shortcuts, capture flow, wiring between windows | yes |
| `internal/ui/...` | one package per window, plus `theme` for the shared look and icons | yes |
| `internal/actions` | copy, save, export, compress, delete | yes |
| `internal/platform` | displays, screen and window capture, capture exclusion, wallpaper, launch at login | no |
| `internal/ocr` | the Windows OCR engine and reading order | no |
| `internal/settings` | `settings.json`, `presets.json`, the data directory | no |
| `internal/store` | the Library on disk | no |
| `internal/redact` | sensitive text found in OCR lines | no |
| `internal/render` | CPU compositor: styles, backgrounds, effects, annotations, `EditDoc` | no |
| `internal/media` | recording (screen, camera, audio, input), MP4 encode and decode, WAV, transcode, playback | no |
| `internal/studio` | Studio project, timeline, auto zoom, motion, frame compositor, export | no |
| `internal/transcribe` | whisper.cpp, transcripts, captions, fillers, silences | no |

Packages that don't import MyGo expose plain Go types and blocking functions meant for
goroutines, with a `context.Context` for cancelling long jobs and progress callbacks. Their tests
run without a window.

Each window package names what it needs from the app as a small `Host` interface, and
`internal/app` implements them all. `internal/app.Surfaces` is the table of functions that opens
each window.

## Platforms

OS code lives in `_windows.go` files, with a `_other.go` (or `_darwin.go`) twin, so every package
builds for both Windows and macOS. On macOS the twins mostly return `errors.ErrUnsupported`.
`docs/mac-port.md` lists what works there. Window IDs and display IDs are `int64`: `HWND` and
`HMONITOR` on Windows, `CGWindowID` and `CGDirectDisplayID` on macOS. Rectangles in
`internal/platform` are physical pixels of the virtual desktop. MyGo's are in DIPs.

The Library, settings, presets, Studio projects and transcripts use the same files and JSON as
the Rust build that came before, byte for byte. A Library made by either build opens in the
other.

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

`meta.json` (`store.Meta`):

```json
{
  "version": 1,
  "kind": "screenshot",
  "title": "Screenshot 2026-10-14 at 14.03.22",
  "created": "2026-10-14T14:03:22.418+02:00",
  "modified": "2026-10-14T14:05:10.002+02:00",
  "width": 2560,
  "height": 1440,
  "duration_ms": null,
  "source": "Display 1",
  "imported_from": null
}
```

`kind` is `"screenshot"` or `"recording"`. `width` and `height` are the pixels of `base.png` or of
the screen master. `source` is a window title or a display name.

## Recording session contract (`internal/media` writes, `internal/studio` reads)

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
only shortcut-like presses (modifier combos, Enter, Esc, Tab, arrows, function keys), never
plain typing, and never Glint's own global shortcuts.

Audio samples and video frames are placed on the media clock by the device timestamps of the
packets and frames, not by when they arrive. That keeps sound and picture within a frame.

## Studio project (`project.json`)

Owned by `internal/studio`. Versioned with a `version` field. Clip times are in source media time.
Output time is derived by walking clips in order and dividing each clip's duration by its speed.

## Shared rendering

`render.Background` and the gradient presets in `internal/render` are the single source of truth for
backgrounds in both the screenshot editor and Studio. Studio composes recordings with the same
shadow, corner and background code as screenshots so presets look identical in both.

## Threads

MyGo owns the main thread, and every view and window call runs there. Capture, OCR, recording,
decode, export and transcription run on goroutines and hand results back with `Window.Update`
or `mygo.RunOnMain`. COM and WinRT calls lock their goroutine to an OS thread with an initialized
apartment.
Glint's own windows call `SetWindowDisplayAffinity(WDA_EXCLUDEFROMCAPTURE)` unless the user
opts in to including them.
