package mf

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type GUID = windows.GUID

// HRESULT is a failed COM status.
type HRESULT uint32

const (
	S_OK                     = 0
	S_FALSE                  = 1
	RPC_E_CHANGED_MODE       = HRESULT(0x80010106)
	MF_E_INVALIDSTREAMNUMBER = HRESULT(0xC00D36B3)
)

func (h HRESULT) Error() string {
	msg := windows.Errno(h).Error()
	return fmt.Sprintf("HRESULT 0x%08X: %s", uint32(h), msg)
}

// Error is a failed call: the method's name and its status.
type Error struct {
	Op string
	HR HRESULT
}

func (e *Error) Error() string { return e.Op + ": " + e.HR.Error() }
func (e *Error) Unwrap() error { return e.HR }

// IsHR reports whether err carries the status hr.
func IsHR(err error, hr HRESULT) bool {
	var h HRESULT
	return errors.As(err, &h) && h == hr
}

func check(op string, r uintptr) error {
	if int32(r) < 0 {
		return &Error{Op: op, HR: HRESULT(r)}
	}
	return nil
}

var (
	ole32    = windows.NewLazySystemDLL("ole32.dll")
	mfplat   = windows.NewLazySystemDLL("mfplat.dll")
	mfreadwr = windows.NewLazySystemDLL("mfreadwrite.dll")
	d3d11    = windows.NewLazySystemDLL("d3d11.dll")

	procCoInitializeEx      = ole32.NewProc("CoInitializeEx")
	procCoUninitialize      = ole32.NewProc("CoUninitialize")
	procCoIncrementMTAUsage = ole32.NewProc("CoIncrementMTAUsage")
	procPropVariantClear    = ole32.NewProc("PropVariantClear")
)

// Unknown is a COM interface pointer. The zero value is a nil pointer.
type Unknown struct{ p unsafe.Pointer }

func (u Unknown) IsNil() bool   { return u.p == nil }
func (u Unknown) this() uintptr { return uintptr(u.p) }

// fn is the address of vtable slot i.
func (u Unknown) fn(i int) uintptr {
	vtbl := *(*unsafe.Pointer)(u.p)
	return *(*uintptr)(unsafe.Add(vtbl, i*int(unsafe.Sizeof(uintptr(0)))))
}

func (u Unknown) Release() {
	if u.p != nil {
		syscall.SyscallN(u.fn(2), u.this())
	}
}

// QueryInterface returns the object's iid interface, which the caller releases.
func (u Unknown) QueryInterface(iid *GUID) (Unknown, error) {
	var out unsafe.Pointer
	r, _, _ := syscall.SyscallN(u.fn(0), u.this(), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	if err := check("QueryInterface", r); err != nil {
		return Unknown{}, err
	}
	return Unknown{out}, nil
}

var (
	startOnce sync.Once
	startErr  error
)

// Startup starts Media Foundation for the life of the process and keeps the multithreaded
// apartment alive. MFShutdown is never called: tests and background jobs overlap, and a
// shutdown from one would tear down objects another still uses.
func Startup() error {
	startOnce.Do(func() {
		var cookie uintptr
		r, _, _ := procCoIncrementMTAUsage.Call(uintptr(unsafe.Pointer(&cookie)))
		if err := check("CoIncrementMTAUsage", r); err != nil {
			startErr = err
			return
		}
		leave, err := Enter()
		if err != nil {
			startErr = err
			return
		}
		defer leave()
		const mfVersion = 0x0002<<16 | 0x0070
		const mfStartupNoSocket = 1
		r, _, _ = procMFStartup.Call(mfVersion, mfStartupNoSocket)
		startErr = check("MFStartup", r)
	})
	return startErr
}

// Enter pins the goroutine to its OS thread and joins that thread to the multithreaded
// apartment until leave is called. A thread a caller already made single-threaded stays
// so; Media Foundation's objects are free-threaded and work from either.
func Enter() (leave func(), err error) {
	runtime.LockOSThread()
	r, _, _ := procCoInitializeEx.Call(0, windows.COINIT_MULTITHREADED)
	switch {
	case r == S_OK || r == S_FALSE:
		return func() {
			procCoUninitialize.Call()
			runtime.UnlockOSThread()
		}, nil
	case HRESULT(r) == RPC_E_CHANGED_MODE:
		return runtime.UnlockOSThread, nil
	default:
		runtime.UnlockOSThread()
		return nil, check("CoInitializeEx", r)
	}
}

const (
	VT_UI4 = 19
	VT_I8  = 20
)

// PropVariant is PROPVARIANT and VARIANT for the scalar types this package uses: a type tag
// and an 8-byte value, padded to the 24 bytes the 64-bit structs take.
type PropVariant struct {
	VT  uint16
	_   [3]uint16
	Val int64
	_   uintptr
}

func (v *PropVariant) Clear() {
	procPropVariantClear.Call(uintptr(unsafe.Pointer(v)))
}

// takeString copies a NUL-terminated UTF-16 string that COM allocated, then frees it.
func takeString(p *uint16) string {
	if p == nil {
		return ""
	}
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}
