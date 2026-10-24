package studio

import (
	"fmt"
	"math"
	"strconv"

	"github.com/egoist/mygo/ui"

	core "github.com/wobsoriano/glint/internal/studio"
	"github.com/wobsoriano/glint/internal/ui/theme"
)

var speeds = []float64{0.5, 0.75, 1, 1.25, 1.5, 2, 3, 4}

const (
	thumbCount  = 48
	thumbHeight = 96
	rulerH      = 22
	clipTop     = 26
	clipH       = 54
	zoomTop     = 88
	zoomH       = 26
	tracksH     = 120
	edge        = 8
	maxTLZoom   = 40
)

func speedLabel(speed float64) string {
	return strconv.FormatFloat(math.Round(speed*100)/100, 'f', -1, 64) + "×"
}

// scale is the timeline's mapping now; a trim or a zoom drag keeps the one it started with,
// so the track does not rescale under the pointer as the duration changes.
func (e *editor) scale() timeScale {
	switch d := e.drag.(type) {
	case trimDrag:
		return d.scale
	case zoomDrag:
		return d.scale
	}
	return newTimeScale(e.duration(), e.tracksW, e.tlZoom, e.tlStart)
}

func (e *editor) followPlayhead() {
	if e.tlZoom <= 1 {
		return
	}
	s := e.scale()
	at := e.position()
	if x := s.x(at); x < 0 || x > e.tracksW {
		e.tlStart = at - float64(e.tracksW)*s.msPerPx*0.1
	}
}

func (e *editor) setTimelineZoom(zoom float64, anchorX float32) {
	anchor := e.scale().ms(anchorX)
	e.tlZoom = max(1, min(zoom, maxTLZoom))
	e.tlStart = zoomAround(e.duration(), e.tracksW, e.tlZoom, anchor, anchorX)
}

// hit is what a press at a point of the tracks takes.
type hit struct {
	kind  hitKind
	index int
	// end is a clip's trailing edge; part a zoom's handle or body.
	end  bool
	part zoomPart
}

type hitKind int

const (
	hitTrack hitKind = iota
	hitClip
	hitClipEdge
	hitZoom
)

// hitAt finds the zoom or clip under (x, y), in tracks DIPs.
func (e *editor) hitAt(x, y float32) hit {
	p := e.project()
	s := e.scale()
	if y >= zoomTop && y <= zoomTop+zoomH {
		for i, z := range p.Zooms {
			ranges := p.OutputRanges(core.Span{Start: z.StartMs, End: z.EndMs})
			for k, r := range ranges {
				x0, x1 := s.x(r.Start), s.x(r.End)
				x1 = max(x1, x0+4)
				if x < x0 || x > x1 {
					continue
				}
				part := zoomBody
				switch {
				case k == 0 && x < x0+edge:
					part = zoomStart
				case k == len(ranges)-1 && x > x1-edge:
					part = zoomEnd
				}
				return hit{kind: hitZoom, index: i, part: part}
			}
		}
	}
	if y >= clipTop && y <= clipTop+clipH {
		start := 0.0
		for i, c := range p.Clips {
			x0, x1 := s.x(start), s.x(start+c.OutputLen())
			start += c.OutputLen()
			if x < x0 || x > x1 {
				continue
			}
			switch {
			case x < x0+edge:
				return hit{kind: hitClipEdge, index: i}
			case x > x1-edge:
				return hit{kind: hitClipEdge, index: i, end: true}
			}
			return hit{kind: hitClip, index: i}
		}
	}
	return hit{kind: hitTrack}
}

func (e *editor) tracksInput(ev ui.InputEvent) bool {
	switch {
	case ev.Kind == ui.InputPointerDown && ev.Button == 0:
		e.tracksPress(ev.X, ev.Y)
		return true
	case ev.Kind == ui.InputPointerMove && e.drag != nil:
		e.timelineDrag(ev.X)
		return true
	case ev.Kind == ui.InputPointerUp && e.drag != nil:
		e.endDrag()
		return true
	case ev.Kind == ui.InputZoom:
		e.setTimelineZoom(e.tlZoom*float64(ev.Scale), ev.X)
		return true
	case ev.Kind == ui.InputScroll && ev.Mods&ui.Cmd != 0:
		e.setTimelineZoom(e.tlZoom*(1-float64(ev.DY)/300), ev.X)
		return true
	case ev.Kind == ui.InputScroll && e.tlZoom > 1:
		d := ev.DX
		if d == 0 {
			d = ev.DY
		}
		s := e.scale()
		e.tlStart = newTimeScale(e.duration(), e.tracksW, e.tlZoom, s.startMs+float64(d)*s.msPerPx).startMs
		return true
	}
	return false
}

func (e *editor) tracksPress(x, y float32) {
	e.pause()
	s := e.scale()
	at := s.ms(x)
	h := e.hitAt(x, y)
	switch h.kind {
	case hitClipEdge:
		e.selectItem(selection{selClip, h.index})
		e.lastGroup = ""
		e.drag = trimDrag{clip: h.index, end: h.end, base: e.project().Clone(), fromMs: at, scale: s}
	case hitClip:
		e.selectItem(selection{selClip, h.index})
		e.drag = scrubDrag{}
		e.seek(at)
	case hitZoom:
		e.selectItem(selection{selZoom, h.index})
		base := e.project().Clone()
		z := base.Zooms[h.index]
		ranges := base.OutputRanges(core.Span{Start: z.StartMs, End: z.EndMs})
		if len(ranges) == 0 {
			return
		}
		e.lastGroup = ""
		e.drag = zoomDrag{zoom: h.index, part: h.part, base: base, span: core.Span{Start: ranges[0].Start, End: ranges[len(ranges)-1].End}, fromMs: at, scale: s}
	default:
		e.drag = scrubDrag{}
		e.seek(at)
	}
}

func (e *editor) timelineDrag(x float32) {
	switch d := e.drag.(type) {
	case scrubDrag:
		e.seek(e.scale().ms(x))
	case trimDrag:
		dx := d.scale.ms(x) - d.fromMs
		c := d.base.Clips[d.clip]
		next := d.base.Clone()
		if d.end {
			next.TrimClip(d.clip, c.StartMs, c.EndMs+dx*c.Speed)
		} else {
			next.TrimClip(d.clip, c.StartMs+dx*c.Speed, c.EndMs)
		}
		at := next.ClipOutputStart(d.clip)
		if d.end {
			at += next.Clips[d.clip].OutputLen() - 1
		}
		e.edit("trim", func(p *core.Project) { *p = next })
		e.seek(at)
	case zoomDrag:
		dx := d.scale.ms(x) - d.fromMs
		dur := d.base.DurationMs()
		length := d.span.End - d.span.Start
		var r core.Span
		switch d.part {
		case zoomBody:
			start := math.Max(0, math.Min(d.span.Start+dx, math.Max(dur-length, 0)))
			r = core.Span{Start: start, End: start + length}
		case zoomStart:
			r = core.Span{Start: math.Max(0, math.Min(d.span.Start+dx, d.span.End-core.MinZoomMs)), End: d.span.End}
		case zoomEnd:
			r = core.Span{Start: d.span.Start, End: math.Max(d.span.Start+core.MinZoomMs, math.Min(d.span.End+dx, dur))}
		}
		next := d.base.Clone()
		i := next.RetimeZoom(d.zoom, r)
		e.edit("zoom", func(p *core.Project) { *p = next })
		e.sel = selection{selZoom, i}
	}
}

func (e *editor) timeline(c *ui.Context, pal theme.Palette) {
	if e.player.isPlaying() {
		e.followPlayhead()
	}
	ui.Column(c).Shrink(0).Background(pal.Layer).BorderWidth(1, 0, 0, 0).BorderColor(pal.Border).Children(func() {
		e.timelineToolbar(c, pal)
		e.tracks(c, pal)
	})
}

func (e *editor) timelineToolbar(c *ui.Context, pal theme.Palette) {
	playing := e.player.isPlaying()
	ui.Row(c).Height(44).Gap(4).Padding(0, 12).Children(func() {
		icon, tip := theme.IconPlay, "Play (Space)"
		if playing {
			icon, tip = theme.IconPause, "Pause (Space)"
		}
		if iconButton(c, "play", icon, false, true, tip, pal) {
			e.togglePlay()
		}
		ui.Column(c).Width(130).Children(func() {
			ui.Text(c, formatTime(e.position())).FontSize(13).SingleLine()
			ui.Text(c, "of "+formatTime(e.duration())).FontSize(11).TextColor(pal.Muted).SingleLine()
		})
		separator(c, pal)
		if iconButton(c, "split", theme.IconScissors, false, true, "Split at playhead (S)", pal) {
			e.split()
		}
		if iconButton(c, "delete", theme.IconTrash, false, e.sel.kind != selNone, "Delete selection (Del)", pal) {
			e.deleteSelection()
		}
		if i, ok := e.sel.clip(); ok && i < len(e.project().Clips) {
			current := e.project().Clips[i].Speed
			button(c, "speed", "Speed "+speedLabel(current), theme.IconTimer, false, pal).Menu(func(m *ui.Menu) {
				for _, sp := range speeds {
					if m.Item(speedLabel(sp)).Checked(math.Abs(sp-current) < 1e-6).Chosen() {
						e.setSpeed(sp)
					}
				}
			})
		}
		separator(c, pal)
		if button(c, "add-zoom", "Add zoom", theme.IconZoom, false, pal).Clicked() {
			e.addZoom()
		}
		ui.Spacer(c)
		ui.Text(c, "Ctrl+scroll to zoom").FontSize(12).TextColor(pal.Muted).SingleLine()
		if iconButton(c, "tl-out", theme.IconMinus, false, e.tlZoom > 1, "Zoom timeline out", pal) {
			e.setTimelineZoom(e.tlZoom/1.5, e.tracksW/2)
		}
		if iconButton(c, "tl-in", theme.IconPlus, false, e.tlZoom < maxTLZoom, "Zoom timeline in", pal) {
			e.setTimelineZoom(e.tlZoom*1.5, max(0, min(e.scale().x(e.position()), e.tracksW)))
		}
	})
}

func (e *editor) tracks(c *ui.Context, pal theme.Palette) {
	box := ui.Box(c.Key("tracks")).Height(tracksH).Margin(0, 12, 8, 12).Clip().Label("Timeline")
	hx, hy, over := box.PointerPosition()
	cursor := ui.CursorDefault
	h := hit{kind: hitTrack}
	if over {
		h = e.hitAt(hx, hy)
	}
	switch {
	case e.drag != nil:
		switch d := e.drag.(type) {
		case trimDrag:
			cursor = ui.CursorResizeEW
		case zoomDrag:
			cursor = ui.CursorResizeEW
			if d.part == zoomBody {
				cursor = ui.CursorGrabbing
			}
		}
	case h.kind == hitClipEdge, h.kind == hitZoom && h.part != zoomBody:
		cursor = ui.CursorResizeEW
	case h.kind == hitZoom:
		cursor = ui.CursorGrab
	case h.kind == hitClip:
		cursor = ui.CursorPointer
	}
	box.Cursor(cursor).HandleInput(e.tracksInput)
	box.Draw(func(p *ui.Painter, r ui.Rect) {
		e.tracksW = r.W
		e.paintTracks(p, r, pal, h, over)
	})
}

func (e *editor) paintTracks(p *ui.Painter, r ui.Rect, pal theme.Palette, hover hit, over bool) {
	s := e.scale()
	pr := e.project()
	at := func(x, y, w, h float32) ui.Rect { return ui.Rect{X: r.X + x, Y: r.Y + y, W: w, H: h} }
	p.Fill(at(0, clipTop, r.W, clipH), pal.Canvas, 6)
	p.Fill(at(0, zoomTop, r.W, zoomH), pal.Canvas, 6)

	steps := []float64{100, 250, 500, 1_000, 2_000, 5_000, 10_000, 15_000, 30_000, 60_000, 120_000, 300_000}
	step := 600_000.0
	for _, st := range steps {
		if st/s.msPerPx >= 80 {
			step = st
			break
		}
	}
	for k := math.Floor(s.startMs / step); k <= math.Ceil(s.ms(r.W)/step); k++ {
		x := s.x(k * step)
		p.Fill(at(x, 0, 1, rulerH), pal.Border, 0)
		p.Text(r.X+x+4, r.Y+3, formatTime(k*step), 11, pal.Muted)
	}

	srcDur := max(e.session.SourceDurationMs, 1)
	tileW := float32(clipH) * float32(e.session.SourceW) / float32(max(e.session.SourceH, 1))
	start := 0.0
	for i, c := range pr.Clips {
		x0, x1 := s.x(start), s.x(start+c.OutputLen())
		clipStart := start
		start += c.OutputLen()
		if x1 < 0 || x0 > r.W {
			continue
		}
		box := at(x0, clipTop, x1-x0, clipH)
		selected := e.sel == selection{selClip, i}
		p.Clip(box, 6, func() {
			p.Fill(box, pal.Control, 0)
			if n := len(e.thumbs); n > 0 {
				first := int(math.Floor(float64(max(-x0, 0) / tileW)))
				last := int(math.Ceil(float64(min(r.W-x0, x1-x0) / tileW)))
				for k := first; k < last; k++ {
					tx := float32(k) * tileW
					t := clipStart + float64(tx+tileW/2)*s.msPerPx
					src := c.StartMs + (t-clipStart)*c.Speed
					idx := min(int(src/srcDur*thumbCount), n-1)
					tile := at(x0+tx, clipTop, tileW, clipH)
					p.Clip(tile, 0, func() { p.Image(e.thumbs[max(idx, 0)], tile, ui.Cover) })
				}
			}
			if math.Abs(c.Speed-1) > 1e-6 {
				label := ui.Span{Text: speedLabel(c.Speed), Size: 11, Color: ui.RGB(255, 255, 255)}
				lw, lh := p.MeasureText(0, label)
				p.Fill(at(x0+edge+4, clipTop+4, lw+10, lh+2), ui.RGBA(0, 0, 0, 0.75), theme.RadiusControl)
				p.RichText(r.X+x0+edge+9, r.Y+clipTop+5, 0, label)
			}
			for _, end := range []bool{false, true} {
				hovered := over && hover.kind == hitClipEdge && hover.index == i && hover.end == end
				if !selected && !hovered {
					continue
				}
				ex := x0
				if end {
					ex = x1 - edge
				}
				p.Fill(at(ex, clipTop, edge, clipH), pal.Accent, 0)
			}
		})
		border := pal.Border
		if selected {
			border = pal.Accent
		}
		p.Stroke(box, border, 6, 2)
	}

	for i, z := range pr.Zooms {
		ranges := pr.OutputRanges(core.Span{Start: z.StartMs, End: z.EndMs})
		selected := e.sel == selection{selZoom, i}
		label := speedLabel(float64(z.Scale))
		if z.Auto {
			label = "Auto · " + label
		}
		if z.Focus.Kind == core.FocusFixed {
			label += " · Fixed"
		}
		for _, rg := range ranges {
			x0, x1 := s.x(rg.Start), s.x(rg.End)
			if x1 < 0 || x0 > r.W {
				continue
			}
			box := at(x0, zoomTop, max(x1-x0, 4), zoomH)
			fill, border, text := pal.Accent.Alpha(0.33), pal.Accent, pal.Text
			if !z.Enabled {
				fill, border, text = pal.Control, pal.Border, pal.Muted
			}
			if selected {
				border = pal.Text
			}
			p.Fill(box, fill, 6)
			p.Stroke(box, border, 6, 2)
			p.Clip(box, 6, func() {
				p.Icon(theme.Icon(theme.IconZoom), at(x0+edge+2, zoomTop+7, 12, 12), text)
				p.Text(r.X+x0+edge+18, r.Y+zoomTop+5, label, 11, text)
			})
		}
	}

	x := s.x(e.position())
	p.Fill(at(x-1, 0, 2, r.H), pal.Text, 0)
	p.Fill(at(x-6, 0, 12, 12), pal.Text, 6)
}

func formatSeconds(ms float64) string { return fmt.Sprintf("%.1f s", ms/1000) }
