// Package studio is the window-free half of Glint Studio: the project.json model, presets,
// the timeline, auto zoom, motion tracks and overlay layout.
package studio

import (
	"cmp"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"slices"

	"github.com/wobsoriano/glint/internal/render"
)

const (
	ProjectVersion = 1
	ProjectFile    = "project.json"
)

// Project is the contents of project.json. Clip and zoom times are in source media time.
type Project struct {
	Version    uint32       `json:"version"`
	Clips      []Clip       `json:"clips"`
	Zooms      []Zoom       `json:"zooms"`
	Design     Design       `json:"design"`
	Aspect     AspectPreset `json:"aspect"`
	AspectMode AspectMode   `json:"aspect_mode"`
	// Region of the screen master shown in the card, normalized to the source size.
	Crop            CropRect          `json:"crop"`
	ClickHighlights bool              `json:"click_highlights"`
	Keystrokes      KeystrokeSettings `json:"keystrokes"`
	Captions        CaptionSettings   `json:"captions"`
	// Caption lines in source time, made from the transcript and edited by hand.
	CaptionCues []CaptionCue  `json:"caption_cues,omitempty"`
	Audio       AudioSettings `json:"audio"`
	Export      ExportOptions `json:"export"`
}

func DefaultProject() Project {
	return Project{
		Version:         ProjectVersion,
		Design:          DefaultDesign(),
		Aspect:          AspectOriginal,
		AspectMode:      AspectFit,
		Crop:            FullCrop,
		ClickHighlights: true,
		Keystrokes:      DefaultKeystrokeSettings(),
		Captions:        DefaultCaptionSettings(),
		Audio:           DefaultAudioSettings(),
		Export:          DefaultExportOptions(),
	}
}

// NewProject is one clip over the whole recording with every other setting at its default.
func NewProject(sourceDurationMs float64) Project {
	p := DefaultProject()
	p.Clips = []Clip{{StartMs: 0, EndMs: sourceDurationMs, Speed: 1}}
	return p
}

func (p *Project) UnmarshalJSON(data []byte) error {
	type plain Project
	v := plain(DefaultProject())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*p = Project(v)
	return nil
}

func (p Project) MarshalJSON() ([]byte, error) {
	type plain Project
	if p.Clips == nil {
		p.Clips = []Clip{}
	}
	if p.Zooms == nil {
		p.Zooms = []Zoom{}
	}
	return json.Marshal(plain(p))
}

// Clone copies the project deeply enough that edits to the copy never reach the original.
func (p Project) Clone() Project {
	p.Clips = slices.Clone(p.Clips)
	p.Zooms = slices.Clone(p.Zooms)
	p.CaptionCues = slices.Clone(p.CaptionCues)
	p.Design.Background = cloneBackground(p.Design.Background)
	return p
}

// ParseProject reads the contents of project.json, repaired to fit the recording.
func ParseProject(data []byte, sourceDurationMs float64) (Project, error) {
	var p Project
	if err := json.Unmarshal(data, &p); err != nil {
		return Project{}, err
	}
	if p.Version > ProjectVersion {
		return Project{}, fmt.Errorf("project version %d is newer than supported %d", p.Version, ProjectVersion)
	}
	p.Normalize(sourceDurationMs)
	return p, nil
}

func (p Project) Save(sessionDir string) error {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(filepath.Join(sessionDir, ProjectFile), data)
}

// Normalize repairs a project read from disk: clips are clamped to the source, sorted, made
// non-overlapping, and an empty clip list becomes one clip over the whole recording.
func (p *Project) Normalize(sourceDurationMs float64) {
	p.Version = ProjectVersion
	dur := max(sourceDurationMs, 0)
	var clips []Clip
	for _, c := range p.Clips {
		start := clamp(c.StartMs, 0, dur)
		end := clamp(c.EndMs, start, dur)
		speed := c.Speed
		if math.IsInf(speed, 0) || math.IsNaN(speed) {
			speed = 1
		}
		if end-start >= MinClipMs {
			clips = append(clips, Clip{StartMs: start, EndMs: end, Speed: clamp(speed, MinSpeed, MaxSpeed)})
		}
	}
	slices.SortStableFunc(clips, func(a, b Clip) int { return cmp.Compare(a.StartMs, b.StartMs) })
	kept := clips[:0]
	prevEnd := 0.0
	for _, c := range clips {
		c.StartMs = max(c.StartMs, prevEnd)
		if c.EndMs-c.StartMs >= MinClipMs {
			prevEnd = c.EndMs
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 && dur > 0 {
		kept = append(kept, Clip{StartMs: 0, EndMs: dur, Speed: 1})
	}
	p.Clips = kept
	p.Zooms = slices.DeleteFunc(p.Zooms, func(z Zoom) bool { return !(z.EndMs > z.StartMs) })
	slices.SortStableFunc(p.Zooms, func(a, b Zoom) int { return cmp.Compare(a.StartMs, b.StartMs) })
	slices.SortStableFunc(p.CaptionCues, func(a, b CaptionCue) int { return cmp.Compare(a.StartMs, b.StartMs) })
}

// OutputSize is the canvas size for export: the aspect preset applied to the cropped
// recording, scaled so its short side matches the chosen resolution. Both sides are even for
// the encoder.
func (p *Project) OutputSize(srcW, srcH uint32) (uint32, uint32) {
	visW := float32(srcW) * p.Crop.W
	visH := float32(srcH) * p.Crop.H
	var w, h float32
	r, fixed := p.Aspect.Ratio()
	switch {
	case !fixed:
		pad := clamp(p.Design.Padding, 0, 0.475)
		inset := min(visW, visH) * pad / (1 - 2*pad)
		w, h = visW+2*inset, visH+2*inset
	case r < visW/visH:
		w, h = visH*r, visH
	default:
		w, h = visW, visW/r
	}
	k := float32(1)
	if s, ok := p.Export.Resolution.ShortSide(); ok {
		k = float32(s) / min(w, h)
	}
	even := func(v float32) uint32 { return max(roundToU32(v*k)&^1, 2) }
	return even(w), even(h)
}

// roundToU32 rounds half away from zero and saturates like Rust's `f32::round() as u32`.
func roundToU32(v float32) uint32 {
	r := math.Round(float64(v))
	switch {
	case r <= 0 || math.IsNaN(r):
		return 0
	case r >= math.MaxUint32:
		return math.MaxUint32
	}
	return uint32(r)
}

type Clip struct {
	StartMs float64 `json:"start_ms"`
	EndMs   float64 `json:"end_ms"`
	Speed   float64 `json:"speed"`
}

func (c *Clip) UnmarshalJSON(data []byte) error {
	type plain Clip
	if err := requireKeys(data, "start_ms", "end_ms"); err != nil {
		return err
	}
	v := plain{Speed: 1}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*c = Clip(v)
	return nil
}

type Zoom struct {
	StartMs float64 `json:"start_ms"`
	EndMs   float64 `json:"end_ms"`
	// Magnification while fully zoomed in (1 = none).
	Scale   float32   `json:"scale"`
	Focus   ZoomFocus `json:"focus"`
	Enabled bool      `json:"enabled"`
	// Generated by auto zoom. Regenerating auto zoom replaces these and keeps manual ones.
	Auto bool `json:"auto"`
}

func (z *Zoom) UnmarshalJSON(data []byte) error {
	type plain Zoom
	if err := requireKeys(data, "start_ms", "end_ms"); err != nil {
		return err
	}
	v := plain{Scale: 1.5, Enabled: true}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*z = Zoom(v)
	return nil
}

type FocusKind uint8

const (
	FocusPointer FocusKind = iota
	// FocusFixed holds a normalized point inside the cropped screen card, top-left origin.
	FocusFixed
)

// ZoomFocus is where a zoom looks. X and Y are meaningful only for FocusFixed; the zero value
// follows the pointer.
type ZoomFocus struct {
	Kind FocusKind
	X, Y float32
}

func FixedFocus(x, y float32) ZoomFocus { return ZoomFocus{Kind: FocusFixed, X: x, Y: y} }

func (f ZoomFocus) MarshalJSON() ([]byte, error) {
	if f.Kind == FocusFixed {
		return json.Marshal(struct {
			Kind string  `json:"kind"`
			X    float32 `json:"x"`
			Y    float32 `json:"y"`
		}{"fixed", f.X, f.Y})
	}
	return []byte(`{"kind":"pointer"}`), nil
}

func (f *ZoomFocus) UnmarshalJSON(data []byte) error {
	var tag struct {
		Kind *string `json:"kind"`
	}
	if err := json.Unmarshal(data, &tag); err != nil {
		return err
	}
	if tag.Kind == nil {
		return fmt.Errorf("missing field `kind`")
	}
	switch *tag.Kind {
	case "pointer":
		*f = ZoomFocus{}
		return nil
	case "fixed":
		if err := requireKeys(data, "x", "y"); err != nil {
			return err
		}
		var v struct{ X, Y float32 }
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*f = FixedFocus(v.X, v.Y)
		return nil
	}
	return fmt.Errorf("unknown variant `%s`, expected `pointer` or `fixed`", *tag.Kind)
}

type Design struct {
	Background render.Background `json:"-"`
	// Card inset as a fraction of the canvas's short side.
	Padding float32 `json:"padding"`
	// Card corner radius as a fraction of the canvas's short side.
	CornerRadius float32 `json:"corner_radius"`
	// Shadow strength, 0..=1.
	Shadow float32        `json:"shadow"`
	Cursor CursorSettings `json:"cursor"`
	Camera CameraSettings `json:"camera"`
}

func DefaultDesign() Design {
	return Design{
		Background:   cloneBackground(render.GradientBackground{Gradient: render.Gradients[0].Gradient}),
		Padding:      0.06,
		CornerRadius: 0.02,
		Shadow:       0.45,
		Cursor:       DefaultCursorSettings(),
		Camera:       DefaultCameraSettings(),
	}
}

func (d Design) MarshalJSON() ([]byte, error) {
	type plain Design
	bg, err := render.MarshalBackground(d.Background)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Background json.RawMessage `json:"background"`
		plain
	}{bg, plain(d)})
}

func (d *Design) UnmarshalJSON(data []byte) error {
	type plain Design
	v := plain(DefaultDesign())
	wire := struct {
		Background json.RawMessage `json:"background"`
		*plain
	}{plain: &v}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Background != nil {
		bg, err := render.ParseBackground(wire.Background)
		if err != nil {
			return fmt.Errorf("background: %w", err)
		}
		v.Background = bg
	}
	*d = Design(v)
	return nil
}

func cloneBackground(b render.Background) render.Background {
	if g, ok := b.(render.GradientBackground); ok {
		g.Gradient.Stops = slices.Clone(g.Gradient.Stops)
		return g
	}
	return b
}

type CursorSettings struct {
	Visible bool `json:"visible"`
	// Multiplier on the cursor's recorded size.
	Size float32 `json:"size"`
	// 0 draws raw pointer samples; 1 is the heaviest glide.
	Smoothing float32 `json:"smoothing"`
}

func DefaultCursorSettings() CursorSettings {
	return CursorSettings{Visible: true, Size: 1, Smoothing: 0.5}
}

func (c *CursorSettings) UnmarshalJSON(data []byte) error {
	type plain CursorSettings
	*c = DefaultCursorSettings()
	return json.Unmarshal(data, (*plain)(c))
}

type CameraSettings struct {
	Visible bool `json:"visible"`
	// Normalized bubble center on the canvas, top-left origin.
	Center [2]float32 `json:"center"`
	// Bubble diameter as a fraction of the canvas's short side.
	Size float32 `json:"size"`
	// Corner radius as a fraction of the diameter: 0.5 is a circle, 0 a square.
	Roundness float32 `json:"roundness"`
}

func DefaultCameraSettings() CameraSettings {
	return CameraSettings{Visible: true, Center: [2]float32{0.85, 0.82}, Size: 0.26, Roundness: 0.5}
}

func (c *CameraSettings) UnmarshalJSON(data []byte) error {
	type plain struct {
		Visible   bool            `json:"visible"`
		Center    json.RawMessage `json:"center"`
		Size      float32         `json:"size"`
		Roundness float32         `json:"roundness"`
	}
	d := DefaultCameraSettings()
	v := plain{Visible: d.Visible, Size: d.Size, Roundness: d.Roundness}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	center := d.Center
	if v.Center != nil {
		var xs []float32
		if err := json.Unmarshal(v.Center, &xs); err != nil {
			return err
		}
		if len(xs) != 2 {
			return fmt.Errorf("invalid length %d, expected an array of length 2", len(xs))
		}
		center = [2]float32{xs[0], xs[1]}
	}
	*c = CameraSettings{Visible: v.Visible, Center: center, Size: v.Size, Roundness: v.Roundness}
	return nil
}

type AspectPreset string

const (
	AspectOriginal     AspectPreset = "original"
	AspectWide16x9     AspectPreset = "wide16x9"
	AspectVertical9x16 AspectPreset = "vertical9x16"
	AspectSquare       AspectPreset = "square"
	AspectPortrait4x5  AspectPreset = "portrait4x5"
)

var AllAspectPresets = []AspectPreset{AspectOriginal, AspectWide16x9, AspectVertical9x16, AspectSquare, AspectPortrait4x5}

func (a AspectPreset) Label() string {
	switch a {
	case AspectWide16x9:
		return "16:9"
	case AspectVertical9x16:
		return "9:16"
	case AspectSquare:
		return "1:1"
	case AspectPortrait4x5:
		return "4:5"
	}
	return "Original"
}

// Ratio is width over height; ok is false when the recording keeps its own aspect.
func (a AspectPreset) Ratio() (ratio float32, ok bool) {
	switch a {
	case AspectWide16x9:
		return 16.0 / 9.0, true
	case AspectVertical9x16:
		return 9.0 / 16.0, true
	case AspectSquare:
		return 1, true
	case AspectPortrait4x5:
		return 4.0 / 5.0, true
	}
	return 0, false
}

func (a *AspectPreset) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, a, AllAspectPresets)
}

type AspectMode string

const (
	AspectFill AspectMode = "fill"
	AspectFit  AspectMode = "fit"
)

func (m *AspectMode) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, m, []AspectMode{AspectFill, AspectFit})
}

type CropRect struct {
	X float32 `json:"x"`
	Y float32 `json:"y"`
	W float32 `json:"w"`
	H float32 `json:"h"`
}

var FullCrop = CropRect{X: 0, Y: 0, W: 1, H: 1}

func (c *CropRect) UnmarshalJSON(data []byte) error {
	type plain CropRect
	if err := requireKeys(data, "x", "y", "w", "h"); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(c))
}

type KeystrokePosition string

const (
	KeysTopLeft      KeystrokePosition = "top_left"
	KeysTopCenter    KeystrokePosition = "top_center"
	KeysTopRight     KeystrokePosition = "top_right"
	KeysBottomLeft   KeystrokePosition = "bottom_left"
	KeysBottomCenter KeystrokePosition = "bottom_center"
	KeysBottomRight  KeystrokePosition = "bottom_right"
)

var AllKeystrokePositions = []KeystrokePosition{
	KeysTopLeft, KeysTopCenter, KeysTopRight, KeysBottomLeft, KeysBottomCenter, KeysBottomRight,
}

func (k KeystrokePosition) top() bool {
	return k == KeysTopLeft || k == KeysTopCenter || k == KeysTopRight
}

func (k *KeystrokePosition) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, k, AllKeystrokePositions)
}

type KeystrokeSettings struct {
	Enabled  bool              `json:"enabled"`
	Position KeystrokePosition `json:"position"`
}

func DefaultKeystrokeSettings() KeystrokeSettings {
	return KeystrokeSettings{Enabled: true, Position: KeysBottomCenter}
}

func (k *KeystrokeSettings) UnmarshalJSON(data []byte) error {
	type plain KeystrokeSettings
	*k = DefaultKeystrokeSettings()
	return json.Unmarshal(data, (*plain)(k))
}

type CaptionCue struct {
	StartMs float64 `json:"start_ms"`
	EndMs   float64 `json:"end_ms"`
	Text    string  `json:"text"`
}

func (c *CaptionCue) UnmarshalJSON(data []byte) error {
	type plain CaptionCue
	if err := requireKeys(data, "start_ms", "end_ms", "text"); err != nil {
		return err
	}
	return json.Unmarshal(data, (*plain)(c))
}

type CaptionSettings struct {
	Enabled bool `json:"enabled"`
	// Vertical center of the caption bar as a fraction of canvas height (0.1..=0.95).
	Position float32 `json:"position"`
	// Multiplier on the default caption size (0.6..=1.8).
	Size    float32 `json:"size"`
	Karaoke bool    `json:"karaoke"`
}

func DefaultCaptionSettings() CaptionSettings {
	return CaptionSettings{Enabled: false, Position: 0.92, Size: 1, Karaoke: false}
}

func (c *CaptionSettings) UnmarshalJSON(data []byte) error {
	type plain CaptionSettings
	*c = DefaultCaptionSettings()
	return json.Unmarshal(data, (*plain)(c))
}

type AudioTrack struct {
	Enabled bool `json:"enabled"`
	// Linear gain, 0..=2.
	Volume float32 `json:"volume"`
}

func DefaultAudioTrack() AudioTrack { return AudioTrack{Enabled: true, Volume: 1} }

func (a *AudioTrack) UnmarshalJSON(data []byte) error {
	type plain AudioTrack
	*a = DefaultAudioTrack()
	return json.Unmarshal(data, (*plain)(a))
}

type AudioSettings struct {
	Mic    AudioTrack `json:"mic"`
	System AudioTrack `json:"system"`
}

func DefaultAudioSettings() AudioSettings {
	return AudioSettings{Mic: DefaultAudioTrack(), System: DefaultAudioTrack()}
}

func (a *AudioSettings) UnmarshalJSON(data []byte) error {
	type plain AudioSettings
	*a = DefaultAudioSettings()
	return json.Unmarshal(data, (*plain)(a))
}

type Fps string

const (
	Fps30 Fps = "30"
	Fps60 Fps = "60"
)

func (f Fps) Value() uint32 {
	if f == Fps30 {
		return 30
	}
	return 60
}

func (f *Fps) UnmarshalJSON(data []byte) error { return unmarshalEnum(data, f, []Fps{Fps30, Fps60}) }

// Resolution names the output size by the short side of the canvas.
type Resolution string

const (
	ResSource Resolution = "source"
	Res720    Resolution = "p720"
	Res1080   Resolution = "p1080"
	Res1440   Resolution = "p1440"
	Res2160   Resolution = "p2160"
)

func (r Resolution) ShortSide() (uint32, bool) {
	switch r {
	case Res720:
		return 720, true
	case Res1080:
		return 1080, true
	case Res1440:
		return 1440, true
	case Res2160:
		return 2160, true
	}
	return 0, false
}

func (r *Resolution) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, r, []Resolution{ResSource, Res720, Res1080, Res1440, Res2160})
}

type Quality string

const (
	QualityLow    Quality = "low"
	QualityMedium Quality = "medium"
	QualityHigh   Quality = "high"
)

func (q *Quality) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, q, []Quality{QualityLow, QualityMedium, QualityHigh})
}

type Codec string

const (
	CodecH264 Codec = "h264"
	CodecHevc Codec = "hevc"
)

func (c *Codec) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, c, []Codec{CodecH264, CodecHevc})
}

type Format string

const (
	FormatMp4 Format = "mp4"
	FormatGif Format = "gif"
)

func (f *Format) UnmarshalJSON(data []byte) error {
	return unmarshalEnum(data, f, []Format{FormatMp4, FormatGif})
}

type ExportOptions struct {
	Fps        Fps        `json:"fps"`
	MotionBlur bool       `json:"motion_blur"`
	Resolution Resolution `json:"resolution"`
	Quality    Quality    `json:"quality"`
	Codec      Codec      `json:"codec"`
	Format     Format     `json:"format"`
}

func DefaultExportOptions() ExportOptions {
	return ExportOptions{Fps: Fps60, MotionBlur: true, Resolution: Res1080, Quality: QualityHigh, Codec: CodecH264, Format: FormatMp4}
}

func (e *ExportOptions) UnmarshalJSON(data []byte) error {
	type plain ExportOptions
	*e = DefaultExportOptions()
	return json.Unmarshal(data, (*plain)(e))
}
