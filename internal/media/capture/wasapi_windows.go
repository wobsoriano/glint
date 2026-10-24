package capture

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	iidIAudioClient        = windows.GUID{Data1: 0x1CB9AD4C, Data2: 0xDBFA, Data3: 0x4C32, Data4: [8]byte{0xB1, 0x78, 0xC2, 0xF5, 0x68, 0xA7, 0x03, 0xB2}}
	iidIAudioCaptureClient = windows.GUID{Data1: 0xC8ADBD64, Data2: 0xE71E, Data3: 0x48A0, Data4: [8]byte{0xA4, 0xDE, 0x18, 0x5C, 0x39, 0x5C, 0xD3, 0x17}}
	subtypePCM             = windows.GUID{Data1: 0x00000001, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
	subtypeIEEEFloat       = windows.GUID{Data1: 0x00000003, Data2: 0x0000, Data3: 0x0010, Data4: [8]byte{0x80, 0x00, 0x00, 0xAA, 0x00, 0x38, 0x9B, 0x71}}
)

const (
	streamFlagsLoopback          = 0x00020000
	streamFlagsAutoConvertPCM    = 0x80000000
	streamFlagsSrcDefaultQuality = 0x08000000
	bufferFlagsSilent            = 0x2
	bufferFlagsTimestampError    = 0x4
	shareModeShared              = 0
	bufferDuration100ns          = 2_000_000

	waveFormatPCM        = 1
	waveFormatIEEEFloat  = 3
	waveFormatExtensible = 0xFFFE
)

// IAudioClient and IAudioCaptureClient vtable slots.
const (
	clientInitialize     = 3
	clientGetMixFormat   = 8
	clientStart          = 10
	clientStop           = 11
	clientGetService     = 14
	captureGetBuffer     = 3
	captureReleaseBuffer = 4
	captureGetNextPacket = 5
)

// waveFormat is WAVEFORMATEXTENSIBLE. A plain WAVEFORMATEX is its first 18 bytes, so the
// extensible fields are only read when tag says they exist.
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

type deviceFormat struct {
	rate, channels, bits int
	float                bool
}

func floatFormat(rate, channels int) waveFormat {
	mask := uint32(0x3)
	if channels == 1 {
		mask = 0x4
	}
	return waveFormat{
		tag: waveFormatExtensible, channels: uint16(channels), rate: uint32(rate),
		avgBytes: uint32(rate * channels * 4), blockAlign: uint16(channels * 4), bits: 32, cbSize: 22,
		validBits: 32, channelMask: mask, subFormat: subtypeIEEEFloat,
	}
}

func parseFormat(f *waveFormat) (deviceFormat, error) {
	d := deviceFormat{rate: int(f.rate), channels: int(f.channels), bits: int(f.bits)}
	switch f.tag {
	case waveFormatExtensible:
		switch f.subFormat {
		case subtypeIEEEFloat:
			d.float = true
		case subtypePCM:
		default:
			return d, fmt.Errorf("unsupported mix format subtype %v", f.subFormat)
		}
	case waveFormatIEEEFloat:
		d.float = true
	case waveFormatPCM:
	default:
		return d, fmt.Errorf("unsupported mix format tag %d", f.tag)
	}
	return d, nil
}

// StartTrack opens src and records it to path with channels channels at SampleRate. Each
// converted buffer is also offered to tap, when set; a full tap drops the buffer and the
// recording never waits.
func StartTrack(src Source, path string, channels int, clock *Clock, tap chan<- []float32) (*Track, error) {
	r, err := startCapture(src, channels, tap, func() (*clockedWAV, error) { return createClockedWAV(path, channels, clock) })
	if err != nil {
		return nil, err
	}
	return &Track{Path: path, r: r}, nil
}

// StartMicMonitor meters id, or the default microphone when id is empty.
func StartMicMonitor(id DeviceID) (*MicMonitor, error) {
	r, err := startCapture(Microphone(id), 1, nil, func() (*clockedWAV, error) { return nil, nil })
	if err != nil {
		return nil, err
	}
	return &MicMonitor{r: r}, nil
}

// startCapture opens the device on the capture goroutine's own locked thread and returns
// once it is open or failed to open.
func startCapture(src Source, channels int, tap chan<- []float32, makeSink func() (*clockedWAV, error)) (*runner, error) {
	r := &runner{done: make(chan struct{})}
	opened := make(chan error, 1)
	go func() {
		defer close(r.done)
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		leave, err := enterCOM()
		if err != nil {
			opened <- err
			return
		}
		defer leave()
		c, err := openCapture(src, channels)
		if err != nil {
			opened <- fmt.Errorf("%v: %w", src, err)
			return
		}
		defer c.close()
		sink, err := makeSink()
		if err != nil {
			opened <- err
			return
		}
		opened <- nil
		r.stats, r.err = c.run(sink, r, tap)
	}()
	if err := <-opened; err != nil {
		<-r.done
		return nil, err
	}
	return r, nil
}

type wasapiCapture struct {
	client, capture *comObject
	format          deviceFormat
	conv            *converter
	decoded         []float32
}

func openCapture(src Source, outChannels int) (*wasapiCapture, error) {
	en, err := coCreate(&clsidMMDeviceEnumerator, &iidIMMDeviceEnumerator)
	if err != nil {
		return nil, err
	}
	defer en.release()
	dev, err := openEndpoint(en, src)
	if err != nil {
		return nil, err
	}
	defer dev.release()

	flags := uintptr(streamFlagsAutoConvertPCM | streamFlagsSrcDefaultQuality)
	if src.loopback {
		flags |= streamFlagsLoopback
	}
	client, err := activateClient(dev)
	if err != nil {
		return nil, err
	}
	wanted := floatFormat(SampleRate, outChannels)
	format := deviceFormat{rate: SampleRate, channels: outChannels, bits: 32, float: true}
	if client.call(clientInitialize, shareModeShared, flags, bufferDuration100ns, 0, uintptr(unsafe.Pointer(&wanted)), 0).failed() {
		// The engine would not convert for us: take the mix format and convert here.
		client.release()
		if client, err = activateClient(dev); err != nil {
			return nil, err
		}
		if format, err = initMixFormat(client, flags&^(streamFlagsAutoConvertPCM|streamFlagsSrcDefaultQuality)); err != nil {
			client.release()
			return nil, err
		}
	}
	var capture *comObject
	if err := client.call(clientGetService, uintptr(unsafe.Pointer(&iidIAudioCaptureClient)), uintptr(unsafe.Pointer(&capture))).err("IAudioCaptureClient"); err != nil {
		client.release()
		return nil, err
	}
	if err := client.call(clientStart).err("IAudioClient::Start"); err != nil {
		capture.release()
		client.release()
		return nil, err
	}
	return &wasapiCapture{client: client, capture: capture, format: format, conv: newConverter(format.rate, format.channels, outChannels)}, nil
}

func activateClient(dev *comObject) (*comObject, error) {
	var client *comObject
	if err := dev.call(devActivate, uintptr(unsafe.Pointer(&iidIAudioClient)), clsctxAll, 0, uintptr(unsafe.Pointer(&client))).err("IAudioClient"); err != nil {
		return nil, err
	}
	return client, nil
}

func initMixFormat(client *comObject, flags uintptr) (deviceFormat, error) {
	var mix *waveFormat
	if err := client.call(clientGetMixFormat, uintptr(unsafe.Pointer(&mix))).err("GetMixFormat"); err != nil {
		return deviceFormat{}, err
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(mix))
	format, err := parseFormat(mix)
	if err != nil {
		return format, err
	}
	return format, client.call(clientInitialize, shareModeShared, flags, bufferDuration100ns, 0, uintptr(unsafe.Pointer(mix)), 0).err("IAudioClient::Initialize with mix format")
}

func (c *wasapiCapture) close() {
	c.capture.release()
	c.client.release()
}

func (c *wasapiCapture) run(sink *clockedWAV, r *runner, tap chan<- []float32) (Stats, error) {
	var out []float32
	// next is where the packet after this one starts, standing in for a timestamp the
	// device marks as wrong.
	next := Now()
	err := func() error {
		for !r.stop.Load() {
			gotAny := false
			for {
				var packet uint32
				if err := c.capture.call(captureGetNextPacket, uintptr(unsafe.Pointer(&packet))).err("GetNextPacketSize"); err != nil {
					return err
				}
				if packet == 0 {
					break
				}
				gotAny = true
				var data *byte
				var frames, flags uint32
				var stamp Instant
				if err := c.capture.call(captureGetBuffer, uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&frames)), uintptr(unsafe.Pointer(&flags)), 0, uintptr(unsafe.Pointer(&stamp))).err("GetBuffer"); err != nil {
					return err
				}
				if flags&bufferFlagsTimestampError != 0 {
					stamp = next
				}
				next = stamp + Instant(int64(frames)*1e7/int64(c.format.rate))
				var raw []byte
				if flags&bufferFlagsSilent == 0 && data != nil {
					raw = unsafe.Slice(data, int(frames)*c.format.channels*c.format.bits/8)
				}
				out = c.conv.push(c.decode(raw, int(frames)), out[:0])
				if err := c.capture.call(captureReleaseBuffer, uintptr(frames)).err("ReleaseBuffer"); err != nil {
					return err
				}
				r.level.update(out)
				if tap != nil {
					select {
					case tap <- append([]float32(nil), out...):
					default:
					}
				}
				if sink != nil {
					if err := sink.write(out, stamp); err != nil {
						return err
					}
				}
			}
			if !gotAny {
				time.Sleep(5 * time.Millisecond)
			}
		}
		return nil
	}()
	stopErr := c.client.call(clientStop).err("IAudioClient::Stop")
	if sink == nil {
		return Stats{}, errors.Join(err, stopErr)
	}
	stats, finishErr := sink.finish()
	return stats, errors.Join(err, finishErr)
}

// decode turns the device's samples into float32; raw is nil for a silent packet.
func (c *wasapiCapture) decode(raw []byte, frames int) []float32 {
	f := c.format
	n := frames * f.channels
	c.decoded = c.decoded[:0]
	if raw == nil {
		for range n {
			c.decoded = append(c.decoded, 0)
		}
		return c.decoded
	}
	le := binary.LittleEndian
	width := f.bits / 8
	for i := range n {
		b := raw[i*width : i*width+width]
		var s float32
		switch {
		case f.float && f.bits == 32:
			s = math.Float32frombits(le.Uint32(b))
		case !f.float && f.bits == 16:
			s = float32(int16(le.Uint16(b))) / 32768
		case !f.float && f.bits == 32:
			s = float32(int32(le.Uint32(b))) / 2147483648
		case !f.float && f.bits == 24:
			s = float32(int32(uint32(b[0])<<8|uint32(b[1])<<16|uint32(b[2])<<24)>>8) / 8388608
		}
		c.decoded = append(c.decoded, s)
	}
	return c.decoded
}
