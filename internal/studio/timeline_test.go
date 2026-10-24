package studio

import "testing"

type clipView [3]float64

func clipsOf(p *Project) []clipView {
	var out []clipView
	for _, c := range p.Clips {
		out = append(out, clipView{c.StartMs, c.EndMs, c.Speed})
	}
	return out
}

type opt struct {
	V  float64
	Ok bool
}

func some(v float64) opt { return opt{v, true} }

var none = opt{}

func outTime(p *Project, s float64) opt {
	v, ok := p.OutputTime(s)
	if !ok {
		return none
	}
	return some(v)
}

func eventTime(p *Project, s float64) opt {
	v, ok := p.EventOutputTime(s)
	if !ok {
		return none
	}
	return some(v)
}

func split(p *Project, at float64) opt {
	i, ok := p.SplitAt(at)
	if !ok {
		return none
	}
	return some(float64(i))
}

func TestSplitThenSpeedMapsBothDirections(t *testing.T) {
	p := NewProject(10_000)
	eq(t, split(&p, 4_000), some(1))
	p.SetSpeed(1, 2)
	eq(t, clipsOf(&p), []clipView{{0, 4_000, 1}, {4_000, 10_000, 2}})
	eq(t, p.DurationMs(), 7_000.0)
	eq(t, p.SourceTime(3_000), 3_000.0)
	eq(t, p.SourceTime(5_000), 6_000.0)
	eq(t, p.SourceTime(7_000), 10_000.0)
	eq(t, outTime(&p, 8_000), some(6_000))
	eq(t, outTime(&p, 4_000), some(4_000))
}

func TestDeleteRemovesFootageFromOutput(t *testing.T) {
	p := NewProject(9_000)
	p.SplitAt(3_000)
	p.SplitAt(6_000)
	eq(t, p.DeleteClip(1), true)
	eq(t, clipsOf(&p), []clipView{{0, 3_000, 1}, {6_000, 9_000, 1}})
	eq(t, p.DurationMs(), 6_000.0)
	eq(t, p.SourceTime(3_500), 6_500.0)
	eq(t, outTime(&p, 4_000), none)
	eq(t, outTime(&p, 7_000), some(4_000))
	eq(t, p.DeleteClip(0), true)
	if p.DeleteClip(0) {
		t.Fatal("the last clip stays")
	}
}

func TestSplitRefusesSlivers(t *testing.T) {
	p := NewProject(10_000)
	eq(t, split(&p, 50), none)
	eq(t, split(&p, 9_950), none)
	eq(t, len(p.Clips), 1)
}

func TestTrimClampsToNeighbours(t *testing.T) {
	p := NewProject(10_000)
	p.SplitAt(5_000)
	eq(t, p.TrimClip(1, 4_000, 9_000), true)
	eq(t, clipsOf(&p), []clipView{{0, 5_000, 1}, {5_000, 9_000, 1}})
	eq(t, p.TrimClip(0, 1_000, 4_000), true)
	eq(t, p.Clips[0].StartMs, 1_000.0)
	eq(t, p.DurationMs(), 7_000.0)
}

func TestTranscriptCutsSplitAndDropClips(t *testing.T) {
	p := NewProject(10_000)
	p.SplitAt(5_000)
	p.SetSpeed(1, 2)
	eq(t, p.CutSourceRanges([]Span{{1_000, 2_000}, {4_500, 6_000}, {9_950, 10_000}}), true)
	eq(t, clipsOf(&p), []clipView{{0, 1_000, 1}, {2_000, 4_500, 1}, {6_000, 9_950, 2}})
	eq(t, p.DurationMs(), 1_000.0+2_500.0+1_975.0)
	eq(t, p.SourceTime(3_500), 6_000.0)
	eq(t, outTime(&p, 7_000), some(4_000))
	eq(t, outTime(&p, 1_500), none)
	eq(t, p.CutSourceRanges([]Span{{0, 10_000}}), false)
	eq(t, len(p.Clips), 3)
}

func TestEventMappingIsHalfOpen(t *testing.T) {
	p := NewProject(4_000)
	p.SplitAt(2_000)
	p.DeleteClip(1)
	p.Clips = append(p.Clips, Clip{StartMs: 3_000, EndMs: 4_000, Speed: 1})
	eq(t, outTime(&p, 2_000), some(2_000))
	eq(t, eventTime(&p, 2_000), none)
	eq(t, eventTime(&p, 3_000), some(2_000))
}

func TestZoomsFollowTheirFootageThroughEdits(t *testing.T) {
	p := NewProject(10_000)
	i := p.AddZoom(Span{6_000, 8_000})
	eq(t, [2]float64{p.Zooms[i].StartMs, p.Zooms[i].EndMs}, [2]float64{6_000, 8_000})
	p.SplitAt(2_000)
	p.SplitAt(4_000)
	p.DeleteClip(1)
	eq(t, p.OutputRanges(Span{6_000, 8_000}), []Span{{4_000, 6_000}})
	j := p.RetimeZoom(0, Span{1_000, 2_500})
	eq(t, [2]float64{p.Zooms[j].StartMs, p.Zooms[j].EndMs}, [2]float64{1_000, 4_500})
	eq(t, p.OutputRanges(Span{1_000, 4_500}), []Span{{1_000, 2_000}, {2_000, 2_500}})
}

func TestHistoryRoundTripsEdits(t *testing.T) {
	p := NewProject(10_000)
	var h History
	h.Record(&p)
	p.SplitAt(5_000)
	h.Record(&p)
	p.SetSpeed(0, 4)
	eq(t, p.DurationMs(), 6_250.0)
	eq(t, h.Undo(&p), true)
	eq(t, p.DurationMs(), 10_000.0)
	eq(t, len(p.Clips), 2)
	eq(t, h.Undo(&p), true)
	eq(t, len(p.Clips), 1)
	eq(t, h.Undo(&p), false)
	eq(t, h.Redo(&p), true)
	eq(t, h.Redo(&p), true)
	eq(t, p.DurationMs(), 6_250.0)
	eq(t, h.CanRedo(), false)
}

func TestHistorySnapshotsDoNotShareClips(t *testing.T) {
	p := NewProject(10_000)
	var h History
	h.Record(&p)
	p.Clips[0].EndMs = 5_000
	h.Undo(&p)
	eq(t, p.Clips[0].EndMs, 10_000.0)
}
