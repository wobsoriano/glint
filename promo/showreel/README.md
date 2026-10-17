# Glint showreel

A 32.5-second, 1920x1080, 60 fps feature tour of Glint, made with [fframes](https://github.com/dmtrKovalenko/fframes) on the CPU backend.

The first 30 seconds are 16 shots of 1.5 to 2.5 s. Each shot shows the whole window it is about, plays one interaction at a human pace, and then pushes the camera toward the result. The cuts land on the 120 BPM grid of the music bed, a beat every 0.5 s. The last 2.5 seconds are the logo, which starts on the bed's closing chord at 30.0 s.

## The shot table

`SHOTS` in `src/lib.rs` lists every shot. Each row has these fields:

| Field | Meaning |
| --- | --- |
| `at`, `len` | Start and length in seconds. |
| `group` | The feature group. The group tag above the label and the progress rail come from it. |
| `label` | The on-screen label and the scene name in the CLI. |
| `sub` | A short second line beside the label, or `""` for none. |
| `cut` | How the shot opens: `Open`, `Match`, `Fade`, or `Whip(dx, dy)`. A whip also plays a whoosh. |
| `cues` | Sounds inside the shot, in seconds from the shot's start. |
| `draw` | The function in `src/shots.rs` that draws the shot from its local time. |

The scenes, the label overlay, the rail, and the audio map all read the table. A `const` block checks at compile time that the rows start at 0, follow each other without gaps, sit on the 0.5 s grid, are at least 1.5 s long, and end at `T_LOGO`. A row that breaks any of these rules fails the build.

| At (s) | Length (s) | Group | Label | Cut |
| --- | --- | --- | --- | --- |
| 0.0 | 2.0 | Capture | Area | Open |
| 2.0 | 2.0 | Capture | Window or full screen | Fade |
| 4.0 | 1.5 | Capture | Self-timer | Fade |
| 5.5 | 2.0 | Capture | Capture Text | Fade |
| 7.5 | 2.0 | Quick access | Preview cards | Whip |
| 9.5 | 1.5 | Quick access | Drag and drop | Fade |
| 11.0 | 1.5 | Quick access | Pin | Fade |
| 12.5 | 2.0 | Editor | Annotate | Whip |
| 14.5 | 1.5 | Editor | Smart redaction | Match |
| 16.0 | 2.0 | Editor | Backgrounds and mockups | Match |
| 18.0 | 2.5 | Record | Record | Whip |
| 20.5 | 1.5 | Record | Teleprompter | Match |
| 22.0 | 2.0 | Studio | Auto zoom | Whip |
| 24.0 | 2.5 | Studio | Captions and transcript | Match |
| 26.5 | 1.5 | Studio | Export | Fade |
| 28.0 | 2.0 | Library | Library | Whip |

Only group changes use a whip. A whip enters with a 260 px offset and a directional blur, and the shot before it speeds up in the same direction over its last 0.1 s. A fade dissolves the next shot in over the last 0.4 s of the current one, and the label changes halfway through. A match cut is a hard cut between two shots that share the same window and camera, so the first frame of the new shot continues the last frame of the old one.

## Retime a shot

1. Change the shot's `len` in `SHOTS`, and shift the `at` of every row after it by the same amount.
2. If the total changes, set `T_LOGO` to the end of the last row.
3. If the shot's interaction no longer fits, change the event constants above its draw function in `src/shots.rs`. The cues read the same constants, so the sounds move with the picture.
4. Run `python tools/gen_audio.py`. The script reads `T_LOGO`, `LOGO_LEN`, and the first shot of each group from `src/lib.rs`, then writes a bed of the new length with the drop 0.25 s before the logo.
5. Run `./r.sh audio analyze` and check that the loudness is still about -14.7 LUFS with a true peak below -1 dBTP.

## Files

| Path | Contents |
| --- | --- |
| `src/lib.rs` | The shot table, the cut transitions, the label overlay, and the audio map. |
| `src/shots.rs` | One draw function per shot, with the cue times it shares with its row. |
| `src/kit.rs` | Drawings that several shots share: the mock app, the desktop, cards, the editor, Studio, the camera, and the helper that puts a whole window on the desktop. |
| `src/logo.rs` | The logo scene. |
| `src/motion.rs` | Easing curves and the spring. |
| `src/main.rs` | The fframes command line. |
| `tools/gen_audio.py` | A Python synth, standard library only. It writes the music bed and sound effects into `media/`. The bed adds hats from Quick access, claps from Editor, and an arpeggio from Studio. |
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
./r.sh timeline                          # shots and audio cues
./r.sh strip "Capture Text" -n 12        # contact sheet, written to strip.png
./r.sh inspect --pops                    # finds problems and elements that pop in or out
./r.sh audio analyze                     # loudness, about -14.7 LUFS and -1.3 dBTP
./r.sh render -o glint-showreel.mp4
```
