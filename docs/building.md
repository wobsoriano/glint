# Building Glint

Glint is a Go app built on [MyGo](https://mygo.egoist.dev) v0.6.3. Building it needs Go 1.27 and
nothing else: no cgo, no C compiler, no SDK. `go.mod` pins the MyGo CLI as a tool, so
`go tool mygo` runs the right version. Any machine compiles both platforms. Only the macOS disk
image and its signature need a Mac.

## Run it

```sh
GLINT_DATA_DIR=/tmp/glint-dev go run .
```

`GLINT_DATA_DIR` keeps a development run away from your real Library (`%APPDATA%\Glint` on
Windows). It also gives the run a single-instance lock of its own, so it starts beside an
installed Glint instead of handing its command line to it. `go tool mygo dev` runs the app the
same way and rebuilds it when the code changes.

## Test

```sh
go vet ./...
go test ./...
GOOS=darwin GOARCH=arm64 go build ./...
```

Run `go test` from a signed-in desktop session. Tests that need one are marked like this:

| Marking | Tests | Skipped by |
| --- | --- | --- |
| `testing.Short()` | screen capture, playback through the speakers, slow OCR runs | `go test -short ./...` |
| a missing device | microphone, camera, audio output, wallpaper | skipped on their own when the device is absent |
| `GLINT_TEST_TRASH=1` | moving folders to the real Recycle Bin | skipped unless set |
| `GLINT_MEDIA_BENCH=1` | encoder throughput | skipped unless set |

Some behavior is proved by commands rather than tests, because it drives the desktop, downloads
a model or runs for minutes. Each command's doc comment gives its flags:

| Command | Proves |
| --- | --- |
| `internal/app/cmd/glintcheck` and its `scenarios` scripts | the app's flows, driven end to end on the desktop |
| `internal/transcribe/cmd/transcribecheck` | on-device transcription |
| `internal/studio/cmd/exportcheck` | Studio export of a synthetic session. It checks each render with `ffprobe` (on `PATH`) and against `ComposeFrame`, and checks that its tones and flashes start where the edit puts them. |
| `tools/packagecheck` | the built executable and the installer (see below) |
| `tools/verify` | the checks behind `docs/parity.md`: each flow driven in the packaged app, a Rust-written Library and settings, adversarial cases, idle cost, and the macOS gap list (`macgaps.sh`). `docs/go-port/verification.md` gives the results. |

## Build the app

```sh
go tool mygo build                              # Windows, on Windows
go tool mygo build -platform windows/amd64      # Windows, from any machine
go tool mygo build -platform darwin/universal   # macOS, Apple silicon and Intel in one app
```

`mygo.json` configures the build: the name, identifier, version, file types and the macOS
`Info.plist` keys. Outputs go to `build/<os>-<arch>/`:

| Platform | Output |
| --- | --- |
| Windows | `Glint.exe`, `icon.png` beside it, and the installer `Glint Setup <version> amd64.exe` |
| macOS | `Glint.app`, and on a Mac the disk image `Glint <version>.dmg` |

The first Windows build downloads NSIS 3.13, which makes the installer, into
`%LOCALAPPDATA%\mygo`. On macOS and Linux, install NSIS (`brew install makensis`,
`apt install nsis`) to get the installer. Without it, `mygo build` skips the installer.

### Windows

`Glint.exe` runs without a console window and carries the icon from `resources/icon.png`, the
version information and a DPI-aware manifest. Check those with:

```sh
go run ./tools/packagecheck build/windows-amd64/Glint.exe
```

The installer installs Glint for the current user, without administrator rights, in
`%LOCALAPPDATA%\Programs\Glint`. It adds a Start menu shortcut and an entry in Settings > Apps,
and registers Glint under "Open with" for `.png`, `.jpg`, `.jpeg`, `.webp`, `.bmp`, `.gif`,
`.mp4`, `.mov` and `.m4v`, as ProgIDs `com.glint.app.<ext>`. Uninstalling removes all of it.
`/S` installs silently and `/D=<folder>` (last on the command line) installs elsewhere.
`tools/packagecheck/install.ps1` installs into a scratch folder, checks the files, the shortcut,
the registry and the tray icon, uninstalls, and checks that the registry is as it was:

```powershell
powershell -NoProfile -File tools/packagecheck/install.ps1 -Scratch $env:TEMP\glint-install
```

Glint 0.1, the Rust build, registered "Open with" itself as `Glint.Image`, `Glint.Video` and
`Applications\glint.exe`. The installed Go build removes those entries when it starts, so
"Open with" lists one Glint.

The executable and the installer are not signed, so SmartScreen warns on first run. Set
`windows.certificate` or `windows.signCommand` in `mygo.json` to sign them (see MyGo's
`docs/distribution.md`).

Transcription downloads the whisper.cpp 1.8.3 libraries on first use. They need the Microsoft
Visual C++ Redistributable (x64), which most PCs have and clean Windows installs lack. Glint
does not ship the runtime DLLs: Microsoft lets only licensed Visual Studio users redistribute
them, and its installer keeps them updated through Windows Update. When the runtime is missing
or too old, transcription fails with a message that links to
<https://aka.ms/vc14/vc_redist.x64.exe>.

### macOS

The app is an agent app (`LSUIElement`): it lives in the menu bar, with no Dock icon. Its
`Info.plist` carries the usage strings that the Screen Recording, Microphone and Camera prompts
show. Input Monitoring has no usage string; macOS shows its own text. The icon is
`resources/darwin/Glint.icns`, made from `assets/glint-mac-1024.png`, which sits inside Apple's
icon grid. Rerun `go run ./tools/macicon` after changing that artwork.

macOS grants Screen Recording, Input Monitoring, Microphone and Camera to an app's code
signature. An app signed ad hoc, the default, asks again after each rebuild. Sign with a stable
identity to keep the grants:

```sh
security find-identity -v -p codesigning
go tool mygo build -platform darwin/arm64 -sign "Apple Development: Name (TEAMID)"
open build/darwin-arm64/Glint.app
```

The released app is signed ad hoc and is not notarized. Gatekeeper blocks the first launch. On
macOS 14 and earlier, users open it with right-click > Open. On macOS 15 and later, they click
Open Anyway under System Settings > Privacy & Security after the first attempt. Either way,
clearing the quarantine flag also works:

```sh
xattr -dr com.apple.quarantine /Applications/Glint.app
```

## Releasing

The `version` in `mygo.json` names the release. The Release workflow builds the Windows
installer on a Windows runner and the macOS disk image on a macOS runner with `mygo build`, tags
the release `v<version>`, and attaches `Glint-<version>-windows-x64-setup.exe` and
`Glint-<version>-macos-universal.dmg`.

1. Bump `version` in `mygo.json`, then commit and push to `master`.
2. Run the Release workflow from the Actions tab, or from a terminal:

   ```sh
   gh workflow run release.yml -f draft=true
   ```

3. Review the draft release on GitHub, then publish it.

Running the workflow again for the same version replaces the files on the draft release. Once a
release is published, the workflow refuses to run for that version until `version` is bumped.
