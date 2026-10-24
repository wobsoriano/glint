# Verification of the Go build

An independent pass over the packaged Go build of Glint on 2026-10-23 and 2026-10-24. It drove
every checked Windows item of `docs/parity.md` and the claims of `README.md` on a real desktop,
opened a copy of the user's Rust-written data, tried adversarial cases, measured idle cost and
leaks, and listed what macOS lacks. Product code was not changed.

## Summary

Of the 57 checked Windows items in `docs/parity.md`, 42 are VERIFIED, 12 PARTIAL, 2 FAILED and
1 NOT RUN. The 2 unchecked camera items are NOT VERIFIABLE HERE (no camera). PARTIAL means the
listed parts were driven and the rest were not run.

Plan predicates 1, 2 and 4 hold. Predicate 3 holds except for the two FAILED items. Predicate 5
does not hold yet: the Rust tree and the Rust-era `README.md` are still in place.

The most important findings:

1. The packaged `Glint.exe` deletes the Rust build's "Open with" registrations every time it
   starts, installed or not. A side-by-side run with `GLINT_DATA_DIR` does it too.
2. Transcript word timings were wrong on a short narration, so a transcript cut removed the
   wrong part of the video.
3. The About pane of the packaged 0.2.0 build says "Version dev".
4. A capture with a title longer than about 250 characters can never be saved.

## Environment

| What | Value |
| --- | --- |
| OS | Windows 11 Enterprise 10.0.26200, one 1920x1080 display at 100% |
| Hardware | AMD Ryzen 5 5600X (12 logical cores), 16 GB, AMD Radeon RX 6700 XT |
| Audio and camera | Arctis 7P+ microphone and output; no camera |
| Toolchain | Go 1.27.2, MyGo v0.6.3 |
| Source | branch `go/verify` from `go-rewrite` at `f1d58d2` |
| Build | `go tool mygo build`: `build/windows-amd64/Glint.exe`, SHA-256 `6d8ef893cd6cb8458eda3a2948db960b36c5703e0e3aee0a2d0b0be6d30413fc`, and `Glint Setup 0.2.0 amd64.exe`. A second build gave the same hash. |
| Run as | the packaged `Glint.exe`, copied as `glint.exe` with `glintcheck.exe` and `record.exe` into a scratch `bin`, every run on its own `GLINT_DATA_DIR` |

Evidence is in the scratch folder `V = C:\Users\wobsoriano\AppData\Local\Temp\claude\C--Users-wobsoriano-Documents-dev-glint\a06a1b06-fc5b-4a03-b548-8d40b9b5fe31\scratchpad\verify`.
It is not committed, because the screenshots show the desktop and account details. Paths below
are relative to `V`: `runs/scen/<name>` for the existing scenarios, `runs/x/<name>` for the new
checks, `runs/realdata-*` for the real-data sessions, and `logs/` for every console log.

## How to rerun

Every check is a script. Run the desktop ones inside the lock, as the standing orders say:

```powershell
$env:GLINT_BIN = '<dir with glint.exe (the packaged Glint.exe), glintcheck.exe, record.exe>'
$env:GLINT_OUT = '<evidence dir>'
go run ./tools/desklock -- powershell -NoProfile -File tools/verify/<script>.ps1
```

| Script | Checks |
| --- | --- |
| `tools/verify/static.sh` | gofmt, vet and build for both platforms, both `mygo build` targets, `go test ./...` |
| `tools/verify/macgaps.sh` | the macOS stub files and every darwin-built line that returns an unsupported error |
| `tools/verify/realdata.ps1` | a Rust-written data copy: Library, every screenshot in the editor, every recording in Studio playing, quit, and the list of created, changed or deleted files |
| `tools/verify/realsettings.ps1` | the Rust-written `settings.json` in every Settings pane, then a capture that runs its after-capture steps |
| `tools/verify/rustrender.ps1` | Copy on a Rust recording that already has a Rust render; After recording > Save |
| `tools/verify/rustopenwith.ps1` | backs up, restores and shows the Rust build's "Open with" registrations |
| `tools/verify/library.ps1`, `library2.ps1` | Library filters, views, sort, search, inspector, Space, remembered layout, multi-select, Copy, Show in Explorer, Export, Recycle Bin |
| `tools/verify/cards.ps1` | three cards, Copy Text, Compress, Dismiss, peek tab, Clear All, auto close, rearranged layout, Pin after capture |
| `tools/verify/editor.ps1`, `editor3.ps1`, `presets.ps1`, `savedialog.ps1` | every tool by key, undo and redo, selection, zoom keys, crop, the look controls, presets, Save through a dialog |
| `tools/verify/editor2.ps1` | screenshots of the editor sidebar and crop bar, to find controls |
| `tools/verify/record.ps1`, `record2.ps1` | countdown cancel, pause, restart, delete, missing devices, area with teleprompter, window, microphone list |
| `tools/verify/misc.ps1`, `secrets.ps1`, `redactprobe` | automatic redaction, Trim & Compress, a microphone recording; `redactprobe` prints what OCR reads in a PNG and what redaction finds |
| `tools/verify/studio.ps1` to `studio4.ps1`, `transcribe.ps1` | Studio panes, split, delete, undo, zoom, look, export, cache, cancel, speed, trim, presets, camera, cursor, keystrokes, transcription |
| `tools/verify/settings.ps1`, `holdkey.ps1` | Settings panes and refused shortcuts |
| `tools/verify/cli.ps1` | the README command line |
| `tools/verify/adversarial.ps1` to `adversarial3.ps1` | odd captures, bursts, bad command lines, deleted capture, long and unicode titles, denied folders, kill mid-recording, quit and close mid-export |
| `tools/verify/perf.ps1` | idle CPU and memory, and open-close cycles of the Library, the editor and Studio |

## Plan predicate

| # | Predicate | Status | Evidence |
| --- | --- | --- | --- |
| 1 | `go build ./...` for windows/amd64 and darwin/arm64; `go vet ./...` and `go test ./...` on Windows | PASS | `logs/static.log`: gofmt lists nothing, vet exits 0 for both platforms, 29 test packages ok |
| 2 | `go tool mygo build` produces `Glint.exe` and its installer | PASS | `logs/static.log`, `logs/packagecheck.log` (all PASS), `logs/install.log` (all PASS; the registry is as before after uninstall) |
| 3 | Each checked Windows flow driven in the Go build, with screenshots | PASS except 2 FAILED items | tables below |
| 4 | Library folders, `settings.json` and `presets.json` from the Rust build open unchanged | PASS, with notes | [Real-data compatibility](#real-data-compatibility) |
| 5 | Rust tree deleted; `README.md`, `docs/` and the release workflow describe the Go build | FAIL | `src/`, `crates/`, `Cargo.*` remain. `README.md` (intro, Build, Crate layout, Tests), `docs/architecture.md` and `docs/mac-port.md` still describe Rust. `.github/workflows/release.yml` and `docs/building.md` describe Go. |

## Parity, Windows

### Capture

| Item | Verdict | Evidence or repro |
| --- | --- | --- |
| Capture display, window, area | VERIFIED | `runs/scen/area`: the capture equals the screen read before the shortcut, 0 of 150500 pixels differ. `runs/scen/captures`: window by Space and click, Ctrl+Shift+3, Ctrl+Shift+5. |
| Global hotkeys; rebinding with failure message and fallback | VERIFIED | `runs/scen/prefs` (Ctrl+Alt+K captures, Ctrl+Shift+4 stops). `runs/x/x-settings/rebinds.png`: Ctrl+Shift+3 is refused as used by full screen, F12 as not registrable; Ctrl+Shift+4 still captures. |
| Self-timer 3/5/10 s | VERIFIED | captures 2907, 4922 and 9963 ms after the menu item (`logs/scen-tray*.log`) |
| PNG or JPEG with quality; export folder; Save bypasses dialog option | VERIFIED | `runs/scen/save-jpeg`, `save-png` (PNG export equals `image.png`); `runs/x/x-savedialog`: with the option off, Save asks and writes to the chosen folder |
| Window shadow | VERIFIED | 1142x695 with transparent corners, against 1086x633 opaque without (`logs/scen-captures.log`) |
| Glint windows excluded by default, opt-in | VERIFIED | `logs/scen-automation-2.log`: the Library is absent from a full-screen capture by default and present with the option on |
| Capture Text | VERIFIED | clipboard holds the three stage lines in order; Library count unchanged |
| Imported before automation | VERIFIED | every after-capture step reads the Library item; with the Library folder denied, nothing runs and a toast says why (`runs/x/x-adv2-ro-item/02-library-denied.png`) |

### After capture and preview stack

| Item | Verdict | Evidence or repro |
| --- | --- | --- |
| Screenshot automation | VERIFIED | preview, copy, save (scenarios); editor (`runs/scen/editor`); pin (`runs/x/x-cards-pin`) |
| Recording automation | VERIFIED | preview and copy (`runs/scen/record`: the clipboard holds Studio's render), Studio (`runs/scen/studio`), save (`logs/x-rustrender.log`: 2866 ms MP4 in the export folder) |
| Floating stack | VERIFIED | three cards, left and right, auto close at 3 s, dismiss after drag (`runs/x/x-cards*`, `runs/scen/cards`) |
| Card actions | VERIFIED | copy, save, Quick Look, pin, delete, drag into Explorer (`runs/scen/cards`); Copy Text, Compress (JPEG on the clipboard as a file), Dismiss (`runs/x/x-cards`). Save As was not clicked. |
| Rearrangeable card actions | VERIFIED | a layout of Pin, Delete, Dismiss, More with Edit and Copy in the center is honored, with the rest in More (`runs/x/x-cards-layout`). The drag editor in Settings > Overlay renders but was not dragged. |
| Peek tab | VERIFIED | `runs/x/x-cards/05-peek-screen.png`; clicking the tab brings the cards back |
| Save failure dialog | VERIFIED | Retry or another folder, for a missing folder, a write-denied folder and an over-long file name |

### Library

| Item | Verdict | Evidence or repro |
| --- | --- | --- |
| Sidebar filter | VERIFIED | `runs/x/x-library/02-*.png`, `03-*.png` |
| Grid and list, search, sort, remembered | VERIFIED | `settings.json` keeps view, sort and inspector across a reopen (`logs/x-library.log`) |
| Inspector | VERIFIED | dimensions, dates, duration, disk usage, editing state, source, location (`04-inspector-recording.png`, `05-inspector-edited.png`) |
| Quick Look on double-click or Space; Edit reopens | VERIFIED | Space opens and closes it; Edit reopens the editor and Studio (scenarios) |
| Multi-select; copy, export, reveal, Recycle Bin | FAILED | Ctrl-click, Shift-click, Ctrl+A, Copy (two files), Export (two PNGs) and the Recycle Bin pass. Show in Explorer with two captures selected selects only the first (`runs/x/x-library/20-show-in-explorer.png`, "1 item selected"). `docs/parity.md` records both selected for the Rust build. |
| Rename with Enter | VERIFIED | `meta.json` title updated (`runs/scen/library`) |
| Opens from tray, Ctrl+Shift+L, plain launch; autostart stays in tray | VERIFIED | tray and scenarios; a plain launch opens the Library (`runs/realdata-vis/01-library.png`); `--background` shows no window |
| Open with Glint | VERIFIED | the installer registers `com.glint.app.<ext>` and Explorer lists one Glint (`logs/install.log`); `glint.exe <image>` imports a copy and opens the editor. The Explorer menu itself was not clicked. |

### Annotation editor

| Item | Verdict | Evidence or repro |
| --- | --- | --- |
| Non-destructive, `edit.json`, reopen restores | VERIFIED | `glintcheck edited`: `image.png` equals RenderDoc of `base.png` and `edit.json`; a reopened capture shows the saved look and closes without a prompt (`runs/x/x-editor3/look-reopen.png`) |
| Tools | VERIFIED | one of each tool by its key; `edit.json` holds rect, filled rect, ellipse, line, arrow, pen, two counters, text, highlight, pixelate and blur (`logs/x-editor.log`) |
| Select, move, resize, multi-select, delete, undo, redo | VERIFIED | pixel diffs: undo removes exactly the 90x81 blur box (7290 px), redo restores it, delete of a marquee pair removes 14580 px and undo restores them. Resizing by a handle was not driven. |
| Smart redaction with editable strength | VERIFIED | automatic redaction hid an email, a phone number and an IP address (`runs/x/x-redact`); the strength slider shows on a redaction. Windows OCR returns no line for "4111 1111 1111 1111", so that card was not hidden; 4242 and 5555 cards are found (`redactprobe`). |
| Crop presets, remaps annotations, undoable | VERIFIED | a 1:1 crop of a 500x301 capture is `{x:100,w:301,h:301}`; the arrow moves by -100; undo restores both |
| Backgrounds | PARTIAL | solid and gradient driven; custom image and wallpaper not run |
| Presets | VERIFIED | Save look writes `presets.json`; Export writes a `glint-presets` v1 file; Import reads it back (`logs/x-presets.log`); a Rust `presets.json` lists Plain, Dusk and Photo |
| Padding, radius, shadow, aspect, alignment | PARTIAL | padding 64, radius 12 and shadow 40 restored from a Rust `edit.json`; aspect and the nine-point grid not driven |
| Border | VERIFIED | `effects.border.enabled` saved and rendered (`logs/x-editor3.log`) |
| Text watermark | VERIFIED | enabled with the default text; rendered (white on a light background, so not visible in the screenshot) |
| Perspective, rotation, zoom, pan | PARTIAL | the Hero preset saves rot_x 14, rot_y -20, rot_z 4; zoom and pan not driven |
| Progressive blur | PARTIAL | radial with bleed saved; directional and clipped not driven |
| Canvas zoom | PARTIAL | Ctrl+0, Ctrl+1 and Ctrl+scroll (`runs/x/x-editor/contact.png`); scroll to pan not driven |
| Save, Done, copy, pin; failed save keeps editor open | VERIFIED | scenarios; with the capture folder denied writes, Done keeps the editor open and `image.png` unchanged, then succeeds once writes are allowed (`logs/x-adversarial3.log`) |

### Screen recording

| Item | Verdict | Evidence or repro |
| --- | --- | --- |
| Picker | VERIFIED | display, window (click a window, then Enter), area (800x500), microphone list, system audio, timer, teleprompter (`runs/x/x-record-*`). Camera shows "No camera". |
| Countdown cancellable | VERIFIED | Esc and Cancel during a 3 s countdown record nothing |
| Missing devices warn and continue | VERIFIED | a remembered missing camera and microphone warn in the picker and after, and a 2668 ms recording lands |
| Live camera preview (unchecked) | NOT VERIFIABLE HERE | no camera |
| Area guide | PARTIAL | dashed outline and dimmed surroundings shown (`runs/x/x-record-area/02-area-recording.png`); click-through and exclusion from the recording not proven |
| Control bar | VERIFIED | pause excludes 2 s (3705 ms recorded of about 5.5 s), restart gives 1516 ms, stop, delete asks then leaves nothing |
| Separate masters (screen, mic, system audio, input) | VERIFIED | `screen.mp4` without an audio track, `system.wav`, `mic.wav` (550858 bytes), `input.json`; the first frame has no pointer |
| Camera master (unchecked) | NOT VERIFIABLE HERE | no camera |

### Teleprompter

| Item | Verdict | Evidence or repro |
| --- | --- | --- |
| Script, line count, pill at the top | VERIFIED | `runs/x/x-record-area/02-area-recording.png`. Line breaks in the script become spaces, as in the Rust build (`split_whitespace`). |
| Follows narration; static otherwise; never breaks recording | PARTIAL | static with "Scroll to move through the script", and the recording completes; following speech needs live speech into the microphone, not possible here |

### Recording Studio

| Item | Verdict | Evidence or repro |
| --- | --- | --- |
| Session package; reopens from Library | VERIFIED | project edits survive close and reopen (`logs/x-studio.log`) |
| Timeline | VERIFIED | thumbnails, Shift+Right to scrub, split, delete, undo, redo, speed 1.25x, trim by dragging the clip end (`logs/x-studio.log`, `logs/x-studio3.log`) |
| Background, padding, radius, shadow; aspect; Fill or Fit | PARTIAL | background and 16:9 (export 1920x1080) driven; sliders and Fill or Fit not driven |
| Crop with presets | NOT RUN | Crop video opened, nothing applied |
| Studio presets | PARTIAL | a named preset saved to `StudioPresets/verify-preset.json`, the Rust build's folder; set default not driven |
| Auto zoom | PARTIAL | auto zooms from clicks appear in the project; Z adds one at the playhead; remove, enable, resize, retime and fixed focus not driven |
| Cursor, click highlights, keystroke positions | VERIFIED | size 2.6x, highlights off and Top left saved (`logs/x-studio4.log`) |
| Camera track | PARTIAL | a synthetic `camera.mp4` shows the bubble and its pane (`runs/x/x-studio4/sheet.png`); changes not driven |
| Transcription; captions editor | PARTIAL | model and runtime download, then a transcript in 9 s ("Glenn turns this narration into captions on the device."); captions show over the video; word times wrong (bug 2) |
| Transcript editing, fillers, silences | FAILED | cutting "this narration into" changed the clips from 0-7033 to 0-6880, removing the last 153 ms instead of the words; trimming silences used the same wrong times (`logs/x-studio3.log`) |
| Export, progress, cancel, cached renders | VERIFIED | dialog with format, codec, resolution, quality, 30 or 60 fps, motion blur and audio; export 4.7 s, the same export again 1.2 s with the same render file; Esc cancels and removes the partial file. GIF and HEVC not driven. |
| Trim & Compress | VERIFIED | 1 s to 3 s of a 4 s MP4 exports 2112 ms with sound; `--trim clip.mov` opens it |

## README claims

| Claim | Verdict | Evidence |
| --- | --- | --- |
| Features list | as the parity tables | |
| Global shortcuts Ctrl+Shift+4, 3, 5, 6, 2, 7, L; double-click the tray icon | VERIFIED | scenarios |
| Editor keys (Ctrl+C, Ctrl+S, Ctrl+Shift+S, Ctrl+Enter, Ctrl+P, Ctrl+Z, Ctrl+Y, V R F O L A P N T H X B C, Ctrl+1, Ctrl+0) | VERIFIED | `tools/verify/editor.ps1`, `savedialog.ps1`, scenarios |
| Command line: no arguments, `--background`, `--library`, `--record`, `--capture area\|window\|screen\|text\|previous`, `--studio`, `--trim`, a file path | VERIFIED | `logs/x-cli.log`, scenarios. `--capture previous` with no previous area opens the area overlay. Bad options exit 2 with the usage on stderr. |
| One Glint at a time; a second sends its arguments | VERIFIED | every forwarded command exits 0 and acts in the running Glint |
| `GLINT_DATA_DIR` | VERIFIED | every run |
| "written in Rust with GPUI", Build, Crate layout, Tests | FAILED | the README still describes the Rust build (predicate 5) |

## Real-data compatibility

The source was a read-only copy of `%APPDATA%\Glint`: `settings.json` (2165 bytes) and a
Library of 10 captures, 6 screenshots (one with a style-only `edit.json`) and 4 recordings (3
with Rust renders in `render/`). It has no `presets.json` and no `models\`, so the Rust test
fixture `internal/settings/testdata/rust/presets.json` stood in for presets.
`%APPDATA%\Glint` and `%USERPROFILE%\Pictures\Glint` kept their 2026-10-17 timestamps
throughout.

| Check | Result |
| --- | --- |
| Settings load | All panes show the file's values: preview and copy after capture, PNG, quality 85, export folder, timer 5 s, window shadow, recording preview, countdown 3 s, system audio, stack left with no auto close, default shortcuts, automatic redaction with blur. Ctrl+Shift+3 runs preview and copy. (`runs/x/x-realsettings/panes.png`) |
| Library | Lists all 10 with thumbnails, 6 screenshots and 4 recordings, durations 0:02 to 0:08 (`runs/realdata-vis/01-library.png`) |
| Editor | Every screenshot opens. The edited one restores its 2-stop gradient and 64 px padding at 1.5x on an 878x1015 canvas, as the Rust `image.png` (`runs/realdata-vis/02-editor-2026-10-17_06-59-28_033f.png`). Each closes without a save prompt. The user's `edit.json` holds no annotations, so annotation restore was proved on Go-made edits only. |
| Studio | Every recording opens and plays: two screenshots 0.7 s apart differ (`logs/realdata-vis.log`) |
| Presets | The Rust `presets.json` lists Plain, Dusk and Photo in the editor |
| Session with no changes | `tools/verify/realdata.ps1` on an untouched copy (`logs/realdata-1.log`): no file changed or deleted. It created `project.json` and `render/thumbs-48x96.png` in each of the 4 recordings when Studio opened them. The Rust build also writes `project.json` on a recording's first Studio open (`src/studio/mod.rs`), and the thumbnails are a cache. `settings.json`, every `meta.json`, `edit.json` and `thumb.png` kept their bytes and timestamps. Opening Settings without a change does not rewrite `settings.json`. |
| Rust renders | Not reused. Copy on a Rust recording took 1.9 s and replaced `render/export-acd9709fc81d8e56.mp4` with `export-338091b6dd3858e0.mp4` (`logs/x-rustrender.log`), because the Go cache key differs from Rust's. |

## Adversarial cases

| Case | Result |
| --- | --- |
| 1x1 drag | Treated as a click: captures the window under the pointer (1142x695). The minimum side is 2 px, as in Rust. |
| Click without a drag | Captures the window under the pointer |
| Area over the taskbar to the screen corner | 418x178, identical to the screen |
| Ten Ctrl+Shift+3 in a burst | Ten captures; Glint alive |
| Ctrl+Shift+4 twice | One overlay |
| Ctrl+Shift+3 while the overlay is open | A full-screen capture is taken and the overlay stays open |
| Esc in the overlay, picker, countdown, Quick Look, pin, save dialog, delete confirmation, shortcut recorder | Each cancels or closes |
| `--bogus`, `--capture`, `--capture bogus`, `--trim` without a file | Exit 2 with the usage on stderr; the running Glint is unaffected. The packaged exe has no console, so a user at a prompt sees nothing. |
| Missing image, missing session, `--trim` on a non-video, a JSON file as an image | Exit 0 and a toast. The toast leads with the full path and is cut off, so the reason never shows (bug 6). |
| Capture folder deleted while its card shows | Copy, Save and Edit each report an error and nothing crashes. The card shows "Copy failed" under a green check mark, as the Rust build does. |
| Unicode title | Saved as `Gl日本語 🎉 café a_b_c_______.png` |
| 300-character title | Every save fails with "The filename, directory name, or volume label syntax is incorrect", and Retry cannot succeed (bug 4) |
| Title `CON` | Saved as `CON.png` (allowed on this Windows build) |
| Export folder denying writes | The save failure dialog |
| Capture folder denying writes | Done keeps the editor open; `image.png` unchanged |
| Library folder denying writes | Toast "Could not add the capture to the Library"; Glint alive |
| Glint killed mid-recording, started again | The hidden staging folder with a 1.1 MB `screen.mp4` stays in the Library and is never recovered or removed. The Rust build behaves the same. |
| Studio closed during an export | Closes at once; no partial file and no export left |
| Quit from the tray during an export | Glint exited within 15 s, and the export it had started was complete (3900 ms, 234 frames) with no partial file left |
| Second-instance flags while windows are open | A second `--studio` for an open session focuses its window instead of opening another. `--capture window`, `text` and `previous` act while the stage window is open (`logs/x-cli.log`). |
| Disk full | Not simulated; write-denied folders stood in |

## Idle cost and leaks

Glint ran in the tray on a 10-capture Library (`tools/verify/perf.ps1`). CPU is
`TotalProcessorTime`, whose tick is 15.6 ms, so 0.000 s over 60 s means under 0.026% of one
core.

| State | CPU over the window | Working set | Private bytes | Handles |
| --- | --- | --- | --- | --- |
| Started in the tray | 0.000 s in each of two 60 s windows | 21 MB | 49 MB | 237 |
| After 10 Library cycles | | 103 MB | 136 MB | 1310 |
| After 10 editor cycles | | 139 MB | 171 MB (peaks of 270 MB fall back) | 1312 |
| After 10 Studio cycles | | 205 MB | 306 MB (457 MB after the first) | 1513 |
| Idle after all windows closed | 0.000 s over 60 s, and again over 30 s | 173 MB | 187 MB | 1515 |
| Same, about 10 minutes later | 0.000 s over 30 s | 113 MB | 128 MB | 1516 |

Memory does not grow per cycle once a window type has opened once, and Go returns memory to
Windows over minutes. A separate run of 25 Library cycles held private bytes at 135 MB but
raised handles from 1295 to 1341 and USER objects from 28 to 34: about one handle per open and
close after warm-up. The editor and Studio cycles kept handles flat. The first editor open
costs about 3.6 s of CPU; later opens cost 0.1 to 0.3 s.

## macOS

`GOOS=darwin GOARCH=arm64 go build ./...` and `go vet ./...` pass, and
`go tool mygo build -platform darwin/universal` makes `build/darwin-universal/Glint.app`
(29 MB; the disk image needs a Mac). The app was not run. `tools/verify/macgaps.sh` lists the
stubs compiled on darwin. Each one returns `ErrUnsupported` or does nothing:

| Package | Stubbed on macOS | Effect |
| --- | --- | --- |
| `internal/platform` | `Displays`, `CaptureRect`, `CaptureWindow`, `Windows`, `CursorPos` (0,0), `ExcludeFromCapture`, `Wallpaper`, `SetLaunchAtLogin` | no screenshot of any kind, no window picking, no capture exclusion, no wallpaper background, no launch at login |
| `internal/ocr` | `Recognize` | no Capture Text, Copy Text or automatic redaction |
| `internal/media` | video encoder, video and audio decoders, `AvailableCodecs`, `StartRecorder`, `Cameras`, camera preview | no recording, Studio playback or export, Trim & Compress, or video Quick Look |
| `internal/media/capture` | `Microphones`, `DefaultOutput`, `StartTrack`, `StartMicMonitor` | no audio capture or level meter |
| `internal/media/input` | `Start`, `Stop` | no input track, so no cursor redraw, click zooms or keystroke captions |
| `internal/media/playback` | audio output | no sound in Studio or Quick Look |
| `internal/transcribe` | `loadWhisper`; no published runtime for darwin | no transcription or live teleprompter following |
| `internal/app` | foreground window, foreground permission, own-window exclusion, Open with cleanup | no-ops |
| `internal/ui/overlay`, `pin`, `stack`, `toast`, `record` | native placement and window rects | generic fallbacks; `record` cannot find a window's rect |

`internal/store` moves items to the Trash on macOS, and `internal/render` reads macOS fonts.
The checked lines in the macOS section of `docs/parity.md` describe the Rust build. For the Go
build every one of them is a gap.

## Bugs, by priority

| # | Severity | Bug | Repro | Suspected package |
| --- | --- | --- | --- | --- |
| 1 | Medium | The packaged exe deletes the Rust build's "Open with" registrations on every start, installed or not. The comment on `forgetRustOpenWith` says "once this Glint runs installed", but `mygo.App.IsPackaged()` is true for any `mygo build` output. | With `HKCU\Software\Classes\Glint.Image`, `Glint.Video` and `Applications\glint.exe` present, run `Glint.exe --background` from any folder with `GLINT_DATA_DIR` set. The keys and the `OpenWithProgids` values are gone. Seen twice here; `tools/verify/rustopenwith.ps1` restored them. | `internal/app` (`app.go` `ready`, `openwith_windows.go`) |
| 2 | Medium | Word timings from transcription can be badly wrong, so transcript cuts and silence trims edit the wrong part of the video. | `tools/verify/transcribe.ps1`: speech from about 1.2 s to 4.7 s of a 7 s clip gives "Glenn" 1170-4110 ms, "turns" 4110-5740 ms and the other seven words all at 7000-7000 ms. Cutting "this narration into" removes 6880-7033 ms. The whisper parameters match Rust's (token timestamps, no DTW), so it is probably inherited; compare the Rust engine on `runs/x/x-transcribe/data/Library/*/system.wav`. | `internal/transcribe` (`engine.go` word timing; DTW timestamps) |
| 3 | Medium-low | About shows "Version dev" in the packaged 0.2.0 build | Settings > About. `internal/ui/prefs/prefs.go` has `var Version = "dev"` and nothing sets it; `mygo.App.Version()` returns 0.2.0. | `internal/ui/prefs` |
| 4 | Low-medium | A title longer than about 250 characters makes every save of that capture fail, and Retry cannot succeed | Set a 300-character title (rename in the Library), open the capture, Ctrl+S. `actions.Sanitize` has no length limit, as in Rust `src/shell.rs`. | `internal/actions` (`Sanitize`) |
| 5 | Low | Show in Explorer with several captures selected selects only the first | Select two captures, click Show in Explorer: Explorer says "1 item selected". Deliberate in `App.Reveal`; the Rust build selected all. | `internal/app` (`Reveal`) |
| 6 | Low | Error toasts lead with a full path and are cut off before the reason | `glint.exe <missing.png>`; Save or Copy on a deleted capture. The card's failure status shows a success check mark (inherited from Rust). | `internal/app`, `internal/actions`, `internal/ui/stack` |
| 7 | Low | Rust renders in `render/` are not reused; the first Copy or Save of a Rust recording re-renders and deletes the Rust file | `tools/verify/rustrender.ps1` | `internal/studio` (`cacheKey`) |
| 8 | Low | About one handle leaks per Library open and close | 25 cycles: handles 1295 to 1341, USER objects 28 to 34, memory flat | `internal/ui/library` or MyGo window teardown |
| 9 | Low | The control bar's delete confirmation cuts its label to "Delete this recordi…" | `runs/x/x-record-bar/03-delete.png` | `internal/ui/record` |
| 10 | Low | Missing-device warnings: the picker toast covers the picker chips; the after-recording toast repeats the device and is cut off | `runs/x/x-record-missing` | `internal/ui/record`, `internal/media` |
| 11 | Low | A recording killed mid-way leaves a hidden staging folder that is never recovered or removed (as in Rust) | Kill Glint 2 s into a recording, start it again | `internal/store` |
| 12 | Info | Windows OCR returns no line for "4111 1111 1111 1111", so redaction cannot hide it | `go run ./tools/verify/redactprobe <png>` on `runs/x/x-redact/cards.png` | `internal/ocr` (engine behavior) |
| 13 | Info | Trim & Compress shows the same size estimate for High, Medium and Small on small sources: the 1.5 Mbps floor makes Small no smaller | `runs/scen/media/01-trim.png` (all three about 621 KB) | `internal/studio` (`Bitrate`), `internal/ui/trim` |
| 14 | Info | The first editor open costs about 3.6 s of CPU | `logs/perf-editor.log` | `internal/ui/editor` |

A shortcut that another app holds never reaches the shortcut recorder, so the recorder shows no
error for it; Windows delivers the key to the holder. That is not a Glint bug.

## Harness changes

Three scenario bugs were fixed and are committed with this report:

- `internal/app/cmd/glintcheck/scenarios/common.ps1`: `Stop-Glint` now waits for the process
  to exit. A Glint started right after a kill handed its command line to the dying instance and
  exited; a following `glint.exe <image>` then became the first instance and
  `Start-Process -Wait` hung.
- `common.ps1`: `Show-Overflow` finds the taskbar's Show Hidden Icons button through UI
  Automation (`Chevron`). The fixed point 1722,1056 landed on the Windows Update icon once the
  microphone-in-use indicator shifted the tray, and opened Windows Settings.
- `internal/app/cmd/glintcheck/scenarios/automation.ps1`: the own-windows check compared a
  toast area, but no toast shows with every after-capture step off, so both cases passed without
  testing anything. It now compares the Library window's area.

The scenarios still assume the tray icon at 1642,955 in the overflow. The `glintcheck mark`
check makes that fail safely.

## Not verified here

- Camera hardware: live preview and the camera master track.
- The teleprompter following live speech.
- Multiple displays and scales other than 100%.
- A full disk.
- The macOS app at runtime.
- GIF and HEVC export, Studio crop presets, the editor's image and wallpaper backgrounds, and
  the other controls marked PARTIAL or NOT RUN above.

## Side effects on this machine

- The Rust build's "Open with" registrations were deleted by the packaged exe (bug 1) and
  restored from `V\regbackup` with `tools/verify/rustopenwith.ps1 -Mode restore`.
- Captures deleted during checks are in the Recycle Bin. Compress left JPEGs in
  `%TEMP%\Glint\Compressed`.
- A harness click opened Windows Settings > Windows Update; it was closed. A harness click
  switched Chrome's tab; it was switched back.
- `HKCU\Control Panel\NotifyIconSettings` remembers the tray icon of the scratch `glint.exe`.
