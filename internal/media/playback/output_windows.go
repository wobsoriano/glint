package playback

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")

	clsidMMDeviceEnumerator = windows.GUID{Data1: 0xBCDE0395, Data2: 0xE52F, Data3: 0x467C, Data4: [8]byte{0x8E, 0x3D, 0xC4, 0x57, 0x92, 0x91, 0x69, 0x2E}}
	iidIMMDeviceEnumerator  = windows.GUID{Data1: 0xA95664D2, Data2: 0x9614, Data3: 0x4F35, Data4: [8]byte{0xA7, 0x46, 0xDE, 0x8D, 0xB6, 0x36, 0x17, 0xE6}}
	iidIAudioClient         = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioRenderClient   = windows.GUID{Data1: 0xF294ACFC, Data2: 0x3146, Data3: 0x4483, Data4: [8]byte{0xA7, 0xBF, 0xAD, 0xDC, 0xA7, 0xC2, 0x60, 0xE2}}
	iidIAudioClock          = windows.GUID{Data1: 0xCD63314F, Data2: 0x3FBA, Data3: 0x4A1B, Data4: [8]byte{0x81, 0x2C, 0xEF, 0x96, 0x35, 0x87, 0x28, 0xE7}}
	subtypeIEEEFloat        = windows.GUID{Data1: 0x00000003, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

const (
	clsctxAll       = 0x17
	rpcEChangedMode = 0x80010106
	coinitMTA       = 0
	eRender         = 0
	eConsole        = 0

	shareModeShared              = 0
	streamFlagsAutoConvertPCM    = 0x80000000
	streamFlagsSrcDefaultQuality = 0x08000000
	// bufferDuration is 100 ms in 100-nanosecond units: room for the feed loop to sleep.
	bufferDuration       = 1_000_000
	waveFormatExtensible = 0xFFFE
)

// IMMDeviceEnumerator, IMMDevice, IAudioClient, IAudioRenderClient and IAudioClock vtable slots.
const (
	enumGetDefaultAudioEndpoint = 4
	devActivate                 = 3
	clientInitialize            = 3
	clientGetBufferSize         = 4
	clientGetCurrentPadding     = 6
	clientStart                 = 10
	clientStop                  = 11
	clientGetService            = 14
	renderGetBuffer             = 3
	renderReleaseBuffer         = 4
	clockGetFrequency           = 3
	clockGetPosition            = 4
)

type comObject struct{ vtbl *[32]uintptr }

func (o *comObject) call(method int, args ...uintptr) hresult {
	r, _, _ := syscall.SyscallN(o.vtbl[method], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return hresult(r)
}

func (o *comObject) release() {
	if o != nil {
		o.call(2)
	}
}

type hresult uint32

func (h hresult) err(what string) error {
	if int32(h) >= 0 {
		return nil
	}
	return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(h))
}

// waveFormat is WAVEFORMATEXTENSIBLE.
type waveFormat struct {
	tag         uint16
	channels    uint16
	rate        uint32
	avgBytes    uint32
	blockAlign  uint16
	bits        uint16
	cbSize      uint16
	validBits   uint16
	channelMask uint32
	subFormat   windows.GUID
}

type stream struct {
	client, render, clock *comObject
	frequency             uint64
	bufferFrames          uint32
}

func (o *AudioOutput) start(fill func([]float32)) error {
	opened := make(chan error, 1)
	go func() {
		defer close(o.done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		r, _, _ := procCoInitializeEx.Call(0, coinitMTA)
		if h := hresult(r); h != rpcEChangedMode {
			if err := h.err("CoInitializeEx"); err != nil {
				opened <- err
				return
			}
			defer windows.CoUninitialize()
		}
		s, err := openStream()
		if err != nil {
			opened <- err
			return
		}
		defer s.close()
		if err := s.feed(fill); err != nil {
			opened <- err
			return
		}
		if err := s.client.call(clientStart).err("IAudioClient::Start"); err != nil {
			opened <- err
			return
		}
		opened <- nil
		if err := s.run(o, fill); err != nil {
			o.fail(err)
		}
	}()
	if err := <-opened; err != nil {
		<-o.done
		return err
	}
	return nil
}

func openStream() (*stream, error) {
	var en *comObject
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidMMDeviceEnumerator)), 0, clsctxAll, uintptr(unsafe.Pointer(&iidIMMDeviceEnumerator)), uintptr(unsafe.Pointer(&en)))
	if err := hresult(r).err("CoCreateInstance(MMDeviceEnumerator)"); err != nil {
		return nil, err
	}
	defer en.release()
	var dev *comObject
	if err := en.call(enumGetDefaultAudioEndpoint, eRender, eConsole, uintptr(unsafe.Pointer(&dev))).err("no default output device"); err != nil {
		return nil, err
	}
	defer dev.release()
	s := &stream{}
	if err := dev.call(devActivate, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&s.client))).err("IAudioClient"); err != nil {
		return nil, err
	}
	format := waveFormat{
		tag: waveFormatExtensible, channels: Channels, rate: SampleRate,
		avgBytes: SampleRate * Channels * 4, blockAlign: Channels * 4, bits: 32, cbSize: 22,
		validBits: 32, channelMask: 0x3, subFormat: subtypeIEEEFloat,
	}
	steps := []struct {
		what string
		h    func() hresult
	}{
		{"IAudioClient::Initialize 48 kHz stereo float", func() hresult {
			return s.client.call(clientInitialize, shareModeShared, streamFlagsAutoConvertPCM|streamFlagsSrcDefaultQuality, bufferDuration, 0, uintptr(unsafe.Pointer(&format)), 0)
		}},
		{"GetBufferSize", func() hresult { return s.client.call(clientGetBufferSize, uintptr(unsafe.Pointer(&s.bufferFrames))) }},
		{"IAudioRenderClient", func() hresult {
			return s.client.call(clientGetService, uintptr(unsafe.Pointer(&iidIAudioRenderClient)), uintptr(unsafe.Pointer(&s.render)))
		}},
		{"IAudioClock", func() hresult {
			return s.client.call(clientGetService, uintptr(unsafe.Pointer(&iidIAudioClock)), uintptr(unsafe.Pointer(&s.clock)))
		}},
		{"IAudioClock::GetFrequency", func() hresult { return s.clock.call(clockGetFrequency, uintptr(unsafe.Pointer(&s.frequency))) }},
	}
	for _, step := range steps {
		if err := step.h().err(step.what); err != nil {
			s.close()
			return nil, err
		}
	}
	return s, nil
}

func (s *stream) close() {
	s.clock.release()
	s.render.release()
	s.client.release()
}

func (s *stream) run(o *AudioOutput, fill func([]float32)) error {
	defer s.client.call(clientStop)
	for {
		select {
		case <-o.stop:
			return nil
		default:
		}
		if err := s.feed(fill); err != nil {
			return err
		}
		var pos uint64
		if err := s.clock.call(clockGetPosition, uintptr(unsafe.Pointer(&pos)), 0).err("IAudioClock::GetPosition"); err != nil {
			return err
		}
		o.setPosition(float64(pos) * 1000 / float64(max(s.frequency, 1)))
		time.Sleep(4 * time.Millisecond)
	}
}

// feed fills whatever room the device buffer has.
func (s *stream) feed(fill func([]float32)) error {
	var padding uint32
	if err := s.client.call(clientGetCurrentPadding, uintptr(unsafe.Pointer(&padding))).err("GetCurrentPadding"); err != nil {
		return err
	}
	free := s.bufferFrames - padding
	if free == 0 {
		return nil
	}
	var data *float32
	if err := s.render.call(renderGetBuffer, uintptr(free), uintptr(unsafe.Pointer(&data))).err("IAudioRenderClient::GetBuffer"); err != nil {
		return err
	}
	fill(unsafe.Slice(data, int(free)*Channels))
	return s.render.call(renderReleaseBuffer, uintptr(free), 0).err("IAudioRenderClient::ReleaseBuffer")
}
