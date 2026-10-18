#!/bin/sh
# Type-checks the workspace for Windows from macOS or Linux, so a change to shared or macOS code
# can be checked against the Windows backend without a Windows machine. Extra arguments go to
# cargo check, for example `tools/check-windows.sh --workspace --all-targets`.
#
# Needs `rustup target add x86_64-pc-windows-msvc` and a clang on PATH. C dependencies compile
# against the minimal headers in tools/check-windows and are never linked. whisper.cpp itself is
# skipped through its DOCS_RS switch; bindgen still generates its Windows bindings.
set -e
dir=$(cd "$(dirname "$0")/check-windows" && pwd)
export CC_x86_64_pc_windows_msvc=clang
export AR_x86_64_pc_windows_msvc="$dir/fakelib"
export CFLAGS_x86_64_pc_windows_msvc="--target=x86_64-pc-windows-msvc -I$dir"
export BINDGEN_EXTRA_CLANG_ARGS_x86_64_pc_windows_msvc="-I$dir"
export DOCS_RS=1
exec cargo check --target x86_64-pc-windows-msvc "$@"
