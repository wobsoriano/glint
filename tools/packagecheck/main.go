// Command packagecheck checks a Glint.exe that mygo build made: the GUI subsystem, so no
// console window opens; the icon, against resources/icon.png at 256 px; and the version
// information. It exits 1 when a check fails. From the repository root:
//
//	go run ./tools/packagecheck build/windows-amd64/Glint.exe
package main

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"unicode/utf16"

	"golang.org/x/image/draw"
)

const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24

	subsystemWindowsGUI = 2
)

func main() {
	icon := flag.String("icon", "resources/icon.png", "the PNG the executable's icon is made from")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: packagecheck [-icon resources/icon.png] <Glint.exe>")
		os.Exit(2)
	}
	failed := false
	check := func(ok bool, format string, args ...any) {
		status := "PASS"
		if !ok {
			status, failed = "FAIL", true
		}
		fmt.Printf("%s %s\n", status, fmt.Sprintf(format, args...))
	}
	if err := run(flag.Arg(0), *icon, check); err != nil {
		fmt.Println("FAIL", err)
		failed = true
	}
	if failed {
		os.Exit(1)
	}
}

func run(exe, iconPNG string, check func(bool, string, ...any)) error {
	f, err := pe.Open(exe)
	if err != nil {
		return err
	}
	defer f.Close()
	oh, ok := f.OptionalHeader.(*pe.OptionalHeader64)
	if !ok {
		return errors.New("not a 64-bit executable")
	}
	check(oh.Subsystem == subsystemWindowsGUI, "PE subsystem is %d (2 is the Windows GUI subsystem, 3 the console)", oh.Subsystem)

	rsrc, err := readResources(f, oh)
	if err != nil {
		return err
	}
	_, hasManifest := rsrc[rtManifest]
	check(hasManifest, "has an application manifest")

	group := first(rsrc[rtGroupIcon])
	check(group != nil, "has an icon group")
	if group != nil {
		icon, size, err := largestIcon(group, rsrc[rtIcon])
		if err != nil {
			return err
		}
		check(size == 256, "the largest icon is %d px", size)
		want, err := scaledPNG(iconPNG, size)
		if err != nil {
			return err
		}
		mean, worst := compare(icon, want)
		check(mean < 2, "the %d px icon matches %s scaled to %d px: mean channel difference %.3f, largest %d", size, iconPNG, size, mean, worst)
	}

	version := first(rsrc[rtVersion])
	check(version != nil, "has version information")
	if version != nil {
		strs := versionStrings(version)
		js, _ := json.Marshal(strs)
		fmt.Println("     version strings:", string(js))
		for _, k := range []string{"ProductName", "FileDescription", "FileVersion", "ProductVersion", "OriginalFilename"} {
			check(strs[k] != "", "version information has %s %q", k, strs[k])
		}
	}
	return nil
}

func first(m map[uint32][]byte) []byte {
	for _, b := range m {
		return b
	}
	return nil
}

// readResources returns the resources of the executable by type and id, with the first
// language of each.
func readResources(f *pe.File, oh *pe.OptionalHeader64) (map[uint32]map[uint32][]byte, error) {
	dir := oh.DataDirectory[pe.IMAGE_DIRECTORY_ENTRY_RESOURCE]
	var sec *pe.Section
	for _, s := range f.Sections {
		if dir.VirtualAddress >= s.VirtualAddress && dir.VirtualAddress < s.VirtualAddress+s.VirtualSize {
			sec = s
		}
	}
	if sec == nil {
		return nil, errors.New("no resource section")
	}
	data, err := sec.Data()
	if err != nil {
		return nil, err
	}
	base := dir.VirtualAddress - sec.VirtualAddress
	le := binary.LittleEndian
	entries := func(off uint32) [][2]uint32 {
		p := data[base+off:]
		n := int(le.Uint16(p[12:])) + int(le.Uint16(p[14:]))
		var out [][2]uint32
		for i := range n {
			e := p[16+8*i:]
			out = append(out, [2]uint32{le.Uint32(e), le.Uint32(e[4:])})
		}
		return out
	}
	const subdir = 0x80000000
	res := map[uint32]map[uint32][]byte{}
	for _, t := range entries(0) {
		if t[1]&subdir == 0 {
			continue
		}
		res[t[0]] = map[uint32][]byte{}
		for _, id := range entries(t[1] &^ subdir) {
			if id[1]&subdir == 0 {
				continue
			}
			langs := entries(id[1] &^ subdir)
			if len(langs) == 0 {
				continue
			}
			leaf := data[base+langs[0][1]:]
			rva, size := le.Uint32(leaf), le.Uint32(leaf[4:])
			start := rva - sec.VirtualAddress
			res[t[0]][id[0]] = data[start : start+size]
		}
	}
	return res, nil
}

// largestIcon decodes the largest image of an icon group, which mygo build stores as PNG.
func largestIcon(group []byte, icons map[uint32][]byte) (image.Image, int, error) {
	le := binary.LittleEndian
	n := int(le.Uint16(group[4:]))
	best, bestID := 0, uint32(0)
	for i := range n {
		e := group[6+14*i:]
		size := int(e[0])
		if size == 0 {
			size = 256
		}
		if size > best {
			best, bestID = size, uint32(le.Uint16(e[12:]))
		}
	}
	img, err := png.Decode(bytes.NewReader(icons[bestID]))
	if err != nil {
		return nil, 0, fmt.Errorf("icon %d is not a PNG: %w", bestID, err)
	}
	return img, best, nil
}

func scaledPNG(path string, size int) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	src, err := png.Decode(f)
	if err != nil {
		return nil, err
	}
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst, nil
}

// compare returns the mean and the largest difference of the 8-bit channels of two images
// of one size, as premultiplied colors so that hidden colors under full transparency do not
// count.
func compare(a, b image.Image) (float64, int) {
	r := a.Bounds()
	var sum, n float64
	worst := 0
	for y := range r.Dy() {
		for x := range r.Dx() {
			ca := a.At(r.Min.X+x, r.Min.Y+y)
			cb := b.At(b.Bounds().Min.X+x, b.Bounds().Min.Y+y)
			r1, g1, b1, a1 := ca.RGBA()
			r2, g2, b2, a2 := cb.RGBA()
			for _, d := range [4][2]uint32{{r1, r2}, {g1, g2}, {b1, b2}, {a1, a2}} {
				diff := int(d[0]>>8) - int(d[1]>>8)
				diff = max(diff, -diff)
				worst = max(worst, diff)
				sum += float64(diff)
				n++
			}
		}
	}
	return sum / n, worst
}

// versionStrings walks a VS_VERSIONINFO block and returns the strings of its StringFileInfo.
func versionStrings(b []byte) map[string]string {
	out := map[string]string{}
	var walk func(b []byte)
	walk = func(b []byte) {
		le := binary.LittleEndian
		for len(b) >= 6 {
			length, valueLen, typ := int(le.Uint16(b)), int(le.Uint16(b[2:])), le.Uint16(b[4:])
			if length < 6 || length > len(b) {
				return
			}
			node := b[:length]
			key, off := utf16z(node[6:])
			off = align4(6 + off)
			valueBytes := valueLen
			if typ == 1 {
				valueBytes = 2 * valueLen
			}
			if typ == 1 && valueLen > 0 && off+valueBytes <= len(node) {
				v, _ := utf16z(node[off : off+valueBytes])
				out[key] = v
			}
			if children := align4(off + valueBytes); children < len(node) {
				walk(node[children:])
			}
			if align4(length) >= len(b) {
				return
			}
			b = b[align4(length):]
		}
	}
	walk(b)
	return out
}

// utf16z decodes a NUL-terminated UTF-16 string and returns it with the bytes it took.
func utf16z(b []byte) (string, int) {
	var u []uint16
	i := 0
	for ; i+1 < len(b); i += 2 {
		c := binary.LittleEndian.Uint16(b[i:])
		if c == 0 {
			i += 2
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u)), i
}

func align4(n int) int { return (n + 3) &^ 3 }
