package capture

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	ole32                = windows.NewLazySystemDLL("ole32.dll")
	procCoInitializeEx   = ole32.NewProc("CoInitializeEx")
	procCoCreateInstance = ole32.NewProc("CoCreateInstance")
	procPropVariantClear = ole32.NewProc("PropVariantClear")
)

const (
	clsctxAll         = 0x17
	rpcEChangedMode   = 0x80010106
	coinitMultithread = 0
)

// comObject is a COM interface pointer: its first word points at the vtable.
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

func (h hresult) failed() bool { return int32(h) < 0 }

func (h hresult) err(what string) error {
	if !h.failed() {
		return nil
	}
	return fmt.Errorf("%s: HRESULT 0x%08X", what, uint32(h))
}

// withCOM runs fn on a locked OS thread in the multithreaded apartment.
func withCOM(fn func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	done, err := enterCOM()
	if err != nil {
		return err
	}
	defer done()
	return fn()
}

// enterCOM joins the calling thread to the multithreaded apartment. A thread already in
// another apartment keeps it; WASAPI works from either.
func enterCOM() (func(), error) {
	r, _, _ := procCoInitializeEx.Call(0, coinitMultithread)
	switch h := hresult(r); {
	case h == rpcEChangedMode:
		return func() {}, nil
	case h.failed():
		return nil, h.err("CoInitializeEx")
	}
	return windows.CoUninitialize, nil
}

func coCreate(clsid, iid *windows.GUID) (*comObject, error) {
	var obj *comObject
	r, _, _ := procCoCreateInstance.Call(uintptr(unsafe.Pointer(clsid)), 0, clsctxAll, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&obj)))
	if err := hresult(r).err("CoCreateInstance"); err != nil {
		return nil, err
	}
	return obj, nil
}

// propVariant is PROPVARIANT on 64-bit Windows, read only as a VT_LPWSTR.
type propVariant struct {
	vt  uint16
	_   [3]uint16
	str *uint16
	_   uintptr
}

func (p *propVariant) clear() { procPropVariantClear.Call(uintptr(unsafe.Pointer(p))) }

// takeString converts a CoTaskMemAlloc'd string and frees it.
func takeString(p *uint16) string {
	s := windows.UTF16PtrToString(p)
	windows.CoTaskMemFree(unsafe.Pointer(p))
	return s
}
