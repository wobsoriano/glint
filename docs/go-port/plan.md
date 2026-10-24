# Rewriting Glint in Go with MyGo

Glint moves from Rust and GPUI to Go and [MyGo](https://mygo.egoist.dev) (v0.6.3), whose native
UI draws on the GPU (Direct3D 11 on Windows, Metal on macOS) and needs no cgo. The Rust tree
(`src/`, `crates/`, `Cargo.*`, `build.rs`) stays as the reference until the Go app reaches parity.
Then it is deleted in one commit.

## Done

The rewrite is done when all of the following hold:

1. `go build ./...` passes for `windows/amd64` and `darwin/arm64`, and `go vet ./...` and `go test ./...` pass on Windows.
2. `go tool mygo build` produces `Glint.exe` and its installer.
3. Each flow in `docs/parity.md` that was checked for Windows has been driven in the Go build on a real
   desktop, with screenshots kept as evidence. Transcription may be marked open if it cannot be
   done without cgo. macOS must compile, and each macOS gap must be listed.
4. Existing Library folders, `settings.json` and `presets.json` written by the Rust build open unchanged.
5. The Rust tree is deleted, and `README.md`, `docs/` and the release workflow describe the Go build.

## Packages

| Package | Owns | Ported from |
| --- | --- | --- |
| `main.go` | flags, single instance, `App.Run` | `src/main.rs`, `src/ipc.rs` |
| `internal/platform` | screen pixels, windows list, capture exclusion, wallpaper, launch at login | `src/sys/*`, `src/capture.rs` |
| `internal/ocr` | text recognition with the OS engine, reading order | `src/ocr.rs`, `src/sys/*/ocr.rs` |
| `internal/settings` | `settings.json`, `presets.json`, data dir, `GLINT_DATA_DIR` | `src/settings.rs` |
| `internal/store` | the Library on disk: `CaptureMeta`, ids, import, thumbnails, rename, trash | `src/store.rs`, `src/library.rs` |
| `internal/render` | CPU compositor: styles, backgrounds, effects, annotations, `EditDoc` | `crates/glint-render` |
| `internal/redact` | finding emails, phones, cards, keys and tokens in OCR text | `src/redact.rs` |
| `internal/media` | screen, audio and input recording, MP4 encode and decode, WAV, transcode | `crates/glint-media` |
| `internal/studio` | Studio project, timeline, auto zoom, motion, frame compositor, export | `crates/glint-studio` |
| `internal/transcribe` | speech to text, captions, fillers, silences | `crates/glint-transcribe` |
| `internal/app` | app state, tray, hotkeys, capture flow, toasts, routing between windows | `src/main.rs`, `src/flow.rs`, `src/actions.rs`, `src/toast.rs` |
| `internal/ui/...` | one package per surface: `overlay`, `stack`, `editor`, `pin`, `library`, `prefs`, `record`, `studio`, `trim`, `quicklook`, plus `theme` for shared look and icons | the matching `src/` modules |

Library packages never import `github.com/egoist/mygo`. Their tests run without a window.

## Rules every unit follows

1. The on-disk formats are a contract. Go reads and writes the same JSON field names, enum
   spellings and defaults as the Rust serde types. Test them against JSON produced by the Rust code
   or found in a real Library.
2. No cgo. Windows APIs are called through `golang.org/x/sys/windows` and `syscall.SyscallN` on COM
   vtables. macOS code uses `github.com/ebitengine/purego`, as MyGo does.
3. OS-specific code lives in `_windows.go` and `_darwin.go` files, with a `_other.go` stub returning
   `platform.ErrUnsupported`, so every package builds for both platforms.
4. MyGo's docs for the pinned version are in `$(go env GOMODCACHE)/github.com/egoist/mygo@v0.6.3/docs`
   and its examples in `.../examples`. Read them before using an API. Do not guess.
5. Comments explain a non-obvious why only. No narration.
6. A unit is done when its own tests pass and its behavior has been checked on the real thing. That
   means pixels compared, files decoded, or the app driven. Compiling is not enough.
