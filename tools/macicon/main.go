// Command macicon writes resources/darwin/Glint.icns from assets/glint-mac-1024.png.
//
// mygo build makes the app icon of every platform from resources/icon.png, which fills its
// square as Windows icons do. macOS icons sit inside Apple's icon grid with a margin, so the
// macOS app ships this .icns, which mygo.json's macos.infoPlist names as CFBundleIconFile.
// Rerun it from the repository root after changing the macOS artwork:
//
//	go run ./tools/macicon
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"

	"golang.org/x/image/draw"
)

func main() {
	src := flag.String("src", "assets/glint-mac-1024.png", "the 1024 px macOS icon artwork")
	out := flag.String("out", "resources/darwin/Glint.icns", "the .icns to write")
	flag.Parse()
	f, err := os.Open(*src)
	if err != nil {
		log.Fatal(err)
	}
	img, err := png.Decode(f)
	f.Close()
	if err != nil {
		log.Fatalf("%s: %v", *src, err)
	}
	if b := img.Bounds(); b.Dx() != 1024 || b.Dy() != 1024 {
		log.Fatalf("%s is %dx%d, not 1024x1024", *src, b.Dx(), b.Dy())
	}
	icns, err := encodeICNS(img)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, icns, 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Println(*out)
}

// encodeICNS stores PNG payloads under the types macOS reads them from. The 16 and 32 px
// types (icp4, icp5) are left out, as mygo build leaves them out: IconServices reads those as
// the old ARGB format even when they hold a PNG, and scales ic11 and ic12 down instead.
func encodeICNS(img image.Image) ([]byte, error) {
	entries := []struct {
		kind string
		size int
	}{
		{"ic11", 32}, {"ic12", 64}, {"ic07", 128}, {"ic13", 256}, {"ic08", 256},
		{"ic14", 512}, {"ic09", 512}, {"ic10", 1024},
	}
	var body bytes.Buffer
	for _, e := range entries {
		dst := image.NewNRGBA(image.Rect(0, 0, e.size, e.size))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Src, nil)
		var p bytes.Buffer
		if err := png.Encode(&p, dst); err != nil {
			return nil, err
		}
		body.WriteString(e.kind)
		binary.Write(&body, binary.BigEndian, uint32(p.Len()+8))
		body.Write(p.Bytes())
	}
	var out bytes.Buffer
	out.WriteString("icns")
	binary.Write(&out, binary.BigEndian, uint32(body.Len()+8))
	out.Write(body.Bytes())
	return out.Bytes(), nil
}
