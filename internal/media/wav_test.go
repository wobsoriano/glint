package media

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestFloatWAVRoundTripKeepsFormatAndSamples(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rt.wav")
	w, err := CreateWAV(path, 48000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write([]float32{0.25, -0.5, 1.0, 0.0}); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteSilence(2); err != nil {
		t.Fatal(err)
	}
	n, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if n != 46+8*4 {
		t.Fatalf("Finish returned %d bytes, want %d", n, 46+8*4)
	}
	d, err := ReadWAV(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []float32{0.25, -0.5, 1.0, 0.0, 0, 0, 0, 0}
	if d.SampleRate != 48000 || d.Channels != 2 || !slices.Equal(d.Samples, want) {
		t.Fatalf("read back %+v", d)
	}
	if d.Frames() != 4 {
		t.Fatalf("Frames() = %d, want 4", d.Frames())
	}
	if st, _ := os.Stat(path); st.Size() != n {
		t.Fatalf("file is %d bytes, Finish said %d", st.Size(), n)
	}
}

func TestWAVTruncateCutsTheTailAndTheHeaderFollows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cut.wav")
	w, err := CreateWAV(path, 48000, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write([]float32{0.25, -0.5, 1.0, 0.0, 0.5, 0.5}); err != nil {
		t.Fatal(err)
	}
	if err := w.Truncate(5); err != nil {
		t.Fatal(err)
	}
	if err := w.Truncate(2); err != nil {
		t.Fatal(err)
	}
	n, err := w.Finish()
	if err != nil {
		t.Fatal(err)
	}
	if n != 46+4*4 {
		t.Fatalf("Finish returned %d bytes, want %d", n, 46+4*4)
	}
	d, err := ReadWAV(path)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(d.Samples, []float32{0.25, -0.5, 1.0, 0.0}) {
		t.Fatalf("read back %v", d.Samples)
	}
	if st, _ := os.Stat(path); st.Size() != n {
		t.Fatalf("file is %d bytes, Finish said %d", st.Size(), n)
	}
}

func TestReadsPCM16Mono(t *testing.T) {
	le := binary.LittleEndian
	b := []byte("RIFF")
	b = le.AppendUint32(b, 40)
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 16)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint16(b, 1)
	b = le.AppendUint32(b, 8000)
	b = le.AppendUint32(b, 16000)
	b = le.AppendUint16(b, 2)
	b = le.AppendUint16(b, 16)
	b = append(b, "data"...)
	b = le.AppendUint32(b, 4)
	b = le.AppendUint16(b, uint16(0xC000)) // -16384
	b = le.AppendUint16(b, 16384)
	d, err := parseWAV(b)
	if err != nil {
		t.Fatal(err)
	}
	if d.SampleRate != 8000 || d.Channels != 1 || !slices.Equal(d.Samples, []float32{-0.5, 0.5}) {
		t.Fatalf("parsed %+v", d)
	}
	if d.DurationMS() != 0 {
		t.Fatalf("DurationMS() = %d, want 0", d.DurationMS())
	}
}

func TestReadsPCM24StereoAndExtensibleHeaders(t *testing.T) {
	le := binary.LittleEndian
	b := []byte("RIFF")
	b = le.AppendUint32(b, 0)
	b = append(b, "WAVEfmt "...)
	b = le.AppendUint32(b, 40)
	b = le.AppendUint16(b, 0xFFFE)
	b = le.AppendUint16(b, 2)
	b = le.AppendUint32(b, 44100)
	b = le.AppendUint32(b, 44100*6)
	b = le.AppendUint16(b, 6)
	b = le.AppendUint16(b, 24)
	b = le.AppendUint16(b, 22)
	b = le.AppendUint16(b, 24)
	b = le.AppendUint32(b, 3)
	b = le.AppendUint16(b, 1) // KSDATAFORMAT_SUBTYPE_PCM's first two bytes
	b = append(b, make([]byte, 14)...)
	b = append(b, "data"...)
	b = le.AppendUint32(b, 6)
	b = append(b, 0x00, 0x00, 0x40, 0x00, 0x00, 0xC0) // 0.5, -0.5
	d, err := parseWAV(b)
	if err != nil {
		t.Fatal(err)
	}
	if d.SampleRate != 44100 || d.Channels != 2 || !slices.Equal(d.Samples, []float32{0.5, -0.5}) {
		t.Fatalf("parsed %+v", d)
	}
}
