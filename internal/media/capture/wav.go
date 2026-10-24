package capture

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"time"
)

// wavHeaderSize is the 46-byte header the Rust writer emits: a WAVE_FORMAT_IEEE_FLOAT fmt
// chunk of 18 bytes (cbSize 0), then the data chunk header.
const wavHeaderSize = 46

// wavWriter writes interleaved 32-bit float WAV. The RIFF and data sizes are patched in
// finish.
type wavWriter struct {
	file     *os.File
	buf      *bufio.Writer
	channels int
	frames   uint64
}

func createWAV(path string, sampleRate, channels int) (*wavWriter, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	blockAlign := channels * 4
	h := make([]byte, 0, wavHeaderSize)
	h = append(h, "RIFF"...)
	h = binary.LittleEndian.AppendUint32(h, 0)
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 18)
	h = binary.LittleEndian.AppendUint16(h, 3)
	h = binary.LittleEndian.AppendUint16(h, uint16(channels))
	h = binary.LittleEndian.AppendUint32(h, uint32(sampleRate))
	h = binary.LittleEndian.AppendUint32(h, uint32(sampleRate*blockAlign))
	h = binary.LittleEndian.AppendUint16(h, uint16(blockAlign))
	h = binary.LittleEndian.AppendUint16(h, 32)
	h = binary.LittleEndian.AppendUint16(h, 0)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, 0)
	w := &wavWriter{file: f, buf: bufio.NewWriterSize(f, 64<<10), channels: channels}
	if _, err := w.buf.Write(h); err != nil {
		f.Close()
		return nil, err
	}
	return w, nil
}

func (w *wavWriter) write(samples []float32) error {
	var b [4]byte
	for _, s := range samples {
		binary.LittleEndian.PutUint32(b[:], math.Float32bits(s))
		if _, err := w.buf.Write(b[:]); err != nil {
			return err
		}
	}
	w.frames += uint64(len(samples) / w.channels)
	return nil
}

func (w *wavWriter) writeSilence(frames uint64) error {
	var zeros [4096]byte
	for left := frames * uint64(w.channels) * 4; left > 0; {
		n := min(left, uint64(len(zeros)))
		if _, err := w.buf.Write(zeros[:n]); err != nil {
			return err
		}
		left -= n
	}
	w.frames += frames
	return nil
}

// truncate drops every frame after the first frames.
func (w *wavWriter) truncate(frames uint64) error {
	if frames >= w.frames {
		return nil
	}
	if err := w.buf.Flush(); err != nil {
		return err
	}
	if err := w.file.Truncate(wavHeaderSize + int64(frames)*int64(w.channels)*4); err != nil {
		return err
	}
	if _, err := w.file.Seek(0, io.SeekEnd); err != nil {
		return err
	}
	w.frames = frames
	return nil
}

// finish patches the header and closes the file, returning its size in bytes.
func (w *wavWriter) finish() (uint64, error) {
	dataLen := w.frames * uint64(w.channels) * 4
	err := w.buf.Flush()
	if err == nil {
		_, err = w.file.WriteAt(binary.LittleEndian.AppendUint32(nil, uint32(dataLen+wavHeaderSize-8)), 4)
	}
	if err == nil {
		_, err = w.file.WriteAt(binary.LittleEndian.AppendUint32(nil, uint32(dataLen)), wavHeaderSize-4)
	}
	if cerr := w.file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return 0, fmt.Errorf("finish %s: %w", w.file.Name(), err)
	}
	return dataLen + wavHeaderSize, nil
}

// clockedWAV is a WAV track at SampleRate that follows the media clock. Each buffer lands at
// the media time of the instant the device stamped on its first frame, so the file lines up
// with every other track whatever the capture latency. Frames stamped in a pause are left
// out. A gap, such as loopback delivering nothing while nothing plays, becomes silence; a
// device clock drifting from the performance counter is pulled back once it is more than
// placementSlack off. finish pads or cuts the file to media time.
type clockedWAV struct {
	w     *wavWriter
	clock *Clock
}

// placementSlack is how far a buffer may land from where its timestamp puts it before it is
// moved: jitter below this never cuts or pads, and drift is corrected well within a frame.
const placementSlack = SampleRate * 2 / 1000

func createClockedWAV(path string, channels int, clock *Clock) (*clockedWAV, error) {
	w, err := createWAV(path, SampleRate, channels)
	if err != nil {
		return nil, err
	}
	return &clockedWAV{w: w, clock: clock}, nil
}

func framesAt(d time.Duration) int64 { return int64(math.Round(d.Seconds() * SampleRate)) }

// write takes 48 kHz interleaved samples whose first frame the device stamped with at.
func (c *clockedWAV) write(samples []float32, at Instant) error {
	ch := c.w.channels
	for len(samples) > 0 {
		media, ok, next := c.clock.locate(at)
		n := len(samples) / ch
		if next != forever {
			n = min(n, max(int(math.Ceil(next.sub(at).Seconds()*SampleRate)), 1))
		}
		if ok {
			if err := c.place(samples[:n*ch], framesAt(media)); err != nil {
				return err
			}
		}
		samples = samples[n*ch:]
		at += Instant(int64(n) * 1e7 / SampleRate)
	}
	return nil
}

// place writes samples so their first frame is frame pos of the file.
func (c *clockedWAV) place(samples []float32, pos int64) error {
	switch gap := pos - int64(c.w.frames); {
	case gap > placementSlack:
		if err := c.w.writeSilence(uint64(gap)); err != nil {
			return err
		}
	case gap < -placementSlack:
		samples = samples[min(int(-gap)*c.w.channels, len(samples)):]
	}
	return c.w.write(samples)
}

func (c *clockedWAV) finish() (Stats, error) {
	target := uint64(framesAt(c.clock.Now()))
	var err error
	if have := c.w.frames; target > have {
		err = c.w.writeSilence(target - have)
	} else {
		err = c.w.truncate(target)
	}
	frames := c.w.frames
	bytes, ferr := c.w.finish()
	if err == nil {
		err = ferr
	}
	return Stats{Frames: frames, Bytes: bytes}, err
}
