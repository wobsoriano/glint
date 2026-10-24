package studio

import (
	"bufio"
	"compress/lzw"
	"encoding/binary"
	"image"
	"io"
	"math/bits"
)

// palette is one 256-colour table for a whole GIF, trained on sample frames, with a lookup
// from 15-bit colour to its nearest entry so mapping a frame is one table read per pixel.
type palette struct {
	rgb []byte
	lut []uint8
}

var bayer4 = [4][4]int32{{0, 8, 2, 10}, {12, 4, 14, 6}, {3, 11, 1, 9}, {15, 7, 13, 5}}

func trainPalette(frames []*image.NRGBA) *palette {
	var pixels []byte
	for _, f := range frames {
		pixels = append(pixels, f.Pix...)
	}
	nq := newNeuQuant(10, 256, pixels)
	lut := make([]uint8, 32768)
	for c := range uint32(32768) {
		ch := func(shift uint32) uint8 { return uint8((c>>shift)&31<<3 | 4) }
		lut[c] = uint8(nq.indexOf(ch(10), ch(5), ch(0), 255))
	}
	return &palette{rgb: nq.colorMapRGB(), lut: lut}
}

// mapFrame turns a frame into palette indices with a 4x4 ordered dither, which keeps
// gradients from banding without the frame-to-frame shimmer of error diffusion.
func (p *palette) mapFrame(f *image.NRGBA, dst []uint8) []uint8 {
	w, h := f.Rect.Dx(), f.Rect.Dy()
	dst = append(dst[:0], make([]uint8, w*h)...)
	q := func(v uint8, d int32) int { return int(min(max(int32(v)+d, 0), 255) >> 3) }
	for y := range h {
		row := f.Pix[y*f.Stride : y*f.Stride+w*4]
		out := dst[y*w : y*w+w]
		for x := range w {
			d := bayer4[y%4][x%4] - 8
			px := row[x*4 : x*4+3 : x*4+3]
			out[x] = p.lut[q(px[0], d)<<10|q(px[1], d)<<5|q(px[2], d)]
		}
	}
	return dst
}

// gifWriter streams a looping GIF89a with one global palette, the layout the gif crate
// writes: every frame full size, kept on screen until the next, no transparency.
type gifWriter struct {
	w   *bufio.Writer
	buf []byte
}

func newGIFWriter(w io.Writer, width, height int, rgb []byte) (*gifWriter, error) {
	g := &gifWriter{w: bufio.NewWriter(w)}
	colors := len(rgb) / 3
	size := flagSize(colors)
	var hdr []byte
	hdr = append(hdr, "GIF89a"...)
	hdr = binary.LittleEndian.AppendUint16(hdr, uint16(width))
	hdr = binary.LittleEndian.AppendUint16(hdr, uint16(height))
	hdr = append(hdr, 0x80|size<<4|size, 0, 0)
	hdr = append(hdr, rgb[:colors*3]...)
	hdr = append(hdr, make([]byte, ((2<<size)-colors)*3)...)
	hdr = append(hdr, 0x21, 0xFF, 11)
	hdr = append(hdr, "NETSCAPE2.0"...)
	hdr = append(hdr, 3, 1, 0, 0, 0)
	_, err := g.w.Write(hdr)
	return g, err
}

// flagSize is the colour-table size field: the table holds 2 << flagSize entries.
func flagSize(colors int) byte {
	n := uint(min(max(colors, 2), 255))
	return byte(bits.Len(n-1) - 1)
}

// writeFrame appends a frame of palette indices shown for delay hundredths of a second.
func (g *gifWriter) writeFrame(indices []uint8, width, height int, delay uint16) error {
	var hdr []byte
	hdr = append(hdr, 0x21, 0xF9, 4, 1<<2)
	hdr = binary.LittleEndian.AppendUint16(hdr, delay)
	hdr = append(hdr, 0, 0, 0x2C, 0, 0, 0, 0)
	hdr = binary.LittleEndian.AppendUint16(hdr, uint16(width))
	hdr = binary.LittleEndian.AppendUint16(hdr, uint16(height))
	hdr = append(hdr, 0)
	var maxIndex uint8
	for _, v := range indices {
		maxIndex = max(maxIndex, v)
		if maxIndex > 127 {
			break
		}
	}
	litWidth := bits.Len(uint(max(int(maxIndex)+1, 4)) - 1)
	hdr = append(hdr, byte(litWidth))
	if _, err := g.w.Write(hdr); err != nil {
		return err
	}
	g.buf = g.buf[:0]
	enc := lzw.NewWriter(sliceWriter{&g.buf}, lzw.LSB, litWidth)
	if _, err := enc.Write(indices); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	for data := g.buf; len(data) > 0; {
		n := min(len(data), 0xFF)
		if err := g.w.WriteByte(byte(n)); err != nil {
			return err
		}
		if _, err := g.w.Write(data[:n]); err != nil {
			return err
		}
		data = data[n:]
	}
	return g.w.WriteByte(0)
}

func (g *gifWriter) finish() error {
	if err := g.w.WriteByte(0x3B); err != nil {
		return err
	}
	return g.w.Flush()
}

type sliceWriter struct{ b *[]byte }

func (s sliceWriter) Write(p []byte) (int, error) {
	*s.b = append(*s.b, p...)
	return len(p), nil
}
