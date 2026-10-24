package studio

import (
	"cmp"
	"math"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/wobsoriano/glint/internal/media/input"
)

const (
	ClickMs       = 400.0
	impactMs      = 120.0
	rippleDelayMs = 60.0
)

func easeOutCubic(p float64) float64 {
	q := 1 - clamp(p, 0, 1)
	return 1 - q*q*q
}

type ClickFrame struct {
	// Screen-master pixels of the recorded click (not the smoothed cursor).
	X, Y                      float64
	ImpactRadius, ImpactAlpha float64
	RingRadius, RingAlpha     float64
	RingWidth                 float64
}

type clickPress struct{ t, x, y float64 }

// ClickTrack is the click highlights on the output timeline.
type ClickTrack struct{ presses []clickPress }

func BuildClickTrack(p *Project, in *input.Timeline) ClickTrack {
	var presses []clickPress
	for _, c := range in.Clicks {
		if t, ok := p.EventOutputTime(float64(c.T)); ok {
			presses = append(presses, clickPress{t, float64(c.X), float64(c.Y)})
		}
	}
	slices.SortStableFunc(presses, func(a, b clickPress) int { return cmp.Compare(a.t, b.t) })
	return ClickTrack{presses}
}

// At lists the highlights visible at outputMs, sized relative to base pixels.
func (c ClickTrack) At(outputMs, base float64) []ClickFrame {
	end := sort.Search(len(c.presses), func(i int) bool { return !(c.presses[i].t <= outputMs) })
	start := sort.Search(len(c.presses), func(i int) bool { return !(c.presses[i].t < outputMs-ClickMs) })
	var out []ClickFrame
	for _, pr := range c.presses[start:end] {
		age := outputMs - pr.t
		impact := easeOutCubic(age / impactMs)
		ripple := easeOutCubic((age - rippleDelayMs) / (ClickMs - rippleDelayMs))
		f := ClickFrame{
			X:            pr.x,
			Y:            pr.y,
			ImpactRadius: base * (0.38 + 0.34*impact),
			RingRadius:   base * (0.62 + 0.93*ripple),
			RingWidth:    max(base*(0.18-0.08*ripple), 1),
		}
		if age <= impactMs {
			f.ImpactAlpha = 0.5 * (1 - impact)
		}
		if age >= rippleDelayMs {
			f.RingAlpha = 0.75 * (1 - ripple)
		}
		out = append(out, f)
	}
	return out
}

const (
	popInMs   = 160.0
	keyHoldMs = 1_100.0
	popOutMs  = 300.0
	keyLifeMs = popInMs + keyHoldMs + popOutMs
)

type KeyFrame struct {
	// Modifiers is the modifier part including its trailing `+`, drawn dimmer (e.g.
	// `Ctrl+Shift+`).
	Modifiers string
	Key       string
	Opacity   float64
	Scale     float64
}

type keyEvent struct {
	t     float64
	label string
}

// KeyTrack is the keystroke caption on the output timeline.
type KeyTrack struct{ events []keyEvent }

func BuildKeyTrack(p *Project, in *input.Timeline) KeyTrack {
	var events []keyEvent
	for _, k := range in.Keys {
		if k.Label == "" {
			continue
		}
		if t, ok := p.EventOutputTime(float64(k.T)); ok {
			events = append(events, keyEvent{t, k.Label})
		}
	}
	slices.SortStableFunc(events, func(a, b keyEvent) int { return cmp.Compare(a.t, b.t) })
	return KeyTrack{events}
}

func (k KeyTrack) At(outputMs float64) (KeyFrame, bool) {
	i := sort.Search(len(k.events), func(i int) bool { return !(k.events[i].t <= outputMs) }) - 1
	if i < 0 {
		return KeyFrame{}, false
	}
	t, label := k.events[i].t, k.events[i].label
	naturalEnd := t + keyLifeMs
	next := math.Inf(1)
	if i+1 < len(k.events) {
		next = k.events[i+1].t
	}
	if outputMs >= min(naturalEnd, next) {
		return KeyFrame{}, false
	}
	continues := i > 0 && k.events[i-1].t+keyLifeMs > t
	opacity, scale := 1.0, 1.0
	if !continues && outputMs-t < popInMs {
		e := easeOutCubic((outputMs - t) / popInMs)
		opacity, scale = e, 0.92+0.08*e
	}
	if naturalEnd <= next && outputMs > naturalEnd-popOutMs {
		e := easeOutCubic((naturalEnd - outputMs) / popOutMs)
		opacity = min(opacity, e)
		scale = min(scale, 0.97+0.03*e)
	}
	if opacity <= 0.005 {
		return KeyFrame{}, false
	}
	mods, key := splitLabel(label)
	return KeyFrame{Modifiers: mods, Key: key, Opacity: opacity, Scale: scale}, true
}

// splitLabel splits a shortcut label into its modifiers and its key: `Ctrl+Shift+` and `S`
// for a label recorded on Windows, `⇧⌘` and `S` for one recorded on macOS.
func splitLabel(label string) (string, string) {
	split := -1
	if i := strings.LastIndexByte(label, '+'); i >= 0 && i+1 < len(label) {
		split = i + 1
	}
	for i, r := range label {
		if strings.ContainsRune("⌃⌥⇧⌘", r) {
			split = max(split, i+utf8.RuneLen(r))
		}
	}
	if split < 0 || split >= len(label) {
		split = 0
	}
	return label[:split], label[split:]
}

// CaptionWord is a transcribed word in source time.
type CaptionWord struct {
	Text    string  `json:"text"`
	StartMs float64 `json:"start_ms"`
	EndMs   float64 `json:"end_ms"`
}

const lineMinMs = 800.0

type captionWordSpan struct {
	text       string
	start, end float64
}

type captionLine struct {
	start, end float64
	words      []captionWordSpan
}

type CaptionFrame struct {
	Words []string
	// Active is the index of the word being spoken, or -1 between words.
	Active int
	// Spoken counts the words that have started.
	Spoken int
}

// CaptionTrack is the caption cues laid on the output timeline with per-word timing.
type CaptionTrack struct{ lines []captionLine }

func BuildCaptionTrack(p *Project, words []CaptionWord) CaptionTrack {
	cues := p.CaptionCues
	timed := make([][]Span, len(cues))
	for _, w := range words {
		i := max(sort.Search(len(cues), func(i int) bool { return !(cues[i].StartMs <= w.StartMs) })-1, 0)
		if i < len(timed) {
			timed[i] = append(timed[i], Span{w.StartMs, max(w.EndMs, w.StartMs)})
		}
	}
	var lines []captionLine
	for ci, cue := range cues {
		spans := timed[ci]
		if len(spans) == 0 {
			spans = []Span{{cue.StartMs, max(cue.EndMs, cue.StartMs)}}
		}
		shown := strings.Fields(cue.Text)
		var ws []captionWordSpan
		for k, text := range shown {
			s, e := spread(spans, k, len(shown))
			start, ok := p.EventOutputTime(s)
			if !ok {
				continue
			}
			end, ok := p.OutputTime(e)
			if !ok || !(end >= start) {
				end = start + (e - s)
			}
			ws = append(ws, captionWordSpan{text, start, end})
		}
		if len(ws) > 0 {
			lines = append(lines, captionLine{start: ws[0].start, end: ws[len(ws)-1].end, words: ws})
		}
	}
	slices.SortStableFunc(lines, func(a, b captionLine) int { return cmp.Compare(a.start, b.start) })
	for i := range lines {
		next := math.Inf(1)
		if i+1 < len(lines) {
			next = lines[i+1].start
		}
		l := &lines[i]
		l.end = min(max(l.end, l.start+lineMinMs), next)
	}
	return CaptionTrack{lines}
}

func (c CaptionTrack) At(outputMs float64) (CaptionFrame, bool) {
	i := sort.Search(len(c.lines), func(i int) bool { return !(c.lines[i].start <= outputMs) }) - 1
	if i < 0 || outputMs >= c.lines[i].end {
		return CaptionFrame{}, false
	}
	f := CaptionFrame{Words: make([]string, len(c.lines[i].words)), Active: -1}
	for k, w := range c.lines[i].words {
		f.Words[k] = w.text
		if w.start <= outputMs {
			f.Spoken++
			if outputMs < w.end && f.Active < 0 {
				f.Active = k
			}
		}
	}
	return f, true
}

// spread maps word k of count shown words onto the timed spans, so an edited cue keeps its
// timing spread evenly over whatever words it now has.
func spread(spans []Span, k, count int) (float64, float64) {
	n := len(spans)
	at := func(x float64, end bool) float64 {
		var i int
		if end {
			i = max(int(math.Ceil(x)), 1) - 1
		} else {
			i = min(int(math.Floor(x)), n-1)
		}
		s := spans[i]
		return s.Start + clamp(x-float64(i), 0, 1)*(s.End-s.Start)
	}
	x0 := float64(k*n) / float64(count)
	x1 := float64((k+1)*n) / float64(count)
	return at(x0, false), at(x1, true)
}
