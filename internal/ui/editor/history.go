package editor

import (
	"bytes"
	"time"

	"github.com/wobsoriano/glint/internal/render"
)

const (
	historyLimit = 200
	// coalesce is how long edits of one control (a slider dragged, a key held) stay one step.
	coalesce = 900 * time.Millisecond
)

// history is the undo and redo stacks of whole documents.
type history struct {
	undo, redo  []render.EditDoc
	lastControl string
	lastAt      time.Time
}

func (h *history) checkpoint(doc render.EditDoc) {
	h.undo = append(h.undo, doc.Clone())
	if len(h.undo) > historyLimit {
		h.undo = h.undo[1:]
	}
	h.redo = nil
	h.lastControl = ""
}

// checkpointFor checkpoints unless control changed the document within coalesce.
func (h *history) checkpointFor(control string, doc render.EditDoc, now time.Time) {
	recent := h.lastControl == control && now.Sub(h.lastAt) < coalesce
	if !recent {
		h.checkpoint(doc)
	}
	h.lastControl, h.lastAt = control, now
}

// dropNoop removes the last checkpoint when doc is unchanged since it.
func (h *history) dropNoop(doc render.EditDoc) {
	if n := len(h.undo); n > 0 && sameDoc(h.undo[n-1], doc) {
		h.undo = h.undo[:n-1]
	}
}

func (h *history) popUndo(cur render.EditDoc) (render.EditDoc, bool) {
	n := len(h.undo)
	if n == 0 {
		return cur, false
	}
	prev := h.undo[n-1]
	h.undo = h.undo[:n-1]
	h.redo = append(h.redo, cur)
	h.lastControl = ""
	return prev, true
}

func (h *history) popRedo(cur render.EditDoc) (render.EditDoc, bool) {
	n := len(h.redo)
	if n == 0 {
		return cur, false
	}
	next := h.redo[n-1]
	h.redo = h.redo[:n-1]
	h.undo = append(h.undo, cur)
	h.lastControl = ""
	return next, true
}

// sameDoc compares documents as edit.json would store them, so nil and empty slices match.
func sameDoc(a, b render.EditDoc) bool { return bytes.Equal(a.JSON(), b.JSON()) }
