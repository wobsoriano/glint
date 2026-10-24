package media

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

type WavData struct {
	SampleRate int
	Channels   int
	// Samples are interleaved, Channels values per frame, in -1..1.
	Samples []float32
}

// Frames is the number of sample frames (samples per channel).
func (d *WavData) Frames() int { return len(d.Samples) / max(d.Channels, 1) }

func (d *WavData) DurationMS() int64 {
	return int64(d.Frames()) * 1000 / int64(max(d.SampleRate, 1))
}

// ReadWAV reads a PCM (8/16/24/32-bit) or IEEE float (32/64-bit) WAV file, extensible
// headers included.
func ReadWAV(path string) (*WavData, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	d, err := parseWAV(b)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return d, nil
}

func parseWAV(b []byte) (*WavData, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, errors.New("not a RIFF/WAVE file")
	}
	le := binary.LittleEndian
	var tag, bits uint16
	var channels, rate int
	haveFmt := false
	for pos := 12; pos+8 <= len(b); {
		id := string(b[pos : pos+4])
		n := int(le.Uint32(b[pos+4:]))
		body := b[pos+8 : min(pos+8+n, len(b))]
		switch id {
		case "fmt ":
			if len(body) < 16 {
				return nil, errors.New("fmt chunk too short")
			}
			tag = le.Uint16(body[0:])
			channels = int(le.Uint16(body[2:]))
			rate = int(le.Uint32(body[4:]))
			bits = le.Uint16(body[14:])
			if tag == 0xFFFE && len(body) >= 26 {
				tag = le.Uint16(body[24:])
			}
			haveFmt = true
		case "data":
			if !haveFmt {
				return nil, errors.New("data chunk before fmt chunk")
			}
			samples, err := decodeSamples(body, tag, bits)
			if err != nil {
				return nil, err
			}
			return &WavData{SampleRate: rate, Channels: channels, Samples: samples}, nil
		}
		pos += 8 + n + n&1
	}
	return nil, errors.New("no data chunk")
}

func decodeSamples(body []byte, tag, bits uint16) ([]float32, error) {
	le := binary.LittleEndian
	size := int(bits) / 8
	if size == 0 {
		return nil, fmt.Errorf("unsupported WAV format tag %d with %d bits", tag, bits)
	}
	out := make([]float32, len(body)/size)
	switch {
	case tag == 1 && bits == 8:
		for i := range out {
			out[i] = (float32(body[i]) - 128) / 128
		}
	case tag == 1 && bits == 16:
		for i := range out {
			out[i] = float32(int16(le.Uint16(body[i*2:]))) / 32768
		}
	case tag == 1 && bits == 24:
		for i := range out {
			c := body[i*3:]
			out[i] = float32(int32(uint32(c[0])<<8|uint32(c[1])<<16|uint32(c[2])<<24)>>8) / 8_388_608
		}
	case tag == 1 && bits == 32:
		for i := range out {
			out[i] = float32(int32(le.Uint32(body[i*4:]))) / 2_147_483_648
		}
	case tag == 3 && bits == 32:
		for i := range out {
			out[i] = math.Float32frombits(le.Uint32(body[i*4:]))
		}
	case tag == 3 && bits == 64:
		for i := range out {
			out[i] = float32(math.Float64frombits(le.Uint64(body[i*8:])))
		}
	default:
		return nil, fmt.Errorf("unsupported WAV format tag %d with %d bits", tag, bits)
	}
	return out, nil
}

// wavHeaderLen is the 46-byte header WAVWriter writes: RIFF, an 18-byte float fmt chunk and
// the data chunk's header.
const wavHeaderLen = 46

// WAVWriter writes 32-bit float WAV, patching the header's sizes on Finish.
type WAVWriter struct {
	f        *os.File
	w        *bufio.Writer
	channels int
	frames   int64
	buf      []byte
}

func CreateWAV(path string, sampleRate, channels int) (*WAVWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	blockAlign := channels * 4
	h := make([]byte, 0, wavHeaderLen)
	le := binary.LittleEndian
	h = append(h, "RIFF"...)
	h = le.AppendUint32(h, 0)
	h = append(h, "WAVEfmt "...)
	h = le.AppendUint32(h, 18)
	h = le.AppendUint16(h, 3)
	h = le.AppendUint16(h, uint16(channels))
	h = le.AppendUint32(h, uint32(sampleRate))
	h = le.AppendUint32(h, uint32(sampleRate*blockAlign))
	h = le.AppendUint16(h, uint16(blockAlign))
	h = le.AppendUint16(h, 32)
	h = le.AppendUint16(h, 0)
	h = append(h, "data"...)
	h = le.AppendUint32(h, 0)
	w := bufio.NewWriterSize(f, 64<<10)
	if _, err := w.Write(h); err != nil {
		f.Close()
		return nil, err
	}
	return &WAVWriter{f: f, w: w, channels: channels}, nil
}

func (w *WAVWriter) FramesWritten() int64 { return w.frames }

// Write appends interleaved samples, whole frames only.
func (w *WAVWriter) Write(samples []float32) error {
	w.buf = w.buf[:0]
	for _, s := range samples {
		w.buf = binary.LittleEndian.AppendUint32(w.buf, math.Float32bits(s))
	}
	if _, err := w.w.Write(w.buf); err != nil {
		return err
	}
	w.frames += int64(len(samples) / w.channels)
	return nil
}

func (w *WAVWriter) WriteSilence(frames int64) error {
	var zeros [4096]byte
	for left := frames * int64(w.channels) * 4; left > 0; {
		n := min(left, int64(len(zeros)))
		if _, err := w.w.Write(zeros[:n]); err != nil {
			return err
		}
		left -= n
	}
	w.frames += frames
	return nil
}

// Truncate drops every frame after the first frames.
func (w *WAVWriter) Truncate(frames int64) error {
	if frames >= w.frames {
		return nil
	}
	if err := w.w.Flush(); err != nil {
		return err
	}
	if err := w.f.Truncate(wavHeaderLen + frames*int64(w.channels)*4); err != nil {
		return err
	}
	if _, err := w.f.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	w.frames = frames
	return nil
}

// Finish writes the header's sizes and closes the file. It returns the file's length.
func (w *WAVWriter) Finish() (int64, error) {
	dataLen := w.frames * int64(w.channels) * 4
	err := w.w.Flush()
	var b [4]byte
	if err == nil {
		binary.LittleEndian.PutUint32(b[:], uint32(dataLen+wavHeaderLen-8))
		_, err = w.f.WriteAt(b[:], 4)
	}
	if err == nil {
		binary.LittleEndian.PutUint32(b[:], uint32(dataLen))
		_, err = w.f.WriteAt(b[:], wavHeaderLen-4)
	}
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	return dataLen + wavHeaderLen, err
}
