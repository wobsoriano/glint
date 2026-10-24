package render

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DocVersion is the edit.json version this package writes and the newest it reads.
const DocVersion uint32 = 1

// EditDoc is a capture's edit.json: the look, the effects, the crop and the annotations.
// Missing JSON fields take DefaultEditDoc's values.
type EditDoc struct {
	Version uint32
	// Scale factor of the monitor the capture came from; point sizes are multiplied by it.
	DPI         float32
	WindowTitle *string
	Style       Style
	Effects     Effects
	// Kept region of the base image in base pixels; nil keeps the whole image.
	Crop *R
	// In pixels of the cropped image.
	Annotations []Annotation
}

func DefaultEditDoc() EditDoc {
	return EditDoc{Version: DocVersion, DPI: 1, Style: DefaultStyle(), Effects: DefaultEffects(), Annotations: []Annotation{}}
}

// NewEditDoc is a document with no crop, effects or annotations.
func NewEditDoc(style Style, dpi float32, windowTitle *string) EditDoc {
	d := DefaultEditDoc()
	d.Style, d.DPI, d.WindowTitle = style, dpi, windowTitle
	return d
}

type editDocJSON struct {
	Version     uint32       `json:"version"`
	DPI         float32      `json:"dpi"`
	WindowTitle *string      `json:"window_title"`
	Style       Style        `json:"style"`
	Effects     Effects      `json:"effects"`
	Crop        *R           `json:"crop"`
	Annotations []Annotation `json:"annotations"`
}

func (d EditDoc) MarshalJSON() ([]byte, error) {
	anns := d.Annotations
	if anns == nil {
		anns = []Annotation{}
	}
	return json.Marshal(editDocJSON{d.Version, d.DPI, d.WindowTitle, d.Style, d.Effects, d.Crop, anns})
}

func (d *EditDoc) UnmarshalJSON(data []byte) error {
	def := DefaultEditDoc()
	j := editDocJSON{def.Version, def.DPI, def.WindowTitle, def.Style, def.Effects, def.Crop, def.Annotations}
	if err := json.Unmarshal(data, &j); err != nil {
		return err
	}
	if j.Annotations == nil {
		j.Annotations = []Annotation{}
	}
	*d = EditDoc{j.Version, j.DPI, j.WindowTitle, j.Style, j.Effects, j.Crop, j.Annotations}
	return nil
}

// ParseEditDoc reads edit.json and rejects documents newer than DocVersion.
func ParseEditDoc(data []byte) (EditDoc, error) {
	var d EditDoc
	if err := json.Unmarshal(data, &d); err != nil {
		return EditDoc{}, err
	}
	if d.Version > DocVersion {
		return EditDoc{}, fmt.Errorf("edit.json version %d is newer than this app supports", d.Version)
	}
	return d, nil
}

// JSON is the document as pretty-printed edit.json.
func (d EditDoc) JSON() []byte {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		panic(fmt.Sprintf("EditDoc serializes: %v", err))
	}
	return b
}

// Clone copies d so that the copy shares no slices or pointers with it.
func (d EditDoc) Clone() EditDoc {
	if d.WindowTitle != nil {
		t := *d.WindowTitle
		d.WindowTitle = &t
	}
	if d.Crop != nil {
		c := *d.Crop
		d.Crop = &c
	}
	d.Style = d.Style.Clone()
	anns := make([]Annotation, len(d.Annotations))
	for i, a := range d.Annotations {
		anns[i] = a.Clone()
	}
	d.Annotations = anns
	return d
}

// CropRect is the kept region, clamped to a w×h base image.
func (d EditDoc) CropRect(w, h uint32) R {
	full := R{0, 0, float32(w), float32(h)}
	if d.Crop != nil {
		if c, ok := d.Crop.RoundOut().Intersect(full); ok {
			return c
		}
	}
	return full
}

// SetCrop replaces the crop and moves annotations so they stay on the same base pixels.
func (d *EditDoc) SetCrop(crop R, w, h uint32) {
	old := d.CropRect(w, h)
	full := R{0, 0, float32(w), float32(h)}
	nc, ok := crop.RoundOut().Intersect(full)
	if !ok {
		nc = full
	}
	dx, dy := old.X-nc.X, old.Y-nc.Y
	for i := range d.Annotations {
		d.Annotations[i].Translate(dx, dy)
	}
	if nc == full {
		d.Crop = nil
	} else {
		d.Crop = &nc
	}
}

type Effects struct {
	Border      Border          `json:"border"`
	Watermark   Watermark       `json:"watermark"`
	Perspective Perspective     `json:"perspective"`
	Blur        ProgressiveBlur `json:"blur"`
}

func DefaultEffects() Effects {
	return Effects{DefaultBorder(), DefaultWatermark(), DefaultPerspective(), DefaultProgressiveBlur()}
}

func (e *Effects) UnmarshalJSON(data []byte) error {
	type plain Effects
	v := plain(DefaultEffects())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*e = Effects(v)
	return nil
}

type Border struct {
	Enabled bool `json:"enabled"`
	// Points.
	Width float32 `json:"width"`
	Color Color   `json:"color"`
}

func DefaultBorder() Border { return Border{Enabled: false, Width: 6, Color: White} }

func (b *Border) UnmarshalJSON(data []byte) error {
	type plain Border
	v := plain(DefaultBorder())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*b = Border(v)
	return nil
}

// Points is the width in points, zero when off.
func (b Border) Points() float32 {
	if b.Enabled {
		return max(b.Width, 0)
	}
	return 0
}

// Placement is a cell of the 3x3 grid over the screenshot, or a tiled pattern.
type Placement struct {
	tiled bool
	cell  uint8
}

// Grid places the watermark in cell i (0..=8, row-major).
func Grid(i uint8) Placement { return Placement{cell: i} }
func Tiled() Placement       { return Placement{tiled: true} }

func (p Placement) IsTiled() bool { return p.tiled }
func (p Placement) Cell() uint8   { return p.cell }

func (p Placement) MarshalJSON() ([]byte, error) {
	if p.tiled {
		return []byte(`"tiled"`), nil
	}
	return json.Marshal(map[string]uint8{"grid": p.cell})
}

func (p *Placement) UnmarshalJSON(data []byte) error {
	var s string
	if json.Unmarshal(data, &s) == nil {
		if s != "tiled" {
			return fmt.Errorf("unknown placement %q", s)
		}
		*p = Tiled()
		return nil
	}
	var g struct {
		Grid *uint8 `json:"grid"`
	}
	if err := json.Unmarshal(data, &g); err != nil {
		return err
	}
	if g.Grid == nil {
		return fmt.Errorf("unknown placement %s", data)
	}
	*p = Grid(*g.Grid)
	return nil
}

type Watermark struct {
	Enabled bool     `json:"enabled"`
	Text    string   `json:"text"`
	Font    TextFont `json:"font"`
	// Points.
	Size      float32   `json:"size"`
	Opacity   float32   `json:"opacity"`
	Color     Color     `json:"color"`
	Placement Placement `json:"placement"`
	// Degrees, tiled placement only.
	Rotation float32 `json:"rotation"`
}

func DefaultWatermark() Watermark {
	return Watermark{Font: DefaultTextFont(), Size: 22, Opacity: 0.6, Color: White, Placement: Grid(8), Rotation: -30}
}

func (w *Watermark) UnmarshalJSON(data []byte) error {
	type plain Watermark
	v := plain(DefaultWatermark())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*w = Watermark(v)
	return nil
}

func (w Watermark) IsVisible() bool {
	return w.Enabled && strings.TrimSpace(w.Text) != "" && w.Opacity > 0
}

type Perspective struct {
	// Degrees around the card's horizontal axis (top edge leans away when positive).
	RotX float32 `json:"rot_x"`
	// Degrees around the card's vertical axis.
	RotY float32 `json:"rot_y"`
	// Degrees around the viewing axis.
	RotZ float32 `json:"rot_z"`
	Zoom float32 `json:"zoom"`
	// Fractions of the canvas size.
	PanX float32 `json:"pan_x"`
	PanY float32 `json:"pan_y"`
	// Lens angle in degrees; smaller is flatter.
	FOV float32 `json:"fov"`
}

func DefaultPerspective() Perspective { return Perspective{Zoom: 1, FOV: 24} }

func (p *Perspective) UnmarshalJSON(data []byte) error {
	type plain Perspective
	v := plain(DefaultPerspective())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*p = Perspective(v)
	return nil
}

func (p Perspective) IsActive() bool {
	for _, v := range []float32{p.RotX, p.RotY, p.RotZ, p.PanX, p.PanY} {
		if abs32(v) > 1e-4 {
			return true
		}
	}
	return abs32(p.Zoom-1) > 1e-4
}

type BlurMode uint8

const (
	BlurRadial BlurMode = iota
	BlurDirectional
)

var blurModeNames = []string{"radial", "directional"}

func (m BlurMode) MarshalText() ([]byte, error) { return enumMarshal(m, blurModeNames) }
func (m *BlurMode) UnmarshalText(b []byte) error {
	return enumUnmarshal(m, b, blurModeNames, "blur mode")
}

type BlurEdge uint8

const (
	// BlurClipped blurs only the screenshot.
	BlurClipped BlurEdge = iota
	// BlurBleed blurs the whole scene, background included.
	BlurBleed
)

var blurEdgeNames = []string{"clipped", "bleed"}

func (e BlurEdge) MarshalText() ([]byte, error) { return enumMarshal(e, blurEdgeNames) }
func (e *BlurEdge) UnmarshalText(b []byte) error {
	return enumUnmarshal(e, b, blurEdgeNames, "blur edge")
}

type ProgressiveBlur struct {
	Enabled bool     `json:"enabled"`
	Mode    BlurMode `json:"mode"`
	Edge    BlurEdge `json:"edge"`
	// 0..100; the blur radius is strength/1000 of the blurred area's shortest edge.
	Strength float32 `json:"strength"`
	// 0..1: size of the sharp area, from a small spot to the farthest edge.
	Start float32 `json:"start"`
	// Degrees; direction of the sharp band in directional mode (0 = horizontal).
	Angle float32 `json:"angle"`
}

func DefaultProgressiveBlur() ProgressiveBlur {
	return ProgressiveBlur{Mode: BlurRadial, Edge: BlurBleed, Strength: 30, Start: 0.35}
}

func (b *ProgressiveBlur) UnmarshalJSON(data []byte) error {
	type plain ProgressiveBlur
	v := plain(DefaultProgressiveBlur())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*b = ProgressiveBlur(v)
	return nil
}

func (b ProgressiveBlur) IsActive() bool { return b.Enabled && b.Strength > 0.01 }

// RatioCrop is the largest rect of ratio (width / height) centered in within, in whole pixels.
func RatioCrop(within R, ratio float32) R {
	w, h := within.W, within.W/ratio
	if h > within.H {
		h = within.H
		w = h * ratio
	}
	w, h = max(round32(w), 1), max(round32(h), 1)
	return R{round32(within.X + (within.W-w)/2), round32(within.Y + (within.H-h)/2), w, h}
}
