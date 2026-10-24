package render

import (
	"bytes"
	"encoding/json"
	"math"
	"reflect"
	"strconv"
	"strings"
)

// The style and effects types write their float32 fields as serde_json does, so settings.json
// and presets.json, which embed them, stay byte-for-byte what the Rust build writes.

func (g Gradient) MarshalJSON() ([]byte, error) {
	type plain Gradient
	return marshalSerde(plain(g))
}

func (a Aspect) MarshalJSON() ([]byte, error) {
	type plain Aspect
	return marshalSerde(plain(a))
}

func (b Border) MarshalJSON() ([]byte, error) {
	type plain Border
	return marshalSerde(plain(b))
}

func (w Watermark) MarshalJSON() ([]byte, error) {
	type plain Watermark
	return marshalSerde(plain(w))
}

func (p Perspective) MarshalJSON() ([]byte, error) {
	type plain Perspective
	return marshalSerde(plain(p))
}

func (b ProgressiveBlur) MarshalJSON() ([]byte, error) {
	type plain ProgressiveBlur
	return marshalSerde(plain(b))
}

// marshalSerde writes struct v's exported fields under their json tag names, float32 fields
// as appendF32 spells them and the rest as encoding/json writes them. The tags may carry no
// options.
func marshalSerde(v any) ([]byte, error) {
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	out := []byte{'{'}
	for i := range rt.NumField() {
		f := rt.Field(i)
		name := f.Tag.Get("json")
		if !f.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		if len(out) > 1 {
			out = append(out, ',')
		}
		out = strconv.AppendQuote(out, name)
		out = append(out, ':')
		fv := rv.Field(i)
		if fv.Kind() == reflect.Float32 {
			out = appendF32(out, float32(fv.Float()))
			continue
		}
		buf.Reset()
		if err := enc.Encode(fv.Interface()); err != nil {
			return nil, err
		}
		out = append(out, bytes.TrimSuffix(buf.Bytes(), []byte("\n"))...)
	}
	return append(out, '}'), nil
}

// appendF32 spells f as serde_json does: the shortest digits that read back as f, written as
// an integer with ".0" while the decimal point falls within 13 digits, as a plain decimal down
// to 1e-6, and in exponent form ("1e+20", "1.5e-7") beyond. Non-finite values are null.
func appendF32(b []byte, f float32) []byte {
	v := float64(f)
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return append(b, "null"...)
	}
	if math.Signbit(v) {
		b = append(b, '-')
		v = -v
	}
	if v == 0 {
		return append(b, "0.0"...)
	}
	mant, exp, _ := strings.Cut(strconv.FormatFloat(v, 'e', -1, 32), "e")
	digits := strings.Replace(mant, ".", "", 1)
	e, _ := strconv.Atoi(exp)
	n := len(digits)
	kk := e + 1 // the value is 0.digits × 10^kk
	switch {
	case n <= kk && kk <= 13:
		b = append(b, digits...)
		b = append(b, strings.Repeat("0", kk-n)...)
		return append(b, ".0"...)
	case 0 < kk && kk <= 13:
		b = append(b, digits[:kk]...)
		b = append(b, '.')
		return append(b, digits[kk:]...)
	case -6 < kk && kk <= 0:
		b = append(b, "0."...)
		b = append(b, strings.Repeat("0", -kk)...)
		return append(b, digits...)
	}
	b = append(b, digits[0])
	if n > 1 {
		b = append(b, '.')
		b = append(b, digits[1:]...)
	}
	b = append(b, 'e')
	if kk > 0 {
		b = append(b, '+')
	}
	return strconv.AppendInt(b, int64(kk-1), 10)
}
