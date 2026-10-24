// Command redactprobe prints what the Windows OCR engine reads in a PNG, line by line, and the
// redactions internal/redact finds in it with every rule on.
//
//	go run ./tools/verify/redactprobe capture.png
package main

import (
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"

	"github.com/wobsoriano/glint/internal/ocr"
	"github.com/wobsoriano/glint/internal/redact"
	"github.com/wobsoriano/glint/internal/settings"
)

func main() {
	f, err := os.Open(os.Args[1])
	if err != nil {
		panic(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		panic(err)
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	lines, err := ocr.Recognize(img)
	if err != nil {
		panic(err)
	}
	for _, l := range lines {
		fmt.Printf("line %q\n", l.Text())
	}
	all := settings.RedactRules{Emails: true, Phones: true, CreditCards: true, Secrets: true, IPAddresses: true, IDs: true, URLsWithTokens: true}
	for _, f := range redact.Find(lines, all, 2) {
		fmt.Printf("finding %s at %+v\n", f.Kind.Label(), f.Rect)
	}
}
