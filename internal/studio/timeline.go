package studio

import (
	"cmp"
	"math"
	"reflect"
	"slices"
	"sort"
)

const (
	MinClipMs = 120.0
	MinZoomMs = 500.0
	MinSpeed  = 0.25
	MaxSpeed  = 8.0
)

// Span is a half-open range of milliseconds, in source or output time as the caller says.
type Span struct{ Start, End float64 }

func (c Clip) SourceLen() float64 { return max(c.EndMs-c.StartMs, 0) }

func (c Clip) OutputLen() float64 { return c.SourceLen() / c.Speed }

func (p *Project) DurationMs() float64 {
	var d float64
	for _, c := range p.Clips {
		d += c.OutputLen()
	}
	return d
}

// ClipAt finds the clip playing at outputMs and the output time it starts at. A time on a
// boundary belongs to the later clip; the end of the timeline belongs to the last clip.
func (p *Project) ClipAt(outputMs float64) (index int, start float64, ok bool) {
	for i, c := range p.Clips {
		end := start + c.OutputLen()
		if outputMs < end || i+1 == len(p.Clips) {
			return i, start, true
		}
		start = end
	}
	return 0, 0, false
}

func (p *Project) ClipOutputStart(index int) float64 {
	var s float64
	for _, c := range p.Clips[:index] {
		s += c.OutputLen()
	}
	return s
}

func (p *Project) SourceTime(outputMs float64) float64 {
	t := clamp(outputMs, 0, p.DurationMs())
	i, start, ok := p.ClipAt(t)
	if !ok {
		return 0
	}
	c := p.Clips[i]
	return min(c.StartMs+(t-start)*c.Speed, c.EndMs)
}

// OutputTime maps a source instant to output time; ok is false when that footage was
// removed. Clip boundaries are inclusive, which suits the playhead.
func (p *Project) OutputTime(sourceMs float64) (float64, bool) {
	return p.mapSource(sourceMs, func(s float64, c Clip) bool { return s >= c.StartMs && s <= c.EndMs })
}

// EventOutputTime is OutputTime with clips owning [start, end), so an event at the exact end
// of an outgoing clip never shows up on the first frame of the next one.
func (p *Project) EventOutputTime(sourceMs float64) (float64, bool) {
	return p.mapSource(sourceMs, func(s float64, c Clip) bool { return s >= c.StartMs && s < c.EndMs })
}

func (p *Project) mapSource(sourceMs float64, inside func(float64, Clip) bool) (float64, bool) {
	var start float64
	for _, c := range p.Clips {
		if inside(sourceMs, c) {
			return start + (sourceMs-c.StartMs)/c.Speed, true
		}
		start += c.OutputLen()
	}
	return 0, false
}

// OutputRanges lists the output ranges where the source range is visible, for drawing zooms
// and transcript spans on the output timeline.
func (p *Project) OutputRanges(source Span) []Span {
	var out []Span
	var start float64
	for _, c := range p.Clips {
		a, b := max(source.Start, c.StartMs), min(source.End, c.EndMs)
		if b > a {
			out = append(out, Span{start + (a-c.StartMs)/c.Speed, start + (b-c.StartMs)/c.Speed})
		}
		start += c.OutputLen()
	}
	return out
}

// SplitAt splits the clip under the playhead and returns the index of the new trailing clip.
// ok is false when either half would be shorter than MinClipMs.
func (p *Project) SplitAt(outputMs float64) (index int, ok bool) {
	i, _, ok := p.ClipAt(outputMs)
	if !ok {
		return 0, false
	}
	at := p.SourceTime(outputMs)
	c := p.Clips[i]
	if at-c.StartMs < MinClipMs || c.EndMs-at < MinClipMs {
		return 0, false
	}
	p.Clips[i].EndMs = at
	tail := c
	tail.StartMs = at
	p.Clips = slices.Insert(p.Clips, i+1, tail)
	return i + 1, true
}

// TrimClip moves a clip's source bounds, clamped so it never overlaps its neighbours or
// shrinks below MinClipMs. It reports whether anything changed.
func (p *Project) TrimClip(index int, startMs, endMs float64) bool {
	if index < 0 || index >= len(p.Clips) {
		return false
	}
	c := p.Clips[index]
	lo, hi := 0.0, math.Inf(1)
	if index > 0 {
		lo = p.Clips[index-1].EndMs
	}
	if index+1 < len(p.Clips) {
		hi = p.Clips[index+1].StartMs
	}
	start := clamp(startMs, lo, c.EndMs-MinClipMs)
	end := clamp(endMs, start+MinClipMs, max(hi, start+MinClipMs))
	if start == c.StartMs && end == c.EndMs {
		return false
	}
	p.Clips[index].StartMs = start
	p.Clips[index].EndMs = end
	return true
}

// DeleteClip removes a clip. The last remaining clip cannot be deleted.
func (p *Project) DeleteClip(index int) bool {
	if len(p.Clips) < 2 || index < 0 || index >= len(p.Clips) {
		return false
	}
	p.Clips = slices.Delete(p.Clips, index, index+1)
	return true
}

func (p *Project) SetSpeed(index int, speed float64) {
	if index >= 0 && index < len(p.Clips) {
		p.Clips[index].Speed = clamp(speed, MinSpeed, MaxSpeed)
	}
}

// CutSourceRanges removes source ranges (transcript cuts, filler words, silences) from every
// clip they touch, splitting clips where a range falls inside one. Pieces shorter than
// MinClipMs are dropped. It returns false, leaving the project unchanged, when nothing
// playable would survive.
func (p *Project) CutSourceRanges(ranges []Span) bool {
	var cuts []Span
	for _, r := range ranges {
		if r.End > r.Start {
			cuts = append(cuts, r)
		}
	}
	slices.SortStableFunc(cuts, func(a, b Span) int { return cmp.Compare(a.Start, b.Start) })
	var next []Clip
	for _, c := range p.Clips {
		push := func(a, b float64) {
			if b-a >= MinClipMs {
				next = append(next, Clip{StartMs: a, EndMs: b, Speed: c.Speed})
			}
		}
		cursor := c.StartMs
		for _, cut := range cuts {
			if !(cut.End > c.StartMs && cut.Start < c.EndMs) {
				continue
			}
			if cut.Start > cursor {
				push(cursor, cut.Start)
			}
			cursor = max(cursor, cut.End)
		}
		if cursor < c.EndMs {
			push(cursor, c.EndMs)
		}
	}
	if len(next) == 0 {
		return false
	}
	p.Clips = next
	return true
}

// AddZoom adds a manual zoom over an output range and returns its index.
func (p *Project) AddZoom(output Span) int {
	start, end := p.sourceSpan(output)
	return p.insertZoom(Zoom{StartMs: start, EndMs: end, Scale: 1.5, Enabled: true})
}

func (p *Project) RemoveZoom(index int) {
	if index >= 0 && index < len(p.Zooms) {
		p.Zooms = slices.Delete(p.Zooms, index, index+1)
	}
}

// RetimeZoom moves or resizes a zoom to an output range and returns its new index. An edited
// auto zoom becomes manual so regenerating auto zoom keeps the edit.
func (p *Project) RetimeZoom(index int, output Span) int {
	start, end := p.sourceSpan(output)
	z := p.Zooms[index]
	p.Zooms = slices.Delete(p.Zooms, index, index+1)
	z.StartMs, z.EndMs, z.Auto = start, end, false
	return p.insertZoom(z)
}

func (p *Project) sourceSpan(output Span) (float64, float64) {
	outEnd := output.Start + max(output.End-output.Start, MinZoomMs)
	start := p.SourceTime(output.Start)
	var end float64
	if i, clipStart, ok := p.ClipAt(output.Start); ok && outEnd <= clipStart+p.Clips[i].OutputLen() {
		end = p.Clips[i].StartMs + (outEnd-clipStart)*p.Clips[i].Speed
	} else {
		end = p.SourceTime(outEnd)
	}
	return start, max(end, start+MinZoomMs)
}

func (p *Project) insertZoom(z Zoom) int {
	i := sort.Search(len(p.Zooms), func(i int) bool { return !(p.Zooms[i].StartMs <= z.StartMs) })
	p.Zooms = slices.Insert(p.Zooms, i, z)
	return i
}

const historyLimit = 200

// History undoes and redoes whole-project snapshots. Record the project before each edit.
type History struct {
	undo, redo []Project
}

func (h *History) Record(before *Project) {
	if n := len(h.undo); n > 0 && reflect.DeepEqual(h.undo[n-1], *before) {
		return
	}
	if len(h.undo) == historyLimit {
		h.undo = slices.Delete(h.undo, 0, 1)
	}
	h.undo = append(h.undo, before.Clone())
	h.redo = nil
}

func (h *History) Undo(p *Project) bool {
	n := len(h.undo)
	if n == 0 {
		return false
	}
	h.redo = append(h.redo, *p)
	*p = h.undo[n-1]
	h.undo = h.undo[:n-1]
	return true
}

func (h *History) Redo(p *Project) bool {
	n := len(h.redo)
	if n == 0 {
		return false
	}
	h.undo = append(h.undo, *p)
	*p = h.redo[n-1]
	h.redo = h.redo[:n-1]
	return true
}

func (h *History) CanUndo() bool { return len(h.undo) > 0 }

func (h *History) CanRedo() bool { return len(h.redo) > 0 }
