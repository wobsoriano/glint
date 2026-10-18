# Mac port handoff

This note is for the session that starts the macOS port of Glint. It covers the state of the
Windows build, where the platform code sits, and the open decisions.

## State of the Windows build

- Glint runs on Windows 10 and 11. It is written in Rust with GPUI 0.2.2. Every feature in
  `docs/parity.md` was exercised in the running app except two camera lines. The test machine
  had no camera, and a Mac has one, so check those two lines on the Mac.
- `cargo test --workspace` passes 169 tests on Windows, with 10 ignored because they need
  hardware or models.
- Read `docs/architecture.md` for the crate layout, the Library format on disk and the
  recording session contract. Read `docs/design.md` for the design rules.
- The evidence from the Windows test runs (screenshots, logs) is local to the Windows machine
  and is not in the repo.

## Progress on macOS

Steps 1 and 2 of the suggested first steps are done.

- The whole workspace builds on macOS (Apple silicon, Xcode clang, Homebrew cmake), and
  `cargo run -- --library` opens the Library window.
- The Windows code sits behind per-platform backends: `crates/glint-media/src/win/` and
  `src/sys/win/`. The macOS backends, `crates/glint-media/src/mac/mod.rs` and
  `src/sys/mac/mod.rs`, are stubs. Actions return "... is not supported on macOS yet", and window
  styling calls do nothing. `docs/architecture.md` describes the layout.
- `tools/check-windows.sh --workspace --all-targets` type-checks the Windows build from the Mac.
  It passes with zero warnings. Run it after every change to shared code.
- `cargo test --workspace` on macOS: everything passes except the tests that need the media
  backend: `glint-media` `roundtrip` (8) and `screen_failure` (4), and `glint-studio` `engine`
  (6). Treat them as the acceptance tests for the AVFoundation backend.
- Step 3 (screenshots) is mostly done and checked in the running app on a 2x display:
  full-screen, click-a-window and area capture, the overlay with loupe, the preview stack, the
  Library, the editor (drawing, ⌘Z), and the clipboard.
  - Screen space: `glint_media::space` (`crates/glint-media/src/mac/space.rs`) is the one place
    that converts between macOS points and Glint pixels. Read its module doc before touching
    coordinates.
  - Floating windows (overlay, stack, toasts, pins, recording chrome) drop AppKit's `Titled`
    style in `sys/mac/desktop.rs::float`, because AppKit keeps titled windows below the menu
    bar.
  - Fonts: `glint-render/src/fonts.rs` has a macOS catalog (SF Pro is the default, and variable
    fonts use the `wght` axis). The UI font is `.SystemUIFont`.
  - Theme: light or dark and the accent come from the system. Without Mica, window surfaces are
    opaque (`Appearance::backdrop`).
  - Shortcuts: in-app shortcuts use `modifiers.secondary()` and show as ⌘ through `ui::keys`.
    Global hotkeys keep the Windows defaults (⌃⇧2 to ⌃⇧7, ⌃⇧L), which avoid the system's
    ⌘⇧3/4/5.
  - Custom title bars leave the traffic lights to the system (`ui::SYSTEM_CAPTION_INSET`).
  - Still open in step 3: copying and dragging files (`shell::copy_files`, `drag_out`), Trash
    (`library::recycle`), OCR (Vision), the save-failed dialog, the menu bar item in place of
    the tray, the accessory activation policy, single instance and IPC, launch at login, and
    the wallpaper background.
- The macOS build warns about dead code in shared `glint-media` helpers (`WavWriter`,
  `copy_planes`, `take_buffer`, the BT.601/709 matrices, `LevelMeter::update`) and in root
  items only the Windows backend uses so far. The macOS backend will use them. Don't silence the
  warnings.

## What already works on macOS

These parts have no Windows code and should compile on macOS as they are:

| Part | Notes |
| --- | --- |
| `crates/glint-render` | tiny-skia compositor: styles, backgrounds, annotations, screenshot render. Except `fonts.rs`, see below. |
| `crates/glint-studio` | Project model, timeline, auto zoom, compositor, export logic. It calls `glint-media` for decode and encode. |
| `crates/glint-transcribe` | whisper.cpp through `whisper-rs`. Turn on the Metal feature on Apple silicon. |
| GPUI windows and views | GPUI is native on macOS (it is Zed's UI framework). |
| `xcap`, `global-hotkey`, `tray-icon`, `arboard` | These crates support macOS. |
| `src/redact.rs`, `src/library.rs` (most of it), `src/prefs.rs`, `src/editor/`, `src/studio/` | Plain Rust plus GPUI, apart from the calls listed below. |

## Windows-only code to replace

The counts are `windows::` references in each file at commit 1d33f3c. The files now live in
`crates/glint-media/src/win/` and, for the root crate, in `src/sys/win/` (only the
Windows-specific functions moved; the shared parts stayed in `src/`). `win.rs` is now
`desktop.rs` for its shared half and `sys/win/desktop.rs` for the rest.

### `crates/glint-media` (the media engine)

| File | Windows API | macOS replacement |
| --- | --- | --- |
| `screen.rs`, `d3d.rs` | Windows.Graphics.Capture on Direct3D 11 | ScreenCaptureKit (`SCStream`), IOSurface or CVPixelBuffer frames |
| `encoder.rs`, `mf.rs`, `writer.rs` | Media Foundation sink writer: H.264/HEVC and AAC into MP4 | AVFoundation `AVAssetWriter` (VideoToolbox does the hardware encode) |
| `reader.rs`, `frames.rs` | Media Foundation source reader for MP4 decode and seek | `AVAssetReader` |
| `audio.rs` | WASAPI capture: mic, plus loopback for system audio | Mic through AVFoundation or CoreAudio. System audio through ScreenCaptureKit audio (macOS 13 and later). |
| `audio_out.rs` | WASAPI playback for the Studio preview | CoreAudio or `AVAudioEngine` |
| `devices.rs` | Display, mic and camera enumeration | `NSScreen` or `CGDisplay`, `AVCaptureDevice.DiscoverySession` |
| `camera.rs` | Media Foundation camera reader | `AVCaptureSession` |
| `hooks.rs`, `keys.rs` | Low-level mouse and keyboard hooks for `input.json` | `CGEventTap`, which needs Accessibility permission |
| `cursor.rs` | GDI cursor bitmaps for the reconstructed cursor | `NSCursor.currentSystem` image and hotspot |
| `recorder.rs` | Ties these together | Same shape behind the new backends |

The output contract does not change. A session package holds `screen.mp4` (no cursor),
optionally `camera.mp4`, `mic.wav`, `system.wav`, and `input.json` in media milliseconds. Studio
reads exactly that. Keep the public API of `glint-media` and put a per-platform backend behind
it, so `glint-studio` and the UI stay the same.

### Root crate (`src/`)

| File | Windows API | macOS replacement |
| --- | --- | --- |
| `shell.rs` (32 refs) | COM `IDataObject` and `IDragSourceHelper` for dragging a card into other apps, `CF_HDROP` clipboard, DWM and window styles | `NSDraggingSource` with an `NSPasteboardItem` file URL; `NSPasteboard` |
| `win.rs` | Single-instance mutex, topmost windows, Run-key launch at login, wallpaper path, clipboard images | `NSRunningApplication` or a lock file, `NSWindow.level`, `SMAppService`, `NSWorkspace` desktop image |
| `ipc.rs` | `WM_COPYDATA`, which forwards CLI and Open-with requests to the running instance | Apple events or `application:openURLs:`, or a Unix socket |
| `capture.rs`, `record/target.rs`, `record/chrome.rs` | Window enumeration, DWM frame bounds, `WDA_EXCLUDEFROMCAPTURE`, monitor lookup | `CGWindowListCopyWindowInfo` or `SCShareableContent`; exclude Glint's windows through the `SCContentFilter` |
| `ocr.rs` | Windows.Media.Ocr | Vision `VNRecognizeTextRequest` |
| `library.rs` | Recycle Bin, reveal in Explorer | `NSWorkspace.recycle`, `activateFileViewerSelecting` |
| `ui.rs` | Accent color and light or dark theme from the registry | `NSColor.controlAccentColor`, `NSApp.effectiveAppearance` |
| `hotkeys.rs` | Default shortcuts are Windows chords | Default to Cmd-based chords. `global-hotkey` works on macOS. |

For Objective-C interop, use `objc2` and its framework crates (`objc2-app-kit`,
`objc2-screen-capture-kit`, `objc2-av-foundation`, `objc2-vision`). They match the
`windows`-crate style.

## Things that will bite

- **Permissions.** Screen Recording (TCC), Accessibility (event taps), Microphone and Camera
  each need a prompt and an `Info.plist` usage string. macOS keys the grants to the code
  signature, so a `cargo run` binary loses them on every rebuild. Build into a `.app` bundle
  signed with a stable identity early.
- **Coordinates.** macOS uses points with a bottom-left origin, and Retina scale is per
  display. `input.json` and the capture code use top-left screen-master pixels, so convert in
  exactly one place.
- **The menu bar replaces the tray.** `tray-icon` maps to `NSStatusItem`. Set the activation
  policy to accessory so Glint has no Dock icon while it runs in the background.
- **Shortcut conflicts.** Cmd+Shift+3, 4 and 5 belong to the system screenshot tool. Pick other
  defaults.
- **The build is simpler.** whisper-rs builds with Xcode's clang and Homebrew's cmake. No
  `LIBCLANG_PATH` file is needed.

## Decisions

1. **The look on macOS.** Glint follows macOS conventions on macOS, as it follows Windows 11
   conventions on Windows: the system font, accent color and appearance, the menu bar in place
   of the tray, and Cmd-based shortcuts. The layout and Glint's own visual identity stay the
   same on both. Platform conventions are shared by every Mac app, so this does not copy
   Screendrop or CleanShot X.
2. **Repo layout.** One codebase with `cfg(target_os)` backends.
3. **Minimum macOS version.** 13, the first with ScreenCaptureKit system audio. Gate anything
   that needs 14 at runtime.

## Suggested first steps

1. Clone, then run `cargo build -p glint-render -p glint-studio -p glint-transcribe` and fix
   whatever does not compile.
2. Put `cfg(windows)` around the Windows modules in `glint-media` and the root crate, and add
   stubs that return a clear "not supported on macOS yet" error, so the app launches.
3. Implement the macOS screenshot path first (capture, preview card, editor, clipboard). It
   exercises the most shared code for the least platform code.
4. Then recording (ScreenCaptureKit, AVAssetWriter, event tap), then Studio playback.
5. Keep `docs/parity.md` honest. Check a macOS line only after doing it in the running app.

Promo material (`promo/`) is git-ignored and stays on the Windows machine.
