# Glint on macOS

The Go build compiles for macOS and packages `Glint.app`, but nobody has run that app yet. Most
of what makes Glint useful is missing there. It cannot take a screenshot, record, decode or
encode video, run OCR or transcribe. The UI, the Library, the editor's renderer and the Studio
model are shared Go and build as they are. This page lists the gaps package by package, says
what already works, and gives the plan for closing the gaps without cgo.

## Build it

Any machine builds the macOS app. No Mac, Xcode or cgo is needed:

```sh
GOOS=darwin GOARCH=arm64 go build ./...
GOOS=darwin GOARCH=arm64 go vet ./...
go tool mygo build -platform darwin/universal
```

The last command writes `build/darwin-universal/Glint.app` (29 MB, Apple silicon and Intel in
one bundle). The disk image needs a Mac, so `mygo build` skips it elsewhere. Signing, the icon
and the `Info.plist` keys are in `docs/building.md`.

To list the gaps from the code, run this from the repository root:

```sh
bash tools/verify/macgaps.sh
```

It prints every `_other.go` stub file that builds for darwin, with its functions, and every line
in a darwin-built file that returns an unsupported error.

## What is stubbed

OS code lives in `_windows.go` files. Each has a twin, usually `_other.go` with
`//go:build !windows`, that compiles on macOS and returns `errors.ErrUnsupported`
(`platform.ErrUnsupported` in `internal/platform`) or does nothing.

| Area | Package and file | Stubbed on macOS | What the user loses |
| --- | --- | --- | --- |
| Screen capture | `internal/platform`, `platform_other.go` | `Displays`, `CaptureRect`, `CaptureWindow`, `Windows`, `ExcludeFromCapture`, `Wallpaper`, `SetLaunchAtLogin`. `CursorPos` returns 0,0. | every screenshot, window picking, capture exclusion, the wallpaper background, launch at login |
| OCR | `internal/ocr`, `ocr_other.go` | `Recognize` | Capture Text, Copy Text and automatic redaction |
| Video encode | `internal/media`, `media_other.go` | `CreateVideoWriter` and every `VideoWriter` method. `AvailableCodecs` returns none. | Studio export and Trim & Compress |
| Video and audio decode | `internal/media`, `media_other.go` | `OpenFrameReader`, `OpenAudioReader`. `DecodableCodecs` returns none. | Studio playback and timeline thumbnails, video in Quick Look and Trim & Compress |
| Recording | `internal/media`, `recorder_other.go` | `StartRecorder` | every screen recording |
| Audio capture | `internal/media/capture`, `audio_other.go` | `Microphones`, `DefaultOutput`, `StartTrack`, `StartMicMonitor` | microphone and system audio tracks, the level meter, the teleprompter's speech tap |
| Audio output | `internal/media/playback`, `output_other.go` | `AudioOutput.start` | sound in Studio and Quick Look |
| Input | `internal/media/input`, `track_other.go` | `Start`, `Track.Stop` | `input.json`, so no reconstructed cursor, click zooms or keystroke captions |
| Transcription | `internal/transcribe`, `whisper_other.go`, `runtime_other.go` | `loadWhisper`. No whisper.cpp runtime is pinned for darwin, so `installRuntime` returns `errors.ErrUnsupported`. | transcripts, captions and the teleprompter following speech |
| Camera | `internal/media`, `recorder_other.go`, `camera_preview_other.go` | `Cameras`, `StartCameraPreview` | the camera in the picker, the live preview, `camera.mp4` |
| App | `internal/app`, `native_other.go` | `foregroundWindow` returns 0. `excludeOwnWindows` does nothing. | Capture Window (`Ctrl+Shift+5`) finds no front window and falls back to full screen. Glint's windows are not kept out of captures. |
| Window placement | `internal/ui/overlay`, `pin`, `stack`, `toast`, `record` | native placement and window rects | MyGo's generic placement. `record` cannot find a window's rect. |

`doubleClickTime` in `internal/app` is a fixed 500 ms, and `AllowForeground` and
`forgetRustOpenWith` have nothing to do on macOS.

## What works

These parts have macOS code or need none. They compile and vet for darwin. None of them has run
on a Mac.

- **The UI.** MyGo's macOS backend draws the windows with Metal and supplies the menu bar icon,
  menus, global shortcuts, the clipboard, dialogs, the single-instance lock and
  `Shell.ShowItemInFolder`. The menu bar icon is a template image that macOS tints
  (`internal/app/tray.go`).
- **Platform wording.** The Library, Studio, Quick Look and Trim & Compress say Trash and Finder.
  Settings shows shortcuts as macOS chords such as ⌃⇧4 (`internal/ui/prefs/accel`). The default
  global shortcuts stay `Ctrl+Shift+2` to `Ctrl+Shift+7` and `Ctrl+Shift+L`, which leave the
  system's ⌘⇧3, ⌘⇧4 and ⌘⇧5 alone.
- **Files.** `internal/settings` keeps `settings.json` and `presets.json` in
  `~/Library/Application Support/Glint` and saves to `~/Pictures/Glint`. `internal/store` reads
  and writes the Library and moves captures to the Trash through `NSFileManager`
  (`trash_darwin.go`), so Finder's Put Back works.
- **Rendering.** `internal/render` has a macOS font catalog with SF Pro as the default
  (`fonts_darwin.go`), so the editor's renders and `image.png` work.
- **Shared logic.** `internal/redact`, the Studio project, timeline, auto zoom and frame
  compositor in `internal/studio`, and the captions, fillers and silences in
  `internal/transcribe` are plain Go. They work once their inputs exist.

## Plan

Call the macOS frameworks from Go through `github.com/ebitengine/purego`, as MyGo does. No cgo.
`internal/store/trash_darwin.go` shows the pattern: `purego.Dlopen` the framework, then send
Objective-C messages with `purego/objc`.

For each area, add a `_darwin.go` file beside the `_windows.go` one and narrow the stub's build
line to `//go:build !windows && !darwin`, as `internal/store/trash_other.go` does. Keep each
package's API as it is, so `internal/studio`, `internal/app` and the UI do not change. A
recording must still write the session package that `docs/architecture.md` describes:
`screen.mp4` without the cursor, an optional `camera.mp4`, `mic.wav`, `system.wav` and
`input.json` in media milliseconds.

The Rust build's macOS port reached screenshots, OCR, recording and Studio playback with these
frameworks, so they are the first choice:

| Area | macOS API |
| --- | --- |
| Screen capture, window list, capture exclusion | ScreenCaptureKit (`SCScreenshotManager`, `SCShareableContent`, an `SCContentFilter` that leaves Glint's windows out), `CGWindowListCopyWindowInfo` |
| OCR | Vision `VNRecognizeTextRequest` |
| Video encode | AVFoundation `AVAssetWriter`, with VideoToolbox doing the hardware encode |
| Video and audio decode | `AVAssetReader` |
| Recording | one `SCStream` for the screen and system audio, at a constant frame rate |
| Audio capture | `AVCaptureSession` for the microphone. System audio comes from the `SCStream`. |
| Audio output | an AudioToolbox `AudioQueue`. MyGo's `audio` package already drives one through purego (`audio/output_darwin.go`), to reuse or copy. |
| Input | a `CGEventTap`, and `NSCursor` images for the cursor sprites |
| Camera | `AVCaptureSession` and `AVCaptureDevice.DiscoverySession` |
| Wallpaper | `NSWorkspace` `desktopImageURLForScreen:` |
| Launch at login | MyGo's `App.SetOpenAtLogin`, which registers a login item on macOS 13 and later. Glint would then read `App.WasOpenedAtLogin` in place of `--background`. |

Transcription needs a whisper.cpp library that purego can load. The release Glint pins ships one
for Windows only, so the macOS source of that library is an open decision.

The Rust port also found these traps:

- **Coordinates.** macOS uses points with a bottom-left origin, and each display has its own
  scale. `internal/platform` rectangles are physical pixels of the virtual desktop with a
  top-left origin. Convert in one place.
- **Capture exclusion.** `NSWindow.sharingType` no longer keeps a window out of captures on
  macOS 14 and later. Only the `SCContentFilter` does.
- **Focus.** macOS 14 lets an app take focus only from user input to that app, so a window
  opened by a forwarded command line stays behind the active app.
- **Minimum version.** `mygo.json` sets macOS 13, the first with ScreenCaptureKit system audio.
  Check for anything that needs 14 at runtime.

## What needs a Mac

Compiling and packaging work anywhere. These steps do not:

- Running `Glint.app`, and every check in `docs/parity.md`'s macOS section.
- The purego code itself. It loads system frameworks, so its tests and its proof run only on
  macOS.
- The permission prompts. Screen Recording, Input Monitoring, Microphone and Camera are granted
  to the code signature, so test from a bundle signed with a stable identity
  (`docs/building.md`).
- The disk image `Glint <version>.dmg`, which `mygo build` makes only on macOS.
- Displays with mixed scales.
