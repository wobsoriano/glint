package ocr

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"golang.org/x/image/draw"
	"golang.org/x/sys/windows"
)

const (
	pixelFormatBgra8 = 87
	tileOverlap      = 160
)

// Recognize reads the text in img with the OCR engine for the user's profile languages and
// returns its lines in reading order. It blocks until recognition ends, which takes up to a
// few seconds for a 4K image. It is safe to call from any goroutine.
func Recognize(img *image.RGBA) ([]Line, error) {
	if img.Rect.Empty() {
		return nil, errors.New("ocr: empty image")
	}
	type result struct {
		lines []Line
		err   error
	}
	done := make(chan result, 1)
	go func() {
		// The thread stays locked, so Go ends it with this goroutine and the apartment
		// this call joins never leaks into other goroutines.
		runtime.LockOSThread()
		lines, err := recognizeOnThread(img)
		done <- result{lines, err}
	}()
	r := <-done
	return r.lines, r.err
}

func recognizeOnThread(img *image.RGBA) ([]Line, error) {
	hr, _, _ := procRoInitialize.Call(roInitMultithreaded)
	switch {
	case hr == uintptr(windows.RPC_E_CHANGED_MODE):
		// The thread is already in a single-threaded apartment. The OCR classes are agile,
		// so they work there too, and that initialization is not ours to undo.
	case int32(hr) < 0:
		return nil, fmt.Errorf("ocr: RoInitialize: %w", windows.Errno(hr))
	default:
		defer procRoUninitialize.Call()
	}

	var s session
	defer s.close()
	if err := s.open(); err != nil {
		return nil, err
	}

	// Small captures are upscaled, since Windows OCR misses tiny UI text.
	src, b := img, img.Rect
	k := float32(1)
	if max(b.Dx(), b.Dy())*2 <= s.maxDim && b.Dx()*b.Dy() < 1_500_000 {
		k = 2
		src = image.NewRGBA(image.Rect(0, 0, b.Dx()*2, b.Dy()*2))
		draw.CatmullRom.Scale(src, src.Rect, img, b, draw.Src, nil)
	}
	ww, wh := src.Rect.Dx(), src.Rect.Dy()

	// Larger images go in overlapping tiles no bigger than the engine accepts.
	step := s.maxDim - tileOverlap
	var lines []Line
	for ty := 0; ; ty += step {
		th := min(wh-ty, s.maxDim)
		for tx := 0; ; tx += step {
			tw := min(ww-tx, s.maxDim)
			r := image.Rect(tx, ty, tx+tw, ty+th).Add(src.Rect.Min)
			found, err := s.recognizeTile(src, r)
			if err != nil {
				return nil, err
			}
			to := func(q Rect) Rect {
				return Rect{
					X: (q.X+float32(tx))/k + float32(b.Min.X),
					Y: (q.Y+float32(ty))/k + float32(b.Min.Y),
					W: q.W / k,
					H: q.H / k,
				}
			}
			earlier := len(lines)
			for _, l := range found {
				l.Bounds = to(l.Bounds)
				for i := range l.Words {
					l.Words[i].Bounds = to(l.Words[i].Bounds)
				}
				if !covered(lines[:earlier], l.Bounds) {
					lines = append(lines, l)
				}
			}
			if tx+tw >= ww {
				break
			}
		}
		if ty+th >= wh {
			break
		}
	}
	sortReadingOrder(lines)
	return lines, nil
}

// covered reports whether one of lines covers most of r. Neighbor tiles overlap, so the
// engine reads a line inside the overlap once for each.
func covered(lines []Line, r Rect) bool {
	for _, l := range lines {
		if l.Bounds.overlap(r) > 0.5*min(l.Bounds.area(), r.area()) {
			return true
		}
	}
	return false
}

// session holds the WinRT objects one Recognize call uses, on the thread that made them.
type session struct {
	engine, bitmaps, buffers *object
	maxDim                   int
}

func (s *session) open() error {
	statics, err := activationFactory("Windows.Media.Ocr.OcrEngine", &iidOcrEngineStatics)
	if err != nil {
		return fmt.Errorf("ocr: OcrEngine: %w", err)
	}
	defer statics.release()
	if err := statics.call(slotOcrEngineStaticsTryCreateFromUserProfileLanguages, uintptr(unsafe.Pointer(&s.engine))); err != nil {
		return fmt.Errorf("ocr: no OCR language pack installed: %w", err)
	}
	if s.engine == nil {
		return errors.New("ocr: no OCR language pack installed")
	}
	var maxDim uint32
	if statics.call(slotOcrEngineStaticsMaxImageDimension, uintptr(unsafe.Pointer(&maxDim))) != nil {
		maxDim = 2600
	}
	s.maxDim = max(int(maxDim), 1000)

	if s.bitmaps, err = activationFactory("Windows.Graphics.Imaging.SoftwareBitmap", &iidSoftwareBitmapStatics); err != nil {
		return fmt.Errorf("ocr: SoftwareBitmap: %w", err)
	}
	if s.buffers, err = activationFactory("Windows.Security.Cryptography.CryptographicBuffer", &iidCryptographicBufferStatics); err != nil {
		return fmt.Errorf("ocr: CryptographicBuffer: %w", err)
	}
	return nil
}

func (s *session) close() {
	s.engine.release()
	s.bitmaps.release()
	s.buffers.release()
}

// recognizeTile reads the text in the part r of img, with boxes relative to r's corner.
func (s *session) recognizeTile(img *image.RGBA, r image.Rectangle) ([]Line, error) {
	w, h := r.Dx(), r.Dy()
	bgra := make([]byte, w*h*4)
	for y := range h {
		row := img.Pix[img.PixOffset(r.Min.X, r.Min.Y+y):][: w*4 : w*4]
		out := bgra[y*w*4 : (y+1)*w*4]
		for i := 0; i < len(row); i += 4 {
			out[i], out[i+1], out[i+2], out[i+3] = row[i+2], row[i+1], row[i], 255
		}
	}

	var buf *object
	if err := s.buffers.call(slotCryptographicBufferStaticsCreateFromByteArray,
		uintptr(len(bgra)), uintptr(unsafe.Pointer(&bgra[0])), uintptr(unsafe.Pointer(&buf))); err != nil {
		return nil, fmt.Errorf("ocr: CreateFromByteArray: %w", err)
	}
	defer buf.release()
	var bitmap *object
	if err := s.bitmaps.call(slotSoftwareBitmapStaticsCreateCopyFromBuffer,
		uintptr(unsafe.Pointer(buf)), pixelFormatBgra8, uintptr(w), uintptr(h), uintptr(unsafe.Pointer(&bitmap))); err != nil {
		return nil, fmt.Errorf("ocr: CreateCopyFromBuffer: %w", err)
	}
	defer bitmap.release()
	var op *object
	if err := s.engine.call(slotOcrEngineRecognizeAsync, uintptr(unsafe.Pointer(bitmap)), uintptr(unsafe.Pointer(&op))); err != nil {
		return nil, fmt.Errorf("ocr: RecognizeAsync: %w", err)
	}
	defer op.release()
	res, err := await(op)
	if err != nil {
		return nil, fmt.Errorf("ocr: RecognizeAsync: %w", err)
	}
	defer res.release()
	lines, err := readLines(res)
	if err != nil {
		return nil, fmt.Errorf("ocr: reading results: %w", err)
	}
	return lines, nil
}

func readLines(res *object) ([]Line, error) {
	view, err := res.getObject(slotOcrResultLines)
	if err != nil {
		return nil, err
	}
	defer view.release()
	n, err := view.vectorSize()
	if err != nil {
		return nil, err
	}
	var out []Line
	for i := range n {
		line, err := view.vectorAt(i)
		if err != nil {
			return nil, err
		}
		words, err := readWords(line)
		line.release()
		if err != nil {
			return nil, err
		}
		if len(words) == 0 {
			continue
		}
		bounds := words[0].Bounds
		for _, w := range words[1:] {
			bounds = bounds.union(w.Bounds)
		}
		out = append(out, Line{Words: words, Bounds: bounds})
	}
	return out, nil
}

func readWords(line *object) ([]Word, error) {
	view, err := line.getObject(slotOcrLineWords)
	if err != nil {
		return nil, err
	}
	defer view.release()
	n, err := view.vectorSize()
	if err != nil {
		return nil, err
	}
	words := make([]Word, 0, n)
	for i := range n {
		word, err := view.vectorAt(i)
		if err != nil {
			return nil, err
		}
		w, err := readWord(word)
		word.release()
		if err != nil {
			return nil, err
		}
		words = append(words, w)
	}
	return words, nil
}

func readWord(word *object) (Word, error) {
	var r Rect // Windows.Foundation.Rect has the same layout: X, Y, Width, Height as float32.
	if err := word.call(slotOcrWordBoundingRect, uintptr(unsafe.Pointer(&r))); err != nil {
		return Word{}, err
	}
	var text hstring
	if err := word.call(slotOcrWordText, uintptr(unsafe.Pointer(&text))); err != nil {
		return Word{}, err
	}
	defer text.free()
	return Word{Text: text.String(), Bounds: r}, nil
}
