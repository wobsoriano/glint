# Building Glint

## Windows

Glint builds with `cargo build` on Windows 10/11 with the Rust MSVC toolchain. Two native build
tools are needed because `glint-transcribe` depends on `whisper-rs`, which compiles whisper.cpp
at build time.

### Prerequisites

| Tool | Why | Where to get it |
| --- | --- | --- |
| Rust (MSVC toolchain) | Everything | rustup |
| Windows 10/11 SDK | `windows` crate | Visual Studio Installer, "Windows 11 SDK" |
| cmake | `whisper-rs-sys` builds whisper.cpp with it | Visual Studio Build Tools 2022 ships one under `Common7\IDE\CommonExtensions\Microsoft\CMake\CMake\bin` (select "C++ CMake tools for Windows"), or install cmake from cmake.org |
| libclang | `whisper-rs-sys` runs bindgen, which loads `libclang.dll` | See below |

#### libclang

bindgen finds libclang through the `LIBCLANG_PATH` environment variable, which must name the
folder that contains `libclang.dll`. Two routes:

1. Visual Studio Installer: add the "C++ Clang tools for Windows" component. `libclang.dll` then
   lives under `VC\Tools\Llvm\x64\bin` of the Build Tools or Visual Studio install.
2. The PyPI `libclang` wheel, which is a plain zip holding the DLL and needs no install:

   ```powershell
   py -m pip download libclang==18.1.1 --only-binary :all: --no-deps -d $env:TEMP\libclang
   Copy-Item $env:TEMP\libclang\libclang-18.1.1-py2.py3-none-win_amd64.whl $env:TEMP\libclang\wheel.zip
   Expand-Archive $env:TEMP\libclang\wheel.zip $env:TEMP\libclang\whl
   New-Item -ItemType Directory -Force $HOME\.glint-build\libclang-18.1.1
   Copy-Item $env:TEMP\libclang\whl\libclang-18.1.1.data\platlib\clang\native\libclang.dll $HOME\.glint-build\libclang-18.1.1\
   ```

   Then `LIBCLANG_PATH` is `C:\Users\<you>\.glint-build\libclang-18.1.1`.

### Telling cargo where the tools are

`.cargo/config.toml` is git-ignored because it holds machine-specific paths. Copy the example and
edit both paths:

```powershell
Copy-Item .cargo\config.toml.example .cargo\config.toml
```

The `[env]` table sets `CMAKE` (full path to `cmake.exe`) and `LIBCLANG_PATH` for every cargo
invocation in this workspace. Setting the same two variables in your shell works too, if you
prefer not to keep the file.

### Build and test

```powershell
cargo build --workspace --all-targets   # expected: zero warnings
cargo test --workspace
cargo run --release
```

The first build compiles whisper.cpp and GPUI and takes several minutes. Later builds are
incremental.

## macOS

The macOS port is in progress (see `mac-port.md`). It needs Xcode, which provides the clang
that whisper.cpp builds with and the Metal compiler GPUI uses, and cmake from Homebrew
(`brew install cmake`). No `.cargo/config.toml` is needed.

```sh
cargo build --workspace --all-targets
cargo test --workspace
```

Screen Recording, Input Monitoring, Microphone and Camera access are granted to an app's code
signature, so a plain `cargo run` binary asks again after each rebuild, and the terminal you
run it from gets the grants. `tools/bundle-mac.sh` builds `target/debug/Glint.app` (or
`target/release` with `--release`) with the usage strings the prompts need. It signs ad hoc
unless `GLINT_SIGN_IDENTITY` names a certificate; sign with a stable identity to keep the
grants across rebuilds:

```sh
security find-identity -v -p codesigning
GLINT_SIGN_IDENTITY="Apple Development: Name (TEAMID)" tools/bundle-mac.sh
open target/debug/Glint.app
```

To check that a change still compiles for Windows without a Windows machine:

```sh
rustup target add x86_64-pc-windows-msvc
tools/check-windows.sh --workspace --all-targets   # expected: zero warnings
```

## Releasing

The `version` in the root `Cargo.toml` names the release. The Release workflow tags it
`v<version>` and attaches `Glint-<version>-macos-arm64.dmg` and `Glint-<version>-windows-x64.exe`.

1. Bump `version` in `Cargo.toml`, then commit and push to `master`.
2. Run the Release workflow from the Actions tab, or from a terminal:

   ```sh
   gh workflow run release.yml -f draft=true
   ```

3. Review the draft release on GitHub, then publish it.

Running the workflow again for the same version replaces the files on the existing release.

To build the DMG locally, run `tools/package-mac.sh`. It prints the path of the DMG under
`target/release`.

The macOS app is signed ad hoc and is not notarized. Gatekeeper blocks the first launch. On
macOS 14 and earlier, users open it with right-click > Open. On macOS 15 and later, they click
Open Anyway under System Settings > Privacy & Security after the first attempt. Either way,
clearing the quarantine flag also works:

```sh
xattr -dr com.apple.quarantine /Applications/Glint.app
```

The DMG is for Apple silicon only for now.
