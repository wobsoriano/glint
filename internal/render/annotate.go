package render

import (
	"encoding/json"
	"fmt"
)

// TextStyle is how a text annotation stands off the screenshot.
type TextStyle uint8

const (
	TextPlain TextStyle = iota
	TextOutline
	TextPill
)

var textStyleNames = []string{"plain", "outline", "pill"}

func (s TextStyle) MarshalText() ([]byte, error) { return enumMarshal(s, textStyleNames) }
func (s *TextStyle) UnmarshalText(b []byte) error {
	return enumUnmarshal(s, b, textStyleNames, "text style")
}

// RedactStyle is how a redaction hides the pixels under it.
type RedactStyle uint8

const (
	RedactBlur RedactStyle = iota
	RedactPixelate
	RedactSolid
)

var redactStyleNames = []string{"Blur", "Pixelate", "Solid"}

var AllRedactStyles = [3]RedactStyle{RedactBlur, RedactPixelate, RedactSolid}

func (s RedactStyle) Label() string { return redactStyleNames[s] }

func (s RedactStyle) MarshalText() ([]byte, error) { return enumMarshal(s, redactStyleNames) }
func (s *RedactStyle) UnmarshalText(b []byte) error {
	return enumUnmarshal(s, b, redactStyleNames, "redact style")
}

// RedactStrength is the default redaction strength: pixel block size or blur radius, in image
// pixels at 1x DPI.
const RedactStrength float32 = 12

// Shape is one of Arrow, Line, RectShape, Ellipse, Pen, Text, Counter, Redact and Highlight.
// Coordinates are pixels of the cropped image.
type Shape interface{ shapeType() string }

type Arrow struct{ From, To Pt }
type Line struct{ From, To Pt }
type RectShape struct {
	Rect   R
	Filled bool
}
type Ellipse struct {
	Rect   R
	Filled bool
}

// Pen is a freehand stroke.
type Pen struct{ Points []Pt }
type Text struct {
	Pos   Pt
	Text  string
	Size  float32
	Style TextStyle
	Font  TextFont
}

// Counter is a numbered marker.
type Counter struct {
	Center Pt
	N      uint32
	Size   float32
}

// Redact blurs, pixelates or blacks out the screenshot under Rect. Strength is in image pixels.
type Redact struct {
	Rect     R
	Kind     RedactStyle
	Strength float32
}

// Highlight keeps Rect bright and dims the rest of the screenshot.
type Highlight struct{ Rect R }

func (Arrow) shapeType() string     { return "arrow" }
func (Line) shapeType() string      { return "line" }
func (RectShape) shapeType() string { return "rect" }
func (Ellipse) shapeType() string   { return "ellipse" }
func (Pen) shapeType() string       { return "pen" }
func (Text) shapeType() string      { return "text" }
func (Counter) shapeType() string   { return "counter" }
func (Redact) shapeType() string    { return "redact" }
func (Highlight) shapeType() string { return "highlight" }

// Annotation is a shape drawn over the screenshot.
type Annotation struct {
	ID    uint64
	Shape Shape
	Color Color
	Width float32
}

type annotationJSON struct {
	ID    uint64          `json:"id"`
	Shape json.RawMessage `json:"shape"`
	Color Color           `json:"color"`
	Width float32         `json:"width"`
}

type (
	twoPointsJSON struct {
		Type string `json:"type"`
		From Pt     `json:"from"`
		To   Pt     `json:"to"`
	}
	boxJSON struct {
		Type   string `json:"type"`
		Rect   R      `json:"rect"`
		Filled bool   `json:"filled"`
	}
	penJSON struct {
		Type   string `json:"type"`
		Points []Pt   `json:"points"`
	}
	textJSON struct {
		Type  string    `json:"type"`
		Pos   Pt        `json:"pos"`
		Text  string    `json:"text"`
		Size  float32   `json:"size"`
		Style TextStyle `json:"style"`
		Font  TextFont  `json:"font"`
	}
	counterJSON struct {
		Type   string  `json:"type"`
		Center Pt      `json:"center"`
		N      uint32  `json:"n"`
		Size   float32 `json:"size"`
	}
	redactJSON struct {
		Type     string      `json:"type"`
		Rect     R           `json:"rect"`
		Kind     RedactStyle `json:"kind"`
		Strength float32     `json:"strength"`
	}
	highlightJSON struct {
		Type string `json:"type"`
		Rect R      `json:"rect"`
	}
)

func marshalShape(s Shape) ([]byte, error) {
	switch s := s.(type) {
	case Arrow:
		return json.Marshal(twoPointsJSON{"arrow", s.From, s.To})
	case Line:
		return json.Marshal(twoPointsJSON{"line", s.From, s.To})
	case RectShape:
		return json.Marshal(boxJSON{"rect", s.Rect, s.Filled})
	case Ellipse:
		return json.Marshal(boxJSON{"ellipse", s.Rect, s.Filled})
	case Pen:
		pts := s.Points
		if pts == nil {
			pts = []Pt{}
		}
		return json.Marshal(penJSON{"pen", pts})
	case Text:
		return json.Marshal(textJSON{"text", s.Pos, s.Text, s.Size, s.Style, s.Font})
	case Counter:
		return json.Marshal(counterJSON{"counter", s.Center, s.N, s.Size})
	case Redact:
		return json.Marshal(redactJSON{"redact", s.Rect, s.Kind, s.Strength})
	case Highlight:
		return json.Marshal(highlightJSON{"highlight", s.Rect})
	}
	return nil, fmt.Errorf("unknown shape %T", s)
}

func unmarshalShape(data []byte) (Shape, error) {
	var tag struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return nil, err
	}
	switch tag.Type {
	case "arrow", "line":
		var v twoPointsJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		if tag.Type == "arrow" {
			return Arrow{v.From, v.To}, nil
		}
		return Line{v.From, v.To}, nil
	case "rect", "ellipse":
		var v boxJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		if tag.Type == "rect" {
			return RectShape{v.Rect, v.Filled}, nil
		}
		return Ellipse{v.Rect, v.Filled}, nil
	case "pen":
		var v penJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return Pen{v.Points}, nil
	case "text":
		v := textJSON{Font: DefaultTextFont()}
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return Text{v.Pos, v.Text, v.Size, v.Style, v.Font}, nil
	case "counter":
		var v counterJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return Counter{v.Center, v.N, v.Size}, nil
	case "redact":
		v := redactJSON{Strength: RedactStrength}
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return Redact{v.Rect, v.Kind, v.Strength}, nil
	case "highlight":
		var v highlightJSON
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, err
		}
		return Highlight{v.Rect}, nil
	}
	return nil, fmt.Errorf("unknown shape type %q", tag.Type)
}

func (a Annotation) MarshalJSON() ([]byte, error) {
	s, err := marshalShape(a.Shape)
	if err != nil {
		return nil, err
	}
	return json.Marshal(annotationJSON{a.ID, s, a.Color, a.Width})
}

func (a *Annotation) UnmarshalJSON(data []byte) error {
	var j annotationJSON
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	if j.Shape == nil {
		return fmt.Errorf("annotation %d has no shape", j.ID)
	}
	s, err := unmarshalShape(j.Shape)
	if err != nil {
		return err
	}
	*a = Annotation{j.ID, s, j.Color, j.Width}
	return nil
}

// Clone copies a so that the copy shares no slices with it.
func (a Annotation) Clone() Annotation {
	if p, ok := a.Shape.(Pen); ok {
		a.Shape = Pen{append([]Pt(nil), p.Points...)}
	}
	return a
}

func (a Annotation) IsRedaction() bool {
	_, ok := a.Shape.(Redact)
	return ok
}

// Bounds are approximate bounds used for selection and hit testing.
func (a Annotation) Bounds() R {
	pad := a.Width
	switch s := a.Shape.(type) {
	case Arrow:
		return RectFromPts(s.From, s.To).Inflate(pad * 2)
	case Line:
		return RectFromPts(s.From, s.To).Inflate(pad * 2)
	case RectShape:
		return s.Rect
	case Ellipse:
		return s.Rect
	case Highlight:
		return s.Rect
	case Redact:
		return s.Rect
	case Pen:
		if len(s.Points) == 0 {
			return R{}
		}
		r := RectFromPts(s.Points[0], s.Points[0])
		for _, p := range s.Points {
			r = r.Union(RectFromPts(p, p))
		}
		return r.Inflate(pad)
	case Text:
		b, _ := a.TextBox()
		return b.Inflate(s.Size * 0.3)
	case Counter:
		return R{s.Center.X - s.Size, s.Center.Y - s.Size, s.Size * 2, s.Size * 2}
	}
	return R{}
}

// TextBox is the laid-out text block (without padding) of a text annotation.
func (a Annotation) TextBox() (R, bool) {
	t, ok := a.Shape.(Text)
	if !ok {
		return R{}, false
	}
	w, h := FontFor(t.Font.Family, t.Font.Weight).Measure(t.Text, t.Size)
	return R{t.Pos.X, t.Pos.Y, max(w, t.Size*0.5), h}, true
}

func (a Annotation) Hit(p Pt, tolerance float32) bool {
	switch s := a.Shape.(type) {
	case Arrow:
		return segDist(p, s.From, s.To) <= a.Width+tolerance
	case Line:
		return segDist(p, s.From, s.To) <= a.Width+tolerance
	case RectShape:
		if !s.Filled {
			outer := s.Rect.Inflate(a.Width + tolerance)
			inner := s.Rect.Inflate(-(a.Width + tolerance))
			return outer.Contains(p) && !(inner.W > 0 && inner.H > 0 && inner.Contains(p))
		}
	case Ellipse:
		if !s.Filled {
			r := s.Rect
			cx, cy := r.X+r.W/2, r.Y+r.H/2
			rx, ry := r.W/2, r.H/2
			if rx < 1 || ry < 1 {
				return r.Inflate(tolerance).Contains(p)
			}
			dx, dy := (p.X-cx)/rx, (p.Y-cy)/ry
			d := sqrt32(dx*dx + dy*dy)
			return abs32(d-1)*min(rx, ry) <= a.Width+tolerance
		}
	case Pen:
		for i := 0; i+1 < len(s.Points); i++ {
			if segDist(p, s.Points[i], s.Points[i+1]) <= a.Width+tolerance {
				return true
			}
		}
		return false
	}
	return a.Bounds().Inflate(tolerance).Contains(p)
}

// Translate moves the annotation by (dx, dy).
func (a *Annotation) Translate(dx, dy float32) {
	mv := func(p Pt) Pt { return Pt{p.X + dx, p.Y + dy} }
	switch s := a.Shape.(type) {
	case Arrow:
		a.Shape = Arrow{mv(s.From), mv(s.To)}
	case Line:
		a.Shape = Line{mv(s.From), mv(s.To)}
	case RectShape:
		s.Rect = s.Rect.Translate(dx, dy)
		a.Shape = s
	case Ellipse:
		s.Rect = s.Rect.Translate(dx, dy)
		a.Shape = s
	case Highlight:
		a.Shape = Highlight{s.Rect.Translate(dx, dy)}
	case Redact:
		s.Rect = s.Rect.Translate(dx, dy)
		a.Shape = s
	case Pen:
		pts := make([]Pt, len(s.Points))
		for i, p := range s.Points {
			pts[i] = mv(p)
		}
		a.Shape = Pen{pts}
	case Text:
		s.Pos = mv(s.Pos)
		a.Shape = s
	case Counter:
		s.Center = mv(s.Center)
		a.Shape = s
	}
}

func boxOf(s Shape) (R, bool) {
	switch s := s.(type) {
	case RectShape:
		return s.Rect, true
	case Ellipse:
		return s.Rect, true
	case Highlight:
		return s.Rect, true
	case Redact:
		return s.Rect, true
	}
	return R{}, false
}

func withBox(s Shape, r R) Shape {
	switch s := s.(type) {
	case RectShape:
		s.Rect = r
		return s
	case Ellipse:
		s.Rect = r
		return s
	case Highlight:
		return Highlight{r}
	case Redact:
		s.Rect = r
		return s
	}
	return s
}

// Handles are the draggable control points.
func (a Annotation) Handles() []Pt {
	if r, ok := boxOf(a.Shape); ok {
		return []Pt{{r.X, r.Y}, {r.Right(), r.Y}, {r.Right(), r.Bottom()}, {r.X, r.Bottom()}}
	}
	switch s := a.Shape.(type) {
	case Arrow:
		return []Pt{s.From, s.To}
	case Line:
		return []Pt{s.From, s.To}
	case Text:
		b, _ := a.TextBox()
		return []Pt{{b.Right(), b.Bottom()}}
	case Counter:
		return []Pt{{s.Center.X + s.Size, s.Center.Y}}
	}
	return nil
}

// SetHandle drags handle i to p.
func (a *Annotation) SetHandle(i int, p Pt) {
	if r, ok := boxOf(a.Shape); ok {
		opposite := [4]Pt{{r.Right(), r.Bottom()}, {r.X, r.Bottom()}, {r.X, r.Y}, {r.Right(), r.Y}}[min(i, 3)]
		a.Shape = withBox(a.Shape, RectFromPts(opposite, p))
		return
	}
	switch s := a.Shape.(type) {
	case Arrow:
		if i == 0 {
			s.From = p
		} else {
			s.To = p
		}
		a.Shape = s
	case Line:
		if i == 0 {
			s.From = p
		} else {
			s.To = p
		}
		a.Shape = s
	case Text:
		b, _ := a.TextBox()
		if b.H > 0 {
			s.Size = clamp32(s.Size*(p.Y-s.Pos.Y)/b.H, 6, 2000)
			a.Shape = s
		}
	case Counter:
		s.Size = clamp32(s.Center.Dist(p), 6, 1000)
		a.Shape = s
	}
}

func segDist(p, a, b Pt) float32 {
	dx, dy := b.X-a.X, b.Y-a.Y
	len2 := dx*dx + dy*dy
	if len2 < 1e-6 {
		return p.Dist(a)
	}
	t := clamp32(((p.X-a.X)*dx+(p.Y-a.Y)*dy)/len2, 0, 1)
	return p.Dist(Pt{a.X + t*dx, a.Y + t*dy})
}

// Tool is an editor tool.
type Tool uint8

const (
	ToolSelect Tool = iota
	ToolRect
	ToolFilledRect
	ToolEllipse
	ToolLine
	ToolArrow
	ToolPen
	ToolCounter
	ToolText
	ToolHighlight
	ToolPixelate
	ToolBlur
	ToolCrop
)

var AllTools = [13]Tool{ToolSelect, ToolRect, ToolFilledRect, ToolEllipse, ToolLine, ToolArrow, ToolPen, ToolCounter, ToolText, ToolHighlight, ToolPixelate, ToolBlur, ToolCrop}

var toolInfo = [13]struct{ icon, name, key string }{
	{"select", "Select (V)", "v"},
	{"rect", "Rectangle (R)", "r"},
	{"rect-filled", "Filled rectangle (F)", "f"},
	{"ellipse", "Circle (O)", "o"},
	{"line", "Line (L)", "l"},
	{"arrow", "Arrow (A)", "a"},
	{"pen", "Freehand (P)", "p"},
	{"counter", "Numbered marker (N)", "n"},
	{"text", "Text (T)", "t"},
	{"spotlight", "Highlight (H)", "h"},
	{"pixelate", "Pixelate (X)", "x"},
	{"blur", "Blur (B)", "b"},
	{"crop", "Crop (C)", "c"},
}

func (t Tool) Icon() string { return toolInfo[t].icon }
func (t Tool) Name() string { return toolInfo[t].name }

// ToolFromKey maps a lowercase key to its tool.
func ToolFromKey(key string) (Tool, bool) {
	for i, ti := range toolInfo {
		if ti.key == key {
			return Tool(i), true
		}
	}
	return 0, false
}
