package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/wobsoriano/glint/internal/media"
	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/transcribe"
	"github.com/wobsoriano/glint/internal/ui/record"
	"github.com/wobsoriano/glint/internal/ui/stack/drive"
)

func narrationPath() string { return filepath.Join(settings.DataDir(), "narration.wav") }

// prepareFollow speaks the script into narration.wav with Windows SAPI and installs the
// teleprompter's speech model into GLINT_DATA_DIR, without touching the desktop.
func prepareFollow() error {
	if os.Getenv("GLINT_DATA_DIR") == "" {
		return fmt.Errorf("set GLINT_DATA_DIR to a scratch dir")
	}
	ps := `Add-Type -AssemblyName System.Speech
$s = New-Object System.Speech.Synthesis.SpeechSynthesizer
$f = New-Object System.Speech.AudioFormat.SpeechAudioFormatInfo(48000, [System.Speech.AudioFormat.AudioBitsPerSample]::Sixteen, [System.Speech.AudioFormat.AudioChannel]::Mono)
$s.SetOutputToWaveFile($env:GLINT_NARRATION_WAV, $f)
$s.Speak($env:GLINT_NARRATION_TEXT)
$s.Dispose()`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	cmd.Env = append(os.Environ(), "GLINT_NARRATION_WAV="+narrationPath(), "GLINT_NARRATION_TEXT="+script)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("SAPI: %v: %s", err, out)
	}
	fmt.Printf("narration %s\n", narrationPath())
	last := -1
	path, err := transcribe.DownloadModel(context.Background(), settings.DataDir(), transcribe.ModelTinyEn, func(done, total uint64) {
		if pct := int(done * 100 / max(total, 1)); pct/10 != last/10 {
			last = pct
			fmt.Printf("download %d%%\n", pct)
		}
	})
	if err != nil {
		return err
	}
	fmt.Printf("model %s\n", path)
	return nil
}

// runFollow records the display with the teleprompter on while narration.wav plays into it,
// shooting the pill and logging the word it is on, then records again with the model out of
// reach and checks the pill falls back to a static script while the recording still lands.
func runFollow(h *host, ctl *record.Controller) {
	l := drive.NewLog(*outDir)
	defer l.Finish()
	time.AfterFunc(3*time.Minute, func() {
		l.Printf("FAIL the drive took over 3 minutes")
		os.Exit(2)
	})
	wav, err := media.ReadWAV(narrationPath())
	if !l.Check(err == nil, "read the narration (run -prepare first): %v", err) {
		return
	}
	words := strings.Fields(script)
	speech := time.Duration(wav.DurationMS()) * time.Millisecond
	l.Printf("narration %v of %d words", speech, len(words))
	if !l.Check(transcribe.IsInstalled(settings.DataDir(), transcribe.ModelTinyEn), "tiny.en is installed in %s", settings.DataDir()) {
		return
	}
	os.Setenv("GLINT_CAPTURE_CHROME", "1")
	os.Setenv("GLINT_TELEPROMPTER_WAV", narrationPath())
	defer os.Unsetenv("GLINT_TELEPROMPTER_WAV")
	h.s.Record.Teleprompter, h.s.Record.Script, h.s.Record.PrompterLines = true, script, 2
	h.s.Record.Countdown, h.s.Record.Target, h.s.Record.Mic, h.s.Record.Camera = 0, settings.RecordDisplay, nil, nil
	d := &driver{l: l, h: h, ctl: ctl}
	displays, err := platform.Displays()
	if !l.Check(err == nil && len(displays) > 0, "read the displays: %v", err) {
		return
	}
	d.display = displays[0]

	pill, ok := d.startPrompted()
	if !ok {
		return
	}
	start := time.Now()
	type at struct {
		t     time.Duration
		index int
	}
	var path []at
	last, lastNote, shot := -2, "", 0
	for time.Since(start) < speech+3*time.Second {
		index, note := ctl.PrompterPosition()
		if note != lastNote {
			l.Printf("status %q", note)
			lastNote = note
		}
		if index != last {
			word := ""
			if index >= 0 {
				word = words[index]
			}
			l.Printf("tracker word %d %q at %.1f s", index, word, time.Since(start).Seconds())
			path = append(path, at{time.Since(start), index})
			last = index
		}
		if time.Since(start) >= time.Duration(shot)*3*time.Second {
			l.Shot(fmt.Sprintf("follow-%02d-%04.1fs-word%d.png", shot, time.Since(start).Seconds(), index), pill)
			shot++
		}
		time.Sleep(100 * time.Millisecond)
	}
	monotonic := true
	for i := 1; i < len(path); i++ {
		monotonic = monotonic && path[i].index > path[i-1].index
	}
	l.Check(len(path) > 5 && monotonic, "the tracker moved forward %d times and never back", len(path)-1)
	final := path[len(path)-1].index
	l.Check(final >= len(words)*8/10, "it ended on word %d of %d", final, len(words))
	l.Check(lastNote == "Following your voice", "the pill says %q", lastNote)
	if !d.stopInto("the narrated recording") {
		return
	}

	// A data dir whose models path is a file makes the model download fail at once.
	broken := filepath.Join(*outDir, "no-model")
	os.MkdirAll(broken, 0o755)
	os.WriteFile(filepath.Join(broken, "models"), nil, 0o644)
	os.Setenv("GLINT_DATA_DIR", broken)
	pill, ok = d.startPrompted()
	if !ok {
		return
	}
	var note string
	l.Check(drive.Until(5*time.Second, func() bool {
		_, note = ctl.PrompterPosition()
		return note == "Voice following is unavailable · scroll to move"
	}), "without the model the pill says %q", note)
	time.Sleep(500 * time.Millisecond)
	a := l.Shot("follow-no-model-a.png", pill)
	drive.Wheel(pill.X+pill.W/2, pill.Y+pill.H/3, -2)
	time.Sleep(400 * time.Millisecond)
	b := l.Shot("follow-no-model-b.png", pill)
	l.Check(drive.Diff(a, b, 30) > 200, "the wheel still scrolls it (%d px changed)", drive.Diff(a, b, 30))
	index, _ := ctl.PrompterPosition()
	l.Check(index == -1, "no word is marked read (%d)", index)
	time.Sleep(1500 * time.Millisecond)
	d.stopInto("the recording without the model")
	ctl.Quit(func() {})
}

func (d *driver) startPrompted() (pill platform.Rect, ok bool) {
	l := d.l
	pressHotkey()
	if !l.Check(d.waitSurface(record.SurfacePicker, true, 3*time.Second), "the hotkey opens the picker") {
		return pill, false
	}
	time.Sleep(800 * time.Millisecond)
	ok = d.click(record.SurfacePicker, "record") && drive.Until(5*time.Second, d.ctl.Live)
	if !ok {
		l.Shot("follow-not-started.png", d.display.Bounds)
	}
	if !l.Check(ok, "the recording starts") {
		return pill, false
	}
	if !l.Check(d.waitSurface(record.SurfacePrompter, true, 2*time.Second), "the teleprompter shows") {
		return pill, false
	}
	return drive.WindowRect(d.window(record.SurfacePrompter)), true
}

func (d *driver) stopInto(what string) bool {
	before := d.h.finishedCount()
	pressHotkey()
	if !d.l.Check(drive.Until(10*time.Second, func() bool { return d.h.finishedCount() > before }), "the hotkey stops %s into the Library", what) {
		return false
	}
	item := d.h.lastFinished()
	_, err := os.Stat(filepath.Join(item.Dir, "screen.mp4"))
	return d.l.Check(err == nil && *item.Meta.DurationMs > 1000, "%s holds screen.mp4 of %d ms", what, *item.Meta.DurationMs)
}
