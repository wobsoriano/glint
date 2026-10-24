package mf

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procMFStartup                   = mfplat.NewProc("MFStartup")
	procMFCreateAttributes          = mfplat.NewProc("MFCreateAttributes")
	procMFCreateMediaType           = mfplat.NewProc("MFCreateMediaType")
	procMFCreateSample              = mfplat.NewProc("MFCreateSample")
	procMFCreateMemoryBuffer        = mfplat.NewProc("MFCreateMemoryBuffer")
	procMFTEnumEx                   = mfplat.NewProc("MFTEnumEx")
	procMFCreateDXGIDeviceManager   = mfplat.NewProc("MFCreateDXGIDeviceManager")
	procMFCreateSinkWriterFromURL   = mfreadwr.NewProc("MFCreateSinkWriterFromURL")
	procMFCreateSourceReaderFromURL = mfreadwr.NewProc("MFCreateSourceReaderFromURL")
)

func create(op string, proc *windows.LazyProc) (Unknown, error) {
	var out unsafe.Pointer
	r, _, _ := proc.Call(uintptr(unsafe.Pointer(&out)))
	if err := check(op, r); err != nil {
		return Unknown{}, err
	}
	return Unknown{out}, nil
}

// Attributes is IMFAttributes.
type Attributes struct{ Unknown }

func CreateAttributes(size uint32) (Attributes, error) {
	var out unsafe.Pointer
	r, _, _ := procMFCreateAttributes.Call(uintptr(unsafe.Pointer(&out)), uintptr(size))
	if err := check("MFCreateAttributes", r); err != nil {
		return Attributes{}, err
	}
	return Attributes{Unknown{out}}, nil
}

func (a Attributes) GetUINT32(key *GUID) (uint32, error) {
	var v uint32
	r, _, _ := syscall.SyscallN(a.fn(7), a.this(), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v)))
	return v, check("IMFAttributes::GetUINT32", r)
}

func (a Attributes) GetUINT64(key *GUID) (uint64, error) {
	var v uint64
	r, _, _ := syscall.SyscallN(a.fn(8), a.this(), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v)))
	return v, check("IMFAttributes::GetUINT64", r)
}

func (a Attributes) GetGUID(key *GUID) (GUID, error) {
	var v GUID
	r, _, _ := syscall.SyscallN(a.fn(10), a.this(), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v)))
	return v, check("IMFAttributes::GetGUID", r)
}

// GetString is the attribute's string value.
func (a Attributes) GetString(key *GUID) (string, error) {
	var p *uint16
	var n uint32
	r, _, _ := syscall.SyscallN(a.fn(13), a.this(), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&p)), uintptr(unsafe.Pointer(&n)))
	if err := check("IMFAttributes::GetAllocatedString", r); err != nil {
		return "", err
	}
	return takeString(p), nil
}

func (a Attributes) SetUINT32(key *GUID, v uint32) error {
	r, _, _ := syscall.SyscallN(a.fn(21), a.this(), uintptr(unsafe.Pointer(key)), uintptr(v))
	return check("IMFAttributes::SetUINT32", r)
}

func (a Attributes) SetUINT64(key *GUID, v uint64) error {
	r, _, _ := syscall.SyscallN(a.fn(22), a.this(), uintptr(unsafe.Pointer(key)), uintptr(v))
	return check("IMFAttributes::SetUINT64", r)
}

func (a Attributes) SetGUID(key, v *GUID) error {
	r, _, _ := syscall.SyscallN(a.fn(24), a.this(), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(v)))
	return check("IMFAttributes::SetGUID", r)
}

func (a Attributes) SetUnknown(key *GUID, v Unknown) error {
	r, _, _ := syscall.SyscallN(a.fn(27), a.this(), uintptr(unsafe.Pointer(key)), v.this())
	return check("IMFAttributes::SetUnknown", r)
}

// Pack2 packs a (width, height) or (numerator, denominator) pair the way MF_MT_FRAME_SIZE and
// MF_MT_FRAME_RATE store it.
func Pack2(hi, lo uint32) uint64 { return uint64(hi)<<32 | uint64(lo) }

func Unpack2(v uint64) (hi, lo uint32) { return uint32(v >> 32), uint32(v) }

// MediaType is IMFMediaType.
type MediaType struct{ Attributes }

func CreateMediaType() (MediaType, error) {
	u, err := create("MFCreateMediaType", procMFCreateMediaType)
	return MediaType{Attributes{u}}, err
}

// TypeSetting is one attribute of a media type being built.
type TypeSetting struct {
	Key   *GUID
	Value any // uint32, uint64 or *GUID
}

// NewMediaType builds a media type from settings, in order.
func NewMediaType(settings ...TypeSetting) (MediaType, error) {
	t, err := CreateMediaType()
	if err != nil {
		return t, err
	}
	for _, s := range settings {
		switch v := s.Value.(type) {
		case uint32:
			err = t.SetUINT32(s.Key, v)
		case uint64:
			err = t.SetUINT64(s.Key, v)
		case *GUID:
			err = t.SetGUID(s.Key, v)
		default:
			panic("mf: unsupported media type value")
		}
		if err != nil {
			t.Release()
			return MediaType{}, err
		}
	}
	return t, nil
}

// Sample is IMFSample.
type Sample struct{ Attributes }

func CreateSample() (Sample, error) {
	u, err := create("MFCreateSample", procMFCreateSample)
	return Sample{Attributes{u}}, err
}

func (s Sample) SampleTime() (int64, error) {
	var t int64
	r, _, _ := syscall.SyscallN(s.fn(35), s.this(), uintptr(unsafe.Pointer(&t)))
	return t, check("IMFSample::GetSampleTime", r)
}

func (s Sample) SetSampleTime(t int64) error {
	r, _, _ := syscall.SyscallN(s.fn(36), s.this(), uintptr(t))
	return check("IMFSample::SetSampleTime", r)
}

func (s Sample) SetSampleDuration(d int64) error {
	r, _, _ := syscall.SyscallN(s.fn(38), s.this(), uintptr(d))
	return check("IMFSample::SetSampleDuration", r)
}

func (s Sample) BufferByIndex(i uint32) (MediaBuffer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(s.fn(40), s.this(), uintptr(i), uintptr(unsafe.Pointer(&out)))
	return MediaBuffer{Unknown{out}}, check("IMFSample::GetBufferByIndex", r)
}

func (s Sample) ContiguousBuffer() (MediaBuffer, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(s.fn(41), s.this(), uintptr(unsafe.Pointer(&out)))
	return MediaBuffer{Unknown{out}}, check("IMFSample::ConvertToContiguousBuffer", r)
}

func (s Sample) AddBuffer(b MediaBuffer) error {
	r, _, _ := syscall.SyscallN(s.fn(42), s.this(), b.this())
	return check("IMFSample::AddBuffer", r)
}

// MediaBuffer is IMFMediaBuffer.
type MediaBuffer struct{ Unknown }

func CreateMemoryBuffer(size uint32) (MediaBuffer, error) {
	var out unsafe.Pointer
	r, _, _ := procMFCreateMemoryBuffer.Call(uintptr(size), uintptr(unsafe.Pointer(&out)))
	return MediaBuffer{Unknown{out}}, check("MFCreateMemoryBuffer", r)
}

// Lock maps the buffer's memory: its first maxLen bytes are writable and the first curLen
// hold data. The memory belongs to the buffer and is valid until Unlock.
func (b MediaBuffer) Lock() (data unsafe.Pointer, maxLen, curLen uint32, err error) {
	r, _, _ := syscall.SyscallN(b.fn(3), b.this(), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&maxLen)), uintptr(unsafe.Pointer(&curLen)))
	return data, maxLen, curLen, check("IMFMediaBuffer::Lock", r)
}

func (b MediaBuffer) Unlock() error {
	r, _, _ := syscall.SyscallN(b.fn(4), b.this())
	return check("IMFMediaBuffer::Unlock", r)
}

func (b MediaBuffer) SetCurrentLength(n uint32) error {
	r, _, _ := syscall.SyscallN(b.fn(6), b.this(), uintptr(n))
	return check("IMFMediaBuffer::SetCurrentLength", r)
}

// Buffer2D is IMF2DBuffer.
type Buffer2D struct{ Unknown }

func (b MediaBuffer) As2D() (Buffer2D, bool) {
	u, err := b.QueryInterface(&IID_IMF2DBuffer)
	return Buffer2D{u}, err == nil
}

// Lock2D maps the buffer: scan0 is the first row and pitch the bytes from one row to the next.
func (b Buffer2D) Lock2D() (scan0 unsafe.Pointer, pitch int32, err error) {
	r, _, _ := syscall.SyscallN(b.fn(3), b.this(), uintptr(unsafe.Pointer(&scan0)), uintptr(unsafe.Pointer(&pitch)))
	return scan0, pitch, check("IMF2DBuffer::Lock2D", r)
}

func (b Buffer2D) Unlock2D() error {
	r, _, _ := syscall.SyscallN(b.fn(4), b.this())
	return check("IMF2DBuffer::Unlock2D", r)
}

func (b Buffer2D) ContiguousLength() (uint32, error) {
	var n uint32
	r, _, _ := syscall.SyscallN(b.fn(7), b.this(), uintptr(unsafe.Pointer(&n)))
	return n, check("IMF2DBuffer::GetContiguousLength", r)
}

// DXGIBuffer is IMFDXGIBuffer.
type DXGIBuffer struct{ Unknown }

func (b MediaBuffer) AsDXGI() (DXGIBuffer, bool) {
	u, err := b.QueryInterface(&IID_IMFDXGIBuffer)
	return DXGIBuffer{u}, err == nil
}

func (b DXGIBuffer) Texture() (Texture2D, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(b.fn(3), b.this(), uintptr(unsafe.Pointer(&IID_ID3D11Texture2D)), uintptr(unsafe.Pointer(&out)))
	return Texture2D{Unknown{out}}, check("IMFDXGIBuffer::GetResource", r)
}

func (b DXGIBuffer) SubresourceIndex() (uint32, error) {
	var i uint32
	r, _, _ := syscall.SyscallN(b.fn(4), b.this(), uintptr(unsafe.Pointer(&i)))
	return i, check("IMFDXGIBuffer::GetSubresourceIndex", r)
}

// Transform is IMFTransform.
type Transform struct{ Unknown }

func (t Transform) Attributes() (Attributes, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(t.fn(8), t.this(), uintptr(unsafe.Pointer(&out)))
	return Attributes{Unknown{out}}, check("IMFTransform::GetAttributes", r)
}

// CodecAPI is ICodecAPI.
type CodecAPI struct{ Unknown }

func (c CodecAPI) SetUINT32(api *GUID, v uint32) error {
	value := PropVariant{VT: VT_UI4, Val: int64(v)}
	r, _, _ := syscall.SyscallN(c.fn(9), c.this(), uintptr(unsafe.Pointer(api)), uintptr(unsafe.Pointer(&value)))
	return check("ICodecAPI::SetValue", r)
}

// TypeInfo is MFT_REGISTER_TYPE_INFO.
type TypeInfo struct {
	Major, Sub GUID
}

const (
	MFT_ENUM_FLAG_SYNCMFT       = 0x01
	MFT_ENUM_FLAG_ASYNCMFT      = 0x02
	MFT_ENUM_FLAG_HARDWARE      = 0x04
	MFT_ENUM_FLAG_SORTANDFILTER = 0x40
)

// CountTransforms is how many transforms of category accept input and produce output.
func CountTransforms(category GUID, flags uint32, input, output *TypeInfo) (int, error) {
	var activates unsafe.Pointer
	var n uint32
	// The x64 ABI passes the 16-byte category by reference to a caller-owned copy.
	r, _, _ := procMFTEnumEx.Call(uintptr(unsafe.Pointer(&category)), uintptr(flags), uintptr(unsafe.Pointer(input)),
		uintptr(unsafe.Pointer(output)), uintptr(unsafe.Pointer(&activates)), uintptr(unsafe.Pointer(&n)))
	if err := check("MFTEnumEx", r); err != nil {
		return 0, err
	}
	if activates != nil {
		for _, p := range unsafe.Slice((*unsafe.Pointer)(activates), n) {
			Unknown{p}.Release()
		}
		windows.CoTaskMemFree(activates)
	}
	return int(n), nil
}
