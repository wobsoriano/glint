# Glint showreel

A 15-second, 1920x1080, 60 fps motion piece about Glint, made with [fframes](https://github.com/dmtrKovalenko/fframes) on the CPU backend.

The cuts land on a 120 BPM grid, a beat every 0.5 s. One piece of geometry carries the eye through every cut:

1. The capture selection becomes the preview card.
2. The card opens into the editor canvas.
3. The editor's red record dot drops into the recording control bar.
4. The recorded screen shrinks into the Studio preview.
5. Studio collapses into the glint star.

| Path | Contents |
| --- | --- |
| `src/lib.rs` | The six scenes (Capture, Cards, Editor, Record, Studio, Logo) and the audio map. |
| `src/main.rs` | The fframes command line. |
| `tools/gen_audio.py` | A Python synth, standard library only. It writes the music bed and sound effects into `media/`. |
| `media/` | Fonts and audio. Both are git-ignored and regenerated with the steps below. |

## Setup (Windows)

1. Unzip the BtbN `ffmpeg-n9.0-latest-win64-gpl-shared-9.0` build.
2. Create `.cargo/config.toml` with `FFMPEG_DIR` (the unzipped folder) and `LIBCLANG_PATH` set in an `[env]` table. This file is git-ignored.
3. Edit `r.sh` so it puts that FFmpeg `bin` folder on `PATH`. Rustc loads the media macro while it compiles, and the macro needs the FFmpeg DLLs.
4. Copy the fonts into `media/`:
   - `segoeui.ttf`, `segoeuib.ttf`, `segoeuil.ttf`, `seguisb.ttf` and `seguibl.ttf` from `C:\Windows\Fonts`. Segoe UI is Microsoft's font, so it is not committed.
   - `CascadiaMono.ttf`, also from `C:\Windows\Fonts`.
5. Generate the audio with `python tools/gen_audio.py`.

## Commands

```sh
./r.sh timeline                    # scenes and audio cues
./r.sh strip Editor -n 12          # contact sheet, written to strip.png
./r.sh inspect --pops              # finds problems and elements that pop in or out
./r.sh audio analyze               # loudness, about -15 LUFS and -1.2 dBTP
./r.sh render -o glint-showreel.mp4
```
