package media

import (
	"bytes"
	"image"
	"math"
	"testing"
)

func rgba(w, h int, px ...byte) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	copy(img.Pix, px)
	return img
}

func TestNV12EncodesBT709LimitedRange(t *testing.T) {
	out := make([]byte, 6)
	whiteBlack := []byte{255, 255, 255, 255, 0, 0, 0, 255}
	toNV12(append(whiteBlack, whiteBlack...), 8, 2, 2, orderRGBA, out)
	if want := []byte{235, 16, 235, 16, 128, 128}; !bytes.Equal(out, want) {
		t.Fatalf("white and black: got %v, want %v", out, want)
	}
	red := bytes.Repeat([]byte{255, 0, 0, 255}, 4)
	toNV12(red, 8, 2, 2, orderRGBA, out)
	if want := []byte{63, 63, 63, 63, 102, 240}; !bytes.Equal(out, want) {
		t.Fatalf("red: got %v, want %v", out, want)
	}
	toNV12(bytes.Repeat([]byte{0, 0, 255, 255}, 4), 8, 2, 2, orderBGRA, out)
	if want := []byte{63, 63, 63, 63, 102, 240}; !bytes.Equal(out, want) {
		t.Fatalf("BGRA red: got %v, want %v", out, want)
	}
}

func TestNV12DecodesBT709LimitedRange(t *testing.T) {
	f := &Nv12Frame{Width: 2, Height: 2, Stride: 2, Data: []byte{235, 16, 235, 16, 128, 128}, matrix: bt709Limited}
	got := f.ToRGBA(nil).Pix
	if want := []byte{255, 255, 255, 255, 0, 0, 0, 255, 255, 255, 255, 255, 0, 0, 0, 255}; !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	f.Data = []byte{63, 63, 63, 63, 102, 240}
	got = f.ToRGBA(nil).Pix
	if got[0] <= 250 || got[1] >= 8 || got[2] >= 8 || !bytes.Equal(got[:4], got[12:]) {
		t.Fatalf("red decoded to %v", got)
	}
}

func TestFullRangeMatricesMapExtremes(t *testing.T) {
	for _, m := range []yuvMatrix{bt709Full, bt601Full} {
		f := &Nv12Frame{Width: 2, Height: 2, Stride: 2, Data: []byte{255, 0, 255, 0, 128, 128}, matrix: m}
		got := f.ToRGBA(nil).Pix
		if want := []byte{255, 255, 255, 255, 0, 0, 0, 255, 255, 255, 255, 255, 0, 0, 0, 255}; !bytes.Equal(got, want) {
			t.Fatalf("full range %+v: got %v, want %v", m, got, want)
		}
	}
}

func TestPickMatrixFollowsTagsThenSize(t *testing.T) {
	cases := []struct {
		bt709, bt601, full bool
		height             int
		want               yuvMatrix
	}{
		{false, false, false, 480, bt601Limited},
		{false, false, false, 720, bt709Limited},
		{true, false, false, 480, bt709Limited},
		{false, true, false, 1080, bt601Limited},
		{true, false, true, 1080, bt709Full},
		{false, false, true, 480, bt601Full},
	}
	for _, c := range cases {
		if got := pickMatrix(c.bt709, c.bt601, c.full, c.height); got != c.want {
			t.Errorf("pickMatrix(%v, %v, %v, %d) = %+v, want %+v", c.bt709, c.bt601, c.full, c.height, got, c.want)
		}
	}
}

// A smooth image survives RGBA -> NV12 -> RGBA nearly intact; the parallel bands must meet
// without seams, so the height is not a multiple of any core count.
func TestNV12RoundTripIsCloseOnSmoothContent(t *testing.T) {
	const w, h = 642, 362
	src := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			p := src.Pix[y*src.Stride+x*4:]
			p[0] = uint8(128 + 100*math.Sin(float64(x)/40))
			p[1] = uint8(128 + 100*math.Sin(float64(y)/30))
			p[2] = uint8(128 + 100*math.Sin(float64(x+y)/50))
			p[3] = 255
		}
	}
	nv12 := make([]byte, w*h*3/2)
	toNV12(src.Pix, src.Stride, w, h, orderRGBA, nv12)
	back := (&Nv12Frame{Width: w, Height: h, Stride: w, Data: nv12, matrix: bt709Limited}).ToRGBA(nil)
	if p := psnr(src, back); p < 40 {
		t.Fatalf("round trip PSNR %.1f dB, want at least 40", p)
	}
}

func psnr(a, b *image.RGBA) float64 {
	var sum float64
	n := 0
	for y := 0; y < a.Rect.Dy(); y++ {
		ra := a.Pix[y*a.Stride : y*a.Stride+a.Rect.Dx()*4]
		rb := b.Pix[y*b.Stride : y*b.Stride+b.Rect.Dx()*4]
		for i := 0; i < len(ra); i += 4 {
			for c := 0; c < 3; c++ {
				d := float64(ra[i+c]) - float64(rb[i+c])
				sum += d * d
			}
			n += 3
		}
	}
	if sum == 0 {
		return math.Inf(1)
	}
	return 10 * math.Log10(255*255/(sum/float64(n)))
}

func TestShrinkAveragesEachOutputArea(t *testing.T) {
	src := rgba(4, 2,
		0, 0, 0, 255, 255, 255, 255, 255, 255, 0, 0, 255, 255, 0, 0, 255,
		255, 255, 255, 255, 0, 0, 0, 255, 255, 0, 0, 255, 255, 0, 0, 255)
	if got, want := Shrink(src, 2, 1).Pix, []byte{128, 128, 128, 255, 255, 0, 0, 255}; !bytes.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	flat := rgba(5, 3, bytes.Repeat([]byte{10, 20, 30, 255}, 15)...)
	for i, v := range Shrink(flat, 2, 2).Pix {
		if want := []byte{10, 20, 30, 255}[i%4]; v != want {
			t.Fatalf("odd shrink byte %d is %d, want %d", i, v, want)
		}
	}
}

func BenchmarkRGBAToNV12_4K(b *testing.B) {
	const w, h = 3840, 2160
	px := make([]byte, w*h*4)
	for i := range px {
		px[i] = byte(i * 7)
	}
	dst := make([]byte, w*h*3/2)
	b.SetBytes(int64(len(px)))
	for b.Loop() {
		toNV12(px, w*4, w, h, orderRGBA, dst)
	}
}

func BenchmarkNV12ToRGBA_4K(b *testing.B) {
	const w, h = 3840, 2160
	f := &Nv12Frame{Width: w, Height: h, Stride: w, Data: make([]byte, w*h*3/2), matrix: bt709Limited}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	b.SetBytes(int64(w * h * 4))
	for b.Loop() {
		f.ToRGBA(dst)
	}
}
