package ocr

import (
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	combase                       = windows.NewLazySystemDLL("combase.dll")
	procRoInitialize              = combase.NewProc("RoInitialize")
	procRoUninitialize            = combase.NewProc("RoUninitialize")
	procRoGetActivationFactory    = combase.NewProc("RoGetActivationFactory")
	procWindowsCreateString       = combase.NewProc("WindowsCreateString")
	procWindowsDeleteString       = combase.NewProc("WindowsDeleteString")
	procWindowsGetStringRawBuffer = combase.NewProc("WindowsGetStringRawBuffer")
)

const roInitMultithreaded = 1

// IIDs and method slots come from the Windows SDK 10.0.26100 IDL files. Every interface
// derives from IInspectable, whose six methods take slots 0 to 5.
var (
	iidAsyncInfo                  = mustGUID("{00000036-0000-0000-C000-000000000046}")
	iidOcrEngineStatics           = mustGUID("{5BFFA85A-3384-3540-9940-699120D428A8}")
	iidSoftwareBitmapStatics      = mustGUID("{DF0385DB-672F-4A9D-806E-C2442F343E86}")
	iidCryptographicBufferStatics = mustGUID("{320B7E22-3CB0-4CDF-8663-1D28910065EB}")
)

const (
	slotQueryInterface = 0
	slotRelease        = 2

	slotOcrEngineStaticsMaxImageDimension                 = 6
	slotOcrEngineStaticsTryCreateFromUserProfileLanguages = 10
	slotOcrEngineRecognizeAsync                           = 6
	slotOcrResultLines                                    = 6
	slotOcrLineWords                                      = 6
	slotOcrWordBoundingRect                               = 6
	slotOcrWordText                                       = 7

	slotVectorViewGetAt = 6
	slotVectorViewSize  = 7

	slotAsyncOperationGetResults = 8
	slotAsyncInfoStatus          = 7
	slotAsyncInfoErrorCode       = 8

	slotSoftwareBitmapStaticsCreateCopyFromBuffer     = 9
	slotCryptographicBufferStaticsCreateFromByteArray = 9
)

const (
	asyncStarted   = 0
	asyncCompleted = 1
	asyncCanceled  = 2
)

func mustGUID(s string) windows.GUID {
	g, err := windows.GUIDFromString(s)
	if err != nil {
		panic(err)
	}
	return g
}

// object is a COM interface pointer. Its first word points to the method table.
type object struct{ vtbl *[16]uintptr }

// call invokes the method in slot with the object as its first argument.
//
//go:uintptrescapes
func (o *object) call(slot int, args ...uintptr) error {
	hr, _, _ := syscall.SyscallN(o.vtbl[slot], append([]uintptr{uintptr(unsafe.Pointer(o))}, args...)...)
	return hresult(hr)
}

func (o *object) release() {
	if o != nil {
		syscall.SyscallN(o.vtbl[slotRelease], uintptr(unsafe.Pointer(o)))
	}
}

func (o *object) queryInterface(iid *windows.GUID) (*object, error) {
	var out *object
	err := o.call(slotQueryInterface, uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&out)))
	return out, err
}

// getObject calls a method whose only argument is an out pointer to an interface.
func (o *object) getObject(slot int) (*object, error) {
	var out *object
	if err := o.call(slot, uintptr(unsafe.Pointer(&out))); err != nil {
		return nil, err
	}
	if out == nil {
		return nil, windows.Errno(windows.E_POINTER)
	}
	return out, nil
}

func (o *object) vectorSize() (uint32, error) {
	var n uint32
	err := o.call(slotVectorViewSize, uintptr(unsafe.Pointer(&n)))
	return n, err
}

func (o *object) vectorAt(i uint32) (*object, error) {
	var out *object
	err := o.call(slotVectorViewGetAt, uintptr(i), uintptr(unsafe.Pointer(&out)))
	return out, err
}

func hresult(hr uintptr) error {
	if int32(hr) < 0 {
		return windows.Errno(hr)
	}
	return nil
}

func activationFactory(class string, iid *windows.GUID) (*object, error) {
	name, err := newHString(class)
	if err != nil {
		return nil, err
	}
	defer name.free()
	var f *object
	hr, _, _ := procRoGetActivationFactory.Call(uintptr(name), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&f)))
	return f, hresult(hr)
}

// await blocks until op, an IAsyncOperation<T>, finishes and returns its result. Polling
// adds at most a millisecond to a recognition that takes tens to thousands of them, and
// needs no completion handler object.
func await(op *object) (*object, error) {
	info, err := op.queryInterface(&iidAsyncInfo)
	if err != nil {
		return nil, err
	}
	defer info.release()
	for {
		var status int32
		if err := info.call(slotAsyncInfoStatus, uintptr(unsafe.Pointer(&status))); err != nil {
			return nil, err
		}
		switch status {
		case asyncStarted:
			time.Sleep(time.Millisecond)
		case asyncCompleted:
			return op.getObject(slotAsyncOperationGetResults)
		case asyncCanceled:
			return nil, errors.New("canceled")
		default:
			var code int32
			if err := info.call(slotAsyncInfoErrorCode, uintptr(unsafe.Pointer(&code))); err != nil {
				return nil, err
			}
			return nil, hresult(uintptr(uint32(code)))
		}
	}
}

type hstring uintptr

func newHString(s string) (hstring, error) {
	u, err := windows.UTF16FromString(s)
	if err != nil {
		return 0, err
	}
	var h hstring
	hr, _, _ := procWindowsCreateString.Call(uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&h)))
	return h, hresult(hr)
}

func (h hstring) String() string {
	var n uint32
	p, _, _ := procWindowsGetStringRawBuffer.Call(uintptr(h), uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return ""
	}
	// p points into the HSTRING's own memory, outside the Go heap.
	chars := unsafe.Slice(*(**uint16)(unsafe.Pointer(&p)), n)
	return windows.UTF16ToString(chars)
}

func (h hstring) free() { procWindowsDeleteString.Call(uintptr(h)) }
