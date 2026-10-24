package mf

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SinkWriter is IMFSinkWriter.
type SinkWriter struct{ Unknown }

func CreateSinkWriter(path string, attrs Attributes) (SinkWriter, error) {
	url, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return SinkWriter{}, err
	}
	var out unsafe.Pointer
	r, _, _ := procMFCreateSinkWriterFromURL.Call(uintptr(unsafe.Pointer(url)), 0, attrs.this(), uintptr(unsafe.Pointer(&out)))
	return SinkWriter{Unknown{out}}, check("MFCreateSinkWriterFromURL", r)
}

func (w SinkWriter) AddStream(t MediaType) (uint32, error) {
	var stream uint32
	r, _, _ := syscall.SyscallN(w.fn(3), w.this(), t.this(), uintptr(unsafe.Pointer(&stream)))
	return stream, check("IMFSinkWriter::AddStream", r)
}

// SetInputMediaType sets what the stream is fed. params, which may be nil, holds CODECAPI_*
// settings for the encoder the writer picks.
func (w SinkWriter) SetInputMediaType(stream uint32, t MediaType, params Attributes) error {
	r, _, _ := syscall.SyscallN(w.fn(4), w.this(), uintptr(stream), t.this(), params.this())
	return check("IMFSinkWriter::SetInputMediaType", r)
}

func (w SinkWriter) BeginWriting() error {
	r, _, _ := syscall.SyscallN(w.fn(5), w.this())
	return check("IMFSinkWriter::BeginWriting", r)
}

func (w SinkWriter) WriteSample(stream uint32, s Sample) error {
	r, _, _ := syscall.SyscallN(w.fn(6), w.this(), uintptr(stream), s.this())
	return check("IMFSinkWriter::WriteSample", r)
}

func (w SinkWriter) Finalize() error {
	r, _, _ := syscall.SyscallN(w.fn(11), w.this())
	return check("IMFSinkWriter::Finalize", r)
}

// Service is the stream's iid interface, looked up on the encoder or the sink itself.
func (w SinkWriter) Service(stream uint32, iid *GUID) (Unknown, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(w.fn(12), w.this(), uintptr(stream), uintptr(unsafe.Pointer(&GUID_NULL)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := check("IMFSinkWriter::GetServiceForStream", r); err != nil {
		return Unknown{}, err
	}
	return Unknown{out}, nil
}

// Source reader stream indices and ReadSample flags, from mfreadwrite.h.
const (
	FirstVideoStream = 0xFFFFFFFC
	FirstAudioStream = 0xFFFFFFFD
	AllStreams       = 0xFFFFFFFE
	MediaSource      = 0xFFFFFFFF

	ReaderFlagError            = 0x1
	ReaderFlagEndOfStream      = 0x2
	ReaderFlagMediaTypeChanged = 0x20
)

// SourceReader is IMFSourceReader.
type SourceReader struct{ Unknown }

func CreateSourceReader(path string, attrs Attributes) (SourceReader, error) {
	url, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return SourceReader{}, err
	}
	var out unsafe.Pointer
	r, _, _ := procMFCreateSourceReaderFromURL.Call(uintptr(unsafe.Pointer(url)), attrs.this(), uintptr(unsafe.Pointer(&out)))
	return SourceReader{Unknown{out}}, check("MFCreateSourceReaderFromURL", r)
}

func (s SourceReader) SetStreamSelection(stream uint32, selected bool) error {
	var b uintptr
	if selected {
		b = 1
	}
	r, _, _ := syscall.SyscallN(s.fn(4), s.this(), uintptr(stream), b)
	return check("IMFSourceReader::SetStreamSelection", r)
}

func (s SourceReader) NativeMediaType(stream, index uint32) (MediaType, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(s.fn(5), s.this(), uintptr(stream), uintptr(index), uintptr(unsafe.Pointer(&out)))
	return MediaType{Attributes{Unknown{out}}}, check("IMFSourceReader::GetNativeMediaType", r)
}

func (s SourceReader) CurrentMediaType(stream uint32) (MediaType, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(s.fn(6), s.this(), uintptr(stream), uintptr(unsafe.Pointer(&out)))
	return MediaType{Attributes{Unknown{out}}}, check("IMFSourceReader::GetCurrentMediaType", r)
}

func (s SourceReader) SetCurrentMediaType(stream uint32, t MediaType) error {
	r, _, _ := syscall.SyscallN(s.fn(7), s.this(), uintptr(stream), 0, t.this())
	return check("IMFSourceReader::SetCurrentMediaType", r)
}

// SetPosition seeks to t in 100-nanosecond units.
func (s SourceReader) SetPosition(t int64) error {
	pos := PropVariant{VT: VT_I8, Val: t}
	r, _, _ := syscall.SyscallN(s.fn(8), s.this(), uintptr(unsafe.Pointer(&GUID_NULL)), uintptr(unsafe.Pointer(&pos)))
	return check("IMFSourceReader::SetCurrentPosition", r)
}

// ReadSample blocks for the stream's next sample. The sample is nil on a gap or at the end;
// flags says which.
func (s SourceReader) ReadSample(stream uint32) (sample Sample, flags uint32, t int64, err error) {
	var actual uint32
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(s.fn(9), s.this(), uintptr(stream), 0, uintptr(unsafe.Pointer(&actual)),
		uintptr(unsafe.Pointer(&flags)), uintptr(unsafe.Pointer(&t)), uintptr(unsafe.Pointer(&out)))
	return Sample{Attributes{Unknown{out}}}, flags, t, check("IMFSourceReader::ReadSample", r)
}

// Duration is the media source's MF_PD_DURATION in 100-nanosecond units.
func (s SourceReader) Duration() (int64, error) {
	var v PropVariant
	r, _, _ := syscall.SyscallN(s.fn(12), s.this(), MediaSource, uintptr(unsafe.Pointer(&MF_PD_DURATION)), uintptr(unsafe.Pointer(&v)))
	if err := check("IMFSourceReader::GetPresentationAttribute", r); err != nil {
		return 0, err
	}
	defer v.Clear()
	return v.Val, nil
}
