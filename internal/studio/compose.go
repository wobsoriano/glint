package studio

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/wobsoriano/glint/internal/media/input"
	"github.com/wobsoriano/glint/internal/render"
	_ "golang.org/x/image/webp"
)

// CursorImage is a cursor sprite in screen-master pixels, premultiplied.
type CursorImage struct {
	Pixmap  *image.RGBA
	Hotspot [2]uint32
	// Scale is the display scale the sprite was captured at.
	Scale float32
}

// Assets is what a session gives every frame besides the decoded media: the recorded input,
// the cursor sprites it names and the transcript's words. It caches the motion tracks and the
// static stage behind the screen, so composing many frames of one edit builds them once.
type Assets struct {
	input    input.Timeline
	cursors  map[uint32]CursorImage
	captions []CaptionWord

	mu        sync.Mutex
	motionKey string
	motion    *Motion
	stageKey  stageKey
	stage     *stage
}

func NewAssets(in input.Timeline, cursors map[uint32]CursorImage, captions []CaptionWord) *Assets {
	return &Assets{input: in, cursors: cursors, captions: captions}
}

// LoadAssets reads input.json and the cursor sprites it references from a session directory.
func LoadAssets(sessionDir string, captions []CaptionWord) (*Assets, error) {
	in, err := input.Load(sessionDir)
	if err != nil {
		return nil, fmt.Errorf("reading input timeline in %s: %w", sessionDir, err)
	}
	cursors := make(map[uint32]CursorImage, len(in.Cursors))
	for _, c := range in.Cursors {
		path := filepath.Join(sessionDir, c.File)
		img, err := decodeImage(path)
		if err != nil {
			return nil, fmt.Errorf("reading cursor %s: %w", path, err)
		}
		cursors[c.ID] = CursorImage{Pixmap: img, Hotspot: c.Hotspot, Scale: c.Scale}
	}
	return NewAssets(in, cursors, captions), nil
}

// decodeImage reads an image file into premultiplied RGBA at its full size.
func decodeImage(path string) (*image.RGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	if _, ok := img.(*image.NRGBA); !ok {
		n := image.NewNRGBA(image.Rect(0, 0, img.Bounds().Dx(), img.Bounds().Dy()))
		draw.Draw(n, n.Rect, img, img.Bounds().Min, draw.Src)
		img = n
	}
	return render.Premultiply(img), nil
}

func (a *Assets) Input() *input.Timeline { return &a.input }

// Motion returns the motion tracks for p, rebuilt only when an edit that moves them changes.
func (a *Assets) Motion(p *Project) *Motion {
	key, _ := json.Marshal([]any{p.Clips, p.Zooms, p.Aspect, p.AspectMode, p.Crop, p.Design.Padding, p.Design.Cursor.Smoothing, p.CaptionCues})
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.motion == nil || a.motionKey != string(key) {
		a.motion, a.motionKey = BuildMotion(p, &a.input, a.captions), string(key)
	}
	return a.motion
}

// stageKey is everything the stage depends on.
type stageKey struct {
	outW, outH, srcW, srcH uint32
	hasCamera              bool
	background             string
	padding, radius        float32
	shadow                 float32
	camera                 CameraSettings
	aspect                 AspectPreset
	mode                   AspectMode
	crop                   CropRect
}

func (a *Assets) stageFor(p *Project, screen *image.RGBA, hasCamera bool, outW, outH uint32) *stage {
	d := &p.Design
	bg, _ := render.MarshalBackground(d.Background)
	key := stageKey{
		outW, outH, uint32(screen.Rect.Dx()), uint32(screen.Rect.Dy()), hasCamera, string(bg),
		d.Padding, d.CornerRadius, d.Shadow, d.Camera, p.Aspect, p.AspectMode, p.Crop,
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stage == nil || a.stageKey != key {
		a.stage, a.stageKey = buildStage(p, screen, hasCamera, outW, outH), key
	}
	return a.stage
}

// FrameInputs are the decoded frames one output frame is made from. Both are opaque.
type FrameInputs struct {
	// Screen is the screen frame at the source time of OutputMs.
	Screen *image.RGBA
	// Camera is nil when there is no camera track.
	Camera   *image.RGBA
	OutputMs float64
}

// stage is the part of a frame that only changes with the design: the background, the shadows
// and the card's clip mask.
type stage struct {
	layout   Layout
	base     *image.RGBA
	cardMask *image.Alpha
}

func buildStage(p *Project, screen *image.RGBA, hasCamera bool, outW, outH uint32) *stage {
	l := FrameLayout(p, uint32(screen.Rect.Dx()), uint32(screen.Rect.Dy()), outW, outH)
	w, h := int(outW), int(outH)
	base := image.NewRGBA(image.Rect(0, 0, w, h))
	var blur, bgImage *image.RGBA
	switch bg := p.Design.Background.(type) {
	case render.BlurredBackground:
		blur = render.MakeBlurBg(screen)
	case render.ImageBackground:
		bgImage, _ = decodeImage(bg.Path)
	}
	render.DrawBackground(base, p.Design.Background, float32(w), float32(h), render.Pt{}, blur, bgImage)
	short := float32(min(outW, outH))
	if shadow := clamp(p.Design.Shadow, 0, 1); shadow > 0 {
		if path := render.RoundedRect(l.Card, l.Radius); path != nil {
			render.DrawShadow(base, path, short*0.035, shadow)
		}
		if l.Bubble != nil && hasCamera {
			if path := render.RoundedRect(l.Bubble.Rect, l.Bubble.Radius); path != nil {
				render.DrawShadow(base, path, short*0.02, shadow)
			}
		}
	}
	mask := render.NewMask(w, h)
	if path := render.RoundedRect(l.Card, l.Radius); path != nil {
		render.FillMask(mask, path)
	}
	return &stage{l, base, mask}
}

// ComposeFrame draws one output frame of outW by outH at in.OutputMs: the stage, the screen
// through the zoom viewport, click highlights, the cursor, the camera bubble, the caption bar
// and the keystroke caption. The result has straight alpha; hand it to RecycleFrame once it is
// no longer needed.
func ComposeFrame(p *Project, a *Assets, in FrameInputs, outW, outH uint32) *image.NRGBA {
	return compose(p, a, in, outW, outH, false)
}

// ComposeFrameMotionBlur is ComposeFrame with the screen blurred along the camera's motion
// when the project's export asks for it.
func ComposeFrameMotionBlur(p *Project, a *Assets, in FrameInputs, outW, outH uint32) *image.NRGBA {
	return compose(p, a, in, outW, outH, p.Export.MotionBlur)
}

const maxSubframes = 8

func compose(p *Project, a *Assets, in FrameInputs, outW, outH uint32, blur bool) *image.NRGBA {
	motion := a.Motion(p)
	st := a.stageFor(p, in.Screen, in.Camera != nil, outW, outH)
	l := &st.layout
	srcW, srcH := uint32(in.Screen.Rect.Dx()), uint32(in.Screen.Rect.Dy())
	t := in.OutputMs
	xfAt := func(t float64) ScreenXf { return NewScreenXf(p, l, srcW, srcH, motion.Viewport(t)) }
	xf := xfAt(t)

	pm := canvasFrom(st.base)
	samples := []ScreenXf{xf}
	if blur {
		samples = shutterSamples(p, t, xfAt, l)
	}
	// Blending sample i at 1/(i+1) over the ones before it weights every sample equally.
	for i, s := range samples {
		drawScreen(pm, st.cardMask, l.Card, in.Screen, s, uint32(256/(i+1)))
	}

	if p.ClickHighlights {
		for _, c := range motion.Clicks.At(t, ClickBase(p, l)) {
			cx, cy := xf.Point(c.X, c.Y)
			drawClick(pm, cx, cy, c, st.cardMask)
		}
	}
	if p.Design.Cursor.Visible {
		if ptr, ok := motion.Pointer.At(t); ok {
			cx, cy := xf.Point(ptr.X, ptr.Y)
			sprite := a.sprite(ptr.Cursor)
			s := float32(CursorScale(l.FillH, sprite.Scale) * float64(p.Design.Cursor.Size) * ptr.PressScale)
			tf := render.Translate(cx, cy).
				PreConcat(render.Scale(s, s)).
				PreConcat(render.Translate(-float32(sprite.Hotspot[0]), -float32(sprite.Hotspot[1])))
			drawSprite(pm, sprite.Pixmap, tf, st.cardMask)
		}
	}
	if l.Bubble != nil && in.Camera != nil {
		drawCamera(pm, l.Bubble.Rect, l.Bubble.Radius, in.Camera)
	}
	var captionBar *render.R
	if c, ok := motion.Captions.At(t); ok && p.Captions.Enabled {
		bar := drawCaptions(pm, p.Captions, c)
		captionBar = &bar
	}
	if p.Keystrokes.Enabled {
		if k, ok := motion.Keys.At(t); ok {
			drawKeystroke(pm, l.Card, p.Keystrokes.Position, k, captionBar)
		}
	}
	return unpremultiplyOpaque(pm)
}

// drawScreen blends the screen frame, mapped by xf, into the card with the given weight out of
// 256. The bilinear filter and the blend are the Rust compositor's fixed-point math, so the
// screen matches it exactly.
func drawScreen(pm *image.RGBA, mask *image.Alpha, card render.R, src *image.RGBA, xf ScreenXf, weight uint32) {
	pw, ph := pm.Rect.Dx(), pm.Rect.Dy()
	x0 := index(floor32(card.X), pw)
	x1 := index(ceil32(card.Right()), pw)
	y0 := index(floor32(card.Y), ph)
	y1 := index(ceil32(card.Bottom()), ph)
	if x0 >= x1 || y0 >= y1 {
		return
	}
	sw, sh := src.Rect.Dx(), src.Rect.Dy()
	inv := 1 / xf.K
	type col struct {
		a, b int
		fx   uint32
	}
	cols := make([]col, x1-x0)
	for j := range cols {
		sx := clamp((float64(x0+j)+0.5-xf.Tx)*inv-0.5, 0, float64(sw-1))
		i := int(sx)
		cols[j] = col{i * 4, min(i+1, sw-1) * 4, uint32((sx - float64(i)) * 256)}
	}
	srcRow := func(y int) []uint8 {
		o := src.PixOffset(src.Rect.Min.X, src.Rect.Min.Y+y)
		return src.Pix[o : o+sw*4]
	}
	row := func(y int) {
		sy := clamp((float64(y)+0.5-xf.Ty)*inv-0.5, 0, float64(sh-1))
		iy := int(sy)
		fy := uint32((sy - float64(iy)) * 256)
		ify := 256 - fy
		top, bot := srcRow(iy), srcRow(min(iy+1, sh-1))
		mrow := mask.Pix[y*mask.Stride:]
		out := pm.Pix[y*pm.Stride:]
		for j, c := range cols {
			x := x0 + j
			m := uint32(mrow[x])
			if m == 0 {
				continue
			}
			ta, tb := top[c.a:c.a+4:c.a+4], top[c.b:c.b+4:c.b+4]
			ba, bb := bot[c.a:c.a+4:c.a+4], bot[c.b:c.b+4:c.b+4]
			fx, ifx := c.fx, 256-c.fx
			var v [4]uint32
			for k := range 4 {
				t := uint32(ta[k])*ifx + uint32(tb[k])*fx
				u := uint32(ba[k])*ifx + uint32(bb[k])*fx
				v[k] = (t*ify + u*fy + 1<<15) >> 16
			}
			d := out[x*4 : x*4+4 : x*4+4]
			if m == 255 && weight == 256 {
				// The blend below with alpha 256 is exactly v.
				d[0], d[1], d[2], d[3] = uint8(v[0]), uint8(v[1]), uint8(v[2]), uint8(v[3])
				continue
			}
			alpha := weight
			if m != 255 {
				alpha = (m*weight + 127) / 255
			}
			for k := range 4 {
				d[k] = uint8((uint32(d[k])*(256-alpha) + v[k]*alpha + 128) >> 8)
			}
		}
	}
	threads := min(runtime.GOMAXPROCS(0), 8)
	per := (y1 - y0 + threads - 1) / threads
	var wg sync.WaitGroup
	for start := y0; start < y1; start += per {
		end := min(start+per, y1)
		wg.Go(func() {
			for y := start; y < end; y++ {
				row(y)
			}
		})
	}
	wg.Wait()
}

// index is Rust's `v.max(0.0) as usize` capped at hi.
func index(v float32, hi int) int {
	if !(v > 0) {
		return 0
	}
	if v >= float32(hi) {
		return hi
	}
	return int(v)
}

func floor32(v float32) float32 { return float32(math.Floor(float64(v))) }
func ceil32(v float32) float32  { return float32(math.Ceil(float64(v))) }

// shutterSamples spreads the screen over the frame's shutter interval when the camera moves
// far enough within it to smear: one sample per two pixels the card's corners travel.
func shutterSamples(p *Project, t float64, xfAt func(float64) ScreenXf, l *Layout) []ScreenXf {
	shutter := 1000.0 / float64(p.Export.Fps.Value())
	corners := func(x ScreenXf) [4]float64 {
		return [4]float64{x.Tx, x.Ty, x.Tx + l.FillW*x.K, x.Ty + l.FillH*x.K}
	}
	a, b := corners(xfAt(t-shutter/2)), corners(xfAt(t+shutter/2))
	var travel float64
	for i := range a {
		travel = max(travel, math.Abs(a[i]-b[i]))
	}
	n := 1
	if travel > 1.5 {
		n = min(max(int(math.Ceil(travel/2)), 2), maxSubframes)
	}
	out := make([]ScreenXf, n)
	for i := range out {
		out[i] = xfAt(t - shutter/2 + shutter*(float64(i)+0.5)/float64(n))
	}
	return out
}

// alpha is a 0..1 opacity as a color byte.
func alpha(a float64) uint8 { return uint8(math.Round(clamp(a, 0, 1) * 255)) }

func drawClick(pm *image.RGBA, x, y float32, c ClickFrame, mask *image.Alpha) {
	paint := func(base render.Color, a float64) render.Paint { return render.Solid(base.WithAlpha(alpha(a))) }
	if c.ImpactAlpha > 0 {
		if path := render.CirclePath(x, y, float32(c.ImpactRadius)); path != nil {
			halo := render.Stroke{Width: float32(max(c.ImpactRadius*0.25, 1))}
			render.StrokePath(pm, path, paint(render.Black, c.ImpactAlpha*0.4), halo, mask)
			render.FillPath(pm, path, paint(render.White, c.ImpactAlpha), mask)
		}
	}
	if c.RingAlpha > 0 {
		if path := render.CirclePath(x, y, float32(c.RingRadius)); path != nil {
			w := float32(c.RingWidth)
			render.StrokePath(pm, path, paint(render.Black, c.RingAlpha*0.3), render.Stroke{Width: w * 2.2}, mask)
			render.StrokePath(pm, path, paint(render.White, c.RingAlpha), render.Stroke{Width: w}, mask)
		}
	}
}

func fillRounded(pm *image.RGBA, r render.R, radius float32, c render.Color) {
	if path := render.RoundedRect(r, radius); path != nil {
		render.FillPath(pm, path, render.Solid(c), nil)
	}
}

func drawKeystroke(pm *image.RGBA, card render.R, pos KeystrokePosition, k KeyFrame, captionBar *render.R) {
	box := LayoutKeystroke(card, pos, k, captionBar, render.MeasureText)
	fillRounded(pm, box.Rect, box.Radius, render.Black.WithAlpha(alpha(box.BackAlpha)))
	if k.Modifiers != "" {
		render.DrawText(pm, k.Modifiers, box.ModifiersX, box.TextY, box.Font, render.White.WithAlpha(alpha(box.ModsAlpha)))
	}
	render.DrawText(pm, k.Key, box.KeyX, box.TextY, box.Font, render.White.WithAlpha(alpha(box.KeyAlpha)))
}

// drawCamera fills the bubble with the camera frame scaled to cover it.
func drawCamera(pm *image.RGBA, b render.R, radius float32, cam *image.RGBA) {
	path := render.RoundedRect(b, radius)
	if path == nil {
		return
	}
	cw, ch := float32(cam.Rect.Dx()), float32(cam.Rect.Dy())
	s := max(b.W/cw, b.H/ch)
	tf := render.TransformFromRow(s, 0, 0, s, b.X+(b.W-cw*s)/2, b.Y+(b.H-ch*s)/2)
	shader := &render.Pattern{Image: cam, Spread: render.SpreadPad, Quality: render.FilterBilinear, Transform: tf}
	render.FillPath(pm, path, render.Paint{Shader: shader}, nil)
}

func drawCaptions(pm *image.RGBA, s CaptionSettings, c CaptionFrame) render.R {
	w, h := float32(pm.Rect.Dx()), float32(pm.Rect.Dy())
	bar := LayoutCaptions(w, h, s, c, render.MeasureText)
	fillRounded(pm, bar.Rect, bar.Radius, render.Black.WithAlpha(alpha(bar.BackAlpha)))
	for _, word := range bar.Words {
		color := render.White
		switch word.Tone {
		case ToneActive:
			color = KaraokeColor
		case TonePending:
			color = render.White.WithAlpha(alpha(0.55))
		}
		render.DrawText(pm, word.Text, word.X, word.Y, bar.Font, color)
	}
	return bar.Rect
}

// drawSprite draws src scaled and moved by tf, a transform without rotation, with bicubic
// filtering. Like tiny-skia's draw_pixmap it covers whole pixels, those whose centers fall
// inside the scaled sprite, so the sprite's edge is not anti-aliased a second time.
func drawSprite(pm, src *image.RGBA, tf render.Transform, mask *image.Alpha) {
	b := render.RectPath(render.R{W: float32(src.Rect.Dx()), H: float32(src.Rect.Dy())}).Transformed(tf).Bounds()
	x0, y0 := ceil32(b.X-0.5), ceil32(b.Y-0.5)
	x1, y1 := ceil32(b.Right()-0.5), ceil32(b.Bottom()-0.5)
	if x1 <= x0 || y1 <= y0 {
		return
	}
	shader := &render.Pattern{Image: src, Spread: render.SpreadPad, Quality: render.FilterBicubic, Transform: tf}
	render.FillRect(pm, render.R{X: x0, Y: y0, W: x1 - x0, H: y1 - y0}, render.Paint{Shader: shader}, mask)
}

func (a *Assets) sprite(id uint32) CursorImage {
	if c, ok := a.cursors[id]; ok {
		return c
	}
	return fallbackCursor()
}

// fallbackCursor is an arrow for pointer samples whose shape was not captured.
var fallbackCursor = sync.OnceValue(func() CursorImage {
	pm := image.NewRGBA(image.Rect(0, 0, 26, 38))
	path := render.PolygonPath([]render.Pt{{X: 2, Y: 2}, {X: 2, Y: 29}, {X: 9, Y: 23}, {X: 14, Y: 34}, {X: 19, Y: 32}, {X: 14, Y: 21}, {X: 23, Y: 21}})
	render.FillPath(pm, path, render.Solid(render.Black), nil)
	render.StrokePath(pm, path, render.Solid(render.White), render.Stroke{Width: 1.8}, nil)
	return CursorImage{Pixmap: pm, Hotspot: [2]uint32{2, 2}, Scale: 1}
})

// canvases holds the pixel buffers of frames handed back through RecycleFrame. Export
// composes a frame every 16 ms; reusing their 8 MB buffers keeps that from churning the GC.
var canvases struct {
	sync.Mutex
	free [][]uint8
}

const maxPooled = 8

// RecycleFrame lets a later frame reuse f's pixels. f must not be used afterwards.
func RecycleFrame(f *image.NRGBA) {
	canvases.Lock()
	defer canvases.Unlock()
	if len(canvases.free) < maxPooled {
		canvases.free = append(canvases.free, f.Pix[:0])
	}
}

func canvasFrom(base *image.RGBA) *image.RGBA {
	canvases.Lock()
	var pix []uint8
	if n := len(canvases.free); n > 0 {
		pix = canvases.free[n-1]
		canvases.free = canvases.free[:n-1]
	}
	canvases.Unlock()
	return &image.RGBA{Pix: append(pix, base.Pix...), Stride: base.Stride, Rect: base.Rect}
}

// unpremultiplyOpaque converts pm to straight alpha in place. Frames are opaque wherever the
// background is, so only the rare translucent pixel pays for the division.
func unpremultiplyOpaque(pm *image.RGBA) *image.NRGBA {
	px := pm.Pix
	for i := 0; i < len(px); i += 4 {
		a := uint32(px[i+3])
		if a != 255 && a != 0 {
			for k := range 3 {
				px[i+k] = uint8(min((uint32(px[i+k])*255+a/2)/a, 255))
			}
		}
	}
	return &image.NRGBA{Pix: px, Stride: pm.Stride, Rect: pm.Rect}
}
