package main

import (
	"fmt"
	"math"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	winmm                    = windows.NewLazySystemDLL("winmm.dll")
	procWaveOutOpen          = winmm.NewProc("waveOutOpen")
	procWaveOutPrepareHeader = winmm.NewProc("waveOutPrepareHeader")
	procWaveOutWrite         = winmm.NewProc("waveOutWrite")
	procWaveOutGetPosition   = winmm.NewProc("waveOutGetPosition")
	procWaveOutReset         = winmm.NewProc("waveOutReset")
	procWaveOutUnprepare     = winmm.NewProc("waveOutUnprepareHeader")
	procWaveOutClose         = winmm.NewProc("waveOutClose")
)

const toneRate = 48_000

type waveHdr struct {
	data          *int16
	bufferLength  uint32
	bytesRecorded uint32
	user          uintptr
	flags         uint32
	loops         uint32
	next          uintptr
	reserved      uintptr
}

// tones plays one buffer through the default output: silence with a 1 kHz burst at each
// offset. Its playback position is the clock the stage's flash follows, so a flash and
// its burst leave the speakers together whatever the output latency.
type tones struct {
	h      uintptr
	hdr    *waveHdr
	buf    []int16
	bursts []time.Duration
	length time.Duration
}

func playTones(total time.Duration, bursts []time.Duration, length time.Duration) (*tones, error) {
	frames := int(total.Seconds() * toneRate)
	t := &tones{buf: make([]int16, frames*2), bursts: bursts, length: length}
	for _, b := range bursts {
		start := int(b.Seconds() * toneRate)
		for i := start; i < min(start+int(length.Seconds()*toneRate), frames); i++ {
			v := int16(0.5 * 32767 * math.Sin(2*math.Pi*1000*float64(i-start)/toneRate))
			t.buf[2*i], t.buf[2*i+1] = v, v
		}
	}
	format := struct {
		tag, channels    uint16
		rate, bytesPerS  uint32
		align, bits, ext uint16
	}{tag: 1, channels: 2, rate: toneRate, bytesPerS: toneRate * 4, align: 4, bits: 16}
	if r, _, _ := procWaveOutOpen.Call(uintptr(unsafe.Pointer(&t.h)), 0xFFFFFFFF, uintptr(unsafe.Pointer(&format)), 0, 0, 0); r != 0 {
		return nil, fmt.Errorf("waveOutOpen: MMRESULT %d", r)
	}
	t.hdr = &waveHdr{data: &t.buf[0], bufferLength: uint32(len(t.buf) * 2)}
	if r, _, _ := procWaveOutPrepareHeader.Call(t.h, uintptr(unsafe.Pointer(t.hdr)), unsafe.Sizeof(*t.hdr)); r != 0 {
		procWaveOutClose.Call(t.h)
		return nil, fmt.Errorf("waveOutPrepareHeader: MMRESULT %d", r)
	}
	if r, _, _ := procWaveOutWrite.Call(t.h, uintptr(unsafe.Pointer(t.hdr)), unsafe.Sizeof(*t.hdr)); r != 0 {
		t.close()
		return nil, fmt.Errorf("waveOutWrite: MMRESULT %d", r)
	}
	return t, nil
}

// position is how far playback has got.
func (t *tones) position() time.Duration {
	mm := struct {
		kind   uint32
		sample uint32
		_      uint32
	}{kind: 2} // TIME_SAMPLES
	procWaveOutGetPosition.Call(t.h, uintptr(unsafe.Pointer(&mm)), unsafe.Sizeof(mm))
	return time.Duration(mm.sample) * time.Second / toneRate
}

// sounding says whether a burst is playing now.
func (t *tones) sounding() bool {
	p := t.position()
	for _, b := range t.bursts {
		if p >= b && p < b+t.length {
			return true
		}
	}
	return false
}

func (t *tones) close() {
	procWaveOutReset.Call(t.h)
	procWaveOutUnprepare.Call(t.h, uintptr(unsafe.Pointer(t.hdr)), unsafe.Sizeof(*t.hdr))
	procWaveOutClose.Call(t.h)
}
