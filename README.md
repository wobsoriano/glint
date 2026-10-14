# Glint

A CleanShot-style screenshot tool for Windows, built in Rust with [GPUI](https://gpui.rs) (Zed's GPU-accelerated UI framework).

## Features

**Capture** (global shortcuts, all configurable)

| Shortcut | Action |
| --- | --- |
| `Ctrl+Shift+4` | Capture area: drag a region, or click a window to snap to it. Space captures the whole screen, Esc cancels. Includes a pixel magnifier with color readout. |
| `Ctrl+Shift+3` | Capture the screen under the cursor |
| `Ctrl+Shift+5` | Capture the active window (with its title, for window chrome) |
| `Ctrl+Shift+6` | Capture the previous area again |
| `Ctrl+Shift+2` | Capture text: select an area and its text (OCR) is copied |

The tray icon menu also has a 3-second self-timer and "Open last capture". Double-clicking the tray icon starts an area capture.

**After capture**: a floating quick-access thumbnail (Copy / Save / Edit / Pin), or straight into the editor. Optionally auto-copy and auto-save.

**Editor**
- Backgrounds: 18 gradient presets, an **Auto** gradient sampled from your screenshot, solid colors with a custom hue, a blurred copy of the screenshot, any image file, or your **desktop wallpaper**. Transparent backgrounds are supported too.
- Padding, inset (with auto-balanced edge color), corner radius, shadow size and opacity.
- Window frames: macOS (light/dark) and Windows 11 (light/dark), optionally with the window title.
- Aspect ratios (1:1, 4:3, 16:9, 9:16...) with 9-way alignment.
- Annotations: arrow, rectangle, ellipse, line, pen, text (plain / outline / label), highlighter, numbered step counters, redaction (blur / pixelate / solid) and spotlight. Plus crop.
- Select, move and resize; Shift constrains angles and squares; arrow keys nudge; full undo/redo.
- Copy, save, save as (PNG/JPEG/WebP), and **pin** as an always-on-top floating window (drag to move, scroll to zoom, Ctrl+scroll for opacity).

**Privacy: automatic hiding of sensitive info**

Uses the on-device Windows OCR engine (`Windows.Media.Ocr`), so nothing leaves your PC. It detects:
- emails and phone numbers
- credit card numbers (Luhn-validated)
- API keys and tokens: Stripe, OpenAI/Anthropic `sk-`, GitHub, Slack, AWS, Google, GitLab, npm, Shopify, JWTs, `password:`/`token:` values, and generic high-entropy strings
- IP and MAC addresses, SSNs and IBANs
- URLs carrying tokens or credentials

Run it in the editor (`Ctrl+Shift+R`), or turn on "Automatically hide sensitive info on capture" in settings. Redactions are ordinary annotations, so you can move, delete or undo them. The Blur style pixelates before blurring, so the result can't simply be de-blurred.

## Editor shortcuts

`V` select · `A` arrow · `R` rect · `O` ellipse · `L` line · `P` pen · `T` text · `H` highlight · `N` counter · `B` redact · `S` spotlight · `C` crop
`Ctrl+C` copy · `Ctrl+S` save · `Ctrl+Shift+S` save as · `Ctrl+Z` / `Ctrl+Y` undo/redo · `Ctrl+P` pin · `Ctrl+Shift+R` hide sensitive info · `Ctrl+Shift+T` copy text · `Del` delete

## Build & run

Requires Rust (MSVC toolchain) and the Windows 10/11 SDK.

```sh
cargo run --release               # opens the main window
glint.exe --background       # start silently in the tray
```

Settings are stored in `%APPDATA%\Glint\settings.json`. Edit the `hotkeys` section to rebind shortcuts, then click "Reload shortcuts" in the main window.

## Tests

```sh
cargo test                                         # detector unit tests
cargo test --release -- --ignored --nocapture      # real capture + OCR + render pipeline
```

## Layout

| File | Purpose |
| --- | --- |
| `main.rs` | App bootstrap, global hotkeys, tray |
| `flow.rs` | Capture entry points and post-capture actions |
| `overlay.rs` | Frozen full-screen area/window picker with magnifier |
| `editor.rs` | Editor UI, tools, undo, export |
| `render.rs` | tiny-skia compositor shared by preview and export (WYSIWYG) |
| `ocr.rs` / `redact.rs` | Windows OCR and sensitive-data detection |
| `quick.rs` / `pin.rs` / `home.rs` | Quick-access thumbnail and toasts, pinned windows, settings |
