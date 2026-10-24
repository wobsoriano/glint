package app

import (
	"strings"

	"github.com/wobsoriano/glint/internal/platform"
	"github.com/wobsoriano/glint/internal/settings"
	"github.com/wobsoriano/glint/internal/store"
	"github.com/wobsoriano/glint/internal/ui/record"
)

var _ record.Host = (*App)(nil)

// SelectRegion is the recording picker's window and area choice, over a frozen display.
func (a *App) SelectRegion(kind settings.RecordTarget, display platform.Display, done func(record.Selection, bool)) {
	record.Select(kind, display, done)
}

func (a *App) Toast(msg string) { a.toast(msg) }

// RecordingFinished takes a recording now in the Library: it becomes the last capture, the
// Library lists it, and Settings > After recording runs on it.
func (a *App) RecordingFinished(item store.Item, warnings []string) {
	a.last = item.Dir
	if a.Surfaces.Imported != nil {
		a.Surfaces.Imported(item)
	}
	if len(warnings) > 0 {
		a.toast(strings.Join(warnings, " · "))
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

// quit ends a recording that is running into the Library before Glint exits, as the Rust
// build's quit did.
func (a *App) quit(exit func()) {
	if r := a.win.recorder; r != nil {
		r.Quit(exit)
		return
	}
	exit()
}
