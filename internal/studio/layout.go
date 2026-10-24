package studio

import "math"

// Rect is an axis-aligned rectangle in canvas pixels. It mirrors glint-render's
// `annotate::R`; switch to the internal/render type once that package exports one.
type Rect struct{ X, Y, W, H float32 }

func (r Rect) Right() float32  { return r.X + r.W }
func (r Rect) Bottom() float32 { return r.Y + r.H }

func (r Rect) overlaps(o Rect) bool {
	return r.X < o.Right() && o.X < r.Right() && r.Y < o.Bottom() && o.Y < r.Bottom()
}

// MeasureText returns the width and line height of text drawn at size pixels in the face the
// compositor draws with.
type MeasureText func(text string, size float32) (w, h float32)

type Bubble struct {
	Rect   Rect
	Radius float32
}

// Layout places the screen card and camera bubble on an output canvas.
type Layout struct {
	Card   Rect
	Radius float32
	// FillW and FillH are the size the cropped screen is drawn at before zoom; in Fill mode
	// it overflows the card on one axis.
	FillW, FillH float64
	// Bubble is nil when the camera is hidden.
	Bubble *Bubble
}

func FrameLayout(p *Project, srcW, srcH, outW, outH uint32) Layout {
	d := &p.Design
	w, h := float32(outW), float32(outH)
	short := min(w, h)
	inset := clamp(d.Padding, 0, 0.475) * short
	aw, ah := w-2*inset, h-2*inset
	content := (float32(srcW) * p.Crop.W) / max(float32(srcH)*p.Crop.H, 1)
	fill := p.Aspect != AspectOriginal && p.AspectMode == AspectFill
	var cw, ch float32
	switch {
	case fill:
		cw, ch = aw, ah
	case aw/ah > content:
		cw, ch = ah*content, ah
	default:
		cw, ch = aw, aw/content
	}
	l := Layout{
		Card:   Rect{X: (w - cw) / 2, Y: (h - ch) / 2, W: cw, H: ch},
		Radius: d.CornerRadius * short,
	}
	l.FillH = max(float64(ch), float64(cw)/float64(content))
	l.FillW = l.FillH * float64(content)

	if cam := d.Camera; cam.Visible {
		dia := max(cam.Size*short, 24)
		margin := round32(short * 0.03)
		hx := min(dia/2+margin, w/2)
		hy := min(dia/2+margin, h/2)
		cx := clamp(cam.Center[0]*w, hx, w-hx)
		cy := clamp(cam.Center[1]*h, hy, h-hy)
		l.Bubble = &Bubble{
			Rect:   Rect{X: cx - dia/2, Y: cy - dia/2, W: dia, H: dia},
			Radius: clamp(cam.Roundness, 0, 0.5) * dia,
		}
	}
	return l
}

// ScreenXf maps screen-master pixels to canvas pixels for one viewport.
type ScreenXf struct{ K, Tx, Ty float64 }

func NewScreenXf(p *Project, l *Layout, srcW, srcH uint32, v Viewport) ScreenXf {
	dw, dh := l.FillW*v.Scale, l.FillH*v.Scale
	x0 := float64(l.Card.X+l.Card.W/2) - v.X*dw
	y0 := float64(l.Card.Y+l.Card.H/2) - v.Y*dh
	k := dw / (float64(p.Crop.W) * float64(srcW))
	return ScreenXf{
		K:  k,
		Tx: x0 - float64(p.Crop.X)*float64(srcW)*k,
		Ty: y0 - float64(p.Crop.Y)*float64(srcH)*k,
	}
}

func (x ScreenXf) Point(px, py float64) (float32, float32) {
	return float32(px*x.K + x.Tx), float32(py*x.K + x.Ty)
}

// ClickBase is the pixel size click highlights scale from.
func ClickBase(p *Project, l *Layout) float64 {
	return l.FillH * 21.0 / 1080.0 * float64(p.Design.Cursor.Size)
}

// CursorScale is the sprite scale that sizes a cursor captured at spriteScale to the card.
func CursorScale(fillH float64, spriteScale float32) float64 {
	return fillH * 0.032 / 16.0 / float64(max(spriteScale, 0.5))
}

// KeystrokeBox is a laid-out keystroke caption. Its alphas already include the frame's
// opacity.
type KeystrokeBox struct {
	Rect       Rect
	Radius     float32
	Font       float32
	ModifiersX float32
	KeyX       float32
	TextY      float32
	BackAlpha  float64
	ModsAlpha  float64
	KeyAlpha   float64
}

// LayoutKeystroke places the keystroke caption inside the card at pos, moving it clear of
// the caption bar when they would overlap.
func LayoutKeystroke(card Rect, pos KeystrokePosition, k KeyFrame, captionBar *Rect, measure MeasureText) KeystrokeBox {
	short := min(card.W, card.H)
	font := max(short*0.033, 11) * float32(k.Scale)
	padX, padY := font*0.72, font*0.46
	margin := short * 0.045
	mw, th := float32(0), font*1.25
	if k.Modifiers != "" {
		mw, th = measure(k.Modifiers, font)
	}
	kw, _ := measure(k.Key, font)
	w, h := mw+kw+2*padX, th+2*padY
	var x float32
	switch pos {
	case KeysTopLeft, KeysBottomLeft:
		x = card.X + margin
	case KeysTopCenter, KeysBottomCenter:
		x = card.X + (card.W-w)/2
	default:
		x = card.Right() - margin - w
	}
	y := card.Bottom() - margin - h
	if pos.top() {
		y = card.Y + margin
	}
	if captionBar != nil && (Rect{x, y, w, h}).overlaps(*captionBar) {
		if pos.top() {
			y = captionBar.Bottom() + margin/2
		} else {
			y = captionBar.Y - margin/2 - h
		}
	}
	return KeystrokeBox{
		Rect:       Rect{x, y, w, h},
		Radius:     font * 0.55,
		Font:       font,
		ModifiersX: x + padX,
		KeyX:       x + padX + mw,
		TextY:      y + padY,
		BackAlpha:  0.62 * k.Opacity,
		ModsAlpha:  0.7 * k.Opacity,
		KeyAlpha:   k.Opacity,
	}
}

// WordTone is how a caption word is drawn: plain white, the karaoke highlight, or dimmed
// because it has not been spoken yet.
type WordTone uint8

const (
	TonePlain WordTone = iota
	ToneActive
	TonePending
)

// KaraokeColor is the highlight for the word being spoken, packed as 0xRRGGBBAA.
const KaraokeColor uint32 = 0xFFD60AFF

type PlacedWord struct {
	Text string
	X, Y float32
	Tone WordTone
}

type CaptionBar struct {
	Rect   Rect
	Radius float32
	Font   float32
	// BackAlpha is the bar's black fill opacity.
	BackAlpha float64
	Words     []PlacedWord
}

const captionMaxLines = 3

type captionRow struct {
	start, end int
	width      float32
}

// LayoutCaptions wraps the caption words into at most three centered lines, shrinking the
// font when they would not fit, and places the bar on a canvas of w by h.
func LayoutCaptions(w, h float32, s CaptionSettings, c CaptionFrame, measure MeasureText) CaptionBar {
	font := max(min(w, h)*0.032*clamp(s.Size, 0.6, 1.8), 11)
	var rows []captionRow
	var widths []float32
	for {
		padX := font * 0.85
		maxW := w*0.92 - 2*padX
		space, _ := measure(" ", font)
		widths = widths[:0]
		for _, wd := range c.Words {
			ww, _ := measure(wd, font)
			widths = append(widths, ww)
		}
		rows = []captionRow{{}}
		for i, ww := range widths {
			last := &rows[len(rows)-1]
			if last.end > last.start && last.width+space+ww > maxW {
				rows = append(rows, captionRow{i, i + 1, ww})
				continue
			}
			if last.end > last.start {
				last.width += space + ww
			} else {
				last.width += ww
			}
			last.end = i + 1
		}
		if len(rows) <= captionMaxLines || font <= 11 {
			break
		}
		font = max(font*captionMaxLines/float32(len(rows)), 11)
	}
	padX, padY, lineH := font*0.85, font*0.5, font*1.25
	space, _ := measure(" ", font)
	var widest float32
	for _, r := range rows {
		widest = max(widest, r.width)
	}
	bw, bh := widest+2*padX, lineH*float32(len(rows))+2*padY
	bar := CaptionBar{
		Rect:      Rect{X: (w - bw) / 2, Y: h*clamp(s.Position, 0.1, 0.95) - bh/2, W: bw, H: bh},
		Radius:    min(font*0.6, bh/2),
		Font:      font,
		BackAlpha: 0.78,
	}
	for li, r := range rows {
		x := (w - r.width) / 2
		y := bar.Rect.Y + padY + float32(li)*lineH
		for i := r.start; i < r.end; i++ {
			tone := TonePlain
			switch {
			case !s.Karaoke:
			case i == c.Active:
				tone = ToneActive
			case i >= c.Spoken:
				tone = TonePending
			}
			bar.Words = append(bar.Words, PlacedWord{Text: c.Words[i], X: x, Y: y, Tone: tone})
			x += widths[i] + space
		}
	}
	return bar
}

func round32(v float32) float32 { return float32(math.Round(float64(v))) }
