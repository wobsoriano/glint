package render

import (
	"encoding/binary"
	"encoding/json"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

type Weight uint8

const (
	WeightLight Weight = iota
	WeightRegular
	WeightSemibold
	WeightBold
	WeightBlack
)

var weightNames = []string{"light", "regular", "semibold", "bold", "black"}

var AllWeights = [5]Weight{WeightLight, WeightRegular, WeightSemibold, WeightBold, WeightBlack}

func (w Weight) Label() string {
	return [...]string{"Light", "Regular", "Semibold", "Bold", "Black"}[w]
}

func (w Weight) MarshalText() ([]byte, error) { return enumMarshal(w, weightNames) }
func (w *Weight) UnmarshalText(b []byte) error {
	return enumUnmarshal(w, b, weightNames, "weight")
}

type Align uint8

const (
	AlignLeft Align = iota
	AlignCenter
	AlignRight
)

var alignNames = []string{"left", "center", "right"}

func (a Align) MarshalText() ([]byte, error) { return enumMarshal(a, alignNames) }
func (a *Align) UnmarshalText(b []byte) error {
	return enumUnmarshal(a, b, alignNames, "align")
}

// TextFont is a family, weight and line alignment. Missing JSON fields take the defaults.
type TextFont struct {
	Family string `json:"family"`
	Weight Weight `json:"weight"`
	Align  Align  `json:"align"`
}

func DefaultTextFont() TextFont {
	return TextFont{Family: DefaultFamily, Weight: WeightSemibold, Align: AlignLeft}
}

func (t *TextFont) UnmarshalJSON(data []byte) error {
	type plain TextFont
	v := plain(DefaultTextFont())
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*t = TextFont(v)
	return nil
}

// LineHeight is the line advance as a multiple of the text size.
const LineHeight float32 = 1.25

// face is one face of a family: a font file, the face's index in a collection, and for
// variable fonts the wght axis value.
type face struct {
	file  string
	index int
	wght  float32
}

type family struct {
	name  string
	faces [5]*face // Light, Regular, Semibold, Bold, Black; Regular is required
}

func single(file string) *face                  { return &face{file: file} }
func inCollection(file string, index int) *face { return &face{file: file, index: index} }
func instance(file string, wght float32) *face  { return &face{file: file, wght: wght} }

var installed = sync.OnceValue(func() []string {
	dir := fontsDir()
	var out []string
	for _, fam := range catalog {
		if r := fam.faces[WeightRegular]; r != nil {
			if _, err := os.Stat(filepath.Join(dir, r.file)); err == nil {
				out = append(out, fam.name)
			}
		}
	}
	return out
})

// Families lists the catalog families whose regular face is installed.
func Families() []string { return installed() }

// Weights lists the weights with a real face for family.
func Weights(familyName string) []Weight {
	for _, fam := range catalog {
		if fam.name == familyName {
			var out []Weight
			for _, w := range AllWeights {
				if fam.faces[w] != nil {
					out = append(out, w)
				}
			}
			return out
		}
	}
	return []Weight{WeightRegular}
}

// Font is a loaded face. A nil *Font measures with a fixed advance and draws nothing.
type Font struct {
	sf      *sfnt.Font
	upem    float32
	ascent  float32 // font units, ttf-parser's rules
	descent float32
	kern    map[uint32]int16
	glyphs  sync.Map // sfnt.GlyphIndex -> *glyph
	indices sync.Map // rune -> sfnt.GlyphIndex
	bufs    sync.Pool
}

type glyph struct {
	advance float32 // font units
	segs    []glyphSeg
	bounds  [4]float32 // min x, min y, max x, max y in font units, y down
	ok      bool
}

type glyphSeg struct {
	op  sfnt.SegmentOp
	pts [3]Pt
}

type fontKey struct {
	family string
	weight Weight
}

var (
	fontMu    sync.Mutex
	fontCache = map[fontKey]*Font{}
	fileCache = map[face]*Font{}
)

// FontFor is the face for family at the nearest available weight, falling back to the
// platform's default family, then Arial. It is nil when nothing loads.
func FontFor(familyName string, w Weight) *Font {
	key := fontKey{familyName, w}
	fontMu.Lock()
	defer fontMu.Unlock()
	if f, ok := fontCache[key]; ok {
		return f
	}
	var loaded *Font
	for _, name := range []string{familyName, DefaultFamily, "Arial"} {
		var fam *family
		for i := range catalog {
			if catalog[i].name == name {
				fam = &catalog[i]
			}
		}
		if fam == nil {
			continue
		}
		var order []int
		for i := range 5 {
			if fam.faces[i] != nil {
				order = append(order, i)
			}
		}
		want := int(w)
		for i := 1; i < len(order); i++ {
			for j := i; j > 0 && weightCloser(order[j], order[j-1], want); j-- {
				order[j], order[j-1] = order[j-1], order[j]
			}
		}
		for _, i := range order {
			if loaded = loadFace(*fam.faces[i]); loaded != nil {
				break
			}
		}
		if loaded != nil {
			break
		}
	}
	fontCache[key] = loaded
	return loaded
}

// weightCloser orders faces by distance to want, the heavier face first on ties.
func weightCloser(a, b, want int) bool {
	da, db := absInt(a-want), absInt(b-want)
	if da != db {
		return da < db
	}
	return a > b
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// DefaultFont is the default family at its default weight.
func DefaultFont() *Font {
	t := DefaultTextFont()
	return FontFor(t.Family, t.Weight)
}

func loadFace(fc face) *Font {
	if f, ok := fileCache[fc]; ok {
		return f
	}
	data, err := os.ReadFile(filepath.Join(fontsDir(), fc.file))
	if err != nil {
		return nil
	}
	f := parseFont(data, fc.index)
	fileCache[fc] = f
	return f
}

func parseFont(data []byte, index int) *Font {
	var sf *sfnt.Font
	var err error
	parse := func(data []byte) (*sfnt.Font, error) {
		if strings.HasPrefix(string(data[:min(4, len(data))]), "ttcf") {
			col, err := sfnt.ParseCollection(data)
			if err != nil {
				return nil, err
			}
			return col.Font(index)
		}
		return sfnt.Parse(data)
	}
	if sf, err = parse(data); err != nil && strings.Contains(err.Error(), "kern") {
		// sfnt rejects kern subtables whose 16-bit length overflowed (Calibri, Cambria, Corbel
		// have over 10k pairs). ttf-parser reads them; parseKern below does too, so hide the
		// table from sfnt only.
		sf, err = parse(withoutTable(data, index, "kern"))
	}
	if err != nil {
		return nil
	}
	tables := tableDirectory(data, index)
	f := &Font{sf: sf, upem: float32(sf.UnitsPerEm())}
	f.ascent, f.descent = verticalMetrics(tables)
	f.kern = parseKern(tables["kern"])
	f.bufs.New = func() any { return &sfnt.Buffer{} }
	return f
}

// withoutTable is a copy of data whose directory records tag with length zero, which sfnt
// treats as absent.
func withoutTable(data []byte, index int, tag string) []byte {
	out := append([]byte(nil), data...)
	off := 0
	if string(out[:4]) == "ttcf" {
		off = int(binary.BigEndian.Uint32(out[12+4*index:]))
	}
	n := int(binary.BigEndian.Uint16(out[off+4:]))
	for i := 0; i < n; i++ {
		rec := off + 12 + 16*i
		if rec+16 <= len(out) && string(out[rec:rec+4]) == tag {
			binary.BigEndian.PutUint32(out[rec+12:], 0)
		}
	}
	return out
}

// tableDirectory maps table tags to their bytes for face index of a font or collection.
func tableDirectory(data []byte, index int) map[string][]byte {
	u16 := func(o int) int { return int(binary.BigEndian.Uint16(data[o:])) }
	u32 := func(o int) int { return int(binary.BigEndian.Uint32(data[o:])) }
	off := 0
	if len(data) >= 12 && string(data[:4]) == "ttcf" {
		if index >= u32(8) {
			return nil
		}
		off = u32(12 + 4*index)
	}
	if off+12 > len(data) {
		return nil
	}
	n := u16(off + 4)
	out := map[string][]byte{}
	for i := 0; i < n; i++ {
		rec := off + 12 + 16*i
		if rec+16 > len(data) {
			break
		}
		start, length := u32(rec+8), u32(rec+12)
		if start+length <= len(data) {
			out[string(data[rec:rec+4])] = data[start : start+length]
		}
	}
	return out
}

func i16(b []byte, o int) int16 {
	if o+2 > len(b) {
		return 0
	}
	return int16(binary.BigEndian.Uint16(b[o:]))
}

// verticalMetrics follows ttf-parser's Face::ascender and Face::descender.
func verticalMetrics(t map[string][]byte) (float32, float32) {
	hhea, os2 := t["hhea"], t["OS/2"]
	if len(os2) >= 78 {
		version := binary.BigEndian.Uint16(os2)
		fsSelection := binary.BigEndian.Uint16(os2[62:])
		if version >= 4 && fsSelection&(1<<7) != 0 {
			return float32(i16(os2, 68)), float32(i16(os2, 70))
		}
	}
	asc, desc := i16(hhea, 4), i16(hhea, 6)
	if asc == 0 && len(os2) >= 78 {
		if asc = i16(os2, 68); asc == 0 {
			asc = int16(binary.BigEndian.Uint16(os2[74:]))
		}
	}
	if desc == 0 && len(os2) >= 78 {
		if desc = i16(os2, 70); desc == 0 {
			desc = -int16(binary.BigEndian.Uint16(os2[76:]))
		}
	}
	return float32(asc), float32(desc)
}

// parseKern reads the horizontal format 0 subtables of a legacy kern table, which is the
// only kerning ab_glyph applies. The first subtable listing a pair wins.
func parseKern(k []byte) map[uint32]int16 {
	if len(k) < 4 {
		return nil
	}
	out := map[uint32]int16{}
	u16 := func(o int) int { return int(binary.BigEndian.Uint16(k[o:])) }
	addPairs := func(o, end int) {
		if o+8 > end {
			return
		}
		n := u16(o)
		for i := 0; i < n; i++ {
			p := o + 8 + 6*i
			if p+6 > end {
				return
			}
			key := uint32(u16(p))<<16 | uint32(u16(p+2))
			if _, seen := out[key]; !seen {
				out[key] = i16(k, p+4)
			}
		}
	}
	if u16(0) == 0 {
		n, o := u16(2), 4
		for i := 0; i < n && o+6 <= len(k); i++ {
			length, coverage := u16(o+2), u16(o+4)
			end := min(o+length, len(k))
			if length < 6 || n == 1 {
				// A lone subtable owns the rest of the table, as ttf-parser assumes; its
				// 16-bit length overflows in fonts with many pairs.
				end = len(k)
			}
			if coverage&1 != 0 && coverage>>8 == 0 {
				addPairs(o+6, end)
			}
			if length < 6 {
				break
			}
			o += length
		}
	} else if len(k) >= 8 && binary.BigEndian.Uint32(k) == 0x00010000 {
		n, o := int(binary.BigEndian.Uint32(k[4:])), 8
		for i := 0; i < n && o+8 <= len(k); i++ {
			length := int(binary.BigEndian.Uint32(k[o:]))
			coverage := u16(o + 4)
			end := min(o+length, len(k))
			if coverage&0x8000 == 0 && coverage&0x2000 == 0 && coverage&0xFF == 0 {
				addPairs(o+8, end)
			}
			if length < 8 {
				break
			}
			o += length
		}
	}
	return out
}

func (f *Font) index(r rune) sfnt.GlyphIndex {
	if v, ok := f.indices.Load(r); ok {
		return v.(sfnt.GlyphIndex)
	}
	b := f.bufs.Get().(*sfnt.Buffer)
	gi, err := f.sf.GlyphIndex(b, r)
	f.bufs.Put(b)
	if err != nil {
		gi = 0
	}
	f.indices.Store(r, gi)
	return gi
}

func (f *Font) glyph(gi sfnt.GlyphIndex) *glyph {
	if v, ok := f.glyphs.Load(gi); ok {
		return v.(*glyph)
	}
	b := f.bufs.Get().(*sfnt.Buffer)
	defer f.bufs.Put(b)
	g := &glyph{}
	ppem := fixed.Int26_6(f.sf.UnitsPerEm())
	if adv, err := f.sf.GlyphAdvance(b, gi, ppem, 0); err == nil {
		g.advance = float32(adv)
	}
	if segs, err := f.sf.LoadGlyph(b, gi, ppem, nil); err == nil && len(segs) > 0 {
		g.bounds = [4]float32{1e9, 1e9, -1e9, -1e9}
		for _, s := range segs {
			gs := glyphSeg{op: s.Op}
			n := map[sfnt.SegmentOp]int{sfnt.SegmentOpMoveTo: 1, sfnt.SegmentOpLineTo: 1, sfnt.SegmentOpQuadTo: 2, sfnt.SegmentOpCubeTo: 3}[s.Op]
			for i := 0; i < n; i++ {
				p := Pt{float32(s.Args[i].X), float32(s.Args[i].Y)}
				gs.pts[i] = p
				g.bounds = [4]float32{min(g.bounds[0], p.X), min(g.bounds[1], p.Y), max(g.bounds[2], p.X), max(g.bounds[3], p.Y)}
			}
			g.segs = append(g.segs, gs)
		}
		g.ok = g.bounds[0] < g.bounds[2] && g.bounds[1] < g.bounds[3]
	}
	f.glyphs.Store(gi, g)
	return g
}

// scale is ab_glyph's PxScale factor: size is the pixel height of ascent minus descent.
func (f *Font) scale(size float32) float32 { return size / (f.ascent - f.descent) }

func (f *Font) kernUnits(a, b sfnt.GlyphIndex) float32 {
	return float32(f.kern[uint32(a)<<16|uint32(b)])
}

func (f *Font) lineWidth(line string, h float32) float32 {
	caret := float32(0)
	var prev sfnt.GlyphIndex
	first := true
	for _, r := range line {
		gi := f.index(r)
		if !first {
			caret += h * f.kernUnits(prev, gi)
		}
		caret += h * f.glyph(gi).advance
		prev, first = gi, false
	}
	return caret
}

// Measure is the width and height of a (possibly multi-line) block of text.
func (f *Font) Measure(text string, size float32) (float32, float32) {
	lines := strings.Split(text, "\n")
	if f == nil {
		w := 0
		for _, l := range lines {
			w = max(w, len([]rune(l)))
		}
		return float32(w) * size * 0.55, float32(len(lines)) * size * LineHeight
	}
	h := f.scale(size)
	w := float32(0)
	for _, l := range lines {
		w = max(w, f.lineWidth(l, h))
	}
	return w, float32(len(lines)) * size * LineHeight
}

// Draw draws text with the block's top-left corner at (x, y); lines align within the block.
func (f *Font) Draw(dst *image.RGBA, text string, x, y, size float32, align Align, color Color) {
	if f == nil {
		return
	}
	h := f.scale(size)
	asc, desc := h*f.ascent, h*f.descent
	lineH := size * LineHeight
	ascent := asc + (lineH-(asc-desc))/2
	lines := strings.Split(text, "\n")
	blockW := float32(0)
	for _, l := range lines {
		blockW = max(blockW, f.lineWidth(l, h))
	}
	pw, ph := dst.Rect.Dx(), dst.Rect.Dy()
	r := rasterPool.Get().(*rasterizer)
	defer rasterPool.Put(r)
	for li, line := range lines {
		baseline := y + ascent + float32(li)*lineH
		lw := f.lineWidth(line, h)
		caret := x
		switch align {
		case AlignCenter:
			caret += (blockW - lw) / 2
		case AlignRight:
			caret += blockW - lw
		}
		var prev sfnt.GlyphIndex
		first := true
		for _, ch := range line {
			gi := f.index(ch)
			if !first {
				caret += h * f.kernUnits(prev, gi)
			}
			g := f.glyph(gi)
			pos := Pt{caret, baseline}
			caret += h * g.advance
			prev, first = gi, false
			if !g.ok {
				continue
			}
			f.drawGlyph(dst, r, g, pos, h, pw, ph, color)
		}
	}
}

func truncFract(v float32) (float32, float32) {
	t := float32(int32(v))
	return t, v - t
}

func (f *Font) drawGlyph(dst *image.RGBA, r *rasterizer, g *glyph, pos Pt, h float32, pw, ph int, color Color) {
	xt, xf := truncFract(pos.X)
	yt, yf := truncFract(pos.Y)
	minX := floor32(g.bounds[0]*h+xf) + xt
	minY := floor32(g.bounds[1]*h+yf) + yt
	maxX := ceil32(g.bounds[2]*h+xf) + xt
	maxY := ceil32(g.bounds[3]*h+yf) + yt
	if maxX < 0 || maxY < 0 || minX > float32(pw) || minY > float32(ph) {
		return
	}
	w, hh := int(maxX-minX), int(maxY-minY)
	if w <= 0 || hh <= 0 {
		return
	}
	r.reset(w, hh)
	off := Pt{pos.X - minX, pos.Y - minY}
	sc := func(p Pt) Pt { return Pt{float32(p.X*h) + off.X, float32(p.Y*h) + off.Y} }
	var start, cur Pt
	for _, s := range g.segs {
		switch s.op {
		case sfnt.SegmentOpMoveTo:
			if cur != start {
				r.line(cur, start)
			}
			start, cur = sc(s.pts[0]), sc(s.pts[0])
		case sfnt.SegmentOpLineTo:
			p := sc(s.pts[0])
			r.line(cur, p)
			cur = p
		case sfnt.SegmentOpQuadTo:
			p1, p2 := sc(s.pts[0]), sc(s.pts[1])
			r.quad(cur, p1, p2)
			cur = p2
		case sfnt.SegmentOpCubeTo:
			p1, p2, p3 := sc(s.pts[0]), sc(s.pts[1]), sc(s.pts[2])
			r.cubic(cur, p1, p2, p3, 0)
			cur = p3
		}
	}
	if cur != start {
		r.line(cur, start)
	}
	ox, oy := int(minX), int(minY)
	acc := float32(0)
	for i, v := range r.a[:w*hh] {
		acc += v
		px, py := ox+i%w, oy+i/w
		if px < 0 || py < 0 || px >= pw || py >= ph {
			continue
		}
		blendPx(dst.Pix[dst.PixOffset(dst.Rect.Min.X+px, dst.Rect.Min.Y+py):], color, abs32(acc))
	}
}

// blendPx is the Rust compositor's text and shadow blend: straight color over premultiplied
// pixel, with float math and rounding.
func blendPx(d []uint8, c Color, cov float32) {
	a := float32(c.A()) / 255 * clamp32(cov, 0, 1)
	if a <= 0 {
		return
	}
	inv := 1 - a
	mix := func(s, dv uint8) uint8 {
		return uint8(min(round32(float32(float32(s)*a)+float32(float32(dv)*inv)), 255))
	}
	na := uint8(min(round32(float32(255*a)+float32(float32(d[3])*inv)), 255))
	d[0] = min(mix(c.R(), d[0]), na)
	d[1] = min(mix(c.G(), d[1]), na)
	d[2] = min(mix(c.B(), d[2]), na)
	d[3] = na
}

// MeasureText measures text in the default font.
func MeasureText(text string, size float32) (float32, float32) {
	return DefaultFont().Measure(text, size)
}

// DrawText draws text in the default font, left-aligned, with its top-left corner at (x, y).
func DrawText(dst *image.RGBA, text string, x, y, size float32, color Color) {
	DefaultFont().Draw(dst, text, x, y, size, AlignLeft, color)
}
