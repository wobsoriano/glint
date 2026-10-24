# Building Glint

Glint is a Go app built on [MyGo](https://mygo.egoist.dev) v0.6.3. Building it needs Go 1.27 and
nothing else: no cgo, no C compiler, no SDK. `go.mod` pins the MyGo CLI as a tool, so
`go tool mygo` runs the right version. Glint ships for Windows only. The code also compiles for
macOS, so the port described in `docs/mac-port.md` has a base to start from, but no macOS app is
released.

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
GOOS=darwin GOARCH=arm64 go build ./...   # keeps the macOS stubs compiling
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
go tool mygo build                              # on Windows
go tool mygo build -platform windows/amd64      # from any machine
```

`mygo.json` configures the build: the name, identifier, version and file types. The output goes
to `build/windows-amd64/`: `Glint.exe`, `icon.png` beside it, and the installer
`Glint Setup <version> amd64.exe`.

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

A local build is not signed, so SmartScreen warns on its first run. Releases are signed in CI;
see "Code signing" below.

Transcription downloads the whisper.cpp 1.8.3 libraries on first use. They need the Microsoft
Visual C++ Redistributable (x64), which most PCs have and clean Windows installs lack. Glint
does not ship the runtime DLLs: Microsoft lets only licensed Visual Studio users redistribute
them, and its installer keeps them updated through Windows Update. When the runtime is missing
or too old, transcription fails with a message that links to
<https://aka.ms/vc14/vc_redist.x64.exe>.

## Releasing

The `version` in `mygo.json` names the release. The Release workflow builds the installer on a
Windows runner with `mygo build`, signs it when SignPath is set up, tags the release
`v<version>`, and attaches `Glint-<version>-windows-x64-setup.exe`.

1. Bump `version` in `mygo.json`, then commit and push to `master`.
2. Run the Release workflow from the Actions tab, or from a terminal:

   ```sh
   gh workflow run release.yml -f draft=true
   ```

3. Review the draft release on GitHub, then publish it.

Running the workflow again for the same version replaces the files on the draft release. Once a
release is published, the workflow refuses to run for that version until `version` is bumped.

## Code signing

Releases are signed through [SignPath Foundation](https://signpath.org), which signs open-source
projects for free. The certificate is issued to SignPath Foundation, so Windows names it as the
publisher. Until the project is accepted, the workflow builds an unsigned installer and
SmartScreen warns users on first run.

The workflow signs two files, each in its own signing request that an approver accepts in
SignPath:

1. `Glint.exe`, as `mygo build` first makes it.
2. The installer. The installer holds `Glint.exe` compressed, where SignPath cannot sign it, so
   the workflow builds a second time with `tools/signswap` as `windows.signCommand`. The build
   is reproducible, so the second `Glint.exe` has the same bytes as the first, and signswap
   swaps in the signed copy before mygo packs the installer. The workflow then checks that
   `Glint.exe` and the signed installer both carry a valid signature.

To set it up once the project is accepted:

1. In SignPath, add the predefined GitHub.com trusted build system to the organization and link
   it to the Glint project. Give the project an artifact configuration whose root is a single
   `<pe-file>` with `<authenticode-sign/>`, because the workflow uploads each file unzipped.
2. In the GitHub repository, add the secret `SIGNPATH_API_TOKEN`, a token of a SignPath user who
   may submit signing requests, and the variables `SIGNPATH_ORGANIZATION_ID`,
   `SIGNPATH_PROJECT_SLUG` and `SIGNPATH_SIGNING_POLICY_SLUG`.

The signing steps run only while `SIGNPATH_PROJECT_SLUG` is set. A request waits up to an hour
for approval.
