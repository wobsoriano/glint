package render

import (
	"image"
	"math"
	"testing"
)

// syntheticScreenshot is the Rust golden example's base_image, so both sides time the same
// pixels.
func syntheticScreenshot(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			fx, fy := float32(x)/float32(w), float32(y)/float32(h)
			var c [4]uint8
			switch line := (y - h/8) % 22; {
			case y < h/8:
				c = [4]uint8{40, 44, 52, 255}
			case x < w/5:
				if (y/18)%2 == 0 {
					c = [4]uint8{236, 238, 242, 255}
				} else {
					c = [4]uint8{226, 229, 235, 255}
				}
			case fx > 0.55 && fy > 0.45:
				c = [4]uint8{uint8(255 * fx), uint8(180*(1-fy) + 40), uint8((x*7 + y*3) % 256), 255}
			case line < 9 && x > w/4 && x < w/4+40+(y*37%160):
				c = [4]uint8{70, 74, 82, 255}
			case ((x/12)+(y/12))%2 == 0 && fx > 0.8 && fy < 0.4:
				c = [4]uint8{255, 204, 0, 255}
			default:
				c = [4]uint8{250, 250, 252, 255}
			}
			copy(img.Pix[img.PixOffset(x, y):], c[:])
		}
	}
	return img
}

// benchDoc is the Rust golden example's bench_doc: a gradient, a shadow and 20 annotations.
func benchDoc() EditDoc {
	d := NewEditDoc(DefaultStyle(), 1, nil)
	colors := []uint32{0xFF3B30, 0x007AFF, 0x34C759, 0xFFCC00, 0xAF52DE}
	for i := range 20 {
		x := 100 + float32(i%5)*450
		y := 100 + float32(i/5)*320
		var s Shape
		switch i % 10 {
		case 0:
			s = RectShape{R{x, y, 300, 180}, false}
		case 1:
			s = Arrow{P(x, y+200), P(x+300, y)}
		case 2:
			s = Ellipse{R{x, y, 260, 160}, false}
		case 3:
			s = Line{P(x, y), P(x+320, y+150)}
		case 4:
			pts := make([]Pt, 120)
			for k := range pts {
				pts[k] = P(x+float32(k)*2.5, y+80+float32(math.Sin(float64(float32(k)*0.2)))*60)
			}
			s = Pen{pts}
		case 5:
			s = Counter{P(x+40, y+40), uint32(i), 24}
		case 6:
			s = Text{P(x, y), "Release notes", 40, TextPill, DefaultTextFont()}
		case 7:
			s = Redact{R{x, y, 300, 120}, RedactPixelate, 12}
		case 8:
			s = Redact{R{x, y, 300, 120}, RedactBlur, 12}
		default:
			s = RectShape{R{x, y, 200, 100}, true}
		}
		d.Annotations = append(d.Annotations, Annotation{uint64(i), s, RGB(colors[i%5]), 5})
	}
	return d
}

// BenchmarkRender2560 renders a 2560x1440 screenshot with a gradient background, a shadow and
// 20 annotations from a cold Compositor, as an export does.
func BenchmarkRender2560(b *testing.B) {
	base := syntheticScreenshot(2560, 1440)
	doc := benchDoc()
	DefaultFont()
	b.ResetTimer()
	for b.Loop() {
		var c Compositor
		out := c.Render(doc, Sources{Base: base}, 1, nil)
		if out.Rect.Dx() != 2560+128 {
			b.Fatal(out.Rect)
		}
	}
}
