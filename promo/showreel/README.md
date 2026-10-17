# Glint showreel

A 15-second, 1920x1080, 60 fps feature montage of Glint, made with [fframes](https://github.com/dmtrKovalenko/fframes) on the CPU backend.

The first 13 seconds are 19 shots of 0.5 or 1 s, one feature each. The cuts land on the 120 BPM grid of the music bed, a beat every 0.5 s. The last 2 seconds are the logo, which starts on the bed's closing chord at 13.0 s.

## The shot table

`SHOTS` in `src/lib.rs` lists every shot. Each row has these fields:

| Field | Meaning |
| --- | --- |
| `at`, `len` | Start and length in seconds. |
| `group` | The feature group. The group tag above the label and the progress rail come from it. |
| `label` | The on-screen label and the scene name in the CLI. |
| `cut` | How the shot opens: `Open`, `Match`, `Whip(dx, dy)`, `Punch`, or `Flash`. The cut also picks the sound at the cut. |
| `cues` | Sounds inside the shot, in seconds from the shot's start. |
| `draw` | The function in `src/shots.rs` that draws the shot from its local time. |

To add or retime a shot, edit its row. The scenes, the label overlay, the rail, and the audio map all read the table. A `const` block checks at compile time that the rows start at 0, follow each other without gaps, sit on the 0.5 s grid, and end at `T_LOGO`. A row that breaks any of these rules fails the build.

| At (s) | Length (s) | Group | Label | Cut |
| --- | --- | --- | --- | --- |
| 0.0 | 1.0 | Capture | Area | Open |
| 1.0 | 0.5 | Capture | Window | Whip |
| 1.5 | 0.5 | Capture | Full screen | Flash |
| 2.0 | 0.5 | Capture | Self-timer | Punch |
| 2.5 | 1.0 | Capture | Capture Text | Whip |
| 3.5 | 1.0 | Quick access | Preview cards | Whip |
| 4.5 | 0.5 | Quick access | Drag and drop | Match |
| 5.0 | 0.5 | Quick access | Pin | Whip |
| 5.5 | 1.0 | Editor | Annotate | Punch |
| 6.5 | 0.5 | Editor | Smart redaction | Match |
| 7.0 | 0.5 | Editor | Backgrounds | Whip |
| 7.5 | 0.5 | Editor | Mockups | Match |
| 8.0 | 1.0 | Record | Record | Whip |
| 9.0 | 0.5 | Record | Teleprompter | Punch |
| 9.5 | 1.0 | Studio | Auto zoom | Whip |
| 10.5 | 0.5 | Studio | Captions | Punch |
| 11.0 | 0.5 | Studio | Transcript edits | Whip |
| 11.5 | 0.5 | Studio | Export | Punch |
| 12.0 | 1.0 | Library | Library | Whip |

A whip enters with a 300 px offset and a directional blur. The shot before it speeds up in the same direction over its last 0.1 s. A punch scales the new shot from 1.08 to 1. A flash is two white frames.

## Files

| Path | Contents |
| --- | --- |
| `src/lib.rs` | The shot table, the cut transitions, the label overlay, and the audio map. |
| `src/shots.rs` | One draw function per shot, with the cue times it shares with its row. |
| `src/kit.rs` | Drawings that several shots share: the mock app, the desktop, cards, the editor, and Studio. |
| `src/logo.rs` | The logo scene. |
| `src/motion.rs` | Easing curves and the spring. |
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

Scene names are the shot labels, so quote the ones with spaces.

```sh
./r.sh timeline                        # shots and audio cues
./r.sh strip "Capture Text" -n 12      # contact sheet, written to strip.png
./r.sh inspect --pops                  # finds problems and elements that pop in or out
./r.sh audio analyze                   # loudness, about -15 LUFS and -1.1 dBTP
./r.sh render -o glint-showreel.mp4
```
