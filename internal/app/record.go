package app

import (
	"context"
	"fmt"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/record"
	"github.com/wobsoriano/glint/internal/ui/toast"
)

var _ record.Host = (*App)(nil)

// SelectRegion is the recording picker's window and area choice, over a frozen display.
func (a *App) SelectRegion(kind settings.RecordTarget, display platform.Display, done func(record.Selection, bool)) {
	record.Select(kind, display, done)
}

func (a *App) Toast(msg string) { a.toast(msg) }

func (a *App) ToastAbove(msg string, gap float64) { toast.ShowAbove(msg, gap) }

// RecordingFinished takes a recording now in the Library: it becomes the last capture, the
// Library lists it, and Settings > After recording runs on it.
func (a *App) RecordingFinished(item store.Item, notice string) {
	a.last = item.Dir
	if a.Surfaces.Imported != nil {
		a.Surfaces.Imported(item)
	}
	if notice != "" {
		a.toast(notice)
	}
	for _, s := range recordingPlan(a.settings.AfterRecording) {
		switch s {
		case stepCopy:
			a.acts.Copy(item)
		case stepSave:
			a.acts.Save(item, false, a.acts.ReportSave)
		case stepEdit:
			a.Edit(item)
		case stepCard:
			if a.surface("The preview card", a.Surfaces.ShowCard != nil) {
				a.Surfaces.ShowCard(item)
			}
		}
	}
}

// recoverRecordings puts the recordings an earlier Glint stopped but never finished into the
// Library and clears the rest of what it left, then tells the user what came back.
func (a *App) recoverRecordings(orphans []store.PendingRecording) {
	saved := record.Recover(a.library, orphans)
	if len(saved) == 0 {
		return
	}
	a.post(func() {
		for _, item := range saved {
			if a.Surfaces.Imported != nil {
				a.Surfaces.Imported(item)
			}
		}
		if len(saved) == 1 {
			a.toast("Recovered a recording Glint did not finish")
		} else {
			a.toast(fmt.Sprintf("Recovered %d recordings Glint did not finish", len(saved)))
		}
	})
}

// recordingPlan is the order of the Rust build's record::automate: copy the video, save it,
// open Studio, then the card.
func recordingPlan(after settings.AfterRecording) []step {
	var steps []step
	for _, s := range []struct {
		on bool
		do step
	}{{after.Copy, stepCopy}, {after.Save, stepSave}, {after.Studio, stepEdit}, {after.Preview, stepCard}} {
		if s.on {
			steps = append(steps, s.do)
		}
	}
	return steps
}

// studioDeliverable is the video a recording hands out, as the Rust build's
// glint_studio::deliverable: Studio's render of its project, with the recorded sound mixed
// in, which Studio reuses until the project changes.
func studioDeliverable(item store.Item) (string, []string, error) {
	s, err := studio.LoadSession(item.Dir)
	if err != nil {
		return "", nil, err
	}
	path, err := studio.Deliverable(context.Background(), s)
	return path, s.Notices, err
}

// quit ends a recording that is running into the Library before Glint exits, as the Rust
// build's quit did.
func (a *App) quit(exit func()) {
	if r := a.win.recorder; r != nil {
		r.Quit(exit)
		return
	}
	exit()
}
