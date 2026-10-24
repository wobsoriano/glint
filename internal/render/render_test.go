package render

import (
	"image"
	"math"
	"reflect"
	"slices"
	"testing"
)

func testBase() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, 320, 200))
	for y := 0; y < 200; y++ {
		for x := 0; x < 320; x++ {
			copy(img.Pix[img.PixOffset(x, y):], []uint8{uint8(x % 256), uint8(y % 256), uint8((x * y) % 251), 255})
		}
	}
	return img
}

func richDoc() EditDoc {
	title := "Window"
	st := DefaultStyle()
	st.Chrome = ChromeWinDark
	doc := NewEditDoc(st, 1, &title)
	doc.Effects.Border = Border{Enabled: true, Width: 5, Color: RGB(0xFF00FF)}
	doc.Effects.Watermark = DefaultWatermark()
	doc.Effects.Watermark.Enabled, doc.Effects.Watermark.Text = true, "Glint"
	mk := func(s Shape) Annotation { return Annotation{Shape: s, Color: RGB(0xFF3B30), Width: 4} }
	doc.Annotations = []Annotation{
		mk(Redact{R{10, 10, 90, 40}, RedactPixelate, 9}),
		mk(Redact{R{120, 10, 90, 40}, RedactBlur, 9}),
		mk(Arrow{P(20, 180), P(200, 90)}),
		mk(Highlight{R{200, 100, 80, 60}}),
		mk(Text{P(30, 60), "Hello", 24, TextOutline, DefaultTextFont()}),
	}
	return doc
}

func maxChannelDiff(a, b *image.RGBA) int {
	d := 0
	for i := range a.Pix {
		d = max(d, abs(int(a.Pix[i])-int(b.Pix[i])))
	}
	return d
}

func TestRegionRenderMatchesTheSamePixelsOfAFullRender(t *testing.T) {
	base := testBase()
	for _, tc := range []struct {
		name  string
		edit  func(*EditDoc)
		limit int
	}{
		{"flat", func(*EditDoc) {}, 1},
		{"perspective and blur", func(d *EditDoc) {
			d.Effects.Perspective.RotY, d.Effects.Perspective.RotX = 25, 10
			d.Effects.Blur = DefaultProgressiveBlur()
			d.Effects.Blur.Enabled = true
		}, 2},
	} {
		doc := richDoc()
		tc.edit(&doc)
		var full, part Compositor
		whole := full.Render(doc, Sources{Base: base}, 1, nil)
		region := R{57, 41, 150, 120}
		got := part.Render(doc, Sources{Base: base}, 1, &region)
		if got.Rect.Dx() != 150 || got.Rect.Dy() != 120 {
			t.Fatalf("%s: region size %v", tc.name, got.Rect)
		}
		want := subImage(whole, image.Rect(57, 41, 207, 161))
		if d := maxChannelDiff(got, want); d > tc.limit {
			t.Errorf("%s: max channel difference %d", tc.name, d)
		}
	}
}

func TestCachedRenderEqualsFreshRender(t *testing.T) {
	base := testBase()
	doc := richDoc()
	var c Compositor
	c.Render(doc, Sources{Base: base}, 0.5, nil)
	doc.Annotations = doc.Annotations[:len(doc.Annotations)-1]
	cached := c.Render(doc, Sources{Base: base}, 0.5, nil)
	var fresh Compositor
	if d := maxChannelDiff(cached, fresh.Render(doc, Sources{Base: base}, 0.5, nil)); d != 0 {
		t.Errorf("cached render differs by %d", d)
	}
}

func TestBorderSurroundsTheScreenshot(t *testing.T) {
	st := DefaultStyle()
	st.Padding, st.Shadow, st.Radius = 20, 0, 0
	doc := NewEditDoc(st, 1, nil)
	doc.Effects.Border = Border{Enabled: true, Width: 6, Color: RGB(0x00FF00)}
	out := Unpremultiply(RenderDoc(testBase(), doc))
	if out.Rect.Dx() != 320+12+40 || out.Rect.Dy() != 200+12+40 {
		t.Fatalf("size %v", out.Rect)
	}
	px := func(x, y int) []uint8 { return out.Pix[out.PixOffset(x, y) : out.PixOffset(x, y)+4] }
	if !slices.Equal(px(23, 100), []uint8{0, 255, 0, 255}) {
		t.Errorf("left border is %v", px(23, 100))
	}
	if !slices.Equal(px(26, 26), []uint8{0, 0, 0, 255}) {
		t.Errorf("screenshot starts after the border, got %v", px(26, 26))
	}
}

func TestCropRemapsAnnotationsOntoTheSameBasePixels(t *testing.T) {
	doc := DefaultEditDoc()
	doc.Annotations = []Annotation{{ID: 1, Shape: RectShape{R{100, 50, 20, 10}, false}, Color: Black, Width: 2}}
	doc.SetCrop(R{40, 30, 200, 100}, 400, 300)
	if doc.Crop == nil || *doc.Crop != (R{40, 30, 200, 100}) {
		t.Fatalf("crop %v", doc.Crop)
	}
	if got := doc.Annotations[0].Shape; got != (RectShape{R{60, 20, 20, 10}, false}) {
		t.Errorf("after first crop: %v", got)
	}
	doc.SetCrop(R{50, 35, 100, 100}, 400, 300)
	if got := doc.Annotations[0].Shape; got != (RectShape{R{50, 15, 20, 10}, false}) {
		t.Errorf("after second crop: %v", got)
	}
	doc.SetCrop(R{0, 0, 400, 300}, 400, 300)
	if doc.Crop != nil {
		t.Errorf("the full image is stored as no crop, got %v", doc.Crop)
	}
	if got := doc.Annotations[0].Shape; got != (RectShape{R{100, 50, 20, 10}, false}) {
		t.Errorf("after uncrop: %v", got)
	}
}

func TestRatioCropIsCenteredAndMaximal(t *testing.T) {
	for _, tc := range []struct {
		within R
		ratio  float32
		want   R
	}{
		{R{0, 0, 1600, 900}, 1, R{350, 0, 900, 900}},
		{R{0, 0, 1600, 900}, 9.0 / 16.0, R{547, 0, 506, 900}},
		{R{0, 0, 1000, 1000}, 16.0 / 9.0, R{0, 219, 1000, 563}},
	} {
		if got := RatioCrop(tc.within, tc.ratio); got != tc.want {
			t.Errorf("RatioCrop(%v, %v) = %v, want %v", tc.within, tc.ratio, got, tc.want)
		}
	}
}

func close2(a, b Pt) bool { return abs32(a.X-b.X) < 1e-2 && abs32(a.Y-b.Y) < 1e-2 }

func TestRectToQuadMapsCornersExactlyAndInverts(t *testing.T) {
	src := R{10, 20, 300, 200}
	quad := [4]Pt{{40, 30}, {290, 60}, {260, 250}, {20, 210}}
	h, ok := RectToQuad(src, quad)
	if !ok {
		t.Fatal("no homography")
	}
	inv, _ := h.Inverse()
	for i, c := range [4]Pt{{10, 20}, {310, 20}, {310, 220}, {10, 220}} {
		if !close2(h.Apply(c), quad[i]) || !close2(inv.Apply(quad[i]), c) {
			t.Errorf("corner %d: %v -> %v, want %v", i, c, h.Apply(c), quad[i])
		}
	}
	if close2(h.Apply(src.Center()), P(152.5, 137.5)) {
		t.Error("projective, not affine: the center must move")
	}
}

func TestPerspectiveProjectsCardCornersLikeAPinholeCamera(t *testing.T) {
	card := R{100, 100, 400, 300}
	s := DefaultPerspective()
	s.RotY = 30
	h, ok := Projection(card, 600, 500, s)
	if !ok {
		t.Fatal("no projection")
	}
	tl, tr, br, bl := h.Apply(P(100, 100)), h.Apply(P(500, 100)), h.Apply(P(500, 400)), h.Apply(P(100, 400))
	if left, right := bl.Y-tl.Y, br.Y-tr.Y; !(left > 300 && right < 300) {
		t.Errorf("left %v right %v", left, right)
	}
	if !close2(h.Apply(P(300, 250)), P(300, 250)) {
		t.Error("the pivot is fixed")
	}
	focal := 400 * (0.35 + 0.5/math.Tan(12*math.Pi/180))
	z := 200 * math.Sin(30*math.Pi/180)
	x := 200 * math.Cos(30*math.Pi/180)
	want := P(300+float32(x*focal/(focal+z)), 250-float32(150*focal/(focal+z)))
	if !close2(tr, want) {
		t.Errorf("top right %v, want %v", tr, want)
	}
	if _, ok := Projection(card, 600, 500, DefaultPerspective()); ok {
		t.Error("an inactive perspective has no projection")
	}
}

func TestWarpIdentityReproducesSource(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 8, 8))
	Fill(src, RGBA8(200, 10, 10, 255))
	dst := image.NewRGBA(image.Rect(0, 0, 12, 12))
	WarpOnto(dst, P(-2, -2), src, P(0, 0), IdentityHomography)
	at := func(x, y int) []uint8 { return dst.Pix[dst.PixOffset(x, y) : dst.PixOffset(x, y)+4] }
	if at(2, 2)[0] != 200 || at(9, 9)[0] != 200 || at(1, 1)[3] != 0 || at(10, 10)[3] != 0 {
		t.Errorf("%v %v %v %v", at(2, 2), at(9, 9), at(1, 1), at(10, 10))
	}
}

func TestBlurFieldIsSharpAtFocusAndBlurredAtTheEdges(t *testing.T) {
	extent := R{0, 0, 1000, 600}
	s := DefaultProgressiveBlur()
	s.Enabled, s.Start = true, 0.2
	radial := NewBlurField(extent, s)
	if radial.At(P(500, 300)) != 0 || radial.At(P(0, 0)) != 1 {
		t.Errorf("radial: %v %v", radial.At(P(500, 300)), radial.At(P(0, 0)))
	}
	s.Mode = BlurDirectional
	band := NewBlurField(extent, s)
	if band.At(P(0, 300)) != 0 || band.At(P(500, 0)) != 1 {
		t.Errorf("band: %v %v", band.At(P(0, 300)), band.At(P(500, 0)))
	}
}

func TestFontCatalogListsInstalledFamiliesAndFallsBack(t *testing.T) {
	if !slices.Contains(Families(), DefaultFamily) {
		t.Skipf("%s is not installed", DefaultFamily)
	}
	if got := Weights("Georgia"); !reflect.DeepEqual(got, []Weight{WeightRegular, WeightBold}) {
		t.Errorf("Georgia weights %v", got)
	}
	if FontFor("Georgia", WeightBlack) == nil || FontFor("No Such Font", WeightRegular) == nil {
		t.Error("nearest weight and default family fallbacks must load")
	}
	w1, _ := FontFor(DefaultFamily, WeightRegular).Measure("Hello", 40)
	w2, _ := FontFor(DefaultFamily, WeightBlack).Measure("Hello", 40)
	if w2 <= w1 {
		t.Errorf("black is wider than regular: %v vs %v", w2, w1)
	}
	for _, fam := range catalog {
		if !slices.Contains(Families(), fam.name) {
			continue
		}
		for _, fc := range fam.faces {
			if fc != nil && loadFace(*fc) == nil {
				t.Errorf("%s: %s does not load", fam.name, fc.file)
			}
		}
	}
}

func TestAnnotationEditing(t *testing.T) {
	a := Annotation{Shape: RectShape{R{10, 10, 100, 50}, false}, Width: 4}
	if !a.Hit(P(10, 30), 2) || a.Hit(P(60, 35), 2) {
		t.Error("an outlined rect is hit on its edge, not its middle")
	}
	a.SetHandle(0, P(0, 0))
	if got := a.Shape.(RectShape).Rect; got != (R{0, 0, 110, 60}) {
		t.Errorf("dragging the top-left handle keeps the opposite corner: %v", got)
	}
	pen := Annotation{Shape: Pen{[]Pt{{0, 0}, {10, 0}}}, Width: 2}
	clone := pen.Clone()
	pen.Translate(5, 5)
	if clone.Shape.(Pen).Points[0] != (Pt{0, 0}) || pen.Shape.(Pen).Points[0] != (Pt{5, 5}) {
		t.Error("translating a pen must not move its clone")
	}
	if tool, ok := ToolFromKey("x"); !ok || tool != ToolPixelate || tool.Icon() != "pixelate" {
		t.Errorf("x maps to %v", tool)
	}
}

func TestRenderBaseDrawsTheScreenshotInsideTheCard(t *testing.T) {
	base := testBase()
	st := DefaultStyle()
	l := LayoutFor(st, R{0, 0, 320, 200}, 1, 1)
	anns := []Annotation{{Shape: Arrow{P(20, 180), P(200, 90)}, Color: RGB(0xFF3B30), Width: 4}}
	pm := RenderBase(Inputs{Style: st, Layout: l, Image: PrepareImage(base, l.Crop, 320, 200)}, anns)
	before := slices.Clone(pm.Pix)
	RenderAnnotations(pm, l, anns)
	if pm.Rect.Dx() != l.Width || pm.Rect.Dy() != l.Height {
		t.Fatalf("size %v", pm.Rect)
	}
	x, y := int(l.Image.X)+160, int(l.Image.Y)+20
	if got, want := pm.Pix[pm.PixOffset(x, y):pm.PixOffset(x, y)+4], base.Pix[base.PixOffset(160, 20):base.PixOffset(160, 20)+4]; !slices.Equal(got, want) {
		t.Errorf("screenshot pixel %v, want %v", got, want)
	}
	if slices.Equal(before, pm.Pix) {
		t.Error("RenderAnnotations drew nothing")
	}
}
