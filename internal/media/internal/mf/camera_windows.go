package mf

import (
	"errors"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	mfdll                                   = windows.NewLazySystemDLL("mf.dll")
	procMFEnumDeviceSources                 = mfdll.NewProc("MFEnumDeviceSources")
	procMFCreateSourceReaderFromMediaSource = mfreadwr.NewProc("MFCreateSourceReaderFromMediaSource")

	MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE                      = newGUID(0xc60ac5fe, 0x252a, 0x478f, [8]byte{0xa0, 0xef, 0xbc, 0x8f, 0xa5, 0xf7, 0xca, 0xd3})
	MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_GUID          = newGUID(0x8ac3587a, 0x4ae7, 0x42d8, [8]byte{0x99, 0xe0, 0x0a, 0x60, 0x13, 0xee, 0xf9, 0x0f})
	MF_DEVSOURCE_ATTRIBUTE_FRIENDLY_NAME                    = newGUID(0x60d0e559, 0x52f8, 0x4fa2, [8]byte{0xbb, 0xce, 0xac, 0xdb, 0x34, 0xa8, 0xec, 0x01})
	MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_SYMBOLIC_LINK = newGUID(0x58f0aad8, 0x22bf, 0x4f8a, [8]byte{0xbb, 0x3d, 0xd2, 0xc4, 0x97, 0x8c, 0x6e, 0x2f})
	MFVideoFormat_RGB32                                     = formatGUID(22) // D3DFMT_X8R8G8B8
	IID_IMFMediaSource                                      = newGUID(0x279a808d, 0xaec7, 0x40c8, [8]byte{0x9c, 0x6b, 0xa6, 0xb4, 0x92, 0xc7, 0x8a, 0x66})
)

const (
	slotActivateObject = 33
	slotSourceShutdown = 12
)

// Activate is IMFActivate.
type Activate struct{ Attributes }

// VideoCaptureDevices lists the cameras Media Foundation knows, in its order. The caller
// releases each.
func VideoCaptureDevices() ([]Activate, error) {
	attrs, err := CreateAttributes(1)
	if err != nil {
		return nil, err
	}
	defer attrs.Release()
	if err := attrs.SetGUID(&MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE, &MF_DEVSOURCE_ATTRIBUTE_SOURCE_TYPE_VIDCAP_GUID); err != nil {
		return nil, err
	}
	var list unsafe.Pointer
	var n uint32
	r, _, _ := procMFEnumDeviceSources.Call(attrs.this(), uintptr(unsafe.Pointer(&list)), uintptr(unsafe.Pointer(&n)))
	if err := check("MFEnumDeviceSources", r); err != nil {
		return nil, err
	}
	if list == nil {
		return nil, nil
	}
	defer windows.CoTaskMemFree(list)
	out := make([]Activate, n)
	for i, p := range unsafe.Slice((*unsafe.Pointer)(list), n) {
		out[i] = Activate{Attributes{Unknown{p}}}
	}
	return out, nil
}

// LiveSource is IMFMediaSource. Shut it down before releasing it.
type LiveSource struct{ Unknown }

func (a Activate) Source() (LiveSource, error) {
	const op = "IMFActivate::ActivateObject"
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(a.fn(slotActivateObject), a.this(), uintptr(unsafe.Pointer(&IID_IMFMediaSource)), uintptr(unsafe.Pointer(&out)))
	u, err := nonNil(op, out, check(op, r))
	return LiveSource{u}, err
}

func (s LiveSource) Shutdown() {
	syscall.SyscallN(s.fn(slotSourceShutdown), s.this())
}

// SourceReaderFor reads samples from a live source such as a camera.
func SourceReaderFor(source LiveSource, attrs Attributes) (SourceReader, error) {
	var out unsafe.Pointer
	r, _, _ := procMFCreateSourceReaderFromMediaSource.Call(source.this(), attrs.this(), uintptr(unsafe.Pointer(&out)))
	if err := check("MFCreateSourceReaderFromMediaSource", r); err != nil {
		return SourceReader{}, err
	}
	if out == nil {
		return SourceReader{}, errors.New("MFCreateSourceReaderFromMediaSource returned nothing")
	}
	return SourceReader{Unknown{out}}, nil
}
