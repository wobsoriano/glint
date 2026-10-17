#!/usr/bin/env bash
# Runs the showreel CLI with the FFmpeg DLLs on PATH: ./r.sh strip all -n 12
export PATH="$HOME/.glint-build/ffmpeg-n9.0-latest-win64-gpl-shared-9.0/bin:$PATH"
cd "$(dirname "$0")" && cargo run --release --quiet -- "$@"
